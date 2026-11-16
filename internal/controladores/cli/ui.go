package cli

// marga ui: un panel en la terminal (como lazygit o k9s) para ver de un vistazo
// los entornos, qué migraciones están aplicadas o pendientes, el SQL de cada una,
// su riesgo y el drift. Las acciones (up, down, sql, commit…) no se reimplementan:
// el panel se suspende, corre el comando real de la CLI en la terminal, con sus
// confirmaciones y su salida, y al volver recarga.

import (
	"bufio"
	"context"
	"fmt"
	"github.com/zara-te05/Marga/internal/vistas/terminal"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/datos/ramabd"
	"github.com/zara-te05/Marga/internal/modelos/riesgo"
)

// envPausaUI: cuando `marga ui` lanza un comando, este espera un Enter al terminar
// para que se pueda leer la salida antes de volver al panel.
const envPausaUI = "MARGA_UI_PAUSA"

func pausaUI() {
	fmt.Fprint(os.Stderr, "\n"+sTenue.Render("Enter para volver a marga ui… "))
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

var uiTema string

func init() {
	uiCmd.Flags().StringVar(&uiTema, "tema", "", "tema de colores: marga, noche, bosque, atardecer, papel o mono")
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Panel interactivo: entornos, migraciones, SQL, riesgo y drift",
	Long: `Abre un panel en la terminal con los entornos del proyecto, las migraciones
aplicadas y pendientes de cada uno, el SQL (UP y DOWN) de la que elijas, su
análisis de riesgo y el drift.

Las acciones corren el comando real de marga en la terminal (con sus mismas
confirmaciones) y al terminar vuelves al panel:

  u  marga up        d  marga down       s  marga sql        e  abrir en tu editor
  c  marga commit    n  marga new        a  aceptar el drift r  recargar
  Tab / ← →  cambiar de panel   ↑ ↓  moverse   1-4  UP · DOWN · Riesgo · Drift
  t  cambiar de tema   ?  ayuda   q  salir

Temas: marga, noche, bosque, atardecer, papel (para fondo claro) y mono. Se elige con
--tema, con la variable MARGA_TEMA o con t (se guarda en ~/.marga/tema.json, donde
también puedes cambiar cada color).`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		t, err := terminal.Cargar(uiTema)
		if err != nil {
			return err
		}
		m, err := NuevoModeloUI(p, t)
		if err != nil {
			return err
		}
		_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
		return err
	},
}

// ---------------------------------------------------------------- datos

type uiDatos struct {
	donde     string // base y rama
	filas     []migrador.Fila
	drift     []string
	nuevaBase bool
	faltaBase string // la base de esta rama todavía no existe
	err       error
	cuando    time.Time
}

type msgCargado struct {
	nombre string
	datos  *uiDatos
}

type msgTermino struct {
	que string
	err error
}

// entornoSinCrear resuelve la base de la rama como resolverRama, pero sin crear
// nada: el panel solo mira. Si falta la base de la rama lo dice en `falta`.
func entornoSinCrear(p *proyecto.Proyecto, nombre string, e proyecto.Entorno) (proyecto.Entorno, string, error) {
	if !sigueRamas(nombre, e) {
		return e, "", nil
	}
	repo := gitDe(p)
	if repo == nil {
		return e, "", nil
	}
	rama, separado, err := repo.RamaActual()
	if err != nil || separado {
		return e, "", nil
	}
	local, err := p.Local()
	if err != nil {
		return e, "", err
	}
	principal, _ := ramaPrincipal(p, repo, local)
	urlP, err := e.URLReal()
	if err != nil {
		return e, "", err
	}
	baseP, err := ramabd.BaseDeURL(urlP)
	if err != nil {
		return e, "", err
	}
	if rama == principal {
		return e.ConRama(e.URL, rama, baseP, ""), "", nil
	}
	br := local.Base(nombre, rama)
	if br == nil {
		return e, fmt.Sprintf("La rama %s todavía no tiene su base en %s: u la crea (copiando %s) y aplica lo pendiente.", rama, nombre, principal), nil
	}
	u, err := ramabd.ConBase(urlP, br.Base)
	if err != nil {
		return e, "", err
	}
	return e.ConRama(u, rama, br.Base, claveRama(nombre, br.Base)), "", nil
}

