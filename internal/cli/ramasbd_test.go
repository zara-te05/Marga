package cli

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/api"
	"github.com/zara-te05/Marga/internal/config"
	"github.com/zara-te05/Marga/internal/database"
	"github.com/zara-te05/Marga/internal/pruebabd"
	"github.com/zara-te05/Marga/internal/ramabd"
	"github.com/zara-te05/Marga/internal/store"
)

func gitEn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@ejemplo.com", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func escribirMigracion(t *testing.T, dir, id, up, down string) {
	t.Helper()
	base := filepath.Join(dir, "migrations", id)
	if err := os.WriteFile(base+".up.sql", []byte(up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+".down.sql", []byte(down), 0o644); err != nil {
		t.Fatal(err)
	}
}

func existeTabla(t *testing.T, url, tabla string) bool {
	t.Helper()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ok bool
	if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, tabla).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func debeFallar(t *testing.T, contiene string, args ...string) string {
	t.Helper()
	out, err := correr(t, args...)
	if err == nil {
		t.Fatalf("marga %s debía fallar:\n%s", strings.Join(args, " "), out)
	}
	if !strings.Contains(err.Error()+out, contiene) {
		t.Fatalf("marga %s: esperaba %q en el error:\n%v\n%s", strings.Join(args, " "), contiene, err, out)
	}
	return err.Error()
}

