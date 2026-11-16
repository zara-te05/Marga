package cli

import (
	"bufio"
	"context"
	"fmt"
	"github.com/zara-te05/Marga/internal/vistas/terminal"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/introspeccion"
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
)

// ---------------------------------------------------------------- Objetos repetibles (sql/)

func relRepetible(p *proyecto.Proyecto, archivo string) string {
	return relativo(p, filepath.Join(p.DirRepetibles(), filepath.FromSlash(archivo)))
}

// repetiblesPendientes: los archivos de sql/ nuevos o cambiados, y los que se
// aplicaron pero ya no existen.
func repetiblesPendientes(ctx context.Context, p *proyecto.Proyecto, m *migrador.Migrador) ([]migrador.FilaScript, []string, error) {
	locales, err := p.Repetibles()
	if err != nil {
		return nil, nil, err
	}
	aplicados, err := m.Scripts(ctx, migrador.TipoRepetible)
	if err != nil {
		return nil, nil, err
	}
	filas, borrados := migrador.CompararScripts(locales, aplicados)
	var out []migrador.FilaScript
	for _, f := range filas {
		if f.Estado != migrador.ScriptAlDia {
			out = append(out, f)
		}
	}
	return out, borrados, nil
}

func aplicarRepetibles(ctx context.Context, c *cobra.Command, p *proyecto.Proyecto, m *migrador.Migrador, nombre string, reps []migrador.FilaScript) error {
	if len(reps) == 0 {
		return nil
	}
	scripts := make([]proyecto.Script, len(reps))
	estado := map[string]migrador.EstadoScript{}
	for i, f := range reps {
		scripts[i] = f.Script
		estado[f.Script.Archivo] = f.Estado
	}
	linea(c, "Aplicando %s en %s…", plural(len(scripts), "objeto repetible", "objetos repetibles"), sNegri.Render(nombre))
	hechos := 0
	err := m.AplicarScripts(ctx, migrador.TipoRepetible, scripts, autor(p), true, func(s proyecto.Script, d time.Duration, err error) {
		if err != nil {
			linea(c, "  %s %s %s", sError.Render("✗"), relRepetible(p, s.Archivo), sError.Render(err.Error()))
			return
		}
		hechos++
		que := "nuevo"
		if estado[s.Archivo] == migrador.ScriptCambiado {
			que = "cambió"
		}
		linea(c, "  %s %s %s", sAcento.Render("↻"), relleno(relRepetible(p, s.Archivo), 40), sTenue.Render(fmt.Sprintf("%s · %dms", que, d.Milliseconds())))
	})
	if err != nil {
		return fmt.Errorf("se aplicaron %d de %d objetos repetibles; el que falló se deshizo. Corrige el archivo y repite marga up", hechos, len(scripts))
	}
	linea(c, "%s %s", sAcento.Render("✓"), plural(hechos, "objeto repetible aplicado", "objetos repetibles aplicados"))
	return nil
}

// olvidarRepetibles: el archivo se borró. Marga no sabe deshacer el objeto (no hay
// DOWN), así que avisa y deja de seguirlo.
func olvidarRepetibles(ctx context.Context, c *cobra.Command, m *migrador.Migrador, borrados []string) {
	for _, b := range borrados {
		linea(c, "%s Se borró %s: el objeto sigue en la base. Si ya no lo quieres, quítalo con una migración (p. ej. DROP VIEW …).",
			sAviso.Render("!"), b)
	}
	if err := m.OlvidarScripts(ctx, migrador.TipoRepetible, borrados); err != nil {
		linea(c, "%s %v", sAviso.Render("!"), err)
	}
}

