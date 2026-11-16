// Package checksum define la identidad de una migración: el sha256 de su SQL normalizado.
//
// La CLI y el servidor usan esta misma función, así que una migración se reconoce
// igual en cualquier rama o entorno aunque la hayan renumerado o la hayan guardado
// con saltos de línea de Windows.
package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Normalizar convierte CRLF en LF y deja exactamente un salto de línea al final.
func Normalizar(sql string) string {
	sql = strings.ReplaceAll(sql, "\r\n", "\n")
	return strings.TrimRight(sql, " \t\n") + "\n"
}

// SQL devuelve el checksum (hex) del SQL normalizado.
func SQL(sql string) string {
	s := sha256.Sum256([]byte(Normalizar(sql)))
	return hex.EncodeToString(s[:])
}
