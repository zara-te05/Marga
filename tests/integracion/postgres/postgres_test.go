package postgres_test

import (
	"context"
	"encoding/json"
	"github.com/zara-te05/Marga/internal/datos/motor/postgres"
	"slices"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/modelos/esquema"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

// Ida y vuelta: dado un esquema A y uno B, el UP generado debe convertir A en B
// y el DOWN debe regresar B a A, sin que introspección vea diferencias.
func TestIdaYVuelta(t *testing.T) {
	db, _ := pruebabd.Abrir(t)
	ctx := context.Background()
	leer := func() esquema.Esquema {
		t.Helper()
		e, err := postgres.Leer(ctx, db)
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

	gen := postgres.Generar(a, b)
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
	if d := esquema.Comparar(a, leer()); len(d) != 0 {
		t.Fatalf("el DOWN no regresó a A:\n%s\n%v", gen.Down, d)
	}
	// A → B con el UP
	pruebabd.Exec(t, db, gen.Up)
	if d := esquema.Comparar(b, leer()); len(d) != 0 {
		t.Fatalf("el UP no llegó a B:\n%s\n%v", gen.Up, d)
	}
	// Sin cambios no genera nada.
	if g := postgres.Generar(b, b); strings.TrimSpace(g.Up) != "" {
		t.Fatalf("sin cambios generó:\n%s", g.Up)
	}
}

// Ida y vuelta con funciones, vistas, triggers, ENUM y CHECK: una vista sobre una
// columna que cambia de tipo, vistas encadenadas, un trigger que llama a una función,
// una función que cambia de tipo de retorno (DROP + CREATE) y otra que solo cambia
// su cuerpo (CREATE OR REPLACE).
func TestIdaYVueltaObjetos(t *testing.T) {
	db, _ := pruebabd.Abrir(t)
	ctx := context.Background()
	leer := func() esquema.Esquema {
		t.Helper()
		e, err := postgres.Leer(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}

	pruebabd.Exec(t, db, `
CREATE TYPE estado AS ENUM ('nuevo', 'pagado');
CREATE TYPE viejo_tipo AS ENUM ('a', 'b');
CREATE TABLE clientes (id serial PRIMARY KEY, nombre text NOT NULL);
CREATE TABLE pedidos (
  id serial PRIMARY KEY,
  cliente_id int REFERENCES clientes(id),
  total numeric(10,2) CONSTRAINT total_positivo CHECK (total >= 0),
  estado estado NOT NULL DEFAULT 'nuevo',
  actualizado timestamptz
);
CREATE FUNCTION calcular_total(p_cliente integer) RETURNS numeric LANGUAGE sql STABLE
  AS $$ SELECT coalesce(sum(total), 0) FROM pedidos WHERE cliente_id = p_cliente $$;
CREATE FUNCTION calcular_total(p_cliente integer, p_desde date) RETURNS numeric LANGUAGE sql
  AS $$ SELECT 0::numeric $$;
CREATE FUNCTION tocar() RETURNS trigger LANGUAGE plpgsql
  AS $$ BEGIN NEW.actualizado := now(); RETURN NEW; END $$;
CREATE TRIGGER pedidos_tocar BEFORE UPDATE ON pedidos FOR EACH ROW EXECUTE FUNCTION tocar();
CREATE TRIGGER pedidos_total BEFORE UPDATE OF total ON pedidos FOR EACH ROW EXECUTE FUNCTION tocar();
CREATE VIEW v_ids AS SELECT id, cliente_id FROM pedidos;
CREATE VIEW v_pedidos AS SELECT p.id, p.total, c.nombre FROM pedidos p JOIN clientes c ON c.id = p.cliente_id;
CREATE VIEW v_grandes AS SELECT id, total FROM v_pedidos WHERE total > 100;
CREATE VIEW v_saldos AS SELECT c.id, calcular_total(c.id) AS saldo FROM clientes c;
CREATE VIEW v_viejo AS SELECT 1 AS x;
CREATE MATERIALIZED VIEW mv_totales AS SELECT cliente_id, sum(total) AS suma FROM pedidos GROUP BY cliente_id;
CREATE UNIQUE INDEX mv_totales_idx ON mv_totales (cliente_id);
CREATE PROCEDURE limpiar() LANGUAGE sql AS $$ DELETE FROM pedidos WHERE total = 0 $$;`)
	a := leer()
	if len(a.Funciones) != 4 || len(a.Vistas) != 6 || len(a.Triggers) != 2 || len(a.Tipos) != 2 {
		t.Fatalf("introspección incompleta: %d funciones, %d vistas, %d triggers, %d tipos",
			len(a.Funciones), len(a.Vistas), len(a.Triggers), len(a.Tipos))
	}
	if a.BuscarFuncion("funcion:calcular_total(integer)") == nil || a.BuscarFuncion("funcion:calcular_total(integer, date)") == nil {
		t.Fatalf("las dos calcular_total deben ser funciones distintas: %+v", a.Funciones)
	}
	if v := a.BuscarVista("v_grandes"); v == nil || !slices.Contains(v.Depende, "vista:v_pedidos") {
		t.Fatalf("v_grandes debe depender de v_pedidos: %+v", v)
	}
	if v := a.BuscarVista("v_ids"); v == nil || !slices.Contains(v.Depende, "columna:pedidos.cliente_id") || slices.Contains(v.Depende, "columna:pedidos.total") {
		t.Fatalf("v_ids debe registrar las columnas que usa: %+v", v)
	}
	if tr := a.Triggers[0]; tr.Funcion != "funcion:tocar()" {
		t.Fatalf("el trigger debe apuntar a tocar(): %+v", tr)
	}

	pruebabd.Exec(t, db, `
DROP VIEW v_grandes; DROP VIEW v_pedidos; DROP MATERIALIZED VIEW mv_totales; DROP VIEW v_viejo;
DROP TRIGGER pedidos_total ON pedidos;
ALTER TABLE pedidos ALTER COLUMN total TYPE numeric(12,2);
CREATE TRIGGER pedidos_total BEFORE UPDATE OF total ON pedidos FOR EACH ROW EXECUTE FUNCTION tocar();
CREATE VIEW v_pedidos AS SELECT p.id, p.total, c.nombre FROM pedidos p JOIN clientes c ON c.id = p.cliente_id;
CREATE VIEW v_grandes AS SELECT id, total FROM v_pedidos WHERE total > 1000;
CREATE MATERIALIZED VIEW mv_totales AS SELECT cliente_id, sum(total) AS suma FROM pedidos GROUP BY cliente_id;
CREATE UNIQUE INDEX mv_totales_idx ON mv_totales (cliente_id);

DROP VIEW v_saldos; DROP FUNCTION calcular_total(integer);
CREATE FUNCTION calcular_total(p_cliente integer) RETURNS bigint LANGUAGE sql STABLE
  AS $$ SELECT count(*) FROM pedidos WHERE cliente_id = p_cliente $$;
CREATE VIEW v_saldos AS SELECT c.id, calcular_total(c.id) AS saldo FROM clientes c;

CREATE OR REPLACE FUNCTION tocar() RETURNS trigger LANGUAGE plpgsql
  AS $$ BEGIN NEW.actualizado := clock_timestamp(); RETURN NEW; END $$;

CREATE TYPE prioridad AS ENUM ('baja', 'alta');
ALTER TABLE pedidos ADD COLUMN prioridad prioridad;
DROP TYPE viejo_tipo;
ALTER TABLE clientes ADD CONSTRAINT nombre_no_vacio CHECK (length(nombre) > 0);
ALTER TABLE pedidos DROP CONSTRAINT total_positivo;
ALTER TABLE pedidos ADD CONSTRAINT total_positivo CHECK (total > 0);

CREATE FUNCTION validar() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
  AS $$ BEGIN IF NEW.nombre = '' THEN RAISE EXCEPTION 'vacío'; END IF; RETURN NEW; END $$;
CREATE TRIGGER clientes_validar BEFORE INSERT ON clientes FOR EACH ROW EXECUTE FUNCTION validar();
CREATE VIEW v_clientes AS SELECT id, nombre FROM clientes;
CREATE TRIGGER v_clientes_ins INSTEAD OF INSERT ON v_clientes FOR EACH ROW EXECUTE FUNCTION validar();
DROP PROCEDURE limpiar();`)
	b := leer()

	gen := postgres.Generar(a, b)
	t.Logf("UP:\n%s\nDOWN:\n%s\nAvisos: %v", gen.Up, gen.Down, gen.Avisos)

	// El orden importa: triggers y vistas fuera → cambios → vistas y triggers dentro.
	enOrden(t, gen.Up, "DROP TRIGGER pedidos_total", "DROP VIEW v_grandes", "DROP VIEW v_pedidos",
		"ALTER TABLE pedidos ALTER COLUMN total TYPE", "CREATE VIEW v_pedidos", "CREATE VIEW v_grandes", "CREATE TRIGGER pedidos_total")
	// Lo que no toca la columna que cambia se queda como está.
	for _, no := range []string{"DROP VIEW v_ids", "DROP TRIGGER pedidos_tocar"} {
		if strings.Contains(gen.Up, no) {
			t.Errorf("no hacía falta %q:\n%s", no, gen.Up)
		}
	}
	enOrden(t, gen.Up, "DROP VIEW v_saldos", "DROP FUNCTION calcular_total(p_cliente integer)",
		"CREATE OR REPLACE FUNCTION public.calcular_total(p_cliente integer)", "CREATE VIEW v_saldos")
	enOrden(t, gen.Up, "CREATE TYPE prioridad", "ADD COLUMN prioridad", "DROP TYPE viejo_tipo")
	enOrden(t, gen.Up, "CREATE VIEW v_clientes", "CREATE TRIGGER v_clientes_ins")
	if strings.Contains(gen.Up, "DROP FUNCTION tocar") {
		t.Errorf("tocar() solo cambió su cuerpo: debe ir con CREATE OR REPLACE, sin DROP")
	}

	pruebabd.Exec(t, db, gen.Down)
	if d := esquema.Comparar(a, leer()); len(d) != 0 {
		t.Fatalf("el DOWN no regresó a A:\n%s\n%v", gen.Down, d)
	}
	pruebabd.Exec(t, db, gen.Up)
	if d := esquema.Comparar(b, leer()); len(d) != 0 {
		t.Fatalf("el UP no llegó a B:\n%s\n%v", gen.Up, d)
	}
	if g := postgres.Generar(b, b); strings.TrimSpace(g.Up) != "" {
		t.Fatalf("sin cambios generó:\n%s", g.Up)
	}
}

// ENUM: agregar valores se genera (en su posición); quitarlos solo se avisa.
func TestEnumValores(t *testing.T) {
	db, _ := pruebabd.Abrir(t)
	ctx := context.Background()
	pruebabd.Exec(t, db, `CREATE TYPE estado AS ENUM ('nuevo', 'pagado');`)
	a, _ := postgres.Leer(ctx, db)
	pruebabd.Exec(t, db, `ALTER TYPE estado ADD VALUE 'enviado' BEFORE 'pagado'; ALTER TYPE estado ADD VALUE 'cancelado';`)
	b, _ := postgres.Leer(ctx, db)

	gen := postgres.Generar(a, b)
	if !strings.Contains(gen.Up, `ADD VALUE 'enviado' BEFORE 'pagado'`) || !strings.Contains(gen.Up, `ADD VALUE 'cancelado';`) {
		t.Fatalf("UP:\n%s", gen.Up)
	}
	if !strings.Contains(gen.Down, "-- marga: quita a mano el valor 'enviado'") || strings.Contains(gen.Down, "ALTER") {
		t.Fatalf("el DOWN debe pedir quitar el valor a mano, sin SQL:\n%s", gen.Down)
	}
	if g := postgres.Generar(b, a); len(g.Avisos) != 2 {
		t.Fatalf("quitar dos valores debe dar dos avisos: %v", g.Avisos)
	}

	// Aplicar el UP sobre una base en A deja el tipo igual que B.
	db2, _ := pruebabd.Abrir(t)
	pruebabd.Exec(t, db2, `CREATE TYPE estado AS ENUM ('nuevo', 'pagado');`)
	pruebabd.Exec(t, db2, gen.Up)
	b2, _ := postgres.Leer(ctx, db2)
	if d := esquema.Comparar(b, b2); len(d) != 0 {
		t.Fatalf("el UP no llegó a B: %v", d)
	}
}

// Una línea base vieja (version 1: sin funciones, vistas, triggers, ENUM ni CHECK)
// no debe reportar los objetos que ya existían como cambios.
func TestCompatibilidadLineaBaseVieja(t *testing.T) {
	db, _ := pruebabd.Abrir(t)
	ctx := context.Background()
	pruebabd.Exec(t, db, `
CREATE TYPE estado AS ENUM ('nuevo');
CREATE TABLE t (id int PRIMARY KEY, n int CHECK (n > 0), e estado);
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f();
CREATE VIEW v AS SELECT id FROM t;`)
	real, _ := postgres.Leer(ctx, db)

	// Así se veía el JSON que guardaba Marga antes de esta versión.
	viejo := `{"motor":"postgresql","tablas":[{"nombre":"t","columnas":[
		{"nombre":"id","tipo":"integer","nulo":false},{"nombre":"n","tipo":"integer","nulo":true},
		{"nombre":"e","tipo":"estado","nulo":true}],"pk":["id"],"pk_nombre":"t_pkey","fks":[],"indices":[]}]}`
	var base esquema.Esquema
	if err := json.Unmarshal([]byte(viejo), &base); err != nil {
		t.Fatal(err)
	}
	if base.Version != 0 {
		t.Fatalf("version = %d", base.Version)
	}
	if d := esquema.Comparar(base, real); len(d) == 0 {
		t.Fatal("sin completar, la base vieja debería verse distinta (es lo que hay que evitar)")
	}
	completa, inc := esquema.Completar(base, real)
	if n := len(inc); n != 5 { // 1 CHECK + 1 tipo + 1 función + 1 vista + 1 trigger
		t.Errorf("se incorporaron %d objetos, esperaba 5", n)
	}
	if d := esquema.Comparar(completa, real); len(d) != 0 {
		t.Fatalf("la base completada no debe tener drift: %v", d)
	}
	if completa.Version != esquema.VersionActual {
		t.Fatalf("version = %d", completa.Version)
	}
	// Ya completada, no se vuelve a tocar: un cambio nuevo sí es drift.
	pruebabd.Exec(t, db, `CREATE VIEW v2 AS SELECT 1 AS x;`)
	real2, _ := postgres.Leer(ctx, db)
	if otra, inc := esquema.Completar(completa, real2); len(inc) != 0 || len(esquema.Comparar(otra, real2)) != 1 {
		t.Fatalf("una base v2 no se completa: inc=%v %v", inc, esquema.Comparar(otra, real2))
	}
}

func enOrden(t *testing.T, sql string, partes ...string) {
	t.Helper()
	pos := -1
	for _, p := range partes {
		i := strings.Index(sql, p)
		if i < 0 {
			t.Errorf("falta %q en:\n%s", p, sql)
			return
		}
		if i < pos {
			t.Errorf("%q aparece antes de lo que debe (orden esperado: %v)", p, partes)
			return
		}
		pos = i
	}
}
