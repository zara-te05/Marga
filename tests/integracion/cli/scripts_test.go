package cli_test

import (
	"github.com/zara-te05/Marga/internal/controladores/cli"
	"github.com/zara-te05/Marga/tests/apoyo/clitest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

func TestRepetiblesSeedsYSQL(t *testing.T) {
	dev, staging := pruebabd.Nueva(t), pruebabd.Nueva(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MARGA_SERVIDOR", "")
	dir := t.TempDir()
	t.Chdir(dir)
	clitest.DebeCorrer(t, "init")
	clitest.DebeCorrer(t, "env", "add", "dev", dev)
	clitest.DebeCorrer(t, "env", "add", "staging", staging)
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.EscribirMigracion(t, dir, "0001_crear_clientes",
		"CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL, email text);\n", "DROP TABLE clientes;\n")

	// ------------------------------------------------ objetos repetibles
	if out := clitest.DebeCorrer(t, "new", "--repetible", "vistas/clientes con email"); !strings.Contains(out, "sql/vistas/clientes_con_email.sql") {
		t.Fatalf("new --repetible:\n%s", out)
	}
	vista := filepath.Join(dir, "sql", "vistas", "clientes_con_email.sql")
	_ = os.WriteFile(vista, []byte("CREATE OR REPLACE VIEW clientes_con_email AS\n  SELECT id, nombre FROM clientes WHERE email IS NOT NULL;\n"), 0o644)

	out := clitest.DebeCorrer(t, "up", "--env", "dev")
	if !strings.Contains(out, "1 migración aplicada") || !strings.Contains(out, "1 objeto repetible aplicado") {
		t.Fatalf("up con un repetible nuevo:\n%s", out)
	}
	if !clitest.ExisteTabla(t, dev, "clientes_con_email") {
		t.Fatal("no se creó la vista")
	}
	out = clitest.DebeCorrer(t, "status", "--env", "dev")
	if !strings.Contains(out, "vistas/clientes_con_email.sql") || !strings.Contains(out, "Sin drift") {
		t.Fatalf("status con el repetible aplicado:\n%s", out)
	}
	if out := clitest.DebeCorrer(t, "up", "--env", "dev"); !strings.Contains(out, "ya está al día") {
		t.Fatalf("up sin cambios:\n%s", out)
	}

	// Al cambiar el archivo se vuelve a aplicar (sin migración nueva).
	_ = os.WriteFile(vista, []byte("CREATE OR REPLACE VIEW clientes_con_email AS\n  SELECT id, nombre, email FROM clientes WHERE email IS NOT NULL;\n"), 0o644)
	if out := clitest.DebeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "cambió") {
		t.Fatalf("status con el repetible cambiado:\n%s", out)
	}
	if out := clitest.DebeCorrer(t, "up", "--env", "dev", "--dry-run"); !strings.Contains(out, "SELECT id, nombre, email") {
		t.Fatalf("up --dry-run:\n%s", out)
	}
	if clitest.ColumnaExiste(t, dev, "clientes_con_email", "email") {
		t.Fatal("--dry-run aplicó el repetible")
	}
	clitest.DebeCorrer(t, "up", "--env", "dev")
	if !clitest.ColumnaExiste(t, dev, "clientes_con_email", "email") {
		t.Fatal("up no volvió a aplicar el repetible cambiado")
	}

	// Uno roto: se deshace y dice dónde.
	rota := filepath.Join(dir, "sql", "vistas", "rota.sql")
	_ = os.WriteFile(rota, []byte("CREATE OR REPLACE VIEW rota AS\n  SELECT nada FROM clientes;\n"), 0o644)
	clitest.DebeFallar(t, "objetos repetibles", "up", "--env", "dev")
	if clitest.ExisteTabla(t, dev, "rota") {
		t.Fatal("quedó a medias el repetible roto")
	}
	_ = os.Remove(rota)

	// Se borra el archivo: avisa que el objeto sigue en la base.
	_ = os.Remove(vista)
	if out := clitest.DebeCorrer(t, "up", "--env", "dev"); !strings.Contains(out, "Se borró vistas/clientes_con_email.sql") {
		t.Fatalf("up tras borrar el archivo:\n%s", out)
	}
	if out := clitest.DebeCorrer(t, "status", "--env", "dev"); strings.Contains(out, "clientes_con_email") {
		t.Fatalf("status siguió mostrando el repetible borrado:\n%s", out)
	}

	// ------------------------------------------------ seeds
	if out := clitest.DebeCorrer(t, "new", "--seed", "clientes"); !strings.Contains(out, "seeds/0001_clientes.sql") {
		t.Fatalf("new --seed:\n%s", out)
	}
	seed := filepath.Join(dir, "seeds", "0001_clientes.sql")
	_ = os.WriteFile(seed, []byte("INSERT INTO clientes (nombre, email) VALUES ('Ana', 'ana@ejemplo.com'), ('Beto', NULL);\n"), 0o644)
	if out := clitest.DebeCorrer(t, "seed"); !strings.Contains(out, "1 seed cargado") {
		t.Fatalf("seed:\n%s", out)
	}
	if n := clitest.Contar(t, dev, "SELECT count(*) FROM clientes"); n != 2 {
		t.Fatalf("clientes tras seed = %d", n)
	}
	if out := clitest.DebeCorrer(t, "seed"); !strings.Contains(out, "ya tiene todos los datos de prueba") {
		t.Fatalf("seed repetido:\n%s", out)
	}
	_ = os.WriteFile(seed, []byte("INSERT INTO clientes (nombre) VALUES ('Carla');\n"), 0o644)
	if out := clitest.DebeCorrer(t, "seed"); !strings.Contains(out, "no se repite") {
		t.Fatalf("seed cambiado:\n%s", out)
	}
	clitest.DebeCorrer(t, "seed", "0001_clientes.sql")
	if n := clitest.Contar(t, dev, "SELECT count(*) FROM clientes"); n != 3 {
		t.Fatalf("clientes tras forzar el seed = %d", n)
	}
	clitest.DebeFallar(t, "compartido", "seed", "--env", "staging")
	if out := clitest.DebeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "Sin drift") {
		t.Fatalf("los seeds no deben causar drift:\n%s", out)
	}

	// ------------------------------------------------ sql suelto
	out = clitest.DebeCorrer(t, "sql", "-c", "SELECT nombre, email FROM clientes ORDER BY id")
	if !strings.Contains(out, "Ana") || !strings.Contains(out, "null") || !strings.Contains(out, "3 filas") {
		t.Fatalf("sql SELECT:\n%s", out)
	}
	if strings.Contains(out, "se deshizo") {
		t.Fatalf("un SELECT no debe decir que se deshizo:\n%s", out)
	}
	out = clitest.DebeCorrer(t, "sql", "-c", "DELETE FROM clientes; SELECT count(*) AS quedan FROM clientes")
	if !strings.Contains(out, "quedan") || !strings.Contains(out, "se deshizo") {
		t.Fatalf("sql DELETE:\n%s", out)
	}
	if n := clitest.Contar(t, dev, "SELECT count(*) FROM clientes"); n != 3 {
		t.Fatalf("sin --guardar se borraron clientes: quedan %d", n)
	}
	clitest.DebeFallar(t, "--si", "sql", "--env", "staging", "--guardar", "-c", "SELECT 1")
	clitest.DebeCorrer(t, "sql", "-c", "DELETE FROM clientes WHERE nombre = 'Carla'", "--guardar")
	if n := clitest.Contar(t, dev, "SELECT count(*) FROM clientes"); n != 2 {
		t.Fatalf("con --guardar: quedan %d", n)
	}
	prueba := filepath.Join(dir, "prueba.sql")
	_ = os.WriteFile(prueba, []byte("SELECT 1;\nSELEC 2;\n"), 0o644)
	clitest.DebeFallar(t, "línea 2", "sql", prueba)

	// Consola: sentencias de varias líneas terminadas en ;
	var ejecutadas []string
	c := &cobra.Command{}
	var buf strings.Builder
	c.SetOut(&buf)
	err := cli.Consola(c, strings.NewReader("SELECT 1\n  AS a;\nSELECT 2;\n\\q\nSELECT 3;\n"), func(s string) error {
		ejecutadas = append(ejecutadas, strings.TrimSpace(s))
		return nil
	})
	if err != nil || len(ejecutadas) != 2 || ejecutadas[0] != "SELECT 1\n  AS a;" {
		t.Fatalf("consola: %v %q", err, ejecutadas)
	}
}
