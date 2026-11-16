package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type Revision struct {
	UsuarioID  int64     `json:"usuario_id"`
	Usuario    string    `json:"usuario"`
	Estado     string    `json:"estado"` // "aprobado" o "cambios"
	Comentario string    `json:"comentario"`
	Fecha      time.Time `json:"fecha"`
}

func (s *Store) ListarRevisiones(ctx context.Context, hash string) ([]Revision, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.usuario_id, u.nombre, a.estado, a.comentario, a.fecha
		FROM aprobacion a JOIN usuario u ON u.usuario_id = a.usuario_id
		WHERE a.commit_hash = $1 ORDER BY a.fecha`, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.UsuarioID, &r.Usuario, &r.Estado, &r.Comentario, &r.Fecha); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Revisar registra (o actualiza) la revisión del usuario sobre un commit.
func (s *Store) Revisar(ctx context.Context, u Usuario, repoID int64, ref, estado, comentario string) error {
	estado, comentario = strings.TrimSpace(estado), strings.TrimSpace(comentario)
	if estado != "aprobado" && estado != "cambios" {
		return errf(ErrInvalido, "estado debe ser 'aprobado' o 'cambios'")
	}
	if estado == "cambios" && comentario == "" {
		return errf(ErrInvalido, "explica qué cambios pides")
	}
	if len(comentario) > 4000 {
		return errf(ErrInvalido, "el comentario es demasiado largo")
	}
	hash, err := s.resolverHash(ctx, s.DB, repoID, ref)
	if err != nil {
		return err
	}
	var autor sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT autor_id FROM commit WHERE hash_id = $1`, hash).Scan(&autor); err != nil {
		return err
	}
	if estado == "aprobado" && autor.Valid && autor.Int64 == u.ID {
		return errf(ErrProhibido, "no puedes aprobar tu propia migración: pídele a otra persona que la revise")
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO aprobacion (commit_hash, usuario_id, estado, comentario) VALUES ($1, $2, $3, $4)
			ON CONFLICT (commit_hash, usuario_id)
			DO UPDATE SET estado = EXCLUDED.estado, comentario = EXCLUDED.comentario, fecha = now()`,
			hash, u.ID, estado, comentario); err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, &repoID, "commit.revisar", map[string]any{"hash": hash, "estado": estado})
	})
}

func (s *Store) QuitarRevision(ctx context.Context, u Usuario, repoID int64, ref string) error {
	hash, err := s.resolverHash(ctx, s.DB, repoID, ref)
	if err != nil {
		return err
	}
	r, err := s.DB.ExecContext(ctx, `DELETE FROM aprobacion WHERE commit_hash = $1 AND usuario_id = $2`, hash, u.ID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return errf(ErrNoEncontrado, "no tienes una revisión en ese commit")
	}
	return nil
}
