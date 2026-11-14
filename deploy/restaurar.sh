#!/bin/sh
# Restaura un respaldo sobre la base de Marga. ¡Reemplaza los datos actuales!
#   cd deploy && ./restaurar.sh respaldos/marga-20261113-030000.dump
set -eu
if [ $# -ne 1 ] || [ ! -f "$1" ]; then
	echo "uso: $0 respaldos/marga-AAAAMMDD-HHMMSS.dump" >&2
	exit 1
fi
printf "Esto reemplaza TODOS los datos actuales de Marga con %s. Escribe 'si' para seguir: " "$1"
read -r r
[ "$r" = "si" ] || { echo "cancelado"; exit 1; }
docker compose stop marga
docker compose exec -T db pg_restore --clean --if-exists --no-owner -U marga -d marga < "$1"
docker compose start marga
echo "✓ restaurado"
