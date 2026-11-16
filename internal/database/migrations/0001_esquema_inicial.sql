-- =====================================================================
--  Marga · Esquema inicial (PostgreSQL 14+)
--  Traducción del diagrama ER "Diagrama en blanco" con correcciones:
--   * FKs con typo (ususario_id / reposirorio_id) -> usuario_id / repositorio_id
--   * commit.repositorio_id: un commit pertenece a un repositorio; la rama
--     es solo un puntero (branch.ultimo_commit_hash), igual que en git.
--   * commit.sql_up / sql_down: el contenido real de la migración.
--   * cache_diff: PK compuesta (commit_origen, commit_destino) en vez de
--     una cadena concatenada; "ttl" se guarda como fecha de expiración.
--   * sesion.sesion_id: SHA-256 del token (el token en claro nunca se guarda).
--   * indice_busqueda_commit: columna tsvector + índice GIN (búsqueda real).
-- =====================================================================

-- ---------- Equipo ----------
CREATE TABLE equipo (
    equipo_id  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre     VARCHAR(120) NOT NULL UNIQUE,
    creado_en  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- ---------- Usuario ----------
CREATE TABLE usuario (
    usuario_id    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre        VARCHAR(120) NOT NULL,
    email         VARCHAR(255) NOT NULL,
    password_hash TEXT         NOT NULL,
    rol           VARCHAR(20)  NOT NULL DEFAULT 'desarrollador'
                  CHECK (rol IN ('admin', 'desarrollador', 'lector')),
    equipo_id     BIGINT REFERENCES equipo (equipo_id) ON DELETE SET NULL,
    creado_en     TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX usuario_email_uq ON usuario (lower(email));
CREATE INDEX usuario_equipo_idx ON usuario (equipo_id);

-- ---------- Sesion ----------
CREATE TABLE sesion (
    sesion_id  CHAR(64)    PRIMARY KEY,               -- sha256(token) en hex
    usuario_id BIGINT      NOT NULL REFERENCES usuario (usuario_id) ON DELETE CASCADE,
    creado_en  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira_en  TIMESTAMPTZ NOT NULL
);
CREATE INDEX sesion_usuario_idx ON sesion (usuario_id);
CREATE INDEX sesion_expira_idx  ON sesion (expira_en);

-- ---------- Repositorio ----------
CREATE TABLE repositorio (
    repositorio_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre         VARCHAR(120) NOT NULL,
    slug           VARCHAR(140) NOT NULL,
    descripcion    TEXT         NOT NULL DEFAULT '',
    visibilidad    VARCHAR(10)  NOT NULL DEFAULT 'privado'
                   CHECK (visibilidad IN ('publico', 'privado')),
    motor          VARCHAR(20)  NOT NULL DEFAULT 'postgresql'
                   CHECK (motor IN ('postgresql', 'mysql', 'sqlserver', 'mongodb', 'sqlite')),
    entorno        VARCHAR(20)  NOT NULL DEFAULT 'Desarrollo'
                   CHECK (entorno IN ('Producción', 'Staging', 'Desarrollo', 'Sandbox')),
    creado_en     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    propietario_id BIGINT       NOT NULL REFERENCES usuario (usuario_id) ON DELETE RESTRICT,
    CONSTRAINT repositorio_slug_uq UNIQUE (propietario_id, slug)
);

-- ---------- Colaborador ----------
CREATE TABLE colaborador (
    colaborador_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id     BIGINT      NOT NULL REFERENCES usuario (usuario_id) ON DELETE CASCADE,
    repositorio_id BIGINT      NOT NULL REFERENCES repositorio (repositorio_id) ON DELETE CASCADE,
    permiso        VARCHAR(12) NOT NULL DEFAULT 'lectura'
                   CHECK (permiso IN ('lectura', 'escritura', 'admin')),
    CONSTRAINT colaborador_uq UNIQUE (usuario_id, repositorio_id)
);
CREATE INDEX colaborador_repo_idx ON colaborador (repositorio_id);

-- ---------- Auditoria ----------
CREATE TABLE auditoria (
    auditoria_id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id     BIGINT REFERENCES usuario (usuario_id) ON DELETE SET NULL,
    repositorio_id BIGINT REFERENCES repositorio (repositorio_id) ON DELETE SET NULL,
    accion         VARCHAR(60) NOT NULL,
    parametros     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    fecha          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX auditoria_repo_fecha_idx    ON auditoria (repositorio_id, fecha DESC);
CREATE INDEX auditoria_usuario_fecha_idx ON auditoria (usuario_id, fecha DESC);

-- ---------- Branch ----------
CREATE TABLE branch (
    branch_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre             VARCHAR(120) NOT NULL,
    repositorio_id     BIGINT       NOT NULL REFERENCES repositorio (repositorio_id) ON DELETE CASCADE,
    ultimo_commit_hash CHAR(64),                       -- FK se añade tras crear commit
    creado_en          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT branch_nombre_uq UNIQUE (repositorio_id, nombre)
);

-- ---------- Commit ----------
CREATE TABLE commit (
    hash_id          CHAR(64)    PRIMARY KEY,           -- sha256 del contenido (como git)
    repositorio_id   BIGINT      NOT NULL REFERENCES repositorio (repositorio_id) ON DELETE CASCADE,
    mensaje          TEXT        NOT NULL,
    fecha            TIMESTAMPTZ NOT NULL DEFAULT now(),
    autor_id         BIGINT      REFERENCES usuario (usuario_id) ON DELETE SET NULL,
    branch_id        BIGINT      REFERENCES branch (branch_id) ON DELETE SET NULL, -- rama donde se creó
    padre_hash       CHAR(64)    REFERENCES commit (hash_id),
    padre_merge_hash CHAR(64)    REFERENCES commit (hash_id),                     -- 2º padre en merges
    sql_up           TEXT        NOT NULL,
    sql_down         TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX commit_repo_fecha_idx ON commit (repositorio_id, fecha DESC);
CREATE INDEX commit_padre_idx      ON commit (padre_hash);

ALTER TABLE branch
    ADD CONSTRAINT branch_ultimo_commit_fk
    FOREIGN KEY (ultimo_commit_hash) REFERENCES commit (hash_id) ON DELETE SET NULL;

-- ---------- Snapshot_Esquema ----------
CREATE TABLE snapshot_esquema (
    snapshot_e_id  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    commit_hash_id CHAR(64)     NOT NULL REFERENCES commit (hash_id) ON DELETE CASCADE,
    tabla          VARCHAR(255) NOT NULL,
    ddl            TEXT         NOT NULL DEFAULT '',   -- DDL completo de la tabla en ese commit
    diff           TEXT         NOT NULL DEFAULT '',   -- cambio respecto al commit padre
    CONSTRAINT snapshot_uq UNIQUE (commit_hash_id, tabla)
);

-- ---------- Cache_Diff ----------
CREATE TABLE cache_diff (
    commit_origen  CHAR(64)    NOT NULL REFERENCES commit (hash_id) ON DELETE CASCADE,
    commit_destino CHAR(64)    NOT NULL REFERENCES commit (hash_id) ON DELETE CASCADE,
    diff_calculado JSONB       NOT NULL,
    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira_en      TIMESTAMPTZ NOT NULL,                -- equivale al "ttl" del diagrama
    PRIMARY KEY (commit_origen, commit_destino)
);
CREATE INDEX cache_diff_expira_idx ON cache_diff (expira_en);

-- ---------- Indice_Busqueda_Commit ----------
CREATE TABLE indice_busqueda_commit (
    indice_busqueda_c_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    commit_hash_id       CHAR(64) NOT NULL UNIQUE REFERENCES commit (hash_id) ON DELETE CASCADE,
    repositorio_id       BIGINT   NOT NULL REFERENCES repositorio (repositorio_id) ON DELETE CASCADE,
    mensaje              TEXT     NOT NULL,
    documento            TSVECTOR GENERATED ALWAYS AS (to_tsvector('spanish', mensaje)) STORED
);
CREATE INDEX indice_busqueda_doc_idx  ON indice_busqueda_commit USING GIN (documento);
CREATE INDEX indice_busqueda_repo_idx ON indice_busqueda_commit (repositorio_id);

-- El índice se mantiene solo: cada commit nuevo se indexa automáticamente.
CREATE FUNCTION indexar_commit() RETURNS trigger AS $$
BEGIN
    INSERT INTO indice_busqueda_commit (commit_hash_id, repositorio_id, mensaje)
    VALUES (NEW.hash_id, NEW.repositorio_id, NEW.mensaje || ' ' || left(NEW.sql_up, 4000));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER commit_indexar_trg
    AFTER INSERT ON commit
    FOR EACH ROW EXECUTE FUNCTION indexar_commit();
