package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "marga-init",
	Short: "Configure your environment",
	Run: func(cmd *cobra.Command, args []string) {
		Inicializar()
	},
}

func Inicializar() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("¿Desea realizar un scaneo automatico?: (Si/No)")
	revisionAutomatica, err := reader.ReadString('\n')

	if err != nil {
		fmt.Println("Error al leer la entrada:", err)
		return
	}

	respuestaRevision := strings.ToLower(strings.TrimSpace(revisionAutomatica))

	if respuestaRevision == "si" || respuestaRevision == "s" || respuestaRevision == "sí" {
		Scaneo()
	} else {
		fmt.Println("\nOmitiendo escaneo automático. Configuración manual:")

		fmt.Print("¿Cuál es el gestor de tu base de datos? (ej. PostgreSQL, MySQL, SQL Server): ")
		inputGestor, err := reader.ReadString('\n')

		if err != nil {
			fmt.Println("Error al leer la entrada: ", err)
		}

		gestores := map[string]bool{
			"sqlserver":   true,
			"mysql":       true,
			"postgressql": true,
		}

		if _, ok := gestores[inputGestor]; !ok {
			fmt.Println("El gestor no esta en la lista")
		}
	}
	
}
