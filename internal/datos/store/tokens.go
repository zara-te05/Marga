package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// PrefijoToken distingue un token de acceso personal de un token de sesión.
const PrefijoToken = "marga_pat_"

type TokenAPI struct {
	ID        int64      `json:"id"`
	Nombre    string     `json:"nombre"`
	Prefijo   string     `json:"prefijo"`
	CreadoEn  time.Time  `json:"creado_en"`
	UltimoUso *time.Time `json:"ultimo_uso"`
}

func (s *Store) CrearToken(ctx context.Context, u Usuario, nombre, tokenPlano, tokenHash string) (TokenAPI, error) {
	nombre = strings.TrimSpace(nombre)
	if nombre == "" {
		return TokenAPI{}, errf(ErrInvalido, "ponle un nombre al token (ej. \"Laptop\" o \"CI\")")
	}
	if len(nombre) > 120 {
		return TokenAPI{}, errf(ErrInvalido, "el nombre no puede superar 120 caracteres")
	}
	var t TokenAPI
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO token_api (usuario_id, nombre, token_hash, prefijo) VALUES ($1, $2, $3, $4)
			RETURNING token_id, nombre, prefijo, creado_en`,
			u.ID, nombre, tokenHash, tokenPlano[:len(PrefijoToken)+4]).Scan(&t.ID, &t.Nombre, &t.Prefijo, &t.CreadoEn)
		if err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, nil, "token.crear", map[string]any{"nombre": nombre})
	})
	return t, err
}

func (s *Store) ListarTokens(ctx context.Context, u Usuario) ([]TokenAPI, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT token_id, nombre, prefijo, creado_en, ultimo_uso
		FROM token_api WHERE usuario_id = $1 ORDER BY creado_en DESC`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TokenAPI{}
	for rows.Next() {
		var t TokenAPI
		if err := rows.Scan(&t.ID, &t.Nombre, &t.Prefijo, &t.CreadoEn, &t.UltimoUso); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevocarToken borra un token del usuario por id, o por hash si id == 0.
func (s *Store) RevocarToken(ctx context.Context, u Usuario, id int64, hash string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var nombre string
		err := tx.QueryRowContext(ctx, `
			DELETE FROM token_api
			WHERE usuario_id = $1 AND (token_id = $2 OR ($2 = 0 AND token_hash = $3))
			RETURNING nombre`, u.ID, id, hash).Scan(&nombre)
		if err == sql.ErrNoRows {
			return errf(ErrNoEncontrado, "token no encontrado")
		}
		if err != nil {
			return err
		}
		return Auditar(ctx, tx, u.ID, nil, "token.revocar", map[string]any{"nombre": nombre})
	})
}

// UsuarioDeToken valida un token personal y registra su último uso.
func (s *Store) UsuarioDeToken(ctx context.Context, tokenHash string) (Usuario, error) {
	u, err := scanUsuario(s.DB.QueryRowContext(ctx, selUsuario+`
		JOIN token_api t ON t.usuario_id = u.usuario_id
		WHERE t.token_hash = $1`, tokenHash))
	if err == nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE token_api SET ultimo_uso = now() WHERE token_hash = $1`, tokenHash)
	}
	return u, err
}
