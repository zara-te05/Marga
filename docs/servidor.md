# `marga-server` · web, API y base de datos

Backend en **Go** (librería estándar + `lib/pq`) sobre **PostgreSQL**. Sirve el front
(embebido en el binario) y expone una API REST para que la web y la CLI compartan el mismo
historial de migraciones.

## Puesta en marcha (desarrollo, Windows)

1. **PostgreSQL** — una de dos:
   - Con Docker Desktop, desde la raíz del repositorio: `docker compose -f docker-compose.dev.yml up -d`
   - Instalado: crea usuario y BD en `psql`:
     ```sql
     CREATE ROLE marga LOGIN PASSWORD 'marga';
     CREATE DATABASE marga OWNER marga;
     ```
2. **Configuración** (opcional): copia `.env.example` como `.env` y ajusta `MARGA_DB_URL`.
3. **Arrancar** (Go 1.24 o superior), desde la raíz del repositorio:
   ```powershell
   go run ./cmd/marga-server -seed
   ```
   La primera vez crea todas las tablas (`schema_migrations` registra cuáles se aplicaron)
   y `-seed` añade datos demo. Después basta con `go run ./cmd/marga-server`.
4. Abre **http://localhost:8080** → entra con `demo@marga.dev` / `marga1234`
   (o crea tu cuenta; el **primer** usuario registrado es `admin`).

Para editar el front sin recompilar: `MARGA_WEB_DIR=./web`. Si cambias clases de Tailwind,
regenera el CSS con `cd web && npm install && npm run css` (el CSS compilado va en el repositorio,
así que para compilar Go no hace falta Node).

Para producción ver [despliegue.md](despliegue.md).

## Configuración

| Variable | Por defecto | Descripción |
|---|---|---|
| `MARGA_DB_URL` | `postgres://marga:marga@localhost:5432/marga?sslmode=disable` | base de datos del servidor |
| `MARGA_ADDR` | `:8080` | dirección donde escucha |
| `MARGA_SESSION_TTL` / `MARGA_DIFF_TTL` | `72h` / `24h` | duración de sesiones y de diffs en caché |
| `MARGA_SECURE_COOKIE` | `false` | `true` detrás de HTTPS (cookie `Secure` y HSTS) |
| `MARGA_URL_PUBLICA` | `http://localhost:8080` | URL con la que se abre Marga; se usa en los enlaces de correo |
| `MARGA_CONFIAR_PROXY` | `false` | `true` detrás de Caddy/nginx: la IP real se lee de `X-Forwarded-For` |
| `MARGA_SMTP_HOST`, `_PUERTO`, `_USUARIO`, `_PASSWORD`, `_DE` | — | correo para restablecer contraseñas (587 STARTTLS o 465 TLS) |
| `MARGA_WEB_DIR` | — | servir el front desde disco en lugar del embebido |

Banderas: `-seed` (datos demo), `-migrate` (solo migra la BD), `-restablecer EMAIL` (imprime un
enlace para que esa persona cambie su contraseña), `-version`.

## Seguridad

- Contraseñas con **PBKDF2-SHA256** (600 000 iteraciones). Sesiones y tokens se guardan como SHA-256.
- **Límite de intentos**: 8 inicios de sesión fallidos por email y 40 por IP cada 15 min (responde 429);
  también se limitan el registro y la recuperación de contraseña.
- **Recuperar contraseña**: enlace de un solo uso que vale una hora. Sin SMTP, el enlace se escribe
  en el log del servidor (o se genera con `-restablecer`). La respuesta es la misma exista o no la cuenta.
