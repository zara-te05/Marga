# `marga` · la CLI

Control de versiones para bases de datos. Aplica, revierte y sincroniza migraciones
de PostgreSQL, y las comparte con tu equipo a través del servidor de Marga (`marga-server`).

**Instalar**: descarga `marga_<versión>_<sistema>.zip` de la página de *Releases* del repositorio
y pon `marga` (o `marga.exe`) en tu `PATH`. O compílala desde la raíz del repositorio:

```
go build -o marga.exe ./cmd/marga      # Windows  (en Linux/Mac: go build -o marga ./cmd/marga)
marga version
```

- `marga <comando>` → CLI normal (sirve para scripts y CI).
- `marga` sin argumentos → consola interactiva (TUI). Ahí escribes los mismos comandos sin el `marga`.

## Flujo de trabajo

```powershell
# 1. Crear el proyecto en la carpeta de tu app
marga login --servidor http://localhost:8080        # pide email y contraseña (oculta)
marga init --crear "Tienda Online"                  # o --repo 7 para uno que ya existe

# 2. Decir dónde está la base de datos
marga env add dev "postgres://usuario:clave@localhost:5432/tienda?sslmode=disable"
marga env probar dev

# 3. Escribir una migración (dos formas)
marga new crear tabla clientes            # a mano: crea los archivos vacíos
#   -> migrations/0001_crear_tabla_clientes.up.sql    (el cambio)
#   -> migrations/0001_crear_tabla_clientes.down.sql  (cómo deshacerlo)
marga commit crear tabla clientes         # automática: cambia tu base dev y Marga escribe el SQL
marga revisar                             # análisis de riesgo de lo que aún no envías

# 4. Aplicarla
marga status           # qué está aplicado y qué falta
marga up --dry-run     # ver el SQL sin ejecutarlo
marga up               # aplicar
marga down             # revertir la última
marga drift            # cambios hechos a mano en la base (fuera de Marga)
marga esquema [tabla]  # el esquema real, o el DDL de una tabla

# 5. Compartirla con el equipo
marga push             # sube tus migraciones al servidor
marga pull             # baja las de tus compañeros
marga log              # historial de la rama en el servidor

# 6. Trabajar en ramas
marga branch feature/facturas      # crea la rama en el servidor
marga checkout feature/facturas    # push/pull/log usan esa rama
marga merge feature/facturas --hacia main --simular
marga merge feature/facturas --hacia main
```

## Qué guarda y dónde

| Archivo | ¿Va a git? | Contenido |
|---|---|---|
| `.marga/config.json` | sí | servidor, ID del proyecto, rama, motor, carpeta de migraciones |
| `.marga/remoto.json` | sí | qué migración local corresponde a qué commit del servidor |
| `.marga/entornos.json` | **no** (`.marga/.gitignore`) | cadenas de conexión con contraseña |
| `.marga/snapshots/<entorno>/*.json` | **no** | esquema que dejó cada migración (lo usa `push`) |
| `migrations/*.sql` | sí | tus migraciones |
| `~/.marga/credenciales.json` | — | token de acceso por servidor (permisos 600). Tu contraseña nunca se guarda |
| tabla `marga_history` en **tu** BD | — | qué se aplicó, cuándo, cuánto tardó, quién y con qué checksum |
| tabla `marga_esquema` en **tu** BD | — | el esquema tal como lo dejó Marga (línea base para detectar drift) |

## Garantías

- **Cada migración corre en una transacción** junto con su registro en `marga_history`: si falla, no queda nada a medias.
  Para SQL que no puede ir en transacción (p. ej. `CREATE INDEX CONCURRENTLY`) agrega el comentario `-- marga:sin-transaccion`.
- **Bloqueo**: dos personas no pueden aplicar migraciones a la vez en la misma base.
- **Checksums**: si editas una migración que ya se aplicó, `status` lo marca y `up` se niega a seguir.
  La regla es la de git: lo publicado no se reescribe; crea una migración nueva.
- **Producción**: los entornos llamados `prod`/`produccion` exigen `--si` en `up` y `down`.
- **Errores legibles**: los errores de PostgreSQL muestran la línea del archivo donde ocurrieron.
- **Equipos**: si tú y un compañero crean la misma versión (p. ej. `0006`) sin saberlo, `push` te pide
  hacer `pull` primero y `pull` renumera **tu** migración sin enviar. Así todos tienen la misma numeración.

## Drift: cambios hechos fuera de Marga

Después de cada `up`/`down`, Marga guarda el esquema resultante en `marga_esquema`.
`marga status` lo compara con el esquema real y lista cualquier diferencia:

