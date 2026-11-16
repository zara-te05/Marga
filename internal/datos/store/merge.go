package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zara-te05/Marga/internal/modelos/riesgo"
)

// Fusionar ramas de migraciones.
//
// Las migraciones de una base se aplican en orden, así que la historia tiene que
// quedar lineal. Por eso "fusionar" significa:
//   - si la rama destino no avanzó: fast-forward (se mueve el puntero);
//   - si avanzó: se copian los commits de la rama origen encima del destino
//     (como un rebase). Si alguna versión choca con las del destino, se renumera.
//   - si ambas ramas tocan el mismo objeto (tabla, función, vista, trigger o tipo):
//     conflicto (no se fusiona salvo forzar).

type Conflicto struct {
	Tabla string   `json:"tabla"` // nombre del objeto (de una función, sin argumentos)
	Tipo  string   `json:"tipo"`  // tabla | funcion | vista | trigger | tipo
	Desde []string `json:"desde"` // migraciones de la rama origen que la tocan
	Hacia []string `json:"hacia"` // migraciones del destino que la tocan
}

type Renumeracion struct {
	Antes   string `json:"antes"`
	Despues string `json:"despues"`
}

type ResultadoMerge struct {
	Desde       string          `json:"desde"`
	Hacia       string          `json:"hacia"`
	Tipo        string          `json:"tipo"` // al_dia | fast_forward | copia | conflicto
	Base        *string         `json:"base"`
	Nuevos      []CommitResumen `json:"nuevos"` // en origen y no en destino
	Otros       []CommitResumen `json:"otros"`  // en destino y no en origen
	Conflictos  []Conflicto     `json:"conflictos"`
	Renumeradas []Renumeracion  `json:"renumeradas"`
	Aplicado    bool            `json:"aplicado"`
	Head        *string         `json:"head"`
}

type eslabon struct {
	hash, mensaje, up, down, checksum string
	padre                             sql.NullString
	fecha                             time.Time
	autorID                           sql.NullInt64
	autor                             sql.NullString
}

func (e eslabon) resumen() CommitResumen {
	c := CommitResumen{Hash: e.hash, HashCorto: corto(e.hash), Mensaje: e.mensaje, Fecha: e.fecha}
	if e.autor.Valid {
		c.Autor = &e.autor.String
	}
	return c
}

func primeraLinea(s string) string { return strings.SplitN(s, "\n", 2)[0] }