func TestBasePorRama(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está instalado")
	}
	ctx := context.Background()
	sdb, err := database.Open(ctx, pruebabd.Nueva(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	if err := database.Migrate(ctx, sdb); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(config.Config{SessionTTL: time.Hour, DiffTTL: time.Hour}, store.New(sdb, time.Hour)).Handler())
	defer srv.Close()
	res, err := http.Post(srv.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"nombre":"Ana","email":"ana@ejemplo.com","password":"clave-segura-1"}`))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("registro: %v %v", res, err)
	}
	res.Body.Close()

	dev := pruebabd.Nueva(t)
	base, _ := ramabd.BaseDeURL(dev)
	urlDe := func(rama string) string { u, _ := ramabd.ConBase(dev, ramabd.Nombre(base, rama)); return u }
	admin, err := ramabd.AbrirAdmin(ctx, dev)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	t.Cleanup(func() { // las bases de rama que haya dejado la prueba
		a, err := ramabd.AbrirAdmin(context.Background(), dev)
		if err != nil {
			return
		}
		defer a.Close()
		for _, r := range []string{"feature/facturas", "feature/vieja", "feature/ocupada"} {
			_ = a.Borrar(context.Background(), ramabd.Nombre(base, r), base, true)
		}
	})

	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MARGA_SERVIDOR", "")
	dir := t.TempDir()
	t.Chdir(dir)
	gitEn(t, dir, "init", "-q")

	debeCorrer(t, "login", "--servidor", srv.URL, "--email", "ana@ejemplo.com", "--password", "clave-segura-1")
	debeCorrer(t, "init", "--servidor", srv.URL, "--crear", "Tienda")
	debeCorrer(t, "env", "add", "dev", dev)
	escribirMigracion(t, dir, "0001_crear_clientes", "CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL);\n", "DROP TABLE clientes;\n")

	// 1) En main se usa la base de la cadena de conexión.
	out := debeCorrer(t, "up", "--env", "dev")
	if strings.Contains(out, "base nueva") {
		t.Fatalf("main no debe crear otra base:\n%s", out)
	}
	if out := debeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "Rama main · base "+base) {
		t.Fatalf("status en main:\n%s", out)
	}
	debeCorrer(t, "push")
	gitEn(t, dir, "add", ".")
	gitEn(t, dir, "commit", "-q", "-m", "clientes")
	if _, err := os.Stat(filepath.Join(dir, ".marga", "local.json")); err != nil {
		t.Fatal("no se creó .marga/local.json")
	}
	if gi, _ := os.ReadFile(filepath.Join(dir, ".marga", ".gitignore")); !strings.Contains(string(gi), "local.json") {
		t.Fatal("local.json no está en .marga/.gitignore")
	}

	// 2) Rama nueva: su base es una copia de la de main.
	gitEn(t, dir, "checkout", "-q", "-b", "feature/facturas")
	out = debeCorrer(t, "up", "--env", "dev")
	if !strings.Contains(out, "base nueva "+ramabd.Nombre(base, "feature/facturas")) || !strings.Contains(out, "copia de "+base) {
		t.Fatalf("up en la rama nueva:\n%s", out)
	}
	if !existeTabla(t, urlDe("feature/facturas"), "clientes") {
		t.Fatal("la copia no tiene la tabla clientes")
	}
	if m, _ := admin.Marca(ctx, ramabd.Nombre(base, "feature/facturas")); m != "feature/facturas" {
		t.Fatalf("la base de la rama no tiene la marca de Marga: %q", m)
	}

	// marga url: la cadena de la base de la rama, para que la app también cambie de base.
	if out := debeCorrer(t, "url"); strings.TrimSpace(out) != urlDe("feature/facturas") {
		t.Fatalf("marga url en la rama:\n%s", out)
	}
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(envFile, []byte("PORT=3000\r\nexport DATABASE_URL=postgres://viejo\r\nDEBUG=1\r\n"), 0o644)
	debeCorrer(t, "url", "--escribir", envFile)
	if b, _ := os.ReadFile(envFile); string(b) != "PORT=3000\r\nexport DATABASE_URL="+urlDe("feature/facturas")+"\r\nDEBUG=1\r\n" {
		t.Fatalf(".env tras marga url --escribir:\n%q", b)
	}
	debeCorrer(t, "url", "--escribir", envFile, "--variable", "PG_URL")
	if b, _ := os.ReadFile(envFile); !strings.HasSuffix(string(b), "DEBUG=1\r\nPG_URL="+urlDe("feature/facturas")+"\r\n") {
		t.Fatalf(".env con variable nueva:\n%q", b)
	}
	_ = os.Remove(envFile)

	// 3) Aislamiento: cambiar la base de la rama no toca la de main.
	bdb, _ := sql.Open("postgres", urlDe("feature/facturas"))
	if _, err := bdb.Exec(`CREATE TABLE facturas (id serial PRIMARY KEY, total numeric NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	bdb.Close()
	debeCorrer(t, "commit", "crear facturas", "--env", "dev")
	if existeTabla(t, dev, "facturas") {
		t.Fatal("la tabla de la rama apareció en la base de main")
	}
	// push crea la rama en el servidor desde main y la usa.
	if out := debeCorrer(t, "push"); !strings.Contains(out, "creada en el servidor desde main") || !strings.Contains(out, "feature/facturas") {
		t.Fatalf("push en rama nueva:\n%s", out)
	}
	if out := debeCorrer(t, "log"); !strings.HasPrefix(stripANSI(out), "feature/facturas · 2 commits") {
		t.Fatalf("log debía usar la rama de git:\n%s", out)
	}
	// El servidor sabe en qué rama está la base personal.
	var ramaGit sql.NullString
	if err := sdb.QueryRow(`SELECT rama_git FROM entorno_bd WHERE nombre LIKE 'dev@%'`).Scan(&ramaGit); err != nil || ramaGit.String != "feature/facturas" {
		t.Fatalf("rama_git en el servidor = %v, %v", ramaGit, err)
	}
	// …y cuenta sus pendientes contra feature/facturas, no contra main.
	var cfg struct {
		RepoID int64 `json:"repo_id"`
	}
	leerJSON(t, filepath.Join(dir, ".marga", "config.json"), &cfg)
	ents, err := store.New(sdb, time.Hour).ListarEntornos(ctx, cfg.RepoID, "")
	if err != nil || len(ents) != 1 || ents[0].RamaComparada != "feature/facturas" || ents[0].Pendientes != 0 || ents[0].Aplicadas != 2 {
		t.Fatalf("entornos en el servidor: %+v %v", ents, err)
	}
	debeFallar(t, "git checkout", "checkout", "main") // con git, marga checkout no aplica
	gitEn(t, dir, "add", ".")
	gitEn(t, dir, "commit", "-q", "-m", "facturas")

	// 4) De vuelta en main: su base, sin facturas.
	gitEn(t, dir, "checkout", "-q", "main")
	if out := debeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "1 aplicada · 0 pendientes") || !strings.Contains(out, "base "+base) {
		t.Fatalf("status de vuelta en main:\n%s", out)
	}
	if out := debeCorrer(t, "url"); strings.TrimSpace(out) != dev {
		t.Fatalf("marga url en main:\n%s", out)
	}
	out = debeCorrer(t, "ramas")
	if !strings.Contains(out, ramabd.Nombre(base, "feature/facturas")) || !strings.Contains(out, "principal") {
		t.Fatalf("marga ramas:\n%s", out)
	}

	// 5) Protección al borrar: la principal nunca.
	debeFallar(t, "rama principal", "ramas", "borrar", "main")

	// 6) Rama nacida de un main más viejo: la copia traería 0002, que la rama no conoce.
	gitEn(t, dir, "branch", "feature/vieja")
	escribirMigracion(t, dir, "0002_agregar_email", "ALTER TABLE clientes ADD COLUMN email text;\n", "ALTER TABLE clientes DROP COLUMN email;\n")
	debeCorrer(t, "up", "--env", "dev")
	gitEn(t, dir, "add", ".")
	gitEn(t, dir, "commit", "-q", "-m", "email")
	gitEn(t, dir, "checkout", "-q", "feature/vieja")
	out = debeCorrer(t, "up", "--env", "dev")
	if !strings.Contains(out, "no conoce") || !strings.Contains(out, "vacía") {
		t.Fatalf("rama de un main viejo:\n%s", out)
	}
	if !existeTabla(t, urlDe("feature/vieja"), "clientes") || columnaExiste(t, urlDe("feature/vieja"), "clientes", "email") {
		t.Fatal("la base de la rama vieja debía tener solo las migraciones de la rama")
	}

	// 7) Base ocupada: alguien conectado a la de main impide copiarla.
	gitEn(t, dir, "checkout", "-q", "main")
	ocupa, _ := sql.Open("postgres", dev)
	if err := ocupa.Ping(); err != nil {
		t.Fatal(err)
	}
	gitEn(t, dir, "checkout", "-q", "-b", "feature/ocupada")
	msg := debeFallar(t, "--vacia", "up", "--env", "dev")
	if !strings.Contains(msg, "conectado") {
		t.Fatalf("el error debe decir quién está conectado:\n%s", msg)
	}
	if out := debeCorrer(t, "up", "--env", "dev", "--vacia"); !strings.Contains(out, "vacía") || !strings.Contains(out, "2 migraciones aplicadas") {
		t.Fatalf("up --vacia:\n%s", out)
	}
	ocupa.Close()

	// 8) Borrar con la base abierta pide --forzar; limpiar quita las de ramas borradas en git.
	abierta, _ := sql.Open("postgres", urlDe("feature/ocupada"))
	_ = abierta.Ping()
	gitEn(t, dir, "checkout", "-q", "main")
	debeFallar(t, "--forzar", "ramas", "borrar", "feature/ocupada")
	debeCorrer(t, "ramas", "borrar", "feature/ocupada", "--forzar")
	abierta.Close()
	if ok, _ := admin.Existe(ctx, ramabd.Nombre(base, "feature/ocupada")); ok {
		t.Fatal("ramas borrar no borró la base")
	}

	gitEn(t, dir, "branch", "-q", "-D", "feature/vieja")
	if out := debeCorrer(t, "ramas", "limpiar", "--simular"); !strings.Contains(out, "feature/vieja") || strings.Contains(out, "feature/facturas") {
		t.Fatalf("limpiar --simular:\n%s", out)
	}
	debeCorrer(t, "ramas", "limpiar")
	if ok, _ := admin.Existe(ctx, ramabd.Nombre(base, "feature/vieja")); ok {
		t.Fatal("limpiar no borró la base de la rama borrada")
	}
	if ok, _ := admin.Existe(ctx, ramabd.Nombre(base, "feature/facturas")); !ok {
		t.Fatal("limpiar borró la base de una rama que sigue en git")
	}
	if ok, _ := admin.Existe(ctx, base); !ok {
		t.Fatal("se borró la base principal")
	}

	// 9) reiniciar: base nueva para la rama actual. main ya tiene 0002_agregar_email,
	// que feature/facturas no conoce, así que se crea vacía con las migraciones de la rama.
	gitEn(t, dir, "checkout", "-q", "feature/facturas")
	if out := debeCorrer(t, "ramas", "reiniciar", "--forzar"); !strings.Contains(out, "no conoce") || !strings.Contains(out, "vacía") {
		t.Fatalf("reiniciar:\n%s", out)
	}
	if existeTabla(t, urlDe("feature/facturas"), "facturas") {
		t.Fatal("tras reiniciar, la base debía empezar sin facturas")
	}
	if out := debeCorrer(t, "status", "--env", "dev"); !strings.Contains(out, "0002_crear_facturas") || !strings.Contains(out, "2 pendientes") {
		t.Fatalf("status tras reiniciar:\n%s", out)
	}
	debeCorrer(t, "up", "--env", "dev")
	if !existeTabla(t, urlDe("feature/facturas"), "facturas") {
		t.Fatal("up no dejó la base de la rama al día")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
