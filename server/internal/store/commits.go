package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zarat/Marga-Sun/server/internal/diff"
)

func corto(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

var reHex = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// resolverHash acepta un prefijo de al menos 4 caracteres (como `git show 8f3b`).
func (s *Store) resolverHash(ctx context.Context, q Querier, repoID int64, prefijo string) (string, error) {
	if !reHex.MatchString(prefijo) {
		return "", errf(ErrInvalido, "hash inválido")
	}
	rows, err := q.QueryContext(ctx,
		`SELECT hash_id FROM commit WHERE repositorio_id=$1 AND hash_id LIKE $2 || '%' LIMIT 2`,
		repoID, strings.ToLower(prefijo))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var hs []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return "", err
		}
		hs = append(hs, h)
	}
	switch len(hs) {
	case 0:
		return "", errf(ErrNoEncontrado, "commit no encontrado")
	case 1:
		return hs[0], nil
	default:
		return "", errf(ErrConflicto, "prefijo de hash ambiguo; usa más caracteres")
	}
}

// ---------------------------------------------------------------- Tipos

type Snapshot struct {
	Tabla string `json:"tabla"`
	DDL   string `json:"ddl"`
	Diff  string `json:"diff"`
}

type Commit struct {
	Numero         int        `json:"numero,omitempty"` // posición en la historia (1 = primer commit)
	Hash           string     `json:"hash"`
	HashCorto      string     `json:"hash_corto"`
	Mensaje        string     `json:"mensaje"`
	Fecha          time.Time  `json:"fecha"`
	AutorID        *int64     `json:"autor_id"`
	Autor          *string    `json:"autor"`
	Branch         *string    `json:"branch"`
	PadreHash      *string    `json:"padre_hash"`
	PadreMergeHash *string    `json:"padre_merge_hash,omitempty"`
	SQLUp          string     `json:"sql_up,omitempty"`
	SQLDown        string     `json:"sql_down,omitempty"`
	Reversible     bool       `json:"reversible"`
	Tablas         []string   `json:"tablas"`
	Entornos       []string   `json:"entornos"` // entornos donde está aplicado
	Snapshots      []Snapshot `json:"snapshots,omitempty"`
}

// ---------------------------------------------------------------- Historial

type Historial struct {
	Branch  string   `json:"branch"`
	Head    *string  `json:"head"`
	Total   int      `json:"total"`
	Commits []Commit `json:"commits"`
}

// ListarCommits recorre la historia desde la punta de la rama siguiendo
// padre_hash (equivalente a `git log <rama>`).
func (s *Store) ListarCommits(ctx context.Context, repoID int64, ref string, limite, offset int) (Historial, error) {
	if ref == "" {
		ref = "main"
	}
	h := Historial{Branch: ref, Commits: []Commit{}}
	head, err := s.resolverRef(ctx, s.DB, repoID, ref)
	if err != nil || head == nil {
		return h, err
	}
	h.Head = head
	rows, err := s.DB.QueryContext(ctx, `
		WITH RECURSIVE hist AS (
		    SELECT c.hash_id, c.padre_hash, 1 AS prof FROM commit c WHERE c.hash_id = $1
		    UNION ALL
		    SELECT c.hash_id, c.padre_hash, hist.prof + 1
		    FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
		    WHERE hist.prof < 100000
		)
		SELECT count(*) OVER () - hist.prof + 1 AS numero, count(*) OVER () AS total,
		       c.hash_id, c.mensaje, c.fecha, c.autor_id, u.nombre, b.nombre,
		       c.padre_hash, c.padre_merge_hash, (c.sql_down <> '') AS reversible,
		       coalesce((SELECT array_to_string(array_agg(s.tabla ORDER BY s.tabla), ',')
		                 FROM snapshot_esquema s WHERE s.commit_hash_id = c.hash_id), ''),
		       `+selEntornosDeCommit+`
		FROM hist
		JOIN commit c       ON c.hash_id = hist.hash_id
		LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		LEFT JOIN branch b  ON b.branch_id = c.branch_id
		ORDER BY hist.prof
		LIMIT $2 OFFSET $3`, *head, limite, offset)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Commit
		var tablas, entornos string
		if err := rows.Scan(&c.Numero, &h.Total, &c.Hash, &c.Mensaje, &c.Fecha, &c.AutorID, &c.Autor,
			&c.Branch, &c.PadreHash, &c.PadreMergeHash, &c.Reversible, &tablas, &entornos); err != nil {
			return h, err
		}
		c.HashCorto = corto(c.Hash)
		c.Tablas = splitNoVacio(tablas)
		c.Entornos = splitNoVacio(entornos)
		h.Commits = append(h.Commits, c)
	}
	return h, rows.Err()
}

