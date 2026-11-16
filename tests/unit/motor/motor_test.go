package motor_test

import (
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/datos/motor"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

func TestElegirMotor(t *testing.T) {
	for _, n := range []string{"", "postgresql"} {
		m, err := motor.De(n)
		if err != nil || m.Nombre() != "postgresql" || m.Driver() != "postgres" || !m.DDLEnTransaccion() {
			t.Fatalf("%q: %v %v", n, m, err)
		}
	}
	_, err := motor.De("mysql")
	if err == nil || !strings.Contains(err.Error(), "todavía no está soportado") || !strings.Contains(err.Error(), "postgresql") {
		t.Fatalf("un motor sin implementar debe decir cuáles hay: %v", err)
	}
	// El DDL sale del dialecto del motor.
	m, _ := motor.De("postgresql")
	ddl := m.DDLTabla(esquema.Tabla{Nombre: "t", PK: []string{"id"}, Columnas: []esquema.Columna{{Nombre: "id", Tipo: "integer"}}})
	if !strings.Contains(ddl, "CREATE TABLE t") || !strings.Contains(ddl, "PRIMARY KEY (id)") {
		t.Fatalf("DDL: %s", ddl)
	}
}

func TestCompararEsNeutral(t *testing.T) {
	a := esquema.Esquema{Vistas: []esquema.Vista{{Nombre: "v", Definicion: " SELECT 1"}}}
	b := esquema.Esquema{Vistas: []esquema.Vista{{Nombre: "v", Definicion: " SELECT 2"}}}
	if d := esquema.Comparar(a, b); len(d) != 1 || !strings.Contains(d[0], "~ vista v") {
		t.Fatalf("%v", d)
	}
	if !esquema.MismaVista(a.Vistas[0], a.Vistas[0]) || esquema.MismaVista(a.Vistas[0], b.Vistas[0]) {
		t.Fatal("MismaVista")
	}
}
