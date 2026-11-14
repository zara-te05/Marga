# Marga Sun · Servidor web + Base de datos

Backend en **Go** (solo librería estándar + `lib/pq`, el mismo driver que usa Marga Moon) sobre **PostgreSQL**.
Sirve el front de `Marga Sun` y expone una API REST para que la web y la CLI compartan el mismo historial de migraciones.

```
server/
├── main.go                         # arranque: conecta, migra, (seed), levanta HTTP
├── .env.example                    # configuración (cópialo como .env)
├── docker-compose.yml              # PostgreSQL 16 para desarrollo
└── internal/
    ├── config/      config.go      # variables de entorno / .env
    ├── database/    database.go    # pool de conexiones + migrador embebido
    │   └── migrations/0001_esquema_inicial.sql   # tu diagrama ER en SQL
    ├── security/    security.go    # PBKDF2 para contraseñas, tokens de sesión
    ├── diff/        diff.go        # diff línea a línea (LCS) del DDL
    ├── store/                      # TODO el SQL vive aquí
    │   ├── store.go                # usuarios, sesiones, equipos, auditoría
    │   ├── repos.go                # repositorios, permisos, colaboradores, ramas
    │   ├── commits.go              # historial, crear commit, revert, búsqueda
    │   ├── diffs.go                # diff entre commits + cache_diff
    │   └── seed.go                 # datos de demostración
    └── api/
        ├── api.go                  # router, middlewares, sesión, front estático
        └── handlers.go             # endpoints
```

## Puesta en marcha (Windows)

1. **PostgreSQL** — una de dos:
   - Con Docker Desktop: `cd server` → `docker compose up -d`
   - Instalado: crea usuario y BD en `psql`:
     ```sql
     CREATE ROLE marga LOGIN PASSWORD 'marga';
     CREATE DATABASE marga OWNER marga;
     ```
2. **Configuración**: copia `.env.example` como `.env` y ajusta `MARGA_DB_URL` si hace falta.
3. **Arrancar** (Go 1.24 o superior):
   ```powershell
   cd "Marga Sun\server"
   go run . -seed
   ```
   La primera vez crea todas las tablas (`schema_migrations` registra cuáles se aplicaron)
   y `-seed` añade datos demo. Después basta con `go run .`
4. Abre **http://localhost:8080** → entra con `demo@marga.dev` / `marga1234`
   (o crea tu cuenta; el **primer** usuario registrado es `admin`).

> Si abres los HTML con doble clic (`file://`) siguen funcionando en modo demo, sin datos reales.

Para compilar un ejecutable: `go build -o marga-sun.exe .`

## Modelo de datos

Es tu diagrama traducido a PostgreSQL, con estos ajustes:

| Cambio | Por qué |
|---|---|
| `ususario_id`, `reposirorio_id` → `usuario_id`, `repositorio_id` | typos del diagrama |
| `commit.repositorio_id` (nuevo) | en git un commit pertenece al repo; la rama solo **apunta** a un commit (`branch.ultimo_commit_hash`). `commit.branch_id` queda como "rama donde se creó". |
| `commit.sql_up` / `sql_down` (nuevos) | el contenido real de la migración y su reversión |
| `commit.padre_merge_hash` (nuevo) | segundo padre, para merges en el futuro |
| `hash_id` = SHA-256 de (padre, repo, autor, fecha, mensaje, SQL) | como git: si algo cambia, el hash cambia |
| `cache_diff`: PK `(commit_origen, commit_destino)` y `ttl` → `expira_en` | evita la clave concatenada `claveOrigen_Ddestino_id` |
| `sesion.sesion_id` = SHA-256 del token | si alguien lee la tabla no puede robar sesiones |
| `indice_busqueda_commit.documento` (tsvector + GIN) y trigger | búsqueda de texto completo en español; se llena sola al crear commits |
| `snapshot_esquema.ddl` (nuevo) | DDL completo de la tabla en ese commit; `diff` se calcula solo contra el padre |
| `repositorio.slug`, `motor`, `entorno` (nuevos) | los usa el front (tarjetas, filtros) y la CLI |
| `CHECK` en `rol`, `permiso`, `visibilidad`, `motor`, `entorno` | valores válidos garantizados por la BD |

**Tablas agregadas después del diagrama:** `token_api` (tokens personales de la CLI),
`entorno_bd` (cada base reportada por la CLI: esquema real en JSON y drift) y
`migracion_aplicada` (qué migraciones tiene cada entorno, enlazadas a su commit por hash o por checksum del SQL),
`aprobacion` (revisiones de cada commit) y `commit.checksum` (sha256 del SQL: la identidad de una migración
entre ramas). Donde la API recibe un commit (`{ref}`) acepta hash, prefijo de hash o nombre de la migración.
Cada commit incluye su análisis de riesgo (`internal/riesgo`, el mismo paquete que usa la CLI).