func splitNoVacio(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// ObtenerCommit devuelve el commit completo con su SQL y snapshots.
func (s *Store) ObtenerCommit(ctx context.Context, repoID int64, ref string) (Commit, error) {
	hash, err := s.resolverHash(ctx, s.DB, repoID, ref)
	if err != nil {
		return Commit{}, err
	}
	var c Commit
	var entornos string
	err = s.DB.QueryRowContext(ctx, `
		SELECT c.hash_id, c.mensaje, c.fecha, c.autor_id, u.nombre, b.nombre,
		       c.padre_hash, c.padre_merge_hash, c.sql_up, c.sql_down, `+selEntornosDeCommit+`
		FROM commit c
		LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		LEFT JOIN branch b  ON b.branch_id = c.branch_id
		WHERE c.hash_id = $1`, hash).Scan(&c.Hash, &c.Mensaje, &c.Fecha, &c.AutorID, &c.Autor, &c.Branch,
		&c.PadreHash, &c.PadreMergeHash, &c.SQLUp, &c.SQLDown, &entornos)
	if err != nil {
		return c, err
	}
	c.Entornos = splitNoVacio(entornos)
	c.HashCorto = corto(c.Hash)
	c.Reversible = c.SQLDown != ""
	rows, err := s.DB.QueryContext(ctx,
		`SELECT tabla, ddl, diff FROM snapshot_esquema WHERE commit_hash_id=$1 ORDER BY tabla`, hash)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	c.Snapshots, c.Tablas = []Snapshot{}, []string{}
	for rows.Next() {
		var sn Snapshot
		if err := rows.Scan(&sn.Tabla, &sn.DDL, &sn.Diff); err != nil {
			return c, err
		}
		c.Snapshots = append(c.Snapshots, sn)
		c.Tablas = append(c.Tablas, sn.Tabla)
	}
	return c, rows.Err()
}

// ---------------------------------------------------------------- Crear commit

type NuevoSnapshot struct {
	Tabla     string `json:"tabla"`
	DDL       string `json:"ddl"`       // DDL completo de la tabla tras este commit
	Eliminada bool   `json:"eliminada"` // true si el commit hace DROP de la tabla
}

type NuevoCommit struct {
	Branch        string          `json:"branch"`
	Mensaje       string          `json:"mensaje"`
	SQLUp         string          `json:"sql_up"`
	SQLDown       string          `json:"sql_down"`
	PadreEsperado *string         `json:"padre_esperado"` // evita sobrescribir si la rama avanzó (como push no fast-forward)
	Snapshots     []NuevoSnapshot `json:"snapshots"`
}

// HashCommit = sha256(padre, repo, autor, fecha, mensaje, sql_up, sql_down).
// Cambiar cualquier dato cambia el hash: la historia es verificable.
func HashCommit(padre string, repoID, autorID int64, fecha time.Time, mensaje, up, down string) string {
	h := sha256.New()
	fmt.Fprintf(h, "padre %s\nrepo %d\nautor %d\nfecha %s\n\n%s\n--up--\n%s\n--down--\n%s",
		padre, repoID, autorID, fecha.UTC().Format(time.RFC3339Nano), mensaje, up, down)
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) CrearCommit(ctx context.Context, u Usuario, repoID int64, n NuevoCommit) (Commit, error) {
	n.Mensaje = strings.TrimSpace(n.Mensaje)
	if n.Branch == "" {
		n.Branch = "main"
	}
	if n.Mensaje == "" {
		return Commit{}, errf(ErrInvalido, "el mensaje del commit es obligatorio")
	}
	if strings.TrimSpace(n.SQLUp) == "" {
		return Commit{}, errf(ErrInvalido, "sql_up no puede estar vacío")
	}
	vistos := map[string]bool{}
	for _, sn := range n.Snapshots {
		if strings.TrimSpace(sn.Tabla) == "" || vistos[sn.Tabla] {
			return Commit{}, errf(ErrInvalido, "cada snapshot necesita un nombre de tabla único")
		}
		vistos[sn.Tabla] = true
	}

	var hash string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var branchID int64
		var head sql.NullString
		err := tx.QueryRowContext(ctx, `
			SELECT branch_id, ultimo_commit_hash FROM branch
			WHERE repositorio_id=$1 AND nombre=$2 FOR UPDATE`, repoID, n.Branch).Scan(&branchID, &head)
		if errors.Is(err, sql.ErrNoRows) {
			return errf(ErrNoEncontrado, "la rama '"+n.Branch+"' no existe")
		}
		if err != nil {
			return err
		}
		if n.PadreEsperado != nil && *n.PadreEsperado != head.String {
			return errf(ErrConflicto, "la rama avanzó desde tu última sincronización (head actual: "+corto(head.String)+")")
		}

		fecha := time.Now().UTC().Truncate(time.Microsecond) // precisión de TIMESTAMPTZ
		hash = HashCommit(head.String, repoID, u.ID, fecha, n.Mensaje, n.SQLUp, n.SQLDown)
		var padre any
		if head.Valid {
			padre = head.String
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO commit (hash_id, repositorio_id, mensaje, fecha, autor_id, branch_id, padre_hash, sql_up, sql_down)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			hash, repoID, n.Mensaje, fecha, u.ID, branchID, padre, n.SQLUp, n.SQLDown); err != nil {
			return err
		}

		for _, sn := range n.Snapshots {
			ddl := sn.DDL
			if sn.Eliminada {
				ddl = ""
			}
			previo := ""
			if head.Valid {
				esquema, err := esquemaEn(ctx, tx, head.String)
				if err != nil {
					return err
				}
				previo = esquema[sn.Tabla]
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO snapshot_esquema (commit_hash_id, tabla, ddl, diff) VALUES ($1, $2, $3, $4)`,
				hash, sn.Tabla, ddl, diff.Unificado(previo, ddl)); err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE branch SET ultimo_commit_hash=$1 WHERE branch_id=$2`, hash, branchID); err != nil {
			return err
		}
		// Si algún entorno ya había reportado esta migración aplicada (se aplicó antes
		// de hacer push), se enlaza ahora con el commit por el checksum de su SQL.
		if _, err := tx.ExecContext(ctx, `
			UPDATE migracion_aplicada m SET commit_hash = $1
			FROM entorno_bd e
			WHERE e.entorno_bd_id = m.entorno_bd_id AND e.repositorio_id = $2
			  AND m.commit_hash IS NULL AND m.checksum = $3::text`, hash, repoID, checksumDe(n.SQLUp)); err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, &repoID, "commit.crear", map[string]any{
			"hash": hash, "branch": n.Branch, "mensaje": n.Mensaje, "tablas": len(n.Snapshots)})
	})
	if err != nil {
		return Commit{}, err
	}
	return s.ObtenerCommit(ctx, repoID, hash)
}

