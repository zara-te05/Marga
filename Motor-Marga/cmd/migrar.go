package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zarat/Motor-Marga/internal/introspeccion"
	"github.com/zarat/Motor-Marga/internal/migrador"
	"github.com/zarat/Motor-Marga/internal/proyecto"
	"github.com/zarat/Motor-Marga/internal/remoto"
	"github.com/zarat/Motor-Marga/internal/riesgo"
)

var migFlags struct {
	env    string
	hasta  string
	pasos  int
	dryRun bool
	si     bool
}

// conectar abre el proyecto, sus migraciones locales y el entorno elegido.
func conectar(ctx context.Context) (*proyecto.Proyecto, []proyecto.Migracion, string, proyecto.Entorno, *migrador.Migrador, error) {
	p, err := proyectoActual()
	if err != nil {
		return nil, nil, "", proyecto.Entorno{}, nil, err
	}
	locales, err := p.Migraciones()
	if err != nil {
		return nil, nil, "", proyecto.Entorno{}, nil, err
	}
	nombre, e, err := p.Entorno(migFlags.env)
	if err != nil {
		return p, locales, "", e, nil, err
	}
	m, err := migrador.Abrir(ctx, e)
	if err != nil {
		return p, locales, nombre, e, nil, fmt.Errorf("%s: %w", nombre, err)
	}
	return p, locales, nombre, e, m, nil
}

// ---------------------------------------------------------------- status

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Muestra qué migraciones están aplicadas y cuáles faltan en un entorno",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, locales, nombre, e, m, err := conectar(ctx)
		if p != nil && m == nil && strings.Contains(fmt.Sprint(err), "no hay entornos") {
			// Sin entornos: al menos listamos lo local.
			linea(c, "%d migraciones locales (sin entorno configurado):", len(locales))
			for _, l := range locales {
				linea(c, "  %s %s", sTenue.Render("·"), l.ID())
			}
			linea(c, "Agrega uno con: marga env add dev \"postgres://...\"")
			return nil
		}
		if err != nil {
			return err
		}
		defer m.Close()
		aplicadas, err := m.Historial(ctx)
		if err != nil {
			return err
		}
		filas := migrador.Comparar(locales, aplicadas)

		linea(c, "Entorno %s · %s · %s", sNegri.Render(nombre), e.Motor, sTenue.Render(e.URLOculta()))
		if len(filas) == 0 {
			linea(c, "  No hay migraciones todavía. Crea una con marga new, o cambia la base y usa marga commit.")
		}
		cuenta := map[migrador.Situacion]int{}
		for _, f := range filas {
			cuenta[f.Situacion]++
			id := relleno(f.ID(), 40)
			switch f.Situacion {
			case migrador.Aplicada_:
				linea(c, "  %s %s %s", sAcento.Render("✓"), id, sTenue.Render(fmt.Sprintf("aplicada %s · %dms · %s",
					f.Registro.AplicadaEn.Local().Format("2006-01-02 15:04"), f.Registro.DuracionMs, f.Registro.AplicadaPor)))
			case migrador.Pendiente:
				nota := "pendiente"
				if f.FueraDeOrden {
					nota += " (fuera de orden: es anterior a la última aplicada)"
				}
				linea(c, "  %s %s %s", sTenue.Render("·"), id, nota)
			case migrador.Modificada:
				linea(c, "  %s %s %s", sAviso.Render("!"), id, sAviso.Render("el archivo cambió después de aplicarse"))
			case migrador.Huerfana:
				linea(c, "  %s %s %s", sError.Render("?"), id, sError.Render("aplicada en la base pero no existe el archivo"))
			case migrador.Renumerada:
				linea(c, "  %s %s %s", sAcento.Render("↻"), id, sTenue.Render("ya aplicada como "+f.Registro.Version+"_"+f.Registro.Nombre+" (renumerada; `marga up` actualiza el historial)"))
			}
		}
		resumen := plural(cuenta[migrador.Aplicada_]+cuenta[migrador.Modificada]+cuenta[migrador.Renumerada], "aplicada", "aplicadas") + " · " +
			plural(cuenta[migrador.Pendiente], "pendiente", "pendientes")
		if n := cuenta[migrador.Modificada] + cuenta[migrador.Huerfana]; n > 0 {
			resumen += sAviso.Render(fmt.Sprintf(" · %d con problemas", n))
		}
		linea(c, "%s", resumen)

		cambios, real, nuevaBase, err := m.Drift(ctx)
		switch {
		case err != nil:
			linea(c, "%s No se pudo revisar el drift: %v", sAviso.Render("!"), err)
		case nuevaBase:
			linea(c, "%s", sTenue.Render("Se guardó el esquema actual como línea base: desde ahora se detectan cambios hechos fuera de Marga."))
		case len(cambios) > 0:
			mostrarDrift(c, nombre, cambios, 8)
		default:
			linea(c, "%s", sTenue.Render("Sin drift: el esquema coincide con lo que dejó Marga."))
		}
		if err == nil {
			reportar(c, p, nombre, e, m, &real, cambios)
		}
		return nil
	},
}