**Permisos por repositorio:** propietario o admin global → `admin`; colaborador → su `permiso`
(`lectura` / `escritura` / `admin`); repo `publico` → `lectura`; resto → no lo ve (404).

## API

Autenticación por cookie `HttpOnly` (navegador) o `Authorization: Bearer <token>` (CLI).
Las peticiones que modifican datos deben enviar `Content-Type: application/json`.

| Método | Ruta | Permiso | Descripción |
|---|---|---|---|
| GET | `/api/health` | — | estado de la BD |
| POST | `/api/auth/register` | — | `{nombre, email, password, equipo_id?}` |
| POST | `/api/auth/login` | — | `{email, password}` → cookie + `token` |
| POST | `/api/auth/logout` | sesión | |
| GET | `/api/auth/me` | sesión | usuario actual |
| GET / POST | `/api/tokens` | sesión | tokens de acceso personales; `POST {nombre}` devuelve el token **una sola vez** |
| DELETE | `/api/tokens/{id}` | sesión | revoca un token (`/api/tokens/actual` revoca el que hace la petición) |
| GET / POST | `/api/equipos` | — / admin | |
| GET / POST | `/api/repos` | sesión | listar / crear `{nombre, slug?, descripcion, visibilidad, motor, entorno}` |
| GET / PATCH / DELETE | `/api/repos/{id}` | lectura / admin / admin | |
| GET / POST | `/api/repos/{id}/colaboradores` | lectura / admin | `{email, permiso}` |
| DELETE | `/api/repos/{id}/colaboradores/{uid}` | admin | |
| GET / POST | `/api/repos/{id}/branches` | lectura / escritura | `{nombre, desde}` (`desde` = rama o hash) |
| DELETE | `/api/repos/{id}/branches/{nombre}` | escritura | no permite borrar `main` |
| GET | `/api/repos/{id}/commits?branch=main&limit=50&offset=0` | lectura | `git log` de la rama |
| POST | `/api/repos/{id}/commits` | escritura | crear commit (ver abajo) |
| GET | `/api/repos/{id}/commits/{hash}` | lectura | acepta prefijo de 4+ caracteres |
| GET | `/api/repos/{id}/commits/{hash}/diff` | lectura | cambios contra su padre |
| POST | `/api/repos/{id}/commits/{hash}/revert` | escritura | crea un commit inverso `{branch}` |
| GET | `/api/repos/{id}/diff?from=main&to=feature/x` | lectura | ramas o hashes; se cachea en `cache_diff` |
| GET | `/api/repos/{id}/buscar?q=wallets` | lectura | texto completo o prefijo de hash |
| GET | `/api/repos/{id}/esquema?ref=main` | lectura | tablas y DDL en una rama o commit |
| GET | `/api/repos/{id}/entornos?rama=main` | lectura | entornos reportados: aplicadas, pendientes, drift |
| GET | `/api/repos/{id}/entornos/{nombre}` | lectura | detalle: lista de aplicadas y pendientes, esquema real |
| PUT | `/api/repos/{id}/entornos/{nombre}` | escritura | lo usa la CLI para reportar el estado de un entorno |
| DELETE | `/api/repos/{id}/entornos/{nombre}` | admin | olvida un entorno |
| PATCH | `/api/repos/{id}/entornos/{nombre}` | admin | `{requiere_aprobacion}`: protege el entorno |
| POST / DELETE | `/api/repos/{id}/commits/{ref}/revisiones` | escritura | `{estado: aprobado\|cambios, comentario}`; nadie aprueba lo suyo |
| POST | `/api/repos/{id}/fusionar` | escritura | `{desde, hacia, simular, forzar}` → fast_forward / copia / conflicto |
| GET | `/api/repos/{id}/auditoria` | admin | |

### Conectar la CLI (Marga Moon)

La CLI de Marga Moon ya usa esta API (ver su README):

```bash
marga login --servidor http://localhost:8080   # crea un token personal "CLI · <equipo>"
marga init --repo 1
marga push / pull / log
```

Para CI crea un token en **Cuenta → Tokens de acceso** y úsalo como `Authorization: Bearer marga_pat_…`.
Los tokens se guardan como SHA-256 en `token_api`; el token en claro solo se muestra al crearlo.

## Probado

Contra PostgreSQL 16: migración y seed, login/registro/logout, permisos (propietario, colaborador,
público, sin acceso), historial por rama, diff por commit y entre ramas (incl. caché), revert,
búsqueda, conflicto 409 por `padre_esperado`, validaciones y protección CSRF; y el front
(login → proyectos → crear proyecto → commits → diff → cambio de rama → búsqueda) en navegador.
