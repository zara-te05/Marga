// Package motor es lo que Marga necesita de cada base de datos. El resto del
// código (migrador, drift, commit, verificar, el panel, la web) habla con esta
// interfaz; cada motor la implementa en su carpeta (postgres/, y luego mysql/,
// sqlserver/, sqlite/…).
package motor

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/zara-te05/Marga/internal/datos/motor/postgres"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

// Querier: una base o una transacción abierta.
type Querier = interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Motor es una base de datos que Marga sabe leer y migrar.
type Motor interface {
	// Nombre como se guarda en el proyecto: "postgresql".
	Nombre() string
	// Driver de database/sql: "postgres".
	Driver() string
	// Leer obtiene el esquema real (introspección). Funciona dentro de una transacción.
	Leer(ctx context.Context, q Querier) (esquema.Esquema, error)
	// Generar escribe el SQL que lleva de un esquema a otro, en el dialecto del motor.
	Generar(antes, despues esquema.Esquema) esquema.Cambios
	// El DDL de cada objeto, para mostrarlo y para los snapshots.
	esquema.Dialecto
	// DDLEnTransaccion: si un CREATE o ALTER se puede deshacer con ROLLBACK.
	// Sin eso, una migración que falla a la mitad deja la base a medias.
	DDLEnTransaccion() bool
	// Bloquear y Desbloquear evitan que dos procesos migren la misma base a la vez.
	Bloquear(ctx context.Context, conn *sql.Conn, clave int64) (bool, error)
	Desbloquear(ctx context.Context, conn *sql.Conn, clave int64)
}

// ConPreparacion: el motor sabe qué necesita una base vacía antes de recibir un
// esquema adoptado (esquemas, extensiones…). Opcional.
type ConPreparacion interface {
	Preparacion(ctx context.Context, q Querier) (crear, quitar []string, err error)
}

var motores = map[string]Motor{
	"postgresql": postgres.Motor{},
}

// De devuelve el motor por su nombre ("" es PostgreSQL, el de siempre).
func De(nombre string) (Motor, error) {
	if nombre == "" {
		nombre = "postgresql"
	}
	if m, ok := motores[nombre]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("aplicar migraciones en %s todavía no está soportado (por ahora: %s)", nombre, strings.Join(Soportados(), ", "))
}

// Soportados lista los motores con los que Marga ya puede migrar.
func Soportados() []string {
	var out []string
	for n := range motores {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// PorDefecto es PostgreSQL.
func PorDefecto() Motor { return motores["postgresql"] }