// revisarDrift detiene up/down si alguien cambió la base a mano.
func revisarDrift(ctx context.Context, c *cobra.Command, m *migrador.Migrador, nombre string) error {
	cambios, _, nuevaBase, err := m.Drift(ctx)
	if err != nil {
		return fmt.Errorf("no se pudo revisar el drift: %w", err)
	}
	if nuevaBase {
		linea(c, "%s", sTenue.Render("Se guardó el esquema actual como línea base."))
	}
	if len(cambios) == 0 {
		return nil
	}
	mostrarDrift(c, nombre, cambios, 8)
	if migFlags.dryRun {
		return nil // la simulación no toca nada: solo avisa
	}
	return errors.New("no se hizo nada: la base tiene cambios hechos fuera de Marga.\n" +
		"  Si son correctos: marga drift --aceptar --env " + nombre + "\n" +
		"  Si no: deshazlos a mano y vuelve a intentarlo")
}

// ---------------------------------------------------------------- up

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Aplica las migraciones pendientes en un entorno",
	Example: `  marga up                  aplica todo lo pendiente (si solo hay un entorno)
  marga up --env staging
  marga up --hasta 0005      aplica hasta la versión 0005
  marga up --dry-run         muestra el SQL sin ejecutarlo`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, locales, nombre, e, m, err := conectar(ctx)
		if err != nil {
			return err
		}
		defer m.Close()
		aplicadas, err := m.Historial(ctx)
		if err != nil {
			return err
		}
		filas := migrador.Comparar(locales, aplicadas)

		var problemas, vacias []string
		var pendientes []proyecto.Migracion
		var renumeradas []migrador.Fila
		for _, f := range filas {
			switch f.Situacion {
			case migrador.Renumerada:
				renumeradas = append(renumeradas, f)
			case migrador.Modificada:
				problemas = append(problemas, f.ID()+": el archivo cambió después de aplicarse")
			case migrador.Huerfana:
				problemas = append(problemas, f.ID()+": está aplicada pero no existe el archivo (¿te falta un marga pull?)")
			case migrador.Pendiente:
				if migFlags.hasta != "" && versionNum(f.Version) > versionNum(migFlags.hasta) {
					continue
				}
				if proyecto.EsVacio(f.Local.Up) {
					vacias = append(vacias, f.ID())
					continue
				}
				if f.FueraDeOrden {
					linea(c, "%s %s es anterior a la última aplicada; se aplicará igual", sAviso.Render("!"), f.ID())
				}
				pendientes = append(pendientes, *f.Local)
			}
		}
		if len(vacias) > 0 {
			return fmt.Errorf("no se aplicó nada: %s no tiene SQL todavía (edita su .up.sql)", strings.Join(vacias, ", "))
		}
		if len(problemas) > 0 {
			return errors.New("no se aplicó nada porque el historial no cuadra:\n  - " + strings.Join(problemas, "\n  - ") +
				"\nNunca edites una migración ya aplicada: crea una nueva con `marga new`.")
		}
		if len(renumeradas) > 0 && !migFlags.dryRun {
			if err := m.Renumerar(ctx, renumeradas); err != nil {
				return err
			}
			for _, f := range renumeradas {
				linea(c, "  %s %s ya estaba aplicada como %s_%s: historial actualizado", sAcento.Render("↻"), f.ID(), f.Registro.Version, f.Registro.Nombre)
			}
		}
		if len(pendientes) == 0 {
			if len(renumeradas) > 0 && !migFlags.dryRun {
				reportar(c, p, nombre, e, m, nil, nil)
			}
			linea(c, "%s La base %s ya está al día", sAcento.Render("✓"), sNegri.Render(nombre))
			return nil
		}
		if err := revisarDrift(ctx, c, m, nombre); err != nil {
			return err
		}
		for _, mig := range pendientes {
			mostrarRiesgo(c, mig.ID(), riesgo.Analizar(mig.Up, mig.Down), false)
		}
		if err := verificarAprobaciones(c, p, nombre, e, pendientes); err != nil {
			return err
		}

		if migFlags.dryRun {
			linea(c, "Se aplicaría(n) %s en %s (simulación, no se ejecuta nada):", plural(len(pendientes), "migración", "migraciones"), sNegri.Render(nombre))
			for _, mig := range pendientes {
				linea(c, "\n%s", sAcento.Render("-- "+mig.ID()))
				linea(c, "%s", strings.TrimRight(mig.Up, "\n"))
			}
			return nil
		}
		if esProduccion(nombre) && !migFlags.si {
			return fmt.Errorf("%q parece producción: revisa con --dry-run y confirma con --si", nombre)
		}

		guardarSnapshotPrevio(ctx, p, m, nombre, locales, pendientes[0])

		linea(c, "Aplicando %s en %s…", plural(len(pendientes), "migración", "migraciones"), sNegri.Render(nombre))
		hechas := 0
		err = m.Subir(ctx, pendientes, autor(p), func(mig proyecto.Migracion, d time.Duration, snap *introspeccion.Esquema, err error) {
			if err != nil {
				linea(c, "  %s %s %s", sError.Render("✗"), mig.ID(), sError.Render(err.Error()))
				return
			}
			hechas++
			// El esquema que dejó esta migración: `marga push` lo envía al servidor.
			if snap != nil {
				if err := p.GuardarSnapshot(nombre, mig.ID(), snap); err != nil {
					linea(c, "  %s no se pudo guardar el snapshot de %s: %v", sAviso.Render("!"), mig.ID(), err)
				}
			}
			extra := ""
			if mig.SinTransaccion {
				extra = " (sin transacción)"
			}
			linea(c, "  %s %s %s", sAcento.Render("✓"), relleno(mig.ID(), 40), sTenue.Render(fmt.Sprintf("%dms%s", d.Milliseconds(), extra)))
		})
		if hechas > 0 {
			reportar(c, p, nombre, e, m, nil, nil)
		}
		if err != nil {
			return fmt.Errorf("se aplicaron %d de %d; la que falló se deshizo por completo", hechas, len(pendientes))
		}
		linea(c, "%s %s", sAcento.Render("✓"), plural(hechas, "migración aplicada", "migraciones aplicadas"))
		return nil
	},
}

