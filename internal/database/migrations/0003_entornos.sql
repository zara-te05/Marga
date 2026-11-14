-- =====================================================================
--  Fase 2: estado real de cada entorno (dev, staging, prod…)
--  La CLI reporta qué migraciones tiene aplicadas cada base de datos,
--  su esquema real (introspección) y los cambios hechos fuera de Marga (drift).
-- =====================================================================
CREATE TABLE entorno_bd (
    entorno_bd_id  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repositorio_id BIGINT       NOT NULL REFERENCES repositorio (repositorio_id) ON DELETE CASCADE,
    nombre         VARCHAR(80)  NOT NULL,                 -- "staging", o "dev@zara" si es personal
    motor          VARCHAR(20)  NOT NULL DEFAULT 'postgresql',
    compartido     BOOLEAN      NOT NULL DEFAULT false,   -- false = base personal de un desarrollador
    esquema        JSONB,                                 -- tablas, columnas, índices y FKs reales
    drift          JSONB        NOT NULL DEFAULT '[]'::jsonb,
    reportado_en   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    reportado_por  BIGINT       REFERENCES usuario (usuario_id) ON DELETE SET NULL,
    CONSTRAINT entorno_bd_uq UNIQUE (repositorio_id, nombre)
);

CREATE TABLE migracion_aplicada (
    entorno_bd_id BIGINT      NOT NULL REFERENCES entorno_bd (entorno_bd_id) ON DELETE CASCADE,
    version       VARCHAR(32) NOT NULL,
    nombre        TEXT        NOT NULL,
    checksum      CHAR(64)    NOT NULL,
    commit_hash   CHAR(64)    REFERENCES commit (hash_id) ON DELETE SET NULL,  -- NULL si no se ha enviado
    aplicada_en   TIMESTAMPTZ NOT NULL,
    duracion_ms   INTEGER     NOT NULL DEFAULT 0,
    aplicada_por  TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (entorno_bd_id, version)
);
CREATE INDEX migracion_aplicada_commit_idx ON migracion_aplicada (commit_hash);
