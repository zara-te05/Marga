package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var create = &cobra.Command{
	Use:   "create [nombre_archivo]",
	Short: "Crea un archivo vacío",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		file, err := os.Create(args[0])

		if err != nil {
			fmt.Println("Error al crear el archivo:", err)
			return
		}
		file.Close() // Es muy importante cerrarlo inmediatamente después de crearlo
		cmd.Println("Archivo creado exitosamente: ", file)
	},
}
