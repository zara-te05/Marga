package cli

// Cómo se ve `marga ui`. Todos los colores salen del tema (tema.go).

import (
	"fmt"
	"github.com/zara-te05/Marga/internal/vistas/terminal"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"github.com/zara-te05/Marga/internal/modelos/riesgo"
)

type estilosUI struct {
	Texto, tenue, acento, ok, aviso, err, negrita lipgloss.Style
	marca                                         lipgloss.Style // ◆ MARGA
	chip                                          lipgloss.Style // dev, rama…
	sel                                           lipgloss.Style // fondo de la fila elegida
	barra                                         lipgloss.Style // ▌ de la fila elegida
	tabOn, tabOff                                 lipgloss.Style
	tecla                                         lipgloss.Style
	gutter                                        lipgloss.Style // números de línea
	borde, bordeFoco                              lipgloss.Color
	nivel                                         map[string]lipgloss.Style // ALTO / MEDIO / BAJO como píldora
}

func nuevosEstilosUI(t terminal.Tema) estilosUI {
	ns := lipgloss.NewStyle
	return estilosUI{
		Texto:     ns().Foreground(terminal.Color(t.Texto)),
		tenue:     ns().Foreground(terminal.Color(t.Tenue)),
		acento:    ns().Foreground(terminal.Color(t.Acento)).Bold(true),
		ok:        ns().Foreground(terminal.Color(t.Ok)),
		aviso:     ns().Foreground(terminal.Color(t.Aviso)),
		err:       ns().Foreground(terminal.Color(t.Error)),
		negrita:   ns().Foreground(terminal.Color(t.Texto)).Bold(true),
		marca:     ns().Background(terminal.Color(t.Acento)).Foreground(terminal.Color(t.SobreAcen)).Bold(true).Padding(0, 1),
		chip:      ns().Background(terminal.Color(t.Seleccion)).Foreground(terminal.Color(t.Texto)).Padding(0, 1),
		sel:       ns().Background(terminal.Color(t.Seleccion)),
		barra:     ns().Foreground(terminal.Color(t.Acento)).Background(terminal.Color(t.Seleccion)).Bold(true),
		tabOn:     ns().Background(terminal.Color(t.Acento)).Foreground(terminal.Color(t.SobreAcen)).Bold(true).Padding(0, 1),
		tabOff:    ns().Foreground(terminal.Color(t.Tenue)).Padding(0, 1),
		tecla:     ns().Background(terminal.Color(t.Seleccion)).Foreground(terminal.Color(t.Acento)).Bold(true).Padding(0, 1),
		gutter:    ns().Foreground(terminal.Color(t.Borde)),
		borde:     terminal.Color(t.Borde),
		bordeFoco: terminal.Color(t.Acento),
		nivel: map[string]lipgloss.Style{
			"alto":  ns().Background(terminal.Color(t.Error)).Foreground(terminal.Color(t.SobreAcen)).Bold(true).Padding(0, 1),
			"medio": ns().Background(terminal.Color(t.Aviso)).Foreground(terminal.Color(t.SobreAcen)).Bold(true).Padding(0, 1),
			"bajo":  ns().Background(terminal.Color(t.Borde)).Foreground(terminal.Color(t.Texto)).Padding(0, 1),
		},
	}
}

// ---------------------------------------------------------------- piezas

// medidas del diseño: columna izquierda, derecha y alturas.
func (m *modeloUI) medidas() (izq, der, altoEnt, altoMig, altoDet int) {
	izq = min(max(m.ancho*2/5, 36), 62)
	der = max(m.ancho-izq-1, 24) // 1 columna de aire entre los paneles
	cuerpo := max(m.alto-3, 8)   // cabecera, aviso, pie
	altoEnt = min(max(len(m.Nombres), 1), 6) + 2
	altoMig = max(cuerpo-altoEnt, 4)
	altoDet = cuerpo
	return
}

