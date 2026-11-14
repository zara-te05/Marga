package cmd

import (
	"fmt"
	"net"
	"time"
)

// Estructura para mapear puertos con sus respectivos motores de BD
type DBEngine struct {
	Name string
	Port int
}

func Scaneo() {

	databases := []DBEngine{
		{"MySQL / MariaDB", 3306},
		{"PostgreSQL", 5432},
		{"Microsoft SQL Server", 1433},
		{"Oracle Database", 1521},
		{"MongoDB", 27017},
		{"Redis", 6379},
		{"SQLite", 0}, // SQLite es un archivo, no usa puerto de red
		{"Cassandra", 9042},
	}

	fmt.Println("Buscando motores de bases de datos instalados/activos en localhost...\n")

	for _, db := range databases {
		if db.Port == 0 {
			continue // Omitimos los que no usan red por ahora
		}

		address := fmt.Sprintf("127.0.0.1:%d", db.Port)

		// Intentamos conectar con un tiempo límite corto (500ms)
		conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			fmt.Printf("[DETECTADO] %s está activo en el puerto %d\n", db.Name, db.Port)
		} else {
			fmt.Printf("[NO DETECTADO] %s (Puerto %d cerrado o sin servicio)\n", db.Name, db.Port)
		}
	}
}
