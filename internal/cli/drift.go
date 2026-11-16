package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var driftAceptar bool

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Muestra los cambios hechos en la base fuera de Marga",
	Long: `Compara el esquema real de la base con el que dejó la última migración de Marga.
Cualquier diferencia (una columna agregada a mano, un índice borrado…) es "drift".

  marga drift              lista los cambios
  marga drift --aceptar    los toma como nueva línea base (ya no se reportan)`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, _, nombre, e, m, err := conectar(ctx, c)
		if err != nil {
			return err
		}
		defer m.Close()
		cambios, real, nuevaBase, err := m.Drift(ctx)
		if err != nil {
			return err
		}
		switch {
		case nuevaBase:
			linea(c, "No había línea base en %s: se guardó el esquema actual. Desde ahora se detectan cambios.", sNegri.Render(nombre))
		case len(cambios) == 0:
			linea(c, "%s Sin drift en %s: el esquema coincide con lo que dejó Marga", sAcento.Render("✓"), sNegri.Render(nombre))
		case driftAceptar:
			mostrarDrift(c, nombre, cambios, 0)
			if _, err := m.AceptarEsquema(ctx); err != nil {
				return err
			}
			linea(c, "%s Línea base actualizada: %s", sAcento.Render("✓"), plural(len(cambios), "cambio aceptado", "cambios aceptados"))
			cambios = nil
		default:
			mostrarDrift(c, nombre, cambios, 0)
			linea(c, "")
			linea(c, "Si son correctos: marga drift --aceptar --env %s", nombre)
			linea(c, "Si no: deshazlos a mano. Para que el equipo los tenga, conviértelos en una migración.")
		}
		reportar(c, p, nombre, e, m, &real, cambios)
		return nil
	},
}

var esquemaCmd = &cobra.Command{
	Use:   "esquema [TABLA]",
	Short: "Muestra el esquema real de la base (o el DDL de una tabla)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		_, _, nombre, _, m, err := conectar(ctx, c)
		if err != nil {
			return err
		}
		defer m.Close()
		real, err := m.EsquemaReal(ctx)
		if err != nil {
			return err
		}
		if len(args) == 1 {
			t := real.Buscar(args[0])
			if t == nil {
				return fmt.Errorf("la tabla %q no existe en %s", args[0], nombre)
			}
			linea(c, "%s", strings.TrimRight(t.DDL(), "\n"))
			return nil
		}
		if len(real.Tablas) == 0 {
			linea(c, "La base %s no tiene tablas todavía", sNegri.Render(nombre))
			return nil
		}
		linea(c, "Entorno %s · %s", sNegri.Render(nombre), plural(len(real.Tablas), "tabla", "tablas"))
		for _, t := range real.Tablas {
			detalle := plural(len(t.Columnas), "columna", "columnas")
			if len(t.FKs) > 0 {
				refs := make([]string, 0, len(t.FKs))
				for _, f := range t.FKs {
					refs = append(refs, f.Tabla)
				}
				detalle += " · → " + strings.Join(refs, ", ")
			}
			if len(t.Indices) > 0 {
				detalle += " · " + plural(len(t.Indices), "índice", "índices")
			}
			linea(c, "  %s %s", relleno(sNegri.Render(t.Nombre), 28), sTenue.Render(detalle))
		}
		linea(c, "%s", sTenue.Render("Usa `marga esquema <tabla>` para ver su DDL."))
		return nil
	},
}

func init() {
	driftCmd.Flags().BoolVar(&driftAceptar, "aceptar", false, "acepta los cambios como nueva línea base")
}
