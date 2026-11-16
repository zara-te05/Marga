package cli_test

import (
	"github.com/zara-te05/Marga/internal/vistas/terminal"
	"github.com/zara-te05/Marga/tests/apoyo/clitest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zara-te05/Marga/internal/controladores/cli"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

// El panel sin terminal: se cargan los datos de verdad y se simulan teclas.
func TestPanelUI(t *testing.T) {
	dev := pruebabd.Nueva(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	dir := t.TempDir()
	t.Chdir(dir)
	clitest.DebeCorrer(t, "init")
	clitest.DebeCorrer(t, "env", "add", "dev", dev)
	clitest.DebeCorrer(t, "env", "add", "prod", "postgres://nadie:x@127.0.0.1:1/nada?sslmode=disable", "--compartido")
	clitest.DebeCorrer(t, "new", "crear clientes")
	_ = os.WriteFile(filepath.Join(dir, "migrations", "0001_crear_clientes.up.sql"), []byte("CREATE TABLE clientes (id int PRIMARY KEY);\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "migrations", "0001_crear_clientes.down.sql"), []byte("DROP TABLE clientes;\n"), 0o644)
	clitest.DebeCorrer(t, "up", "--env", "dev")
	clitest.DebeCorrer(t, "new", "borrar clientes")
	_ = os.WriteFile(filepath.Join(dir, "migrations", "0002_borrar_clientes.up.sql"), []byte("DROP TABLE clientes;\n"), 0o644)

	p, err := proyecto.Buscar(".")
	if err != nil {
		t.Fatal(err)
	}
	m, err := cli.NuevoModeloUI(p, terminal.Temas["marga"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Nombres, ",") != "dev,prod" {
		t.Fatalf("orden de entornos: %v", m.Nombres)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	// Cargar dev (como lo hace Init) y aplicar el resultado.
	m.Update(m.Cargar(m.Entorno())())
	vista := ansi.Strip(m.View())
	for _, quiero := range []string{"◆ MARGA", "Entornos", "1 pendiente", "✓ 0001 crear clientes", "○ 0002 borrar clientes", "1 │ DROP TABLE clientes;"} {
		if !strings.Contains(vista, quiero) {
			t.Errorf("falta %q en el panel:\n%s", quiero, vista)
		}
	}
	// Arranca en la pendiente; la pestaña Riesgo muestra el DROP TABLE.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "ALTO") || !strings.Contains(v, "borra-tabla") {
		t.Errorf("la pestaña Riesgo debía mostrar borra-tabla:\n%s", v)
	}
	// u pregunta antes de aplicar; n cancela.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if m.Pregunta == nil || !strings.Contains(m.Pregunta.Texto, "1 migración pendiente en dev") {
		t.Fatalf("u debía preguntar: %+v", m.Pregunta)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.Pregunta != nil {
		t.Fatal("n debía cancelar")
	}
	// Bajar a prod: sin conexión, pero el panel sigue funcionando; en producción se agrega --si.
	m.Foco = cli.FocoEntornos
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(cmd())
	if v := ansi.Strip(m.View()); !strings.Contains(v, "sin conexión") {
		t.Errorf("prod debía verse sin conexión:\n%s", v)
	}
	// La ayuda muestra los terminal.Temas, y t cambia al siguiente.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "bosque") || !strings.Contains(v, "Así se ve el SQL") {
		t.Errorf("la ayuda debía listar los terminal.Temas:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.Tema.Nombre != "noche" || terminal.LeerConfig().Tema != "noche" {
		t.Errorf("t debía pasar a noche y guardarlo: %s / %+v", m.Tema.Nombre, terminal.LeerConfig())
	}
	if args := cli.ConSi("prod", "up", "--env", "prod"); args[len(args)-1] != "--si" {
		t.Errorf("en producción la confirmación del panel debe pasar --si: %v", args)
	}
}
