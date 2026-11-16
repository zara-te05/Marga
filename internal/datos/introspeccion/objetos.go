package introspeccion

// Objetos que no son tablas: funciones y procedimientos, vistas (normales y
// materializadas), triggers y tipos ENUM. Solo se leen del esquema public y se
// excluye lo que pertenece a una extensión (pgcrypto, postgis…).
//
// Cada objeto guarda su definición canónica (la que da PostgreSQL con
// pg_get_functiondef, pg_get_viewdef, pg_get_triggerdef) y la lista de objetos
// de los que depende (pg_depend), que el generador usa para ordenar el SQL.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// Enum es un tipo ENUM.
type Enum struct {
	Nombre  string   `json:"nombre"`
	Valores []string `json:"valores"`
}

// Funcion es una función o un procedimiento. Se identifica por su firma:
// calcular_total(integer, date) y calcular_total(integer) son dos funciones distintas.
type Funcion struct {
	Nombre string `json:"nombre"`
	// Tipos: los tipos de los argumentos de entrada ("integer, date"). Nombre + Tipos = firma.
	Tipos string `json:"tipos"`
	// Identidad: los argumentos como los pide DROP FUNCTION ("p_id integer, p_fecha date").
	Identidad string `json:"identidad"`
	// Argumentos: la lista completa, con nombres y DEFAULT. Si cambia, no basta CREATE OR REPLACE.
	Argumentos string `json:"argumentos"`
	Retorno    string `json:"retorno,omitempty"`
	// Procedimiento: true para CREATE PROCEDURE.
	Procedimiento   bool     `json:"procedimiento,omitempty"`
	SecurityDefiner bool     `json:"security_definer,omitempty"`
	SearchPath      bool     `json:"search_path,omitempty"` // tiene SET search_path
	Definicion      string   `json:"definicion"`
	Depende         []string `json:"depende,omitempty"`
}

// Firma identifica la función: nombre(tipos).
func (f Funcion) Firma() string { return f.Nombre + "(" + f.Tipos + ")" }

// Clave identifica la función entre todos los objetos del esquema.
func (f Funcion) Clave() string { return f.TipoObjeto() + ":" + f.Firma() }

// TipoObjeto: "funcion" o "procedimiento".
func (f Funcion) TipoObjeto() string {
	if f.Procedimiento {
		return "procedimiento"
	}
	return "funcion"
}

func (f Funcion) DDL() string { return strings.TrimRight(f.Definicion, "\n; ") + ";\n" }

// Vista es una vista normal o materializada.
type Vista struct {
	Nombre        string   `json:"nombre"`
	Materializada bool     `json:"materializada,omitempty"`
	Opciones      string   `json:"opciones,omitempty"` // reloptions: "security_barrier=true,check_option=local"
	Definicion    string   `json:"definicion"`         // el SELECT, sin ";" final
	Indices       []Indice `json:"indices,omitempty"`  // solo vistas materializadas
	// Depende: claves de lo que usa (tabla:x, columna:x.c, vista:x, funcion:f(int), tipo:x).
	Depende []string `json:"depende,omitempty"`
}

func (v Vista) TipoObjeto() string {
	if v.Materializada {
		return "vista_materializada"
	}
	return "vista"
}

// Clave: las vistas y las tablas comparten espacio de nombres en PostgreSQL.
func (v Vista) Clave() string { return "vista:" + v.Nombre }

// Crear devuelve el CREATE de la vista (y sus índices si es materializada).
func (v Vista) DDL() string {
	tipo := "VIEW"
	if v.Materializada {
		tipo = "MATERIALIZED VIEW"
	}
	opc := ""
	if v.Opciones != "" {
		opc = " WITH (" + v.Opciones + ")"
	}
	s := fmt.Sprintf("CREATE %s %s%s AS\n%s;\n", tipo, v.Nombre, opc, v.Definicion)
	for _, i := range v.Indices {
		s += i.Definicion + ";\n"
	}
	return s
}

// Trigger de una tabla (o vista, con INSTEAD OF).
type Trigger struct {
	Nombre     string `json:"nombre"`
	Tabla      string `json:"tabla"`
	Funcion    string `json:"funcion,omitempty"` // clave de la función que ejecuta
	Definicion string `json:"definicion"`        // pg_get_triggerdef
	// Desactivado: ALTER TABLE … DISABLE TRIGGER.
	Desactivado bool `json:"desactivado,omitempty"`
}

// Etiqueta: "nombre ON tabla" (dos tablas pueden tener triggers con el mismo nombre).
func (t Trigger) Etiqueta() string { return t.Nombre + " ON " + t.Tabla }
func (t Trigger) Clave() string    { return "trigger:" + t.Etiqueta() }
func (t Trigger) DDL() string {
	s := t.Definicion + ";\n"
	if t.Desactivado {
		s += fmt.Sprintf("ALTER TABLE %s DISABLE TRIGGER %s;\n", t.Tabla, t.Nombre)
	}
	return s
}

