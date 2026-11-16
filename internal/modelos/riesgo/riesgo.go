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
	fuera string // como texto, pero sin el contenido de los bloques $$ (el cuerpo de una función)
	orig  string
	linea int
}

// etiquetaDolar devuelve "$$" o "$etiqueta$" si en runes[i] empieza una (si no, "").
// "$1" (un parámetro) no es una etiqueta.
func etiquetaDolar(runes []rune, i int) string {
	if runes[i] != '$' {
		return ""
	}
	j := i + 1
	for j < len(runes) && (runes[j] == '_' || runes[j] >= 'a' && runes[j] <= 'z' || runes[j] >= 'A' && runes[j] <= 'Z' ||
		j > i+1 && runes[j] >= '0' && runes[j] <= '9' || runes[j] > 127) {
		j++
	}
	if j < len(runes) && runes[j] == '$' {
		return string(runes[i : j+1])
	}
	return ""
}

// dividir quita comentarios y separa por ";" respetando cadenas y bloques
// $$ o $etiqueta$ (pg_get_functiondef escribe $function$).
func dividir(sql string) []sentencia {
	var out []sentencia
	var b, f strings.Builder
	linea, inicio := 1, 0
	enComillas := false
	dolar := "" // etiqueta del bloque $…$ abierto
	runes := []rune(sql)
	cerrar := func() {
		t := strings.TrimSpace(b.String())
		if t != "" {
			out = append(out, sentencia{texto: normal(t), fuera: normal(strings.TrimSpace(f.String())), orig: t, linea: inicio})
		}
		b.Reset()
		f.Reset()
		inicio = 0
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\n' {
			linea++
		}
		switch {
		case !enComillas && dolar == "" && r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			if i < len(runes) {
				linea++
				b.WriteRune('\n')
				f.WriteRune('\n')
			}
			continue
		case !enComillas && dolar == "" && r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				if runes[i] == '\n' {
					linea++
				}
				i++
			}
			i++
			continue
		case dolar == "" && r == '\'':
			enComillas = !enComillas
		case !enComillas && r == '$':
			if et := etiquetaDolar(runes, i); et != "" && (dolar == "" || et == dolar) {
				if dolar == "" {
					dolar = et
					f.WriteString("$$")
				} else {
					dolar = ""
				}
				if inicio == 0 {
					inicio = linea
				}
				b.WriteString(et)
				i += len([]rune(et)) - 1
				continue
			}
		case !enComillas && dolar == "" && r == ';':
			cerrar()
			continue
		}
		if inicio == 0 && !strings.ContainsRune(" \t\r\n", r) {
			inicio = linea
		}
		b.WriteRune(r)
		if dolar == "" {
			f.WriteRune(r)
		}
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

	reCreateFuncion = regexp.MustCompile(`^CREATE (?:OR REPLACE )?(?:FUNCTION|PROCEDURE) ` + ident + ` ?\(`)
	reAlterFuncion  = regexp.MustCompile(`^ALTER (?:FUNCTION|PROCEDURE|ROUTINE) ` + ident)
	reDropFuncion   = regexp.MustCompile(`^DROP (FUNCTION|PROCEDURE|ROUTINE) (?:IF EXISTS )?(.+?)( CASCADE| RESTRICT)?$`)
	reCreateVista   = regexp.MustCompile(`^CREATE (?:OR REPLACE )?(?:TEMP |TEMPORARY )?(?:RECURSIVE )?(MATERIALIZED )?VIEW (?:IF NOT EXISTS )?` + ident)
	reAlterVista    = regexp.MustCompile(`^(?:ALTER (?:MATERIALIZED )?VIEW (?:IF EXISTS )?|REFRESH MATERIALIZED VIEW (?:CONCURRENTLY )?)` + ident)
	reDropVista     = regexp.MustCompile(`^DROP (MATERIALIZED )?VIEW (?:IF EXISTS )?([A-Z0-9_.", ]+?)( CASCADE| RESTRICT)?$`)
	reCreateTrigger = regexp.MustCompile(`^CREATE (?:OR REPLACE )?(?:CONSTRAINT )?TRIGGER "?([A-Z0-9_]+)"? .*? ON (?:ONLY )?` + ident)
	reDropTrigger   = regexp.MustCompile(`^DROP TRIGGER (?:IF EXISTS )?"?([A-Z0-9_]+)"? ON (?:ONLY )?` + ident + `( CASCADE)?`)
	reCreateTipo    = regexp.MustCompile(`^CREATE TYPE ` + ident)
	reAlterTipo     = regexp.MustCompile(`^ALTER TYPE ` + ident)
	reDropTipo      = regexp.MustCompile(`^DROP TYPE (?:IF EXISTS )?([A-Z0-9_.", ]+?)( CASCADE| RESTRICT)?$`)
	reRenameValor   = regexp.MustCompile(`^ALTER TYPE ` + ident + ` RENAME VALUE `)
	reEjecuta       = regexp.MustCompile(`EXECUTE (?:FUNCTION|PROCEDURE) "?([A-Z0-9_]+(?:\.[A-Z0-9_]+)?)"? ?\(`)
)