// ---------------------------------------------------------------- down

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Revierte las últimas migraciones aplicadas en un entorno",
	Example: `  marga down                 revierte la última
  marga down --pasos 3
  marga down --dry-run`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, locales, nombre, e, m, err := conectar(ctx)
		if err != nil {
			return err
		}
		defer m.Close()
		aplicadas, err := m.Historial(ctx)
		if err != nil {
			return err
		}
		if len(aplicadas) == 0 {
			linea(c, "No hay migraciones aplicadas en %s", sNegri.Render(nombre))
			return nil
		}
		if migFlags.pasos < 1 {
			return errors.New("--pasos debe ser al menos 1")
		}
		porVersion := map[string]proyecto.Migracion{}
		for _, l := range locales {
			porVersion[l.Version] = l
		}
		var aRevertir []proyecto.Migracion
		for i := len(aplicadas) - 1; i >= 0 && len(aRevertir) < migFlags.pasos; i-- {
			a := aplicadas[i]
			l, ok := porVersion[a.Version]
			if !ok {
				return fmt.Errorf("%s_%s está aplicada pero no tengo su archivo para revertirla (¿marga pull?)", a.Version, a.Nombre)
			}
			if l.Down == "" {
				return fmt.Errorf("%s no tiene script DOWN; escribe %s.down.sql para poder revertirla", l.ID(), l.ID())
			}
			if l.Checksum != a.Checksum {
				linea(c, "%s %s cambió después de aplicarse; se usará el .down.sql actual", sAviso.Render("!"), l.ID())
			}
			aRevertir = append(aRevertir, l)
		}
		if err := revisarDrift(ctx, c, m, nombre); err != nil {
			return err
		}

		if migFlags.dryRun {
			linea(c, "Se revertiría(n) %s en %s (simulación):", plural(len(aRevertir), "migración", "migraciones"), sNegri.Render(nombre))
			for _, mig := range aRevertir {
				linea(c, "\n%s", sAcento.Render("-- revertir "+mig.ID()))
				linea(c, "%s", strings.TrimRight(mig.Down, "\n"))
			}
			return nil
		}
		if esProduccion(nombre) && !migFlags.si {
			return fmt.Errorf("%q parece producción: revisa con --dry-run y confirma con --si", nombre)
		}

		linea(c, "Revirtiendo %s en %s…", plural(len(aRevertir), "migración", "migraciones"), sNegri.Render(nombre))
		hechas := 0
		err = m.Bajar(ctx, aRevertir, func(mig proyecto.Migracion, d time.Duration, _ *introspeccion.Esquema, err error) {
			if err != nil {
				linea(c, "  %s %s %s", sError.Render("✗"), mig.ID(), sError.Render(err.Error()))
				return
			}
			hechas++
			linea(c, "  %s %s %s", sAcento.Render("↩"), relleno(mig.ID(), 40), sTenue.Render(fmt.Sprintf("%dms", d.Milliseconds())))
		})
		if hechas > 0 {
			reportar(c, p, nombre, e, m, nil, nil)
		}
		if err != nil {
			return fmt.Errorf("se revirtieron %d de %d", hechas, len(aRevertir))
		}
		linea(c, "%s %s", sAcento.Render("✓"), plural(hechas, "migración revertida", "migraciones revertidas"))
		return nil
	},
}

