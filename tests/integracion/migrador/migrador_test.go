package migrador_test

import (
	"context"
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

func mig(v, n, up string) proyecto.Migracion {
	return proyecto.Migracion{Version: v, Nombre: n, Up: up, Checksum: proyecto.Checksum(up)}
}

func migDown(v, n, up, down string) proyecto.Migracion {
	m := mig(v, n, up)
	m.Down = down
	return m
}

func TestSubirBajarYDrift(t *testing.T) {
	db, url := pruebabd.Abrir(t)
	ctx := context.Background()
	m, err := migrador.Abrir(ctx, proyecto.Entorno{URL: url, Motor: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	migs := []proyecto.Migracion{
		migDown("0001", "clientes", "CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL);", "DROP TABLE clientes;"),
		migDown("0002", "email", "ALTER TABLE clientes ADD COLUMN email text;", "ALTER TABLE clientes DROP COLUMN email;"),
	}
	var eventos []string
	ev := func(mg proyecto.Migracion, _ time.Duration, snap *esquema.Esquema, err error) {
		if err == nil && snap == nil {
			t.Errorf("%s: sin snapshot", mg.ID())
		}
		eventos = append(eventos, mg.ID())
	}
	if err := m.Subir(ctx, migs, "prueba", ev); err != nil {
		t.Fatal(err)
	}
	if strings.Join(eventos, ",") != "0001_clientes,0002_email" {
		t.Fatalf("eventos = %v", eventos)
	}
	hist, err := m.Historial(ctx)
	if err != nil || len(hist) != 2 || hist[1].AplicadaPor != "prueba" {
		t.Fatalf("historial = %+v, %v", hist, err)
	}
	for _, f := range migrador.Comparar(migs, hist) {
		if f.Situacion != migrador.Aplicada_ {
			t.Fatalf("%s: %s", f.ID(), f.Situacion)
		}
	}

	// Sin cambios manuales no hay drift.
	if cambios, _, _, err := m.Drift(ctx); err != nil || len(cambios) != 0 {
		t.Fatalf("drift inesperado: %v %v", cambios, err)
	}
	// Alguien cambia la base a mano: hay drift, hasta que se acepta.
	pruebabd.Exec(t, db, "ALTER TABLE clientes ADD COLUMN telefono text")
	cambios, _, _, err := m.Drift(ctx)
	if err != nil || len(cambios) == 0 || !strings.Contains(strings.Join(cambios, " "), "telefono") {
		t.Fatalf("debió detectar telefono: %v %v", cambios, err)
	}
	if _, err := m.AceptarEsquema(ctx); err != nil {
		t.Fatal(err)
	}
	if cambios, _, _, _ := m.Drift(ctx); len(cambios) != 0 {
		t.Fatalf("tras aceptar no debe haber drift: %v", cambios)
	}

	// Una migración que falla no deja nada a medias (transacción).
	mala := migDown("0003", "mala", "ALTER TABLE clientes ADD COLUMN x int; SELECT * FROM no_existe;", "")
	if err := m.Subir(ctx, []proyecto.Migracion{mala}, "prueba", nil); err == nil {
		t.Fatal("la migración mala debía fallar")
	}
	var existe bool
	_ = db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='clientes' AND column_name='x')`).Scan(&existe)
	if existe {
		t.Fatal("la columna x no debió quedar (rollback)")
	}
	if hist, _ := m.Historial(ctx); len(hist) != 2 {
		t.Fatalf("la migración fallida no debe registrarse: %d", len(hist))
	}

	// Bajar la última.
	if err := m.Bajar(ctx, []proyecto.Migracion{migs[1]}, nil); err != nil {
		t.Fatal(err)
	}
	if hist, _ := m.Historial(ctx); len(hist) != 1 {
		t.Fatalf("tras bajar queda %d", len(hist))
	}

	// Renumerar: 0001 pasa a ser 0007 en el historial.
	filas := migrador.Comparar([]proyecto.Migracion{mig("0007", "clientes", migs[0].Up)}, func() []migrador.Aplicada { h, _ := m.Historial(ctx); return h }())
	if filas[0].Situacion != migrador.Renumerada {
		t.Fatalf("esperaba renumerada: %+v", filas[0])
	}
	if err := m.Renumerar(ctx, filas); err != nil {
		t.Fatal(err)
	}
	if hist, _ := m.Historial(ctx); hist[0].Version != "0007" {
		t.Fatalf("historial tras renumerar: %+v", hist)
	}
}

func TestSoloPostgres(t *testing.T) {
	if _, err := migrador.Abrir(context.Background(), proyecto.Entorno{URL: "x", Motor: "mysql"}); err == nil {
		t.Fatal("mysql todavía no está soportado")
	}
}

// Una base cuya línea base guardó una versión anterior de Marga (sin funciones,
// vistas ni triggers): el primer drift no debe decir "+N objetos fuera de Marga".
func TestDriftConLineaBaseVieja(t *testing.T) {
	db, url := pruebabd.Abrir(t)
	ctx := context.Background()
	m, err := migrador.Abrir(ctx, proyecto.Entorno{URL: url, Motor: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	migs := []proyecto.Migracion{migDown("0001", "todo", `
CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL CHECK (nombre <> ''));
CREATE FUNCTION tocar() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER clientes_tocar BEFORE UPDATE ON clientes FOR EACH ROW EXECUTE FUNCTION tocar();
CREATE VIEW v_clientes AS SELECT id, nombre FROM clientes;`, "DROP VIEW v_clientes; DROP TABLE clientes; DROP FUNCTION tocar();")}
	if err := m.Subir(ctx, migs, "prueba", nil); err != nil {
		t.Fatal(err)
	}
	// Se reescribe la línea base como la habría guardado la versión 1.
	pruebabd.Exec(t, db, `UPDATE marga_esquema SET esquema = (esquema - 'version' - 'tipos' - 'funciones' - 'vistas' - 'triggers')
		|| jsonb_build_object('tablas', (SELECT jsonb_agg(t - 'checks') FROM jsonb_array_elements(esquema->'tablas') t))`)

	cambios, _, nueva, err := m.Drift(ctx)
	if err != nil || nueva {
		t.Fatal(err, nueva)
	}
	if len(cambios) != 0 {
		t.Fatalf("la línea base vieja no debe reportar drift: %v", cambios)
	}
	if len(m.Incorporados) != 4 { // CHECK + función + trigger + vista
		t.Fatalf("incorporados = %v", m.Incorporados)
	}
	// Ya quedó en version 2: un cambio de verdad sí se reporta, y no se vuelve a incorporar nada.
	m.Incorporados = nil
	pruebabd.Exec(t, db, `CREATE OR REPLACE FUNCTION tocar() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE NOTICE 'x'; RETURN NEW; END $$;`)
	cambios, _, _, err = m.Drift(ctx)
	if err != nil || len(cambios) != 1 || !strings.Contains(cambios[0], "función tocar()") || len(m.Incorporados) != 0 {
		t.Fatalf("cambios = %v, incorporados = %v, %v", cambios, m.Incorporados, err)
	}
}
