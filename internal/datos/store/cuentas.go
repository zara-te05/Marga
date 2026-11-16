package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CrearRestablecimiento guarda un enlace de un solo uso para cambiar la contraseña.
// Invalida los anteriores del mismo usuario: solo vale el último que se pidió.
func (s *Store) CrearRestablecimiento(ctx context.Context, usuarioID int64, tokenHash string, ttl time.Duration) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM restablecimiento WHERE usuario_id = $1`, usuarioID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO restablecimiento (token_hash, usuario_id, expira_en)
			VALUES ($1, $2, now() + $3::interval)`, tokenHash, usuarioID, ttl.String())
		return err
	})
}

// Restablecer cambia la contraseña con un enlace válido, lo marca como usado y
// cierra todas las sesiones del usuario (por si alguien más tenía una abierta).
func (s *Store) Restablecer(ctx context.Context, tokenHash, nuevoHash string) (Usuario, error) {
	var uid int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			UPDATE restablecimiento SET usado_en = now()
			WHERE token_hash = $1 AND usado_en IS NULL AND expira_en > now()
			RETURNING usuario_id`, tokenHash).Scan(&uid)
		if errors.Is(err, sql.ErrNoRows) {
			return errf(ErrInvalido, "el enlace no es válido o ya expiró; pide uno nuevo")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE usuario SET password_hash = $1 WHERE usuario_id = $2`, nuevoHash, uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sesion WHERE usuario_id = $1`, uid); err != nil {
			return err
		}
		return Auditar(ctx, tx, uid, nil, "usuario.restablecer_password", nil)
	})
	if err != nil {
		return Usuario{}, err
	}
	return s.UsuarioPorID(ctx, uid)
}

// CambiarPassword guarda el nuevo hash y cierra las demás sesiones del usuario
// (la sesión desde la que se hizo el cambio, `conservar`, sigue abierta).
func (s *Store) CambiarPassword(ctx context.Context, usuarioID int64, nuevoHash, conservar string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE usuario SET password_hash = $1 WHERE usuario_id = $2`, nuevoHash, usuarioID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sesion WHERE usuario_id = $1 AND sesion_id <> $2`, usuarioID, conservar); err != nil {
			return err
		}
		return Auditar(ctx, tx, usuarioID, nil, "usuario.cambiar_password", nil)
	})
}
