// Package pruebabd crea bases de datos PostgreSQL desechables para las pruebas de integración.
//
// Las pruebas que la usan se saltan si no existe MARGA_TEST_DB_URL, así `go test ./...`
// funciona en cualquier máquina. Con PostgreSQL a la mano:
//
//	MARGA_TEST_DB_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" go test ./...
//
// El usuario de esa URL necesita permiso para crear bases (CREATEDB).
package pruebabd

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Nueva crea una base vacía, devuelve su URL y la borra al terminar la prueba.
func Nueva(t testing.TB) string {
	t.Helper()
	base := os.Getenv("MARGA_TEST_DB_URL")
	if base == "" {
		t.Skip("define MARGA_TEST_DB_URL para correr las pruebas contra PostgreSQL")
	}
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	nombre := "marga_prueba_" + hex.EncodeToString(b)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+nombre); err != nil {
		admin.Close()
		t.Fatalf("no se pudo crear la base de prueba (¿el usuario tiene CREATEDB?): %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + nombre
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+nombre+` WITH (FORCE)`)
		admin.Close()
	})
	return u.String()
}

// Abrir crea una base nueva y devuelve la conexión (se cierra sola).
func Abrir(t testing.TB) (*sql.DB, string) {
	t.Helper()
	u := Nueva(t)
	db, err := sql.Open("postgres", u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, u
}

// Exec ejecuta SQL y falla la prueba si hay error.
func Exec(t testing.TB, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}