func cargarEntorno(p *proyecto.Proyecto, locales []proyecto.Migracion, nombre string, e proyecto.Entorno) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d := &uiDatos{cuando: time.Now()}
		e, falta, err := entornoSinCrear(p, nombre, e)
		if err != nil {
			d.err = err
			return msgCargado{nombre, d}
		}
		d.donde = e.Base
		if d.donde == "" {
			if u, err := e.URLReal(); err == nil {
				d.donde, _ = ramabd.BaseDeURL(u)
			}
		}
		if e.RamaGit != "" {
			d.donde += " · rama " + e.RamaGit
		}
		if falta != "" {
			d.faltaBase = falta
			d.filas = migrador.Comparar(locales, nil)
			return msgCargado{nombre, d}
		}
		m, err := migrador.Abrir(ctx, e)
		if err != nil {
			d.err = err
			return msgCargado{nombre, d}
		}
		defer m.Close()
		aplicadas, err := m.Historial(ctx)
		if err != nil {
			d.err = err
			return msgCargado{nombre, d}
		}
		d.filas = migrador.Comparar(locales, aplicadas)
		d.drift, _, d.nuevaBase, err = m.Drift(ctx)
		if err != nil {
			d.drift = []string{"no se pudo revisar: " + err.Error()}
		}
		return msgCargado{nombre, d}
	}
}

// ---------------------------------------------------------------- modelo

const (
	FocoEntornos = iota
	focoMigraciones
	focoDetalle
)

var pestañasUI = []string{"UP", "DOWN", "Riesgo", "Drift"}

type preguntaUI struct {
	Texto   string
	texto2  bool     // pide un texto (descripción) en lugar de s/n
	comando []string // argumentos de marga; con texto2 se agrega lo escrito
}

type modeloUI struct {
	p        *proyecto.Proyecto
	locales  []proyecto.Migracion
	errLocal error
	Nombres  []string
	ents     map[string]proyecto.Entorno
	datos    map[string]*uiDatos
	cargando map[string]bool
	riesgos  map[string][]riesgo.Hallazgo

	iEnt, iMig, desde int
	Foco, pestaña     int
	vp                viewport.Model
	ancho, alto       int
	aviso, avisoTipo  string // avisoTipo: ok, error, aviso, info
	filtro            string // texto de la búsqueda (/)
	buscando          bool   // escribiendo el filtro
	busqueda          textinput.Model
	Pregunta          *preguntaUI
	entrada           textinput.Model

	Tema  terminal.Tema
	est   estilosUI
	sql   terminal.EstilosSQL
	spin  spinner.Model
	ayuda bool // mostrando la ayuda (?)
}

func (m *modeloUI) avisar(tipo, texto string) { m.avisoTipo, m.aviso = tipo, texto }

// ponerTema cambia los colores de todo el panel.
func (m *modeloUI) ponerTema(t terminal.Tema) {
	m.Tema, m.est, m.sql = t, nuevosEstilosUI(t), terminal.NuevosEstilosSQL(t)
	m.spin.Style = lipgloss.NewStyle().Foreground(terminal.Color(t.Acento))
	m.entrada.PromptStyle = lipgloss.NewStyle().Foreground(terminal.Color(t.Acento)).Bold(true)
	m.entrada.TextStyle = lipgloss.NewStyle().Foreground(terminal.Color(t.Texto))
	m.busqueda.PromptStyle = lipgloss.NewStyle().Foreground(terminal.Color(t.Acento)).Bold(true)
	m.busqueda.TextStyle = lipgloss.NewStyle().Foreground(terminal.Color(t.Texto))
	m.busqueda.PlaceholderStyle = lipgloss.NewStyle().Foreground(terminal.Color(t.Tenue))
	m.actualizarDetalle()
}

func NuevoModeloUI(p *proyecto.Proyecto, t terminal.Tema) (*modeloUI, error) {
	ents, err := p.Entornos()
	if err != nil {
		return nil, err
	}
	m := &modeloUI{p: p, ents: ents, datos: map[string]*uiDatos{}, cargando: map[string]bool{}, riesgos: map[string][]riesgo.Hallazgo{}}
	for n := range ents {
		m.Nombres = append(m.Nombres, n)
	}
	// Primero los personales (dev, local), luego el resto; producción al final.
	sort.Slice(m.Nombres, func(i, j int) bool {
		a, b := m.Nombres[i], m.Nombres[j]
		ra, rb := rangoEntorno(a, ents[a]), rangoEntorno(b, ents[b])
		if ra != rb {
			return ra < rb
		}
		return a < b
	})
	m.recargarLocales()
	m.entrada = textinput.New()
	m.entrada.Prompt = "› "
	m.entrada.CharLimit = 120
	m.vp = viewport.New(40, 10)
	m.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m.busqueda = textinput.New()
	m.busqueda.Prompt = "/"
	m.busqueda.Placeholder = "versión, nombre o texto del SQL"
	m.busqueda.CharLimit = 80
	m.ponerTema(t)
	return m, nil
}

