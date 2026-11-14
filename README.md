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

| Pieza | Dónde | Qué es |
|---|---|---|
| `marga` | `cmd/marga`, `internal/cli` | la CLI (y consola interactiva si la abres sin argumentos) |
| `marga-server` | `cmd/marga-server`, `internal/api`, `internal/store` | API REST + web; guarda el historial en PostgreSQL |
| web | `web/` | el front; va embebido dentro de `marga-server` |
| compartido | `internal/riesgo`, `internal/checksum`, … | análisis de riesgo e identidad de migraciones, igual en CLI y servidor |
| despliegue | `deploy/`, `Dockerfile` | Docker Compose con HTTPS (Caddy) y respaldos diarios |

```
cmd/            marga y marga-server (solo arrancan; la lógica está en internal/)
internal/
  cli/          comandos de la CLI (Cobra)
  proyecto/     la carpeta .marga/ y los archivos de migración
  migrador/     aplicar / revertir / drift en la base del usuario
  introspeccion/ leer el esquema real de PostgreSQL
  generador/    escribir el SQL de `marga commit`
  remoto/       cliente HTTP de la API
  api/ store/ database/ security/ diff/ config/ correo/   el servidor
  riesgo/ checksum/ version/                             compartido
web/            páginas, JS y CSS (Tailwind compilado)
deploy/         producción con Docker
docs/           cli.md, servidor.md, despliegue.md
```

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

Guías: [CLI](docs/cli.md) · [servidor y API](docs/servidor.md) · [despliegue](docs/despliegue.md)

## Pruebas

```bash
go test ./...                                   # unitarias
# Con PostgreSQL: además las de integración (API, merge, CLI de punta a punta, generador…)
MARGA_TEST_DB_URL="postgres://marga:marga@localhost:5432/marga?sslmode=disable" go test ./...
```

Cada prueba de integración crea su propia base temporal y la borra al terminar (el usuario
necesita permiso `CREATEDB`). En GitHub, el workflow **CI** las corre en cada push y pull request.

## Versiones

Al subir una etiqueta, el workflow **Release** publica los ejecutables de `marga` y `marga-server`
para Windows, Linux y macOS (amd64 y arm64) y la imagen `ghcr.io/zara-te05/marga-server`:

```bash
git tag v0.4.0
git push origin v0.4.0
```

`marga version` y `/api/health` muestran la versión de cada binario.
