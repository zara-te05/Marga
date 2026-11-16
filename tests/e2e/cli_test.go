package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/zara-te05/Marga/tests/apoyo/clitest"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/controladores/api"
	"github.com/zara-te05/Marga/internal/datos/database"
	"github.com/zara-te05/Marga/internal/datos/store"
	"github.com/zara-te05/Marga/internal/servicios/config"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

func TestFlujoDeLaCLI(t *testing.T) {
	// Servidor de Marga real sobre una base nueva.
	ctx := context.Background()
	db, err := database.Open(ctx, pruebabd.Nueva(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(config.Config{SessionTTL: time.Hour, DiffTTL: time.Hour}, store.New(db, time.Hour)).Handler())
	defer srv.Close()
	res, err := http.Post(srv.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"nombre":"Ana","email":"ana@ejemplo.com","password":"clave-segura-1"}`))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("registro: %v %v", res, err)
	}
	res.Body.Close()

	// Las bases del usuario: dev (personal) y staging (compartida).
	dev, staging := pruebabd.Nueva(t), pruebabd.Nueva(t)

	t.Setenv("HOME", t.TempDir()) // credenciales en un HOME de prueba
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MARGA_SERVIDOR", "")
	proyectoA := t.TempDir()
	t.Chdir(proyectoA)

	clitest.DebeCorrer(t, "login", "--servidor", srv.URL, "--email", "ana@ejemplo.com", "--password", "clave-segura-1")
	if out := clitest.DebeCorrer(t, "whoami", "--servidor", srv.URL); !strings.Contains(out, "ana@ejemplo.com") {
		t.Fatalf("whoami: %s", out)
	}
	clitest.DebeCorrer(t, "init", "--servidor", srv.URL, "--crear", "Tienda")
	// El mismo nombre otra vez, en otra carpeta: el error dice qué proyecto vincular.
	t.Chdir(t.TempDir())
	clitest.DebeFallar(t, "marga init --repo 1", "init", "--servidor", srv.URL, "--crear", "Tienda")
	t.Chdir(proyectoA)
	clitest.DebeCorrer(t, "env", "add", "dev", dev)
	clitest.DebeCorrer(t, "env", "add", "staging", staging, "--compartido")

	// 1) Migración escrita a mano con `marga new`.
	clitest.DebeCorrer(t, "new", "crear clientes")
	ups, _ := filepath.Glob(filepath.Join(proyectoA, "migrations", "0001_crear_clientes.up.sql"))
	if len(ups) != 1 {
		t.Fatal("marga new no creó 0001_crear_clientes.up.sql")
	}
	_ = os.WriteFile(ups[0], []byte("CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL);\n"), 0o644)
	_ = os.WriteFile(strings.Replace(ups[0], ".up.", ".down.", 1), []byte("DROP TABLE clientes;\n"), 0o644)

	if out := clitest.DebeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "0001_crear_clientes") {
		t.Fatalf("status antes de up:\n%s", out)
	}
	clitest.DebeCorrer(t, "up", "--env", "dev")

	// 2) Migración generada: cambio la base de dev a mano y `marga commit` escribe el SQL.
	ddb, _ := sql.Open("postgres", dev)
	if _, err := ddb.Exec(`ALTER TABLE clientes ADD COLUMN email text`); err != nil {
		t.Fatal(err)
	}
	ddb.Close()
	// Con el cambio sin registrar, up se niega (drift).
	if _, err := clitest.Correr(t, "up", "--env", "dev"); err != nil {
		t.Logf("up con drift (esperado que avise): %v", err)
	}
	out := clitest.DebeCorrer(t, "commit", "agregar email", "--env", "dev")
	if !strings.Contains(out, "ADD COLUMN email") {
		t.Fatalf("commit no generó el ALTER:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(proyectoA, "migrations", "0002_agregar_email.down.sql")); err != nil {
		t.Fatal("commit no escribió el down")
	}

	// 3) push y log
	clitest.DebeCorrer(t, "push")
	if out := clitest.DebeCorrer(t, "log"); !strings.Contains(out, "0002_agregar_email") || !strings.Contains(out, "0001_crear_clientes") {
		t.Fatalf("log:\n%s", out)
	}

	// 4) staging recibe las dos.
	clitest.DebeCorrer(t, "up", "--env", "staging")
	if !clitest.ColumnaExiste(t, staging, "clientes", "email") {
		t.Fatal("staging no tiene clientes.email tras up")
	}
	if out := clitest.DebeCorrer(t, "status", "--env", "staging"); !strings.Contains(out, "2 aplicadas · 0 pendientes") {
		t.Fatalf("staging no debería tener pendientes:\n%s", out)
	}

	// 5) Otra persona clona el proyecto y hace pull.
	proyectoB := t.TempDir()
	t.Chdir(proyectoB)
	var cfg struct {
		RepoID int64 `json:"repo_id"`
	}
	leerJSON(t, filepath.Join(proyectoA, ".marga", "config.json"), &cfg)
	clitest.DebeCorrer(t, "init", "--servidor", srv.URL, "--repo", itoa(cfg.RepoID))
	clitest.DebeCorrer(t, "pull")
	for _, f := range []string{"0001_crear_clientes.up.sql", "0002_agregar_email.up.sql", "0002_agregar_email.down.sql"} {
		a, _ := os.ReadFile(filepath.Join(proyectoA, "migrations", f))
		b, err := os.ReadFile(filepath.Join(proyectoB, "migrations", f))
		if err != nil || string(a) != string(b) {
			t.Fatalf("pull no trajo %s igual al original", f)
		}
	}

	// 6) down revierte la última en dev.
	t.Chdir(proyectoA)
	clitest.DebeCorrer(t, "down", "--env", "dev")
	if clitest.ColumnaExiste(t, dev, "clientes", "email") {
		t.Fatal("down no quitó clientes.email")
	}
	clitest.DebeCorrer(t, "revisar")
}

func leerJSON(t *testing.T, ruta string, v any) {
	t.Helper()
	b, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

var jsonUnmarshal = json.Unmarshal