// caja dibuja un panel con borde redondeado, título a la izquierda e
// información a la derecha sobre el borde superior: ╭─ Título ──── 3/7 ─╮
func (m *modeloUI) caja(titulo, info, cuerpo string, ancho, alto int, foco bool) string {
	borde := m.est.borde
	if foco {
		borde = m.est.bordeFoco
	}
	b := lipgloss.NewStyle().Foreground(borde)
	interior := max(ancho-4, 1)
	lineas := strings.Split(cuerpo, "\n")
	if len(lineas) > alto-2 {
		lineas = lineas[:max(alto-2, 0)]
	}
	for len(lineas) < alto-2 {
		lineas = append(lineas, "")
	}
	var out strings.Builder

	// Borde de arriba con el título
	tit := ""
	if titulo != "" {
		est := m.est.tenue.Bold(true)
		if foco {
			est = m.est.acento
		}
		tit = " " + est.Render(ansi.Truncate(titulo, max(interior-lipgloss.Width(info)-4, 4), "…")) + " "
	}
	inf := ""
	if info != "" {
		inf = " " + m.est.tenue.Render(info) + " "
	}
	resto := max(ancho-2-1-lipgloss.Width(tit)-lipgloss.Width(inf)-1, 0)
	out.WriteString(b.Render("╭─") + tit + b.Render(strings.Repeat("─", resto)) + inf + b.Render("─╮") + "\n")

	for _, l := range lineas {
		l = ansi.Truncate(l, interior, "…")
		out.WriteString(b.Render("│ ") + l + strings.Repeat(" ", max(interior-lipgloss.Width(l), 0)) + b.Render(" │") + "\n")
	}
	out.WriteString(b.Render("╰" + strings.Repeat("─", max(ancho-2, 0)) + "╯"))
	return out.String()
}

// fila: una línea de lista que ocupa todo el ancho; elegida, con barra y fondo.
func (m *modeloUI) filaLista(izq, der string, ancho int, elegida bool) string {
	hueco := max(ancho-2-lipgloss.Width(izq)-lipgloss.Width(der), 1)
	if !elegida {
		return "  " + izq + strings.Repeat(" ", hueco) + der
	}
	// El fondo tiene que cubrir también lo que ya viene con color.
	cuerpo := izq + strings.Repeat(" ", hueco) + der
	cuerpo = strings.ReplaceAll(cuerpo, "\x1b[0m", "\x1b[0m"+fondoANSI(m.Tema.Seleccion))
	return m.est.barra.Render("▌") + m.est.sel.Render(" "+cuerpo)
}

// fondoANSI: la secuencia que vuelve a poner el fondo tras un reinicio de color.
func fondoANSI(hex string) string {
	s := lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("x")
	if i := strings.Index(s, "x"); i > 0 {
		return s[:i]
	}
	return ""
}

func (m *modeloUI) chip(k, que string) string {
	return m.est.tecla.Render(k) + " " + m.est.tenue.Render(que)
}

// nombreBonito: 0002_vistas_y_funciones → "0002", "vistas y funciones".
func nombreBonito(id string) (string, string) {
	v, n, ok := strings.Cut(id, "_")
	if !ok {
		return id, ""
	}
	return v, strings.ReplaceAll(n, "_", " ")
}

// ---------------------------------------------------------------- View

func (m *modeloUI) View() string {
	if m.ancho == 0 {
		return ""
	}
	izq, der, altoEnt, altoMig, altoDet := m.medidas()

	cuerpo := ""
	if m.ayuda {
		cuerpo = lipgloss.Place(m.ancho, altoDet, lipgloss.Center, lipgloss.Center, m.vistaAyuda())
	} else {
		ents := m.caja("Entornos", fmt.Sprintf("%d", len(m.Nombres)), m.vistaEntornos(izq-4), izq, altoEnt, m.Foco == FocoEntornos)
		migs, infoMig := m.vistaMigraciones(izq-4, altoMig-2)
		titMig := "Migraciones"
		if n := m.Entorno(); n != "" {
			titMig += " · " + n
		}
		cajaMig := m.caja(titMig, infoMig, migs, izq, altoMig, m.Foco == focoMigraciones)
		titDet, infoDet := m.tituloDetalle()
		det := m.caja(titDet, infoDet, m.vistaDetalle(der-4), der, altoDet, m.Foco == focoDetalle)
		cuerpo = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.JoinVertical(lipgloss.Left, ents, cajaMig), " ", det)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.vistaCabecera(), cuerpo, m.vistaAviso(), m.vistaPie())
}

