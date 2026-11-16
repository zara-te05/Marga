// Package store concentra todo el acceso a datos (SQL) de Marga.
// Los handlers HTTP y el seeder usan estas funciones; nunca escriben SQL.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNoEncontrado = errors.New("no encontrado")
	ErrConflicto    = errors.New("conflicto")
	ErrProhibido    = errors.New("permiso insuficiente")
	ErrInvalido     = errors.New("datos inválidos")
)

// Error con mensaje para el usuario que conserva la categoría (errors.Is).
type Error struct {
	Tipo error
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Tipo }

func errf(tipo error, msg string) error { return &Error{tipo, msg} }

// Querier lo cumplen *sql.DB y *sql.Tx.
type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	DB      *sql.DB
	DiffTTL time.Duration
}

func New(db *sql.DB, diffTTL time.Duration) *Store { return &Store{DB: db, DiffTTL: diffTTL} }

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func esUnico(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// ---------------------------------------------------------------- Auditoría

func Auditar(ctx context.Context, q Querier, usuarioID int64, repoID *int64, accion string, params map[string]any) error {
	if params == nil {
		params = map[string]any{}
	}
	b, _ := json.Marshal(params)
	_, err := q.ExecContext(ctx,
		`INSERT INTO auditoria (usuario_id, repositorio_id, accion, parametros) VALUES ($1, $2, $3, $4)`,
		usuarioID, repoID, accion, string(b))
	return err
}

type EventoAuditoria struct {
	ID         int64           `json:"id"`
	Usuario    *string         `json:"usuario"`
	Accion     string          `json:"accion"`
	Parametros json.RawMessage `json:"parametros"`
	Fecha      time.Time       `json:"fecha"`
}

func (s *Store) ListarAuditoria(ctx context.Context, repoID int64, limite int) ([]EventoAuditoria, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.auditoria_id, u.nombre, a.accion, a.parametros, a.fecha
		FROM auditoria a LEFT JOIN usuario u ON u.usuario_id = a.usuario_id
		WHERE a.repositorio_id = $1
		ORDER BY a.fecha DESC LIMIT $2`, repoID, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventoAuditoria{}
	for rows.Next() {
		var e EventoAuditoria
		var p []byte
		if err := rows.Scan(&e.ID, &e.Usuario, &e.Accion, &p, &e.Fecha); err != nil {
			return nil, err
		}
		e.Parametros = p
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Equipos

type Equipo struct {
	ID       int64  `json:"id"`
	Nombre   string `json:"nombre"`
	Miembros int    `json:"miembros"`
}

func (s *Store) ListarEquipos(ctx context.Context) ([]Equipo, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT e.equipo_id, e.nombre, count(u.usuario_id)
		FROM equipo e LEFT JOIN usuario u ON u.equipo_id = e.equipo_id
		GROUP BY e.equipo_id ORDER BY e.nombre`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Equipo{}
	for rows.Next() {
		var e Equipo
		if err := rows.Scan(&e.ID, &e.Nombre, &e.Miembros); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CrearEquipo(ctx context.Context, nombre string) (Equipo, error) {
	nombre = strings.TrimSpace(nombre)
	if nombre == "" {
		return Equipo{}, errf(ErrInvalido, "el nombre del equipo es obligatorio")
	}
	var e Equipo
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO equipo (nombre) VALUES ($1) RETURNING equipo_id, nombre`, nombre).Scan(&e.ID, &e.Nombre)
	if esUnico(err) {
		return e, errf(ErrConflicto, "ya existe un equipo con ese nombre")
	}
	return e, err
}

// ---------------------------------------------------------------- Usuarios

type Usuario struct {
	ID           int64     `json:"id"`
	Nombre       string    `json:"nombre"`
	Email        string    `json:"email"`
	Rol          string    `json:"rol"`
	EquipoID     *int64    `json:"equipo_id"`
	Equipo       *string   `json:"equipo"`
	CreadoEn     time.Time `json:"creado_en"`
	PasswordHash string    `json:"-"`
}

const selUsuario = `
	SELECT u.usuario_id, u.nombre, u.email, u.rol, u.equipo_id, e.nombre, u.creado_en, u.password_hash
	FROM usuario u LEFT JOIN equipo e ON e.equipo_id = u.equipo_id `

func scanUsuario(r interface{ Scan(...any) error }) (Usuario, error) {
	var u Usuario
	err := r.Scan(&u.ID, &u.Nombre, &u.Email, &u.Rol, &u.EquipoID, &u.Equipo, &u.CreadoEn, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNoEncontrado
	}
	return u, err
}

// CrearUsuario: el primer usuario registrado se vuelve admin.
func (s *Store) CrearUsuario(ctx context.Context, nombre, email, passwordHash string, equipoID *int64) (Usuario, error) {
	nombre, email = strings.TrimSpace(nombre), strings.ToLower(strings.TrimSpace(email))
	if nombre == "" || !strings.Contains(email, "@") {
		return Usuario{}, errf(ErrInvalido, "nombre y email válidos son obligatorios")
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO usuario (nombre, email, password_hash, rol, equipo_id)
		VALUES ($1, $2, $3,
		        CASE WHEN EXISTS (SELECT 1 FROM usuario) THEN 'desarrollador' ELSE 'admin' END,
		        $4)
		RETURNING usuario_id`, nombre, email, passwordHash, equipoID).Scan(&id)
	if esUnico(err) {
		return Usuario{}, errf(ErrConflicto, "ese email ya está registrado")
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23503" {
		return Usuario{}, errf(ErrInvalido, "el equipo indicado no existe")
	}
	if err != nil {
		return Usuario{}, err
	}
	return s.UsuarioPorID(ctx, id)
}

func (s *Store) UsuarioPorID(ctx context.Context, id int64) (Usuario, error) {
	return scanUsuario(s.DB.QueryRowContext(ctx, selUsuario+`WHERE u.usuario_id = $1`, id))
}

func (s *Store) UsuarioPorEmail(ctx context.Context, email string) (Usuario, error) {
	return scanUsuario(s.DB.QueryRowContext(ctx, selUsuario+`WHERE lower(u.email) = lower($1)`, strings.TrimSpace(email)))
}

// ---------------------------------------------------------------- Sesiones

func (s *Store) CrearSesion(ctx context.Context, tokenHash string, usuarioID int64, ttl time.Duration) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sesion (sesion_id, usuario_id, expira_en) VALUES ($1, $2, now() + $3::interval)`,
		tokenHash, usuarioID, ttl.String())
	return err
}

func (s *Store) UsuarioDeSesion(ctx context.Context, tokenHash string) (Usuario, error) {
	return scanUsuario(s.DB.QueryRowContext(ctx, selUsuario+`
		JOIN sesion s ON s.usuario_id = u.usuario_id
		WHERE s.sesion_id = $1 AND s.expira_en > now()`, tokenHash))
}

func (s *Store) BorrarSesion(ctx context.Context, tokenHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sesion WHERE sesion_id = $1`, tokenHash)
	return err
}

// Purgar elimina sesiones y diffs en caché expirados.
func (s *Store) Purgar(ctx context.Context) (int64, error) {
	r1, err := s.DB.ExecContext(ctx, `DELETE FROM sesion WHERE expira_en <= now()`)
	if err != nil {
		return 0, err
	}
	r2, err := s.DB.ExecContext(ctx, `DELETE FROM cache_diff WHERE expira_en <= now()`)
	if err != nil {
		return 0, err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM restablecimiento WHERE expira_en <= now() - interval '1 day'`); err != nil {
		return 0, err
	}
	a, _ := r1.RowsAffected()
	b, _ := r2.RowsAffected()
	return a + b, nil
}
