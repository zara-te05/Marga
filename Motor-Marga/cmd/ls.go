package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var ls = cobra.Command{
	Use:   "ls [ruta]",
	Short: "This command lists the files of the selected path",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		var root string

		if len(args) > 0 {
			// Se pasó una ruta explícita
			root = args[0]
		} else {
			// Sin argumentos: listar la carpeta actual en la que estamos parados
			root = "."
		}

		entries, err := os.ReadDir(root)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				fmt.Println("[DIR]", entry.Name())
			} else {
				fmt.Println("[FILE]", entry.Name())
			}
		}
	},
}
