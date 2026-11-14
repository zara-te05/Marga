package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"  // Driver para MySQL / MariaDB
	_ "github.com/lib/pq"               // Driver para PostgreSQL
	_ "github.com/microsoft/go-mssqldb" // Driver oficial de Microsoft para SQL Server
)

// ProbarConexion es la función genérica que sirve para los tres gestores
func ProbarConexion(driverName, connectionString string) error {

	// 1. Abrir la conexión
	db, err := sql.Open(driverName, connectionString)

	if err != nil {
		return fmt.Errorf("error al estructurar la conexión: %v", err)
	}
	defer db.Close()

	// 2. Configurar tiempos de vida
	db.SetConnMaxLifetime(time.Second * 5)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// 3. Definir un límite de tiempo (timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 4. Probar la conexión real con PingContext
	err = db.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("el servidor no respondió: %v", err)
	}

	return nil
}