// mostrarRepetibles agrega a `status` el estado de sql/ (si la carpeta tiene algo).
func mostrarRepetibles(ctx context.Context, c *cobra.Command, p *proyecto.Proyecto, m *migrador.Migrador) error {
	locales, err := p.Repetibles()
	if err != nil {
		return err
	}
	aplicados, err := m.Scripts(ctx, migrador.TipoRepetible)
	if err != nil {
		return err
	}
	filas, borrados := migrador.CompararScripts(locales, aplicados)
	if len(filas) == 0 && len(borrados) == 0 {
		return nil
	}
	alDia := 0
	linea(c, "Objetos repetibles (%s/):", filepath.ToSlash(relativo(p, p.DirRepetibles())))
	for _, f := range filas {
		id := relleno(f.Script.Archivo, 40)
		switch f.Estado {
		case migrador.ScriptAlDia:
			alDia++
			linea(c, "  %s %s %s", sAcento.Render("✓"), id, sTenue.Render("aplicado "+f.Registro.AplicadoEn.Local().Format("2006-01-02 15:04")))
		case migrador.ScriptCambiado:
			linea(c, "  %s %s %s", sAviso.Render("↻"), id, "cambió: marga up lo vuelve a aplicar")
		case migrador.ScriptNuevo:
			linea(c, "  %s %s %s", sTenue.Render("·"), id, "nuevo: marga up lo aplica")
		}
	}
	for _, b := range borrados {
		linea(c, "  %s %s %s", sError.Render("?"), relleno(b, 40), sError.Render("aplicado pero ya no existe el archivo"))
	}
	if n := len(filas) - alDia; n > 0 {
		linea(c, "%s", plural(n, "objeto repetible por aplicar", "objetos repetibles por aplicar"))
	}
	return nil
}

// ---------------------------------------------------------------- marga seed

// elegirEntornoPersonal: sin --env, seed y sql usan dev (el único entorno personal).
func elegirEntornoPersonal() error {
	if migFlags.env != "" {
		return nil
	}
	p, err := proyectoActual()
	if err != nil {
		return err
	}
	n, err := entornoPersonalPorDefecto(p, "")
	if err != nil {
		return err
	}
	migFlags.env = n
	return nil
}

var seedFlags struct {
	todo bool
}

var seedCmd = &cobra.Command{
	Use:   "seed [ARCHIVO...]",
	Short: "Llena tu base de desarrollo con datos de prueba (carpeta seeds/)",
	Long: `Ejecuta los archivos de seeds/ (en orden alfabético) en tu base personal: la de
tu rama de git en dev/local. Nunca en entornos compartidos (staging, prod).

Cada seed se ejecuta una sola vez por base. Si lo editas, no se repite solo
(insertaría datos duplicados): usa marga ramas reiniciar y marga seed, o --todo.`,
	Example: `  marga seed                       los seeds que esta base aún no tiene
  marga seed 0002_pedidos.sql      solo ese archivo (aunque ya se haya ejecutado)
  marga seed --todo                todos otra vez`,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		if err := elegirEntornoPersonal(); err != nil {
			return err
		}
		p, locales, nombre, e, m, err := conectar(ctx, c)
		if err != nil {
			return err
		}
		defer m.Close()
		if !sigueRamas(nombre, e) {
			return fmt.Errorf("%s es un entorno compartido: los datos de prueba solo van en entornos personales (dev, local)", nombre)
		}
		seeds, err := p.Seeds()
		if err != nil {
			return err
		}
		if len(seeds) == 0 {
			linea(c, "No hay seeds. Crea uno con: marga new --seed clientes de prueba")
			return nil
		}
		aplicadas, err := m.Historial(ctx)
		if err != nil {
			return err
		}
		for _, f := range migrador.Comparar(locales, aplicadas) {
			if f.Situacion == migrador.Pendiente || f.Situacion == migrador.Renumerada {
				return fmt.Errorf("%s tiene migraciones sin aplicar (%s): los seeds esperan el esquema Completo. Primero: marga up --env %s", nombre, f.ID(), nombre)
			}
		}
		hechos, err := m.Scripts(ctx, migrador.TipoSeed)
		if err != nil {
			return err
		}
		filas, _ := migrador.CompararScripts(seeds, hechos)
		pedidos := map[string]bool{}
		for _, a := range args {
			pedidos[strings.TrimPrefix(filepath.ToSlash(a), filepath.ToSlash(relativo(p, p.DirSeeds()))+"/")] = true
		}
		var correr []proyecto.Script
		var cambiados []string
		for _, f := range filas {
			switch {
			case len(pedidos) > 0:
				if pedidos[f.Script.Archivo] {
					correr = append(correr, f.Script)
					delete(pedidos, f.Script.Archivo)
				}
			case seedFlags.todo || f.Estado == migrador.ScriptNuevo:
				correr = append(correr, f.Script)
			case f.Estado == migrador.ScriptCambiado:
				cambiados = append(cambiados, f.Script.Archivo)
			}
		}
		for a := range pedidos {
			return fmt.Errorf("no existe el seed %s en %s", a, relativo(p, p.DirSeeds()))
		}
		for _, a := range cambiados {
			linea(c, "%s %s cambió desde que se ejecutó; no se repite para no duplicar datos (marga seed %s para forzarlo)", sAviso.Render("!"), a, a)
		}
		if len(correr) == 0 {
			linea(c, "%s %s ya tiene todos los datos de prueba", sAcento.Render("✓"), sNegri.Render(baseMostrada(e, nombre)))
			return nil
		}
		linea(c, "Cargando %s en %s…", plural(len(correr), "seed", "seeds"), sNegri.Render(baseMostrada(e, nombre)))
		n := 0
		err = m.AplicarScripts(ctx, migrador.TipoSeed, correr, autor(p), false, func(s proyecto.Script, d time.Duration, err error) {
			if err != nil {
				linea(c, "  %s %s %s", sError.Render("✗"), s.Archivo, sError.Render(err.Error()))
				return
			}
			n++
			linea(c, "  %s %s %s", sAcento.Render("✓"), relleno(s.Archivo, 40), sTenue.Render(fmt.Sprintf("%dms", d.Milliseconds())))
		})
		if err != nil {
			return fmt.Errorf("se cargaron %d de %d; el que falló se deshizo", n, len(correr))
		}
		linea(c, "%s %s", sAcento.Render("✓"), plural(n, "seed cargado", "seeds cargados"))
		return nil
	},
}