func (m *modeloUI) vistaCabecera() string {
	izq := m.est.marca.Render("◆ MARGA") + " " + m.est.negrita.Render(filepath.Base(m.p.Raiz))
	if n := m.Entorno(); n != "" {
		izq += m.est.tenue.Render("  ›  ") + m.est.chip.Render(n)
		if d := m.datos[n]; d != nil && d.donde != "" {
			izq += " " + m.est.tenue.Render(d.donde)
		}
	}
	der := m.est.tenue.Render("tema ") + m.est.acento.Render(m.Tema.Nombre) + m.est.tenue.Render("  ·  ") + m.chip("?", "ayuda")
	hueco := m.ancho - lipgloss.Width(izq) - lipgloss.Width(der)
	if hueco < 1 {
		return ansi.Truncate(izq, m.ancho, "…")
	}
	return izq + strings.Repeat(" ", hueco) + der
}

func (m *modeloUI) vistaEntornos(ancho int) string {
	if len(m.Nombres) == 0 {
		return m.est.tenue.Render("Sin entornos: marga env add dev \"postgres://…\"")
	}
	var ls []string
	for i, n := range m.Nombres {
		punto, estado := m.estadoEntorno(n)
		tipo := m.est.tenue.Render("personal")
		switch {
		case esProduccion(n):
			tipo = m.est.err.Render("producción")
		case m.ents[n].Compartido:
			tipo = m.est.tenue.Render("compartido")
		}
		nombre := m.est.Texto.Render(n)
		if i == m.iEnt {
			nombre = m.est.negrita.Render(n)
		}
		izq := punto + " " + nombre + strings.Repeat(" ", max(12-lipgloss.Width(n), 1)) + tipo
		ls = append(ls, m.filaLista(izq, estado, ancho, i == m.iEnt))
	}
	return strings.Join(ls, "\n")
}

// estadoEntorno: el punto de color y el texto de un entorno.
func (m *modeloUI) estadoEntorno(n string) (string, string) {
	d := m.datos[n]
	switch {
	case m.cargando[n]:
		return m.spin.View(), m.est.tenue.Render("cargando")
	case d == nil:
		return m.est.tenue.Render("○"), m.est.tenue.Render("—")
	case d.err != nil:
		return m.est.err.Render("●"), m.est.err.Render("sin conexión")
	case d.faltaBase != "":
		return m.est.aviso.Render("●"), m.est.aviso.Render("sin base de rama")
	}
	pend, probl := 0, 0
	for _, f := range d.filas {
		switch f.Situacion {
		case migrador.Pendiente:
			pend++
		case migrador.Modificada, migrador.Huerfana:
			probl++
		}
	}
	var partes []string
	if pend > 0 {
		partes = append(partes, m.est.aviso.Render(plural(pend, "pendiente", "pendientes")))
	}
	if len(d.drift) > 0 {
		partes = append(partes, m.est.aviso.Render("drift"))
	}
	if probl > 0 {
		partes = append(partes, m.est.err.Render(fmt.Sprintf("%d con problemas", probl)))
	}
	switch {
	case probl > 0:
		return m.est.err.Render("●"), strings.Join(partes, m.est.tenue.Render(" · "))
	case len(partes) > 0:
		return m.est.aviso.Render("●"), strings.Join(partes, m.est.tenue.Render(" · "))
	}
	return m.est.ok.Render("●"), m.est.ok.Render("al día")
}

