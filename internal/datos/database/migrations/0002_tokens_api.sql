-- =====================================================================
--  Tokens de acceso personales (para la CLI y para CI)
--  El token en claro se muestra una sola vez; aquí solo se guarda su SHA-256.
-- =====================================================================
CREATE TABLE token_api (
    token_id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id BIGINT       NOT NULL REFERENCES usuario (usuario_id) ON DELETE CASCADE,
    nombre     VARCHAR(120) NOT NULL,
    token_hash CHAR(64)     NOT NULL UNIQUE,
    prefijo    VARCHAR(20)  NOT NULL,          -- primeros caracteres, para reconocerlo
    creado_en  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ultimo_uso TIMESTAMPTZ
);
CREATE INDEX token_api_usuario_idx ON token_api (usuario_id);
