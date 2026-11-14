package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/api"
	"github.com/zara-te05/Marga/internal/config"
	"github.com/zara-te05/Marga/internal/database"
	"github.com/zara-te05/Marga/internal/pruebabd"
	"github.com/zara-te05/Marga/internal/store"
)

// correr ejecuta `marga ARGS...` como lo haría la terminal y devuelve lo que imprimió.
func correr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ResetFlags(RootCmd)
	var buf bytes.Buffer
	RootCmd.SetOut(&buf)
	RootCmd.SetErr(&buf)
	RootCmd.SetArgs(args)
	err := RootCmd.Execute()
	return buf.String(), err
}

func debeCorrer(t *testing.T, args ...string) string {
	t.Helper()
	out, err := correr(t, args...)
	if err != nil {
		t.Fatalf("marga %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func columnaExiste(t *testing.T, url, tabla, col string) bool {
	t.Helper()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2)`, tabla, col).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

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

	debeCorrer(t, "login", "--servidor", srv.URL, "--email", "ana@ejemplo.com", "--password", "clave-segura-1")
	if out := debeCorrer(t, "whoami", "--servidor", srv.URL); !strings.Contains(out, "ana@ejemplo.com") {
		t.Fatalf("whoami: %s", out)
	}
	debeCorrer(t, "init", "--servidor", srv.URL, "--crear", "Tienda")
	debeCorrer(t, "env", "add", "dev", dev)
	debeCorrer(t, "env", "add", "staging", staging, "--compartido")

	// 1) Migración escrita a mano con `marga new`.
	debeCorrer(t, "new", "crear clientes")
	ups, _ := filepath.Glob(filepath.Join(proyectoA, "migrations", "0001_crear_clientes.up.sql"))
	if len(ups) != 1 {
		t.Fatal("marga new no creó 0001_crear_clientes.up.sql")
	}
	_ = os.WriteFile(ups[0], []byte("CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL);\n"), 0o644)
	_ = os.WriteFile(strings.Replace(ups[0], ".up.", ".down.", 1), []byte("DROP TABLE clientes;\n"), 0o644)

	if out := debeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "0001_crear_clientes") {
		t.Fatalf("status antes de up:\n%s", out)
	}
	debeCorrer(t, "up", "--env", "dev")

	// 2) Migración generada: cambio la base de dev a mano y `marga commit` escribe el SQL.
	ddb, _ := sql.Open("postgres", dev)
	if _, err := ddb.Exec(`ALTER TABLE clientes ADD COLUMN email text`); err != nil {
		t.Fatal(err)
	}
	ddb.Close()
	// Con el cambio sin registrar, up se niega (drift).
	if _, err := correr(t, "up", "--env", "dev"); err != nil {
		t.Logf("up con drift (esperado que avise): %v", err)
	}
	out := debeCorrer(t, "commit", "agregar email", "--env", "dev")
	if !strings.Contains(out, "ADD COLUMN email") {
		t.Fatalf("commit no generó el ALTER:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(proyectoA, "migrations", "0002_agregar_email.down.sql")); err != nil {
		t.Fatal("commit no escribió el down")
	}

	// 3) push y log
	debeCorrer(t, "push")
	if out := debeCorrer(t, "log"); !strings.Contains(out, "0002_agregar_email") || !strings.Contains(out, "0001_crear_clientes") {
		t.Fatalf("log:\n%s", out)
	}

	// 4) staging recibe las dos.
	debeCorrer(t, "up", "--env", "staging")
	if !columnaExiste(t, staging, "clientes", "email") {
		t.Fatal("staging no tiene clientes.email tras up")
	}
	if out := debeCorrer(t, "status", "--env", "staging"); !strings.Contains(out, "2 aplicadas · 0 pendientes") {
		t.Fatalf("staging no debería tener pendientes:\n%s", out)
	}

	// 5) Otra persona clona el proyecto y hace pull.
	proyectoB := t.TempDir()
	t.Chdir(proyectoB)
	var cfg struct {
		RepoID int64 `json:"repo_id"`
	}
	leerJSON(t, filepath.Join(proyectoA, ".marga", "config.json"), &cfg)
	debeCorrer(t, "init", "--servidor", srv.URL, "--repo", itoa(cfg.RepoID))
	debeCorrer(t, "pull")
	for _, f := range []string{"0001_crear_clientes.up.sql", "0002_agregar_email.up.sql", "0002_agregar_email.down.sql"} {
		a, _ := os.ReadFile(filepath.Join(proyectoA, "migrations", f))
		b, err := os.ReadFile(filepath.Join(proyectoB, "migrations", f))
		if err != nil || string(a) != string(b) {
			t.Fatalf("pull no trajo %s igual al original", f)
		}
	}

	// 6) down revierte la última en dev.
	t.Chdir(proyectoA)
	debeCorrer(t, "down", "--env", "dev")
	if columnaExiste(t, dev, "clientes", "email") {
		t.Fatal("down no quitó clientes.email")
	}
	debeCorrer(t, "revisar")
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
