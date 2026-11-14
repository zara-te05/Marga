package cmd

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/spf13/cobra"
)

// Estructura para mapear puertos con sus respectivos motores de BD
type DBEngine struct {
	Name string
	Port int
}

// Scaneo busca motores de bases de datos escuchando en localhost.
func Scaneo(w io.Writer) {
	databases := []DBEngine{
		{"MySQL / MariaDB", 3306},
		{"PostgreSQL", 5432},
		{"Microsoft SQL Server", 1433},
		{"Oracle Database", 1521},
		{"MongoDB", 27017},
		{"Redis", 6379},
		{"Cassandra", 9042},
	}

	fmt.Fprintln(w, "Buscando motores de bases de datos activos en localhost...")

	for _, db := range databases {
		address := fmt.Sprintf("127.0.0.1:%d", db.Port)

		// Intentamos conectar con un tiempo límite corto (500ms)
		conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			fmt.Fprintf(w, "  %s %s en el puerto %d\n", sAcento.Render("●"), db.Name, db.Port)
		} else {
			fmt.Fprintf(w, "  %s %s (puerto %d cerrado)\n", sTenue.Render("○"), db.Name, db.Port)
		}
	}
}

var detectarCmd = &cobra.Command{
	Use:   "detectar",
	Short: "Detecta motores de bases de datos activos en esta máquina",
	Args:  cobra.NoArgs,
	Run: func(c *cobra.Command, args []string) {
		Scaneo(c.OutOrStdout())
	},
}
