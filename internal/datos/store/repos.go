package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------- Permisos

var nivel = map[string]int{"": 0, "lectura": 1, "escritura": 2, "admin": 3}

// Alcanza indica si el permiso p cubre el mínimo requerido.
func Alcanza(p, minimo string) bool { return nivel[p] >= nivel[minimo] }

// Permiso calcula el acceso del usuario al repositorio:
// propietario o admin global => admin; colaborador => su permiso;
// repositorio público => lectura; resto => "".
func (s *Store) Permiso(ctx context.Context, u Usuario, repoID int64) (string, error) {
	var p string
	err := s.DB.QueryRowContext(ctx, `
		SELECT CASE
		         WHEN $1 = 'admin' OR r.propietario_id = $2 THEN 'admin'
		         WHEN c.permiso IS NOT NULL THEN c.permiso
		         WHEN r.visibilidad = 'publico' THEN 'lectura'
		         ELSE '' END
		FROM repositorio r
		LEFT JOIN colaborador c ON c.repositorio_id = r.repositorio_id AND c.usuario_id = $2
		WHERE r.repositorio_id = $3`, u.Rol, u.ID, repoID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(ErrNoEncontrado, "repositorio no encontrado")
	}
	return p, err
}

// ---------------------------------------------------------------- Repositorios

type CommitResumen struct {
	Hash      string    `json:"hash"`
	HashCorto string    `json:"hash_corto"`
	Mensaje   string    `json:"mensaje"`
	Fecha     time.Time `json:"fecha"`
	Autor     *string   `json:"autor"`
}

type Repositorio struct {
	ID            int64          `json:"id"`
	Nombre        string         `json:"nombre"`
	Slug          string         `json:"slug"`
	Descripcion   string         `json:"descripcion"`
	Visibilidad   string         `json:"visibilidad"`
	Motor         string         `json:"motor"`
	Entorno       string         `json:"entorno"`
	CreadoEn      time.Time      `json:"creado_en"`
	PropietarioID int64          `json:"propietario_id"`
	Propietario   string         `json:"propietario"`
	Permiso       string         `json:"permiso"`
	TotalCommits  int            `json:"total_commits"`
	TotalBranches int            `json:"total_branches"`
	UltimoCommit  *CommitResumen `json:"ultimo_commit"`
}

const selRepo = `
	SELECT r.repositorio_id, r.nombre, r.slug, r.descripcion, r.visibilidad, r.motor, r.entorno,
	       r.creado_en, r.propietario_id, pu.nombre,
	       CASE WHEN $2 = 'admin' OR r.propietario_id = $1 THEN 'admin'
	            WHEN col.permiso IS NOT NULL THEN col.permiso
	            WHEN r.visibilidad = 'publico' THEN 'lectura' ELSE '' END,
	       (SELECT count(*) FROM commit c WHERE c.repositorio_id = r.repositorio_id),
	       (SELECT count(*) FROM branch b WHERE b.repositorio_id = r.repositorio_id),
	       lc.hash_id, lc.mensaje, lc.fecha, la.nombre
	FROM repositorio r
	JOIN usuario pu ON pu.usuario_id = r.propietario_id
	LEFT JOIN colaborador col ON col.repositorio_id = r.repositorio_id AND col.usuario_id = $1
	LEFT JOIN LATERAL (
	    SELECT c.hash_id, c.mensaje, c.fecha, c.autor_id FROM commit c
	    WHERE c.repositorio_id = r.repositorio_id ORDER BY c.fecha DESC LIMIT 1) lc ON true
	LEFT JOIN usuario la ON la.usuario_id = lc.autor_id `

