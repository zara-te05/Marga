package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zara-te05/Marga/internal/introspeccion"
	"github.com/zara-te05/Marga/internal/migrador"
	"github.com/zara-te05/Marga/internal/proyecto"
	"github.com/zara-te05/Marga/internal/remoto"
)

// Interactivo es true cuando el comando corre dentro de la TUI (no hay stdin libre).
var Interactivo bool

var (
	sAcento = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true)
	sTenue  = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))
	sAviso  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
	sError  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	sNegri  = lipgloss.NewStyle().Bold(true)
)

func out(c *cobra.Command) io.Writer { return c.OutOrStdout() }

func linea(c *cobra.Command, format string, a ...any) { fmt.Fprintf(out(c), format+"\n", a...) }

func proyectoActual() (*proyecto.Proyecto, error) { return proyecto.Buscar(".") }

// ResetFlags devuelve todos los flags a su valor por defecto. Necesario en la TUI,
// donde el mismo árbol de comandos de Cobra se ejecuta muchas veces.
func ResetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		ResetFlags(sub)
	}
}

// autor identifica quién aplica una migración (se guarda en marga_history).
func autor(p *proyecto.Proyecto) string {
	if p != nil && p.Config.Servidor != "" {
		if m, err := remoto.LeerCredenciales(); err == nil {
			if c, ok := m[remoto.NormalizarServidor(p.Config.Servidor)]; ok && c.Email != "" {
				return c.Email
			}
		}
	}
	nombre := "desconocido"
	if u, err := user.Current(); err == nil {
		nombre = u.Username
	}
	host, _ := os.Hostname()
	return nombre + "@" + host
}

var reProd = regexp.MustCompile(`(?i)^(prod|produccion|producción|production)`)

// esProduccion: los entornos con nombre de producción piden --si para cambios.
func esProduccion(nombre string) bool { return reProd.MatchString(nombre) }

func hace(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "hace un momento"
	case d < time.Hour:
		return fmt.Sprintf("hace %dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("hace %dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("hace %d días", int(d.Hours()/24))
	}
	return t.Local().Format("2006-01-02")
}

func relleno(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func driverDe(motor string) (string, error) {
	switch motor {
	case "postgresql", "":
		return "postgres", nil
	case "mysql":
		return "mysql", nil
	case "sqlserver":
		return "sqlserver", nil
	}
	return "", fmt.Errorf("motor no soportado: %s (usa postgresql, mysql o sqlserver)", motor)
}

// plural: plural(1, "migración", "migraciones") -> "1 migración".
func plural(n int, uno, varios string) string {
	if n == 1 {
		return "1 " + uno
	}
	return fmt.Sprintf("%d %s", n, varios)
}

// ---------------------------------------------------------------- Reporte al servidor

// nombreReporte: los entornos personales se reportan como "dev@usuario" para que
// la base local de cada quien no se mezcle con la de sus compañeros.
func nombreReporte(nombre string, e proyecto.Entorno, cred remoto.Credencial) string {
	if e.Compartido {
		return nombre
	}
	u := cred.Nombre
	if u == "" {
		u, _, _ = strings.Cut(cred.Email, "@")
	}
	u = regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(strings.ToLower(u), "-")
	if u == "" {
		return nombre
	}
	return nombre + "@" + u
}

// reportar envía al servidor qué tiene aplicado el entorno, su esquema real y su drift.
// Es opcional: si el proyecto no está vinculado o no hay sesión, no hace nada.
func reportar(c *cobra.Command, p *proyecto.Proyecto, nombre string, e proyecto.Entorno, m *migrador.Migrador,
	real *introspeccion.Esquema, drift []string) {
	if p == nil || p.Config.Servidor == "" || p.Config.RepoID == 0 {
		return
	}
	cli, cred, err := remoto.Conectado(p.Config.Servidor)
	if err != nil {
		return
	}
	ctx := context.Background()
	hist, err := m.Historial(ctx)
	if err != nil {
		linea(c, "%s No se pudo leer el historial para reportarlo: %v", sAviso.Render("!"), err)
		return
	}
	if real == nil {
		if r, err := m.EsquemaReal(ctx); err == nil {
			real = &r
		}
	}
	if drift == nil {
		drift = []string{}
	}
	mapa, _ := p.Remoto()
	aplicadas := make([]remoto.MigracionAplicada, 0, len(hist))
	for _, a := range hist {
		var commit *string
		if h, ok := mapa[a.Version+"_"+a.Nombre]; ok {
			commit = &h
		}
		aplicadas = append(aplicadas, remoto.MigracionAplicada{
			Version: a.Version, Nombre: a.Nombre, Checksum: a.Checksum, Commit: commit,
			AplicadaEn: a.AplicadaEn, DuracionMs: a.DuracionMs, AplicadaPor: a.AplicadaPor,
		})
	}
	nombreRep := nombreReporte(nombre, e, cred)
	err = cli.ReportarEntorno(p.Config.RepoID, nombreRep, remoto.ReporteEntorno{
		Motor: e.Motor, Compartido: e.Compartido, Rama: e.RamaGit, Aplicadas: aplicadas, Esquema: real, Drift: drift,
	})
	if err != nil {
		linea(c, "%s No se pudo enviar el estado al servidor: %v", sAviso.Render("!"), err)
		return
	}
	linea(c, "%s", sTenue.Render("↗ estado de "+nombreRep+" enviado al servidor"))
}

// mostrarDrift imprime los cambios hechos fuera de Marga (máximo `limite`; 0 = todos).
func mostrarDrift(c *cobra.Command, nombre string, cambios []string, limite int) {
	linea(c, "%s %s en %s:", sAviso.Render("!"), plural(len(cambios), "cambio hecho fuera de Marga", "cambios hechos fuera de Marga"), sNegri.Render(nombre))
	for i, ch := range cambios {
		if limite > 0 && i == limite {
			linea(c, "    …y %d más (marga drift --env %s)", len(cambios)-limite, nombre)
			break
		}
		linea(c, "    %s", ch)
	}
}
