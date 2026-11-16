// Package ramabd crea, lista y borra las bases de datos por rama de git:
// tienda → tienda__feature_facturas. Solo se usa en entornos personales (dev, local).
//
// Cada base que crea Marga lleva la marca COMMENT ON DATABASE … 'marga:rama=<rama>'.
// Marga solo borra bases con esa marca: nunca puede borrar la base principal.
package ramabd

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
)

// MaxNombre es el largo máximo de un identificador en PostgreSQL (NAMEDATALEN-1).
const MaxNombre = 63

// PrefijoMarca identifica las bases creadas por Marga.
const PrefijoMarca = "marga:rama="

var reNoAlfa = regexp.MustCompile(`[^a-z0-9]+`)

var sinAcentos = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// Sufijo convierte una rama en algo válido para un nombre de base: feature/Facturas-2 → feature_facturas_2.
func Sufijo(rama string) string {
	s := sinAcentos.Replace(strings.ToLower(rama))
	return strings.Trim(reNoAlfa.ReplaceAllString(s, "_"), "_")
}

// Nombre calcula la base de una rama: <base>__<rama>. Si pasa de 63 caracteres se
// recorta y se agrega un hash corto de la rama, para que dos ramas largas no choquen.
func Nombre(base, rama string) string {
	return nombre(base, rama, false)
}

// NombreConHash es como Nombre pero siempre agrega el hash (para resolver un choque:
// feature/a-b y feature/a_b dan el mismo sufijo).
func NombreConHash(base, rama string) string {
	return nombre(base, rama, true)
}

func nombre(base, rama string, conHash bool) string {
	suf := Sufijo(rama)
	if suf == "" {
		suf = "rama"
	}
	n := base + "__" + suf
	if !conHash && len(n) <= MaxNombre {
		return n
	}
	h := sha1.Sum([]byte(rama))
	corto := "_" + hex.EncodeToString(h[:])[:8]
	if len(n)+len(corto) <= MaxNombre {
		return n + corto
	}
	n = n[:MaxNombre-len(corto)]
	return strings.TrimRight(n, "_") + corto
}

// ---------------------------------------------------------------- Cadenas de conexión

var reDbname = regexp.MustCompile(`(?:^|\s)dbname\s*=\s*('(?:[^'\\]|\\.)*'|\S+)`)

// BaseDeURL devuelve el nombre de la base de una cadena de conexión de PostgreSQL
// (postgres://…/tienda o "host=… dbname=tienda").
func BaseDeURL(cadena string) (string, error) {
	if esURL(cadena) {
		u, err := url.Parse(cadena)
		if err != nil {
			return "", fmt.Errorf("cadena de conexión inválida: %w", err)
		}
		b := strings.TrimPrefix(u.Path, "/")
		if b == "" {
			return "", errors.New("la cadena de conexión no dice qué base usar (falta /<base> al final)")
		}
		return b, nil
	}
	m := reDbname.FindStringSubmatch(cadena)
	if m == nil {
		return "", errors.New("la cadena de conexión no dice qué base usar (falta dbname=)")
	}
	return strings.Trim(m[1], "'"), nil
}

// ConBase devuelve la misma cadena de conexión apuntando a otra base.
func ConBase(cadena, base string) (string, error) {
	if esURL(cadena) {
		u, err := url.Parse(cadena)
		if err != nil {
			return "", fmt.Errorf("cadena de conexión inválida: %w", err)
		}
		u.Path, u.RawPath = "/"+base, ""
		return u.String(), nil
	}
	if reDbname.MatchString(cadena) {
		return reDbname.ReplaceAllStringFunc(cadena, func(s string) string {
			pre := s[:len(s)-len(strings.TrimLeft(s, " \t\n"))]
			return pre + "dbname=" + base
		}), nil
	}
	return strings.TrimSpace(cadena) + " dbname=" + base, nil
}

func esURL(s string) bool {
	return strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://")
}

// ---------------------------------------------------------------- Operaciones en el servidor