func init() {
	for _, cm := range []*cobra.Command{statusCmd, upCmd, downCmd, driftCmd, esquemaCmd} {
		cm.Flags().StringVarP(&migFlags.env, "env", "e", "", "entorno (dev, staging, prod…)")
	}
	for _, cm := range []*cobra.Command{upCmd, downCmd} {
		cm.Flags().BoolVar(&migFlags.dryRun, "dry-run", false, "muestra el SQL sin ejecutarlo")
		cm.Flags().BoolVar(&migFlags.si, "si", false, "confirma cambios en un entorno de producción")
	}
	upCmd.Flags().StringVar(&migFlags.hasta, "hasta", "", "aplica solo hasta esta versión (ej. 0005)")
	downCmd.Flags().IntVar(&migFlags.pasos, "pasos", 1, "cuántas migraciones revertir")
}

func versionNum(v string) int { n, _ := strconv.Atoi(strings.TrimSpace(v)); return n }

// guardarSnapshotPrevio: para saber qué tablas cambia la primera migración pendiente,
// `push` necesita el esquema que dejó la anterior. Si no lo tenemos (se aplicó antes
// de que existieran los snapshots), el estado actual de la base es justamente ese.
func guardarSnapshotPrevio(ctx context.Context, p *proyecto.Proyecto, m *migrador.Migrador, entorno string, locales []proyecto.Migracion, primera proyecto.Migracion) {
	for i, l := range locales {
		if l.ID() != primera.ID() || i == 0 {
			continue
		}
		previa := locales[i-1].ID()
		var tmp introspeccion.Esquema
		if ok, _ := p.LeerSnapshot(entorno, previa, &tmp); ok {
			return
		}
		if real, err := m.EsquemaReal(ctx); err == nil {
			_ = p.GuardarSnapshot(entorno, previa, real)
		}
		return
	}
}