func (e Enum) Clave() string { return "tipo:" + e.Nombre }
func (e Enum) DDL() string {
	vals := make([]string, len(e.Valores))
	for i, v := range e.Valores {
		vals[i] = pq.QuoteLiteral(v)
	}
	return fmt.Sprintf("CREATE TYPE %s AS ENUM (%s);\n", e.Nombre, strings.Join(vals, ", "))
}

// ClaveTabla es la clave de una tabla entre todos los objetos del esquema.
func ClaveTabla(nombre string) string { return "tabla:" + nombre }

// ---------------------------------------------------------------- Lectura

const noDeExtension = `NOT EXISTS (SELECT 1 FROM pg_depend x
	WHERE x.classid = '%s'::regclass AND x.objid = %s AND x.deptype = 'e')`

func leerObjetos(ctx context.Context, q Querier, e *Esquema) error {
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
		k := ClaveTabla(nombreTabla(esq, tab))
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
		var en Enum
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
		var f Funcion
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
		var v Vista
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
		var idx Indice
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
		var tr Trigger
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

func (e Esquema) BuscarFuncion(clave string) *Funcion {
	for i := range e.Funciones {
		if e.Funciones[i].Clave() == clave {
			return &e.Funciones[i]
		}
	}
	return nil
}

func (e Esquema) BuscarVista(nombre string) *Vista {
	for i := range e.Vistas {
		if e.Vistas[i].Nombre == nombre {
			return &e.Vistas[i]
		}
	}
	return nil
}

func (e Esquema) BuscarTrigger(etiqueta string) *Trigger {
	for i := range e.Triggers {
		if e.Triggers[i].Etiqueta() == etiqueta {
			return &e.Triggers[i]
		}
	}
	return nil
}

func (e Esquema) BuscarTipo(nombre string) *Enum {
	for i := range e.Tipos {
		if e.Tipos[i].Nombre == nombre {
			return &e.Tipos[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------- Lista de objetos

// Objeto es cualquier cosa del esquema con su DDL: lo que la CLI envía al servidor
// como snapshot y lo que muestran Resumen y Esquema en la web.
type Objeto struct {
	Tipo   string // tabla, funcion, procedimiento, vista, vista_materializada, trigger, tipo
	Nombre string // tabla, firma de la función ("f(integer)") o "trigger ON tabla"
	DDL    string
}

// Objetos lista todo el esquema: tablas, tipos, funciones, vistas y triggers.
// Los nombres no se repiten entre tipos (las firmas llevan paréntesis y los triggers " ON ").
func (e Esquema) Objetos() []Objeto {
	var out []Objeto
	for _, t := range e.Tablas {
		out = append(out, Objeto{"tabla", t.Nombre, t.DDL()})
	}
	for _, t := range e.Tipos {
		out = append(out, Objeto{"tipo", t.Nombre, t.DDL()})
	}
	for _, f := range e.Funciones {
		out = append(out, Objeto{f.TipoObjeto(), f.Firma(), f.DDL()})
	}
	for _, v := range e.Vistas {
		out = append(out, Objeto{v.TipoObjeto(), v.Nombre, v.DDL()})
	}
	for _, t := range e.Triggers {
		out = append(out, Objeto{"trigger", t.Etiqueta(), t.DDL()})
	}
	return out
}

// ---------------------------------------------------------------- Compatibilidad

// Completar pone al día un esquema guardado con un formato anterior (version < 2,
// sin funciones, vistas, triggers, ENUM ni CHECK) tomando esos objetos de `actual`.
// Así, la primera vez que Marga ve la base con esta versión, los objetos que ya
// existían entran a la línea base sin reportarse como drift.
// Devuelve el esquema completado y los objetos que se incorporaron, para mostrarlos
// ("función f(integer)", "check c en t"…; vacío si ya era v2).
func Completar(viejo, actual Esquema) (Esquema, []string) {
	if viejo.Version >= VersionActual {
		return viejo, nil
	}
	var nuevos []string
	e := viejo
	e.Version = VersionActual
	e.Tablas = make([]Tabla, len(viejo.Tablas))
	for i, t := range viejo.Tablas {
		if a := actual.Buscar(t.Nombre); a != nil && len(t.Checks) == 0 {
			t.Checks = a.Checks
			for _, c := range a.Checks {
				nuevos = append(nuevos, "check "+c.Nombre+" en "+t.Nombre)
			}
		}
		e.Tablas[i] = t
	}
	e.Tipos = append([]Enum{}, actual.Tipos...)
	e.Funciones = append([]Funcion{}, actual.Funciones...)
	e.Vistas = append([]Vista{}, actual.Vistas...)
	e.Triggers = append([]Trigger{}, actual.Triggers...)
	for _, t := range e.Tipos {
		nuevos = append(nuevos, "tipo "+t.Nombre)
	}
	for _, f := range e.Funciones {
		nuevos = append(nuevos, map[bool]string{false: "función ", true: "procedimiento "}[f.Procedimiento]+f.Firma())
	}
	for _, v := range e.Vistas {
		nuevos = append(nuevos, map[bool]string{false: "vista ", true: "vista materializada "}[v.Materializada]+v.Nombre)
	}
	for _, t := range e.Triggers {
		nuevos = append(nuevos, "trigger "+t.Nombre+" en "+t.Tabla)
	}
	return e, nuevos
}

// ---------------------------------------------------------------- Comparación

func compararObjetos(a, d Esquema) []string {
	var out []string

	// ENUM
	for _, td := range d.Tipos {
		ta := a.BuscarTipo(td.Nombre)
		if ta == nil {
			out = append(out, fmt.Sprintf("+ tipo %s (ENUM: %s)", td.Nombre, strings.Join(td.Valores, ", ")))
			continue
		}
		enA := map[string]bool{}
		for _, v := range ta.Valores {
			enA[v] = true
		}
		enD := map[string]bool{}
		for _, v := range td.Valores {
			enD[v] = true
			if !enA[v] {
				out = append(out, fmt.Sprintf("~ tipo %s: + valor '%s'", td.Nombre, v))
			}
		}
		for _, v := range ta.Valores {
			if !enD[v] {
				out = append(out, fmt.Sprintf("~ tipo %s: - valor '%s'", td.Nombre, v))
			}
		}
		if len(enA) == len(enD) && strings.Join(ta.Valores, "\x00") != strings.Join(td.Valores, "\x00") && mismosValores(ta.Valores, td.Valores) {
			out = append(out, fmt.Sprintf("~ tipo %s: cambió el orden de los valores", td.Nombre))
		}
	}
	for _, ta := range a.Tipos {
		if d.BuscarTipo(ta.Nombre) == nil {
			out = append(out, "- tipo "+ta.Nombre)
		}
	}

	// Funciones y procedimientos
	for _, fd := range d.Funciones {
		fa := a.BuscarFuncion(fd.Clave())
		nombre := map[bool]string{false: "función", true: "procedimiento"}[fd.Procedimiento]
		switch {
		case fa == nil:
			out = append(out, fmt.Sprintf("+ %s %s", nombre, fd.Firma()))
		case fa.Definicion != fd.Definicion:
			out = append(out, fmt.Sprintf("~ %s %s: cambió su definición", nombre, fd.Firma()))
		}
	}
	for _, fa := range a.Funciones {
		if d.BuscarFuncion(fa.Clave()) == nil {
			nombre := map[bool]string{false: "función", true: "procedimiento"}[fa.Procedimiento]
			out = append(out, fmt.Sprintf("- %s %s", nombre, fa.Firma()))
		}
	}

	// Vistas
	for _, vd := range d.Vistas {
		va := a.BuscarVista(vd.Nombre)
		nombre := map[bool]string{false: "vista", true: "vista materializada"}[vd.Materializada]
		switch {
		case va == nil:
			out = append(out, fmt.Sprintf("+ %s %s", nombre, vd.Nombre))
		case va.DDL() != vd.DDL():
			out = append(out, fmt.Sprintf("~ %s %s: cambió su definición", nombre, vd.Nombre))
		}
	}
	for _, va := range a.Vistas {
		if d.BuscarVista(va.Nombre) == nil {
			nombre := map[bool]string{false: "vista", true: "vista materializada"}[va.Materializada]
			out = append(out, fmt.Sprintf("- %s %s", nombre, va.Nombre))
		}
	}

	// Triggers
	for _, td := range d.Triggers {
		ta := a.BuscarTrigger(td.Etiqueta())
		switch {
		case ta == nil:
			out = append(out, fmt.Sprintf("+ trigger %s en %s", td.Nombre, td.Tabla))
		case ta.DDL() != td.DDL():
			out = append(out, fmt.Sprintf("~ trigger %s en %s: cambió su definición", td.Nombre, td.Tabla))
		}
	}
	for _, ta := range a.Triggers {
		if d.BuscarTrigger(ta.Etiqueta()) == nil {
			out = append(out, fmt.Sprintf("- trigger %s en %s", ta.Nombre, ta.Tabla))
		}
	}
	return out
}

func mismosValores(a, b []string) bool {
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}
