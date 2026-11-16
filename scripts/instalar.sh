#!/bin/sh
# Instala (o actualiza) marga en Linux o macOS sin Homebrew:
#
#   curl -fsSL https://raw.githubusercontent.com/zara-te05/Marga/main/scripts/instalar.sh | sh
#
# Descarga el último release a ~/.local/bin (o $MARGA_DESTINO). Si hay otro marga
# antes en el PATH, te lo dice.
set -eu

repo="zara-te05/Marga"
destino="${MARGA_DESTINO:-$HOME/.local/bin}"

case "$(uname -s)" in
  Linux) so=linux ;;
  Darwin) so=darwin ;;
  *) echo "Sistema no soportado: $(uname -s). Descarga marga de https://github.com/$repo/releases" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arq=amd64 ;;
  arm64 | aarch64) arq=arm64 ;;
  *) echo "Arquitectura no soportada: $(uname -m)" >&2; exit 1 ;;
esac

etiqueta=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
[ -n "$etiqueta" ] || { echo "No pude saber cuál es la última versión" >&2; exit 1; }
version=${etiqueta#v}
archivo="marga_${version}_${so}_${arq}.tar.gz"

echo "Instalando marga $etiqueta en $destino…"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "https://github.com/$repo/releases/download/$etiqueta/$archivo" -o "$tmp/$archivo"
tar -xzf "$tmp/$archivo" -C "$tmp" marga
mkdir -p "$destino"
install -m 0755 "$tmp/marga" "$destino/marga"
[ "$so" = darwin ] && xattr -d com.apple.quarantine "$destino/marga" 2>/dev/null || true

case ":$PATH:" in
  *":$destino:"*) ;;
  *) echo "Agrega $destino a tu PATH, por ejemplo: echo 'export PATH=\"$destino:\$PATH\"' >> ~/.profile" ;;
esac
primero=$(command -v marga 2>/dev/null || true)
if [ -n "$primero" ] && [ "$primero" != "$destino/marga" ]; then
  echo "Aviso: la terminal usa primero otro marga: $primero" >&2
  echo "prueba: bórralo (o pon $destino antes en el PATH) y corre: marga version" >&2
fi
"$destino/marga" version --sin-red
echo "Listo. Siguiente paso: marga init"
