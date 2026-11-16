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

# 6. Trabajar en ramas (Marga sigue la rama de git)
git checkout -b feature/facturas   # marga up usa tienda__feature_facturas (copia de la de main)
marga commit crear facturas        # …cambias la base, commit, push (crea la rama en el servidor)
marga push
marga verificar                    # antes del PR: ¿se aplican desde cero y se revierten limpio?
git checkout main                  # marga up vuelve a usar tienda
git merge feature/facturas         # juntas en git (o con el PR)…
marga renumerar                    # …si las dos ramas crearon la misma versión
marga push                         # …y push fusiona también en el servidor: un solo merge
marga url --escribir .env          # que tu app use la base de la rama actual
marga ramas                        # las bases por rama: tamaño, último uso
marga ramas borrar feature/facturas
```

## Qué guarda y dónde

| Archivo | ¿Va a git? | Contenido |
|---|---|---|
| `.marga/config.json` | sí | servidor, ID del proyecto, rama, motor, carpeta de migraciones |
| `.marga/remoto/<migración>` | sí | qué commit del servidor es cada migración local (un archivo por migración, para que git no choque al juntar ramas; antes era `.marga/remoto.json`) |
| `.marga/entornos.json` | **no** (`.marga/.gitignore`) | cadenas de conexión con contraseña |
| `.marga/local.json` | **no** | qué base usa cada rama de git en tus entornos personales |
| `.marga/snapshots/<entorno>/*.json` | **no** | esquema que dejó cada migración (lo usa `push`); las bases de rama usan `<entorno>@<base>/` |
| `migrations/*.sql` | sí | tus migraciones |
| `sql/**/*.sql` | sí | objetos repetibles: vistas, funciones… (se reaplican al cambiar) |
| `seeds/**/*.sql` | sí | datos de prueba para dev/local (`marga seed`) |
| `~/.marga/credenciales.json` | — | token de acceso por servidor (permisos 600). Tu contraseña nunca se guarda |
| tabla `marga_history` en **tu** BD | — | qué se aplicó, cuándo, cuánto tardó, quién y con qué checksum |
| tabla `marga_esquema` en **tu** BD | — | el esquema tal como lo dejó Marga (línea base para detectar drift) |
| tabla `marga_scripts` en **tu** BD | — | qué versión de cada repetible y seed se aplicó |

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

## Dónde se escribe el SQL

Todo el SQL vive en tu proyecto (va a git) y se prueba en tu base de desarrollo, que en
cada rama es una copia propia. Hay cuatro tipos de archivo:

| Qué | Dónde | Cómo se crea | Cuándo se ejecuta |
|---|---|---|---|
| Migración: un cambio, una vez | `migrations/0007_….up.sql` + `.down.sql` | `marga new …` o `marga commit …` | `marga up`, una sola vez por base |
| Objeto repetible: una vista o función completa | `sql/vistas/ventas.sql` | `marga new --repetible vistas/ventas` | `marga up`, cada vez que el archivo cambia |
| Datos de prueba | `seeds/0001_clientes.sql` | `marga new --seed clientes` | `marga seed`, solo en dev/local |
| SQL para probar | donde quieras, o `-c "…"` | — | `marga sql`: se deshace al terminar |

### Objetos repetibles (`sql/`)

`marga commit` no genera vistas, funciones, CHECK ni ENUM. Para las vistas y funciones, escribe
el objeto **completo** en un archivo de `sql/` (`CREATE OR REPLACE VIEW …`) y edítalo ahí
cada vez, en lugar de crear una migración por cada ajuste. `marga up` aplica primero las
migraciones y después los repetibles nuevos o cambiados, cada uno en su transacción.

- Se aplican en orden alfabético de ruta: si uno depende de otro, numéralos (`01_funciones/…`, `02_vistas/…`).
- `marga status` muestra cuáles están al día, cuáles cambiaron y cuáles son nuevos; `up --dry-run` muestra su SQL.
- Si borras el archivo, el objeto **sigue en la base**: Marga avisa y deja de seguirlo. Quítalo con una migración (`DROP VIEW …`).
- En entornos que requieren aprobación, los repetibles no se aplican (la web todavía no los revisa).
- Viajan con git, no con `marga push`.

### Datos de prueba (`seeds/`)

`marga seed` ejecuta los archivos de `seeds/` en tu base personal (la de tu rama), nunca en
`staging` ni `prod`. Útil cuando una rama se creó con `--vacia` y quedó sin datos.

- Cada seed corre una vez por base. Si lo editas no se repite solo (duplicaría datos):
  `marga seed 0001_clientes.sql` lo fuerza, `marga seed --todo` corre todos otra vez.
- Primero aplica las migraciones (`marga up`): los seeds esperan el esquema completo.

### Probar SQL sin compromiso (`marga sql`)

```
marga sql -c "SELECT * FROM clientes LIMIT 5"
marga sql -c "DELETE FROM clientes WHERE id = 3"     # ves qué pasa; al final se deshace
marga sql prueba.sql                                  # un archivo, una o varias sentencias
marga sql                                             # consola: SQL terminado en ; · \q para salir
marga sql -c "ALTER TABLE clientes ADD COLUMN rfc text" --guardar
```

Todo corre en una transacción que se **deshace** al final, así que puedes probar un `ALTER` o un
`DELETE` y ver el resultado sin cambiar nada. Con `--guardar` los cambios se quedan; si cambiaste
tablas, conviértelos en migración con `marga commit`. En un entorno compartido, `--guardar` pide `--si`.

## Juntar ramas: un solo merge

El merge lo haces en git (o con el botón del PR). Marga se adapta:

1. **Versiones repetidas.** Si tú y un compañero crearon una `0007` en ramas distintas, al juntarlas quedan
   dos archivos `0007` y Marga se detiene. `marga renumerar` deja la versión a la que ya estaba en la rama
   principal (en el servidor, o en git antes de juntar) y mueve las demás al final. `--simular` muestra
   el cambio sin hacerlo. Si ya habías aplicado la que se movió, `marga up` solo actualiza el historial.
2. **El servidor.** En la rama principal, `marga push` nota que las migraciones nuevas ya existen en otra rama
   del servidor (es la que juntaste en git) y la **fusiona en el servidor** en vez de subirlas como commits
   nuevos. Si el servidor las renumera, renombra tus archivos para que coincidan. Si ambas ramas tocaron
   las mismas tablas se detiene: revisa con `marga verificar` y repite con `marga push --forzar`.
3. Haz commit en git de los archivos que se hayan renombrado.

`.marga/remoto/` guarda un archivo por migración (antes era un solo `remoto.json`), así dos ramas que
enviaron migraciones distintas no chocan en git al juntarse.

`marga merge` sigue existiendo para fusionar en el servidor a mano.

## `marga verificar`: ¿estas migraciones funcionan?

Crea una base vacía y temporal y en ella:

1. aplica cada migración desde cero, en orden;
2. revierte cada una, de la última a la primera, y comprueba que el `DOWN` deja la base **exactamente**
   como estaba antes de esa migración (si un `DOWN` olvida quitar una columna, lo dice);
3. las vuelve a aplicar todas;
4. aplica los objetos repetibles (dos veces: deben usar `CREATE OR REPLACE`) y, con `--seeds`, los seeds.

Al terminar borra la base y, si algo falló, sale con error. La base se crea en el servidor de `--url`,
de `MARGA_VERIFICAR_URL` o de tu entorno `dev`; el usuario necesita `CREATEDB`.
Una migración sin `DOWN` es un error, salvo con `--permitir-sin-down`.

En CI, en cada PR (GitHub Actions):

```yaml
jobs:
  migraciones:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env: { POSTGRES_PASSWORD: postgres }
        ports: ["5432:5432"]
        options: --health-cmd pg_isready --health-interval 5s --health-retries 10
    steps:
      - uses: actions/checkout@v4
      - run: curl -sL <url del release de marga> | tar xz   # o descárgalo como prefieras
      - run: ./marga verificar --seeds
        env:
          MARGA_VERIFICAR_URL: postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
