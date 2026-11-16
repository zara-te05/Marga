package esquema

import (
	"fmt"
	"sort"
	"strings"
)

// Dialecto convierte objetos del esquema en el DDL de un motor (lo que se
// muestra en `marga esquema`, en la web y en los snapshots).
type Dialecto interface {
	DDLTabla(Tabla) string
	DDLEnum(Enum) string
	DDLFuncion(Funcion) string
	DDLVista(Vista) string
	DDLTrigger(Trigger) string
}

// Cambios es el SQL que lleva de un esquema a otro (y de regreso), con avisos
// para lo que no se puede hacer en automático.
type Cambios struct {
	Up     string
	Down   string
	Avisos []string
}

// MismaVista: misma definición, mismo tipo, mismas opciones e índices.
func MismaVista(a, b Vista) bool {
	if a.Materializada != b.Materializada || a.Opciones != b.Opciones || a.Definicion != b.Definicion || len(a.Indices) != len(b.Indices) {
		return false
	}
	for i := range a.Indices {
		if a.Indices[i].Definicion != b.Indices[i].Definicion {
			return false
		}
	}
	return true
}

// MismoTrigger: misma definición y mismo estado (activo o desactivado).
func MismoTrigger(a, b Trigger) bool {
	return a.Definicion == b.Definicion && a.Desactivado == b.Desactivado
}

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

func (t Trigger) Clave() string { return "trigger:" + t.Etiqueta() }

func (e Enum) Clave() string { return "tipo:" + e.Nombre }

// ClaveTabla es la clave de una tabla entre todos los objetos del esquema.
func ClaveTabla(nombre string) string { return "tabla:" + nombre }

// ---------------------------------------------------------------- Lectura

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
func (e Esquema) Objetos(d Dialecto) []Objeto {
	var out []Objeto
	for _, t := range e.Tablas {
		out = append(out, Objeto{"tabla", t.Nombre, d.DDLTabla(t)})
	}
	for _, t := range e.Tipos {
		out = append(out, Objeto{"tipo", t.Nombre, d.DDLEnum(t)})
	}
	for _, f := range e.Funciones {
		out = append(out, Objeto{f.TipoObjeto(), f.Firma(), d.DDLFuncion(f)})
	}
	for _, v := range e.Vistas {
		out = append(out, Objeto{v.TipoObjeto(), v.Nombre, d.DDLVista(v)})
	}
	for _, t := range e.Triggers {
		out = append(out, Objeto{"trigger", t.Etiqueta(), d.DDLTrigger(t)})
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
		case !MismaVista(*va, vd):
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
		case !MismoTrigger(*ta, td):
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
