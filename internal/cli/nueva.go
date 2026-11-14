package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/proyecto"
)

var newCmd = &cobra.Command{
	Use:     "new DESCRIPCION...",
	Short:   "Crea una migración nueva (archivos .up.sql y .down.sql)",
	Example: `  marga new agregar telefono a usuarios`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		desc := strings.Join(args, " ")
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
