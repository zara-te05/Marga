-- =====================================================================
--  Fase 3: merge entre ramas y revisiones
-- =====================================================================

-- La identidad real de una migración es su SQL: al fusionar ramas el commit
-- cambia de hash (nuevo padre) pero conserva el checksum. Es el mismo cálculo
-- que hace la CLI: sha256 del sql_up normalizado.
ALTER TABLE commit ADD COLUMN checksum CHAR(64);
UPDATE commit
SET checksum = encode(sha256(convert_to(rtrim(replace(sql_up, E'\r\n', E'\n'), E' \t\n') || E'\n', 'UTF8')), 'hex');
ALTER TABLE commit ALTER COLUMN checksum SET NOT NULL;
CREATE INDEX commit_checksum_idx ON commit (repositorio_id, checksum);

-- Revisiones: otra persona aprueba (o pide cambios) antes de aplicar en entornos protegidos.
CREATE TABLE aprobacion (
    aprobacion_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    commit_hash   CHAR(64)    NOT NULL REFERENCES commit (hash_id) ON DELETE CASCADE,
    usuario_id    BIGINT      NOT NULL REFERENCES usuario (usuario_id) ON DELETE CASCADE,
    estado        VARCHAR(12) NOT NULL CHECK (estado IN ('aprobado', 'cambios')),
    comentario    TEXT        NOT NULL DEFAULT '',
    fecha         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT aprobacion_uq UNIQUE (commit_hash, usuario_id)
);

ALTER TABLE entorno_bd ADD COLUMN requiere_aprobacion BOOLEAN NOT NULL DEFAULT false;
