package migrador

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/introspeccion"
	"github.com/zara-te05/Marga/internal/proyecto"
	"github.com/zara-te05/Marga/internal/pruebabd"
)

func migDown(v, n, up, down string) proyecto.Migracion {
	m := mig(v, n, up)
	m.Down = down
	return m
}

func TestSubirBajarYDrift(t *testing.T) {
	db, url := pruebabd.Abrir(t)
	ctx := context.Background()
	m, err := Abrir(ctx, proyecto.Entorno{URL: url, Motor: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	migs := []proyecto.Migracion{
		migDown("0001", "clientes", "CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL);", "DROP TABLE clientes;"),
		migDown("0002", "email", "ALTER TABLE clientes ADD COLUMN email text;", "ALTER TABLE clientes DROP COLUMN email;"),
	}
	var eventos []string
	ev := func(mg proyecto.Migracion, _ time.Duration, snap *introspeccion.Esquema, err error) {
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
	for _, f := range Comparar(migs, hist) {
		if f.Situacion != Aplicada_ {
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
	filas := Comparar([]proyecto.Migracion{mig("0007", "clientes", migs[0].Up)}, func() []Aplicada { h, _ := m.Historial(ctx); return h }())
	if filas[0].Situacion != Renumerada {
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
	if _, err := Abrir(context.Background(), proyecto.Entorno{URL: "x", Motor: "mysql"}); err == nil {
		t.Fatal("mysql todavía no está soportado")
	}
}