- **Cambiar contraseña** en *Cuenta*; cierra las demás sesiones.
- **CSRF**: las peticiones que modifican datos deben ser JSON o llevar `Authorization`.
- Cabeceras: `Content-Security-Policy`, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy` y HSTS tras HTTPS.
- El servidor **nunca** guarda cadenas de conexión de las bases de los usuarios: solo lo que la CLI reporta.

## Modelo de datos

Es el diagrama ER original traducido a PostgreSQL (`internal/database/migrations/`), con estos ajustes:

| Cambio | Por qué |
|---|---|
| `ususario_id`, `reposirorio_id` → `usuario_id`, `repositorio_id` | typos del diagrama |
| `commit.repositorio_id` (nuevo) | en git un commit pertenece al repo; la rama solo **apunta** a un commit (`branch.ultimo_commit_hash`). `commit.branch_id` queda como "rama donde se creó". |
| `commit.sql_up` / `sql_down` (nuevos) | el contenido real de la migración y su reversión |
| `commit.padre_merge_hash` (nuevo) | segundo padre, para fusiones |
| `hash_id` = SHA-256 de (padre, repo, autor, fecha, mensaje, SQL) | como git: si algo cambia, el hash cambia |
| `cache_diff`: PK `(commit_origen, commit_destino)` y `ttl` → `expira_en` | evita la clave concatenada `claveOrigen_Ddestino_id` |
| `sesion.sesion_id` = SHA-256 del token | si alguien lee la tabla no puede robar sesiones |
| `indice_busqueda_commit.documento` (tsvector + GIN) y trigger | búsqueda de texto completo en español |
| `snapshot_esquema.ddl` (nuevo) | DDL completo de la tabla en ese commit |
| `repositorio.slug`, `motor`, `entorno` (nuevos) | los usan el front y la CLI |
| `CHECK` en `rol`, `permiso`, `visibilidad`, `motor`, `entorno` | valores válidos garantizados por la BD |

**Tablas agregadas después del diagrama:** `token_api` (tokens personales de la CLI),
`entorno_bd` (cada base reportada por la CLI: esquema real en JSON y drift),
`migracion_aplicada` (qué migraciones tiene cada entorno, enlazadas a su commit por hash o checksum),
`aprobacion` (revisiones de cada commit), `restablecimiento` (enlaces para cambiar la contraseña)
y `commit.checksum` (sha256 del SQL: la identidad de una migración entre ramas, el mismo cálculo
que hace la CLI en `internal/checksum`).

**Permisos por repositorio:** propietario o admin global → `admin`; colaborador → su `permiso`
(`lectura` / `escritura` / `admin`); repo `publico` → `lectura`; resto → no lo ve (404).

## API

Autenticación por cookie `HttpOnly` (navegador) o `Authorization: Bearer <token>` (CLI).
Las peticiones que modifican datos deben enviar `Content-Type: application/json`.
Donde la API recibe un commit (`{ref}`) acepta hash, prefijo de hash o nombre de la migración.

| Método | Ruta | Permiso | Descripción |
|---|---|---|---|
| GET | `/api/health` | — | estado de la BD y versión |
| POST | `/api/auth/register` | — | `{nombre, email, password, equipo_id?}` |
| POST | `/api/auth/login` | — | `{email, password}` → cookie + `token` |
| POST | `/api/auth/logout` | sesión | |
| GET | `/api/auth/me` | sesión | usuario actual |
| POST | `/api/auth/recuperar` | — | `{email}` → envía un enlace para restablecer |
| POST | `/api/auth/restablecer` | — | `{token, password}` → cambia la contraseña e inicia sesión |
| POST | `/api/auth/password` | sesión (no token) | `{actual, nueva}` |
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
| POST | `/api/repos/{id}/commits` | escritura | `{branch, mensaje, sql_up, sql_down, padre_esperado?, snapshots?}` |
| GET | `/api/repos/{id}/commits/{ref}` | lectura | incluye riesgo, revisiones y entornos donde está aplicado |
| GET | `/api/repos/{id}/commits/{ref}/diff` | lectura | cambios contra su padre |
| POST | `/api/repos/{id}/commits/{ref}/revert` | escritura | crea un commit inverso `{branch}` |
| POST / DELETE | `/api/repos/{id}/commits/{ref}/revisiones` | escritura | `{estado: aprobado\|cambios, comentario}`; nadie aprueba lo suyo (403) |
| GET | `/api/repos/{id}/diff?from=main&to=feature/x` | lectura | ramas o hashes; se cachea en `cache_diff` |
| GET | `/api/repos/{id}/buscar?q=wallets` | lectura | texto completo o prefijo de hash |
| GET | `/api/repos/{id}/esquema?ref=main` | lectura | tablas y DDL en una rama o commit |
| GET | `/api/repos/{id}/entornos?rama=main` | lectura | entornos reportados: aplicadas, pendientes, drift |
| GET | `/api/repos/{id}/entornos/{nombre}` | lectura | detalle y esquema real |
| PUT | `/api/repos/{id}/entornos/{nombre}` | escritura | lo usa la CLI para reportar un entorno |
| DELETE | `/api/repos/{id}/entornos/{nombre}` | admin | olvida un entorno |
| PATCH | `/api/repos/{id}/entornos/{nombre}` | admin | `{requiere_aprobacion}`: protege el entorno |
| POST | `/api/repos/{id}/fusionar` | escritura | `{desde, hacia, simular, forzar}` → al_dia / fast_forward / copia / conflicto |
| GET | `/api/repos/{id}/auditoria` | admin | |

Para CI crea un token en **Cuenta → Tokens de acceso**: la CLI lo lee de `MARGA_TOKEN`; para
llamar a la API directamente usa `Authorization: Bearer marga_pat_…`.
