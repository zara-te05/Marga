package cli

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/motor"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/datos/ramabd"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
	"github.com/zara-te05/Marga/internal/modelos/riesgo"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
)

var adoptarFlags struct {
	forzar  bool
	simular bool
}

var adoptarCmd = &cobra.Command{
	Use:   "adoptar [DESCRIPCIÓN]",
	Short: "Empieza a usar Marga en una base que ya tiene tablas",
	Long: `Para proyectos que ya tenían base de datos antes de Marga. Lee el esquema actual
y escribe la migración 0001 que lo crea desde cero, para que quien clone el proyecto
pueda reconstruir la base con marga up. En esta base la marca como aplicada.

Antes de escribir nada lo comprueba en una base temporal del mismo servidor: aplica
la 0001 y compara el resultado con la base original; luego aplica el DOWN y revisa
que quede vacía. Si algo no sale igual, lo dice y se detiene (--forzar escribe de
todos modos).

Incluye lo que el esquema necesita aunque Marga no lo siga como objeto: los
esquemas distintos de public, las extensiones (CREATE EXTENSION IF NOT EXISTS) y
las secuencias sueltas. El usuario de la conexión necesita permiso CREATEDB.`,
	Example: `  marga adoptar
  marga adoptar --env dev "esquema de la API de ventas"
  marga adoptar --simular`,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, locales, nombre, e, m, err := conectar(ctx, c)
		if err != nil {
			return err
		}
		defer m.Close()
		desc := strings.TrimSpace(strings.Join(args, " "))
		if desc == "" {
			desc = "esquema inicial"
		}
		nombreMig := proyecto.Slug(desc)
		if nombreMig == "" {
			return sugerencia.Nuevo("la descripción debe tener letras o números", `marga adoptar "esquema inicial"`)
		}

		// Solo tiene sentido al empezar.
		if len(locales) > 0 {
			return sugerencia.Nuevof("marga commit <descripción> guarda como migración los cambios que hiciste en la base",
				"el proyecto ya tiene %s: adoptar es solo para empezar con una base existente", plural(len(locales), "migración", "migraciones"))
		}
		aplicadas, err := m.Historial(ctx)
		if err != nil {
			return err
		}
		if len(aplicadas) > 0 {
			return sugerencia.Nuevof("marga status --env "+nombre+" para ver lo aplicado", "%s ya tiene migraciones aplicadas por Marga; adoptar es para una base que nunca usó Marga", nombre)
		}
		real, err := m.EsquemaReal(ctx)
		if err != nil {
			return err
		}
		if vacio(real) {
			return sugerencia.Nuevof(`marga new "crear tabla clientes" (o crea tus tablas en la base y luego marga commit)`, "%s está vacía: no hay nada que adoptar", nombre)
		}
		mot := m.Motor()
		var prep, quitar []string
		if cp, ok := mot.(motor.ConPreparacion); ok {
			if prep, quitar, err = cp.Preparacion(ctx, m.DB()); err != nil {
				return fmt.Errorf("no se pudo leer lo que necesita el esquema: %w", err)
			}
		}
		nada := esquema.Esquema{Version: esquema.VersionActual, Motor: real.Motor}
		gen := mot.Generar(nada, real)
		if len(prep) > 0 {
			gen.Up = strings.Join(prep, "\n") + "\n\n" + gen.Up
		}
		if len(quitar) > 0 {
			gen.Down = strings.TrimRight(gen.Down, "\n") + "\n\n" + strings.Join(quitar, "\n") + "\n"
		}

		donde := e.Base
		if u, err := e.URLReal(); err == nil && donde == "" {
			donde, _ = ramabd.BaseDeURL(u)
		}
		linea(c, "Adoptando %s (base %s): %s", sNegri.Render(nombre), donde, resumenEsquema(real))
		for _, s := range prep {
			linea(c, "  %s %s", sTenue.Render("+"), s)
		}

		// Comprobación de ida y vuelta en una base temporal.
		linea(c, "Comprobando en una base temporal…")
		problemas, err := comprobarAdopcion(ctx, e, mot, gen, real)
		if err != nil {
			return err
		}
		if len(problemas) == 0 {
			linea(c, "  %s la 0001 reconstruye el esquema igual", sAcento.Render("✓"))
			linea(c, "  %s el DOWN deja la base vacía", sAcento.Render("✓"))
		} else {
			linea(c, "%s La copia no salió igual que %s:", sAviso.Render("!"), nombre)
			for _, pr := range problemas {
				linea(c, "    %s", pr)
			}
			if !adoptarFlags.forzar {
				return sugerencia.Nuevo("no se escribió nada porque la copia no salió igual", "revisa las diferencias de arriba; si las aceptas, marga adoptar --forzar y corrige la 0001 a mano")
			}
		}
		for _, a := range gen.Avisos {
			linea(c, "%s %s", sAviso.Render("!"), a)
		}

		version, err := p.SiguienteVersion()
		if err != nil {
			return err
		}
		id := version + "_" + nombreMig
		up := fmt.Sprintf("-- Migración %s: %s\n-- Generada por `marga adoptar` a partir de %s: crea desde cero el esquema que ya existía.\n\n%s", version, desc, nombre, gen.Up)
		down := fmt.Sprintf("-- Reversión de %s: %s (borra todo el esquema)\n\n%s", version, desc, gen.Down)
		mostrarRiesgo(c, id, riesgo.Analizar(up, down), false)
		if adoptarFlags.simular {
			linea(c, "%s", sTenue.Render("(simulación: no se escribió ni registró nada)"))
			return nil
		}

		mig := proyecto.Migracion{Version: version, Nombre: nombreMig, Up: proyecto.Normalizar(up), Down: proyecto.Normalizar(down)}
		mig.Checksum = proyecto.Checksum(mig.Up)
		snap, err := m.Registrar(ctx, mig, autor(p))
		if err != nil {
			return fmt.Errorf("no se pudo registrar la migración en %s: %w", nombre, err)
		}
		fu, fd, err := p.EscribirMigracion(version, nombreMig, mig.Up, mig.Down)
		if err != nil {
			return fmt.Errorf("la migración quedó registrada en %s pero no se pudieron escribir los archivos: %w", nombre, err)
		}
		if snap != nil {
			_ = p.GuardarSnapshot(e.ClaveSnapshots(nombre), id, snap)
		}
		linea(c, "%s Migración %s creada y marcada como aplicada en %s (el esquema ya estaba ahí)", sAcento.Render("✓"), sNegri.Render(id), nombre)
		linea(c, "  %s", relativo(p, fu))
		linea(c, "  %s", relativo(p, fd))
		linea(c, "Desde ahora: cambia la base y usa marga commit. Quien clone el proyecto la reconstruye con marga up.")
		reportar(c, p, nombre, e, m, snap, nil)
		return nil
	},
}

