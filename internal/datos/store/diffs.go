package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zara-te05/Marga/internal/modelos/diff"
)

// objetoSnap es el estado de un objeto (tabla, función, vista…) en un commit.
type objetoSnap struct {
	tipo, ddl string
}

// esquemaEn reconstruye el estado del esquema en un commit: para cada objeto,
// el snapshot más reciente en su ascendencia. DDL vacío = objeto eliminado.
func esquemaEn(ctx context.Context, q Querier, hash string) (map[string]objetoSnap, error) {
	rows, err := q.QueryContext(ctx, `
		WITH RECURSIVE hist AS (
		    SELECT hash_id, padre_hash, 0 AS d FROM commit WHERE hash_id = $1
		    UNION ALL
		    SELECT c.hash_id, c.padre_hash, hist.d + 1
		    FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
		)
		SELECT DISTINCT ON (s.tabla) s.tabla, s.tipo, s.ddl
		FROM hist JOIN snapshot_esquema s ON s.commit_hash_id = hist.hash_id
		ORDER BY s.tabla, hist.d`, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]objetoSnap{}
	for rows.Next() {
		var t string
		var o objetoSnap
		if err := rows.Scan(&t, &o.tipo, &o.ddl); err != nil {
			return nil, err
		}
		if o.ddl != "" {
			out[t] = o
		}
	}
	return out, rows.Err()
}

// sqlAcumulado concatena el sql_up de toda la ascendencia (orden cronológico).
// Se usa como alternativa cuando el repositorio no guarda snapshots.
func sqlAcumulado(ctx context.Context, q Querier, hash string) (string, error) {
	var s sql.NullString
	err := q.QueryRowContext(ctx, `
		WITH RECURSIVE hist AS (
		    SELECT hash_id, padre_hash, 0 AS d FROM commit WHERE hash_id = $1
		    UNION ALL
		    SELECT c.hash_id, c.padre_hash, hist.d + 1
		    FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
		)
		SELECT string_agg(rtrim(c.sql_up, E'\n'), E'\n\n' ORDER BY hist.d DESC)
		FROM hist JOIN commit c ON c.hash_id = hist.hash_id`, hash).Scan(&s)
	return s.String, err
}

// TablaDiff es el cambio de un objeto del esquema (tabla, función, vista, trigger o tipo).
type TablaDiff struct {
	Tabla      string       `json:"tabla"`  // nombre del objeto
	Tipo       string       `json:"tipo"`   // tabla | funcion | procedimiento | vista | vista_materializada | trigger | tipo
	Estado     string       `json:"estado"` // agregada | eliminada | modificada
	Agregadas  int          `json:"agregadas"`
	Eliminadas int          `json:"eliminadas"`
	Lineas     []diff.Linea `json:"lineas"`
}

type ResultadoDiff struct {
	Desde      *string         `json:"desde"`
	Hasta      string          `json:"hasta"`
	Modo       string          `json:"modo"` // "snapshots" o "sql"
	Commits    []CommitResumen `json:"commits"`
	Tablas     []TablaDiff     `json:"tablas"`
	DesdeCache bool            `json:"desde_cache"`
}

