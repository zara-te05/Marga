package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------- Tipos

type MigracionAplicada struct {
	Version     string    `json:"version"`
	Nombre      string    `json:"nombre"`
	Checksum    string    `json:"checksum"`
	Commit      *string   `json:"commit"` // hash en el servidor; nil si no se ha enviado
	AplicadaEn  time.Time `json:"aplicada_en"`
	DuracionMs  int       `json:"duracion_ms"`
	AplicadaPor string    `json:"aplicada_por"`
}

// ReporteEntorno es lo que envía la CLI tras status / up / down.
type ReporteEntorno struct {
	Motor      string              `json:"motor"`
	Compartido bool                `json:"compartido"`
	Rama       string              `json:"rama"` // rama de git de la base ("" si no sigue ramas)
	Aplicadas  []MigracionAplicada `json:"aplicadas"`
	Esquema    json.RawMessage     `json:"esquema"`
	Drift      []string            `json:"drift"`
}

type EntornoResumen struct {
	ID                 int64              `json:"id"`
	Nombre             string             `json:"nombre"`
	Motor              string             `json:"motor"`
	Compartido         bool               `json:"compartido"`
	RamaGit            *string            `json:"rama_git"`       // rama de git de la base (entornos personales)
	RamaComparada      string             `json:"rama_comparada"` // rama del servidor contra la que se cuentan las pendientes
	ReportadoEn        time.Time          `json:"reportado_en"`
	ReportadoPor       *string            `json:"reportado_por"`
	RequiereAprobacion bool               `json:"requiere_aprobacion"`
	Aplicadas          int                `json:"aplicadas"`
	SinEnviar          int                `json:"sin_enviar"` // aplicadas que no existen en el servidor
	Pendientes         int                `json:"pendientes"` // commits de la rama que faltan aplicar
	Drift              []string           `json:"drift"`
	Ultima             *MigracionAplicada `json:"ultima"`
}

type EntornoDetalle struct {
	EntornoResumen
	Rama            string              `json:"rama"`
	ListaAplicadas  []MigracionAplicada `json:"lista_aplicadas"`
	ListaPendientes []CommitResumen     `json:"lista_pendientes"`
	Esquema         json.RawMessage     `json:"esquema"`
}

var reNombreEntorno = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,79}$`)

// ---------------------------------------------------------------- Reportar

// ReportarEntorno reemplaza el estado conocido de un entorno con lo que vio la CLI.
func (s *Store) ReportarEntorno(ctx context.Context, u Usuario, repoID int64, nombre string, r ReporteEntorno) (EntornoResumen, error) {
	nombre = strings.TrimSpace(nombre)
	if !reNombreEntorno.MatchString(nombre) {
		return EntornoResumen{}, errf(ErrInvalido, "nombre de entorno inválido")
	}
	if r.Motor == "" {
		r.Motor = "postgresql"
	}
	r.Rama = strings.TrimSpace(r.Rama)
	if len(r.Rama) > 255 || strings.ContainsAny(r.Rama, "\x00\n\r") {
		return EntornoResumen{}, errf(ErrInvalido, "rama inválida")
	}
	var ramaGit any
	if r.Rama != "" && !r.Compartido {
		ramaGit = r.Rama
	}
	if len(r.Aplicadas) > 10000 || len(r.Drift) > 1000 {
		return EntornoResumen{}, errf(ErrInvalido, "reporte demasiado grande")
	}
	if r.Drift == nil {
		r.Drift = []string{}
	}
	drift, _ := json.Marshal(r.Drift)
	var esquema any
	if len(r.Esquema) > 0 && string(r.Esquema) != "null" {
		if !json.Valid(r.Esquema) {
			return EntornoResumen{}, errf(ErrInvalido, "esquema no es JSON válido")
		}
		esquema = string(r.Esquema)
	}

	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var id int64
		var antes sql.NullString
		err := tx.QueryRowContext(ctx, `
			SELECT e.entorno_bd_id, (SELECT string_agg(version || ':' || checksum, ',' ORDER BY version)
			                         FROM migracion_aplicada m WHERE m.entorno_bd_id = e.entorno_bd_id)
			FROM entorno_bd e WHERE e.repositorio_id = $1 AND e.nombre = $2`, repoID, nombre).Scan(&id, &antes)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO entorno_bd (repositorio_id, nombre, motor, compartido, esquema, drift, reportado_por, rama_git)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (repositorio_id, nombre) DO UPDATE SET
			    motor = EXCLUDED.motor, compartido = EXCLUDED.compartido, esquema = EXCLUDED.esquema,
			    drift = EXCLUDED.drift, reportado_en = now(), reportado_por = EXCLUDED.reportado_por,
			    rama_git = EXCLUDED.rama_git
			RETURNING entorno_bd_id`,
			repoID, nombre, r.Motor, r.Compartido, esquema, string(drift), u.ID, ramaGit).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM migracion_aplicada WHERE entorno_bd_id = $1`, id); err != nil {
			return err
		}
		var ahora []string
		for _, a := range r.Aplicadas {
			// El commit se toma del que dice la CLI (si es de este repo) o se busca por checksum del SQL.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO migracion_aplicada (entorno_bd_id, version, nombre, checksum, commit_hash, aplicada_en, duracion_ms, aplicada_por)
				VALUES ($1, $2, $3, $4::text,
				    COALESCE(
				        (SELECT c.hash_id FROM commit c WHERE c.hash_id = $5::text AND c.repositorio_id = $9),
				        (SELECT c.hash_id FROM commit c WHERE c.repositorio_id = $9 AND c.checksum = $4::text
				         ORDER BY c.fecha LIMIT 1)),
				    $6, $7, $8)
				ON CONFLICT (entorno_bd_id, version) DO NOTHING`,
				id, a.Version, a.Nombre, a.Checksum, a.Commit, a.AplicadaEn, a.DuracionMs, a.AplicadaPor, repoID); err != nil {
				return err
			}
			ahora = append(ahora, a.Version+":"+a.Checksum)
		}
		// Solo se audita si cambió lo aplicado (un status repetido no llena la bitácora).
		if strings.Join(ahora, ",") != antes.String {
			return Auditar(ctx, tx, u.ID, &repoID, "entorno.reportar", map[string]any{
				"entorno": nombre, "rama": r.Rama, "aplicadas": len(r.Aplicadas), "drift": len(r.Drift)})
		}
		return nil
	})
	if err != nil {
		return EntornoResumen{}, err
	}
	d, err := s.ObtenerEntorno(ctx, repoID, nombre, "")
	return d.EntornoResumen, err
}

