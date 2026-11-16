package cli_test

import (
	"github.com/zara-te05/Marga/internal/controladores/cli"
	"testing"

	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

// El snapshot de la migración anterior lo guardó una versión vieja de Marga (sin
// funciones, vistas ni CHECK). Al enviar la nueva, solo debe ir lo que cambió de
// verdad, no "todas las funciones se crearon aquí".
func TestSnapshotsConSnapshotViejo(t *testing.T) {
	tabla := esquema.Tabla{Nombre: "clientes", PK: []string{"id"},
		Columnas: []esquema.Columna{{Nombre: "id", Tipo: "integer"}}}
	antes := esquema.Esquema{Motor: "postgresql", Tablas: []esquema.Tabla{tabla}} // version 0

	conCheck := tabla
	conCheck.Checks = []esquema.Check{{Nombre: "id_positivo", Definicion: "CHECK ((id > 0))"}}
	nueva := conCheck
	nueva.Nombre = "facturas"
	despues := esquema.Esquema{Version: esquema.VersionActual, Motor: "postgresql",
		Tablas:    []esquema.Tabla{conCheck, nueva},
		Funciones: []esquema.Funcion{{Nombre: "f", Tipos: "integer", Definicion: "CREATE OR REPLACE FUNCTION public.f(integer) …"}},
		Vistas:    []esquema.Vista{{Nombre: "v", Definicion: " SELECT 1"}},
	}
	got := cli.SnapshotsEntre(antes, despues)
	if len(got) != 1 || got[0].Tabla != "facturas" || got[0].Tipo != "tabla" {
		t.Fatalf("solo debía ir la tabla nueva: %+v", got)
	}

	// Entre dos snapshots v2 sí van funciones y vistas, con su tipo.
	despues2 := despues
	despues2.Funciones = []esquema.Funcion{{Nombre: "f", Tipos: "integer", Definicion: "CREATE OR REPLACE FUNCTION public.f(integer) … v2"}}
	despues2.Vistas = nil
	got = cli.SnapshotsEntre(despues, despues2)
	if len(got) != 2 || got[0].Tabla != "f(integer)" || got[0].Tipo != "funcion" || got[1].Tabla != "v" || !got[1].Eliminada || got[1].Tipo != "vista" {
		t.Fatalf("%+v", got)
	}
}