// cadena devuelve la historia desde head hacia atrás (head primero).
func cadena(ctx context.Context, q Querier, head string) ([]eslabon, error) {
	rows, err := q.QueryContext(ctx, `
		WITH RECURSIVE hist AS (
		    SELECT hash_id, padre_hash, 0 AS d FROM commit WHERE hash_id = $1
		    UNION ALL
		    SELECT c.hash_id, c.padre_hash, hist.d + 1 FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
		)
		SELECT c.hash_id, c.mensaje, c.sql_up, c.sql_down, c.checksum, c.padre_hash, c.fecha, c.autor_id, u.nombre
		FROM hist JOIN commit c ON c.hash_id = hist.hash_id
		LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		ORDER BY hist.d`, head)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eslabon
	for rows.Next() {
		var e eslabon
		if err := rows.Scan(&e.hash, &e.mensaje, &e.up, &e.down, &e.checksum, &e.padre, &e.fecha, &e.autorID, &e.autor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// exclusivos: commits de `a` (head primero) que no están en `b`, en orden cronológico,
// y el primer ancestro común.
func exclusivos(a, b []eslabon) ([]eslabon, *string) {
	enB := map[string]bool{}
	for _, e := range b {
		enB[e.hash] = true
	}
	var out []eslabon
	var base *string
	for _, e := range a {
		if enB[e.hash] {
			h := e.hash
			base = &h
			break
		}
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, base
}

var reVersionMsg = regexp.MustCompile(`^(\d{4,})_(.+)$`)

// claveConflicto lleva un objeto de snapshot_esquema a la forma de riesgo.ObjetosAfectados
// ("funcion:calcular_total", "vista:v_saldos"…): así se comparan las dos fuentes.
func claveConflicto(tipo, nombre string) string {
	nombre = strings.TrimPrefix(strings.ToLower(nombre), "public.")
	switch tipo {
	case "funcion", "procedimiento":
		tipo = "funcion"
		if i := strings.Index(nombre, "("); i >= 0 {
			nombre = nombre[:i]
		}
	case "vista_materializada":
		tipo = "vista"
	case "trigger":
		if t, tabla, ok := strings.Cut(nombre, " on "); ok {
			nombre = t + " ON " + tabla
		}
	case "":
		tipo = "tabla"
	}
	return tipo + ":" + nombre
}

// objetosDe: los objetos que toca un commit, según su SQL y sus snapshots.
func (s *Store) objetosDe(ctx context.Context, q Querier, e eslabon) (map[string]bool, error) {
	set := map[string]bool{}
	for _, o := range riesgo.ObjetosAfectados(e.up) {
		set[o] = true
	}
	rows, err := q.QueryContext(ctx, `SELECT tipo, tabla FROM snapshot_esquema WHERE commit_hash_id = $1`, e.hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tipo, t string
		if err := rows.Scan(&tipo, &t); err != nil {
			return nil, err
		}
		set[claveConflicto(tipo, t)] = true
	}
	return set, rows.Err()
}

func (s *Store) Fusionar(ctx context.Context, u Usuario, repoID int64, desde, hacia string, aplicar, forzar bool) (ResultadoMerge, error) {
	if hacia == "" {
		hacia = "main"
	}
	res := ResultadoMerge{Desde: desde, Hacia: hacia, Nuevos: []CommitResumen{}, Otros: []CommitResumen{},
		Conflictos: []Conflicto{}, Renumeradas: []Renumeracion{}}
	if desde == hacia {
		return res, errf(ErrInvalido, "elige dos ramas distintas")
	}

	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var idHacia int64
		var headH, headD sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT branch_id, ultimo_commit_hash FROM branch WHERE repositorio_id = $1 AND nombre = $2 FOR UPDATE`,
			repoID, hacia).Scan(&idHacia, &headH)
		if errors.Is(err, sql.ErrNoRows) {
			return errf(ErrNoEncontrado, "la rama '"+hacia+"' no existe")
		}
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT ultimo_commit_hash FROM branch WHERE repositorio_id = $1 AND nombre = $2`, repoID, desde).Scan(&headD)
		if errors.Is(err, sql.ErrNoRows) {
			return errf(ErrNoEncontrado, "la rama '"+desde+"' no existe")
		}
		if err != nil {
			return err
		}
		if !headD.Valid {
			res.Tipo = "al_dia"
			return nil
		}
		var cadH, cadD []eslabon
		if cadD, err = cadena(ctx, tx, headD.String); err != nil {
			return err
		}
		if headH.Valid {
			if cadH, err = cadena(ctx, tx, headH.String); err != nil {
				return err
			}
		}
		nuevos, base := exclusivos(cadD, cadH)
		otros, _ := exclusivos(cadH, cadD)
		// Una migración cuyo SQL ya está en la otra rama (p. ej. de una fusión anterior)
		// cuenta como ya fusionada: la identidad es el checksum, no el hash.
		nuevos = sinRepetidas(nuevos, cadH)
		otros = sinRepetidas(otros, cadD)
		res.Base = base
		for _, e := range nuevos {
			res.Nuevos = append(res.Nuevos, e.resumen())
		}
		for _, e := range otros {
			res.Otros = append(res.Otros, e.resumen())
		}

		switch {
		case len(nuevos) == 0:
			res.Tipo = "al_dia"
			return nil
		case len(otros) == 0:
			res.Tipo = "fast_forward"
			if aplicar {
				if _, err := tx.ExecContext(ctx, `UPDATE branch SET ultimo_commit_hash = $1 WHERE branch_id = $2`, headD.String, idHacia); err != nil {
					return err
				}
				res.Aplicado, res.Head = true, &headD.String
				return Auditar(ctx, tx, u.ID, &repoID, "branch.fusionar", map[string]any{"desde": desde, "hacia": hacia, "tipo": res.Tipo})
			}
			return nil
		}

		// --- Conflictos: objetos (tablas, funciones, vistas…) tocados por ambas ramas desde que se separaron
		porTablaD, porTablaH := map[string][]string{}, map[string][]string{}
		for _, grupo := range []struct {
			cs  []eslabon
			dst map[string][]string
		}{{nuevos, porTablaD}, {otros, porTablaH}} {
			for _, e := range grupo.cs {
				ts, err := s.objetosDe(ctx, tx, e)
				if err != nil {
					return err
				}
				for t := range ts {
					grupo.dst[t] = append(grupo.dst[t], primeraLinea(e.mensaje))
				}
			}
		}
		for t, ds := range porTablaD {
			if hs, ok := porTablaH[t]; ok {
				tipo, nombre, _ := strings.Cut(t, ":")
				res.Conflictos = append(res.Conflictos, Conflicto{Tabla: nombre, Tipo: tipo, Desde: ds, Hacia: hs})
			}
		}
		sort.Slice(res.Conflictos, func(i, j int) bool {
			if res.Conflictos[i].Tipo != res.Conflictos[j].Tipo {
				return res.Conflictos[i].Tipo < res.Conflictos[j].Tipo
			}
			return res.Conflictos[i].Tabla < res.Conflictos[j].Tabla
		})
		if len(res.Conflictos) > 0 && !forzar {
			res.Tipo = "conflicto"
			return nil
		}

		// --- Copia encima del destino, renumerando versiones que choquen
		res.Tipo = "copia"
		maxV := 0
		for _, e := range cadH {
			if m := reVersionMsg.FindStringSubmatch(primeraLinea(e.mensaje)); m != nil {
				n, _ := strconv.Atoi(m[1])
				maxV = max(maxV, n)
			}
		}
		mensajes := make([]string, len(nuevos))
		for i, e := range nuevos {
			mensajes[i] = e.mensaje
			m := reVersionMsg.FindStringSubmatch(primeraLinea(e.mensaje))
			if m == nil {
				continue
			}
			n, _ := strconv.Atoi(m[1])
			if n <= maxV {
				maxV++
				nueva := fmt.Sprintf("%0*d_%s", len(m[1]), maxV, m[2])
				res.Renumeradas = append(res.Renumeradas, Renumeracion{Antes: primeraLinea(e.mensaje), Despues: nueva})
				mensajes[i] = nueva + strings.TrimPrefix(e.mensaje, primeraLinea(e.mensaje))
			} else {
				maxV = n
			}
		}
		if !aplicar {
			return nil
		}

		padre := headH.String
		ahora := time.Now().UTC().Truncate(time.Microsecond)
		for i, e := range nuevos {
			fecha := ahora.Add(time.Duration(i) * time.Microsecond)
			autor := u.ID
			if e.autorID.Valid {
				autor = e.autorID.Int64
			}
			hash := HashCommit(padre, repoID, autor, fecha, mensajes[i], e.up, e.down)
			var mergePadre any
			if i == len(nuevos)-1 {
				mergePadre = headD.String // el último commit recuerda de qué rama vino
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO commit (hash_id, repositorio_id, mensaje, fecha, autor_id, branch_id, padre_hash, padre_merge_hash, sql_up, sql_down, checksum)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				hash, repoID, mensajes[i], fecha, autor, idHacia, padre, mergePadre, e.up, e.down, e.checksum); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO snapshot_esquema (commit_hash_id, tabla, tipo, ddl, diff)
				SELECT $1, tabla, tipo, ddl, diff FROM snapshot_esquema WHERE commit_hash_id = $2`, hash, e.hash); err != nil {
				return err
			}
			padre = hash
		}
		if _, err := tx.ExecContext(ctx, `UPDATE branch SET ultimo_commit_hash = $1 WHERE branch_id = $2`, padre, idHacia); err != nil {
			return err
		}
		res.Aplicado, res.Head = true, &padre
		return Auditar(ctx, tx, u.ID, &repoID, "branch.fusionar", map[string]any{
			"desde": desde, "hacia": hacia, "tipo": res.Tipo, "commits": len(nuevos),
			"renumeradas": len(res.Renumeradas), "conflictos_forzados": len(res.Conflictos)})
	})
	return res, err
}

func sinRepetidas(cs, en []eslabon) []eslabon {
	ya := map[string]bool{}
	for _, e := range en {
		ya[e.checksum] = true
	}
	out := cs[:0:0]
	for _, e := range cs {
		if !ya[e.checksum] {
			out = append(out, e)
		}
	}
	return out
}
