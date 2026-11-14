# Desplegar Marga en un servidor

Lo necesario: una máquina Linux con **Docker** y un **dominio** que apunte a ella (registro A),
con los puertos 80 y 443 abiertos. Todo lo demás está en `deploy/`:

| Servicio | Qué hace |
|---|---|
| `db` | PostgreSQL 16 con un volumen persistente |
| `marga` | `marga-server` (imagen `ghcr.io/zara-te05/marga-server`, la publica el workflow de release) |
| `caddy` | HTTPS automático con Let's Encrypt (y lo renueva) delante de Marga |
| `respaldo` | `pg_dump` diario en `deploy/respaldos/`, conserva 14 días |

## Pasos

```bash
git clone https://github.com/zara-te05/Marga.git && cd Marga/deploy
cp .env.ejemplo .env
nano .env                       # MARGA_DOMINIO, POSTGRES_PASSWORD (larga) y, si quieres, SMTP
docker compose up -d
docker compose logs -f marga    # "✓ Marga v… en http://localhost:8080"
```

Abre `https://tu-dominio` y crea tu cuenta: **la primera cuenta es administradora**.

¿Todavía no hay una release publicada? En `docker-compose.yml` cambia la línea
`image: ghcr.io/…` por `build: ..` y Docker compila la imagen con el `Dockerfile` del repositorio.

## Actualizar

```bash
docker compose pull marga && docker compose up -d marga
```

Las migraciones de la base del servidor se aplican solas al arrancar.

## Respaldos

- Cada 24 h se crea `deploy/respaldos/marga-AAAAMMDD-HHMMSS.dump`. **Cópialos fuera de la máquina**
  (otro disco, S3, Drive): un respaldo en el mismo servidor no te salva si se pierde el servidor.
- Respaldo inmediato: `docker compose restart respaldo`.
- Restaurar (reemplaza los datos actuales): `./restaurar.sh respaldos/marga-20261113-030000.dump`

## Contraseñas olvidadas

Con SMTP configurado, "¿Olvidaste tu contraseña?" envía el enlace por correo. Sin SMTP:

```bash
docker compose exec marga /marga-server -restablecer persona@correo.com
```

imprime un enlace de un solo uso (vale 1 hora) para dárselo a esa persona.

## Sin Docker

Descarga `marga-server` de *Releases*, crea la base en PostgreSQL y ejecútalo con las variables de
[servidor.md](servidor.md#configuración). Ponle delante un proxy con HTTPS (Caddy, nginx) y usa
`MARGA_SECURE_COOKIE=true`, `MARGA_CONFIAR_PROXY=true` y `MARGA_URL_PUBLICA=https://…`.