func rangoEntorno(n string, e proyecto.Entorno) int {
	switch {
	case !e.Compartido:
		return 0
	case esProduccion(n):
		return 2
	}
	return 1
}

func (m *modeloUI) recargarLocales() {
	m.locales, m.errLocal = m.p.Migraciones()
	m.riesgos = map[string][]riesgo.Hallazgo{}
	for _, l := range m.locales {
		m.riesgos[l.ID()] = riesgo.Analizar(l.Up, l.Down)
	}
}

func (m *modeloUI) Entorno() string {
	if len(m.Nombres) == 0 {
		return ""
	}
	return m.Nombres[m.iEnt]
}

func (m *modeloUI) Cargar(nombre string) tea.Cmd {
	if nombre == "" || m.cargando[nombre] {
		return nil
	}
	m.cargando[nombre] = true
	return cargarEntorno(m.p, m.locales, nombre, m.ents[nombre])
}

// filas: las del entorno elegido; sin datos todavía, las locales sin estado.
// filas: las migraciones que se ven en la lista (con el filtro de / aplicado).
func (m *modeloUI) filas() []migrador.Fila {
	todas := m.filasTodas()
	if m.filtro == "" {
		return todas
	}
	var out []migrador.Fila
	for _, f := range todas {
		if coincide(f, m.filtro) {
			out = append(out, f)
		}
	}
	return out
}

// coincide: el filtro aparece (sin importar mayúsculas) en la versión, el nombre o el SQL.
func coincide(f migrador.Fila, filtro string) bool {
	q := strings.ToLower(filtro)
	if strings.Contains(strings.ToLower(f.ID()), q) || strings.Contains(strings.ToLower(strings.ReplaceAll(f.ID(), "_", " ")), q) {
		return true
	}
	return f.Local != nil && (strings.Contains(strings.ToLower(f.Local.Up), q) || strings.Contains(strings.ToLower(f.Local.Down), q))
}

// filasTodas: las del entorno elegido; sin datos todavía, las locales sin estado.
func (m *modeloUI) filasTodas() []migrador.Fila {
	if d := m.datos[m.Entorno()]; d != nil && d.filas != nil {
		return d.filas
	}
	f := make([]migrador.Fila, len(m.locales))
	for i := range m.locales {
		f[i] = migrador.Fila{Version: m.locales[i].Version, Nombre: m.locales[i].Nombre, Local: &m.locales[i], Situacion: "?"}
	}
	return f
}

func (m *modeloUI) filaActual() *migrador.Fila {
	fs := m.filas()
	if len(fs) == 0 {
		return nil
	}
	m.iMig = min(max(m.iMig, 0), len(fs)-1)
	return &fs[m.iMig]
}

func (m *modeloUI) Init() tea.Cmd {
	// Arranca en la primera pendiente (o la última aplicada) cuando llegan los datos.
	return tea.Batch(m.Cargar(m.Entorno()), m.spin.Tick)
}

// ---------------------------------------------------------------- acciones

// correr suspende el panel y ejecuta `marga args…` en la terminal.
func (m *modeloUI) correr(que string, args ...string) tea.Cmd {
	exe, err := os.Executable()
	if err != nil {
		m.avisar("error", err.Error())
		return nil
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), envPausaUI+"=1")
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return msgTermino{que, err} })
}

func (m *modeloUI) editar() tea.Cmd {
	f := m.filaActual()
	if f == nil || f.Local == nil {
		m.avisar("aviso", "Esa migración no tiene archivo local.")
		return nil
	}
	sufijo := ".up.sql"
	if m.pestaña == 1 {
		sufijo = ".down.sql"
	}
	cmd, err := comandoEditor(filepath.Join(m.p.DirMigraciones(), f.Local.ID()+sufijo))
	if err != nil {
		m.avisar("error", err.Error())
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return msgTermino{"editar " + f.Local.ID() + sufijo, err} })
}

func (m *modeloUI) pendientes() int {
	n := 0
	for _, f := range m.filasTodas() {
		if f.Situacion == migrador.Pendiente {
			n++
		}
	}
	return n
}