// ---------------------------------------------------------------- marga sql

var sqlFlags struct {
	comando string
	guardar bool
	limite  int
}

var sqlCmd = &cobra.Command{
	Use:   "sql [ARCHIVO]",
	Short: "Prueba SQL en tu base sin compromiso (al terminar se deshace)",
	Long: `Ejecuta SQL en la base del entorno (en dev, la de tu rama) y muestra el resultado.
Todo corre en una transacción que al final se DESHACE: puedes probar un ALTER, un
INSERT o un DELETE y ver qué pasaría sin cambiar nada.

Con --guardar los cambios se quedan. Si cambiaste tablas, conviértelos en una
migración con marga commit (si no, aparecen como drift).

Sin archivo ni -c abre una Consola: escribe SQL terminado en ; y \q para salir.
En la Consola: ↑ ↓ recorren el historial (se guarda entre sesiones), Tab completa
tablas, columnas y palabras clave, \d lista los objetos, \d NOMBRE muestra su DDL,
\e abre la última sentencia en tu editor y \? muestra la ayuda.`,
	Example: `  marga sql -c "SELECT * FROM clientes LIMIT 5"
  marga sql prueba.sql
  marga sql -c "ALTER TABLE clientes ADD COLUMN rfc text" --guardar
  marga sql                          Consola`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		if err := elegirEntornoPersonal(); err != nil {
			return err
		}
		_, _, nombre, e, m, err := conectar(ctx, c)
		if err != nil {
			return err
		}
		defer m.Close()
		if !sigueRamas(nombre, e) && sqlFlags.guardar && !migFlags.si {
			return fmt.Errorf("%s es compartido: para guardar cambios ahí confirma con --si (mejor usa una migración)", nombre)
		}
		donde := baseMostrada(e, nombre)
		correr := func(texto string) error {
			res, err := m.Consultar(ctx, texto, sqlFlags.guardar, sqlFlags.limite)
			if err != nil {
				return err
			}
			for i, r := range res {
				if i > 0 {
					linea(c, "")
				}
				imprimirTabla(c, r, sqlFlags.limite)
			}
			if sqlFlags.guardar {
				linea(c, "%s %s", sAcento.Render("✓"), "Cambios guardados en "+donde)
			} else if len(res) == 0 || !soloConsultas(texto) {
				linea(c, "%s", sTenue.Render("↩ se deshizo: "+donde+" no cambió (usa --guardar para conservar)"))
			}
			return nil
		}
		switch {
		case sqlFlags.comando != "":
			return correr(sqlFlags.comando)
		case len(args) == 1:
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			return correr(proyecto.Normalizar(string(b)))
		}
		modo := "se deshace al terminar cada sentencia"
		if sqlFlags.guardar {
			modo = "los cambios SE GUARDAN"
		}
		if !term.IsTerminal(os.Stdin.Fd()) {
			// Entrada redirigida (un archivo o una tubería): sin editor, línea por línea.
			return Consola(c, os.Stdin, correr)
		}
		linea(c, "SQL en %s (%s). Termina con ; · \\? ayuda · \\q para salir", sNegri.Render(donde), modo)
		return consolaInteractiva(c, func() (introspeccion.Esquema, error) { return m.EsquemaReal(ctx) }, correr)
	},
}

