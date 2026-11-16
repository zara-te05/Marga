package terminal_test

import (
	"github.com/zara-te05/Marga/internal/vistas/terminal"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestResaltadoSQL(t *testing.T) {
	e := terminal.NuevosEstilosSQL(terminal.Temas["marga"])
	sql := "CREATE FUNCTION f(p integer) RETURNS text AS $$ SELECT 'a;b' || p::text -- fin\n$$;"
	out := terminal.ResaltarSQL(sql, e)
	if ansi.Strip(out) != sql {
		t.Fatalf("el resaltado no debe cambiar el texto:\n%q\n%q", ansi.Strip(out), sql)
	}
	// Con colores forzados, cada clase de token lleva su color.
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	e = terminal.NuevosEstilosSQL(terminal.Temas["marga"])
	out = terminal.ResaltarSQL(sql, e)
	for nombre, pieza := range map[string]string{
		"palabra reservada": e.Clave.Render("CREATE"), "tipo": e.Tipo.Render("integer"), "cadena": e.Cadena.Render("'a;b'"),
		"función": e.Funcion.Render("f"), "comentario": e.Comentario.Render("-- fin"), "paréntesis": e.Parentesis[0].Render("("),
		"operador": e.Operador.Render("::"),
	} {
		if !strings.Contains(out, pieza) {
			t.Errorf("falta el color de %s en:\n%q", nombre, out)
		}
	}
	// Paréntesis anidados: un color por nivel.
	out = terminal.ResaltarSQL("((x))", e)
	if !strings.Contains(out, e.Parentesis[0].Render("(")+e.Parentesis[1].Render("(")) {
		t.Errorf("los paréntesis anidados debían tener colores distintos: %q", out)
	}
}

func TestTemaPersonalizado(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MARGA_TEMA", "")
	_ = os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".marga"), 0o700)
	_ = os.WriteFile(terminal.Archivo(), []byte(`{"tema":"bosque","colores":{"clave":"#123456","parentesis":"#111111 #222222"}}`), 0o600)
	tm, err := terminal.Cargar("")
	if err != nil || tm.Nombre != "bosque" || tm.Clave != "#123456" || len(tm.Parentesis) != 2 || tm.Cadena != terminal.Temas["bosque"].Cadena {
		t.Fatalf("%+v %v", tm, err)
	}
	if tm, _ := terminal.Cargar("papel"); tm.Nombre != "papel" || tm.Clave != "#123456" {
		t.Fatalf("--tema gana sobre el archivo y los colores propios se mantienen: %+v", tm)
	}
	if _, err := terminal.Cargar("inventado"); err == nil || !strings.Contains(err.Error(), "atardecer") {
		t.Fatalf("un tema desconocido debe listar los que hay: %v", err)
	}
	if err := terminal.GuardarNombre("mono"); err != nil {
		t.Fatal(err)
	}
	if c := terminal.LeerConfig(); c.Tema != "mono" || c.Colores["clave"] != "#123456" {
		t.Fatalf("guardar el tema no debe borrar los colores propios: %+v", c)
	}
}
