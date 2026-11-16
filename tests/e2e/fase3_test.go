package e2e

import (
	"context"
	"github.com/zara-te05/Marga/tests/apoyo/clitest"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/controladores/api"
	"github.com/zara-te05/Marga/internal/datos/database"
	"github.com/zara-te05/Marga/internal/datos/store"
	"github.com/zara-te05/Marga/internal/servicios/config"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

func servidorDePrueba(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	sdb, err := database.Open(ctx, pruebabd.Nueva(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sdb.Close() })
	if err := database.Migrate(ctx, sdb); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(config.Config{SessionTTL: time.Hour, DiffTTL: time.Hour}, store.New(sdb, time.Hour)).Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"nombre":"Ana","email":"ana@ejemplo.com","password":"clave-segura-1"}`))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("registro: %v %v", res, err)
	}
	res.Body.Close()
	return srv.URL
}

func archivoExiste(dir, nombre string) bool {
	_, err := os.Stat(filepath.Join(dir, "migrations", nombre))
	return err == nil
}

// Dos ramas crean su 0002; al juntarlas en git hay que renumerar, y `push` en main
// fusiona la rama también en el servidor (un solo merge: el de git).
func TestRenumerarYUnSoloMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está instalado")
	}
	url := servidorDePrueba(t)
	dev := pruebabd.Nueva(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MARGA_SERVIDOR", "")
	dir := t.TempDir()
	t.Chdir(dir)
	gitEn(t, dir, "init", "-q")
	clitest.DebeCorrer(t, "login", "--servidor", url, "--email", "ana@ejemplo.com", "--password", "clave-segura-1")
	clitest.DebeCorrer(t, "init", "--servidor", url, "--crear", "Tienda")
	clitest.DebeCorrer(t, "env", "add", "dev", dev)
	clitest.EscribirMigracion(t, dir, "0001_crear_clientes", "CREATE TABLE clientes (id serial PRIMARY KEY, nombre text);\n", "DROP TABLE clientes;\n")
	clitest.DebeCorrer(t, "up")
	clitest.DebeCorrer(t, "push")
	gitEn(t, dir, "add", ".")
	gitEn(t, dir, "commit", "-q", "-m", "clientes")

	// La rama crea su 0002 y la envía.
	gitEn(t, dir, "checkout", "-q", "-b", "feature/facturas")
	clitest.EscribirMigracion(t, dir, "0002_crear_facturas", "CREATE TABLE facturas (id serial PRIMARY KEY, total numeric);\n", "DROP TABLE facturas;\n")
	clitest.DebeCorrer(t, "up")
	clitest.DebeCorrer(t, "push")
	gitEn(t, dir, "add", ".")
	gitEn(t, dir, "commit", "-q", "-m", "facturas")

	// Mientras, en main alguien crea OTRA 0002.
	gitEn(t, dir, "checkout", "-q", "main")
	clitest.EscribirMigracion(t, dir, "0002_crear_cupones", "CREATE TABLE cupones (id serial PRIMARY KEY, codigo text);\n", "DROP TABLE cupones;\n")
	clitest.DebeCorrer(t, "up")
	clitest.DebeCorrer(t, "push")
	gitEn(t, dir, "add", ".")
	gitEn(t, dir, "commit", "-q", "-m", "cupones")

	// Se juntan en git: dos 0002.
	gitEn(t, dir, "merge", "-q", "--no-edit", "feature/facturas")
	clitest.DebeFallar(t, "marga renumerar", "status")

	out := clitest.DebeCorrer(t, "renumerar", "--simular")
	if !strings.Contains(out, "se queda 0002_crear_cupones") || !strings.Contains(out, "0003_crear_facturas") {
		t.Fatalf("renumerar --simular:\n%s", out)
	}
	if !archivoExiste(dir, "0002_crear_facturas.up.sql") {
		t.Fatal("--simular renombró")
	}
	clitest.DebeCorrer(t, "renumerar")
	if !archivoExiste(dir, "0003_crear_facturas.up.sql") || !archivoExiste(dir, "0002_crear_cupones.up.sql") || archivoExiste(dir, "0002_crear_facturas.up.sql") {
		t.Fatal("renumerar no dejó 0002_crear_cupones y 0003_crear_facturas")
	}
	out = clitest.DebeCorrer(t, "up")
	if !strings.Contains(out, "0003_crear_facturas") {
		t.Fatalf("up tras renumerar:\n%s", out)
	}
	if !clitest.ExisteTabla(t, dev, "facturas") {
		t.Fatal("la base de main no recibió facturas")
	}

	// Un solo merge: push en main fusiona feature/facturas en el servidor.
	out = clitest.DebeCorrer(t, "push")
	if !strings.Contains(out, "se fusionó en main en el servidor") {
		t.Fatalf("push tras el merge de git:\n%s", out)
	}
	if out := clitest.DebeCorrer(t, "push"); !strings.Contains(out, "Nada que enviar") {
		t.Fatalf("segundo push:\n%s", out)
	}
	out = stripANSI(clitest.DebeCorrer(t, "log"))
	if !strings.HasPrefix(out, "main · 3 commits") || strings.Contains(out, "↓") {
		t.Fatalf("log de main en el servidor:\n%s", out)
	}
	for _, f := range []string{"0001_crear_clientes", "0002_crear_cupones", "0003_crear_facturas"} {
		if !strings.Contains(out, f) {
			t.Fatalf("falta %s en el servidor:\n%s", f, out)
		}
	}
	if out := clitest.DebeCorrer(t, "status"); !strings.Contains(out, "3 aplicadas · 0 pendientes") {
		t.Fatalf("status final:\n%s", out)
	}
}

func TestVerificar(t *testing.T) {
	servidor := os.Getenv("MARGA_TEST_DB_URL")
	if servidor == "" {
		t.Skip("define MARGA_TEST_DB_URL para correr las pruebas contra PostgreSQL")
	}
	temporales := func() int {
		return clitest.Contar(t, servidor, `SELECT count(*) FROM pg_database WHERE datname LIKE 'marga_verificar_%'`)
	}
	antes := temporales()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	clitest.DebeCorrer(t, "init")
	clitest.EscribirMigracion(t, dir, "0001_crear_clientes", "CREATE TABLE clientes (id serial PRIMARY KEY, nombre text);\n", "DROP TABLE clientes;\n")
	// El DOWN olvida la columna: solo quita el índice.
	clitest.EscribirMigracion(t, dir, "0002_email", "ALTER TABLE clientes ADD COLUMN email text;\nCREATE INDEX clientes_email_idx ON clientes (email);\n",
		"DROP INDEX clientes_email_idx;\n")

	out := clitest.DebeFallar(t, "verificación falló", "verificar", "--url", servidor)
	_ = out
	out2, _ := clitest.Correr(t, "verificar", "--url", servidor)
	if !strings.Contains(out2, "0002_email") || !strings.Contains(out2, "no deshace todo") || !strings.Contains(out2, "email") {
		t.Fatalf("verificar con un DOWN incompleto:\n%s", out2)
	}

	// Arreglado: pasa.
	clitest.EscribirMigracion(t, dir, "0002_email", "ALTER TABLE clientes ADD COLUMN email text;\nCREATE INDEX clientes_email_idx ON clientes (email);\n",
		"DROP INDEX clientes_email_idx;\nALTER TABLE clientes DROP COLUMN email;\n")
	if out := clitest.DebeCorrer(t, "verificar", "--url", servidor); !strings.Contains(out, "Todo bien") {
		t.Fatalf("verificar:\n%s", out)
	}

	// Sin DOWN: falla, salvo --permitir-sin-down.
	clitest.EscribirMigracion(t, dir, "0003_telefono", "ALTER TABLE clientes ADD COLUMN telefono text;\n", "-- sin reversión\n")
	clitest.DebeFallar(t, "verificación falló", "verificar", "--url", servidor)
	clitest.DebeCorrer(t, "verificar", "--url", servidor, "--permitir-sin-down")

	// Un repetible que no se puede aplicar dos veces (CREATE VIEW sin OR REPLACE).
	_ = os.MkdirAll(filepath.Join(dir, "sql"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sql", "vista.sql"), []byte("CREATE VIEW v AS SELECT id FROM clientes;\n"), 0o644)
	out2, err := clitest.Correr(t, "verificar", "--url", servidor, "--permitir-sin-down")
	if err == nil || !strings.Contains(out2, "OR REPLACE") {
		t.Fatalf("verificar con un repetible no repetible:\n%v\n%s", err, out2)
	}
	_ = os.WriteFile(filepath.Join(dir, "sql", "vista.sql"), []byte("CREATE OR REPLACE VIEW v AS SELECT id FROM clientes;\n"), 0o644)

	// Seeds rotos se detectan con --seeds.
	_ = os.MkdirAll(filepath.Join(dir, "seeds"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "seeds", "0001_clientes.sql"), []byte("INSERT INTO clientes (nombre, no_existe) VALUES ('Ana', 1);\n"), 0o644)
	clitest.DebeCorrer(t, "verificar", "--url", servidor, "--permitir-sin-down")
	clitest.DebeFallar(t, "verificación falló", "verificar", "--url", servidor, "--permitir-sin-down", "--seeds")

	if despues := temporales(); despues != antes {
		t.Fatalf("quedaron bases temporales: antes %d, después %d", antes, despues)
	}
}
