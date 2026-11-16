// Package generador escribe el SQL de una migración comparando dos esquemas:
// el que dejó Marga (línea base) y el que tiene ahora la base de desarrollo.
//
// Cubre tablas, columnas (tipo, NULL, DEFAULT), llave primaria, llaves foráneas,
// índices y CHECK; y también funciones y procedimientos, vistas (normales y
// materializadas), triggers y tipos ENUM, en el orden que piden sus dependencias
// (ver objetos.go). No detecta renombres (los ve como borrar + crear), y de un
// ENUM solo agrega valores: quitarlos o renombrarlos se hace a mano.
package generador

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zara-te05/Marga/internal/datos/introspeccion"
)

type Resultado struct {
	Up, Down string
	Avisos   []string // cosas que el usuario debe revisar antes de confiar en el SQL
}

// Generar produce el UP (antes → despues) y el DOWN (despues → antes).
func Generar(antes, despues introspeccion.Esquema) Resultado {
	up, avisos := sentencias(antes, despues)
	down, _ := sentencias(despues, antes)
	return Resultado{Up: unir(up), Down: unir(down), Avisos: avisos}
}

func unir(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.Join(s, "\n") + "\n"
}

var reNextval = regexp.MustCompile(`^nextval\('(?:public\.)?"?([A-Za-z0-9_]+)"?'::regclass\)$`)

// defColumna escribe la definición de una columna. Las columnas SERIAL se escriben
// como serial (su secuencia no existe en una base nueva, así que nextval(...) fallaría).
func defColumna(tabla string, c introspeccion.Columna) string {
	if m := reNextval.FindStringSubmatch(c.Defecto); m != nil && m[1] == tabla+"_"+c.Nombre+"_seq" {
		switch c.Tipo {
		case "integer":
			return c.Nombre + " serial"
		case "bigint":
			return c.Nombre + " bigserial"
		case "smallint":
			return c.Nombre + " smallserial"
		}
	}
	return c.Nombre + " " + c.Tipo + introspeccion.Modificadores(c)
}

func crearTabla(t introspeccion.Tabla) string {
	partes := make([]string, 0, len(t.Columnas)+1)
	for _, c := range t.Columnas {
		partes = append(partes, "    "+defColumna(t.Nombre, c))
	}
	if len(t.PK) > 0 {
		partes = append(partes, "    PRIMARY KEY ("+strings.Join(t.PK, ", ")+")")
	}
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);", t.Nombre, strings.Join(partes, ",\n"))
}

func agregarFK(tabla string, f introspeccion.LlaveForanea) string {
	s := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		tabla, f.Nombre, strings.Join(f.Columnas, ", "), f.Tabla, strings.Join(f.RefColumnas, ", "))
	if f.AlBorrar != "" {
		s += " ON DELETE " + f.AlBorrar
	}
	return s + ";"
}

func claveFK(f introspeccion.LlaveForanea) string {
	return f.Nombre + "|" + strings.Join(f.Columnas, ",") + "|" + f.Tabla + "|" + strings.Join(f.RefColumnas, ",") + "|" + f.AlBorrar
}

func nombresOrdenados(e introspeccion.Esquema) []string {
	out := make([]string, 0, len(e.Tablas))
	for _, t := range e.Tablas {
		out = append(out, t.Nombre)
	}
	sort.Strings(out)
	return out
}

// sentencias genera el SQL para pasar de `a` a `b`, en un orden que no rompe dependencias:
// quitar FKs e índices → borrar tablas → crear tablas → columnas → PK → índices → FKs.
// Un índice que respalda una restricción UNIQUE se borra quitando la restricción,
// y se recrea como índice + restricción para que el esquema quede idéntico.
func quitarIndice(tabla string, i introspeccion.Indice) string {
	if i.Restriccion {
		return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s;", tabla, i.Nombre)
	}
	return fmt.Sprintf("DROP INDEX IF EXISTS %s;", i.Nombre)
}

func ponerIndice(tabla string, i introspeccion.Indice) string {
	if i.Restriccion {
		return fmt.Sprintf("%s;\nALTER TABLE %s ADD CONSTRAINT %s UNIQUE USING INDEX %s;", i.Definicion, tabla, i.Nombre, i.Nombre)
	}
	return i.Definicion + ";"
}

