// Package postgres es el motor de PostgreSQL: lee el esquema real desde los
// catálogos (tablas, columnas, llaves, índices y CHECK; y en el esquema public
// también funciones, procedimientos, vistas, triggers y tipos ENUM), lo escribe
// como DDL y genera el SQL para pasar de un esquema a otro.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

// Tablas internas de Marga que no forman parte del esquema del usuario.
var internas = map[string]bool{"marga_history": true, "marga_esquema": true, "marga_scripts": true}

// Querier: una base o una transacción abierta (alias de una interfaz sin nombre,
// para que cualquier paquete pueda declarar la misma).
type Querier = interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const filtroEsquemas = `n.nspname NOT IN ('pg_catalog', 'information_schema')
	AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp%'`

func nombreTabla(esquema, tabla string) string {
	if esquema == "public" {
		return tabla
	}
	return esquema + "." + tabla
}

// Leer obtiene el esquema actual. Funciona dentro de una transacción
// (así se puede leer el esquema justo después de aplicar una migración).
func Leer(ctx context.Context, q Querier) (esquema.Esquema, error) {
	e := esquema.Esquema{Version: esquema.VersionActual, Motor: "postgresql", Tablas: []esquema.Tabla{},
		Tipos: []esquema.Enum{}, Funciones: []esquema.Funcion{}, Vistas: []esquema.Vista{}, Triggers: []esquema.Trigger{}}
	tablas := map[string]*esquema.Tabla{}
	var orden []string

	// 1) Columnas (en el orden en que están definidas)
	rows, err := q.QueryContext(ctx, `
		SELECT n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull, coalesce(pg_get_expr(d.adbin, d.adrelid), ''), a.attidentity::text
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE c.relkind IN ('r', 'p') AND `+filtroEsquemas+`
		ORDER BY n.nspname, c.relname, a.attnum`)
	if err != nil {
		return e, err
	}
	for rows.Next() {
		var esq, tab string
		var col esquema.Columna
		if err := rows.Scan(&esq, &tab, &col.Nombre, &col.Tipo, &col.Nulo, &col.Defecto, &col.Identidad); err != nil {
			rows.Close()
			return e, err
		}
		nombre := nombreTabla(esq, tab)
		if internas[nombre] {
			continue
		}
		t, ok := tablas[nombre]
		if !ok {
			t = &esquema.Tabla{Nombre: nombre, PK: []string{}, FKs: []esquema.LlaveForanea{}, Indices: []esquema.Indice{}}
			tablas[nombre] = t
			orden = append(orden, nombre)
		}
		t.Columnas = append(t.Columnas, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return e, err
	}

	// 2) Llaves primarias, foráneas y CHECK
	rows, err = q.QueryContext(ctx, `
		SELECT n.nspname, c.relname, con.conname, con.contype, pg_get_constraintdef(con.oid),
		       ARRAY(SELECT a.attname::text FROM unnest(con.conkey) WITH ORDINALITY k(num, ord)
		             JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.num ORDER BY k.ord),
		       coalesce(rn.nspname, ''), coalesce(rc.relname, ''),
		       ARRAY(SELECT a.attname::text FROM unnest(coalesce(con.confkey, '{}'::smallint[])) WITH ORDINALITY k(num, ord)
		             JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.num ORDER BY k.ord),
		       con.confdeltype::text
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_class rc ON rc.oid = con.confrelid
		LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		WHERE con.contype IN ('p', 'f', 'c') AND `+filtroEsquemas+`
		ORDER BY n.nspname, c.relname, con.conname`)
	if err != nil {
		return e, err
	}
	for rows.Next() {
		var esq, tab, nom, tipo, def, refEsq, refTab, borrar string
		var cols, refCols pq.StringArray
		if err := rows.Scan(&esq, &tab, &nom, &tipo, &def, &cols, &refEsq, &refTab, &refCols, &borrar); err != nil {
			rows.Close()
			return e, err
		}
		t := tablas[nombreTabla(esq, tab)]
		if t == nil {
			continue
		}
		if tipo == "p" {
			t.PK, t.PKNombre = []string(cols), nom
			continue
		}
		if tipo == "c" {
			t.Checks = append(t.Checks, esquema.Check{Nombre: nom, Definicion: def})
			continue
		}
		t.FKs = append(t.FKs, esquema.LlaveForanea{
			Nombre: nom, Columnas: []string(cols), Tabla: nombreTabla(refEsq, refTab),
			RefColumnas: []string(refCols), AlBorrar: accionBorrado[borrar],
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return e, err
	}

	// 3) Índices (los de la llave primaria ya se ven en PK)
	rows, err = q.QueryContext(ctx, `
		SELECT n.nspname, t.relname, i.relname, ix.indisunique, pg_get_indexdef(ix.indexrelid),
		       EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = ix.indexrelid AND c.conrelid = ix.indrelid AND c.contype = 'u')
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE NOT ix.indisprimary AND t.relkind IN ('r', 'p') AND `+filtroEsquemas+`
		ORDER BY n.nspname, t.relname, i.relname`)
	if err != nil {
		return e, err
	}
	for rows.Next() {
		var esq, tab string
		var idx esquema.Indice
		if err := rows.Scan(&esq, &tab, &idx.Nombre, &idx.Unico, &idx.Definicion, &idx.Restriccion); err != nil {
			rows.Close()
			return e, err
		}
		t := tablas[nombreTabla(esq, tab)]
		if t == nil {
			continue
		}
		idx.Definicion = limpiarIndice(idx.Definicion)
		t.Indices = append(t.Indices, idx)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return e, err
	}

	sort.Strings(orden)
	for _, n := range orden {
		e.Tablas = append(e.Tablas, *tablas[n])
	}

	// 4) Funciones, vistas, triggers y ENUM (objetos.go)
	if err := leerObjetos(ctx, q, &e); err != nil {
		return e, err
	}
	return e, nil
}

var accionBorrado = map[string]string{"c": "CASCADE", "n": "SET NULL", "r": "RESTRICT", "d": "SET DEFAULT"}

func limpiarIndice(def string) string {
	def = strings.Replace(def, " ON public.", " ON ", 1)
	def = strings.Replace(def, " USING btree (", " (", 1)
	return def
}

// ---------------------------------------------------------------- DDL

const noDeExtension = `NOT EXISTS (SELECT 1 FROM pg_depend x
	WHERE x.classid = '%s'::regclass AND x.objid = %s AND x.deptype = 'e')`

func leerObjetos(ctx context.Context, q Querier, e *esquema.Esquema) error {
	// oid de cada objeto (por catálogo) → su clave; se usa para traducir pg_depend.
	claves := map[string]string{}
	ref := func(catalogo string, oid uint32) string { return fmt.Sprintf("%s:%d", catalogo, oid) }

	// Tablas: su oid y el de su tipo fila (una función puede devolver SETOF tabla).
	rows, err := q.QueryContext(ctx, `
		SELECT n.nspname, c.relname, c.oid, c.reltype
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p') AND `+filtroEsquemas)
	if err != nil {
		return err
	}
	for rows.Next() {
		var esq, tab string
		var oid, tipo uint32
		if err := rows.Scan(&esq, &tab, &oid, &tipo); err != nil {
			rows.Close()
			return err
		}
		k := esquema.ClaveTabla(nombreTabla(esq, tab))
		claves[ref("pg_class", oid)] = k
		claves[ref("pg_type", tipo)] = k
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// ENUM
	rows, err = q.QueryContext(ctx, `
		SELECT t.oid, t.typname,
		       ARRAY(SELECT e.enumlabel::text FROM pg_enum e WHERE e.enumtypid = t.oid ORDER BY e.enumsortorder)
		FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typtype = 'e' AND n.nspname = 'public' AND `+fmt.Sprintf(noDeExtension, "pg_type", "t.oid")+`
		ORDER BY t.typname`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var oid uint32
		var en esquema.Enum
		var vals pq.StringArray
		if err := rows.Scan(&oid, &en.Nombre, &vals); err != nil {
			rows.Close()
			return err
		}
		en.Valores = []string(vals)
		claves[ref("pg_type", oid)] = en.Clave()
		e.Tipos = append(e.Tipos, en)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Funciones y procedimientos (no agregados ni funciones de ventana).
	rows, err = q.QueryContext(ctx, `
		SELECT p.oid, p.proname, oidvectortypes(p.proargtypes), pg_get_function_identity_arguments(p.oid),
		       pg_get_function_arguments(p.oid), coalesce(pg_get_function_result(p.oid), ''),
		       p.prokind = 'p', p.prosecdef,
		       EXISTS (SELECT 1 FROM unnest(coalesce(p.proconfig, '{}')) c WHERE c LIKE 'search_path=%'),
		       pg_get_functiondef(p.oid)
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public' AND p.prokind IN ('f', 'p') AND `+fmt.Sprintf(noDeExtension, "pg_proc", "p.oid")+`
		ORDER BY p.proname, 3`)
	if err != nil {
		return err
	}
	oidFuncion := map[uint32]int{}
	for rows.Next() {
		var oid uint32
		var f esquema.Funcion
		if err := rows.Scan(&oid, &f.Nombre, &f.Tipos, &f.Identidad, &f.Argumentos, &f.Retorno,
			&f.Procedimiento, &f.SecurityDefiner, &f.SearchPath, &f.Definicion); err != nil {
			rows.Close()
			return err
		}
		f.Definicion = strings.TrimSpace(f.Definicion)
		claves[ref("pg_proc", oid)] = f.Clave()
		oidFuncion[oid] = len(e.Funciones)
		e.Funciones = append(e.Funciones, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Vistas y vistas materializadas
	rows, err = q.QueryContext(ctx, `
		SELECT c.oid, c.reltype, c.relname, c.relkind = 'm',
		       coalesce(array_to_string(c.reloptions, ', '), ''), pg_get_viewdef(c.oid, true)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('v', 'm') AND n.nspname = 'public' AND `+fmt.Sprintf(noDeExtension, "pg_class", "c.oid")+`
		ORDER BY c.relname`)
	if err != nil {
		return err
	}
	oidVista := map[uint32]int{}
	for rows.Next() {
		var oid, tipo uint32
		var v esquema.Vista
		if err := rows.Scan(&oid, &tipo, &v.Nombre, &v.Materializada, &v.Opciones, &v.Definicion); err != nil {
			rows.Close()
			return err
		}
		v.Definicion = strings.TrimRight(strings.TrimRight(v.Definicion, " \n"), ";")
		claves[ref("pg_class", oid)] = v.Clave()
		claves[ref("pg_type", tipo)] = v.Clave()
		oidVista[oid] = len(e.Vistas)
		e.Vistas = append(e.Vistas, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Índices de las vistas materializadas
	rows, err = q.QueryContext(ctx, `
		SELECT ix.indrelid, i.relname, ix.indisunique, pg_get_indexdef(ix.indexrelid)
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE t.relkind = 'm' AND n.nspname = 'public'
		ORDER BY i.relname`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var oid uint32
		var idx esquema.Indice
		if err := rows.Scan(&oid, &idx.Nombre, &idx.Unico, &idx.Definicion); err != nil {
			rows.Close()
			return err
		}
		if i, ok := oidVista[oid]; ok {
			idx.Definicion = limpiarIndice(idx.Definicion)
			e.Vistas[i].Indices = append(e.Vistas[i].Indices, idx)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Triggers (los internos, como los de las llaves foráneas, no cuentan)
	rows, err = q.QueryContext(ctx, `
		SELECT t.tgname, c.relname, t.tgfoid, pg_get_triggerdef(t.oid), t.tgenabled = 'D'
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE NOT t.tgisinternal AND n.nspname = 'public'
		ORDER BY c.relname, t.tgname`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tr esquema.Trigger
		var foid uint32
		if err := rows.Scan(&tr.Nombre, &tr.Tabla, &foid, &tr.Definicion, &tr.Desactivado); err != nil {
			rows.Close()
			return err
		}
		tr.Definicion = strings.Replace(tr.Definicion, " ON public.", " ON ", 1)
		tr.Funcion = claves[ref("pg_proc", foid)]
		e.Triggers = append(e.Triggers, tr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Dependencias de funciones y vistas (las de una vista cuelgan de su regla en pg_rewrite).
	// De una tabla se guarda qué columnas usa ("columna:pedidos.total"): así, cambiar
	// una columna que la vista no lee no obliga a recrearla.
	if len(e.Funciones) == 0 && len(e.Vistas) == 0 {
		return nil
	}
	rows, err = q.QueryContext(ctx, `
		SELECT DISTINCT
		       CASE WHEN d.classid = 'pg_rewrite'::regclass THEN 'pg_class' ELSE 'pg_proc' END,
		       CASE WHEN d.classid = 'pg_rewrite'::regclass THEN r.ev_class ELSE d.objid END,
		       d.refclassid::regclass::text, d.refobjid, coalesce(a.attname::text, '')
		FROM pg_depend d
		LEFT JOIN pg_rewrite r ON d.classid = 'pg_rewrite'::regclass AND r.oid = d.objid
		LEFT JOIN pg_attribute a ON d.refclassid = 'pg_class'::regclass AND a.attrelid = d.refobjid
		     AND a.attnum = d.refobjsubid AND d.refobjsubid > 0
		WHERE d.deptype = 'n' AND d.classid IN ('pg_proc'::regclass, 'pg_rewrite'::regclass)
		  AND d.refclassid IN ('pg_class'::regclass, 'pg_proc'::regclass, 'pg_type'::regclass)`)
	if err != nil {
		return err
	}
	deps := map[string]map[string]bool{}
	for rows.Next() {
		var cat, refCat, col string
		var oid, refOid uint32
		if err := rows.Scan(&cat, &oid, &refCat, &refOid, &col); err != nil {
			rows.Close()
			return err
		}
		de, a := claves[ref(cat, oid)], claves[ref(refCat, refOid)]
		if de == "" || a == "" || de == a {
			continue
		}
		if col != "" && strings.HasPrefix(a, "tabla:") {
			a = "columna:" + strings.TrimPrefix(a, "tabla:") + "." + col
		}
		if deps[de] == nil {
			deps[de] = map[string]bool{}
		}
		deps[de][a] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	lista := func(k string) []string {
		var out []string
		for d := range deps[k] {
			out = append(out, d)
		}
		sort.Strings(out)
		return out
	}
	for i := range e.Funciones {
		e.Funciones[i].Depende = lista(e.Funciones[i].Clave())
	}
	for i := range e.Vistas {
		e.Vistas[i].Depende = lista(e.Vistas[i].Clave())
	}
	return nil
}

// ---------------------------------------------------------------- Búsquedas