func (m *modeloUI) vistaMigraciones(ancho, visibles int) (string, string) {
	fs := m.filas()
	d := m.datos[m.Entorno()]
	var ls []string
	switch {
	case m.errLocal != nil:
		ls = append(ls, m.est.err.Render("✗ "+m.errLocal.Error()))
	case d != nil && d.err != nil:
		ls = append(ls, m.est.err.Render("✗ "+d.err.Error()), "")
	case d != nil && d.faltaBase != "":
		ls = append(ls, m.est.aviso.Render("! "+d.faltaBase), "")
	}
	if len(fs) == 0 && m.filtro != "" {
		ls = append(ls, "", m.est.tenue.Render("  Ninguna migración tiene «"+m.filtro+"»."), "  "+m.chip("esc", "quitar el filtro"))
		return strings.Join(ls, "\n"), "0 de " + fmt.Sprint(len(m.filasTodas()))
	}
	if len(fs) == 0 && m.errLocal == nil {
		ls = append(ls, "", m.est.tenue.Render("  Todavía no hay migraciones."),
			"  "+m.chip("c", "commit de tus cambios")+"   "+m.chip("n", "migración nueva"))
		return strings.Join(ls, "\n"), ""
	}
	visibles -= len(ls)
	if m.iMig < m.desde {
		m.desde = m.iMig
	}
	if m.iMig >= m.desde+visibles {
		m.desde = m.iMig - visibles + 1
	}
	for i := m.desde; i < len(fs) && i < m.desde+visibles; i++ {
		f := fs[i]
		icono, nota := m.iconoMigracion(f)
		v, nombre := nombreBonito(f.ID())
		r := " "
		if f.Local != nil {
			r = m.puntoRiesgo(m.riesgos[f.Local.ID()])
		}
		izq := icono + " " + m.est.tenue.Render(v) + " " + m.marcarFiltro(nombre, m.est.Texto)
		if i == m.iMig {
			izq = icono + " " + m.est.acento.Render(v) + " " + m.marcarFiltro(nombre, m.est.negrita)
		}
		ls = append(ls, m.filaLista(izq, nota+" "+r, ancho, i == m.iMig))
	}
	info := ""
	if len(fs) > 0 {
		info = fmt.Sprintf("%d/%d", m.iMig+1, len(fs))
	}
	if m.filtro != "" {
		info = fmt.Sprintf("%d de %d · /%s", len(fs), len(m.filasTodas()), m.filtro)
	}
	return strings.Join(ls, "\n"), info
}

// marcarFiltro subraya en el nombre lo que coincide con la búsqueda.
func (m *modeloUI) marcarFiltro(nombre string, base lipgloss.Style) string {
	if m.filtro == "" {
		return base.Render(nombre)
	}
	i := strings.Index(strings.ToLower(nombre), strings.ToLower(m.filtro))
	if i < 0 {
		return base.Render(nombre)
	}
	j := i + len(m.filtro)
	if j > len(nombre) {
		return base.Render(nombre)
	}
	marca := lipgloss.NewStyle().Foreground(terminal.Color(m.Tema.Acento)).Bold(true).Underline(true)
	return base.Render(nombre[:i]) + marca.Render(nombre[i:j]) + base.Render(nombre[j:])
}

func (m *modeloUI) iconoMigracion(f migrador.Fila) (string, string) {
	switch f.Situacion {
	case migrador.Aplicada_:
		if f.Registro == nil {
			return m.est.ok.Render("✓"), ""
		}
		return m.est.ok.Render("✓"), m.est.tenue.Render(hace(f.Registro.AplicadaEn))
	case migrador.Pendiente:
		n := "pendiente"
		if f.FueraDeOrden {
			n = "fuera de orden"
		}
		return m.est.aviso.Render("○"), m.est.aviso.Render(n)
	case migrador.Modificada:
		return m.est.aviso.Render("!"), m.est.aviso.Render("cambió")
	case migrador.Huerfana:
		return m.est.err.Render("?"), m.est.err.Render("sin archivo")
	case migrador.Renumerada:
		return m.est.acento.Render("↻"), m.est.tenue.Render("renumerada")
	}
	return m.est.tenue.Render("·"), ""
}