func scanRepo(r interface{ Scan(...any) error }) (Repositorio, error) {
	var x Repositorio
	var h, msg, autor sql.NullString
	var f sql.NullTime
	err := r.Scan(&x.ID, &x.Nombre, &x.Slug, &x.Descripcion, &x.Visibilidad, &x.Motor, &x.Entorno,
		&x.CreadoEn, &x.PropietarioID, &x.Propietario, &x.Permiso, &x.TotalCommits, &x.TotalBranches,
		&h, &msg, &f, &autor)
	if errors.Is(err, sql.ErrNoRows) {
		return x, errf(ErrNoEncontrado, "repositorio no encontrado")
	}
	if err == nil && h.Valid {
		x.UltimoCommit = &CommitResumen{Hash: h.String, HashCorto: corto(h.String), Mensaje: msg.String, Fecha: f.Time}
		if autor.Valid {
			x.UltimoCommit.Autor = &autor.String
		}
	}
	return x, err
}

func (s *Store) ListarRepos(ctx context.Context, u Usuario) ([]Repositorio, error) {
	rows, err := s.DB.QueryContext(ctx, selRepo+`
		WHERE $2 = 'admin' OR r.propietario_id = $1 OR col.usuario_id IS NOT NULL OR r.visibilidad = 'publico'
		ORDER BY coalesce(lc.fecha, r.creado_en) DESC`, u.ID, u.Rol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Repositorio{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ObtenerRepo(ctx context.Context, u Usuario, id int64) (Repositorio, error) {
	return scanRepo(s.DB.QueryRowContext(ctx, selRepo+`WHERE r.repositorio_id = $3`, u.ID, u.Rol, id))
}

type DatosRepo struct {
	Nombre      *string `json:"nombre"`
	Slug        *string `json:"slug"`
	Descripcion *string `json:"descripcion"`
	Visibilidad *string `json:"visibilidad"`
	Motor       *string `json:"motor"`
	Entorno     *string `json:"entorno"`
}

func val(p *string, def string) string {
	if p == nil {
		return def
	}
	return strings.TrimSpace(*p)
}

var (
	reNoSlug = regexp.MustCompile(`[^a-z0-9]+`)
	vis      = map[string]bool{"publico": true, "privado": true}
	motores  = map[string]bool{"postgresql": true, "mysql": true, "sqlserver": true, "mongodb": true, "sqlite": true}
	entornos = map[string]bool{"Producción": true, "Staging": true, "Desarrollo": true, "Sandbox": true}
)

var sinAcentos = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "ç", "c")

// Slugify: "Núcleo Pagos" -> "nucleo-pagos".
func Slugify(s string) string {
	t := sinAcentos.Replace(strings.ToLower(s))
	return strings.Trim(reNoSlug.ReplaceAllString(t, "-"), "-")
}

func validarRepo(d DatosRepo, nombre, visib, motor, entorno string) error {
	switch {
	case nombre == "":
		return errf(ErrInvalido, "el nombre del proyecto es obligatorio")
	case len(nombre) > 120:
		return errf(ErrInvalido, "el nombre no puede superar 120 caracteres")
	case !vis[visib]:
		return errf(ErrInvalido, "visibilidad debe ser 'publico' o 'privado'")
	case !motores[motor]:
		return errf(ErrInvalido, "motor no soportado")
	case !entornos[entorno]:
		return errf(ErrInvalido, "entorno debe ser Producción, Staging, Desarrollo o Sandbox")
	}
	return nil
}

// CrearRepo crea el repositorio y su rama "main" vacía.
func (s *Store) CrearRepo(ctx context.Context, u Usuario, d DatosRepo) (Repositorio, error) {
	nombre := val(d.Nombre, "")
	slug := Slugify(val(d.Slug, nombre))
	visib, motor, entorno := val(d.Visibilidad, "privado"), val(d.Motor, "postgresql"), val(d.Entorno, "Desarrollo")
	if err := validarRepo(d, nombre, visib, motor, entorno); err != nil {
		return Repositorio{}, err
	}
	if slug == "" {
		return Repositorio{}, errf(ErrInvalido, "el slug no puede quedar vacío")
	}
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO repositorio (nombre, slug, descripcion, visibilidad, motor, entorno, propietario_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING repositorio_id`,
			nombre, slug, val(d.Descripcion, ""), visib, motor, entorno, u.ID).Scan(&id)
		if esUnico(err) {
			return errf(ErrConflicto, "ya tienes un proyecto con el slug '"+slug+"'")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO branch (nombre, repositorio_id) VALUES ('main', $1)`, id); err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, &id, "repositorio.crear", map[string]any{"nombre": nombre, "slug": slug})
	})
	if err != nil {
		return Repositorio{}, err
	}
	return s.ObtenerRepo(ctx, u, id)
}

