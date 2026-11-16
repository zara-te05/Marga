package cli_test

import (
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/controladores/cli"
)

func TestAutocompletadoSQL(t *testing.T) {
	nombres := []string{"cliente_id", "clientes", "nombre"}
	if got := cli.Completar("cli", nombres); strings.Join(got, ",") != "cliente_id,clientes" {
		t.Fatalf("cli → %v", got)
	}
	if got := cli.Completar("sel", nombres); len(got) != 1 || got[0] != "select" {
		t.Fatalf("en minúsculas, la palabra clave también: %v", got)
	}
	if got := cli.Completar("SEL", nombres); len(got) != 1 || got[0] != "SELECT" {
		t.Fatalf("SEL → %v", got)
	}
	if p := cli.PrefijoComun([]string{"cliente_id", "clientes"}); p != "cliente" {
		t.Fatalf("prefijo común: %q", p)
	}
	h := cli.AgregarHistorial(nil, "select 1;")
	h = cli.AgregarHistorial(h, "select 1;")
	if len(h) != 1 {
		t.Fatalf("no se repiten seguidas: %v", h)
	}
	for _, s := range []string{"select 1;", `\d`, "salir"} {
		if !cli.Completo(s) {
			t.Errorf("%q debía estar completa", s)
		}
	}
	if cli.Completo("select 1") {
		t.Error("sin ; no se ejecuta")
	}
}