func nivelMaximo(hs []riesgo.Hallazgo) string {
	n := ""
	for _, h := range hs {
		switch h.Nivel {
		case "alto":
			return "alto"
		case "medio":
			n = "medio"
		case "bajo":
			if n == "" {
				n = "bajo"
			}
		}
	}
	return n
}

func (m *modeloUI) puntoRiesgo(hs []riesgo.Hallazgo) string {
	switch nivelMaximo(hs) {
	case "alto":
		return m.est.err.Render("◆")
	case "medio":
		return m.est.aviso.Render("◆")
	}
	return " "
}

func (m *modeloUI) tituloDetalle() (string, string) {
	f := m.filaActual()
	if f == nil {
		return "Detalle", ""
	}
	v, n := nombreBonito(f.ID())
	info := ""
	if total := m.vp.TotalLineCount(); m.pestaña <= 1 && total > m.vp.Height {
		desde := m.vp.YOffset + 1
		info = fmt.Sprintf("%d–%d de %d", desde, min(desde+m.vp.Height-1, total), total)
	}
	return v + " · " + n, info
}

func (m *modeloUI) vistaDetalle(ancho int) string {
	// Pestañas como píldoras, con insignias de riesgo y drift.
	d := m.datos[m.Entorno()]
	var tabs []string
	for i, t := range pestañasUI {
		etiqueta := fmt.Sprintf("%d %s", i+1, t)
		insignia := ""
		if f := m.filaActual(); i == 2 && f != nil && f.Local != nil {
			if hs := m.riesgos[f.Local.ID()]; len(hs) > 0 {
				est := m.est.tenue
				switch nivelMaximo(hs) {
				case "alto":
					est = m.est.err
				case "medio":
					est = m.est.aviso
				}
				insignia = est.Render(fmt.Sprintf(" %d", len(hs)))
			}
		}
		if i == 3 && d != nil && len(d.drift) > 0 {
			insignia = m.est.aviso.Render(fmt.Sprintf(" %d", len(d.drift)))
		}
		if i == m.pestaña {
			tabs = append(tabs, m.est.tabOn.Render(etiqueta)+insignia)
		} else {
			tabs = append(tabs, m.est.tabOff.Render(etiqueta)+insignia)
		}
	}
	cab := strings.Join(tabs, " ")
	meta := m.vistaMeta()
	sep := m.est.gutter.Render(strings.Repeat("─", max(ancho, 1)))
	return cab + "\n" + meta + "\n" + sep + "\n" + m.vp.View()
}

// vistaMeta: una línea con el estado de la migración elegida.
func (m *modeloUI) vistaMeta() string {
	if m.pestaña == 3 {
		return m.est.tenue.Render("Cambios hechos en la base fuera de Marga")
	}
	f := m.filaActual()
	switch {
	case f == nil:
		return ""
	case f.Registro != nil:
		r := f.Registro
		return m.est.ok.Render("● aplicada ") + m.est.tenue.Render(fmt.Sprintf("%s · %s · %d ms",
			r.AplicadaEn.Local().Format("02 Jan 2006 15:04"), r.AplicadaPor, r.DuracionMs))
	case f.Situacion == migrador.Pendiente:
		return m.est.aviso.Render("○ pendiente ") + m.est.tenue.Render("· u la aplica")
	}
	return ""
}

func (m *modeloUI) vistaAviso() string {
	if m.aviso == "" {
		return ""
	}
	var s string
	switch m.avisoTipo {
	case "ok":
		s = m.est.ok.Render("✓ ") + m.est.Texto.Render(m.aviso)
	case "error":
		s = m.est.err.Render("✗ ") + m.est.err.Render(m.aviso)
	case "aviso":
		s = m.est.aviso.Render("! ") + m.est.Texto.Render(m.aviso)
	default:
		s = m.est.tenue.Render(m.aviso)
	}
	return ansi.Truncate(" "+s, m.ancho, "…")
}

