package cli

// Consola interactiva de `marga sql`: edición de varias líneas, historial que se
// guarda entre sesiones, autocompletado (Tab) de tablas, columnas, vistas,
// funciones y palabras clave, y comandos con "\" (\d, \e, \i, \?, \q).
//
// Cada sentencia se escribe en un pequeño programa de Bubble Tea que corre en
// línea (sin pantalla completa): al terminarla, el texto queda en la terminal y
// el resultado se imprime debajo, así se conserva el scroll como en psql.

import (
	"encoding/json"
	"fmt"
	"github.com/zara-te05/Marga/internal/vistas/terminal"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

const maxHistorialSQL = 500

var palabrasClave = strings.Fields(`SELECT FROM WHERE AND OR NOT NULL IS IN LIKE ILIKE BETWEEN EXISTS
	INSERT INTO VALUES UPDATE SET DELETE RETURNING JOIN LEFT RIGHT INNER FULL OUTER CROSS ON USING
	GROUP BY ORDER HAVING LIMIT OFFSET DISTINCT AS ASC DESC UNION ALL EXCEPT INTERSECT WITH
	CASE WHEN THEN ELSE END CREATE ALTER DROP TABLE VIEW INDEX FUNCTION PROCEDURE TRIGGER TYPE
	ADD COLUMN CONSTRAINT PRIMARY KEY FOREIGN REFERENCES UNIQUE CHECK DEFAULT CASCADE
	BEGIN COMMIT ROLLBACK EXPLAIN ANALYZE COUNT SUM AVG MIN MAX COALESCE NOW TRUE FALSE`)

// ---------------------------------------------------------------- historial

func archivoHistorialSQL() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".marga", "historial_sql.jsonl")
}

