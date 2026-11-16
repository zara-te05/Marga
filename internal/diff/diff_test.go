package diff

import (
	"strings"
	"testing"
)

func TestLineas(t *testing.T) {
	a := "CREATE TABLE c (\n  id int,\n  nombre text\n);"
	b := "CREATE TABLE c (\n  id int,\n  nombre text,\n  email text\n);"
	ls := Lineas(a, b)
	mas, menos := Contar(ls)
	if mas != 2 || menos != 1 {
		t.Fatalf("+%d -%d, esperaba +2 -1", mas, menos)
	}
	u := Unificado(a, b)
	if !strings.Contains(u, "+  email text") || !strings.Contains(u, "-  nombre text") {
		t.Fatalf("diff unificado inesperado:\n%s", u)
	}
	if m, n := Contar(Lineas(a, a)); m != 0 || n != 0 {
		t.Fatal("textos iguales no tienen cambios")
	}
	if m, n := Contar(Lineas("", b)); m != 5 || n != 0 {
		t.Fatalf("desde vacío: +%d -%d", m, n)
	}
}
