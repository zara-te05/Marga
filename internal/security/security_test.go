package security

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correcta-1234")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$") {
		t.Fatalf("formato inesperado: %s", h)
	}
	if !CheckPassword(h, "correcta-1234") {
		t.Fatal("la contraseña correcta debe validar")
	}
	if CheckPassword(h, "incorrecta") {
		t.Fatal("la incorrecta no debe validar")
	}
	h2, _ := HashPassword("correcta-1234")
	if h == h2 {
		t.Fatal("cada hash debe llevar su propia sal")
	}
	for _, malo := range []string{"", "texto", "pbkdf2-sha256$x$y$z", "md5$1$a$b"} {
		if CheckPassword(malo, "correcta-1234") {
			t.Fatalf("un hash inválido no debe validar: %q", malo)
		}
	}
}

func TestTokens(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if a == b || len(a) < 32 {
		t.Fatalf("tokens débiles o repetidos: %q %q", a, b)
	}
	if HashToken(a) != HashToken(a) || HashToken(a) == HashToken(b) || HashToken(a) == a {
		t.Fatal("HashToken debe ser determinista y no devolver el token")
	}
	if ValidarPassword("corta") == nil || ValidarPassword("suficiente") != nil {
		t.Fatal("la longitud mínima es 8")
	}
}
