package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zarat/Motor-Marga/internal/generador"
	"github.com/zarat/Motor-Marga/internal/introspeccion"
	"github.com/zarat/Motor-Marga/internal/migrador"
	"github.com/zarat/Motor-Marga/internal/proyecto"
	"github.com/zarat/Motor-Marga/internal/riesgo"
)

var commitDryRun bool

var commitCmd = &cobra.Command{
	Use:   "commit DESCRIPCION...",
	Short: "Genera una migración a partir de los cambios que hiciste en tu base",
	Long: `Compara tu base de desarrollo con el esquema que dejó la última migración y
escribe el .up.sql y el .down.sql por ti. Como esos cambios ya están en tu base,
la migración queda registrada como aplicada en ese entorno.

  1. Cambia tu base como quieras (SQL a mano, pgAdmin, DBeaver…)
  2. marga commit agregar telefono a clientes
  3. Revisa los archivos generados y súbelos con marga push

Genera: tablas, columnas (tipo, NULL, DEFAULT), llave primaria, llaves foráneas e
índices. Un renombre lo verá como borrar + crear: corrígelo a mano con RENAME.`,
	Example: `  marga commit agregar telefono a clientes
  marga commit crear tabla facturas --dry-run`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, locales, nombre, e, m, err := conectar(ctx)
		if err != nil {
			return err
		}
		defer m.Close()
		desc := strings.Join(args, " ")
		nombreMig := proyecto.Slug(desc)
		if nombreMig == "" {
			return errors.New("la descripción debe tener letras o números")
		}

		aplicadas, err := m.Historial(ctx)
		if err != nil {
			return err
		}
		for _, f := range migrador.Comparar(locales, aplicadas) {
			switch f.Situacion {
			case migrador.Pendiente, migrador.Renumerada:
				return fmt.Errorf("%s tiene migraciones sin aplicar (%s). Aplícalas primero con: marga up --env %s", nombre, f.ID(), nombre)
			case migrador.Modificada, migrador.Huerfana:
				return fmt.Errorf("el historial de %s no cuadra (%s); revísalo con marga status", nombre, f.ID())
			}
		}

		base, err := m.LineaBase(ctx)
		if err != nil {
			return err
		}
		if base == nil {
			if _, err := m.AceptarEsquema(ctx); err != nil {
				return err
			}
			linea(c, "No había línea base en %s: se guardó el estado actual.", sNegri.Render(nombre))
			linea(c, "Haz tus cambios en la base y vuelve a ejecutar: marga commit %s", desc)
			return nil
		}
		real, err := m.EsquemaReal(ctx)
		if err != nil {
			return err
		}
		cambios := introspeccion.Comparar(*base, real)
		if len(cambios) == 0 {
			linea(c, "No hay cambios en %s desde la última migración. Cambia la base y vuelve a intentarlo.", sNegri.Render(nombre))
			return nil
		}
		gen := generador.Generar(*base, real)

		linea(c, "Cambios detectados en %s:", sNegri.Render(nombre))
		for _, ch := range cambios {
			linea(c, "    %s", ch)
		}
		if strings.TrimSpace(gen.Up) == "" {
			return errors.New("esos cambios no se pueden generar automáticamente; escribe la migración a mano con marga new")
		}
		linea(c, "\n%s", sAcento.Render("-- UP"))
		linea(c, "%s", strings.TrimRight(gen.Up, "\n"))
		linea(c, "%s", sTenue.Render("-- DOWN"))
		for _, l := range strings.Split(strings.TrimRight(gen.Down, "\n"), "\n") {
			linea(c, "%s", sTenue.Render(l))
		}
		linea(c, "")
		for _, a := range gen.Avisos {
			linea(c, "%s %s", sAviso.Render("!"), a)
		}

		version, err := p.SiguienteVersion()
		if err != nil {
			return err
		}
		id := version + "_" + nombreMig
		up := fmt.Sprintf("-- Migración %s: %s\n-- Generada por `marga commit` a partir de los cambios en %s. Revísala antes de enviarla.\n\n%s", version, desc, nombre, gen.Up)
		down := fmt.Sprintf("-- Reversión de %s: %s\n\n%s", version, desc, gen.Down)
		mostrarRiesgo(c, id, riesgo.Analizar(up, down), false)
		if commitDryRun {
			linea(c, "%s", sTenue.Render("(simulación: no se escribió ni registró nada)"))
			return nil
		}

		// El estado ANTES de esta migración es la línea base: sirve de snapshot de la anterior
		// para que `push` sepa qué tablas cambió esta.
		if n := len(locales); n > 0 {
			var tmp introspeccion.Esquema
			if ok, _ := p.LeerSnapshot(nombre, locales[n-1].ID(), &tmp); !ok {
				_ = p.GuardarSnapshot(nombre, locales[n-1].ID(), base)
			}
		}
		mig := proyecto.Migracion{Version: version, Nombre: nombreMig, Up: proyecto.Normalizar(up), Down: proyecto.Normalizar(down)}
		mig.Checksum = proyecto.Checksum(mig.Up)
		// Primero se registra (si falla no queda un archivo que luego no se pueda aplicar)…
		snap, err := m.Registrar(ctx, mig, autor(p))
		if err != nil {
			return fmt.Errorf("no se pudo registrar la migración en %s: %w", nombre, err)
		}
		// …y después se escriben los archivos.
		fu, fd, err := p.EscribirMigracion(version, nombreMig, mig.Up, mig.Down)
		if err != nil {
			return fmt.Errorf("la migración quedó registrada en %s pero no se pudieron escribir los archivos: %w", nombre, err)
		}
		if snap != nil {
			_ = p.GuardarSnapshot(nombre, id, snap)
		}
		linea(c, "%s Migración %s creada y marcada como aplicada en %s (los cambios ya estaban ahí)", sAcento.Render("✓"), sNegri.Render(id), nombre)
		linea(c, "  %s", relativo(p, fu))
		linea(c, "  %s", relativo(p, fd))
		linea(c, "Revísala y compártela con: marga push")
		reportar(c, p, nombre, e, m, snap, nil)
		return nil
	},
}