// Diff compara el esquema entre dos commits (desde puede ser nil = esquema vacío).
// El resultado se guarda en cache_diff durante DiffTTL.
func (s *Store) Diff(ctx context.Context, repoID int64, desdeRef, hastaRef string) (ResultadoDiff, error) {
	var res ResultadoDiff
	// Ambos extremos aceptan nombre de rama o (prefijo de) hash.
	hastaP, err := s.resolverRef(ctx, s.DB, repoID, hastaRef)
	if err != nil {
		return res, err
	}
	if hastaP == nil {
		return res, errf(ErrInvalido, "la rama '"+hastaRef+"' aún no tiene commits")
	}
	hasta := *hastaP
	var desde *string
	if desdeRef != "" {
		if desde, err = s.resolverRef(ctx, s.DB, repoID, desdeRef); err != nil {
			return res, err
		}
	}

	if desde != nil {
		var raw []byte
		err := s.DB.QueryRowContext(ctx, `
			SELECT diff_calculado FROM cache_diff
			WHERE commit_origen=$1 AND commit_destino=$2 AND expira_en > now()`, *desde, hasta).Scan(&raw)
		if err == nil && json.Unmarshal(raw, &res) == nil {
			res.DesdeCache = true
			return res, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return res, err
		}
	}

	res = ResultadoDiff{Desde: desde, Hasta: hasta, Commits: []CommitResumen{}, Tablas: []TablaDiff{}}

	// Commits alcanzables desde "hasta" pero no desde "desde" (git log desde..hasta).
	var desdeArg any
	if desde != nil {
		desdeArg = *desde
	}
	rows, err := s.DB.QueryContext(ctx, `
		WITH RECURSIVE a AS (
		    SELECT hash_id, padre_hash FROM commit WHERE hash_id = $1
		    UNION SELECT c.hash_id, c.padre_hash FROM commit c JOIN a ON c.hash_id = a.padre_hash
		), b AS (
		    SELECT hash_id, padre_hash FROM commit WHERE hash_id = $2
		    UNION SELECT c.hash_id, c.padre_hash FROM commit c JOIN b ON c.hash_id = b.padre_hash
		)
		SELECT c.hash_id, c.mensaje, c.fecha, u.nombre
		FROM commit c LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		WHERE c.hash_id IN (SELECT hash_id FROM a EXCEPT SELECT hash_id FROM b)
		ORDER BY c.fecha`, hasta, desdeArg)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var c CommitResumen
		if err := rows.Scan(&c.Hash, &c.Mensaje, &c.Fecha, &c.Autor); err != nil {
			rows.Close()
			return res, err
		}
		c.HashCorto = corto(c.Hash)
		res.Commits = append(res.Commits, c)
	}
	rows.Close()

	antes, despues := map[string]objetoSnap{}, map[string]objetoSnap{}
	if desde != nil {
		if antes, err = esquemaEn(ctx, s.DB, *desde); err != nil {
			return res, err
		}
	}
	if despues, err = esquemaEn(ctx, s.DB, hasta); err != nil {
		return res, err
	}

	if len(antes) == 0 && len(despues) == 0 {
		// Sin snapshots: comparamos el SQL acumulado.
		res.Modo = "sql"
		var a, b string
		if desde != nil {
			if a, err = sqlAcumulado(ctx, s.DB, *desde); err != nil {
				return res, err
			}
		}
		if b, err = sqlAcumulado(ctx, s.DB, hasta); err != nil {
			return res, err
		}
		if t := tablaDiff("migraciones.sql", "sql", a, b); t != nil {
			res.Tablas = append(res.Tablas, *t)
		}
	} else {
		res.Modo = "snapshots"
		nombres := map[string]bool{}
		for t := range antes {
			nombres[t] = true
		}
		for t := range despues {
			nombres[t] = true
		}
		orden := make([]string, 0, len(nombres))
		for t := range nombres {
			orden = append(orden, t)
		}
		sort.Strings(orden)
		for _, t := range orden {
			tipo := despues[t].tipo
			if tipo == "" {
				tipo = antes[t].tipo
			}
			if td := tablaDiff(t, tipo, antes[t].ddl, despues[t].ddl); td != nil {
				res.Tablas = append(res.Tablas, *td)
			}
		}
	}

	if desde != nil {
		if b, err := json.Marshal(res); err == nil {
			_, _ = s.DB.ExecContext(ctx, `
				INSERT INTO cache_diff (commit_origen, commit_destino, diff_calculado, expira_en)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (commit_origen, commit_destino)
				DO UPDATE SET diff_calculado = EXCLUDED.diff_calculado, creado_en = now(), expira_en = EXCLUDED.expira_en`,
				*desde, hasta, string(b), time.Now().Add(s.DiffTTL))
		}
	}
	return res, nil
}

