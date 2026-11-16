package ramabd

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/pruebabd"
)

func TestNombre(t *testing.T) {
	casos := map[[2]string]string{
		{"tienda", "feature/facturas"}:      "tienda__feature_facturas",
		{"tienda", "Feature/Facturación-2"}: "tienda__feature_facturacion_2",
		{"tienda", "fix--x__y"}:             "tienda__fix_x_y",
		{"tienda", "///"}:                   "tienda__rama",
	}
	for in, want := range casos {
		if got := Nombre(in[0], in[1]); got != want {
			t.Errorf("Nombre(%q, %q) = %q, quiero %q", in[0], in[1], got, want)
		}
	}
	// Largo: se recorta a 63 con un hash, y dos ramas largas distintas no chocan.
	largaA := "feature/" + strings.Repeat("a", 80) + "-uno"
	largaB := "feature/" + strings.Repeat("a", 80) + "-dos"
	a, b := Nombre("tienda_online", largaA), Nombre("tienda_online", largaB)
	if len(a) > MaxNombre || len(b) > MaxNombre {
		t.Fatalf("nombres de más de 63: %d %d", len(a), len(b))
	}
	if a == b {
		t.Fatalf("dos ramas largas dieron el mismo nombre: %s", a)
	}
	if !strings.HasPrefix(a, "tienda_online__feature_aaa") {
		t.Fatalf("nombre largo: %s", a)
	}
	if NombreConHash("tienda", "feature/a-b") == NombreConHash("tienda", "feature/a_b") {
		t.Fatal("NombreConHash debe distinguir ramas con el mismo sufijo")
	}
}

func TestCadenas(t *testing.T) {
	u := "postgres://ana:cl%40ve@localhost:5432/tienda?sslmode=disable"
	if b, _ := BaseDeURL(u); b != "tienda" {
		t.Fatalf("BaseDeURL = %q", b)
	}
	got, _ := ConBase(u, "tienda__x")
	if got != "postgres://ana:cl%40ve@localhost:5432/tienda__x?sslmode=disable" {
		t.Fatalf("ConBase URL = %q", got)
	}
	dsn := "host=localhost user=ana dbname=tienda sslmode=disable"
	if b, _ := BaseDeURL(dsn); b != "tienda" {
		t.Fatalf("BaseDeURL dsn = %q", b)
	}
	if got, _ := ConBase(dsn, "tienda__x"); got != "host=localhost user=ana dbname=tienda__x sslmode=disable" {
		t.Fatalf("ConBase dsn = %q", got)
	}
	if got, _ := ConBase("dbname=tienda host=x", "otra"); got != "dbname=otra host=x" {
		t.Fatalf("ConBase dsn al inicio = %q", got)
	}
	if _, err := BaseDeURL("postgres://localhost:5432"); err == nil {
		t.Fatal("una URL sin base debe dar error")
	}
}

func TestCopiarYBorrar(t *testing.T) {
	ctx := context.Background()
	db, url := pruebabd.Abrir(t)
	pruebabd.Exec(t, db, `CREATE TABLE clientes (id int PRIMARY KEY, nombre text); INSERT INTO clientes VALUES (1, 'Ana')`)
	db.Close() // PostgreSQL no copia una base con conexiones abiertas

	base, _ := BaseDeURL(url)
	a, err := AbrirAdmin(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	copia := Nombre(base, "feature/facturas")
	t.Cleanup(func() { _, _ = a.db.Exec(`DROP DATABASE IF EXISTS ` + copia + ` WITH (FORCE)`) })

	if err := a.Copiar(ctx, base, copia, "feature/facturas"); err != nil {
		t.Fatal(err)
	}
	if m, _ := a.Marca(ctx, copia); m != "feature/facturas" {
		t.Fatalf("marca = %q", m)
	}
	// La copia trae los datos.
	cu, _ := ConBase(url, copia)
	cdb, _ := sql.Open("postgres", cu)
	var nombre string
	if err := cdb.QueryRow(`SELECT nombre FROM clientes WHERE id = 1`).Scan(&nombre); err != nil || nombre != "Ana" {
		t.Fatalf("la copia no trae los datos: %q %v", nombre, err)
	}

	// Protección: nunca borra la base principal ni una base sin la marca de Marga.
	if err := a.Borrar(ctx, base, base, true); err == nil {
		t.Fatal("Borrar aceptó la base principal")
	}
	if err := a.Borrar(ctx, base, "otra", true); !errors.Is(err, ErrSinMarca) {
		t.Fatalf("Borrar de una base sin marca: %v", err)
	}
	if ok, _ := a.Existe(ctx, base); !ok {
		t.Fatal("se borró la base principal")
	}

	// Ocupada: con alguien conectado no se borra (salvo forzando).
	if err := cdb.Ping(); err != nil {
		t.Fatal(err)
	}
	var oc *ErrOcupada
	if err := a.Borrar(ctx, copia, base, false); !errors.As(err, &oc) || len(oc.Conexiones) == 0 {
		t.Fatalf("Borrar con conexiones abiertas: %v", err)
	}
	if err := a.Borrar(ctx, copia, base, true); err != nil {
		t.Fatalf("Borrar forzado: %v", err)
	}
	cdb.Close()
	if ok, _ := a.Existe(ctx, copia); ok {
		t.Fatal("la copia sigue existiendo")
	}
}

func TestCopiarOcupada(t *testing.T) {
	ctx := context.Background()
	db, url := pruebabd.Abrir(t)
	if err := db.Ping(); err != nil { // queda una conexión abierta, como pgAdmin
		t.Fatal(err)
	}
	base, _ := BaseDeURL(url)
	a, err := AbrirAdmin(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	err = a.Copiar(ctx, base, Nombre(base, "x"), "x")
	var oc *ErrOcupada
	if !errors.As(err, &oc) || len(oc.Conexiones) == 0 || oc.Base != base {
		t.Fatalf("Copiar con la base ocupada: %v", err)
	}
	if ok, _ := a.Existe(ctx, Nombre(base, "x")); ok {
		t.Fatal("se creó la copia a pesar del error")
	}
	// Vacía sí se puede.
	t.Cleanup(func() { _, _ = a.db.Exec(`DROP DATABASE IF EXISTS ` + Nombre(base, "x")) })
	if err := a.CrearVacia(ctx, Nombre(base, "x"), "x"); err != nil {
		t.Fatal(err)
	}
}
