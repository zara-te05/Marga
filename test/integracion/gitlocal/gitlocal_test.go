package gitlocal_test

import (
	"github.com/zara-te05/Marga/internal/datos/gitlocal"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Prueba", "-c", "user.email=prueba@ejemplo.com", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func nuevoRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está instalado")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hola"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "inicio")
	return dir
}

func TestRamaActualYOrigen(t *testing.T) {
	dir := nuevoRepo(t)
	sub := filepath.Join(dir, "app", "db")
	_ = os.MkdirAll(sub, 0o755)

	r, err := gitlocal.Buscar(sub) // sube hasta encontrar .git
	if err != nil {
		t.Fatal(err)
	}
	if rama, sep, _ := r.RamaActual(); rama != "main" || sep {
		t.Fatalf("rama = %q separado=%v", rama, sep)
	}

	git(t, dir, "checkout", "-q", "-b", "feature/facturas")
	git(t, dir, "checkout", "-q", "-b", "feature/facturas-pdf")
	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "checkout", "-q", "feature/facturas-pdf")
	if rama, _, _ := r.RamaActual(); rama != "feature/facturas-pdf" {
		t.Fatalf("rama = %q", rama)
	}
	// La rama de origen es la de la primera vez que se entró, no la última.
	if o := r.Origen("feature/facturas-pdf"); o != "feature/facturas" {
		t.Fatalf("origen de feature/facturas-pdf = %q", o)
	}
	if o := r.Origen("feature/facturas"); o != "main" {
		t.Fatalf("origen de feature/facturas = %q", o)
	}
	if o := r.Origen("no-existe"); o != "" {
		t.Fatalf("origen de una rama desconocida = %q", o)
	}

	if !r.Existe("feature/facturas") || r.Existe("feature/otra") {
		t.Fatal("Existe no distingue ramas")
	}
	// Las ramas empaquetadas (git pack-refs) también cuentan.
	git(t, dir, "pack-refs", "--all")
	if !r.Existe("feature/facturas") || !r.Existe("main") {
		t.Fatal("Existe no lee packed-refs")
	}
	ramas := r.Ramas()
	sort.Strings(ramas)
	if strings.Join(ramas, ",") != "feature/facturas,feature/facturas-pdf,main" {
		t.Fatalf("Ramas = %v", ramas)
	}
	git(t, dir, "branch", "-q", "-D", "feature/facturas")
	if r.Existe("feature/facturas") {
		t.Fatal("una rama borrada sigue existiendo")
	}

	git(t, dir, "checkout", "-q", "--detach")
	if _, sep, _ := r.RamaActual(); !sep {
		t.Fatal("HEAD separado no detectado")
	}
}

func TestWorktree(t *testing.T) {
	dir := nuevoRepo(t)
	wt := filepath.Join(t.TempDir(), "otro")
	git(t, dir, "worktree", "add", "-q", "-b", "feature/wt", wt)
	r, err := gitlocal.Buscar(wt)
	if err != nil {
		t.Fatal(err)
	}
	if rama, _, _ := r.RamaActual(); rama != "feature/wt" {
		t.Fatalf("rama del worktree = %q", rama)
	}
	if !r.Existe("main") {
		t.Fatal("desde un worktree no se ven las ramas del repositorio")
	}
}

func TestSinGit(t *testing.T) {
	if _, err := gitlocal.Buscar(t.TempDir()); err == nil {
		t.Skip("la carpeta temporal está dentro de un repositorio git")
	}
}