func tablaDiff(nombre, tipo, a, b string) *TablaDiff {
	if strings.TrimSpace(a) == strings.TrimSpace(b) {
		return nil
	}
	ls := diff.Lineas(a, b)
	add, del := diff.Contar(ls)
	estado := "modificada"
	if a == "" {
		estado = "agregada"
	} else if b == "" {
		estado = "eliminada"
	}
	return &TablaDiff{Tabla: nombre, Tipo: tipo, Estado: estado, Agregadas: add, Eliminadas: del, Lineas: ls}
}

// DiffDeCommit compara un commit con su padre (lo que cambió en ese commit).
func (s *Store) DiffDeCommit(ctx context.Context, repoID int64, ref string) (ResultadoDiff, error) {
	hash, err := s.resolverHash(ctx, s.DB, repoID, ref)
	if err != nil {
		return ResultadoDiff{}, err
	}
	var padre sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT padre_hash FROM commit WHERE hash_id=$1`, hash).Scan(&padre); err != nil {
		return ResultadoDiff{}, err
	}
	return s.Diff(ctx, repoID, padre.String, hash)
}

// ---------------------------------------------------------------- Esquema actual

type TablaEsquema struct {
	Tabla  string        `json:"tabla"` // nombre del objeto
	Tipo   string        `json:"tipo"`
	DDL    string        `json:"ddl"`
	Commit CommitResumen `json:"commit"` // último commit que tocó la tabla
}

type Esquema struct {
	Ref    string         `json:"ref"`
	Head   *string        `json:"head"`
	Modo   string         `json:"modo"` // "snapshots" o "sql"
	Tablas []TablaEsquema `json:"tablas"`
	SQL    string         `json:"sql,omitempty"` // SQL acumulado si no hay snapshots
}

// EsquemaActual devuelve el estado del esquema en una rama o commit.
func (s *Store) EsquemaActual(ctx context.Context, repoID int64, ref string) (Esquema, error) {
	if ref == "" {
		ref = "main"
	}
	e := Esquema{Ref: ref, Modo: "snapshots", Tablas: []TablaEsquema{}}
	head, err := s.resolverRef(ctx, s.DB, repoID, ref)
	if err != nil || head == nil {
		return e, err
	}
	e.Head = head
	rows, err := s.DB.QueryContext(ctx, `
		WITH RECURSIVE hist AS (
		    SELECT hash_id, padre_hash, 0 AS d FROM commit WHERE hash_id = $1
		    UNION ALL
		    SELECT c.hash_id, c.padre_hash, hist.d + 1
		    FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
		)
		SELECT * FROM (
		    SELECT DISTINCT ON (s.tabla) s.tabla, s.tipo, s.ddl, c.hash_id, c.mensaje, c.fecha, u.nombre
		    FROM hist
		    JOIN snapshot_esquema s ON s.commit_hash_id = hist.hash_id
		    JOIN commit c           ON c.hash_id = hist.hash_id
		    LEFT JOIN usuario u     ON u.usuario_id = c.autor_id
		    ORDER BY s.tabla, hist.d
		) t WHERE t.ddl <> ''
		ORDER BY array_position(ARRAY['tabla', 'tipo', 'funcion', 'procedimiento', 'vista', 'vista_materializada', 'trigger']::varchar[], t.tipo), t.tabla`, *head)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var t TablaEsquema
		if err := rows.Scan(&t.Tabla, &t.Tipo, &t.DDL, &t.Commit.Hash, &t.Commit.Mensaje, &t.Commit.Fecha, &t.Commit.Autor); err != nil {
			return e, err
		}
		t.Commit.HashCorto = corto(t.Commit.Hash)
		e.Tablas = append(e.Tablas, t)
	}
	if err := rows.Err(); err != nil {
		return e, err
	}
	if len(e.Tablas) == 0 {
		e.Modo = "sql"
		e.SQL, err = sqlAcumulado(ctx, s.DB, *head)
	}
	return e, err
}
