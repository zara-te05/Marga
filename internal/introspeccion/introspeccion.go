// Package introspeccion lee el esquema real de una base PostgreSQL desde sus
// catálogos (tablas, columnas, llaves e índices), lo convierte a DDL legible
// y compara dos esquemas para detectar cambios (drift).
package introspeccion

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
)

type Columna struct {
	Nombre  string `json:"nombre"`
	Tipo    string `json:"tipo"`
	Nulo    bool   `json:"nulo"`
	Defecto string `json:"defecto,omitempty"`
	// Identidad: "a" = GENERATED ALWAYS AS IDENTITY, "d" = BY DEFAULT, "" = no es identidad.
	Identidad string `json:"identidad,omitempty"`
}

type Indice struct {
	Nombre     string `json:"nombre"`
	Unico      bool   `json:"unico"`
	Definicion string `json:"definicion"`
	// Restriccion: el índice pertenece a una restricción UNIQUE (no se borra con DROP INDEX).
	Restriccion bool `json:"restriccion,omitempty"`
}

type LlaveForanea struct {
	Nombre      string   `json:"nombre"`
	Columnas    []string `json:"columnas"`
	Tabla       string   `json:"tabla"`
	RefColumnas []string `json:"ref_columnas"`
	AlBorrar    string   `json:"al_borrar,omitempty"`
}

type Tabla struct {
	Nombre   string         `json:"nombre"`
	Columnas []Columna      `json:"columnas"`
	PK       []string       `json:"pk"`
	PKNombre string         `json:"pk_nombre,omitempty"`
	FKs      []LlaveForanea `json:"fks"`
	Indices  []Indice       `json:"indices"`
}

type Esquema struct {
	Motor  string  `json:"motor"`
	Tablas []Tabla `json:"tablas"`
}

// Tablas internas de Marga que no forman parte del esquema del usuario.
var internas = map[string]bool{"marga_history": true, "marga_esquema": true}

