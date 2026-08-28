package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var mkdir = cobra.Command{
	Use:   "mkdir [name]",
	Short: "This command have the capacity to create folders",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		err := os.MkdirAll(args[0], 0755)

		if err != nil {
			cmd.Println("Error al crear la carpeta:", err)
		}
	},
}
