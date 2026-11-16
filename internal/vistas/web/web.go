// Package web contiene el front de Marga. Se embebe en el binario del servidor,
// así que marga-server funciona solo, sin copiar carpetas.
// Para editar el front sin recompilar: MARGA_WEB_DIR=./internal/vistas/web
package web

import "embed"

//go:embed index.html pages assets
var FS embed.FS