// leerHistorialSQL: una sentencia por línea, en JSON (pueden tener saltos de línea).
func leerHistorialSQL() []string {
	b, err := os.ReadFile(archivoHistorialSQL())
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		var s string
		if json.Unmarshal([]byte(l), &s) == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func guardarHistorialSQL(h []string) {
	ruta := archivoHistorialSQL()
	if ruta == "" {
		return
	}
	if len(h) > maxHistorialSQL {
		h = h[len(h)-maxHistorialSQL:]
	}
	var b strings.Builder
	for _, s := range h {
		j, _ := json.Marshal(s)
		b.Write(j)
		b.WriteByte('\n')
	}
	_ = os.MkdirAll(filepath.Dir(ruta), 0o700)
	_ = os.WriteFile(ruta, []byte(b.String()), 0o600)
}

func AgregarHistorial(h []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || (len(h) > 0 && h[len(h)-1] == s) {
		return h
	}
	return append(h, s)
}

// ---------------------------------------------------------------- autocompletado

// palabrasDe junta los nombres del esquema para el autocompletado.
func palabrasDe(e esquema.Esquema) []string {
	set := map[string]bool{}
	for _, t := range e.Tablas {
		set[t.Nombre] = true
		for _, c := range t.Columnas {
			set[c.Nombre] = true
		}
	}
	for _, v := range e.Vistas {
		set[v.Nombre] = true
	}
	for _, f := range e.Funciones {
		set[f.Nombre] = true
	}
	for _, t := range e.Tipos {
		set[t.Nombre] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func esDeIdentificador(r rune) bool {
	return r == '_' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Completar devuelve las opciones para `prefijo`. Las palabras clave respetan
// si se está escribiendo en minúsculas o mayúsculas.
func Completar(prefijo string, nombres []string) []string {
	if prefijo == "" {
		return nil
	}
	p := strings.ToLower(prefijo)
	minus := prefijo == strings.ToLower(prefijo)
	var out []string
	visto := map[string]bool{}
	for _, n := range nombres {
		if strings.HasPrefix(strings.ToLower(n), p) && !visto[n] {
			out = append(out, n)
			visto[n] = true
		}
	}
	for _, k := range palabrasClave {
		if strings.HasPrefix(strings.ToLower(k), p) {
			if minus {
				k = strings.ToLower(k)
			}
			if !visto[k] {
				out = append(out, k)
				visto[k] = true
			}
		}
	}
	return out
}

func PrefijoComun(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	p := []rune(xs[0])
	for _, x := range xs[1:] {
		r := []rune(x)
		i := 0
		for i < len(p) && i < len(r) && unicode.ToLower(p[i]) == unicode.ToLower(r[i]) {
			i++
		}
		p = p[:i]
	}
	return string(p)
}

// ---------------------------------------------------------------- editor de una sentencia

// estilosConsola: el prompt y las sugerencias, con el tema del usuario.
func estiloPrompt() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(terminal.Color(terminal.Actual().Acento)).Bold(true)
}

func estiloSugerencia() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(terminal.Color(terminal.Actual().Tenue))
}

type editorSQL struct {
	ta        textarea.Model
	historial []string
	pos       int    // posición en el historial; len(historial) = escribiendo algo nuevo
	borrador  string // lo que se estaba escribiendo antes de recorrer el historial
	Nombres   []string
	opciones  []string // opciones de autocompletado a mostrar
	ancho     int      // de la terminal
	listo     bool
	salir     bool
}

func nuevoEditorSQL(inicial string, historial, nombres []string) editorSQL {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetPromptFunc(5, func(i int) string {
		if i == 0 {
			return estiloPrompt().Render("sql> ")
		}
		return estiloPrompt().Render("...> ")
	})
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.SetHeight(1)
	// El textarea solo guarda el texto y el cursor; la vista la dibuja Marga con
	// colores. Muy ancho para que no parta líneas por su cuenta.
	ta.SetWidth(10000)
	ta.Placeholder = "SQL terminado en ;  ·  \\? ayuda  ·  \\q salir"
	ta.Focus()
	ta.SetValue(inicial)
	return editorSQL{ta: ta, historial: historial, pos: len(historial), Nombres: nombres}
}

func (m editorSQL) Init() tea.Cmd { return nil }

// Completo: la sentencia ya se puede ejecutar.
func Completo(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasSuffix(t, ";") || strings.HasPrefix(t, `\`) || t == "salir" || t == "exit" || t == "quit"
}

// palabraAntesDelCursor devuelve el identificador que termina en el cursor.
func (m editorSQL) palabraAntesDelCursor() string {
	lineas := strings.Split(m.ta.Value(), "\n")
	if m.ta.Line() >= len(lineas) {
		return ""
	}
	r := []rune(lineas[m.ta.Line()])
	li := m.ta.LineInfo()
	col := min(li.StartColumn+li.ColumnOffset, len(r))
	i := col
	for i > 0 && esDeIdentificador(r[i-1]) {
		i--
	}
	return string(r[i:col])
}

func (m *editorSQL) ajustarAlto() {
	m.ta.SetHeight(min(max(m.ta.LineCount(), 1), 20))
}

func (m editorSQL) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ancho = msg.Width
	case tea.KeyMsg:
		m.opciones = nil
		switch msg.String() {
		case "ctrl+d":
			if strings.TrimSpace(m.ta.Value()) == "" {
				m.salir = true
				return m, tea.Quit
			}
		case "ctrl+c":
			if strings.TrimSpace(m.ta.Value()) == "" {
				m.salir = true
				return m, tea.Quit
			}
			m.ta.Reset()
			m.ajustarAlto()
			return m, nil
		case "enter":
			if Completo(m.ta.Value()) {
				m.listo = true
				m.ta.Blur()
				return m, tea.Quit
			}
		case "ctrl+j": // Ctrl+Enter en muchas terminales: ejecuta aunque falte el ;
			if strings.TrimSpace(m.ta.Value()) != "" {
				m.listo = true
				m.ta.Blur()
				return m, tea.Quit
			}
		case "up":
			if m.ta.Line() == 0 && m.ta.LineInfo().RowOffset == 0 && m.pos > 0 {
				if m.pos == len(m.historial) {
					m.borrador = m.ta.Value()
				}
				m.pos--
				m.ta.SetValue(m.historial[m.pos])
				m.ajustarAlto()
				return m, nil
			}
		case "down":
			li := m.ta.LineInfo()
			if m.ta.Line() == m.ta.LineCount()-1 && li.RowOffset == li.Height-1 && m.pos < len(m.historial) {
				m.pos++
				if m.pos == len(m.historial) {
					m.ta.SetValue(m.borrador)
				} else {
					m.ta.SetValue(m.historial[m.pos])
				}
				m.ajustarAlto()
				return m, nil
			}
		case "tab":
			p := m.palabraAntesDelCursor()
			// "esquema.tabla": se completa solo lo que va después del punto.
			if i := strings.LastIndex(p, "."); i >= 0 {
				p = p[i+1:]
			}
			ops := Completar(p, m.Nombres)
			switch {
			case len(ops) == 1:
				m.ta.InsertString(string([]rune(ops[0])[utf8.RuneCountInString(p):]) + " ")
			case len(ops) > 1:
				if comun := PrefijoComun(ops); utf8.RuneCountInString(comun) > utf8.RuneCountInString(p) {
					m.ta.InsertString(string([]rune(comun)[utf8.RuneCountInString(p):]))
				}
				m.opciones = ops
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.ajustarAlto()
	return m, cmd
}

func (m editorSQL) View() string {
	if m.listo || m.salir {
		// Lo que queda en la terminal: la sentencia resaltada, sin cursor ni ayudas.
		var b strings.Builder
		for i, l := range strings.Split(terminal.Resaltar(m.ta.Value()), "\n") {
			p := "sql> "
			if i > 0 {
				p = "...> "
			}
			b.WriteString(estiloPrompt().Render(p) + l + "\n")
		}
		return b.String()
	}
	v := m.vistaResaltada()
	if len(m.opciones) > 0 {
		ops := m.opciones
		extra := ""
		if len(ops) > 12 {
			extra = fmt.Sprintf("  …y %d más", len(ops)-12)
			ops = ops[:12]
		}
		v += "\n" + estiloSugerencia().Render("     "+strings.Join(ops, "  ")+extra)
	}
	return v
}

// leerSentencia abre el editor y devuelve lo escrito. salir=true con \q o Ctrl+D.
func leerSentencia(inicial string, historial, nombres []string) (string, bool, error) {
	res, err := tea.NewProgram(nuevoEditorSQL(inicial, historial, nombres)).Run()
	if err != nil {
		return "", true, err
	}
	m := res.(editorSQL)
	return m.ta.Value(), m.salir, nil
}

// ---------------------------------------------------------------- la Consola

const ayudaConsolaSQL = `  SQL terminado en ;          se ejecuta (Enter); sin ; Enter agrega una línea
  ↑ ↓                         historial (se guarda entre sesiones)
  Tab                         completa tablas, columnas, vistas, funciones y palabras clave
  Ctrl+C                      borra lo escrito (con la línea vacía, sale)
  \d                          lista tablas, vistas, funciones y tipos
  \d NOMBRE                   muestra su DDL
  \e                          abre la última sentencia en tu editor ($EDITOR)
  \i ARCHIVO                  ejecuta un archivo .sql
  \q                          salir`

// consolaInteractiva es la Consola de `marga sql` cuando la entrada es una terminal.
func consolaInteractiva(c *cobra.Command, leerEsquema func() (esquema.Esquema, error), correr func(string) error) error {
	esq, err := leerEsquema()
	if err != nil {
		linea(c, "%s No se pudo leer el esquema para autocompletar: %v", sAviso.Render("!"), err)
	}
	nombres := palabrasDe(esq)
	historial := leerHistorialSQL()
	defer func() { guardarHistorialSQL(historial) }()

	inicial := ""
	for {
		texto, salir, err := leerSentencia(inicial, historial, nombres)
		inicial = ""
		if err != nil {
			return err
		}
		if salir {
			return nil
		}
		t := strings.TrimSpace(texto)
		if t == "" {
			continue
		}
		if t != `\q` && t != "salir" && t != "exit" && t != "quit" {
			historial = AgregarHistorial(historial, t)
		}
		switch {
		case t == `\q` || t == "salir" || t == "exit" || t == "quit":
			return nil
		case t == `\?` || t == `\h` || t == "ayuda":
			linea(c, "%s", sTenue.Render(ayudaConsolaSQL))
		case t == `\d` || strings.HasPrefix(t, `\d `):
			if e, err := leerEsquema(); err != nil {
				linea(c, "%s %v", sError.Render("✗"), err)
			} else {
				esq, nombres = e, palabrasDe(e)
				describir(c, esq, strings.TrimSpace(strings.TrimPrefix(t, `\d`)))
			}
		case t == `\e`:
			ultima := ""
			for i := len(historial) - 2; i >= 0; i-- { // la última que no sea el propio \e
				if !strings.HasPrefix(historial[i], `\`) {
					ultima = historial[i]
					break
				}
			}
			editado, err := editarTexto(ultima)
			if err != nil {
				linea(c, "%s %v", sError.Render("✗"), err)
				continue
			}
			inicial = strings.TrimRight(editado, "\n ") // queda en el editor para revisarla y ejecutarla
		case strings.HasPrefix(t, `\i `):
			b, err := os.ReadFile(strings.Trim(strings.TrimSpace(t[3:]), `"'`))
			if err != nil {
				linea(c, "%s %v", sError.Render("✗"), err)
				continue
			}
			if err := correr(proyecto.Normalizar(string(b))); err != nil {
				linea(c, "%s %v", sError.Render("✗"), err)
			}
		case strings.HasPrefix(t, `\`):
			linea(c, "%s comando desconocido %s (\\? para ver la ayuda)", sError.Render("✗"), t)
		default:
			if err := correr(texto); err != nil {
				linea(c, "%s %v", sError.Render("✗"), err)
			}
			if !soloConsultas(texto) {
				// Pudo cambiar el esquema: refrescar el autocompletado.
				if e, err := leerEsquema(); err == nil {
					esq, nombres = e, palabrasDe(e)
				}
			}
		}
	}
}

// describir es \d: sin nombre lista los objetos; con nombre muestra su DDL.
func describir(c *cobra.Command, e esquema.Esquema, nombre string) {
	if nombre != "" {
		ddls := buscarDDL(e, nombre)
		if len(ddls) == 0 {
			linea(c, "%s no hay ninguna tabla, vista, función, trigger ni tipo llamado %q", sError.Render("✗"), nombre)
			return
		}
		linea(c, "%s", terminal.Resaltar(strings.Join(ddls, "\n\n")))
		return
	}
	fila := func(tipo, nombre, detalle string) {
		linea(c, "  %s %s %s", relleno(sTenue.Render(tipo), 14), relleno(sNegri.Render(nombre), 30), sTenue.Render(detalle))
	}
	for _, t := range e.Tablas {
		fila("tabla", t.Nombre, plural(len(t.Columnas), "columna", "columnas"))
	}
	for _, v := range e.Vistas {
		fila(map[bool]string{false: "vista", true: "vista mat."}[v.Materializada], v.Nombre, "")
	}
	for _, f := range e.Funciones {
		fila(map[bool]string{false: "función", true: "procedimiento"}[f.Procedimiento], f.Firma(), f.Retorno)
	}
	for _, t := range e.Tipos {
		fila("tipo", t.Nombre, strings.Join(t.Valores, ", "))
	}
	if len(e.Tablas)+len(e.Vistas)+len(e.Funciones)+len(e.Tipos) == 0 {
		linea(c, "%s", sTenue.Render("  (la base está vacía)"))
	}
}

// buscarDDL encuentra un objeto por nombre: tabla, vista, tipo, trigger o función
// (por nombre o firma; con varias sobrecargas devuelve todas).
func buscarDDL(e esquema.Esquema, buscado string) []string {
	var ddls []string
	for _, o := range e.Objetos(dialectoDe(e.Motor)) {
		base, _, _ := strings.Cut(o.Nombre, "(")
		etiqueta, _, _ := strings.Cut(o.Nombre, " ON ")
		if o.Nombre == buscado || (o.Tipo == "funcion" || o.Tipo == "procedimiento") && base == buscado ||
			o.Tipo == "trigger" && etiqueta == buscado {
			ddls = append(ddls, strings.TrimRight(o.DDL, "\n"))
		}
	}
	return ddls
}

// vistaResaltada dibuja lo que se está escribiendo con colores, como en un
// editor: se resalta todo el texto de una vez (así un comentario o un $$ de
// varias líneas se pinta bien), se pone el cursor y se parten las líneas largas.
func (m editorSQL) vistaResaltada() string {
	valor := m.ta.Value()
	prompt := estiloPrompt()
	cursor := lipgloss.NewStyle().Reverse(true)
	if valor == "" {
		return prompt.Render("sql> ") + cursor.Render(" ") + estiloSugerencia().Render(m.ta.Placeholder)
	}
	crudas := strings.Split(valor, "\n")
	pintadas := strings.Split(terminal.Resaltar(valor), "\n")
	fila := min(m.ta.Line(), len(crudas)-1)
	li := m.ta.LineInfo()
	col := li.StartColumn + li.ColumnOffset

	ancho := m.ancho
	if ancho <= 0 {
		ancho = 100
	}
	cuerpo := max(ancho-6, 10)
	var b strings.Builder
	for i, l := range pintadas {
		if i == fila {
			r := []rune(crudas[i])
			col = min(col, len(r))
			x := ansi.StringWidth(string(r[:col])) // columna en celdas de la terminal
			bajo := " "
			if col < len(r) {
				bajo = string(r[col])
			}
			total := ansi.StringWidth(crudas[i])
			l = ansi.Cut(l, 0, x) + cursor.Render(bajo) + ansi.Cut(l, x+ansi.StringWidth(bajo), max(total, x+1))
		}
		p := "sql> "
		if i > 0 {
			p = "...> "
		}
		for j, parte := range strings.Split(ansi.Hardwrap(l, cuerpo, true), "\n") {
			if j > 0 {
				p = "     "
			}
			if i > 0 || j > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(prompt.Render(p) + parte)
		}
	}
	return b.String()
}
