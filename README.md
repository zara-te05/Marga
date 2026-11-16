# Marga

**Control de versiones para bases de datos.** Como git, pero para el esquema de tu base:
cada cambio es una migración con su reversión, el historial vive en un servidor compartido,
y Marga sabe qué tiene aplicado cada entorno (dev, staging, prod), detecta los cambios
hechos a mano y avisa del SQL peligroso antes de que llegue a producción.

```
marga commit agregar telefono a clientes   # escribe el .up.sql y .down.sql a partir de tu base dev
marga push                                 # lo comparte con el equipo
marga up --env staging                     # lo aplica, con bloqueo, transacción y checksum
```

## Qué hay en este repositorio

El código sigue capas al estilo MVC: los **modelos** son la lógica pura (sin base de datos ni red),
**datos** habla con bases, archivos y la API, los **controladores** reciben las órdenes (comandos de la
CLI y rutas HTTP) y las **vistas** son lo que se ve (la web y los colores de la terminal).

| Pieza | Dónde | Qué es |
|---|---|---|
| `marga` | `cmd/marga`, `internal/controladores/cli` | la CLI (`marga ui` abre el panel interactivo y `marga sql` la consola de SQL) |
| `marga-server` | `cmd/marga-server`, `internal/controladores/api`, `internal/datos/store` | API REST + web; guarda el historial en PostgreSQL |
| web | `internal/vistas/web` | el front; va embebido dentro de `marga-server` |
| despliegue | `deploy/`, `Dockerfile` | Docker Compose con HTTPS (Caddy) y respaldos diarios |

```
cmd/                    marga y marga-server (solo arrancan)
internal/
  modelos/              lógica pura, igual en CLI y servidor
    riesgo/             análisis del SQL peligroso
    esquema/            el modelo del esquema (tablas, vistas, funciones…) y cómo compararlo
    diff/ checksum/ version/
  datos/                bases de datos, archivos y red
    motor/              lo que cambia de un motor a otro (interfaz Motor)
      postgres/         leer el esquema, escribir el SQL de `marga commit`, bloqueos
    migrador/           aplica, revierte y detecta drift en la base del usuario
    ramabd/             una base por rama de git
    proyecto/           la carpeta .marga/ y los archivos de migración
    gitlocal/           el repositorio git del proyecto
    remoto/             cliente HTTP de la API
    store/ database/    la base del servidor y sus migraciones
  servicios/            security/ correo/ config/
  controladores/
    cli/                comandos de la CLI (Cobra), panel `marga ui` y consola `marga sql`
    api/                rutas HTTP
  vistas/
    terminal/           temas de color y resaltado de SQL
    web/                páginas, JS y CSS (Tailwind compilado)
tests/
  unit/                 sin base de datos
  integracion/          con PostgreSQL (y git)
  e2e/                  la CLI completa contra un servidor real
  apoyo/                ayudas compartidas por las pruebas
deploy/                 producción con Docker
docs/                   cli.md, servidor.md, despliegue.md, estado.md
```

## Instalar

```
winget install zara-te05.Marga         # Windows (o Scoop: ver docs/cli.md)
brew install zara-te05/tap/marga       # macOS y Linux
```

Y luego el [inicio rápido](docs/inicio-rapido.md): de cero a tu primera migración en 5 minutos,
sin servidor (`marga init` te pregunta lo necesario).

## Empezar (desarrollo)

Requisitos: **Go** (la versión de `go.mod`) y **PostgreSQL** (o Docker).

```powershell
# 1. Base de datos para el servidor
docker compose -f docker-compose.dev.yml up -d

# 2. Servidor con datos de demostración → http://localhost:8080  (demo@marga.dev / marga1234)
go run ./cmd/marga-server -seed

# 3. La CLI
go build -o marga.exe ./cmd/marga
.\marga.exe login
.\marga.exe init --crear "Mi proyecto"
```

Guías: [inicio rápido](docs/inicio-rapido.md) · [CLI](docs/cli.md) · [servidor y API](docs/servidor.md) · [despliegue](docs/despliegue.md) · [estado: hecho y pendiente](docs/estado.md)

## Pruebas

Todas viven en `tests/`, separadas por tipo:

```powershell
go test ./tests/unit/...                        # unitarias, sin base de datos

# integración y e2e: necesitan PostgreSQL (el de docker-compose.dev.yml sirve)
$env:MARGA_TEST_DB_URL = "postgres://marga:marga@localhost:5432/marga?sslmode=disable"
go test -count=1 ./tests/integracion/...
go test -count=1 ./tests/e2e/...
go test -count=1 ./...                          # todo
```

Sin `MARGA_TEST_DB_URL`, las que necesitan PostgreSQL se saltan; cada una crea su propia base temporal
y la borra al terminar (el usuario necesita permiso `CREATEDB`). En Linux/macOS: `make probar`,
`make probar-integracion`, `make probar-e2e` o `make probar-todo`. El CI (`.github/workflows/ci.yml`)
corre las tres en cada push y pull request.

## Versiones

Al subir una etiqueta, el workflow **Release** publica los ejecutables de `marga` y `marga-server`
para Windows, Linux y macOS (amd64 y arm64), la CLI en Scoop, winget y Homebrew (ver el
comentario al inicio de `.goreleaser.yaml` para lo que hay que preparar una vez) y la imagen
`ghcr.io/zara-te05/marga-server`:

```bash
git tag v0.4.0
git push origin v0.4.0
```

`marga version` y `/api/health` muestran la versión de cada binario; `marga version` además
avisa cuando hay una más nueva.