func (m *modeloUI) ultimaAplicada() string {
	fs := m.filasTodas()
	for i := len(fs) - 1; i >= 0; i-- {
		if fs[i].Situacion == migrador.Aplicada_ || fs[i].Situacion == migrador.Modificada {
			return fs[i].ID()
		}
	}
	return ""
}

// ConSi agrega --si en producción: la confirmación ya se hizo en el panel.
func ConSi(nombre string, args ...string) []string {
	if esProduccion(nombre) {
		args = append(args, "--si")
	}
	return args
}

func (m *modeloUI) tecla(k string) tea.Cmd {
	nombre := m.Entorno()
	switch k {
	case "u":
		if nombre == "" {
			return nil
		}
		n := m.pendientes()
		d := m.datos[nombre]
		if n == 0 && (d == nil || d.faltaBase == "") {
			m.avisar("info", "No hay nada pendiente en "+nombre+".")
			return nil
		}
		m.Pregunta = &preguntaUI{Texto: fmt.Sprintf("¿Aplicar %s en %s?", plural(n, "migración pendiente", "migraciones pendientes"), nombre),
			comando: ConSi(nombre, "up", "--env", nombre)}
	case "d":
		ult := m.ultimaAplicada()
		if ult == "" {
			m.avisar("info", "No hay nada que revertir en "+nombre+".")
			return nil
		}
		m.Pregunta = &preguntaUI{Texto: fmt.Sprintf("¿Revertir %s en %s?", ult, nombre), comando: ConSi(nombre, "down", "--env", nombre)}
	case "a":
		if d := m.datos[nombre]; d == nil || len(d.drift) == 0 {
			m.avisar("info", "No hay drift en "+nombre+".")
			return nil
		}
		m.Pregunta = &preguntaUI{Texto: fmt.Sprintf("¿Aceptar los cambios hechos fuera de Marga en %s como la nueva línea base?", nombre),
			comando: []string{"drift", "--aceptar", "--env", nombre}}
	case "c", "n":
		cmd, txt := "commit", "Describe los cambios que hiciste en la base (marga commit):"
		if k == "n" {
			cmd, txt = "new", "Describe la migración nueva (marga new):"
		}
		args := []string{cmd}
		if k == "c" {
			args = append(args, "--env", nombre)
		}
		m.Pregunta = &preguntaUI{Texto: txt, texto2: true, comando: args}
		m.entrada.SetValue("")
		return m.entrada.Focus()
	case "s":
		return m.correr("sql", "sql", "--env", nombre)
	case "e":
		return m.editar()
	case "r":
		m.recargarLocales()
		delete(m.datos, nombre)
		m.avisar("", "")
		return m.Cargar(nombre)
	}
	return nil
}

// ---------------------------------------------------------------- Update

func (m *modeloUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ancho, m.alto = msg.Width, msg.Height
		m.actualizarDetalle()
		return m, nil

	case msgCargado:
		m.cargando[msg.nombre] = false
		primera := m.datos[msg.nombre] == nil
		m.datos[msg.nombre] = msg.datos
		if msg.nombre == m.Entorno() && primera {
			m.irAPendiente()
		}
		m.actualizarDetalle()
		return m, nil

	case msgTermino:
		if msg.err != nil {
			m.avisar("error", msg.que+": "+msg.err.Error())
		} else {
			m.avisar("ok", msg.que+" terminó")
		}
		// Puede haber cambiado todo: migraciones locales, historial y drift de cualquier entorno.
		m.recargarLocales()
		m.datos, m.cargando = map[string]*uiDatos{}, map[string]bool{}
		return m, m.Cargar(m.Entorno())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if m.Pregunta != nil {
			return m, m.responder(msg)
		}
		if m.buscando {
			return m, m.escribirBusqueda(msg)
		}
		k := msg.String()
		if m.ayuda {
			if k == "ctrl+c" {
				return m, tea.Quit
			}
			m.ayuda = k == "t" // con t se puede probar terminal.Temas sin cerrar la ayuda
			if k != "t" {
				return m, nil
			}
		}
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.ayuda = true
		case "/":
			m.buscando = true
			m.Foco = focoMigraciones
			m.busqueda.SetValue(m.filtro)
			m.busqueda.CursorEnd()
			return m, m.busqueda.Focus()
		case "esc":
			if m.filtro != "" {
				m.filtro = ""
				m.iMig, m.desde = 0, 0
				m.irAPendiente()
				m.actualizarDetalle()
			}
		case "t":
			sig := terminal.Siguiente(m.Tema.Nombre)
			t, _ := terminal.Cargar(sig)
			m.ponerTema(t)
			if err := terminal.GuardarNombre(sig); err != nil {
				m.avisar("aviso", "Tema "+sig+" (no se pudo guardar: "+err.Error()+")")
			} else {
				m.avisar("ok", "Tema "+sig+" · "+t.Descripcion+" · guardado")
			}
		case "tab", "right", "l":
			m.Foco = (m.Foco + 1) % 3
		case "shift+tab", "left", "h":
			m.Foco = (m.Foco + 2) % 3
		case "1", "2", "3", "4":
			m.pestaña = int(k[0] - '1')
			m.actualizarDetalle()
		case "[":
			m.pestaña = (m.pestaña + 3) % 4
			m.actualizarDetalle()
		case "]":
			m.pestaña = (m.pestaña + 1) % 4
			m.actualizarDetalle()
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end", "g", "G":
			return m, m.mover(k, msg)
		default:
			m.avisar("", "")
			return m, m.tecla(k)
		}
	}
	return m, nil
}

