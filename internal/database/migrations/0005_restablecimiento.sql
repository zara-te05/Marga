-- Enlaces para restablecer la contraseña. Como las sesiones, se guarda el
-- SHA-256 del token: quien lea la tabla no puede usar los enlaces.
CREATE TABLE restablecimiento (
    token_hash CHAR(64)    PRIMARY KEY,
    usuario_id BIGINT      NOT NULL REFERENCES usuario (usuario_id) ON DELETE CASCADE,
    creado_en  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira_en  TIMESTAMPTZ NOT NULL,
    usado_en   TIMESTAMPTZ
);
CREATE INDEX restablecimiento_usuario_idx ON restablecimiento (usuario_id);
