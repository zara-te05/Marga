package cli

import (
	"context"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
	"github.com/zara-te05/Marga/internal/vistas/terminal"
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
		avisarIncorporados(c, m, nombre, false)
		switch {
		case nuevaBase:
			linea(c, "No había línea base en %s: se guardó el esquema actual. Desde ahora se detectan cambios.", sNegri.Render(nombre))
			if locales, err := p.Migraciones(); err == nil {
				sugerirAdoptar(c, locales, real)
			}
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
	Use:   "esquema [OBJETO]",
	Short: "Muestra el esquema real de la base (o el DDL de una tabla, vista, función…)",
	Long: `Sin argumentos lista las tablas, tipos ENUM, funciones, vistas y triggers de la base.
Con un nombre muestra su DDL: una tabla, una vista, un tipo, un trigger o una función
(si hay varias con ese nombre se muestran todas; para una sola usa su firma).`,
	Example: `  marga esquema
  marga esquema clientes
  marga esquema calcular_total
  marga esquema "calcular_total(integer, date)"`,
	Args: cobra.MaximumNArgs(1),
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
			buscado := strings.TrimSpace(args[0])
			ddls := buscarDDL(real, buscado)
			if len(ddls) == 0 {
				return sugerencia.Nuevof("marga esquema --env "+nombre+" para ver los nombres", "no hay ninguna tabla, vista, función, trigger ni tipo llamado %q en %s", buscado, nombre)
			}
			linea(c, "%s", terminal.Resaltar(strings.Join(ddls, "\n\n")))
			return nil
		}
		total := len(real.Tablas) + len(real.Tipos) + len(real.Funciones) + len(real.Vistas) + len(real.Triggers)
		if total == 0 {
			linea(c, "La base %s no tiene tablas todavía", sNegri.Render(nombre))
			return nil
		}
		partes := []string{plural(len(real.Tablas), "tabla", "tablas")}
		for _, x := range []struct {
			n        int
			uno, mas string
		}{{len(real.Tipos), "tipo", "tipos"}, {len(real.Funciones), "función", "funciones"},
			{len(real.Vistas), "vista", "vistas"}, {len(real.Triggers), "trigger", "triggers"}} {
			if x.n > 0 {
				partes = append(partes, plural(x.n, x.uno, x.mas))
			}
		}
		linea(c, "Entorno %s · %s", sNegri.Render(nombre), strings.Join(partes, " · "))
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
			if len(t.Checks) > 0 {
				detalle += " · " + plural(len(t.Checks), "check", "checks")
			}
			linea(c, "  %s %s", relleno(sNegri.Render(t.Nombre), 28), sTenue.Render(detalle))
		}
		seccion := func(titulo string, filas [][2]string) {
			if len(filas) == 0 {
				return
			}
			linea(c, "%s", sTenue.Render(titulo))
			for _, f := range filas {
				linea(c, "  %s %s", relleno(sNegri.Render(f[0]), 28), sTenue.Render(f[1]))
			}
		}
		var filas [][2]string
		for _, t := range real.Tipos {
			filas = append(filas, [2]string{t.Nombre, "ENUM (" + strings.Join(t.Valores, ", ") + ")"})
		}
		seccion("Tipos", filas)
		filas = nil
		for _, f := range real.Funciones {
			det := "→ " + f.Retorno
			if f.Procedimiento {
				det = "procedimiento"
			}
			if f.SecurityDefiner {
				det += " · SECURITY DEFINER"
				if !f.SearchPath {
					det += " sin search_path"
				}
			}
			filas = append(filas, [2]string{f.Firma(), det})
		}
		seccion("Funciones", filas)
		filas = nil
		for _, v := range real.Vistas {
			det := "vista"
			if v.Materializada {
				det = "materializada"
			}
			if len(v.Depende) > 0 {
				var deps []string
				visto := map[string]bool{}
				for _, d := range v.Depende {
					tipo, n, _ := strings.Cut(d, ":")
					if tipo == "columna" {
						n = n[:max(strings.LastIndex(n, "."), 0)]
					}
					if !visto[n] {
						visto[n] = true
						deps = append(deps, n)
					}
				}
				det += " · usa " + strings.Join(deps, ", ")
			}
			filas = append(filas, [2]string{v.Nombre, det})
		}
		seccion("Vistas", filas)
		filas = nil
		for _, t := range real.Triggers {
			_, fn, _ := strings.Cut(t.Funcion, ":")
			det := "en " + t.Tabla
			if fn != "" {
				det += " · ejecuta " + fn
			}
			if t.Desactivado {
				det += " · desactivado"
			}
			filas = append(filas, [2]string{t.Nombre, det})
		}
		seccion("Triggers", filas)
		linea(c, "%s", sTenue.Render("Usa `marga esquema <nombre>` para ver su DDL."))
		return nil
	},
}

func init() {
	driftCmd.Flags().BoolVar(&driftAceptar, "aceptar", false, "acepta los cambios como nueva línea base")
}
