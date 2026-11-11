// Package security: hash de contraseñas (PBKDF2-SHA256, librería estándar)
// y tokens de sesión aleatorios.
package security

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	iteraciones = 600_000 // recomendación OWASP para PBKDF2-HMAC-SHA256
	saltLen     = 16
	keyLen      = 32
)

var b64 = base64.RawStdEncoding

// HashPassword devuelve "pbkdf2-sha256$<iter>$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iteraciones, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iteraciones, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// CheckPassword compara en tiempo constante.
func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[2])
	want, err2 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken genera un token opaco de 256 bits (lo que recibe el cliente).
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken es lo que se guarda en sesion.sesion_id.
func HashToken(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

var ErrPasswordDebil = errors.New("la contraseña debe tener al menos 8 caracteres")

func ValidarPassword(p string) error {
	if len([]rune(p)) < 8 {
		return ErrPasswordDebil
	}
	return nil
}
