package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/controladores/cli"
	"github.com/zara-te05/Marga/tests/apoyo/clitest"
)

// Sin terminal (como en CI), `marga init` sin opciones no pregunta: queda en modo local.
func TestInitSinTerminalEsLocal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	out := clitest.DebeCorrer(t, "init")
	if !strings.Contains(out, "modo local") {
		t.Fatalf("init debía decir que es modo local:\n%s", out)
	}
	var cfg struct {
		Nombre   string `json:"nombre"`
		Servidor string `json:"servidor"`
	}
	b, err := os.ReadFile(filepath.Join(dir, ".marga", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(b, &cfg)
	if cfg.Servidor != "" || cfg.Nombre != filepath.Base(dir) {
		t.Fatalf("config: %+v", cfg)
	}
	// Volver a iniciarlo dice qué hacer.
	clitest.DebeFallar(t, "marga status", "init")
}

func TestModoLocalPushPull(t *testing.T) {
	t.Chdir(t.TempDir())
	clitest.DebeCorrer(t, "init", "--local")
	for _, cmd := range []string{"push", "pull"} {
		out := clitest.DebeCorrer(t, cmd) // no es un error: no hay nada que hacer
		if !strings.Contains(out, "no usa servidor") || !strings.Contains(out, "marga_history") {
			t.Fatalf("marga %s en modo local:\n%s", cmd, out)
		}
	}
	clitest.DebeFallar(t, "marga link --repo ID", "log")
	clitest.DebeFallar(t, "no usa servidor", "branch")
}

func TestInitLocalNoVaConServidor(t *testing.T) {
	t.Chdir(t.TempDir())
	clitest.DebeFallar(t, "--local no va con", "init", "--local", "--repo", "3")
	clitest.DebeFallar(t, "no van juntos", "init", "--repo", "3", "--crear", "X")
}

func TestErroresConPrueba(t *testing.T) {
	t.Chdir(t.TempDir())
	// Sin proyecto.
	if _, err := clitest.Correr(t, "status"); err == nil || !strings.Contains(cli.Sugerencia(err), "marga init") {
		t.Fatalf("status sin proyecto: %v / %q", err, cli.Sugerencia(err))
	}
	clitest.DebeCorrer(t, "init", "--local")
	// Sin entornos.
	_, err := clitest.Correr(t, "up")
	if err == nil || !strings.Contains(cli.Sugerencia(err), "marga env add dev") {
		t.Fatalf("up sin entornos: %v / %q", err, cli.Sugerencia(err))
	}
	// Entorno que no existe.
	clitest.DebeCorrer(t, "env", "add", "dev", "postgres://x:y@127.0.0.1:1/nada?sslmode=disable")
	clitest.DebeFallar(t, "marga env ls", "up", "--env", "prod")
	// Base que no responde: qué revisar.
	clitest.DebeFallar(t, "marga env probar dev", "env", "probar", "dev")
	// Comando que no existe.
	if _, err := clitest.Correr(t, "inventado"); err == nil || !strings.Contains(cli.Sugerencia(err), "--help") {
		t.Fatalf("comando inventado: %v / %q", err, cli.Sugerencia(err))
	}
}