// ---------------------------------------------------------------- revisar

var revisarCmd = &cobra.Command{
	Use:   "revisar [MIGRACION...]",
	Short: "Analiza el riesgo de tus migraciones (pérdida de datos, bloqueos…)",
	Long: `Revisa el SQL y avisa de lo que puede perder datos, bloquear tablas o fallar en producción.
Sin argumentos revisa las migraciones que aún no envías al servidor (o todas si ya enviaste todo).`,
	Example: `  marga revisar
  marga revisar 0007 0008`,
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		locales, err := p.Migraciones()
		if err != nil {
			return err
		}
		var elegidas []proyecto.Migracion
		if len(args) > 0 {
			for _, a := range args {
				encontrada := false
				for _, l := range locales {
					if l.ID() == a || l.Version == a || versionNum(l.Version) == versionNum(a) && versionNum(a) > 0 {
						elegidas = append(elegidas, l)
						encontrada = true
					}
				}
				if !encontrada {
					return fmt.Errorf("no encontré la migración %q", a)
				}
			}
		} else {
			mapa, _ := p.Remoto()
			for _, l := range locales {
				if _, enviada := mapa[l.ID()]; !enviada {
					elegidas = append(elegidas, l)
				}
			}
			if len(elegidas) == 0 {
				elegidas = locales
			}
		}
		if len(elegidas) == 0 {
			linea(c, "No hay migraciones que revisar.")
			return nil
		}
		cuenta := map[string]int{}
		for _, l := range elegidas {
			hs := riesgo.Analizar(l.Up, l.Down)
			for _, h := range hs {
				cuenta[h.Nivel]++
			}
			if len(hs) == 0 {
				linea(c, "%s %s %s", sAcento.Render("✓"), l.ID(), sTenue.Render("sin riesgos detectados"))
				continue
			}
			mostrarRiesgo(c, l.ID(), hs, true)
		}
		linea(c, "%s revisadas · %d alto · %d medio · %d bajo", plural(len(elegidas), "migración", "migraciones"), cuenta["alto"], cuenta["medio"], cuenta["bajo"])
		return nil
	},
}

func relativo(p *proyecto.Proyecto, f string) string {
	if r, err := relPath(p.Raiz, f); err == nil {
		return r
	}
	return f
}

func init() {
	commitCmd.Flags().StringVarP(&migFlags.env, "env", "e", "", "entorno donde hiciste los cambios (normalmente dev)")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "muestra el SQL sin escribir ni registrar nada")
}
