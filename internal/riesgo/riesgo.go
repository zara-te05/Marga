// Package riesgo revisa el SQL de una migración y avisa de lo que puede
// perder datos, bloquear tablas o fallar en producción.
//
// Lo usan la CLI (marga revisar, marga up) y el servidor (el riesgo de cada commit).
package riesgo

import (
	"regexp"
	"sort"
	"strings"
)

type Hallazgo struct {
	Nivel      string `json:"nivel"` // "alto", "medio" o "bajo"
	Regla      string `json:"regla"`
	Mensaje    string `json:"mensaje"`
	Sugerencia string `json:"sugerencia,omitempty"`
	Linea      int    `json:"linea"`
}

var pesos = map[string]int{"": 0, "bajo": 1, "medio": 2, "alto": 3}

// Nivel devuelve el nivel más alto de la lista ("" si no hay hallazgos).
func Nivel(hs []Hallazgo) string {
	max := ""
	for _, h := range hs {
		if pesos[h.Nivel] > pesos[max] {
			max = h.Nivel
		}
	}
	return max
}

type sentencia struct {
	texto string // en mayúsculas y con espacios simples
	orig  string
	linea int
}

// dividir quita comentarios y separa por ";" respetando cadenas y bloques $$.
func dividir(sql string) []sentencia {
	var out []sentencia
	var b strings.Builder
	linea, inicio := 1, 0
	enComillas, enDolar := false, false
	runes := []rune(sql)
	cerrar := func() {
		t := strings.TrimSpace(b.String())
		if t != "" {
			out = append(out, sentencia{texto: normal(t), orig: t, linea: inicio})
		}
		b.Reset()
		inicio = 0
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\n' {
			linea++
		}
		switch {
		case !enComillas && !enDolar && r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			if i < len(runes) {
				linea++
				b.WriteRune('\n')
			}
			continue
		case !enComillas && !enDolar && r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				if runes[i] == '\n' {
					linea++
				}
				i++
			}
			i++
			continue
		case !enDolar && r == '\'':
			enComillas = !enComillas
		case !enComillas && r == '$' && i+1 < len(runes) && runes[i+1] == '$':
			enDolar = !enDolar
			b.WriteString("$$")
			i++
			continue
		case !enComillas && !enDolar && r == ';':
			cerrar()
			continue
		}
		if inicio == 0 && !strings.ContainsRune(" \t\r\n", r) {
			inicio = linea
		}
		b.WriteRune(r)
	}
	cerrar()
	return out
}

var espacios = regexp.MustCompile(`\s+`)

func normal(s string) string { return strings.ToUpper(espacios.ReplaceAllString(s, " ")) }

const ident = `"?([A-Z0-9_]+(?:\.[A-Z0-9_]+)?)"?`

var (
	reCreateTable = regexp.MustCompile(`^CREATE (?:UNLOGGED |TEMP |TEMPORARY )?TABLE (?:IF NOT EXISTS )?` + ident)
	reAlterTable  = regexp.MustCompile(`^ALTER TABLE (?:IF EXISTS )?(?:ONLY )?` + ident)
	reDropTable   = regexp.MustCompile(`^DROP TABLE (?:IF EXISTS )?([A-Z0-9_.", ]+?)(?: CASCADE| RESTRICT)?$`)
	reIndex       = regexp.MustCompile(`^CREATE (UNIQUE )?INDEX (CONCURRENTLY )?(?:IF NOT EXISTS )?(?:"?[A-Z0-9_]+"? )?ON (?:ONLY )?` + ident)
	reDropIndex   = regexp.MustCompile(`^DROP INDEX (CONCURRENTLY )?`)
	reInsert      = regexp.MustCompile(`^INSERT INTO ` + ident)
	reUpdate      = regexp.MustCompile(`^UPDATE (?:ONLY )?` + ident + ` .*SET `)
	reDelete      = regexp.MustCompile(`^DELETE FROM (?:ONLY )?` + ident)
	reTruncate    = regexp.MustCompile(`^TRUNCATE (?:TABLE )?(?:ONLY )?` + ident)
	reDropCol     = regexp.MustCompile(`DROP (?:COLUMN )?(?:IF EXISTS )?"?([A-Z0-9_]+)"?(?: CASCADE| RESTRICT)?(?:,|$)`)
	reAddColumn   = regexp.MustCompile(`ADD (?:COLUMN )?(?:IF NOT EXISTS )?"?([A-Z0-9_]+)"? ([^,]+)`)
	reTipo        = regexp.MustCompile(`ALTER (?:COLUMN )?"?([A-Z0-9_]+)"? (?:SET DATA )?TYPE `)
	reSetNotNull  = regexp.MustCompile(`ALTER (?:COLUMN )?"?([A-Z0-9_]+)"? SET NOT NULL`)
	reAddFK       = regexp.MustCompile(`ADD (?:CONSTRAINT "?[A-Z0-9_]+"? )?FOREIGN KEY`)
	reRename      = regexp.MustCompile(`RENAME (?:COLUMN |CONSTRAINT )?(?:"?[A-Z0-9_]+"? )?TO `)
)

