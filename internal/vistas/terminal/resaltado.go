package terminal

// Resaltado de SQL para la terminal: palabras reservadas, tipos, cadenas,
// números, comentarios, nombres de función, operadores, puntuación y
// paréntesis / corchetes / llaves (un color por nivel de anidación, para ver
// qué cierra con qué). El cuerpo de las funciones ($$ … $$) se resalta igual.

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

var palabrasReservadas = conjunto(`
	ABORT ACTION ADD AFTER ALL ALTER ALWAYS ANALYZE AND ANY ARRAY AS ASC ATOMIC AUTHORIZATION
	BEFORE BEGIN BETWEEN BY CACHE CALL CALLED CASCADE CASE CAST CHECK COLLATE COLUMN COMMENT
	COMMIT CONCURRENTLY CONFLICT CONSTRAINT CONSTRAINTS COPY COST CREATE CROSS CURRENT_DATE
	CURRENT_TIMESTAMP CURRENT_USER CURSOR CYCLE DATA DECLARE DEFAULT DEFERRABLE DEFERRED DEFINER
	DELETE DESC DISABLE DISTINCT DO DOMAIN DROP EACH ELSE ELSIF ENABLE END ENUM ESCAPE EXCEPT
	EXCEPTION EXCLUDE EXECUTE EXISTS EXPLAIN EXTENSION FALSE FETCH FILTER FIRST FOLLOWING FOR
	FOREIGN FROM FULL FUNCTION GENERATED GRANT GROUP HAVING IDENTITY IF ILIKE IMMUTABLE IN
	INCLUDE INCREMENT INDEX INHERITS INITIALLY INNER INOUT INSERT INSTEAD INTERSECT INTO INVOKER
	IS ISNULL JOIN KEY LANGUAGE LAST LATERAL LEAKPROOF LEFT LIKE LIMIT LOCAL LOCK LOOP MATCH
	MATERIALIZED MAXVALUE MINVALUE NATURAL NEW NEXT NO NOT NOTHING NOTIFY NOTNULL NOWAIT NULL
	NULLS OF OFF OFFSET OLD ON ONLY OPTION OR ORDER OUT OUTER OVER OWNED OWNER PARALLEL PARTITION
	PERFORM PLACING POLICY PRECEDING PRIMARY PROCEDURE PUBLICATION RAISE RANGE RECURSIVE
	REFERENCES REFERENCING REFRESH RENAME REPLACE RESTRICT RETURN RETURNING RETURNS REVOKE RIGHT
	ROLLBACK ROW ROWS RULE SAVEPOINT SCHEMA SECURITY SELECT SEQUENCE SESSION SET SETOF SHOW
	SIMILAR SOME STABLE START STATEMENT STRICT TABLE TABLESPACE TEMP TEMPORARY THEN TO TRIGGER
	TRUE TRUNCATE TYPE UNBOUNDED UNION UNIQUE UNLOGGED UNTIL UPDATE USING VACUUM VALID VALIDATE
	VALUE VALUES VARIADIC VIEW VOLATILE WHEN WHERE WHILE WINDOW WITH WITHOUT ZONE`)

var tiposDeDato = conjunto(`
	BIGINT BIGSERIAL BIT BOOL BOOLEAN BOX BYTEA CHAR CHARACTER CIDR CITEXT DATE DATERANGE
	DECIMAL DOUBLE FLOAT FLOAT4 FLOAT8 HSTORE INET INT INT2 INT4 INT8 INTEGER INTERVAL JSON JSONB
	MONEY NUMERIC OID PRECISION REAL RECORD REGCLASS SERIAL SERIAL4 SERIAL8 SMALLINT SMALLSERIAL
	TEXT TIME TIMESTAMP TIMESTAMPTZ TIMETZ TSQUERY TSVECTOR UUID VARCHAR VARYING VOID XML TRIGGER
	ANYELEMENT ANYARRAY`)