// ---------------------------------------------------------------- Consultar

// selEntorno cuenta las pendientes de cada entorno contra una rama del servidor:
// $2 si se pidió explícitamente ($3 = true); si no, la rama de git de la base
// (dev@zara en feature/facturas se compara con feature/facturas) o $2 (main).
const selEntorno = `
	SELECT e.entorno_bd_id, e.nombre, e.motor, e.compartido, e.rama_git, rc.rama, e.requiere_aprobacion, e.reportado_en, u.nombre, e.drift,
	       (SELECT count(*) FROM migracion_aplicada m WHERE m.entorno_bd_id = e.entorno_bd_id),
	       (SELECT count(*) FROM migracion_aplicada m WHERE m.entorno_bd_id = e.entorno_bd_id AND m.commit_hash IS NULL),
	       (WITH RECURSIVE hist AS (
	            SELECT c.hash_id, c.padre_hash FROM commit c
	            WHERE c.hash_id = (SELECT b.ultimo_commit_hash FROM branch b WHERE b.repositorio_id = $1 AND b.nombre = rc.rama)
	            UNION ALL
	            SELECT c.hash_id, c.padre_hash FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
	        )
	        SELECT count(*) FROM hist JOIN commit c ON c.hash_id = hist.hash_id
	        WHERE c.checksum NOT IN (SELECT m.checksum FROM migracion_aplicada m WHERE m.entorno_bd_id = e.entorno_bd_id)),
	       e.esquema
	FROM entorno_bd e
	LEFT JOIN usuario u ON u.usuario_id = e.reportado_por
	CROSS JOIN LATERAL (SELECT CASE WHEN $3::boolean THEN $2::text ELSE coalesce(
	    (SELECT b.nombre FROM branch b WHERE b.repositorio_id = $1 AND b.nombre = e.rama_git), $2::text) END AS rama) rc
	WHERE e.repositorio_id = $1 `

