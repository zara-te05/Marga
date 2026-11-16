package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/remoto"
)

var loginFlags struct {
	servidor string
	email    string
	password string
}

// servidorPorDefecto: el del proyecto actual si hay uno, si no localhost.
func servidorPorDefecto(flag string) string {
	if flag != "" {
		return remoto.NormalizarServidor(flag)
	}
	if v := os.Getenv("MARGA_SERVIDOR"); v != "" {
		return remoto.NormalizarServidor(v)
	}
	if p, err := proyectoActual(); err == nil && p.Config.Servidor != "" {
		return p.Config.Servidor
	}
	return "http://localhost:8080"
}

func esTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

var lector = bufio.NewReader(os.Stdin)

func preguntar(etiqueta string) (string, error) {
	fmt.Print(etiqueta)
	s, err := lector.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// ---- entrada de contraseña oculta (mini programa de Bubble Tea)

type modeloClave struct {
	input     textinput.Model
	cancelado bool
}

func (m modeloClave) Init() tea.Cmd { return textinput.Blink }

func (m modeloClave) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyEnter:
			return m, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelado = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m modeloClave) View() string { return m.input.View() + "\n" }

func pedirClave() (string, error) {
	if !esTerminal() { // CI o tubería: se lee una línea normal
		return preguntar("")
	}
	ti := textinput.New()
	ti.Prompt = "Contraseña: "
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Focus()
	res, err := tea.NewProgram(modeloClave{input: ti}).Run()
	if err != nil {
		return "", err
	}
	m := res.(modeloClave)
	if m.cancelado {
		return "", errors.New("cancelado")
	}
	return m.input.Value(), nil
}

// ---- comandos

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Inicia sesión en un servidor de Marga",
	Long: `Inicia sesión y guarda un token de acceso personal en ~/.marga/credenciales.json.
Tu contraseña no se guarda. Puedes revocar el token desde la web (Cuenta).`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		srv := servidorPorDefecto(loginFlags.servidor)
		email, clave := loginFlags.email, loginFlags.password
		var err error
		if email == "" {
			if email, err = preguntar("Email: "); err != nil {
				return err
			}
		}
		if clave == "" {
			if clave = os.Getenv("MARGA_PASSWORD"); clave == "" {
				if clave, err = pedirClave(); err != nil {
					return err
				}
			}
		}

		sesion, u, err := remoto.Nuevo(srv, "").Login(email, clave)
		if err != nil {
			return err
		}
		cli := remoto.Nuevo(srv, sesion)
		host, _ := os.Hostname()
		token, err := cli.CrearToken("CLI · " + host)
		_ = cli.Logout() // la sesión temporal ya no hace falta
		if err != nil {
			return fmt.Errorf("no se pudo crear el token de acceso: %w", err)
		}
		m, err := remoto.LeerCredenciales()
		if err != nil {
			return err
		}
		m[srv] = remoto.Credencial{Token: token, Email: u.Email, Nombre: u.Nombre}
		if err := remoto.GuardarCredenciales(m); err != nil {
			return err
		}
		linea(c, "%s Sesión iniciada como %s en %s", sAcento.Render("✓"), sNegri.Render(u.Nombre), srv)
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Cierra la sesión y revoca el token de esta máquina",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		srv := servidorPorDefecto(loginFlags.servidor)
		cli, _, err := remoto.Conectado(srv)
		if err != nil {
			return err
		}
		if err := cli.RevocarTokenActual(); err != nil {
			linea(c, "%s No se pudo revocar el token en el servidor (%v); se borra localmente", sAviso.Render("!"), err)
		}
		m, err := remoto.LeerCredenciales()
		if err != nil {
			return err
		}
		delete(m, srv)
		if err := remoto.GuardarCredenciales(m); err != nil {
			return err
		}
		linea(c, "%s Sesión cerrada en %s", sAcento.Render("✓"), srv)
		return nil
	},
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Muestra con qué usuario estás conectado",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		srv := servidorPorDefecto(loginFlags.servidor)
		cli, _, err := remoto.Conectado(srv)
		if err != nil {
			return err
		}
		u, err := cli.Yo()
		if err != nil {
			return err
		}
		linea(c, "%s <%s> · %s · %s", sNegri.Render(u.Nombre), u.Email, u.Rol, srv)
		return nil
	},
}

func init() {
	for _, cm := range []*cobra.Command{loginCmd, logoutCmd, whoamiCmd} {
		cm.Flags().StringVar(&loginFlags.servidor, "servidor", "", "URL del servidor (por defecto el del proyecto o http://localhost:8080)")
	}
	loginCmd.Flags().StringVar(&loginFlags.email, "email", "", "tu email")
	loginCmd.Flags().StringVar(&loginFlags.password, "password", "", "tu contraseña (mejor no pasarla aquí; se pide oculta)")
}
