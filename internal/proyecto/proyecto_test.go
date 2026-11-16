package proyecto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	casos := map[string]string{
		"Agregar teléfono a Clientes": "agregar_telefono_a_clientes",
		"  crear   tabla  ñandú!! ":   "crear_tabla_nandu",
		"¿?":                          "",
	}
	for in, want := range casos {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, esperaba %q", in, got, want)
		}
	}
	if len(Slug(strings.Repeat("abc ", 40))) > 60 {
		t.Error("Slug debe recortar a 60 caracteres")
	}
}

func TestEsVacio(t *testing.T) {
	if !EsVacio("-- solo comentarios\n\n   \n-- otro") {
		t.Error("solo comentarios es vacío")
	}
	if EsVacio("-- comentario\nDROP TABLE x;") {
		t.Error("tiene SQL")
	}
}

func TestMigraciones(t *testing.T) {
	dir := t.TempDir()
	p, err := Iniciar(dir, Config{Motor: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := p.SiguienteVersion(); v != "0001" {
		t.Fatalf("primera versión = %s", v)
	}
	if _, _, err := p.EscribirMigracion("0001", "crear_clientes", "CREATE TABLE clientes (id int);\r\n", "DROP TABLE clientes;\n"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.EscribirMigracion("0001", "crear_clientes", "x", "y"); err == nil {
		t.Fatal("no debe sobrescribir una migración existente")
	}
	if _, _, err := p.EscribirMigracion("0002", "indice", "-- marga:sin-transaccion\nCREATE INDEX CONCURRENTLY i ON clientes (id);", "-- sin reversión\n"); err != nil {
		t.Fatal(err)
	}
	// Archivos que no son migraciones se ignoran.
	_ = os.WriteFile(filepath.Join(p.DirMigraciones(), "LEEME.txt"), []byte("hola"), 0o644)

	migs, err := p.Migraciones()
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 2 || migs[0].ID() != "0001_crear_clientes" || migs[1].Version != "0002" {
		t.Fatalf("migraciones = %+v", migs)
	}
	if strings.Contains(migs[0].Up, "\r") || migs[0].Checksum != Checksum("CREATE TABLE clientes (id int);") {
		t.Fatal("el UP debe normalizarse antes del checksum")
	}
	if !migs[1].SinTransaccion || migs[1].Down != "" {
		t.Fatalf("0002: sin transacción=%v down=%q", migs[1].SinTransaccion, migs[1].Down)
	}
	if v, _ := p.SiguienteVersion(); v != "0003" {
		t.Fatalf("siguiente = %s", v)
	}

	// Renombrar cambia ambos archivos.
	if err := p.Renombrar(migs[1], "0005", "indice_clientes"); err != nil {
		t.Fatal(err)
	}
	migs, _ = p.Migraciones()
	if migs[1].ID() != "0005_indice_clientes" {
		t.Fatalf("tras renombrar: %s", migs[1].ID())
	}

	// Dos archivos con la misma versión y distinto nombre es un error.
	_ = os.WriteFile(filepath.Join(p.DirMigraciones(), "0001_otra.up.sql"), []byte("SELECT 1;"), 0o644)
	if _, err := p.Migraciones(); err == nil {
		t.Fatal("dos migraciones con la misma versión deben dar error")
	}

	// Buscar encuentra el proyecto desde una subcarpeta.
	sub := filepath.Join(dir, "a", "b")
	_ = os.MkdirAll(sub, 0o755)
	q, err := Buscar(sub)
	if err != nil || q.Raiz != p.Raiz {
		t.Fatalf("Buscar desde subcarpeta: %v %v", q, err)
	}
	if _, err := Buscar(t.TempDir()); err == nil {
		t.Fatal("fuera de un proyecto Buscar debe fallar")
	}
}

func TestEntornoPersonal(t *testing.T) {
	for _, n := range []string{"dev", "LOCAL", "desarrollo"} {
		if !EsPersonalPorNombre(n) {
			t.Errorf("%s es personal", n)
		}
	}
	for _, n := range []string{"prod", "staging", "devops"} {
		if EsPersonalPorNombre(n) {
			t.Errorf("%s no es personal", n)
		}
	}
}

func TestURLDeVariable(t *testing.T) {
	t.Setenv("MARGA_PRUEBA_URL", "postgres://u:secreto@h/bd")
	e := Entorno{URL: "$MARGA_PRUEBA_URL"}
	u, err := e.URLReal()
	if err != nil || u != "postgres://u:secreto@h/bd" {
		t.Fatalf("URLReal = %q, %v", u, err)
	}
	if strings.Contains(Entorno{URL: "postgres://u:secreto@h/bd"}.URLOculta(), "secreto") {
		t.Fatal("URLOculta no debe mostrar la contraseña")
	}
}
