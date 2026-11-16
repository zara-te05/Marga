// Package clitest tiene ayudas para las pruebas que ejecutan la CLI de Marga
// como lo haría la terminal (integración y e2e).
package clitest

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/zara-te05/Marga/internal/controladores/cli"
)

// Correr ejecuta `marga ARGS...` como lo haría la terminal y devuelve lo que imprimió.
func Correr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cli.ResetFlags(cli.RootCmd)
	var buf bytes.Buffer
	cli.RootCmd.SetOut(&buf)
	cli.RootCmd.SetErr(&buf)
	cli.RootCmd.SetArgs(args)
	err := cli.RootCmd.Execute()
	return buf.String(), err
}

func DebeCorrer(t *testing.T, args ...string) string {
	t.Helper()
	out, err := Correr(t, args...)
	if err != nil {
		t.Fatalf("marga %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func DebeFallar(t *testing.T, contiene string, args ...string) string {
	t.Helper()
	out, err := Correr(t, args...)
	if err == nil {
		t.Fatalf("marga %s debía fallar:\n%s", strings.Join(args, " "), out)
	}
	// Se busca en lo que ve la persona: el error, su "prueba: …" y la salida.
	visto := err.Error() + "\nprueba: " + cli.Sugerencia(err) + "\n" + out
	if !strings.Contains(visto, contiene) {
		t.Fatalf("marga %s: esperaba %q en el error:\n%s", strings.Join(args, " "), contiene, visto)
	}
	return err.Error()
}

func ColumnaExiste(t *testing.T, url, tabla, col string) bool {
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

func ExisteTabla(t *testing.T, url, tabla string) bool {
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

func EscribirMigracion(t *testing.T, dir, id, up, down string) {
	t.Helper()
	base := filepath.Join(dir, "migrations", id)
	if err := os.WriteFile(base+".up.sql", []byte(up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+".down.sql", []byte(down), 0o644); err != nil {
		t.Fatal(err)
	}
}

func Contar(t *testing.T, url, q string) int {
	t.Helper()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