// mostrarRiesgo imprime los hallazgos del análisis de riesgo. Con todos=false omite los "bajo".
func mostrarRiesgo(c *cobra.Command, id string, hs []riesgo.Hallazgo, todos bool) {
	var visibles []riesgo.Hallazgo
	for _, h := range hs {
		if todos || h.Nivel != "bajo" {
			visibles = append(visibles, h)
		}
	}
	if len(visibles) == 0 {
		return
	}
	linea(c, "%s %s:", sAviso.Render("!"), sNegri.Render(id))
	for _, h := range visibles {
		etiqueta := map[string]string{"alto": sError.Render("ALTO "), "medio": sAviso.Render("MEDIO"), "bajo": sTenue.Render("bajo ")}[h.Nivel]
		donde := ""
		if h.Linea > 0 {
			donde = sTenue.Render(fmt.Sprintf(" (línea %d)", h.Linea))
		}
		linea(c, "    %s %s%s", etiqueta, h.Mensaje, donde)
		if h.Sugerencia != "" {
			linea(c, "          %s", sTenue.Render("→ "+h.Sugerencia))
		}
	}
}

// verificarAprobaciones: si el entorno está protegido en la web, cada migración
// pendiente debe estar en el servidor y aprobada por alguien distinto a su autor.
func verificarAprobaciones(c *cobra.Command, p *proyecto.Proyecto, nombre string, e proyecto.Entorno, pendientes []proyecto.Migracion) error {
	if p.Config.Servidor == "" || p.Config.RepoID == 0 {
		return nil
	}
	cli, cred, err := remoto.Conectado(p.Config.Servidor)
	if err != nil {
		return nil
	}
	est, err := cli.Entorno(p.Config.RepoID, nombreReporte(nombre, e, cred))
	if err != nil {
		linea(c, "%s No se pudo comprobar si %s requiere aprobación: %v", sAviso.Render("!"), nombre, err)
		return nil
	}
	if est == nil || !est.RequiereAprobacion {
		return nil
	}
	hist, err := cli.Historial(p.Config.RepoID, p.Config.Rama)
	if err != nil {
		return fmt.Errorf("%s requiere aprobación y no pude leer el historial del servidor: %w", nombre, err)
	}
	porChecksum := map[string]remoto.Commit{}
	for _, cm := range hist.Commits {
		porChecksum[cm.Checksum] = cm
	}
	var faltan []string
	for _, mig := range pendientes {
		cm, ok := porChecksum[mig.Checksum]
		switch {
		case !ok:
			faltan = append(faltan, mig.ID()+": no está en el servidor (súbela con marga push)")
		case cm.CambiosPedidos > 0 && cm.Aprobaciones == 0:
			faltan = append(faltan, mig.ID()+": le pidieron cambios")
		case cm.Aprobaciones == 0:
			faltan = append(faltan, mig.ID()+": nadie la ha aprobado")
		}
	}
	if len(faltan) > 0 {
		return fmt.Errorf("%s requiere que otra persona apruebe cada migración antes de aplicarla:\n  - %s\nSe aprueban en la web: Commits → Revisiones", nombre, strings.Join(faltan, "\n  - "))
	}
	linea(c, "%s Todas las migraciones pendientes están aprobadas (%s requiere aprobación)", sAcento.Render("✓"), nombre)
	return nil
}
