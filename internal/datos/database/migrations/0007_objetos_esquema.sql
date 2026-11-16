-- =====================================================================
--  Funciones, vistas, triggers y tipos: snapshot_esquema guarda ahora
--  cualquier objeto del esquema, no solo tablas. La columna `tabla` sigue
--  siendo el nombre del objeto (para una función, su firma: f(integer);
--  para un trigger, "trigger ON tabla"); `tipo` dice qué es.
--  Los nombres no chocan entre tipos, así que la llave única no cambia.
-- =====================================================================
ALTER TABLE snapshot_esquema
    ADD COLUMN tipo VARCHAR(30) NOT NULL DEFAULT 'tabla'
    CONSTRAINT snapshot_tipo_ck CHECK (tipo IN ('tabla', 'funcion', 'procedimiento', 'vista',
                                                'vista_materializada', 'trigger', 'tipo'));
ALTER TABLE snapshot_esquema ALTER COLUMN tabla TYPE VARCHAR(1000);