func (m *modeloUI) vistaPie() string {
	if q := m.Pregunta; q != nil {
		if q.texto2 {
			return " " + m.est.acento.Render(q.Texto) + " " + m.entrada.View() + "  " + m.chip("enter", "aceptar") + " " + m.chip("esc", "cancelar")
		}
		return " " + m.est.aviso.Render("?") + " " + m.est.negrita.Render(q.Texto) + "  " + m.chip("s", "sí") + " " + m.chip("n", "no")
	}
	if m.ayuda {
		return " " + m.chip("t", "otro tema") + "  " + m.chip("cualquier tecla", "cerrar")
	}
	if m.buscando {
		return " " + m.busqueda.View() + "   " + m.chip("enter", "dejar el filtro") + " " + m.chip("esc", "quitarlo")
	}
	partes := []string{m.chip("/", "buscar"), m.chip("u", "up"), m.chip("d", "down"), m.chip("s", "sql"), m.chip("e", "editar"),
		m.chip("c", "commit"), m.chip("n", "new"), m.chip("a", "drift"), m.chip("t", "tema"), m.chip("q", "salir")}
	out := " "
	for _, p := range partes {
		if lipgloss.Width(out)+lipgloss.Width(p)+2 > m.ancho {
			break
		}
		out += p + "  "
	}
	return out
}

func (m *modeloUI) vistaAyuda() string {
	t := func(k, que string) string {
		return m.est.tecla.Render(fmt.Sprintf("%-7s", k)) + " " + m.est.Texto.Render(que)
	}
	sec := func(s string) string { return m.est.acento.Render(s) }
	var muestras []string
	for _, n := range terminal.OrdenTemas {
		x := terminal.Temas[n]
		sw := ""
		for _, c := range []string{x.Acento, x.Clave, x.TipoDato, x.Cadena, x.Numero, x.Funcion} {
			sw += lipgloss.NewStyle().Foreground(terminal.Color(c)).Render("■")
		}
		nombre := m.est.Texto.Render(fmt.Sprintf("%-10s", n))
		if n == m.Tema.Nombre {
			nombre = m.est.acento.Render(fmt.Sprintf("%-10s", n))
		}
		muestras = append(muestras, nombre+" "+sw+"  "+m.est.tenue.Render(x.Descripcion))
	}
	ejemplo := terminal.ResaltarSQL("SELECT c.nombre, count(*) AS pedidos -- por cliente\n  FROM clientes c\n WHERE c.creado > '2026-01-01' AND (c.activo OR c.id = $1);", m.sql)

	izq := strings.Join([]string{
		sec("Moverse"),
		t("tab ← →", "cambiar de panel"), t("↑ ↓ j k", "moverse en la lista o el SQL"), t("1 2 3 4", "UP · DOWN · Riesgo · Drift"),
		t("/", "buscar migraciones (nombre o SQL)"), t("esc", "quitar la búsqueda"),
		"",
		sec("Acciones (corren marga en la terminal)"),
		t("u", "aplicar lo pendiente"), t("d", "revertir la última"), t("s", "consola de SQL"),
		t("e", "abrir en tu editor ($EDITOR)"), t("c", "commit de tus cambios"), t("n", "migración nueva"),
		t("a", "aceptar el drift"), t("r", "recargar"), t("q", "salir"),
	}, "\n")
	der := strings.Join([]string{
		sec("Temas") + m.est.tenue.Render("  (t cambia y lo guarda)"),
		strings.Join(muestras, "\n"),
		"",
		m.est.tenue.Render("Cada color se cambia en ") + m.est.Texto.Render("~/.marga/tema.json") + m.est.tenue.Render(":"),
		m.est.tenue.Render(`  { "tema": "noche", "colores": { "clave": "#FF79C6" } }`),
		"",
		sec("Así se ve el SQL"),
		ejemplo,
	}, "\n")
	contenido := lipgloss.JoinHorizontal(lipgloss.Top, izq, "     ", der)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.est.bordeFoco).Padding(1, 3).Render(contenido)
}

// ---------------------------------------------------------------- detalle