func conjunto(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// EstilosSQL son los estilos del resaltado, sacados de un tema.
// EstilosSQL son los estilos del resaltado, sacados de un tema.
type EstilosSQL struct {
	Clave, Tipo, Cadena, Numero, Comentario, Funcion, Operador, Puntuacion, Comillas, Parametro, Texto lipgloss.Style
	Parentesis                                                                                         []lipgloss.Style
}

func NuevosEstilosSQL(t Tema) EstilosSQL {
	e := EstilosSQL{
		Clave:      lipgloss.NewStyle().Foreground(Color(t.Clave)).Bold(true),
		Tipo:       lipgloss.NewStyle().Foreground(Color(t.TipoDato)),
		Cadena:     lipgloss.NewStyle().Foreground(Color(t.Cadena)),
		Numero:     lipgloss.NewStyle().Foreground(Color(t.Numero)),
		Comentario: lipgloss.NewStyle().Foreground(Color(t.Comentario)).Italic(true),
		Funcion:    lipgloss.NewStyle().Foreground(Color(t.Funcion)),
		Operador:   lipgloss.NewStyle().Foreground(Color(t.Operador)),
		Puntuacion: lipgloss.NewStyle().Foreground(Color(t.Puntuacion)),
		Comillas:   lipgloss.NewStyle().Foreground(Color(t.Comillas)),
		Parametro:  lipgloss.NewStyle().Foreground(Color(t.Parametro)).Italic(true),
		Texto:      lipgloss.NewStyle().Foreground(Color(t.Texto)),
	}
	for _, c := range t.Parentesis {
		e.Parentesis = append(e.Parentesis, lipgloss.NewStyle().Foreground(Color(c)).Bold(true))
	}
	if len(e.Parentesis) == 0 {
		e.Parentesis = []lipgloss.Style{e.Puntuacion}
	}
	return e
}

// pintar aplica un estilo línea por línea: así un comentario o una cadena de
// varias líneas no rompe la numeración ni el ajuste de líneas.
func pintar(b *strings.Builder, s lipgloss.Style, texto string) {
	for i, l := range strings.Split(texto, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if l != "" {
			b.WriteString(s.Render(l))
		}
	}
}

func esInicioPalabra(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func esDePalabra(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ResaltarSQL devuelve el SQL con colores de la terminal.
func ResaltarSQL(sql string, e EstilosSQL) string {
	var b strings.Builder
	r := []rune(sql)
	nivel := 0
	paren := func(abre bool) lipgloss.Style {
		if !abre && nivel > 0 {
			nivel--
		}
		s := e.Parentesis[nivel%len(e.Parentesis)]
		if abre {
			nivel++
		}
		return s
	}
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		// Comentario de línea
		case c == '-' && i+1 < len(r) && r[i+1] == '-':
			j := i
			for j < len(r) && r[j] != '\n' {
				j++
			}
			pintar(&b, e.Comentario, string(r[i:j]))
			i = j
		// Comentario de bloque
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			j := i + 2
			for j+1 < len(r) && !(r[j] == '*' && r[j+1] == '/') {
				j++
			}
			j = min(j+2, len(r))
			pintar(&b, e.Comentario, string(r[i:j]))
			i = j
		// Cadena: 'texto' (con '' dentro)
		case c == '\'':
			j := i + 1
			for j < len(r) {
				if r[j] == '\'' {
					if j+1 < len(r) && r[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			pintar(&b, e.Cadena, string(r[i:j]))
			i = j
		// Identificador entre comillas
		case c == '"':
			j := i + 1
			for j < len(r) && r[j] != '"' {
				j++
			}
			j = min(j+1, len(r))
			pintar(&b, e.Comillas, string(r[i:j]))
			i = j
		// $1, o un bloque $$ … $$ / $etiqueta$ … $etiqueta$
		case c == '$':
			if et := etiquetaDolarRunes(r, i); et != "" {
				n := len([]rune(et))
				fin := strings.Index(string(r[i+n:]), et)
				b.WriteString(e.Cadena.Render(et))
				if fin < 0 {
					b.WriteString(ResaltarSQL(string(r[i+n:]), e))
					i = len(r)
					break
				}
				cuerpo := []rune(string(r[i+n:])[:fin])
				b.WriteString(ResaltarSQL(string(cuerpo), e))
				b.WriteString(e.Cadena.Render(et))
				i += n + len(cuerpo) + n
				break
			}
			j := i + 1
			for j < len(r) && unicode.IsDigit(r[j]) {
				j++
			}
			pintar(&b, e.Parametro, string(r[i:j]))
			i = j
		// Número
		case unicode.IsDigit(c) || (c == '.' && i+1 < len(r) && unicode.IsDigit(r[i+1]) && (i == 0 || !esDePalabra(r[i-1]))):
			j := i
			for j < len(r) && (unicode.IsDigit(r[j]) || r[j] == '.' || r[j] == 'e' || r[j] == 'E') {
				j++
			}
			b.WriteString(e.Numero.Render(string(r[i:j])))
			i = j
		// Palabra: reservada, tipo, función o nombre
		case esInicioPalabra(c):
			j := i
			for j < len(r) && esDePalabra(r[j]) {
				j++
			}
			w := string(r[i:j])
			k := j
			for k < len(r) && r[k] == ' ' {
				k++
			}
			sigParen := k < len(r) && r[k] == '('
			up := strings.ToUpper(w)
			anterior := ' '
			if i > 0 {
				anterior = r[i-1]
			}
			switch {
			case anterior == '.' && sigParen:
				b.WriteString(e.Funcion.Render(w))
			case anterior == '.':
				b.WriteString(e.Texto.Render(w))
			case tiposDeDato[up]:
				b.WriteString(e.Tipo.Render(w))
			case palabrasReservadas[up]:
				b.WriteString(e.Clave.Render(w))
			case sigParen:
				b.WriteString(e.Funcion.Render(w))
			default:
				b.WriteString(e.Texto.Render(w))
			}
			i = j
		case c == '(' || c == '[' || c == '{':
			b.WriteString(paren(true).Render(string(c)))
			i++
		case c == ')' || c == ']' || c == '}':
			b.WriteString(paren(false).Render(string(c)))
			i++
		case c == ',' || c == ';' || c == '.':
			b.WriteString(e.Puntuacion.Render(string(c)))
			i++
		case c == ':' && i+1 < len(r) && r[i+1] == ':':
			b.WriteString(e.Operador.Render("::"))
			i += 2
		case strings.ContainsRune("=<>!+-*/%|&^~@#?:", c):
			j := i
			for j < len(r) && strings.ContainsRune("=<>!+-*/%|&^~@#?", r[j]) && !(r[j] == '-' && j+1 < len(r) && r[j+1] == '-') {
				j++
			}
			if j == i {
				j++
			}
			b.WriteString(e.Operador.Render(string(r[i:j])))
			i = j
		default:
			b.WriteRune(c)
			i++
		}
	}
	return b.String()
}

func etiquetaDolarRunes(r []rune, i int) string {
	j := i + 1
	for j < len(r) && (r[j] == '_' || unicode.IsLetter(r[j]) || (j > i+1 && unicode.IsDigit(r[j]))) {
		j++
	}
	if j < len(r) && r[j] == '$' {
		return string(r[i : j+1])
	}
	return ""
}

// Resaltar: el DDL que imprimen `marga esquema` y `\d`, con el tema
// del usuario. Si la salida no es una terminal, lipgloss lo deja sin colores.
func Resaltar(sql string) string {
	return ResaltarSQL(sql, NuevosEstilosSQL(Actual()))
}
