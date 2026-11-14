# Marga Moon · CLI

Control de versiones para bases de datos. Aplica, revierte y sincroniza migraciones
de PostgreSQL, y las comparte con tu equipo a través del servidor **Marga Sun**.

```
go build -o marga.exe .      # Windows  (en Linux/Mac: go build -o marga .)
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

# 3. Escribir una migración
marga new crear tabla clientes
#   -> migrations/0001_crear_tabla_clientes.up.sql    (el cambio)
#   -> migrations/0001_crear_tabla_clientes.down.sql  (cómo deshacerlo)

# 4. Aplicarla
marga status           # qué está aplicado y qué falta
marga up --dry-run     # ver el SQL sin ejecutarlo
marga up               # aplicar
marga down             # revertir la última

# 5. Compartirla con el equipo
marga push             # sube tus migraciones al servidor
marga pull             # baja las de tus compañeros
marga log              # historial de la rama en el servidor
```

## Qué guarda y dónde

| Archivo | ¿Va a git? | Contenido |
|---|---|---|
| `.marga/config.json` | sí | servidor, ID del proyecto, rama, motor, carpeta de migraciones |
| `.marga/remoto.json` | sí | qué migración local corresponde a qué commit del servidor |
| `.marga/entornos.json` | **no** (`.marga/.gitignore`) | cadenas de conexión con contraseña |
| `migrations/*.sql` | sí | tus migraciones |
| `~/.marga/credenciales.json` | — | token de acceso por servidor (permisos 600). Tu contraseña nunca se guarda |
| tabla `marga_history` en **tu** BD | — | qué se aplicó, cuándo, cuánto tardó, quién y con qué checksum |

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

## Variables útiles (CI)

```bash
MARGA_PASSWORD=...  marga login --email ci@empresa.com     # login sin teclado
marga env add prod '$DATABASE_URL'                         # la URL se lee de la variable al ejecutar
marga up --env prod --si
```

## Limitaciones actuales

- Aplicar migraciones: solo PostgreSQL (`env probar` también funciona con MySQL y SQL Server).
- `push`/`pull` trabajan sobre una rama (`--rama`); todavía no hay merge entre ramas.
- El servidor aún no sabe qué está aplicado en cada entorno (eso es la fase 2).
