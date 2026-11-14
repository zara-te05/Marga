// Package migrador aplica y revierte migraciones en la base de datos del
// usuario y lleva el registro en la tabla marga_history (dentro de esa BD).
package migrador

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/zarat/Motor-Marga/internal/introspeccion"
	"github.com/zarat/Motor-Marga/internal/proyecto"
)

const tabla = "marga_history"

// Clave del bloqueo: evita que dos personas apliquen migraciones a la vez.
const claveBloqueo = 727_4270

const ddlHistorial = `
CREATE TABLE IF NOT EXISTS marga_history (
    version      VARCHAR(32) PRIMARY KEY,
    nombre       TEXT        NOT NULL,
    checksum     CHAR(64)    NOT NULL,
    aplicada_en  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duracion_ms  INTEGER     NOT NULL,
    aplicada_por TEXT        NOT NULL
)`

// marga_esquema guarda el esquema tal como lo dejó la última migración de Marga.
// Compararlo con el esquema real revela cambios hechos a mano (drift).
const ddlEsquema = `
CREATE TABLE IF NOT EXISTS marga_esquema (
    id             INTEGER     PRIMARY KEY CHECK (id = 1),
    esquema        JSONB       NOT NULL,
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now()
)`

type Aplicada struct {
	Version     string
	Nombre      string
	Checksum    string
	AplicadaEn  time.Time
	DuracionMs  int
	AplicadaPor string
}

type Migrador struct {
	db *sql.DB
}

// Abrir conecta con el entorno. Por ahora Marga aplica migraciones solo en PostgreSQL.
func Abrir(ctx context.Context, e proyecto.Entorno) (*Migrador, error) {
	if e.Motor != "" && e.Motor != "postgresql" {
		return nil, fmt.Errorf("aplicar migraciones en %s todavía no está soportado (solo postgresql)", e.Motor)
	}
	url, err := e.URLReal()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("cadena de conexión inválida: %w", err)
	}
	db.SetMaxOpenConns(2)
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("no se pudo conectar: %w", err)
	}
	return &Migrador{db: db}, nil
}

func (m *Migrador) Close() error { return m.db.Close() }