type Querier interface {
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
func Leer(ctx context.Context, q Querier) (Esquema, error) {
	e := Esquema{Motor: "postgresql", Tablas: []Tabla{}}
	tablas := map[string]*Tabla{}
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
		var col Columna
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
			t = &Tabla{Nombre: nombre, PK: []string{}, FKs: []LlaveForanea{}, Indices: []Indice{}}
			tablas[nombre] = t
			orden = append(orden, nombre)
		}
		t.Columnas = append(t.Columnas, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return e, err
	}

	// 2) Llaves primarias y foráneas
	rows, err = q.QueryContext(ctx, `
		SELECT n.nspname, c.relname, con.conname, con.contype,
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
		WHERE con.contype IN ('p', 'f') AND `+filtroEsquemas+`
		ORDER BY n.nspname, c.relname, con.conname`)
	if err != nil {
		return e, err
	}
	for rows.Next() {
		var esq, tab, nom, tipo, refEsq, refTab, borrar string
		var cols, refCols pq.StringArray
		if err := rows.Scan(&esq, &tab, &nom, &tipo, &cols, &refEsq, &refTab, &refCols, &borrar); err != nil {
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
		t.FKs = append(t.FKs, LlaveForanea{
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
		var idx Indice
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
	return e, nil
}

var accionBorrado = map[string]string{"c": "CASCADE", "n": "SET NULL", "r": "RESTRICT", "d": "SET DEFAULT"}

func limpiarIndice(def string) string {
	def = strings.Replace(def, " ON public.", " ON ", 1)
	def = strings.Replace(def, " USING btree (", " (", 1)
	return def
}

// ---------------------------------------------------------------- DDL

// DDL reconstruye la definición de la tabla de forma legible.
func (t Tabla) DDL() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", t.Nombre)
	var partes []string
	for _, c := range t.Columnas {
		partes = append(partes, "    "+c.Nombre+" "+c.Tipo+Modificadores(c))
	}
	if len(t.PK) > 0 {
		partes = append(partes, "    PRIMARY KEY ("+strings.Join(t.PK, ", ")+")")
	}
	for _, f := range t.FKs {
		p := fmt.Sprintf("    FOREIGN KEY (%s) REFERENCES %s (%s)", strings.Join(f.Columnas, ", "), f.Tabla, strings.Join(f.RefColumnas, ", "))
		if f.AlBorrar != "" {
			p += " ON DELETE " + f.AlBorrar
		}
		partes = append(partes, p)
	}
	b.WriteString(strings.Join(partes, ",\n"))
	b.WriteString("\n);")
	for _, i := range t.Indices {
		b.WriteString("\n" + i.Definicion + ";")
	}
	return b.String() + "\n"
}

// Modificadores devuelve " NOT NULL DEFAULT …" / " GENERATED … AS IDENTITY" de una columna.
func Modificadores(c Columna) string {
	s := ""
	switch c.Identidad {
	case "a":
		s += " GENERATED ALWAYS AS IDENTITY"
	case "d":
		s += " GENERATED BY DEFAULT AS IDENTITY"
	}
	if !c.Nulo && c.Identidad == "" {
		s += " NOT NULL"
	}
	if c.Defecto != "" {
		s += " DEFAULT " + c.Defecto
	}
	return s
}

func (e Esquema) Buscar(nombre string) *Tabla {
	for i := range e.Tablas {
		if e.Tablas[i].Nombre == nombre {
			return &e.Tablas[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------- Comparación

// Comparar describe en español qué cambió de `antes` a `despues`.
func Comparar(antes, despues Esquema) []string {
	var out []string
	nombres := map[string]bool{}
	for _, t := range antes.Tablas {
		nombres[t.Nombre] = true
	}
	for _, t := range despues.Tablas {
		nombres[t.Nombre] = true
	}
	orden := make([]string, 0, len(nombres))
	for n := range nombres {
		orden = append(orden, n)
	}
	sort.Strings(orden)

	for _, n := range orden {
		a, d := antes.Buscar(n), despues.Buscar(n)
		switch {
		case a == nil:
			out = append(out, fmt.Sprintf("+ tabla %s (%d columnas)", n, len(d.Columnas)))
			continue
		case d == nil:
			out = append(out, "- tabla "+n)
			continue
		}
		out = append(out, compararTabla(*a, *d)...)
	}
	return out
}

func compararTabla(a, d Tabla) []string {
	var out []string
	t := a.Nombre
	colA := map[string]Columna{}
	for _, c := range a.Columnas {
		colA[c.Nombre] = c
	}
	colD := map[string]bool{}
	for _, c := range d.Columnas {
		colD[c.Nombre] = true
		ca, ok := colA[c.Nombre]
		if !ok {
			out = append(out, fmt.Sprintf("+ columna %s.%s %s", t, c.Nombre, c.Tipo))
			continue
		}
		if ca.Tipo != c.Tipo {
			out = append(out, fmt.Sprintf("~ columna %s.%s: tipo %s → %s", t, c.Nombre, ca.Tipo, c.Tipo))
		}
		if ca.Nulo != c.Nulo {
			if c.Nulo {
				out = append(out, fmt.Sprintf("~ columna %s.%s: ahora acepta NULL", t, c.Nombre))
			} else {
				out = append(out, fmt.Sprintf("~ columna %s.%s: ahora es NOT NULL", t, c.Nombre))
			}
		}
		if ca.Identidad != c.Identidad {
			out = append(out, fmt.Sprintf("~ columna %s.%s: identidad cambió", t, c.Nombre))
		}
		if ca.Defecto != c.Defecto {
			out = append(out, fmt.Sprintf("~ columna %s.%s: default %q → %q", t, c.Nombre, ca.Defecto, c.Defecto))
		}
	}
	for _, c := range a.Columnas {
		if !colD[c.Nombre] {
			out = append(out, fmt.Sprintf("- columna %s.%s", t, c.Nombre))
		}
	}
	if strings.Join(a.PK, ",") != strings.Join(d.PK, ",") {
		out = append(out, fmt.Sprintf("~ llave primaria de %s: (%s) → (%s)", t, strings.Join(a.PK, ", "), strings.Join(d.PK, ", ")))
	}

	fkClave := func(f LlaveForanea) string {
		return strings.Join(f.Columnas, ",") + "→" + f.Tabla + "(" + strings.Join(f.RefColumnas, ",") + ") " + f.AlBorrar
	}
	fkA, fkD := map[string]LlaveForanea{}, map[string]LlaveForanea{}
	for _, f := range a.FKs {
		fkA[fkClave(f)] = f
	}
	for _, f := range d.FKs {
		fkD[fkClave(f)] = f
	}
	var fks []string
	for k, f := range fkD {
		if _, ok := fkA[k]; !ok {
			fks = append(fks, fmt.Sprintf("+ llave foránea %s(%s) → %s", t, strings.Join(f.Columnas, ", "), f.Tabla))
		}
	}
	for k, f := range fkA {
		if _, ok := fkD[k]; !ok {
			fks = append(fks, fmt.Sprintf("- llave foránea %s(%s) → %s", t, strings.Join(f.Columnas, ", "), f.Tabla))
		}
	}
	sort.Strings(fks) // los mapas no tienen orden: así la salida es estable
	out = append(out, fks...)

	idxA := map[string]string{}
	for _, i := range a.Indices {
		idxA[i.Nombre] = i.Definicion
	}
	idxD := map[string]bool{}
	for _, i := range d.Indices {
		idxD[i.Nombre] = true
		def, ok := idxA[i.Nombre]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("+ índice %s en %s", i.Nombre, t))
		case def != i.Definicion:
			out = append(out, fmt.Sprintf("~ índice %s en %s cambió", i.Nombre, t))
		}
	}
	for _, i := range a.Indices {
		if !idxD[i.Nombre] {
			out = append(out, fmt.Sprintf("- índice %s en %s", i.Nombre, t))
		}
	}
	return out
}
