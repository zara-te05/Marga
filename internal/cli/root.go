package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/version"
)

var RootCmd = &cobra.Command{
	Use:   "marga",
	Short: "Control de versiones para bases de datos",
	Long: `Marga lleva el historial de migraciones de tus bases de datos, como git.

Flujo típico:
  marga init                    crea el proyecto en esta carpeta
  marga env add dev "postgres://usuario:clave@localhost:5432/bd?sslmode=disable"
  marga new crear tabla clientes  (o haz cambios en tu base y: marga commit <descripción>)
  marga status                  qué está aplicado y qué falta
  marga up                      aplica lo pendiente
  marga down                    revierte la última
  marga login / push / pull / log   sincroniza con el servidor

Con git, Marga sigue la rama actual: en dev cada rama tiene su propia base (marga ramas).`,
	Version:       version.Texto(),
	SilenceUsage:  true, // ante un error no repetir la ayuda completa
	SilenceErrors: true, // los errores los imprime Execute (o la TUI)
}

func init() {
	RootCmd.AddGroup(
		&cobra.Group{ID: "migraciones", Title: "Migraciones:"},
		&cobra.Group{ID: "servidor", Title: "Servidor:"},
		&cobra.Group{ID: "consola", Title: "Consola:"},
	)
	for _, c := range []*cobra.Command{initCmd, envCmd, newCmd, commitCmd, statusCmd, upCmd, downCmd, revisarCmd, driftCmd, esquemaCmd, ramasCmd, urlCmd, seedCmd, sqlCmd, renumerarCmd, verificarCmd, detectarCmd} {
		c.GroupID = "migraciones"
		RootCmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{loginCmd, logoutCmd, whoamiCmd, linkCmd, pushCmd, pullCmd, logCmd, branchCmd, checkoutCmd, mergeCmd} {
		c.GroupID = "servidor"
		RootCmd.AddCommand(c)
	}
	RootCmd.AddCommand(versionCmd)
	RootCmd.SetVersionTemplate("marga {{.Version}}\n")
	for _, c := range []*cobra.Command{&ls, cd, &mkdir, create, &clear} {
		c.GroupID = "consola"
		RootCmd.AddCommand(c)
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Muestra la versión de marga",
	Args:  cobra.NoArgs,
	Run: func(c *cobra.Command, _ []string) {
		linea(c, "marga %s", version.Texto())
	},
}

// Execute corre un comando desde la terminal normal (modo no interactivo).
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, sError.Render("Error:"), err)
		os.Exit(1)
	}
}