// Admin es una conexión a la base de mantenimiento (postgres) del mismo servidor.
// CREATE/DROP DATABASE no se pueden hacer conectado a la base que se copia o borra.
type Admin struct {
	db *sql.DB
}

// AbrirAdmin conecta al mismo servidor que `cadena`, a la base "postgres" (o "template1").
func AbrirAdmin(ctx context.Context, cadena string) (*Admin, error) {
	var ultimo error
	for _, b := range []string{"postgres", "template1"} {
		u, err := ConBase(cadena, b)
		if err != nil {
			return nil, err
		}
		db, err := sql.Open("postgres", u)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = db.PingContext(pctx)
		cancel()
		if err == nil {
			return &Admin{db: db}, nil
		}
		db.Close()
		ultimo = err
	}
	return nil, fmt.Errorf("no se pudo conectar al servidor de PostgreSQL para crear la base de la rama: %w", ultimo)
}

func (a *Admin) Close() error { return a.db.Close() }

// Existe indica si la base existe en el servidor.
func (a *Admin) Existe(ctx context.Context, base string) (bool, error) {
	var ok bool
	err := a.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, base).Scan(&ok)
	return ok, err
}

// Marca devuelve la rama con la que Marga marcó la base ("" si no tiene marca de Marga).
func (a *Admin) Marca(ctx context.Context, base string) (string, error) {
	var c sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname = $1`, base).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	rama, ok := strings.CutPrefix(c.String, PrefijoMarca)
	if !ok {
		return "", nil
	}
	return rama, nil
}

// Tamano devuelve el tamaño de la base en bytes.
func (a *Admin) Tamano(ctx context.Context, base string) (int64, error) {
	var n int64
	err := a.db.QueryRowContext(ctx, `SELECT pg_database_size($1)`, base).Scan(&n)
	return n, err
}

// Conexion es alguien conectado a una base (pgAdmin, la app, otra terminal…).
type Conexion struct {
	PID         int
	Usuario     string
	Aplicacion  string
	Cliente     string
	ConectadoEn *time.Time
}

func (c Conexion) String() string {
	app := c.Aplicacion
	if app == "" {
		app = "programa sin nombre"
	}
	s := fmt.Sprintf("%s (usuario %s", app, c.Usuario)
	if c.Cliente != "" {
		s += ", desde " + c.Cliente
	}
	if c.ConectadoEn != nil {
		s += ", conectado " + c.ConectadoEn.Local().Format("2006-01-02 15:04")
	}
	return s + fmt.Sprintf(", pid %d)", c.PID)
}

// Conectados lista quién tiene abierta la base (sin contar esta conexión).
func (a *Admin) Conectados(ctx context.Context, base string) ([]Conexion, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT pid, coalesce(usename, ''), coalesce(application_name, ''),
		       coalesce(host(client_addr), ''), backend_start
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
		ORDER BY backend_start`, base)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conexion
	for rows.Next() {
		var c Conexion
		var t sql.NullTime
		if err := rows.Scan(&c.PID, &c.Usuario, &c.Aplicacion, &c.Cliente, &t); err != nil {
			return nil, err
		}
		if t.Valid {
			c.ConectadoEn = &t.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// esperarLibre espera un momento a que se cierren las conexiones a la base: una
// conexión que se acaba de cerrar (la de la propia CLI) tarda unos milisegundos en
// desaparecer de pg_stat_activity. Devuelve las que sigan abiertas.
func (a *Admin) esperarLibre(ctx context.Context, base string, espera time.Duration) ([]Conexion, error) {
	limite := time.Now().Add(espera)
	for {
		cs, err := a.Conectados(ctx, base)
		if err != nil || len(cs) == 0 || time.Now().After(limite) {
			return cs, err
		}
		select {
		case <-ctx.Done():
			return cs, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// ErrOcupada: PostgreSQL no deja copiar (ni borrar) una base con conexiones abiertas.
type ErrOcupada struct {
	Base       string
	Conexiones []Conexion
}

func (e *ErrOcupada) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "la base %s tiene conexiones abiertas", e.Base)
	for _, c := range e.Conexiones {
		b.WriteString("\n    - " + c.String())
	}
	return b.String()
}

// Copiar crea `destino` como copia exacta (esquema y datos) de `origen` y la marca.
func (a *Admin) Copiar(ctx context.Context, origen, destino, rama string) error {
	if cs, err := a.esperarLibre(ctx, origen, 1500*time.Millisecond); err != nil {
		return err
	} else if len(cs) > 0 {
		return &ErrOcupada{Base: origen, Conexiones: cs}
	}
	_, err := a.db.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(destino)+` TEMPLATE `+pq.QuoteIdentifier(origen))
	if err != nil {
		var pe *pq.Error
		if errors.As(err, &pe) && pe.Code == "55006" { // alguien se conectó justo ahora
			cs, _ := a.Conectados(ctx, origen)
			return &ErrOcupada{Base: origen, Conexiones: cs}
		}
		return fmt.Errorf("no se pudo copiar %s en %s: %w", origen, destino, err)
	}
	return a.marcar(ctx, destino, rama)
}

// CrearVacia crea `destino` sin tablas y la marca.
func (a *Admin) CrearVacia(ctx context.Context, destino, rama string) error {
	if _, err := a.db.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(destino)); err != nil {
		return fmt.Errorf("no se pudo crear %s: %w", destino, err)
	}
	return a.marcar(ctx, destino, rama)
}

func (a *Admin) marcar(ctx context.Context, base, rama string) error {
	_, err := a.db.ExecContext(ctx, `COMMENT ON DATABASE `+pq.QuoteIdentifier(base)+` IS `+pq.QuoteLiteral(PrefijoMarca+rama))
	if err != nil {
		// Sin marca Marga no podría borrarla después: mejor no dejarla a medias.
		_, _ = a.db.ExecContext(ctx, `DROP DATABASE IF EXISTS `+pq.QuoteIdentifier(base))
		return fmt.Errorf("no se pudo marcar %s como base de rama: %w", base, err)
	}
	return nil
}

// ErrSinMarca: la base no la creó Marga, así que no se borra.
var ErrSinMarca = errors.New("no tiene la marca de Marga")

// Borrar elimina una base de rama. Se niega si la base no tiene la marca de Marga
// (así nunca borra la base principal ni ninguna otra) o si es `protegida`.
// Con forzar=false también se niega si hay conexiones abiertas.
func (a *Admin) Borrar(ctx context.Context, base, protegida string, forzar bool) error {
	if base == "" || base == protegida {
		return fmt.Errorf("%s es la base principal: Marga no la borra", base)
	}
	existe, err := a.Existe(ctx, base)
	if err != nil {
		return err
	}
	if !existe {
		return nil
	}
	marca, err := a.Marca(ctx, base)
	if err != nil {
		return err
	}
	if marca == "" {
		return fmt.Errorf("no se borró %s: %w (no la creó Marga como base de rama)", base, ErrSinMarca)
	}
	if !forzar {
		if cs, err := a.Conectados(ctx, base); err != nil {
			return err
		} else if len(cs) > 0 {
			return &ErrOcupada{Base: base, Conexiones: cs}
		}
	}
	q := `DROP DATABASE ` + pq.QuoteIdentifier(base)
	if forzar {
		q += ` WITH (FORCE)` // PostgreSQL 13+: cierra las conexiones abiertas
	}
	if _, err := a.db.ExecContext(ctx, q); err != nil {
		var pe *pq.Error
		if errors.As(err, &pe) && pe.Code == "55006" {
			cs, _ := a.Conectados(ctx, base)
			return &ErrOcupada{Base: base, Conexiones: cs}
		}
		return fmt.Errorf("no se pudo borrar %s: %w", base, err)
	}
	return nil
}

// TamanoLegible: 8421376 → "8.0 MB".
func TamanoLegible(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
}