func (s *Store) scanEntornos(rows *sql.Rows) ([]EntornoDetalle, error) {
	var out []EntornoDetalle
	for rows.Next() {
		var d EntornoDetalle
		var drift, esquema []byte
		if err := rows.Scan(&d.ID, &d.Nombre, &d.Motor, &d.Compartido, &d.RamaGit, &d.RamaComparada, &d.RequiereAprobacion, &d.ReportadoEn, &d.ReportadoPor, &drift,
			&d.Aplicadas, &d.SinEnviar, &d.Pendientes, &esquema); err != nil {
			return nil, err
		}
		d.Drift = []string{}
		_ = json.Unmarshal(drift, &d.Drift)
		if len(esquema) > 0 {
			d.Esquema = esquema
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) aplicadasDe(ctx context.Context, entornoID int64) ([]MigracionAplicada, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT version, nombre, checksum, commit_hash, aplicada_en, duracion_ms, aplicada_por
		FROM migracion_aplicada WHERE entorno_bd_id = $1 ORDER BY aplicada_en, version`, entornoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MigracionAplicada{}
	for rows.Next() {
		var a MigracionAplicada
		if err := rows.Scan(&a.Version, &a.Nombre, &a.Checksum, &a.Commit, &a.AplicadaEn, &a.DuracionMs, &a.AplicadaPor); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListarEntornos resume cada entorno respecto a una rama. Sin rama, cada entorno
// se compara con main, salvo las bases personales de una rama: con su rama.
func (s *Store) ListarEntornos(ctx context.Context, repoID int64, rama string) ([]EntornoResumen, error) {
	explicita := rama != ""
	if !explicita {
		rama = "main"
	}
	rows, err := s.DB.QueryContext(ctx, selEntorno+`ORDER BY e.compartido DESC, e.nombre`, repoID, rama, explicita)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ds, err := s.scanEntornos(rows)
	if err != nil {
		return nil, err
	}
	out := make([]EntornoResumen, 0, len(ds))
	for _, d := range ds {
		if as, err := s.aplicadasDe(ctx, d.ID); err == nil && len(as) > 0 {
			d.Ultima = &as[len(as)-1]
		}
		out = append(out, d.EntornoResumen)
	}
	return out, nil
}

// ObtenerEntorno devuelve el detalle: lo aplicado, lo pendiente y el esquema real.
func (s *Store) ObtenerEntorno(ctx context.Context, repoID int64, nombre, rama string) (EntornoDetalle, error) {
	explicita := rama != ""
	if !explicita {
		rama = "main"
	}
	rows, err := s.DB.QueryContext(ctx, selEntorno+`AND e.nombre = $4`, repoID, rama, explicita, nombre)
	if err != nil {
		return EntornoDetalle{}, err
	}
	ds, err := s.scanEntornos(rows)
	rows.Close()
	if err != nil {
		return EntornoDetalle{}, err
	}
	if len(ds) == 0 {
		return EntornoDetalle{}, errf(ErrNoEncontrado, "entorno no encontrado")
	}
	d := ds[0]
	d.Rama = d.RamaComparada
	rama = d.RamaComparada
	if d.ListaAplicadas, err = s.aplicadasDe(ctx, d.ID); err != nil {
		return d, err
	}
	if n := len(d.ListaAplicadas); n > 0 {
		d.Ultima = &d.ListaAplicadas[n-1]
	}
	prows, err := s.DB.QueryContext(ctx, `
		WITH RECURSIVE hist AS (
		    SELECT c.hash_id, c.padre_hash, 0 AS d FROM commit c
		    WHERE c.hash_id = (SELECT ultimo_commit_hash FROM branch WHERE repositorio_id = $1 AND nombre = $2)
		    UNION ALL
		    SELECT c.hash_id, c.padre_hash, hist.d + 1 FROM commit c JOIN hist ON c.hash_id = hist.padre_hash
		)
		SELECT c.hash_id, c.mensaje, c.fecha, u.nombre
		FROM hist JOIN commit c ON c.hash_id = hist.hash_id
		LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		WHERE c.checksum NOT IN (SELECT m.checksum FROM migracion_aplicada m WHERE m.entorno_bd_id = $3)
		ORDER BY hist.d DESC`, repoID, rama, d.ID)
	if err != nil {
		return d, err
	}
	defer prows.Close()
	d.ListaPendientes = []CommitResumen{}
	for prows.Next() {
		var c CommitResumen
		if err := prows.Scan(&c.Hash, &c.Mensaje, &c.Fecha, &c.Autor); err != nil {
			return d, err
		}
		c.HashCorto = corto(c.Hash)
		d.ListaPendientes = append(d.ListaPendientes, c)
	}
	return d, prows.Err()
}

func (s *Store) EliminarEntorno(ctx context.Context, u Usuario, repoID int64, nombre string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM entorno_bd WHERE repositorio_id = $1 AND nombre = $2`, repoID, nombre)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return errf(ErrNoEncontrado, "entorno no encontrado")
		}
		return Auditar(ctx, tx, u.ID, &repoID, "entorno.eliminar", map[string]any{"entorno": nombre})
	})
}

// ConfigurarEntorno: marcar un entorno como protegido (requiere aprobación).
func (s *Store) ConfigurarEntorno(ctx context.Context, u Usuario, repoID int64, nombre string, requiereAprobacion bool) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE entorno_bd SET requiere_aprobacion = $3 WHERE repositorio_id = $1 AND nombre = $2`,
			repoID, nombre, requiereAprobacion)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return errf(ErrNoEncontrado, "entorno no encontrado")
		}
		return Auditar(ctx, tx, u.ID, &repoID, "entorno.configurar", map[string]any{"entorno": nombre, "requiere_aprobacion": requiereAprobacion})
	})
}