// Consola lee sentencias terminadas en ; y las ejecuta una por una.
func Consola(c *cobra.Command, in io.Reader, correr func(string) error) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var buf strings.Builder
	prompt := func() {
		if buf.Len() == 0 {
			fmt.Fprint(out(c), "sql> ")
		} else {
			fmt.Fprint(out(c), "...> ")
		}
	}
	prompt()
	for sc.Scan() {
		l := sc.Text()
		if buf.Len() == 0 && (strings.TrimSpace(l) == `\q` || strings.TrimSpace(l) == "salir") {
			return nil
		}
		buf.WriteString(l + "\n")
		if strings.HasSuffix(strings.TrimSpace(l), ";") {
			if err := correr(buf.String()); err != nil {
				linea(c, "%s %v", sError.Render("✗"), err)
			}
			buf.Reset()
		}
		prompt()
	}
	fmt.Fprintln(out(c))
	return sc.Err()
}

// soloConsultas: el SQL solo tiene SELECT/WITH/SHOW/EXPLAIN (no hay nada que deshacer).
func soloConsultas(texto string) bool {
	for _, s := range strings.Split(texto, ";") {
		var palabra string
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "--") {
				palabra = strings.ToUpper(strings.Fields(l)[0])
				break
			}
		}
		switch palabra {
		case "", "SELECT", "WITH", "SHOW", "EXPLAIN", "TABLE", "VALUES":
		default:
			return false
		}
	}
	return true
}

func imprimirTabla(c *cobra.Command, r migrador.Resultado, limite int) {
	const maxAncho = 40
	corta := func(s string) string {
		s = strings.ReplaceAll(s, "\n", "↵")
		if utf8.RuneCountInString(s) > maxAncho {
			return string([]rune(s)[:maxAncho-1]) + "…"
		}
		return s
	}
	ancho := make([]int, len(r.Columnas))
	for i, col := range r.Columnas {
		ancho[i] = utf8.RuneCountInString(corta(col))
	}
	for fi, f := range r.Filas {
		for i, v := range f {
			if r.Nulos[fi][i] {
				v = "null"
			}
			ancho[i] = max(ancho[i], utf8.RuneCountInString(corta(v)))
		}
	}
	celdas := make([]string, len(r.Columnas))
	for i, col := range r.Columnas {
		celdas[i] = relleno(sNegri.Render(corta(col)), ancho[i])
	}
	linea(c, " %s", strings.Join(celdas, " │ "))
	seps := make([]string, len(ancho))
	for i, a := range ancho {
		seps[i] = strings.Repeat("─", a)
	}
	linea(c, "%s", sTenue.Render("─"+strings.Join(seps, "─┼─")+"─"))
	for fi, f := range r.Filas {
		for i, v := range f {
			if r.Nulos[fi][i] {
				celdas[i] = relleno(sTenue.Render("null"), ancho[i])
			} else {
				celdas[i] = relleno(corta(v), ancho[i])
			}
		}
		linea(c, " %s", strings.Join(celdas, " │ "))
	}
	pie := plural(r.Total, "fila", "filas")
	if r.Total > limite {
		pie += fmt.Sprintf(" (se muestran %d; --limite para ver más)", limite)
	}
	linea(c, "%s", sTenue.Render(pie))
}

func init() {
	seedCmd.Flags().StringVarP(&migFlags.env, "env", "e", "", "entorno personal (por defecto dev)")
	seedCmd.Flags().BoolVar(&seedFlags.todo, "todo", false, "ejecuta otra vez todos los seeds")
	sqlCmd.Flags().StringVarP(&migFlags.env, "env", "e", "", "entorno (dev, staging…)")
	sqlCmd.Flags().StringVarP(&sqlFlags.comando, "comando", "c", "", "SQL a ejecutar")
	sqlCmd.Flags().BoolVar(&sqlFlags.guardar, "guardar", false, "conserva los cambios (por defecto se deshacen)")
	sqlCmd.Flags().BoolVar(&migFlags.si, "si", false, "confirma --guardar en un entorno compartido")
	sqlCmd.Flags().IntVar(&sqlFlags.limite, "limite", 50, "filas a mostrar por resultado")
	sqlCmd.Flags().StringVar(&terminal.Bandera, "tema", "", "tema de colores: marga, noche, bosque, atardecer, papel o mono")
}
