package cmd

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/zarat/Motor-Marga/internal/proyecto"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Administra los entornos (bases de datos) del proyecto",
	Long: `Cada entorno es una base de datos donde aplicar migraciones: dev, staging, prod...
Las cadenas de conexión se guardan en .marga/entornos.json, que NO se versiona.
Puedes usar "$VARIABLE" en lugar de la cadena para leerla de una variable de entorno.`,
}

var envMotor string

var reNombreEnv = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

var envAddCmd = &cobra.Command{
	Use:   "add NOMBRE URL",
	Short: "Agrega o reemplaza un entorno",
	Example: `  marga env add dev "postgres://marga:marga@localhost:5432/tienda?sslmode=disable"
  marga env add prod '$DATABASE_URL'`,
	Args: cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		if !reNombreEnv.MatchString(args[0]) {
			return fmt.Errorf("nombre de entorno inválido: %q (letras, números, - y _)", args[0])
		}
		motor := envMotor
		if motor == "" {
			motor = p.Config.Motor
		}
		if _, err := driverDe(motor); err != nil {
			return err
		}
		m, err := p.Entornos()
		if err != nil {
			return err
		}
		_, existia := m[args[0]]
		m[args[0]] = proyecto.Entorno{URL: args[1], Motor: motor}
		if err := p.GuardarEntornos(m); err != nil {
			return err
		}
		accion := "agregado"
		if existia {
			accion = "actualizado"
		}
		linea(c, "%s Entorno %s %s. Pruébalo con: marga env probar %s", sAcento.Render("✓"), sNegri.Render(args[0]), accion, args[0])
		return nil
	},
}

var envLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "Lista los entornos",
	Args:    cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		m, err := p.Entornos()
		if err != nil {
			return err
		}
		if len(m) == 0 {
			linea(c, "No hay entornos. Agrega uno con `marga env add dev \"postgres://...\"`")
			return nil
		}
		for _, n := range proyecto.NombresEntornos(m) {
			e := m[n]
			linea(c, "  %s %s %s", relleno(sNegri.Render(n), 12), relleno(sTenue.Render(e.Motor), 11), e.URLOculta())
		}
		return nil
	},
}

var envRmCmd = &cobra.Command{
	Use:   "rm NOMBRE",
	Short: "Elimina un entorno",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		m, err := p.Entornos()
		if err != nil {
			return err
		}
		if _, ok := m[args[0]]; !ok {
			return fmt.Errorf("el entorno %q no existe", args[0])
		}
		delete(m, args[0])
		if err := p.GuardarEntornos(m); err != nil {
			return err
		}
		linea(c, "%s Entorno %s eliminado", sAcento.Render("✓"), args[0])
		return nil
	},
}

var envProbarCmd = &cobra.Command{
	Use:   "probar [NOMBRE]",
	Short: "Prueba la conexión con un entorno",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		nombre := ""
		if len(args) == 1 {
			nombre = args[0]
		}
		nombre, e, err := p.Entorno(nombre)
		if err != nil {
			return err
		}
		driver, err := driverDe(e.Motor)
		if err != nil {
			return err
		}
		url, err := e.URLReal()
		if err != nil {
			return err
		}
		if err := ProbarConexion(driver, url); err != nil {
			return fmt.Errorf("%s: %w", nombre, err)
		}
		linea(c, "%s Conexión correcta con %s (%s)", sAcento.Render("✓"), sNegri.Render(nombre), e.URLOculta())
		return nil
	},
}

func init() {
	envAddCmd.Flags().StringVar(&envMotor, "motor", "", "motor (por defecto el del proyecto)")
	envCmd.AddCommand(envAddCmd, envLsCmd, envRmCmd, envProbarCmd)
}
