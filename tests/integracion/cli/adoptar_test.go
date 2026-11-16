package cli_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/tests/apoyo/clitest"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

// Una base que ya existía antes de Marga, con lo que suele traer una base real:
// otro esquema, una extensión, una secuencia suelta, funciones, vistas y un trigger.
const esquemaLegado = `
CREATE EXTENSION IF NOT EXISTS citext;
CREATE SCHEMA contabilidad;
CREATE SEQUENCE folio_factura START 1000;
CREATE TYPE estado AS ENUM ('activo', 'cerrado');
CREATE TABLE clientes (id serial PRIMARY KEY, correo citext UNIQUE, estado estado NOT NULL DEFAULT 'activo',
  actualizado timestamptz);
CREATE TABLE contabilidad.facturas (id serial PRIMARY KEY, cliente_id int REFERENCES clientes(id),
  folio int NOT NULL DEFAULT nextval('folio_factura'), total numeric(10,2) CHECK (total >= 0));
CREATE FUNCTION tocar() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.actualizado := now(); RETURN NEW; END $$;
CREATE TRIGGER clientes_tocar BEFORE UPDATE ON clientes FOR EACH ROW EXECUTE FUNCTION tocar();
CREATE VIEW v_activos AS SELECT id, correo FROM clientes WHERE estado = 'activo';
INSERT INTO clientes (correo) VALUES ('ana@ejemplo.com');`

func TestAdoptarBaseExistente(t *testing.T) {
	legado, companero, vacia := pruebabd.Nueva(t), pruebabd.Nueva(t), pruebabd.Nueva(t)
	ejecutar(t, legado, esquemaLegado)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	dir := t.TempDir()
	t.Chdir(dir)
	clitest.DebeCorrer(t, "init")
	clitest.DebeCorrer(t, "env", "add", "dev", legado)

	// status la reconoce como base con esquema y sugiere adoptar.
	if out := clitest.DebeCorrer(t, "status"); !strings.Contains(out, "marga adoptar") {
		t.Errorf("status debía sugerir marga adoptar:\n%s", out)
	}
	out := clitest.DebeCorrer(t, "adoptar")
	for _, quiero := range []string{"la 0001 reconstruye el esquema igual", "el DOWN deja la base vacía",
		"CREATE EXTENSION IF NOT EXISTS citext", "CREATE SCHEMA IF NOT EXISTS contabilidad", "START WITH 1000"} {
		if !strings.Contains(out, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "migrations", "0001_esquema_inicial.up.sql")); err != nil {
		t.Fatal("no escribió la 0001")
	}
	// En la base original ya cuenta como aplicada y no hay drift; los datos siguen ahí.
	if out := clitest.DebeCorrer(t, "drift"); !strings.Contains(out, "Sin drift") {
		t.Errorf("drift tras adoptar:\n%s", out)
	}
	if n := contarFilas(t, legado, "SELECT count(*) FROM clientes"); n != 1 {
		t.Errorf("adoptar no debe tocar los datos: %d clientes", n)
	}
	// Alguien que clona el proyecto reconstruye la base desde cero.
	clitest.DebeCorrer(t, "env", "add", "companero", companero)
	clitest.DebeCorrer(t, "up", "--env", "companero")
	if n := contarFilas(t, companero, "SELECT count(*) FROM information_schema.tables WHERE table_name IN ('clientes', 'facturas', 'v_activos')"); n != 3 {
		t.Errorf("la base del compañero debía tener las 3 tablas/vistas, tiene %d", n)
	}
	clitest.DebeCorrer(t, "verificar")

	// Solo al empezar: con migraciones ya no se puede.
	clitest.DebeFallar(t, "ya tiene 1 migración", "adoptar", "--env", "dev")
	// Una base vacía no tiene nada que adoptar.
	dir2 := t.TempDir()
	t.Chdir(dir2)
	clitest.DebeCorrer(t, "init")
	clitest.DebeCorrer(t, "env", "add", "dev", vacia)
	clitest.DebeFallar(t, "está vacía", "adoptar")
}

func ejecutar(t *testing.T, url, q string) {
	t.Helper()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func contarFilas(t *testing.T, url, q string) int {
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
