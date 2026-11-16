#!/bin/sh
# Respaldo diario de la base de Marga (lo ejecuta el servicio "respaldo" de docker compose).
# Formato custom de pg_dump: se restaura con deploy/restaurar.sh.
set -eu
DIAS="${RESPALDOS_DIAS:-14}"
mkdir -p /respaldos
while true; do
	ARCHIVO="/respaldos/marga-$(date +%Y%m%d-%H%M%S).dump"
	if pg_dump --format=custom --file="$ARCHIVO.tmp"; then
		mv "$ARCHIVO.tmp" "$ARCHIVO"
		echo "$(date -Iseconds) respaldo listo: $(basename "$ARCHIVO") ($(du -h "$ARCHIVO" | cut -f1))"
	else
		rm -f "$ARCHIVO.tmp"
		echo "$(date -Iseconds) ✗ falló el respaldo" >&2
	fi
	find /respaldos -name 'marga-*.dump' -mtime +"$DIAS" -delete
	sleep 86400
done
