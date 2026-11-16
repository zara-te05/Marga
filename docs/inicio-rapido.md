# Inicio rápido: tu primera migración en 5 minutos

De cero a una migración aplicada en tu base, sin servidor y sin leer el resto de la
documentación. Solo necesitas una base de PostgreSQL a la que puedas conectarte.

## 1. Instala marga (1 minuto)

| Sistema | Comando |
|---|---|
| Windows | `winget install zara-te05.Marga` |
| Windows (Scoop) | `scoop bucket add marga https://github.com/zara-te05/scoop-marga; scoop install marga` |
| macOS / Linux | `brew install zara-te05/tap/marga` |
| Sin gestor de paquetes (Windows) | `irm https://raw.githubusercontent.com/zara-te05/Marga/main/scripts/instalar.ps1 \| iex` |
| Sin gestor de paquetes (macOS / Linux) | `curl -fsSL https://raw.githubusercontent.com/zara-te05/Marga/main/scripts/instalar.sh \| sh` |

Abre una terminal nueva y comprueba:

```
marga version
```

Te dice la versión, desde dónde se ejecuta y, si hay otro `marga` más viejo en el PATH
que la terminal use antes, cuál es. También avisa si salió una versión más nueva.

> ¿No tienes PostgreSQL? Con Docker:
> `docker run -d --name pg -e POSTGRES_PASSWORD=clave -p 5432:5432 postgres:16`
> y tu cadena es `postgres://postgres:clave@localhost:5432/postgres?sslmode=disable`.

## 2. Crea el proyecto (1 minuto)

En la carpeta de tu aplicación (con git, para que las migraciones queden versionadas):

```
mkdir tienda
cd tienda
git init
marga init
```

`marga init` te pregunta tres cosas y prueba la conexión antes de seguir:

```
Vamos a crear un proyecto de Marga en C:\proyectos\tienda
Enter acepta lo que está entre [corchetes]. Para scripts: marga init --help

Nombre del proyecto [tienda]:

Cadena de conexión de tu base de desarrollo (Enter para hacerlo después)
  ejemplo: postgres://usuario:clave@localhost:5432/mi_bd?sslmode=disable
URL: postgres://postgres:clave@localhost:5432/postgres?sslmode=disable
  ✓ Conexión correcta (postgres://postgres:****@localhost:5432/postgres?sslmode=disable)

¿Compartirás el historial con un servidor de Marga? [s/N]

✓ Proyecto de Marga creado en C:\proyectos\tienda
  .marga/config.json      configuración (versiónala con git)
  migrations/             aquí van tus migraciones
  .marga/entornos.json    entorno dev (no se versiona: tiene tu contraseña)
  modo local: sin servidor. El historial vive en git (migrations/) y en marga_history de cada base.

La base está vacía. Siguiente paso:
  marga new "crear tabla clientes"   escribe la primera migración
  marga up                           y aplícala
```

Contestar **no** al servidor deja el proyecto en **modo local**: no hace falta cuenta
ni `marga login`. El historial vive en git (la carpeta `migrations/`) y en la tabla
`marga_history` de cada base. Siempre puedes conectarlo a un servidor después con
`marga link --repo ID`.

**¿Tu base ya tiene tablas?** `marga init` lo detecta y ofrece adoptarla en el mismo
paso: escribe la migración `0001` con el esquema actual, la comprueba en una base
temporal y la marca como aplicada. Después sigue en el paso 4 con tu siguiente cambio.

## 3. Escribe la migración (2 minutos)

```
marga new "crear tabla clientes"
```

Crea dos archivos. En `migrations/0001_crear_tabla_clientes.up.sql`, lo que aplica el cambio:

```sql
CREATE TABLE clientes (
    id     serial PRIMARY KEY,
    nombre text NOT NULL,
    email  text UNIQUE
);
```

Y en `migrations/0001_crear_tabla_clientes.down.sql`, lo que lo deshace:

```sql
DROP TABLE clientes;
```

## 4. Aplícala (1 minuto)

```
marga status     # 0001_crear_tabla_clientes aparece como pendiente
marga up         # la aplica en una transacción, con bloqueo y checksum
marga status     # ahora aparece como aplicada
```

Listo: tu primera migración está aplicada. Guárdala en git para que tu equipo la tenga:

```
git add .marga migrations
git commit -m "Primera migración: tabla clientes"
```

Quien clone el repositorio corre `marga env add dev "<su cadena>"` y `marga up`, y
su base queda igual que la tuya.

## Si algo sale mal

Cada error de marga termina con una línea `prueba:` con lo siguiente que conviene
hacer. Los más comunes al empezar:

| Ves | Qué pasa | Qué hacer |
|---|---|---|
| `unknown command "init"` o `unknown flag: --local` | La terminal está usando un `marga.exe` viejo (por ejemplo en `%USERPROFILE%\go\bin`) | `where marga` (Windows) o `which -a marga` te dice cuáles hay; borra el viejo y corre `marga version` |
| `este proyecto todavía no tiene entornos` | No has dicho a qué base conectarte | `marga env add dev "postgres://usuario:clave@localhost:5432/mi_bd?sslmode=disable"` |
| `no se pudo conectar` | La base no responde o la cadena está mal | Revisa que PostgreSQL esté corriendo y prueba con `marga env probar dev` |
| `respondió 404 … ahí no hay un servidor de Marga` | La URL del servidor apunta a otra cosa o el servidor está apagado | Levanta `marga-server` y abre `<url>/api/health`; o sigue en modo local |
| `no hay conexión con el servidor` | Nadie escucha en esa URL | Igual que el anterior; si trabajas solo, `marga init --local` |
| `ya tienes un proyecto con ese nombre en el servidor` | Ya existe un proyecto con ese slug | `marga init --repo ID` (el error te dice el ID) o elige otro nombre |
| `"prod" parece producción y no confirmaste` | Protección de entornos de producción | `marga up --env prod --dry-run` para revisar y luego `--si` |

## Siguiente

- `marga commit "descripción"`: cambia la base con tu herramienta de siempre y Marga
  escribe la migración por ti ([CLI](cli.md#marga-commit-la-migración-se-escribe-sola)).
- `marga ui`: el panel con entornos, migraciones, drift y riesgo.
- Para compartir el historial y ver los entornos en la web, levanta un servidor
  ([servidor](servidor.md)) y vincula el proyecto con `marga login` y `marga link --repo ID`.

## En scripts y CI

`marga init` solo pregunta cuando corre en una terminal. Con cualquier opción, en CI o
con `MARGA_NO_INTERACTIVO=1`, no pregunta nada y se comporta como siempre:

```
marga init --local                      # sin servidor
marga init --crear "Tienda"             # crea el proyecto en el servidor y lo vincula
marga init --repo 3                     # vincula uno que ya existe
```

`marga version` no consulta la red en CI; fuera de CI se apaga con `--sin-red` o
`MARGA_SIN_AVISO_VERSION=1`.
