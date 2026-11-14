package cmd

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// Estilos locales para la salida de ls
var (
	folderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true) // Azul celeste brillante
	fileStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1"))            // Gris claro
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))            // Gris oscuro para metadatos
)

var ls = cobra.Command{
	Use:   "ls [ruta]",
	Short: "Lista los archivos del directorio",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		var root string
		if len(args) > 0 {
			root = args[0]
		} else {
			root = "."
		}

		entries, err := os.ReadDir(root)
		if err != nil {
			cmd.Println(dimStyle.Render("Error: " + err.Error()))
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				name := folderStyle.Render(entry.Name() + "/")
				cmd.Printf("  📁 %s\n", name)
			} else {
				name := fileStyle.Render(entry.Name())
				cmd.Printf("  📄 %s\n", name)
			}
		}
	},
}