// nombresDrop separa "a(int), public.b(text, date)" en ["a", "b"].
func nombresDrop(lista string) []string {
	var out []string
	nivel, ini := 0, 0
	partes := []string{}
	for i, r := range lista {
		switch r {
		case '(':
			nivel++
		case ')':
			nivel--
		case ',':
			if nivel == 0 {
				partes = append(partes, lista[ini:i])
				ini = i + 1
			}
		}
	}
	partes = append(partes, lista[ini:])
	for _, p := range partes {
		p = strings.TrimSpace(p)
		if i := strings.IndexAny(p, "( "); i >= 0 {
			p = p[:i]
		}
		if p = limpiar(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

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

// ObjetosAfectados devuelve los objetos que toca el SQL como "tipo:nombre"
// (tabla:clientes, funcion:calcular_total, vista:v_saldos, trigger:t ON pedidos,
// tipo:estado). Las funciones van sin argumentos: dos sobrecargas cuentan como el
// mismo objeto, que para detectar conflictos al fusionar es lo prudente.
func ObjetosAfectados(sql string) []string {
	set := map[string]bool{}
	for _, t := range TablasAfectadas(sql) {
		set["tabla:"+t] = true
	}
	for _, s := range dividir(sql) {
		t := s.fuera
		for _, re := range []*regexp.Regexp{reCreateFuncion, reAlterFuncion} {
			if m := re.FindStringSubmatch(t); m != nil {
				set["funcion:"+limpiar(m[1])] = true
			}
		}
		if m := reDropFuncion.FindStringSubmatch(t); m != nil {
			for _, n := range nombresDrop(m[2]) {
				set["funcion:"+n] = true
			}
		}
		if m := reCreateVista.FindStringSubmatch(t); m != nil {
			set["vista:"+limpiar(m[2])] = true
		}
		if m := reAlterVista.FindStringSubmatch(t); m != nil {
			set["vista:"+limpiar(m[1])] = true
		}
		if m := reDropVista.FindStringSubmatch(t); m != nil {
			for _, n := range strings.Split(m[2], ",") {
				if n = limpiar(n); n != "" {
					set["vista:"+n] = true
				}
			}
		}
		for _, re := range []*regexp.Regexp{reCreateTrigger, reDropTrigger} {
			if m := re.FindStringSubmatch(t); m != nil {
				set["trigger:"+strings.ToLower(m[1])+" ON "+limpiar(m[2])] = true
			}
		}
		for _, re := range []*regexp.Regexp{reCreateTipo, reAlterTipo} {
			if m := re.FindStringSubmatch(t); m != nil {
				set["tipo:"+limpiar(m[1])] = true
			}
		}
		if m := reDropTipo.FindStringSubmatch(t); m != nil {
			for _, n := range strings.Split(m[1], ",") {
				if n = limpiar(n); n != "" {
					set["tipo:"+n] = true
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
		if m := reCreateVista.FindStringSubmatch(s.fuera); m != nil && m[1] != "" {
			nuevas[limpiar(m[2])] = true // vista materializada recién creada: indexarla no bloquea a nadie
		}
	}
	sinTx := strings.Contains(strings.ToLower(up), "marga:sin-transaccion")

	// Funciones y vistas que la misma migración vuelve a crear: quitarlas es parte de recrearlas.
	recreadas := map[string]bool{}
	for _, s := range sentencias {
		if m := reCreateFuncion.FindStringSubmatch(s.fuera); m != nil {
			recreadas["funcion:"+limpiar(m[1])] = true
		}
		if m := reCreateVista.FindStringSubmatch(s.fuera); m != nil {
			recreadas["vista:"+limpiar(m[2])] = true
		}
	}
	// Funciones que ejecuta algún trigger, según el DOWN (que los vuelve a crear) o el propio UP.
	deTrigger := map[string]bool{}
	for _, s := range append(dividir(down), sentencias...) {
		if strings.Contains(s.fuera, " TRIGGER ") {
			for _, m := range reEjecuta.FindAllStringSubmatch(s.fuera, -1) {
				deTrigger[limpiar(m[1])] = true
			}
		}
	}
	// Valores de ENUM que el generador no pudo quitar (los deja como comentario).
	for i, l := range strings.Split(up, "\n") {
		if strings.Contains(l, "marga: quita a mano el valor") {
			hs = append(hs, Hallazgo{Nivel: "alto", Regla: "quita-valor-enum", Linea: i + 1,
				Mensaje:    "Quita un valor de un ENUM: PostgreSQL no lo permite directamente.",
				Sugerencia: "Hazlo a mano: actualiza las filas que lo usan, crea un tipo nuevo sin el valor y convierte las columnas. Si solo lo renombraste, usa ALTER TYPE … RENAME VALUE."})
		}
	}

	for _, s := range sentencias {
		t := s.texto
		f := s.fuera
		if m := reDropFuncion.FindStringSubmatch(f); m != nil {
			for _, n := range nombresDrop(m[2]) {
				switch {
				case m[3] == " CASCADE":
					add(s, "alto", "borra-funcion-cascade", "DROP … CASCADE borra la función "+n+" y también los triggers, vistas y columnas que la usan.",
						"Quita CASCADE y borra explícitamente lo que dependa de ella.")
				case recreadas["funcion:"+n]:
					// DROP + CREATE: la vuelve a crear más abajo.
				case deTrigger[n]:
					add(s, "alto", "borra-funcion-de-trigger", "Borra la función "+n+", que ejecuta un trigger: las inserciones o actualizaciones que dependían de él dejan de hacerlo.",
						"Confirma que el trigger también debe desaparecer.")
				default:
					add(s, "medio", "borra-funcion", "Borra la función "+n+": el código o las consultas que la llamen fallarán.", "")
				}
			}
		}
		if m := reDropVista.FindStringSubmatch(f); m != nil {
			for _, n := range strings.Split(m[2], ",") {
				if n = limpiar(n); n == "" {
					continue
				}
				if recreadas["vista:"+n] {
					add(s, "bajo", "recrea-vista", "La vista "+n+" se quita y se vuelve a crear: pierde sus permisos (GRANT) y, si es materializada, se vuelve a llenar.", "")
				} else {
					add(s, "medio", "borra-vista", "Borra la vista "+n+": las consultas y reportes que la usen fallarán.", "")
				}
				if m[3] == " CASCADE" {
					add(s, "alto", "borra-vista-cascade", "DROP VIEW … CASCADE también borra las vistas que dependen de "+n+".", "Borra explícitamente cada vista dependiente.")
				}
			}
		}
		if m := reDropTipo.FindStringSubmatch(f); m != nil && m[2] == " CASCADE" {
			add(s, "alto", "borra-tipo-cascade", "DROP TYPE … CASCADE borra también las columnas que usan el tipo, con sus datos.", "")
		}
		if strings.Contains(f, "DELETE FROM PG_ENUM") {
			add(s, "alto", "quita-valor-enum", "Borrar de pg_enum deja inservibles las filas que usaban ese valor.", "")
		}
		if reRenameValor.MatchString(f) {
			add(s, "medio", "renombra-valor-enum", "Renombrar un valor de ENUM rompe el código que todavía use el nombre anterior.", "")
		}
		if (strings.HasPrefix(f, "CREATE FUNCTION") || strings.HasPrefix(f, "CREATE OR REPLACE FUNCTION") ||
			strings.HasPrefix(f, "CREATE PROCEDURE") || strings.HasPrefix(f, "CREATE OR REPLACE PROCEDURE") ||
			strings.HasPrefix(f, "ALTER FUNCTION") || strings.HasPrefix(f, "ALTER PROCEDURE")) &&
			strings.Contains(f, "SECURITY DEFINER") && !strings.Contains(f, "SEARCH_PATH") {
			add(s, "alto", "security-definer-sin-search-path", "Función SECURITY DEFINER sin SET search_path: quien la llame puede crear tablas o funciones con el mismo nombre en otro esquema y ejecutarlas con los permisos del dueño.",
				"Agrega SET search_path = public, pg_temp a la función.")
		}
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
		if strings.Contains(f, "CONCURRENTLY") && !sinTx {
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