```
! 2 cambios hechos fuera de Marga en staging:
    + columna cupones.vence date
    + índice idx_pedidos_total en pedidos
```

Mientras haya drift, `up` y `down` no hacen nada (para no construir sobre algo desconocido).
Si los cambios son correctos: `marga drift --aceptar --env staging`. La primera vez que Marga ve
una base, toma su estado actual como línea base.

## Entornos y la web

Cada `status`, `up`, `down` y `drift` envía al servidor el estado del entorno: qué tiene aplicado,
su esquema real y su drift. Con eso la web muestra la pestaña **Entornos** (aplicadas y pendientes
por entorno), el **diagrama ER real** en Esquema y en qué entornos está cada commit.

- `dev` y `local` son **personales**: se reportan como `dev@tu-usuario`, para no mezclar la base de cada quien.
- El resto (`staging`, `prod`…) son **compartidos**. Se puede forzar con `env add --personal` o `--compartido`.
- Si no hay sesión o el proyecto no está vinculado, simplemente no se reporta.

`marga up` también guarda el esquema que dejó cada migración; `marga push` lo usa para enviar el
DDL de las tablas que cambió. Por eso conviene aplicar (`up`) antes de hacer `push`.

## `marga commit`: la migración se escribe sola

1. Cambia tu base de desarrollo como quieras (SQL, pgAdmin, DBeaver…).
2. `marga commit agregar telefono a clientes` compara la base con lo que dejó la última
   migración y escribe el `.up.sql` y el `.down.sql`. Como los cambios ya están en tu base,
   la migración queda registrada como aplicada ahí.
3. Revisa los archivos (sobre todo los avisos) y súbelos con `marga push`.

Genera tablas (incluidas `serial` e `IDENTITY`), columnas (tipo, NULL, DEFAULT), llave primaria,
llaves foráneas e índices, en un orden que no rompe dependencias. Un renombre lo ve como
borrar + crear: corrígelo a mano con `RENAME`. No genera CHECK, vistas, funciones ni ENUM.

## Análisis de riesgo

`marga revisar`, `marga up` y la web avisan del SQL peligroso: `DROP TABLE`/`DROP COLUMN`,
`NOT NULL` sin `DEFAULT` en tablas existentes, cambios de tipo, `SET NOT NULL`, índices sin
`CONCURRENTLY`, llaves foráneas sin `NOT VALID`, renombres, `DELETE`/`UPDATE` sin `WHERE`,
`CONCURRENTLY` sin `-- marga:sin-transaccion`, y migraciones sin `DOWN`.

## Ramas y fusión

La historia de una base es lineal (las migraciones se aplican en orden), así que `marga merge`:

- si el destino no avanzó → **fast-forward**;
- si avanzó → **copia** las migraciones encima; si su número ya existe en el destino, las **renumera**;
- si ambas ramas tocaron la misma tabla → **conflicto** (no fusiona salvo `--forzar`).

La identidad de una migración es su **SQL** (checksum), no su número. Cuando una migración
que ya aplicaste cambia de número al fusionar, `marga pull` renombra tu archivo y `marga up`
actualiza `marga_history` sin volver a ejecutarla (`status` la muestra como `↻ renumerada`).

## Revisiones (entornos protegidos)

En la web, un admin puede marcar un entorno (p. ej. `prod`) como **Requiere aprobación**.
Entonces `marga up` en ese entorno solo aplica migraciones que están en el servidor y que
**otra persona** aprobó en *Commits → Revisiones* (nadie puede aprobar las suyas).

## En CI (GitHub Actions, GitLab…)

En CI no hace falta `marga login`: crea un token en la web (**Cuenta → Tokens de acceso**),
guárdalo como secreto y pásalo en `MARGA_TOKEN`. La URL de la base también puede venir de una variable:

```bash
export MARGA_TOKEN=marga_pat_...                  # secreto del CI
marga env add prod '$DATABASE_URL'                # se guarda el nombre; la URL se lee al ejecutar
marga up --env prod --si
```

| Variable | Para qué |
|---|---|
| `MARGA_TOKEN` | token de acceso personal; reemplaza a `marga login` |
| `MARGA_SERVIDOR` | servidor por defecto cuando no hay proyecto (en lugar de `http://localhost:8080`) |
| `MARGA_PASSWORD` | contraseña para `marga login` sin teclado |
| `MARGA_DEBUG` | escribe `debug.log` al usar la consola interactiva |

## Limitaciones actuales

- Aplicar migraciones: solo PostgreSQL (`env probar` también funciona con MySQL y SQL Server).
- `marga checkout` solo cambia la rama del servidor; los archivos de `migrations/` los maneja tu git.
- La introspección y el drift solo funcionan en PostgreSQL.
