package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/proyecto"
)

var urlFlags struct {
	env      string
	variable string
	escribir string
}

var reNombreVar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var urlCmd = &cobra.Command{
	Use:   "url",
	Short: "Imprime la cadena de conexión de la base que corresponde a tu rama de git",
	Long: `Imprime la cadena de conexión del entorno; en dev/local, la de la base de tu
rama actual (la crea si aún no existe). Úsala para que tu app también cambie de
base al cambiar de rama.

Solo la URL va a la salida estándar; los avisos van a la de errores, así que
funciona dentro de $(...).`,
	Example: `  marga url                                    postgres://…/tienda__feature_facturas?…
  DATABASE_URL=$(marga url) npm run dev        bash / zsh
  $env:DATABASE_URL = marga url; dotnet run    PowerShell
  marga url --escribir .env                    actualiza DATABASE_URL=… en .env
  marga url --escribir .env --variable PG_URL`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		nombre, err := entornoPersonalPorDefecto(p, urlFlags.env)
		if err != nil {
			return err
		}
		_, e, err := p.Entorno(nombre)
		if err != nil {
			return err
		}
		locales, err := p.Migraciones()
		if err != nil {
			return err
		}
		// Los avisos (p. ej. "base nueva…") van a stderr para no ensuciar $(marga url).
		avisos := &cobra.Command{}
		avisos.SetOut(c.ErrOrStderr())
		if e, err = resolverRama(ctx, avisos, p, nombre, e, locales); err != nil {
			return err
		}
		url, err := e.URLReal()
		if err != nil {
			return err
		}
		if urlFlags.escribir == "" {
			if urlFlags.variable != "" && c.Flags().Changed("variable") {
				fmt.Fprintf(out(c), "%s=%s\n", urlFlags.variable, url)
			} else {
				fmt.Fprintln(out(c), url)
			}
			return nil
		}
		if !reNombreVar.MatchString(urlFlags.variable) {
			return fmt.Errorf("nombre de variable inválido: %q", urlFlags.variable)
		}
		cambio, err := escribirVariable(urlFlags.escribir, urlFlags.variable, url)
		if err != nil {
			return err
		}
		donde := urlFlags.variable + " en " + urlFlags.escribir
		if !cambio {
			fmt.Fprintf(c.ErrOrStderr(), "%s %s ya apunta a %s\n", sAcento.Render("✓"), donde, sNegri.Render(baseMostrada(e, e.URLOculta())))
			return nil
		}
		fmt.Fprintf(c.ErrOrStderr(), "%s %s ahora apunta a %s %s\n", sAcento.Render("✓"), donde, sNegri.Render(baseMostrada(e, e.URLOculta())),
			sTenue.Render("(reinicia tu app para que la use)"))
		return nil
	},
}

// baseMostrada: el nombre de la base de la rama, o `alterno` si no sigue ramas.
func baseMostrada(e proyecto.Entorno, alterno string) string {
	if e.Base != "" {
		return e.Base
	}
	return alterno
}

// entornoPersonalPorDefecto: `pedido`, o el único entorno, o el único entorno personal (dev).
func entornoPersonalPorDefecto(p *proyecto.Proyecto, pedido string) (string, error) {
	if pedido != "" {
		return pedido, nil
	}
	m, err := p.Entornos()
	if err != nil {
		return "", err
	}
	if len(m) == 1 {
		for n := range m {
			return n, nil
		}
	}
	var personales []string
	for _, n := range proyecto.NombresEntornos(m) {
		if sigueRamas(n, m[n]) {
			personales = append(personales, n)
		}
	}
	if len(personales) == 1 {
		return personales[0], nil
	}
	if len(m) == 0 {
		return "", errors.New("no hay entornos. Agrega uno con `marga env add dev \"postgres://...\"`")
	}
	return "", fmt.Errorf("hay %d entornos; elige uno con --env (%s)", len(m), strings.Join(proyecto.NombresEntornos(m), ", "))
}

// escribirVariable pone VAR=valor en un archivo .env: reemplaza la línea si ya
// existe (aunque esté como "export VAR=") o la agrega al final. Conserva el resto
// del archivo y sus saltos de línea (CRLF en Windows).
func escribirVariable(ruta, variable, valor string) (bool, error) {
	b, err := os.ReadFile(ruta)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	texto := string(b)
	salto := "\n"
	if strings.Contains(texto, "\r\n") {
		salto = "\r\n"
	}
	nueva := variable + "=" + citarEnv(valor)
	re := regexp.MustCompile(`(?m)^[ \t]*(export[ \t]+)?` + regexp.QuoteMeta(variable) + `[ \t]*=.*?(\r?)$`)
	if loc := re.FindStringSubmatchIndex(texto); loc != nil {
		vieja := texto[loc[0]:loc[1]]
		pre := ""
		if loc[2] >= 0 {
			pre = texto[loc[2]:loc[3]]
		}
		reemplazo := pre + nueva
		if strings.HasSuffix(vieja, "\r") {
			reemplazo += "\r"
		}
		if vieja == reemplazo {
			return false, nil
		}
		texto = texto[:loc[0]] + reemplazo + texto[loc[1]:]
	} else {
		if texto != "" && !strings.HasSuffix(texto, "\n") {
			texto += salto
		}
		texto += nueva + salto
	}
	perm := os.FileMode(0o600)
	if st, err := os.Stat(ruta); err == nil {
		perm = st.Mode().Perm()
	}
	return true, os.WriteFile(ruta, []byte(texto), perm)
}

// citarEnv pone comillas si el valor tiene espacios o # (formato dotenv).
func citarEnv(v string) string {
	if strings.ContainsAny(v, " #\"'") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

func init() {
	urlCmd.Flags().StringVarP(&urlFlags.env, "env", "e", "", "entorno (por defecto dev, si es el único personal)")
	urlCmd.Flags().StringVar(&urlFlags.variable, "variable", "DATABASE_URL", "nombre de la variable (con --escribir, o para imprimir VAR=url)")
	urlCmd.Flags().StringVar(&urlFlags.escribir, "escribir", "", "archivo .env donde actualizar la variable")
	urlCmd.Flags().BoolVar(&ramaBDFlags.vacia, "vacia", false, "si la rama aún no tiene base, créala vacía")
}