// sugerirAdoptar: la base ya tenía esquema y el proyecto no tiene migraciones.
// Sin una 0001 que lo cree, nadie podría reconstruir esa base desde cero.
func sugerirAdoptar(c *cobra.Command, locales []proyecto.Migracion, real esquema.Esquema) {
	if len(locales) > 0 || vacio(real) {
		return
	}
	linea(c, "%s Esta base ya tiene %s y el proyecto no tiene migraciones. Para que el equipo pueda reconstruirla desde cero: %s",
		sAviso.Render("!"), resumenEsquema(real), sNegri.Render("marga adoptar"))
}

func vacio(e esquema.Esquema) bool {
	return len(e.Tablas)+len(e.Tipos)+len(e.Funciones)+len(e.Vistas)+len(e.Triggers) == 0
}

// resumenEsquema: "4 tablas · 1 tipo · 2 funciones · 3 vistas · 1 trigger".
func resumenEsquema(e esquema.Esquema) string {
	var partes []string
	for _, x := range []struct {
		n        int
		uno, mas string
	}{{len(e.Tablas), "tabla", "tablas"}, {len(e.Tipos), "tipo", "tipos"}, {len(e.Funciones), "función", "funciones"},
		{len(e.Vistas), "vista", "vistas"}, {len(e.Triggers), "trigger", "triggers"}} {
		if x.n > 0 {
			partes = append(partes, plural(x.n, x.uno, x.mas))
		}
	}
	return strings.Join(partes, " · ")
}

// comprobarAdopcion crea una base temporal en el mismo servidor, aplica el UP y
// compara con el original; luego aplica el DOWN y revisa que quede vacía.
// Devuelve las diferencias (vacío si todo salió igual) y borra la base al final.
func comprobarAdopcion(ctx context.Context, e proyecto.Entorno, mot motor.Motor, gen esquema.Cambios, original esquema.Esquema) ([]string, error) {
	url, err := e.URLReal()
	if err != nil {
		return nil, err
	}
	admin, err := ramabd.AbrirAdmin(ctx, url)
	if err != nil {
		return nil, err
	}
	defer admin.Close()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	nombreBD := "marga_adoptar_" + hex.EncodeToString(b)
	if err := admin.CrearVacia(ctx, nombreBD, "(adoptar)"); err != nil {
		return nil, fmt.Errorf("no se pudo crear la base temporal para comprobar: %w (el usuario necesita permiso CREATEDB)", err)
	}
	origen, _ := ramabd.BaseDeURL(url)
	defer admin.Borrar(context.Background(), nombreBD, origen, true)

	u, err := ramabd.ConBase(url, nombreBD)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(mot.Driver(), u)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, gen.Up); err != nil {
		return []string{"el UP falla en una base vacía: " + err.Error()}, nil
	}
	copia, err := mot.Leer(ctx, db)
	if err != nil {
		return nil, err
	}
	var problemas []string
	for _, d := range esquema.Comparar(original, copia) {
		problemas = append(problemas, "la copia difiere: "+d)
	}
	if _, err := db.ExecContext(ctx, gen.Down); err != nil {
		return append(problemas, "el DOWN falla: "+err.Error()), nil
	}
	despues, err := mot.Leer(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, d := range esquema.Comparar(esquema.Esquema{Version: esquema.VersionActual}, despues) {
		problemas = append(problemas, "el DOWN no lo quitó: "+d)
	}
	return problemas, nil
}

func init() {
	adoptarCmd.Flags().StringVarP(&migFlags.env, "env", "e", "", "entorno a adoptar (por defecto el personal: dev)")
	adoptarCmd.Flags().BoolVar(&adoptarFlags.forzar, "forzar", false, "escribe la migración aunque la comprobación encuentre diferencias")
	adoptarCmd.Flags().BoolVar(&adoptarFlags.simular, "simular", false, "comprueba y muestra el SQL sin escribir ni registrar nada")
}
