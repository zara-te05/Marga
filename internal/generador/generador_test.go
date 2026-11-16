package generador

import (
	"context"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/introspeccion"
	"github.com/zara-te05/Marga/internal/pruebabd"
)

// Ida y vuelta: dado un esquema A y uno B, el UP generado debe convertir A en B
// y el DOWN debe regresar B a A, sin que introspección vea diferencias.
func TestIdaYVuelta(t *testing.T) {
	db, _ := pruebabd.Abrir(t)
	ctx := context.Background()
	leer := func() introspeccion.Esquema {
		t.Helper()
		e, err := introspeccion.Leer(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}

	pruebabd.Exec(t, db, `
CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL, notas text);
CREATE TABLE pedidos (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  cliente_id int NOT NULL REFERENCES clientes(id) ON DELETE CASCADE,
  total numeric(10,2) DEFAULT 0
);
CREATE INDEX pedidos_cliente_idx ON pedidos (cliente_id);
CREATE TABLE temporal (x int);`)
	a := leer()

	pruebabd.Exec(t, db, `
ALTER TABLE clientes ADD COLUMN email varchar(200) UNIQUE;
ALTER TABLE clientes ALTER COLUMN notas TYPE varchar(500);
ALTER TABLE clientes ALTER COLUMN nombre DROP NOT NULL;
ALTER TABLE pedidos ALTER COLUMN total SET DEFAULT 1;
DROP INDEX pedidos_cliente_idx;
CREATE INDEX pedidos_total_idx ON pedidos (total DESC);
DROP TABLE temporal;
CREATE TABLE facturas (
  id serial PRIMARY KEY,
  pedido_id bigint REFERENCES pedidos(id),
  folio text NOT NULL DEFAULT ''
);`)
	b := leer()

	gen := Generar(a, b)
	if strings.TrimSpace(gen.Up) == "" || strings.TrimSpace(gen.Down) == "" {
		t.Fatalf("generó vacío:\n%s\n---\n%s", gen.Up, gen.Down)
	}
	for _, s := range []string{"facturas", "email", "DROP TABLE", "pedidos_total_idx"} {
		if !strings.Contains(gen.Up, s) {
			t.Errorf("el UP debería mencionar %q:\n%s", s, gen.Up)
		}
	}

	// B → A con el DOWN
	pruebabd.Exec(t, db, gen.Down)
	if d := introspeccion.Comparar(a, leer()); len(d) != 0 {
		t.Fatalf("el DOWN no regresó a A:\n%s\n%v", gen.Down, d)
	}
	// A → B con el UP
	pruebabd.Exec(t, db, gen.Up)
	if d := introspeccion.Comparar(b, leer()); len(d) != 0 {
		t.Fatalf("el UP no llegó a B:\n%s\n%v", gen.Up, d)
	}
	// Sin cambios no genera nada.
	if g := Generar(b, b); strings.TrimSpace(g.Up) != "" {
		t.Fatalf("sin cambios generó:\n%s", g.Up)
	}
}
