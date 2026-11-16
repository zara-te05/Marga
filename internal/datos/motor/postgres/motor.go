package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

// Motor es PostgreSQL para el resto de Marga (implementa motor.Motor).
type Motor struct{ Dialecto }

func (Motor) Nombre() string { return "postgresql" }
func (Motor) Driver() string { return "postgres" }

func (Motor) Leer(ctx context.Context, q Querier) (esquema.Esquema, error) { return Leer(ctx, q) }

func (Motor) Generar(antes, despues esquema.Esquema) esquema.Cambios { return Generar(antes, despues) }

// DDLEnTransaccion: en PostgreSQL CREATE, ALTER y DROP se pueden deshacer.
func (Motor) DDLEnTransaccion() bool { return true }

// Bloquear toma un candado de sesión (pg_try_advisory_lock): dos `marga up`
// sobre la misma base no corren a la vez. ok=false si otro lo tiene.
func (Motor) Bloquear(ctx context.Context, conn *sql.Conn, clave int64) (bool, error) {
	var ok bool
	err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, clave).Scan(&ok)
	return ok, err
}

func (Motor) Desbloquear(ctx context.Context, conn *sql.Conn, clave int64) {
	conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, clave)
}

// Preparacion es lo que una base vacía necesita antes del esquema adoptado y que
// Marga no sigue como objeto: los esquemas distintos de public, las extensiones
// instaladas y las secuencias que no pertenecen a una columna. Devuelve el SQL
// para crearlo (antes del esquema) y para quitarlo (después del DOWN); las
// extensiones no se quitan: pueden ser de otras bases del mismo servidor.
func (Motor) Preparacion(ctx context.Context, q Querier) (crear, quitar []string, err error) {
	leer := func(consulta string, f func(rows *sql.Rows) error) error {
		rows, err := q.QueryContext(ctx, consulta)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := f(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	var esquemas []string
	err = leer(`SELECT nspname FROM pg_namespace
		WHERE nspname NOT IN ('public', 'information_schema') AND nspname NOT LIKE 'pg\_%'
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_namespace'::regclass AND d.objid = pg_namespace.oid AND d.deptype = 'e')
		ORDER BY 1`, func(r *sql.Rows) error {
		var n string
		if err := r.Scan(&n); err != nil {
			return err
		}
		esquemas = append(esquemas, citar(n))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for _, n := range esquemas {
		crear = append(crear, "CREATE SCHEMA IF NOT EXISTS "+n+";")
	}
	err = leer(`SELECT extname FROM pg_extension WHERE extname <> 'plpgsql' ORDER BY 1`, func(r *sql.Rows) error {
		var n string
		if err := r.Scan(&n); err != nil {
			return err
		}
		crear = append(crear, "CREATE EXTENSION IF NOT EXISTS "+citar(n)+";")
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	err = leer(`SELECT CASE WHEN n.nspname = 'public' THEN c.relname ELSE n.nspname || '.' || c.relname END,
		       s.seqstart, s.seqincrement
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace JOIN pg_sequence s ON s.seqrelid = c.oid
		WHERE c.relkind = 'S' AND `+filtroEsquemas+`
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype IN ('a', 'i', 'e'))
		ORDER BY 1`, func(r *sql.Rows) error {
		var n string
		var inicio, paso int64
		if err := r.Scan(&n, &inicio, &paso); err != nil {
			return err
		}
		sq := "CREATE SEQUENCE IF NOT EXISTS " + citar(n)
		if paso != 1 {
			sq += fmt.Sprintf(" INCREMENT BY %d", paso)
		}
		if inicio != 1 {
			sq += fmt.Sprintf(" START WITH %d", inicio)
		}
		crear = append(crear, sq+";")
		quitar = append(quitar, "DROP SEQUENCE IF EXISTS "+citar(n)+";")
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for i := len(esquemas) - 1; i >= 0; i-- {
		quitar = append(quitar, "DROP SCHEMA IF EXISTS "+esquemas[i]+";")
	}
	return crear, quitar, nil
}

// citar pone comillas solo si el nombre las necesita ("uuid-ossp", esquema.tabla queda igual).
func citar(n string) string {
	partes := strings.Split(n, ".")
	for i, p := range partes {
		simple := p != ""
		for _, r := range p {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				simple = false
			}
		}
		if !simple {
			partes[i] = pq.QuoteIdentifier(p)
		}
	}
	return strings.Join(partes, ".")
}