// Revertir crea un commit nuevo que deshace otro (sql_up <-> sql_down),
// sin reescribir la historia, igual que `git revert`.
func (s *Store) Revertir(ctx context.Context, u Usuario, repoID int64, ref, branch string) (Commit, error) {
	orig, err := s.ObtenerCommit(ctx, repoID, ref)
	if err != nil {
		return Commit{}, err
	}
	if !orig.Reversible {
		return Commit{}, errf(ErrInvalido, "el commit "+orig.HashCorto+" no tiene sql_down; no se puede revertir")
	}
	if branch == "" {
		branch = "main"
	}
	// Las tablas vuelven al estado del padre del commit revertido.
	var snaps []NuevoSnapshot
	if len(orig.Snapshots) > 0 {
		previo := map[string]string{}
		if orig.PadreHash != nil {
			if previo, err = esquemaEn(ctx, s.DB, *orig.PadreHash); err != nil {
				return Commit{}, err
			}
		}
		for _, sn := range orig.Snapshots {
			ddl := previo[sn.Tabla]
			snaps = append(snaps, NuevoSnapshot{Tabla: sn.Tabla, DDL: ddl, Eliminada: ddl == ""})
		}
	}
	return s.CrearCommit(ctx, u, repoID, NuevoCommit{
		Branch:    branch,
		Mensaje:   "Revert \"" + orig.Mensaje + "\"\n\nRevierte el commit " + orig.Hash,
		SQLUp:     orig.SQLDown,
		SQLDown:   orig.SQLUp,
		Snapshots: snaps,
	})
}

// ---------------------------------------------------------------- Búsqueda

type ResultadoBusqueda struct {
	CommitResumen
	Rango float64 `json:"rango"`
}

func (s *Store) Buscar(ctx context.Context, repoID int64, q string, limite int) ([]ResultadoBusqueda, error) {
	q = strings.TrimSpace(q)
	out := []ResultadoBusqueda{}
	if q == "" {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.hash_id, c.mensaje, c.fecha, u.nombre,
		       CASE WHEN c.hash_id LIKE lower($2) || '%' THEN 10
		            ELSE ts_rank(i.documento, websearch_to_tsquery('spanish', $2)) END AS rango
		FROM indice_busqueda_commit i
		JOIN commit c       ON c.hash_id = i.commit_hash_id
		LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		WHERE i.repositorio_id = $1
		  AND (i.documento @@ websearch_to_tsquery('spanish', $2)
		       OR (length($2) >= 4 AND c.hash_id LIKE lower($2) || '%')
		       OR i.mensaje ILIKE '%' || $2 || '%')
		ORDER BY rango DESC, c.fecha DESC
		LIMIT $3`, repoID, q, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ResultadoBusqueda
		if err := rows.Scan(&r.Hash, &r.Mensaje, &r.Fecha, &r.Autor, &r.Rango); err != nil {
			return nil, err
		}
		r.HashCorto = corto(r.Hash)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ParseLimite lee ?limit= con valores por defecto y tope.
func ParseLimite(s string, def, max int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

const selEntornosDeCommit = `coalesce((SELECT array_to_string(array_agg(e.nombre ORDER BY e.compartido DESC, e.nombre), ',')
		        FROM migracion_aplicada m JOIN entorno_bd e ON e.entorno_bd_id = m.entorno_bd_id
		        WHERE m.commit_hash = c.hash_id), '')`

// checksumDe replica proyecto.Checksum de la CLI (sha256 del SQL normalizado).
func checksumDe(sqlText string) string {
	sqlText = strings.TrimRight(strings.ReplaceAll(sqlText, "\r\n", "\n"), " \t\n") + "\n"
	sum := sha256.Sum256([]byte(sqlText))
	return hex.EncodeToString(sum[:])
}