func (m *modeloUI) responder(msg tea.KeyMsg) tea.Cmd {
	q := m.Pregunta
	switch msg.String() {
	case "esc", "ctrl+c":
		m.Pregunta = nil
		m.entrada.Blur()
		return nil
	}
	if q.texto2 {
		if msg.String() == "enter" {
			desc := strings.TrimSpace(m.entrada.Value())
			m.Pregunta = nil
			m.entrada.Blur()
			if desc == "" {
				return nil
			}
			args := append(append([]string{}, q.comando...), strings.Fields(desc)...)
			return m.correr(q.comando[0], args...)
		}
		var cmd tea.Cmd
		m.entrada, cmd = m.entrada.Update(msg)
		return cmd
	}
	switch strings.ToLower(msg.String()) {
	case "s", "y", "enter":
		m.Pregunta = nil
		return m.correr(strings.Join(q.comando[:1], ""), q.comando...)
	case "n":
		m.Pregunta = nil
	}
	return nil
}

func (m *modeloUI) mover(k string, msg tea.KeyMsg) tea.Cmd {
	paso := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10}[k]
	switch m.Foco {
	case FocoEntornos:
		if len(m.Nombres) == 0 {
			return nil
		}
		ant := m.iEnt
		switch k {
		case "home", "g":
			m.iEnt = 0
		case "end", "G":
			m.iEnt = len(m.Nombres) - 1
		default:
			m.iEnt = min(max(m.iEnt+paso, 0), len(m.Nombres)-1)
		}
		if ant != m.iEnt {
			m.iMig, m.desde = 0, 0
			if m.datos[m.Entorno()] != nil {
				m.irAPendiente()
			}
			m.actualizarDetalle()
			return m.Cargar(m.Entorno())
		}
	case focoMigraciones:
		n := len(m.filas())
		switch k {
		case "home", "g":
			m.iMig = 0
		case "end", "G":
			m.iMig = n - 1
		default:
			m.iMig += paso
		}
		m.iMig = min(max(m.iMig, 0), max(n-1, 0))
		m.actualizarDetalle()
	case focoDetalle:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return cmd
	}
	return nil
}

// irAPendiente coloca la selección en la primera migración pendiente (o la última).
func (m *modeloUI) irAPendiente() {
	fs := m.filas()
	m.iMig = max(len(fs)-1, 0)
	for i, f := range fs {
		if f.Situacion == migrador.Pendiente {
			m.iMig = i
			break
		}
	}
}

// ---------------------------------------------------------------- View

// escribirBusqueda: mientras se escribe tras /, la lista se filtra al momento.
// Enter deja el filtro puesto; Esc lo quita.
func (m *modeloUI) escribirBusqueda(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.buscando, m.filtro = false, ""
		m.busqueda.Blur()
		m.iMig, m.desde = 0, 0
		m.irAPendiente()
		m.actualizarDetalle()
		return nil
	case "enter":
		m.buscando = false
		m.busqueda.Blur()
		return nil
	case "up", "down":
		return m.mover(msg.String(), msg)
	}
	var cmd tea.Cmd
	m.busqueda, cmd = m.busqueda.Update(msg)
	if f := strings.TrimSpace(m.busqueda.Value()); f != m.filtro {
		m.filtro = f
		m.iMig, m.desde = 0, 0
		m.actualizarDetalle()
	}
	return cmd
}
