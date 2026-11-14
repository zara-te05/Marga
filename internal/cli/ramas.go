package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zarat/Motor-Marga/internal/proyecto"
	"github.com/zarat/Motor-Marga/internal/remoto"
)

func relPath(base, f string) (string, error) {
	r, err := filepath.Rel(base, f)
	return filepath.ToSlash(r), err
}

// vinculado devuelve el proyecto y un cliente con sesión, o un error claro.
func vinculado() (*proyecto.Proyecto, *remoto.Cliente, error) {
	p, err := proyectoActual()
	if err != nil {
		return nil, nil, err
	}
	if p.Config.Servidor == "" || p.Config.RepoID == 0 {
		return nil, nil, errors.New("este proyecto no está vinculado a un servidor. Usa `marga link --repo ID`")
	}
	cli, _, err := remoto.Conectado(p.Config.Servidor)
	return p, cli, err
}

var ramaDesde string

var branchCmd = &cobra.Command{
	Use:   "branch [NOMBRE]",
	Short: "Lista las ramas del servidor, o crea una nueva",
	Example: `  marga branch                       lista las ramas
  marga branch feature/facturas      crea una rama desde la actual`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, cli, err := vinculado()
		if err != nil {
			return err
		}
		if len(args) == 1 {
			desde := ramaDesde
			if desde == "" {
				desde = p.Config.Rama
			}
			if err := cli.CrearRama(p.Config.RepoID, args[0], desde); err != nil {
				return err
			}
			linea(c, "%s Rama %s creada desde %s. Cámbiate a ella con: marga checkout %s", sAcento.Render("✓"), sNegri.Render(args[0]), desde, args[0])
			return nil
		}
		ramas, err := cli.Ramas(p.Config.RepoID)
		if err != nil {
			return err
		}
		for _, r := range ramas {
			marca := "  "
			nombre := r.Nombre
			if r.Nombre == p.Config.Rama {
				marca, nombre = sAcento.Render("* "), sNegri.Render(r.Nombre)
			}
			detalle := sTenue.Render("vacía")
			if r.UltimoCommit != nil {
				detalle = sTenue.Render(r.UltimoCommit.HashCorto + " " + r.UltimoCommit.PrimeraLinea())
			}
			linea(c, "%s%s %s", marca, relleno(nombre, 28), detalle)
		}
		return nil
	},
}

var checkoutCmd = &cobra.Command{
	Use:   "checkout RAMA",
	Short: "Cambia la rama del servidor que usan push, pull y log",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, cli, err := vinculado()
		if err != nil {
			return err
		}
		ramas, err := cli.Ramas(p.Config.RepoID)
		if err != nil {
			return err
		}
		existe := false
		for _, r := range ramas {
			existe = existe || r.Nombre == args[0]
		}
		if !existe {
			return fmt.Errorf("la rama %q no existe en el servidor (créala con marga branch %s)", args[0], args[0])
		}
		p.Config.Rama = args[0]
		if err := p.Guardar(); err != nil {
			return err
		}
		linea(c, "%s Ahora push, pull y log usan la rama %s", sAcento.Render("✓"), sNegri.Render(args[0]))
		linea(c, "%s", sTenue.Render("Los archivos de migrations/ los maneja tu git: si los separas por rama, cambia también de rama ahí."))
		return nil
	},
}

var mergeFlags struct {
	hacia   string
	simular bool
	forzar  bool
}

var mergeCmd = &cobra.Command{
	Use:   "merge RAMA",
	Short: "Fusiona otra rama en la actual (en el servidor)",
	Long: `Trae las migraciones de RAMA a la rama actual (o a --hacia).

Como las migraciones se aplican en orden, la historia queda lineal: si la rama
destino también avanzó, las migraciones de RAMA se copian encima y se renumeran
si chocan. Si ambas ramas tocaron la misma tabla, Marga se detiene (conflicto).`,
	Example: `  marga merge feature/facturas --simular
  marga merge feature/facturas`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		p, cli, err := vinculado()
		if err != nil {
			return err
		}
		hacia := mergeFlags.hacia
		if hacia == "" {
			hacia = p.Config.Rama
		}
		res, err := cli.Fusionar(p.Config.RepoID, args[0], hacia, mergeFlags.simular, mergeFlags.forzar)
		if err != nil {
			return err
		}
		desde, destino := sNegri.Render(args[0]), sNegri.Render(hacia)
		lista := func() {
			for _, cm := range res.Nuevos {
				linea(c, "    %s %s", sAcento.Render(cm.HashCorto), cm.PrimeraLinea())
			}
		}
		switch res.Tipo {
		case "al_dia":
			linea(c, "%s %s ya contiene todo lo de %s", sAcento.Render("✓"), destino, desde)
			return nil
		case "conflicto":
			linea(c, "%s %s y %s modificaron las mismas tablas desde que se separaron:", sError.Render("✗"), desde, destino)
			for _, cf := range res.Conflictos {
				linea(c, "    %s", sNegri.Render(cf.Tabla))
				for _, d := range cf.Desde {
					linea(c, "        %s %s", sTenue.Render(args[0]+":"), d)
				}
				for _, h := range cf.Hacia {
					linea(c, "        %s %s", sTenue.Render(hacia+":"), h)
				}
			}
			return errors.New("no se fusionó. Revisa que esas migraciones sean compatibles entre sí; si lo son, repite con --forzar")
		case "fast_forward":
			verbo := "avanzó"
			if !res.Aplicado {
				verbo = "avanzaría"
			}
			linea(c, "%s %s %s hasta %s (fast-forward) con %s:", sAcento.Render("✓"), destino, verbo, desde, plural(len(res.Nuevos), "migración", "migraciones"))
			lista()
		case "copia":
			verbo := "Se copiaron"
			if !res.Aplicado {
				verbo = "Se copiarían"
			}
			linea(c, "%s %s %s de %s encima de %s:", sAcento.Render("✓"), verbo, plural(len(res.Nuevos), "migración", "migraciones"), desde, destino)
			lista()
			for _, r := range res.Renumeradas {
				linea(c, "    %s %s → %s %s", sAcento.Render("↻"), r.Antes, sNegri.Render(r.Despues), sTenue.Render("(su versión ya existía en "+hacia+")"))
			}
			if len(res.Conflictos) > 0 {
				linea(c, "%s Se forzó a pesar de %s", sAviso.Render("!"), plural(len(res.Conflictos), "conflicto", "conflictos"))
			}
		}
		if !res.Aplicado {
			linea(c, "%s", sTenue.Render("(simulación: no se cambió nada)"))
			return nil
		}
		if hacia == p.Config.Rama {
			linea(c, "Siguiente: marga pull  (y luego marga up en tus entornos)")
		}
		return nil
	},
}

func init() {
	branchCmd.Flags().StringVar(&ramaDesde, "desde", "", "rama o commit desde donde crearla (por defecto la actual)")
	mergeCmd.Flags().StringVar(&mergeFlags.hacia, "hacia", "", "rama destino (por defecto la actual)")
	mergeCmd.Flags().BoolVar(&mergeFlags.simular, "simular", false, "muestra qué pasaría sin cambiar nada")
	mergeCmd.Flags().BoolVar(&mergeFlags.forzar, "forzar", false, "fusiona aunque ambas ramas toquen las mismas tablas")
}
