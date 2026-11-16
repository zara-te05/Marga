package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
)

var newFlags struct {
	repetible bool
	seed      bool
}

var newCmd = &cobra.Command{
	Use:   "new DESCRIPCION...",
	Short: "Crea una migración nueva (o un objeto repetible, o un seed)",
	Long: `Crea los archivos para escribir SQL a mano:

  marga new agregar telefono       migración: migrations/0007_agregar_telefono.up.sql y .down.sql
  marga new --repetible vistas/clientes activos
                                   objeto repetible: sql/vistas/clientes_activos.sql
                                   (una vista o función completa; marga up lo reaplica al cambiar)
  marga new --seed clientes        datos de prueba: seeds/0001_clientes.sql (marga seed)`,
	Example: `  marga new agregar telefono a usuarios
  marga new --repetible vistas/ventas por mes
  marga new --seed clientes de prueba`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		desc := strings.Join(args, " ")
		if newFlags.repetible && newFlags.seed {
			return errors.New("usa --repetible o --seed, no los dos")
		}
		if newFlags.repetible || newFlags.seed {
			return nuevoScript(c, p, desc)
		}
		nombre := proyecto.Slug(desc)
		if nombre == "" {
			return errors.New("la descripción debe tener letras o números")
		}
		version, err := p.SiguienteVersion()
		if err != nil {
			return err
		}
		id := version + "_" + nombre
		up := fmt.Sprintf("-- Migración %s: %s\n-- Escribe aquí el SQL que aplica el cambio.\n\n", version, desc)
		down := fmt.Sprintf("-- Reversión de %s: %s\n-- Escribe aquí el SQL que deshace lo que hace %s.up.sql.\n\n", version, desc, id)
		fu, fd, err := p.EscribirMigracion(version, nombre, up, down)
		if err != nil {
			return err
		}
		rel := func(f string) string {
			if r, err := filepath.Rel(p.Raiz, f); err == nil {
				return filepath.ToSlash(r)
			}
			return f
		}
		linea(c, "%s Migración %s creada", sAcento.Render("✓"), sNegri.Render(id))
		linea(c, "  %s", rel(fu))
		linea(c, "  %s", rel(fd))
		linea(c, "Edítalas y aplícalas con: marga up")
		return nil
	},
}

// nuevoScript crea un objeto repetible (sql/) o un seed (seeds/). La descripción
// puede llevar carpetas: "vistas/ventas por mes" → sql/vistas/ventas_por_mes.sql.
func nuevoScript(c *cobra.Command, p *proyecto.Proyecto, desc string) error {
	partes := strings.Split(filepath.ToSlash(desc), "/")
	for i, parte := range partes {
		if partes[i] = proyecto.Slug(parte); partes[i] == "" {
			return errors.New("la descripción debe tener letras o números (y no carpetas vacías)")
		}
	}
	nombre := strings.Join(partes, "/")
	var dir, contenido, siguiente string
	if newFlags.repetible {
		dir = p.DirRepetibles()
		nombre += ".sql"
		contenido = fmt.Sprintf(`-- Objeto repetible: %s
-- Escribe el objeto COMPLETO, de forma que se pueda ejecutar varias veces:
--   CREATE OR REPLACE VIEW …, CREATE OR REPLACE FUNCTION …
-- Cada vez que cambies este archivo, marga up lo vuelve a aplicar.
-- Si depende de otro repetible, numera los archivos (01_…, 02_…): se aplican en orden.

`, desc)
		siguiente = "Escríbelo y aplícalo con: marga up"
	} else {
		dir = p.DirSeeds()
		base := filepath.Base(nombre)
		nombre = strings.TrimSuffix(nombre, base) + p.SiguienteSeed() + "_" + base + ".sql"
		contenido = fmt.Sprintf(`-- Datos de prueba: %s
-- Solo se cargan en entornos personales (dev, local) con marga seed, una vez por base.
-- Ejemplo:
--   INSERT INTO clientes (nombre, email) VALUES ('Ana', 'ana@ejemplo.com'), ('Beto', 'beto@ejemplo.com');

`, desc)
		siguiente = "Escríbelo y cárgalo con: marga seed"
	}
	ruta, err := proyecto.EscribirScript(dir, nombre, contenido)
	if err != nil {
		return err
	}
	linea(c, "%s Creado %s", sAcento.Render("✓"), sNegri.Render(relativo(p, ruta)))
	linea(c, "%s", siguiente)
	return nil
}

func init() {
	newCmd.Flags().BoolVar(&newFlags.repetible, "repetible", false, "crea un objeto repetible en sql/ (vista, función…)")
	newCmd.Flags().BoolVar(&newFlags.seed, "seed", false, "crea un archivo de datos de prueba en seeds/")
}