// Palabras que siguen a DROP dentro de ALTER TABLE y no son nombres de columna.
var noEsColumna = map[string]bool{"DEFAULT": true, "CONSTRAINT": true, "IDENTITY": true, "EXPRESSION": true, "NOT": true}

func limpiar(t string) string {
	t = strings.ToLower(strings.Trim(t, `" `))
	return strings.TrimPrefix(t, "public.")
}

// TablasAfectadas devuelve las tablas que toca el SQL (para detectar conflictos al fusionar ramas).
func TablasAfectadas(sql string) []string {
	set := map[string]bool{}
	for _, s := range dividir(sql) {
		t := s.texto
		for _, re := range []*regexp.Regexp{reCreateTable, reAlterTable, reInsert, reUpdate, reDelete, reTruncate} {
			if m := re.FindStringSubmatch(t); m != nil {
				set[limpiar(m[len(m)-1])] = true
			}
		}
		if m := reIndex.FindStringSubmatch(t); m != nil {
			set[limpiar(m[3])] = true
		}
		if m := reDropTable.FindStringSubmatch(t); m != nil {
			for _, n := range strings.Split(m[1], ",") {
				if n = limpiar(n); n != "" {
					set[n] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Analizar revisa el UP (y si existe el DOWN) de una migración.
func Analizar(up, down string) []Hallazgo {
	var hs []Hallazgo
	add := func(s sentencia, nivel, regla, msg, sug string) {
		hs = append(hs, Hallazgo{Nivel: nivel, Regla: regla, Mensaje: msg, Sugerencia: sug, Linea: s.linea})
	}
	sentencias := dividir(up)
	nuevas := map[string]bool{} // tablas creadas en esta misma migración: tocarlas es seguro
	for _, s := range sentencias {
		if m := reCreateTable.FindStringSubmatch(s.texto); m != nil {
			nuevas[limpiar(m[1])] = true
		}
	}
	sinTx := strings.Contains(strings.ToLower(up), "marga:sin-transaccion")

	for _, s := range sentencias {
		t := s.texto
		if m := reDropTable.FindStringSubmatch(t); m != nil {
			add(s, "alto", "borra-tabla", "Borra la tabla "+limpiar(m[1])+" y todos sus datos.",
				"Asegúrate de tener respaldo; considera renombrarla primero y borrarla en una migración posterior.")
		}
		if strings.HasPrefix(t, "TRUNCATE") {
			add(s, "alto", "truncate", "Vacía la tabla por completo.", "")
		}
		if m := reDelete.FindStringSubmatch(t); m != nil && !strings.Contains(t, " WHERE ") {
			add(s, "alto", "delete-sin-where", "DELETE sin WHERE: borra todas las filas de "+limpiar(m[1])+".", "Agrega una condición WHERE.")
		}
		if m := reUpdate.FindStringSubmatch(t); m != nil && !strings.Contains(t, " WHERE ") && !nuevas[limpiar(m[1])] {
			add(s, "medio", "update-sin-where", "UPDATE sin WHERE: modifica todas las filas de "+limpiar(m[1])+".",
				"En tablas grandes hazlo por lotes.")
		}
		if strings.Contains(t, "CONCURRENTLY") && !sinTx {
			add(s, "alto", "concurrently-en-transaccion", "CONCURRENTLY no puede ejecutarse dentro de una transacción y Marga usa una por migración.",
				"Agrega el comentario -- marga:sin-transaccion al inicio del archivo.")
		}

		if m := reIndex.FindStringSubmatch(t); m != nil && m[2] == "" && !nuevas[limpiar(m[3])] {
			add(s, "medio", "indice-bloqueante", "Crear el índice bloquea las escrituras en "+limpiar(m[3])+" mientras se construye.",
				"Usa CREATE INDEX CONCURRENTLY (con -- marga:sin-transaccion) en tablas grandes.")
		}
		if m := reDropIndex.FindStringSubmatch(t); m != nil && m[1] == "" {
			add(s, "bajo", "drop-index", "Borrar el índice bloquea la tabla brevemente y puede volver lentas algunas consultas.", "")
		}

		m := reAlterTable.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		tabla := limpiar(m[1])
		if nuevas[tabla] {
			continue
		}
		resto := t[len(m[0]):]
		for _, c := range reDropCol.FindAllStringSubmatch(resto, -1) {
			if noEsColumna[c[1]] { // DROP DEFAULT, DROP CONSTRAINT x, DROP IDENTITY…
				continue
			}
			add(s, "alto", "borra-columna", "Borra la columna "+tabla+"."+strings.ToLower(c[1])+" y sus datos.",
				"Primero deja de usarla en el código; bórrala en un despliegue posterior.")
		}
		for _, c := range reAddColumn.FindAllStringSubmatch(resto, -1) {
			def := c[2]
			if strings.Contains(def, "NOT NULL") && !strings.Contains(def, "DEFAULT") &&
				!strings.Contains(def, "SERIAL") && !strings.Contains(def, "GENERATED") {
				add(s, "alto", "not-null-sin-default", "Agrega "+tabla+"."+strings.ToLower(c[1])+" NOT NULL sin DEFAULT: falla si la tabla ya tiene filas.",
					"Agrega un DEFAULT, o créala aceptando NULL, llénala y después pon SET NOT NULL.")
			}
		}
		if c := reTipo.FindStringSubmatch(resto); c != nil {
			add(s, "medio", "cambio-de-tipo", "Cambiar el tipo de "+tabla+"."+strings.ToLower(c[1])+" reescribe la tabla y la bloquea mientras tanto.", "")
		}
		if c := reSetNotNull.FindStringSubmatch(resto); c != nil {
			add(s, "medio", "set-not-null", "SET NOT NULL en "+tabla+"."+strings.ToLower(c[1])+" revisa toda la tabla y falla si hay valores NULL.", "")
		}
		if reAddFK.MatchString(resto) && !strings.Contains(resto, "NOT VALID") {
			add(s, "medio", "fk-validacion", "La llave foránea valida todas las filas de "+tabla+" con la tabla bloqueada.",
				"Agrégala con NOT VALID y valídala después con VALIDATE CONSTRAINT.")
		}
		if reRename.MatchString(resto) {
			add(s, "medio", "renombrar", "Renombrar rompe el código que todavía use el nombre anterior.", "Despliega primero el código compatible con ambos nombres.")
		}
	}

	if strings.TrimSpace(quitarComentarios(down)) == "" {
		hs = append(hs, Hallazgo{Nivel: "bajo", Regla: "sin-down", Mensaje: "No tiene script DOWN: no se podrá revertir con marga down.", Linea: 0})
	}
	sort.SliceStable(hs, func(i, j int) bool { return pesos[hs[i].Nivel] > pesos[hs[j].Nivel] })
	return hs
}

func quitarComentarios(sql string) string {
	var b strings.Builder
	for _, s := range dividir(sql) {
		b.WriteString(s.orig)
	}
	return b.String()
}