func (s *Store) ActualizarRepo(ctx context.Context, u Usuario, id int64, d DatosRepo) (Repositorio, error) {
	act, err := s.ObtenerRepo(ctx, u, id)
	if err != nil {
		return act, err
	}
	nombre, visib := val(d.Nombre, act.Nombre), val(d.Visibilidad, act.Visibilidad)
	motor, entorno := val(d.Motor, act.Motor), val(d.Entorno, act.Entorno)
	desc := val(d.Descripcion, act.Descripcion)
	if err := validarRepo(d, nombre, visib, motor, entorno); err != nil {
		return act, err
	}
	// El slug es inmutable (lo usan la CLI y los logs).
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE repositorio SET nombre=$1, descripcion=$2, visibilidad=$3, motor=$4, entorno=$5
			WHERE repositorio_id=$6`, nombre, desc, visib, motor, entorno, id); err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, &id, "repositorio.actualizar", map[string]any{
			"nombre": nombre, "visibilidad": visib, "motor": motor, "entorno": entorno})
	})
	if err != nil {
		return act, err
	}
	return s.ObtenerRepo(ctx, u, id)
}

func (s *Store) EliminarRepo(ctx context.Context, u Usuario, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var nombre string
		if err := tx.QueryRowContext(ctx, `DELETE FROM repositorio WHERE repositorio_id=$1 RETURNING nombre`, id).Scan(&nombre); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errf(ErrNoEncontrado, "repositorio no encontrado")
			}
			return err
		}
		// repositorio_id queda NULL en auditoría (ON DELETE SET NULL); guardamos el nombre.
		return Auditar(ctx, tx, u.ID, nil, "repositorio.eliminar", map[string]any{"id": id, "nombre": nombre})
	})
}

// ---------------------------------------------------------------- Colaboradores

type Colaborador struct {
	UsuarioID int64  `json:"usuario_id"`
	Nombre    string `json:"nombre"`
	Email     string `json:"email"`
	Permiso   string `json:"permiso"`
}

func (s *Store) ListarColaboradores(ctx context.Context, repoID int64) ([]Colaborador, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.usuario_id, u.nombre, u.email, c.permiso
		FROM colaborador c JOIN usuario u ON u.usuario_id = c.usuario_id
		WHERE c.repositorio_id = $1 ORDER BY u.nombre`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Colaborador{}
	for rows.Next() {
		var c Colaborador
		if err := rows.Scan(&c.UsuarioID, &c.Nombre, &c.Email, &c.Permiso); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AgregarColaborador añade o actualiza el permiso de un usuario (por email).
func (s *Store) AgregarColaborador(ctx context.Context, u Usuario, repoID int64, email, permiso string) (Colaborador, error) {
	if _, ok := nivel[permiso]; !ok || permiso == "" {
		return Colaborador{}, errf(ErrInvalido, "permiso debe ser lectura, escritura o admin")
	}
	invitado, err := s.UsuarioPorEmail(ctx, email)
	if err != nil {
		return Colaborador{}, errf(ErrNoEncontrado, "no existe un usuario con ese email")
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO colaborador (usuario_id, repositorio_id, permiso) VALUES ($1, $2, $3)
			ON CONFLICT (usuario_id, repositorio_id) DO UPDATE SET permiso = EXCLUDED.permiso`,
			invitado.ID, repoID, permiso); err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, &repoID, "colaborador.agregar", map[string]any{"email": invitado.Email, "permiso": permiso})
	})
	return Colaborador{invitado.ID, invitado.Nombre, invitado.Email, permiso}, err
}

func (s *Store) QuitarColaborador(ctx context.Context, u Usuario, repoID, usuarioID int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM colaborador WHERE repositorio_id=$1 AND usuario_id=$2`, repoID, usuarioID)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return errf(ErrNoEncontrado, "ese usuario no es colaborador")
		}
		return Auditar(ctx, tx, u.ID, &repoID, "colaborador.quitar", map[string]any{"usuario_id": usuarioID})
	})
}

