// Package esquema describe el esquema de una base de datos sin atarse a un motor:
// tablas, columnas, llaves, índices, CHECK, tipos ENUM, funciones, vistas y
// triggers; y cómo se comparan dos esquemas (drift). Leerlo de una base real y
// convertirlo en SQL es trabajo de cada motor (internal/datos/motor).
package esquema

import (
	"fmt"
	"sort"
	"strings"
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
	Checks   []Check        `json:"checks,omitempty"`
}

// Check es una restricción CHECK de una tabla.
type Check struct {
	Nombre     string `json:"nombre"`
	Definicion string `json:"definicion"` // "CHECK ((total >= (0)::numeric))", como lo da pg_get_constraintdef
}

// VersionActual es la versión del formato JSON del esquema.
// 1 (o 0, sin el campo): solo tablas. 2: también CHECK, funciones, vistas, triggers y ENUM.
const VersionActual = 2

type Esquema struct {
	Version   int       `json:"version"`
	Motor     string    `json:"motor"`
	Tablas    []Tabla   `json:"tablas"`
	Tipos     []Enum    `json:"tipos"`
	Funciones []Funcion `json:"funciones"`
	Vistas    []Vista   `json:"vistas"`
	Triggers  []Trigger `json:"triggers"`
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
	out = append(out, compararObjetos(antes, despues)...)
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

	chkA := map[string]string{}
	for _, c := range a.Checks {
		chkA[c.Nombre] = c.Definicion
	}
	chkD := map[string]bool{}
	for _, c := range d.Checks {
		chkD[c.Nombre] = true
		def, ok := chkA[c.Nombre]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("+ check %s en %s: %s", c.Nombre, t, c.Definicion))
		case def != c.Definicion:
			out = append(out, fmt.Sprintf("~ check %s en %s: %s → %s", c.Nombre, t, def, c.Definicion))
		}
	}
	for _, c := range a.Checks {
		if !chkD[c.Nombre] {
			out = append(out, fmt.Sprintf("- check %s en %s", c.Nombre, t))
		}
	}
	return out
}
