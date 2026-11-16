-- =====================================================================
--  Una base de datos por rama: los entornos personales (dev@zara) reportan
--  también la rama de git en la que está su base. La web la muestra
--  ("dev@zara · feature/facturas") y compara el entorno con esa rama.
-- =====================================================================
ALTER TABLE entorno_bd ADD COLUMN rama_git VARCHAR(255);