// Historial lee marga_history; si la tabla no existe, la BD no tiene migraciones aplicadas.
func (m *Migrador) Historial(ctx context.Context) ([]Aplicada, error) {
	var existe bool
	if err := m.db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, tabla).Scan(&existe); err != nil {
		return nil, err
	}
	if !existe {
		return nil, nil
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT version, nombre, checksum, aplicada_en, duracion_ms, aplicada_por
		FROM marga_history`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Aplicada
	for rows.Next() {
		var a Aplicada
		if err := rows.Scan(&a.Version, &a.Nombre, &a.Checksum, &a.AplicadaEn, &a.DuracionMs, &a.AplicadaPor); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return num(out[i].Version) < num(out[j].Version) })
	return out, rows.Err()
}

func num(v string) int { n, _ := strconv.Atoi(v); return n }

// ---------------------------------------------------------------- Comparación

type Situacion string

const (
	Aplicada_  Situacion = "aplicada"
	Pendiente  Situacion = "pendiente"
	Modificada Situacion = "modificada" // el archivo cambió después de aplicarse
	Huerfana   Situacion = "huerfana"   // aplicada en la BD pero sin archivo local
	// Renumerada: el mismo SQL ya está aplicado con otro número (pasa al fusionar
	// ramas: la migración cambia de versión). No se vuelve a ejecutar.
	Renumerada Situacion = "renumerada"
)

type Fila struct {
	Version      string
	Nombre       string
	Situacion    Situacion
	Local        *proyecto.Migracion
	Registro     *Aplicada
	FueraDeOrden bool // pendiente con versión menor que la última aplicada
}

func (f Fila) ID() string { return f.Version + "_" + f.Nombre }

// Comparar cruza los archivos locales con lo aplicado en la BD.
// La identidad de una migración es su SQL (checksum), no su número: al fusionar
// ramas una migración ya aplicada puede cambiar de número.
func Comparar(locales []proyecto.Migracion, aplicadas []Aplicada) []Fila {
	porVersion := map[string]*Aplicada{}
	porChecksum := map[string]*Aplicada{}
	ultima := 0
	for i := range aplicadas {
		a := &aplicadas[i]
		porVersion[a.Version] = a
		if _, ya := porChecksum[a.Checksum]; !ya {
			porChecksum[a.Checksum] = a
		}
		ultima = max(ultima, num(a.Version))
	}
	usadas := map[string]bool{} // versiones aplicadas ya emparejadas
	filas := make([]Fila, len(locales))
	listo := make([]bool, len(locales))
	for i := range locales {
		filas[i] = Fila{Version: locales[i].Version, Nombre: locales[i].Nombre, Local: &locales[i]}
	}
	// 1) mismo número y mismo SQL
	for i, l := range locales {
		if a, ok := porVersion[l.Version]; ok && a.Checksum == l.Checksum {
			filas[i].Situacion, filas[i].Registro = Aplicada_, a
			usadas[a.Version], listo[i] = true, true
		}
	}
	// 2) mismo SQL con otro número: renumerada
	for i, l := range locales {
		if listo[i] {
			continue
		}
		if a, ok := porChecksum[l.Checksum]; ok && !usadas[a.Version] {
			filas[i].Situacion, filas[i].Registro = Renumerada, a
			usadas[a.Version], listo[i] = true, true
		}
	}
	// 3) mismo número con otro SQL (y ese SQL no está en otra parte): el archivo se editó
	for i, l := range locales {
		if listo[i] {
			continue
		}
		if a, ok := porVersion[l.Version]; ok && !usadas[a.Version] {
			filas[i].Situacion, filas[i].Registro = Modificada, a
			usadas[a.Version], listo[i] = true, true
		}
	}
	// 4) el resto está pendiente
	for i, l := range locales {
		if !listo[i] {
			filas[i].Situacion = Pendiente
			filas[i].FueraDeOrden = num(l.Version) < ultima
		}
	}
	for i := range aplicadas {
		if a := &aplicadas[i]; !usadas[a.Version] {
			filas = append(filas, Fila{Version: a.Version, Nombre: a.Nombre, Situacion: Huerfana, Registro: a})
		}
	}
	sort.SliceStable(filas, func(i, j int) bool { return num(filas[i].Version) < num(filas[j].Version) })
	return filas
}

// Renumerar actualiza marga_history cuando migraciones ya aplicadas cambiaron de número.
// Se hace en dos pasos (a un nombre temporal y luego al definitivo) para que
// intercambios como 0011↔0012 no choquen con la llave primaria.
func (m *Migrador) Renumerar(ctx context.Context, filas []Fila) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ren []Fila
	for _, f := range filas {
		if f.Situacion == Renumerada {
			ren = append(ren, f)
		}
	}
	for _, f := range ren {
		if _, err := tx.ExecContext(ctx, `UPDATE marga_history SET version = $1 WHERE version = $2`,
			"tmp_"+f.Registro.Version, f.Registro.Version); err != nil {
			return err
		}
	}
	for _, f := range ren {
		if _, err := tx.ExecContext(ctx, `UPDATE marga_history SET version = $1, nombre = $2 WHERE version = $3`,
			f.Version, f.Nombre, "tmp_"+f.Registro.Version); err != nil {
			return fmt.Errorf("renumerar %s: %w", f.ID(), err)
		}
	}
	return tx.Commit()
}

// Registrar anota una migración como aplicada SIN ejecutarla (la usa `marga commit`,
// porque los cambios ya están hechos en la base) y actualiza la línea base.
func (m *Migrador) Registrar(ctx context.Context, mig proyecto.Migracion, autor string) (*introspeccion.Esquema, error) {
	conn, err := m.bloquear(ctx)
	if err != nil {
		return nil, err
	}
	defer liberar(conn)
	if err := asegurarTablas(ctx, conn); err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO marga_history (version, nombre, checksum, duracion_ms, aplicada_por) VALUES ($1, $2, $3, 0, $4)`,
		mig.Version, mig.Nombre, mig.Checksum, autor); err != nil {
		return nil, err
	}
	snap, err := guardarLineaBase(ctx, tx)
	if err != nil {
		return nil, err
	}
	return snap, tx.Commit()
}

