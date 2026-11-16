package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/datos/ramabd"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
)

var verificarFlags struct {
	url             string
	env             string
	seeds           bool
	permitirSinDown bool
	conservar       bool
}

var verificarCmd = &cobra.Command{
	Use:   "verificar",
	Short: "Prueba todas las migraciones en una base desechable (para CI y antes de un PR)",
	Long: `Crea una base vacía y temporal, y en ella:

  1. aplica cada migración desde cero, en orden
  2. revierte cada una (de la última a la primera) y comprueba que el DOWN deja
     la base exactamente como estaba antes de esa migración
  3. las vuelve a aplicar todas
  4. aplica los objetos repetibles (sql/) y, con --seeds, los datos de prueba

Al terminar borra la base. Si algo falla, sale con error: úsalo en CI en cada PR.

La base se crea en el servidor de --url, de $MARGA_VERIFICAR_URL o del entorno
(dev por defecto). El usuario necesita permiso para crear bases (CREATEDB).`,
	Example: `  marga verificar
  marga verificar --seeds
  marga verificar --url "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		locales, err := p.Migraciones()
		if err != nil {
			return err
		}
		reps, err := p.Repetibles()
		if err != nil {
			return err
		}
		var seeds []proyecto.Script
		if verificarFlags.seeds {
			if seeds, err = p.Seeds(); err != nil {
				return err
			}
		}
		servidor, err := servidorParaVerificar(p)
		if err != nil {
			return err
		}
		admin, err := ramabd.AbrirAdmin(ctx, servidor)
		if err != nil {
			return err
		}
		defer admin.Close()

		b := make([]byte, 4)
		_, _ = rand.Read(b)
		nombreBD := "marga_verificar_" + hex.EncodeToString(b)
		if err := admin.CrearVacia(ctx, nombreBD, "(verificar)"); err != nil {
			return sugerencia.Con(err, "dale permiso al usuario (ALTER USER <usuario> CREATEDB;) o usa --url con otro usuario")
		}
		base, _ := ramabd.BaseDeURL(servidor)
		defer func() {
			if verificarFlags.conservar {
				u, _ := ramabd.ConBase(servidor, nombreBD)
				linea(c, "%s", sTenue.Render("La base se conservó: "+proyecto.Entorno{URL: u}.URLOculta()))
				return
			}
			if err := admin.Borrar(context.Background(), nombreBD, base, true); err != nil {
				linea(c, "%s no se pudo borrar la base temporal %s: %v", sAviso.Render("!"), nombreBD, err)
			}
		}()
		u, err := ramabd.ConBase(servidor, nombreBD)
		if err != nil {
			return err
		}
		m, err := migrador.Abrir(ctx, proyecto.Entorno{URL: u, Motor: "postgresql"})
		if err != nil {
			return err
		}
		defer m.Close()

		linea(c, "Verificando %s en una base temporal (%s)…", plural(len(locales), "migración", "migraciones"), nombreBD)
		v := &verificacion{c: c, p: p, m: m, ctx: ctx}
		v.correr(locales, reps, seeds)
		linea(c, "")
		if len(v.fallos) > 0 {
			linea(c, "%s %s:", sError.Render("✗"), plural(len(v.fallos), "problema", "problemas"))
			for _, f := range v.fallos {
				linea(c, "    - %s", f)
			}
			return sugerencia.Nuevo("la verificación falló", "corrige los problemas de arriba en los archivos de migrations/ y repite marga verificar")
		}
		linea(c, "%s Todo bien: las migraciones se aplican desde cero, se revierten limpio y se vuelven a aplicar", sAcento.Render("✓"))
		return nil
	},
}

// servidorParaVerificar: dónde crear la base temporal.
func servidorParaVerificar(p *proyecto.Proyecto) (string, error) {
	if verificarFlags.url != "" {
		return verificarFlags.url, nil
	}
	if u := os.Getenv("MARGA_VERIFICAR_URL"); u != "" {
		return u, nil
	}
	nombre, err := entornoPersonalPorDefecto(p, verificarFlags.env)
	if err != nil {
		return "", sugerencia.Con(err, "marga verificar --url \"postgres://usuario:clave@localhost:5432/postgres\" (un servidor donde pueda crear bases temporales)")
	}
	_, e, err := p.Entorno(nombre)
	if err != nil {
		return "", err
	}
	if e.Motor != "" && e.Motor != "postgresql" {
		return "", sugerencia.Nuevof("marga verificar --url con un servidor de PostgreSQL", "verificar solo funciona con PostgreSQL (%s es %s)", nombre, e.Motor)
	}
	return e.URLReal()
}

type verificacion struct {
	c      *cobra.Command
	p      *proyecto.Proyecto
	m      *migrador.Migrador
	ctx    context.Context
	fallos []string
}

func (v *verificacion) fallo(format string, a ...any) {
	v.fallos = append(v.fallos, fmt.Sprintf(format, a...))
}

func (v *verificacion) paso(titulo string) { linea(v.c, "\n%s", sNegri.Render(titulo)) }

func (v *verificacion) correr(locales []proyecto.Migracion, reps, seeds []proyecto.Script) {
	vacio, err := v.m.EsquemaReal(v.ctx)
	if err != nil {
		v.fallo("no se pudo leer el esquema: %v", err)
		return
	}
	// esquemas[i] = el esquema ANTES de aplicar locales[i]; esquemas[n] = al final.
	esquemas := []esquema.Esquema{vacio}

	v.paso("1. Aplicar desde cero")
	autorV := "marga verificar"
	err = v.m.Subir(v.ctx, locales, autorV, func(mig proyecto.Migracion, d time.Duration, snap *esquema.Esquema, err error) {
		if err != nil {
			linea(v.c, "  %s %s %s", sError.Render("✗"), mig.ID(), sError.Render(err.Error()))
			return
		}
		esquemas = append(esquemas, *snap)
		linea(v.c, "  %s %s %s", sAcento.Render("✓"), relleno(mig.ID(), 40), sTenue.Render(fmt.Sprintf("%dms", d.Milliseconds())))
	})
	if err != nil {
		v.fallo("no se aplica desde cero: %v", err)
		return // sin las migraciones lo demás no tiene sentido
	}
	final := esquemas[len(esquemas)-1]

	v.paso("2. Revertir una por una")
	desde := len(locales) // primera migración que quedó revertida
	for i := len(locales) - 1; i >= 0; i-- {
		mig := locales[i]
		if mig.Down == "" {
			if verificarFlags.permitirSinDown {
				linea(v.c, "  %s %s %s", sTenue.Render("·"), relleno(mig.ID(), 40), sTenue.Render("sin DOWN (permitido): aquí se detiene la reversión"))
			} else {
				linea(v.c, "  %s %s %s", sError.Render("✗"), relleno(mig.ID(), 40), sError.Render("no tiene DOWN"))
				v.fallo("%s no tiene DOWN: no se puede revertir (si es a propósito, usa --permitir-sin-down)", mig.ID())
			}
			break
		}
		var despues *esquema.Esquema
		err := v.m.Bajar(v.ctx, []proyecto.Migracion{mig}, func(_ proyecto.Migracion, _ time.Duration, snap *esquema.Esquema, err error) {
			despues = snap
		})
		if err != nil {
			linea(v.c, "  %s %s %s", sError.Render("✗"), relleno(mig.ID(), 40), sError.Render(err.Error()))
			v.fallo("el DOWN de %s falla: %v", mig.ID(), err)
			break
		}
		desde = i
		if dif := esquema.Comparar(esquemas[i], *despues); len(dif) > 0 {
			linea(v.c, "  %s %s %s", sError.Render("✗"), relleno(mig.ID(), 40), sError.Render("el DOWN no deja la base como estaba:"))
			for _, d := range dif {
				linea(v.c, "        %s", d)
			}
			v.fallo("el DOWN de %s no deshace todo (%s)", mig.ID(), resumirLista(dif, 2))
			continue
		}
		linea(v.c, "  %s %s %s", sAcento.Render("↩"), relleno(mig.ID(), 40), sTenue.Render("vuelve exactamente al estado anterior"))
	}

	v.paso("3. Aplicar otra vez")
	if desde < len(locales) {
		err := v.m.Subir(v.ctx, locales[desde:], autorV, func(mig proyecto.Migracion, d time.Duration, _ *esquema.Esquema, err error) {
			if err != nil {
				linea(v.c, "  %s %s %s", sError.Render("✗"), mig.ID(), sError.Render(err.Error()))
				return
			}
			linea(v.c, "  %s %s", sAcento.Render("✓"), mig.ID())
		})
		if err != nil {
			v.fallo("no se vuelve a aplicar después de revertir: %v", err)
			return
		}
	} else {
		linea(v.c, "  %s", sTenue.Render("nada que volver a aplicar"))
	}
	if real, err := v.m.EsquemaReal(v.ctx); err == nil {
		if dif := esquema.Comparar(final, real); len(dif) > 0 {
			v.fallo("después de revertir y volver a aplicar, el esquema no es el mismo (%s)", resumirLista(dif, 2))
		}
	}

	if len(reps) > 0 {
		v.paso("4. Objetos repetibles")
		err := v.m.AplicarScripts(v.ctx, migrador.TipoRepetible, reps, autorV, true, func(s proyecto.Script, d time.Duration, err error) {
			if err != nil {
				linea(v.c, "  %s %s %s", sError.Render("✗"), s.Archivo, sError.Render(err.Error()))
				return
			}
			linea(v.c, "  %s %s", sAcento.Render("✓"), s.Archivo)
		})
		if err != nil {
			v.fallo("objeto repetible: %v", err)
		} else {
			// Deben poder aplicarse otra vez sin error (CREATE OR REPLACE…).
			if err := v.m.OlvidarScripts(v.ctx, migrador.TipoRepetible, archivosDe(reps)); err == nil {
				if err := v.m.AplicarScripts(v.ctx, migrador.TipoRepetible, reps, autorV, true, nil); err != nil {
					v.fallo("un objeto repetible no se puede aplicar dos veces (usa CREATE OR REPLACE): %v", err)
				}
			}
		}
	}
	if len(seeds) > 0 {
		v.paso("5. Datos de prueba")
		err := v.m.AplicarScripts(v.ctx, migrador.TipoSeed, seeds, autorV, false, func(s proyecto.Script, d time.Duration, err error) {
			if err != nil {
				linea(v.c, "  %s %s %s", sError.Render("✗"), s.Archivo, sError.Render(err.Error()))
				return
			}
			linea(v.c, "  %s %s", sAcento.Render("✓"), s.Archivo)
		})
		if err != nil {
			v.fallo("seed: %v", err)
		}
	}
}

func archivosDe(ss []proyecto.Script) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Archivo
	}
	return out
}

func init() {
	verificarCmd.Flags().StringVar(&verificarFlags.url, "url", "", "servidor de PostgreSQL donde crear la base temporal")
	verificarCmd.Flags().StringVarP(&verificarFlags.env, "env", "e", "", "usa el servidor de este entorno (por defecto dev)")
	verificarCmd.Flags().BoolVar(&verificarFlags.seeds, "seeds", false, "también carga los datos de prueba (seeds/)")
	verificarCmd.Flags().BoolVar(&verificarFlags.permitirSinDown, "permitir-sin-down", false, "no falla si una migración no tiene DOWN")
	verificarCmd.Flags().BoolVar(&verificarFlags.conservar, "conservar", false, "no borra la base temporal al terminar")
}
