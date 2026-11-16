package migrador

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zara-te05/Marga/internal/proyecto"
)

// Tipos de script que se registran en marga_scripts.
const (
	TipoRepetible = "repetible"
	TipoSeed      = "seed"
)

// marga_scripts recuerda qué versión (checksum) de cada repetible y seed se aplicó.
const ddlScripts = `
CREATE TABLE IF NOT EXISTS marga_scripts (
    tipo         VARCHAR(16) NOT NULL,
    archivo      TEXT        NOT NULL,
    checksum     CHAR(64)    NOT NULL,
    aplicado_en  TIMESTAMPTZ NOT NULL DEFAULT now(),
    aplicado_por TEXT        NOT NULL,
    PRIMARY KEY (tipo, archivo)
)`

type ScriptAplicado struct {
	Archivo     string
	Checksum    string
	AplicadoEn  time.Time
	AplicadoPor string
}

// Scripts devuelve lo aplicado de un tipo, por archivo.
func (m *Migrador) Scripts(ctx context.Context, tipo string) (map[string]ScriptAplicado, error) {
	out := map[string]ScriptAplicado{}
	var existe bool
	if err := m.db.QueryRowContext(ctx, `SELECT to_regclass('marga_scripts') IS NOT NULL`).Scan(&existe); err != nil || !existe {
		return out, err
	}
	rows, err := m.db.QueryContext(ctx, `SELECT archivo, checksum, aplicado_en, aplicado_por FROM marga_scripts WHERE tipo = $1`, tipo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s ScriptAplicado
		if err := rows.Scan(&s.Archivo, &s.Checksum, &s.AplicadoEn, &s.AplicadoPor); err != nil {
			return nil, err
		}
		out[s.Archivo] = s
	}
	return out, rows.Err()
}

type EstadoScript string

const (
	ScriptNuevo    EstadoScript = "nuevo"    // nunca se aplicó en esta base
	ScriptCambiado EstadoScript = "cambiado" // el archivo cambió desde que se aplicó
	ScriptAlDia    EstadoScript = "al_dia"
)

type FilaScript struct {
	Script   proyecto.Script
	Estado   EstadoScript
	Registro *ScriptAplicado
}

// CompararScripts cruza los archivos con lo aplicado. borrados: aplicados cuyo archivo ya no existe.
func CompararScripts(locales []proyecto.Script, aplicados map[string]ScriptAplicado) (filas []FilaScript, borrados []string) {
	vistos := map[string]bool{}
	for _, s := range locales {
		vistos[s.Archivo] = true
		f := FilaScript{Script: s, Estado: ScriptNuevo}
		if a, ok := aplicados[s.Archivo]; ok {
			f.Registro = &a
			f.Estado = ScriptAlDia
			if a.Checksum != s.Checksum {
				f.Estado = ScriptCambiado
			}
		}
		filas = append(filas, f)
	}
	for a := range aplicados {
		if !vistos[a] {
			borrados = append(borrados, a)
		}
	}
	return filas, borrados
}

// EventoScript informa el resultado de cada script mientras se aplica.
type EventoScript func(s proyecto.Script, dur time.Duration, err error)

// AplicarScripts ejecuta cada script en su propia transacción junto con su registro.
// Con lineaBase=true (repetibles) también actualiza la línea base del drift, porque
// el cambio lo hizo Marga. Los seeds solo cambian datos: no la tocan.
func (m *Migrador) AplicarScripts(ctx context.Context, tipo string, scripts []proyecto.Script, autor string, lineaBase bool, ev EventoScript) error {
	conn, err := m.bloquear(ctx)
	if err != nil {
		return err
	}
	defer liberar(conn)
	if err := asegurarTablas(ctx, conn); err != nil {
		return err
	}
	for _, s := range scripts {
		inicio := time.Now()
		sinTx := strings.Contains(s.SQL, "marga:sin-transaccion")
		err := ejecutar(ctx, conn, sinTx, s.SQL, func(q execer) error {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO marga_scripts (tipo, archivo, checksum, aplicado_por) VALUES ($1, $2, $3, $4)
				ON CONFLICT (tipo, archivo) DO UPDATE SET checksum = EXCLUDED.checksum,
				    aplicado_en = now(), aplicado_por = EXCLUDED.aplicado_por`,
				tipo, s.Archivo, s.Checksum, autor); err != nil {
				return err
			}
			if lineaBase {
				_, err := guardarLineaBase(ctx, q)
				return err
			}
			return nil
		})
		if err != nil {
			err = explicar(err, s.SQL)
		}
		if ev != nil {
			ev(s, time.Since(inicio), err)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", s.Archivo, err)
		}
	}
	return nil
}

// OlvidarScripts quita del registro scripts cuyo archivo ya no existe.
func (m *Migrador) OlvidarScripts(ctx context.Context, tipo string, archivos []string) error {
	for _, a := range archivos {
		if _, err := m.db.ExecContext(ctx, `DELETE FROM marga_scripts WHERE tipo = $1 AND archivo = $2`, tipo, a); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- SQL suelto

// Resultado es una tabla devuelta por una consulta (las primeras `limite` filas).
type Resultado struct {
	Columnas []string
	Filas    [][]string // NULL llega como nil en Nulos
	Nulos    [][]bool
	Total    int
}

// Consultar ejecuta SQL suelto (una o varias sentencias) en una transacción.
// Con guardar=false la deshace al final: ves el resultado sin cambiar la base.
func (m *Migrador) Consultar(ctx context.Context, sqlText string, guardar bool, limite int) ([]Resultado, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Sin parámetros, lib/pq usa el protocolo simple: acepta varias sentencias
	// y devuelve un conjunto de resultados por cada una.
	rows, err := tx.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, explicar(err, sqlText)
	}
	var out []Resultado
	for {
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		if len(cols) > 0 {
			r := Resultado{Columnas: cols}
			for rows.Next() {
				r.Total++
				if r.Total > limite {
					continue
				}
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					rows.Close()
					return nil, err
				}
				fila, nulos := make([]string, len(cols)), make([]bool, len(cols))
				for i, v := range vals {
					fila[i], nulos[i] = textoDe(v)
				}
				r.Filas, r.Nulos = append(r.Filas, fila), append(r.Nulos, nulos)
			}
			out = append(out, r)
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, explicar(err, sqlText)
	}
	rows.Close()
	if guardar {
		if err := tx.Commit(); err != nil {
			return nil, explicar(err, sqlText)
		}
	}
	return out, nil
}

func textoDe(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case []byte:
		return string(x), false
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02"), false
		}
		return x.Format("2006-01-02 15:04:05"), false
	case sql.RawBytes:
		return string(x), false
	}
	return fmt.Sprint(v), false
}
