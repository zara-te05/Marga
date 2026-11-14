package riesgo

import (
	"slices"
	"testing"
)

func reglas(hs []Hallazgo) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Regla)
	}
	return out
}

func TestReglas(t *testing.T) {
	casos := []struct {
		nombre   string
		up, down string
		quiero   []string // reglas que deben aparecer
		noQuiero []string // reglas que NO deben aparecer
	}{
		{"borrar tabla", "DROP TABLE clientes;", "x", []string{"borra-tabla"}, nil},
		{"truncate", "TRUNCATE pedidos;", "x", []string{"truncate"}, nil},
		{"delete sin where", "DELETE FROM pedidos;", "x", []string{"delete-sin-where"}, nil},
		{"delete con where", "DELETE FROM pedidos WHERE id = 1;", "x", nil, []string{"delete-sin-where"}},
		{"update sin where", "UPDATE clientes SET activo = true;", "x", []string{"update-sin-where"}, nil},
		{"update en tabla nueva", "CREATE TABLE t (a int);\nUPDATE t SET a = 1;", "x", nil, []string{"update-sin-where"}},
		{"borrar columna", "ALTER TABLE clientes DROP COLUMN telefono;", "x", []string{"borra-columna"}, nil},
		{"drop default no es columna", "ALTER TABLE clientes ALTER COLUMN activo DROP DEFAULT;", "x", nil, []string{"borra-columna"}},
		{"drop constraint no es columna", "ALTER TABLE clientes DROP CONSTRAINT clientes_email_key;", "x", nil, []string{"borra-columna"}},
		{"not null sin default", "ALTER TABLE clientes ADD COLUMN rfc text NOT NULL;", "x", []string{"not-null-sin-default"}, nil},
		{"not null con default", "ALTER TABLE clientes ADD COLUMN activo boolean NOT NULL DEFAULT true;", "x", nil, []string{"not-null-sin-default"}},
		{"cambio de tipo", "ALTER TABLE clientes ALTER COLUMN nombre TYPE varchar(200);", "x", []string{"cambio-de-tipo"}, nil},
		{"set not null", "ALTER TABLE clientes ALTER COLUMN email SET NOT NULL;", "x", []string{"set-not-null"}, nil},
		{"fk valida todo", "ALTER TABLE pedidos ADD CONSTRAINT fk FOREIGN KEY (cliente_id) REFERENCES clientes(id);", "x", []string{"fk-validacion"}, nil},
		{"fk not valid", "ALTER TABLE pedidos ADD CONSTRAINT fk FOREIGN KEY (cliente_id) REFERENCES clientes(id) NOT VALID;", "x", nil, []string{"fk-validacion"}},
		{"indice bloqueante", "CREATE INDEX idx ON pedidos (fecha);", "x", []string{"indice-bloqueante"}, nil},
		{"concurrently en transacción", "CREATE INDEX CONCURRENTLY idx ON pedidos (fecha);", "x", []string{"concurrently-en-transaccion"}, []string{"indice-bloqueante"}},
		{"concurrently sin transacción", "-- marga:sin-transaccion\nCREATE INDEX CONCURRENTLY idx ON pedidos (fecha);", "x", nil, []string{"concurrently-en-transaccion"}},
		{"renombrar", "ALTER TABLE clientes RENAME COLUMN nombre TO nombre_completo;", "x", []string{"renombrar"}, nil},
		{"sin down", "CREATE TABLE t (a int);", "-- nada\n", []string{"sin-down"}, nil},
		{"tabla nueva es segura", "CREATE TABLE t (a int NOT NULL);\nCREATE INDEX i ON t (a);", "DROP TABLE t;", nil, []string{"indice-bloqueante", "not-null-sin-default", "sin-down"}},
		{"texto entre comillas no cuenta", "INSERT INTO log VALUES ('DROP TABLE clientes; DELETE FROM x');", "x", nil, []string{"borra-tabla", "delete-sin-where"}},
		{"cuerpo $$ no cuenta", "CREATE FUNCTION f() RETURNS void AS $$ BEGIN DELETE FROM x; END $$ LANGUAGE plpgsql;", "x", nil, []string{"delete-sin-where"}},
		{"comentario no cuenta", "-- DROP TABLE clientes;\nSELECT 1;", "x", nil, []string{"borra-tabla"}},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := reglas(Analizar(c.up, c.down))
			for _, r := range c.quiero {
				if !slices.Contains(got, r) {
					t.Errorf("faltó %q; obtuve %v", r, got)
				}
			}
			for _, r := range c.noQuiero {
				if slices.Contains(got, r) {
					t.Errorf("no debía aparecer %q; obtuve %v", r, got)
				}
			}
		})
	}
}

func TestNivelYOrden(t *testing.T) {
	hs := Analizar("CREATE INDEX i ON a (x);\nDROP TABLE b;", "")
	if Nivel(hs) != "alto" {
		t.Fatalf("nivel = %q", Nivel(hs))
	}
	if hs[0].Nivel != "alto" || hs[len(hs)-1].Nivel != "bajo" {
		t.Fatalf("los hallazgos deben ir de mayor a menor: %v", hs)
	}
	if hs[0].Linea != 2 {
		t.Fatalf("DROP TABLE está en la línea 2, no en %d", hs[0].Linea)
	}
	if Nivel(nil) != "" {
		t.Fatal("sin hallazgos el nivel es vacío")
	}
}

func TestTablasAfectadas(t *testing.T) {
	got := TablasAfectadas(`CREATE TABLE facturas (id int);
ALTER TABLE public.clientes ADD COLUMN rfc text;
CREATE INDEX i ON pedidos (fecha);
INSERT INTO log VALUES ('ALTER TABLE secreta');`)
	for _, q := range []string{"facturas", "clientes", "pedidos"} {
		if !slices.ContainsFunc(got, func(s string) bool { return s == q || s == "public."+q }) {
			t.Errorf("faltó %s en %v", q, got)
		}
	}
	if slices.ContainsFunc(got, func(s string) bool { return s == "secreta" }) {
		t.Errorf("no debe leer dentro de cadenas: %v", got)
	}
}
