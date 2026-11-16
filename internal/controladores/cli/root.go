package cli

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/modelos/version"
	"github.com/zara-te05/Marga/internal/servicios/actualizacion"
)

var RootCmd = &cobra.Command{
	Use:   "marga",
	Short: "Control de versiones para bases de datos",
	Long: `Marga lleva el historial de migraciones de tus bases de datos, como git.

Flujo típico:
  marga init                    crea el proyecto en esta carpeta (te pregunta lo necesario)
  marga init --local            igual, pero sin servidor: el historial vive en git
  marga env add dev "postgres://usuario:clave@localhost:5432/bd?sslmode=disable"
  marga adoptar                 si tu base ya tenía tablas: crea la migración 0001 con ese esquema
  marga new crear tabla clientes  (o haz cambios en tu base y: marga commit <descripción>)
  marga status                  qué está aplicado y qué falta
  marga up                      aplica lo pendiente
  marga down                    revierte la última
  marga login / push / pull / log   sincroniza con el servidor
  marga ui                      panel interactivo: entornos, migraciones, drift y riesgo
  marga sql                     Consola de SQL en tu base (se deshace al salir)

Con git, Marga sigue la rama actual: en dev cada rama tiene su propia base (marga ramas).`,
	Version:       version.Texto(),
	SilenceUsage:  true, // ante un error no repetir la ayuda completa
	SilenceErrors: true, // los errores los imprime Execute
}

func init() {
	RootCmd.AddGroup(
		&cobra.Group{ID: "migraciones", Title: "Migraciones:"},
		&cobra.Group{ID: "servidor", Title: "Servidor:"},
		&cobra.Group{ID: "herramientas", Title: "Herramientas:"},
	)
	for _, c := range []*cobra.Command{initCmd, envCmd, adoptarCmd, newCmd, commitCmd, statusCmd, upCmd, downCmd, revisarCmd, driftCmd, esquemaCmd, ramasCmd, urlCmd, seedCmd, sqlCmd, renumerarCmd, verificarCmd, detectarCmd} {
		c.GroupID = "migraciones"
		RootCmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{loginCmd, logoutCmd, whoamiCmd, linkCmd, pushCmd, pullCmd, logCmd, branchCmd, checkoutCmd, mergeCmd} {
		c.GroupID = "servidor"
		RootCmd.AddCommand(c)
	}
	versionCmd.Flags().BoolVar(&versionSinRed, "sin-red", false, "no consultar si hay una versión nueva")
	RootCmd.AddCommand(versionCmd)
	RootCmd.SetVersionTemplate("marga {{.Version}}\n")
	for _, c := range []*cobra.Command{uiCmd} {
		c.GroupID = "herramientas"
		RootCmd.AddCommand(c)
	}
}

var versionSinRed bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Muestra la versión de marga y avisa si hay una más nueva",
	Long: `Muestra la versión instalada, de dónde se está ejecutando y si hay otro marga
en el PATH que la terminal usaría antes. Además consulta GitHub (máximo 2 segundos)
para avisar si hay una versión más nueva y cómo actualizarla.

Para no consultar la red: --sin-red, o la variable MARGA_SIN_AVISO_VERSION=1
(en CI no se consulta).`,
	Args: cobra.NoArgs,
	Run: func(c *cobra.Command, _ []string) {
		linea(c, "marga %s", version.Texto())
		yo, _ := os.Executable()
		if yo != "" {
			linea(c, "%s", sTenue.Render("  "+yo))
		}
		rutas, este := otrosMarga()
		if len(rutas) > 0 && este != 0 {
			linea(c, "%s La terminal usa primero otro marga: %s", sAviso.Render("!"), rutas[0])
			linea(c, "  prueba: si es una versión vieja, bórralo o pon %s antes en el PATH; revisa con: where marga (Windows) o which -a marga", filepath.Dir(yo))
		} else if len(rutas) > 1 {
			linea(c, "%s", sTenue.Render("  También hay otro marga más abajo en el PATH (no se usa): "+rutas[1]))
		}
		if versionSinRed || actualizacion.Desactivada() {
			return
		}
		if version.Version == "dev" {
			linea(c, "%s", sTenue.Render("  Compilación de desarrollo: no se buscan actualizaciones."))
			return
		}
		ult, err := actualizacion.Ultima(context.Background(), 2*time.Second)
		if err != nil {
			linea(c, "%s", sTenue.Render("  No se pudo revisar si hay una versión nueva ("+err.Error()+")."))
			return
		}
		if !actualizacion.MasNueva(ult.Version, version.Version) {
			linea(c, "%s Es la versión más reciente.", sAcento.Render("✓"))
			return
		}
		linea(c, "%s Hay una versión nueva: %s (tienes %s)", sAviso.Render("↑"), sNegri.Render(ult.Version), version.Version)
		if cmd := actualizacion.ComoActualizar(yo); cmd != "" {
			linea(c, "  prueba: %s", cmd)
		} else {
			linea(c, "  prueba: descárgala de %s", ult.URL)
		}
	},
}

// Execute corre el comando que pidieron los argumentos.
func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		imprimirError(os.Stderr, err)
	}
	// Lanzado desde `marga ui`: esperar a que la persona lea la salida antes de volver al panel.
	if os.Getenv(envPausaUI) != "" {
		pausaUI()
	}
	if err != nil {
		os.Exit(1)
	}
}