// ---------------------------------------------------------------- Aplicar / revertir

// Evento informa el resultado de cada migración mientras se aplica.
// esquema es el esquema justo después de esa migración (nil si falló).
type Evento func(mig proyecto.Migracion, dur time.Duration, esquema *introspeccion.Esquema, err error)

func (m *Migrador) bloquear(ctx context.Context) (*sql.Conn, error) {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, claveBloqueo).Scan(&ok); err != nil {
		conn.Close()
		return nil, err
	}
	if !ok {
		conn.Close()
		return nil, errors.New("otra sesión está aplicando migraciones en esta base; intenta en un momento")
	}
	return conn, nil
}

func liberar(conn *sql.Conn) {
	conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, claveBloqueo)
	conn.Close()
}

// Subir aplica las migraciones en orden. Cada una va en su propia transacción
// (salvo las marcadas con "-- marga:sin-transaccion"); si una falla, se detiene.
func (m *Migrador) Subir(ctx context.Context, migs []proyecto.Migracion, autor string, ev Evento) error {
	conn, err := m.bloquear(ctx)
	if err != nil {
		return err
	}
	defer liberar(conn)
	if err := asegurarTablas(ctx, conn); err != nil {
		return err
	}
	for _, mig := range migs {
		inicio := time.Now()
		var snap *introspeccion.Esquema
		err := ejecutar(ctx, conn, mig.SinTransaccion, mig.Up, func(q execer) error {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO marga_history (version, nombre, checksum, duracion_ms, aplicada_por)
				VALUES ($1, $2, $3, $4, $5)`,
				mig.Version, mig.Nombre, mig.Checksum, int(time.Since(inicio).Milliseconds()), autor); err != nil {
				return err
			}
			var err error
			snap, err = guardarLineaBase(ctx, q)
			return err
		})
		if err != nil {
			err = explicar(err, mig.Up)
			snap = nil
		}
		if ev != nil {
			ev(mig, time.Since(inicio), snap, err)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", mig.ID(), err)
		}
	}
	return nil
}

// Bajar ejecuta el DOWN de cada migración (en el orden recibido) y la borra del historial.
func (m *Migrador) Bajar(ctx context.Context, migs []proyecto.Migracion, ev Evento) error {
	conn, err := m.bloquear(ctx)
	if err != nil {
		return err
	}
	defer liberar(conn)
	if err := asegurarTablas(ctx, conn); err != nil {
		return err
	}
	for _, mig := range migs {
		inicio := time.Now()
		var err error
		var snap *introspeccion.Esquema
		if mig.Down == "" {
			err = errors.New("no tiene script DOWN (archivo .down.sql vacío)")
		} else {
			err = ejecutar(ctx, conn, mig.SinTransaccion, mig.Down, func(q execer) error {
				if _, err := q.ExecContext(ctx, `DELETE FROM marga_history WHERE version = $1`, mig.Version); err != nil {
					return err
				}
				var err error
				snap, err = guardarLineaBase(ctx, q)
				return err
			})
			if err != nil {
				err = explicar(err, mig.Down)
				snap = nil
			}
		}
		if ev != nil {
			ev(mig, time.Since(inicio), snap, err)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", mig.ID(), err)
		}
	}
	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func asegurarTablas(ctx context.Context, q execer) error {
	if _, err := q.ExecContext(ctx, ddlHistorial); err != nil {
		return fmt.Errorf("no se pudo crear %s: %w", tabla, err)
	}
	if _, err := q.ExecContext(ctx, ddlEsquema); err != nil {
		return fmt.Errorf("no se pudo crear marga_esquema: %w", err)
	}
	return nil
}

// guardarLineaBase lee el esquema actual y lo guarda como "lo que dejó Marga".
func guardarLineaBase(ctx context.Context, q execer) (*introspeccion.Esquema, error) {
	e, err := introspeccion.Leer(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer el esquema: %w", err)
	}
	b, _ := json.Marshal(e)
	if _, err := q.ExecContext(ctx, `
		INSERT INTO marga_esquema (id, esquema) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET esquema = EXCLUDED.esquema, actualizado_en = now()`, string(b)); err != nil {
		return nil, err
	}
	return &e, nil
}

// ---------------------------------------------------------------- Esquema y drift

// EsquemaReal lee el esquema tal como está ahora en la base.
func (m *Migrador) EsquemaReal(ctx context.Context) (introspeccion.Esquema, error) {
	return introspeccion.Leer(ctx, m.db)
}

// LineaBase devuelve el esquema que dejó la última operación de Marga (nil si no hay).
func (m *Migrador) LineaBase(ctx context.Context) (*introspeccion.Esquema, error) {
	var existe bool
	if err := m.db.QueryRowContext(ctx, `SELECT to_regclass('marga_esquema') IS NOT NULL`).Scan(&existe); err != nil || !existe {
		return nil, err
	}
	var raw []byte
	err := m.db.QueryRowContext(ctx, `SELECT esquema FROM marga_esquema WHERE id = 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e introspeccion.Esquema
	return &e, json.Unmarshal(raw, &e)
}

// AceptarEsquema toma el esquema actual como nueva línea base (acepta el drift).
func (m *Migrador) AceptarEsquema(ctx context.Context) (*introspeccion.Esquema, error) {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := asegurarTablas(ctx, conn); err != nil {
		return nil, err
	}
	return guardarLineaBase(ctx, conn)
}

// Drift compara la línea base con el esquema real.
// nuevaBase=true si no había línea base y se acaba de crear con el estado actual.
func (m *Migrador) Drift(ctx context.Context) (cambios []string, real introspeccion.Esquema, nuevaBase bool, err error) {
	if real, err = m.EsquemaReal(ctx); err != nil {
		return
	}
	base, err := m.LineaBase(ctx)
	if err != nil {
		return
	}
	if base == nil {
		// Primera vez (o base creada antes de la fase 2): se adopta el estado actual.
		_, err = m.AceptarEsquema(ctx)
		return nil, real, true, err
	}
	return introspeccion.Comparar(*base, real), real, false, nil
}

// ejecutar corre el SQL y el registro en el historial juntos: o pasan los dos o ninguno.
func ejecutar(ctx context.Context, conn *sql.Conn, sinTx bool, sqlText string, registrar func(execer) error) error {
	if sinTx {
		if _, err := conn.ExecContext(ctx, sqlText); err != nil {
			return err
		}
		return registrar(conn)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		tx.Rollback()
		return err
	}
	if err := registrar(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// explicar convierte un error de PostgreSQL en algo legible, con número de línea.
func explicar(err error, sqlText string) error {
	var pe *pq.Error
	if !errors.As(err, &pe) {
		return err
	}
	msg := pe.Message
	if pos, e := strconv.Atoi(pe.Position); e == nil && pos > 0 && pos <= len(sqlText) {
		linea := strings.Count(sqlText[:pos-1], "\n") + 1
		msg = fmt.Sprintf("línea %d: %s", linea, msg)
	}
	if pe.Detail != "" {
		msg += " (" + pe.Detail + ")"
	}
	if pe.Hint != "" {
		msg += ". Sugerencia: " + pe.Hint
	}
	return errors.New(msg)
}