```

## Una base de datos por rama

Cambiar de rama en git también cambia tu base de desarrollo, como cambia el código.
Puedes romper `feature/facturas` sin tocar `main`.

- **Marga sigue la rama de git**: base de datos, `push`, `pull` y `log` usan la rama actual.
  `push` crea la rama en el servidor si no existe (desde la rama de la que saliste en git, o main).
  `marga checkout` queda solo para proyectos sin git.
- **Solo los entornos personales** (`dev`, `local`) se ramifican. `staging` y `prod` nunca.
- La rama principal (`main`, o `master`) usa la base de la cadena de conexión. Cada otra rama usa
  `<base>__<rama>` (máximo 63 caracteres; si no cabe se recorta y lleva un hash corto).
- **La primera vez que usas una rama** (`marga up`, `status`…) Marga crea su base como copia, con datos,
  de la base de la rama de la que saliste (`CREATE DATABASE … TEMPLATE`): tarda segundos.
  `--vacia` la crea vacía y aplica todas las migraciones desde cero.
- **Base ocupada**: PostgreSQL no deja copiar una base si alguien está conectado (pgAdmin, tu app).
  Marga dice quién está conectado; cierra esas conexiones o usa `marga up --vacia`.
- **Rama nacida de un main más viejo**: si la base de origen tiene migraciones que la rama no conoce,
  la copia tendría un historial roto. Marga lo detecta y crea la base vacía.
- Las **snapshots** y la **línea base del drift** van por base, no por entorno.
- En la web, el entorno aparece como **dev@zara · feature/facturas** y sus pendientes se cuentan
  contra esa rama del servidor.

```
marga ramas                          lista las bases por rama: tamaño, último uso
marga ramas borrar feature/facturas  borra la base de esa rama (--forzar si alguien está conectado)
marga ramas reiniciar                vuelve a crear la base de la rama actual (--vacia, --desde main)
marga ramas limpiar                  borra las bases de ramas que ya no existen en git (--simular)
```

**Que tu app también cambie de base**: Marga cambia la base con la que trabaja Marga, pero tu app
usa su propia cadena de conexión. `marga url` imprime la de la base de tu rama (y la crea si falta):

```
DATABASE_URL=$(marga url) npm run dev          # bash / zsh
$env:DATABASE_URL = marga url; dotnet run      # PowerShell
marga url --escribir .env                      # actualiza DATABASE_URL=… en .env (--variable PG_URL para otro nombre)
```

Solo la URL va a la salida estándar; los avisos van a la de errores.

**Seguridad al borrar**: Marga marca cada base que crea con `COMMENT ON DATABASE … 'marga:rama=…'`
y solo borra bases con esa marca. Nunca puede borrar tu base principal por error.

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
- `marga checkout` solo existe para proyectos sin git; con git, Marga sigue la rama de git.
- Las bases por rama solo existen en PostgreSQL y en entornos personales.
- La introspección y el drift solo funcionan en PostgreSQL.
