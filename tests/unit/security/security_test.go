package security_test

import (
	"github.com/zara-te05/Marga/internal/servicios/security"
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	h, err := security.HashPassword("correcta-1234")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$") {
		t.Fatalf("formato inesperado: %s", h)
	}
	if !security.CheckPassword(h, "correcta-1234") {
		t.Fatal("la contraseña correcta debe validar")
	}
	if security.CheckPassword(h, "incorrecta") {
		t.Fatal("la incorrecta no debe validar")
	}
	h2, _ := security.HashPassword("correcta-1234")
	if h == h2 {
		t.Fatal("cada hash debe llevar su propia sal")
	}
	for _, malo := range []string{"", "texto", "pbkdf2-sha256$x$y$z", "md5$1$a$b"} {
		if security.CheckPassword(malo, "correcta-1234") {
			t.Fatalf("un hash inválido no debe validar: %q", malo)
		}
	}
}

func TestTokens(t *testing.T) {
	a, _ := security.NewToken()
	b, _ := security.NewToken()
	if a == b || len(a) < 32 {
		t.Fatalf("tokens débiles o repetidos: %q %q", a, b)
	}
	if security.HashToken(a) != security.HashToken(a) || security.HashToken(a) == security.HashToken(b) || security.HashToken(a) == a {
		t.Fatal("HashToken debe ser determinista y no devolver el token")
	}
	if security.ValidarPassword("corta") == nil || security.ValidarPassword("suficiente") != nil {
		t.Fatal("la longitud mínima es 8")
	}
}