// ---------------------------------------------------------------- Branches

type Branch struct {
	ID           int64          `json:"id"`
	Nombre       string         `json:"nombre"`
	CreadoEn     time.Time      `json:"creado_en"`
	UltimoCommit *CommitResumen `json:"ultimo_commit"`
}

func (s *Store) ListarBranches(ctx context.Context, repoID int64) ([]Branch, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT b.branch_id, b.nombre, b.creado_en, c.hash_id, c.mensaje, c.fecha, u.nombre
		FROM branch b
		LEFT JOIN commit c  ON c.hash_id = b.ultimo_commit_hash
		LEFT JOIN usuario u ON u.usuario_id = c.autor_id
		WHERE b.repositorio_id = $1
		ORDER BY (b.nombre = 'main') DESC, c.fecha DESC NULLS LAST, b.nombre`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Branch{}
	for rows.Next() {
		var b Branch
		var h, m, a sql.NullString
		var f sql.NullTime
		if err := rows.Scan(&b.ID, &b.Nombre, &b.CreadoEn, &h, &m, &f, &a); err != nil {
			return nil, err
		}
		if h.Valid {
			b.UltimoCommit = &CommitResumen{Hash: h.String, HashCorto: corto(h.String), Mensaje: m.String, Fecha: f.Time}
			if a.Valid {
				b.UltimoCommit.Autor = &a.String
			}
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

var reBranch = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,120}$`)

// CrearBranch crea una rama que apunta al mismo commit que `desde`
// (nombre de rama o hash). Si `desde` está vacío usa main.
func (s *Store) CrearBranch(ctx context.Context, u Usuario, repoID int64, nombre, desde string) (Branch, error) {
	if !reBranch.MatchString(nombre) || strings.Contains(nombre, "..") {
		return Branch{}, errf(ErrInvalido, "nombre de rama inválido (usa letras, números, . _ / -)")
	}
	if desde == "" {
		desde = "main"
	}
	head, err := s.resolverRef(ctx, s.DB, repoID, desde)
	if err != nil {
		return Branch{}, err
	}
	var b Branch
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO branch (nombre, repositorio_id, ultimo_commit_hash) VALUES ($1, $2, $3)
			RETURNING branch_id, nombre, creado_en`, nombre, repoID, head).Scan(&b.ID, &b.Nombre, &b.CreadoEn)
		if esUnico(err) {
			return errf(ErrConflicto, "la rama '"+nombre+"' ya existe")
		}
		if err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, &repoID, "branch.crear", map[string]any{"nombre": nombre, "desde": desde})
	})
	return b, err
}

func (s *Store) EliminarBranch(ctx context.Context, u Usuario, repoID int64, nombre string) error {
	if nombre == "main" {
		return errf(ErrInvalido, "no se puede eliminar la rama main")
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM branch WHERE repositorio_id=$1 AND nombre=$2`, repoID, nombre)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return errf(ErrNoEncontrado, "rama no encontrada")
		}
		return Auditar(ctx, tx, u.ID, &repoID, "branch.eliminar", map[string]any{"nombre": nombre})
	})
}

// resolverRef convierte un nombre de rama o prefijo de hash en un hash completo.
// Devuelve nil si es una rama sin commits todavía.
func (s *Store) resolverRef(ctx context.Context, q Querier, repoID int64, ref string) (*string, error) {
	ref = strings.TrimSpace(ref)
	var head sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT ultimo_commit_hash FROM branch WHERE repositorio_id=$1 AND nombre=$2`, repoID, ref).Scan(&head)
	if err == nil {
		if head.Valid {
			return &head.String, nil
		}
		return nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	h, err := s.resolverHash(ctx, q, repoID, ref)
	if err != nil {
		return nil, err
	}
	return &h, nil
}