func sentencias(a, b introspeccion.Esquema) ([]string, []string) {
	var (
		quitarFK, quitarIdx, borrar, crear, columnas, pks, ponerIdx, ponerFK []string
		avisos                                                               []string
	)
	var borradas []string

	for _, n := range nombresOrdenados(a) {
		ta := a.Buscar(n)
		tb := b.Buscar(n)
		if tb == nil {
			borradas = append(borradas, n)
			avisos = append(avisos, fmt.Sprintf("Se borra la tabla %s y todos sus datos. Si la renombraste, usa ALTER TABLE … RENAME TO.", n))
			continue
		}
		// FKs que desaparecen o cambian
		fkB := map[string]bool{}
		for _, f := range tb.FKs {
			fkB[claveFK(f)] = true
		}
		for _, f := range ta.FKs {
			if !fkB[claveFK(f)] {
				quitarFK = append(quitarFK, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", n, f.Nombre))
			}
		}
		// Índices que desaparecen o cambian
		idxB := map[string]string{}
		for _, i := range tb.Indices {
			idxB[i.Nombre] = i.Definicion
		}
		for _, i := range ta.Indices {
			if def, ok := idxB[i.Nombre]; !ok || def != i.Definicion {
				quitarIdx = append(quitarIdx, quitarIndice(n, i))
			}
		}
	}
	// FKs de otras tablas que apuntan a tablas que se borran: también hay que quitarlas.
	if len(borradas) > 0 {
		borrar = append(borrar, "DROP TABLE "+strings.Join(borradas, ", ")+";")
	}

	for _, n := range nombresOrdenados(b) {
		tb := b.Buscar(n)
		ta := a.Buscar(n)
		if ta == nil {
			crear = append(crear, crearTabla(*tb))
			for _, i := range tb.Indices {
				ponerIdx = append(ponerIdx, ponerIndice(n, i))
			}
			for _, f := range tb.FKs {
				ponerFK = append(ponerFK, agregarFK(n, f))
			}
			continue
		}
		cols, avs := cambiosColumnas(*ta, *tb)
		columnas = append(columnas, cols...)
		avisos = append(avisos, avs...)

		if strings.Join(ta.PK, ",") != strings.Join(tb.PK, ",") {
			if len(ta.PK) > 0 && ta.PKNombre != "" {
				pks = append(pks, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", n, ta.PKNombre))
			}
			if len(tb.PK) > 0 {
				pks = append(pks, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s);", n, strings.Join(tb.PK, ", ")))
			}
		}
		idxA := map[string]string{}
		for _, i := range ta.Indices {
			idxA[i.Nombre] = i.Definicion
		}
		for _, i := range tb.Indices {
			if def, ok := idxA[i.Nombre]; !ok || def != i.Definicion {
				ponerIdx = append(ponerIdx, ponerIndice(n, i))
			}
		}
		fkA := map[string]bool{}
		for _, f := range ta.FKs {
			fkA[claveFK(f)] = true
		}
		for _, f := range tb.FKs {
			if !fkA[claveFK(f)] {
				ponerFK = append(ponerFK, agregarFK(n, f))
			}
		}
	}

	p := planificar(a, b)
	avisos = append(avisos, p.avisos...)
	var inicio []string
	if p.creaFunciones {
		// Como hace pg_dump: el cuerpo de una función puede usar tablas o vistas
		// que se crean más abajo en este mismo archivo.
		inicio = []string{"SET LOCAL check_function_bodies = off;"}
	}

	var todo []string
	for _, grupo := range [][]string{
		inicio,
		p.quitarTriggers, p.quitarVistas, p.quitarChecks, p.quitarFunciones, // 1, 2: quitar lo que depende
		quitarFK, quitarIdx, borrar, // 3: cambiar tipos, funciones y tablas
		p.crearTipos, p.funcionesAntes,
		crear, columnas, pks, ponerIdx, ponerFK,
		p.funcionesTablas, p.ponerChecks,
		p.crearVistas, p.funcionesVistas, // 4: recrear vistas (las internas primero)
		p.crearTriggers, // 5: recrear triggers
		p.borrarTipos,
	} {
		todo = append(todo, grupo...)
	}
	return todo, avisos
}

func cambiosColumnas(ta, tb introspeccion.Tabla) ([]string, []string) {
	var out, avisos []string
	n := ta.Nombre
	colA := map[string]introspeccion.Columna{}
	for _, c := range ta.Columnas {
		colA[c.Nombre] = c
	}
	enB := map[string]bool{}
	for _, c := range tb.Columnas {
		enB[c.Nombre] = true
		ca, existia := colA[c.Nombre]
		if !existia {
			out = append(out, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", n, defColumna(n, c)))
			if !c.Nulo && c.Defecto == "" && c.Identidad == "" {
				avisos = append(avisos, fmt.Sprintf("%s.%s es NOT NULL sin DEFAULT: fallará si la tabla ya tiene filas en otros entornos.", n, c.Nombre))
			}
			continue
		}
		if ca.Tipo != c.Tipo {
			out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s;", n, c.Nombre, c.Tipo, c.Nombre, c.Tipo))
			avisos = append(avisos, fmt.Sprintf("Cambio de tipo en %s.%s (%s → %s): revisa que los datos existentes se puedan convertir.", n, c.Nombre, ca.Tipo, c.Tipo))
		}
		if ca.Identidad != c.Identidad {
			avisos = append(avisos, fmt.Sprintf("La identidad de %s.%s cambió: Marga no la genera, escríbela a mano.", n, c.Nombre))
		}
		if ca.Nulo != c.Nulo && c.Identidad == "" {
			if c.Nulo {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", n, c.Nombre))
			} else {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", n, c.Nombre))
			}
		}
		if ca.Defecto != c.Defecto {
			if c.Defecto == "" {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", n, c.Nombre))
			} else {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", n, c.Nombre, c.Defecto))
			}
		}
	}
	for _, c := range ta.Columnas {
		if !enB[c.Nombre] {
			out = append(out, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", n, c.Nombre))
			avisos = append(avisos, fmt.Sprintf("Se borra la columna %s.%s y sus datos. Si la renombraste, usa RENAME COLUMN.", n, c.Nombre))
		}
	}
	return out, avisos
}
