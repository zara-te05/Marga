package cmd

import (
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
	"github.com/zarat/Motor-Marga/internal/proyecto"
	"github.com/zarat/Motor-Marga/internal/remoto"
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
