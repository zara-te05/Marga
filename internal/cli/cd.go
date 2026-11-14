package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var cd = &cobra.Command{
	Use:   "cd [path] [..]",
	Short: "Cambia de carpeta",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		targetDir := "."

		if len(args) > 0 {
			targetDir = args[0]
		}
		err := os.Chdir(targetDir)

		if err != nil {
			cmd.PrintErrf("Error al cambiar de directorio: %s\n", err)
			return
		}

		dir, _ := os.Getwd()
		cmd.Printf("Directorio actual del proceso: %s\n", dir)
	},
}