// actualizarDetalle vuelve a llenar el visor con la pestaña elegida.
func (m *modeloUI) actualizarDetalle() {
	_, der, _, _, altoDet := m.medidas()
	m.vp.Width, m.vp.Height = max(der-4, 10), max(altoDet-5, 3)
	m.vp.SetContent(m.contenidoDetalle(m.vp.Width))
	m.vp.GotoTop()
}

func (m *modeloUI) contenidoDetalle(ancho int) string {
	d := m.datos[m.Entorno()]
	if m.pestaña == 3 {
		switch {
		case d == nil:
			return m.spin.View() + m.est.tenue.Render(" cargando…")
		case d.err != nil:
			return ansi.Wrap(m.est.err.Render(d.err.Error()), ancho, " ")
		case d.nuevaBase:
			return m.est.tenue.Render("Primera vez que Marga ve esta base: su esquema actual quedó como línea base.")
		case len(d.drift) == 0:
			return m.est.ok.Render("✓ ") + m.est.Texto.Render("Sin drift: la base coincide con lo que dejó Marga.")
		}
		var b strings.Builder
		for _, c := range d.drift {
			b.WriteString(ansi.Wrap(m.colorearCambio(c), ancho, " ") + "\n")
		}
		b.WriteString("\n" + m.chip("c", "convertirlos en migración") + "  " + m.chip("a", "aceptarlos como están"))
		return b.String()
	}
	f := m.filaActual()
	if f == nil {
		return m.est.tenue.Render("Elige una migración.")
	}
	if f.Local == nil {
		return m.est.tenue.Render("Esta migración está aplicada en la base, pero su archivo no existe en el proyecto.")
	}
	switch m.pestaña {
	case 0:
		return m.conLineas(f.Local.Up, ancho)
	case 1:
		if strings.TrimSpace(f.Local.Down) == "" {
			return m.est.aviso.Render("Sin DOWN: esta migración no se puede revertir.")
		}
		return m.conLineas(f.Local.Down, ancho)
	}
	hs := m.riesgos[f.Local.ID()]
	if len(hs) == 0 {
		return m.est.ok.Render("✓ ") + m.est.Texto.Render("Sin riesgos detectados.")
	}
	var b strings.Builder
	for _, h := range hs {
		pill := m.est.nivel[h.Nivel]
		b.WriteString(pill.Render(strings.ToUpper(h.Nivel)) + " " + m.est.negrita.Render(h.Regla) + m.est.tenue.Render(fmt.Sprintf("  línea %d", h.Linea)) + "\n")
		b.WriteString(ansi.Wrap(m.est.Texto.Render(h.Mensaje), ancho, " ") + "\n")
		if h.Sugerencia != "" {
			b.WriteString(ansi.Wrap(m.est.acento.Render("→ ")+m.est.tenue.Render(h.Sugerencia), ancho, " ") + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// conLineas resalta el SQL y lo numera; las líneas largas se parten sin perder la numeración.
func (m *modeloUI) conLineas(sql string, ancho int) string {
	sql = strings.ReplaceAll(strings.TrimRight(sql, "\n"), "\t", "    ")
	lineas := strings.Split(terminal.ResaltarSQL(sql, m.sql), "\n")
	w := len(fmt.Sprint(len(lineas)))
	cuerpo := max(ancho-w-3, 10)
	var b strings.Builder
	for i, l := range lineas {
		partes := strings.Split(ansi.Hardwrap(l, cuerpo, true), "\n")
		for j, p := range partes {
			num := strings.Repeat(" ", w)
			if j == 0 {
				num = fmt.Sprintf("%*d", w, i+1)
			}
			b.WriteString(m.est.gutter.Render(num+" │") + " " + p + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *modeloUI) colorearCambio(c string) string {
	switch {
	case strings.HasPrefix(c, "+"):
		return m.est.ok.Render("+") + m.est.Texto.Render(c[1:])
	case strings.HasPrefix(c, "-"):
		return m.est.err.Render("−") + m.est.Texto.Render(c[1:])
	case strings.HasPrefix(c, "~"):
		return m.est.aviso.Render("~") + m.est.Texto.Render(c[1:])
	}
	return m.est.Texto.Render(c)
}
