package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/datos/remoto"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
)

// Cada error de la CLI termina con la siguiente acción que conviene intentar:
//
//	Error: no hay conexión con el servidor http://localhost:8080
//	  prueba: levanta el servidor (marga-server) o corrige la URL con --servidor
//
// Los comandos ponen la sugerencia con sugerencia.Nuevo / sugerencia.Con; para los
// errores que llegan sin ella (de Cobra, del sistema), sugerir() deduce una.

// imprimirError escribe el error y su "prueba: …" en w.
func imprimirError(w io.Writer, err error) {
	fmt.Fprintln(w, sError.Render("Error:"), err)
	if p := sugerir(err); p != "" {
		fmt.Fprintln(w, " ", sAcento.Render("prueba:"), p)
	}
}

// Sugerencia devuelve el siguiente paso para un error (la usan las pruebas y `marga ui`).
func Sugerencia(err error) string { return sugerir(err) }

func sugerir(err error) string {
	if err == nil {
		return ""
	}
	if s := sugerencia.De(err); s != "" {
		return s
	}
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "unknown command"):
		// Si el comando existe en esta versión, quien respondió fue otro marga más viejo;
		// pero si llegamos aquí, el comando de verdad no existe.
		return "marga --help para ver los comandos" + avisoOtroMarga()
	case strings.HasPrefix(msg, "unknown flag"), strings.HasPrefix(msg, "unknown shorthand flag"):
		return "marga <comando> --help para ver sus opciones" + avisoOtroMarga()
	case strings.Contains(msg, "accepts") && strings.Contains(msg, "arg(s)"),
		strings.Contains(msg, "requires at least"), strings.Contains(msg, "invalid argument"):
		return "marga <comando> --help para ver cómo se usa"
	case errors.Is(err, os.ErrPermission):
		return "revisa los permisos de la carpeta (o ciérrala en otros programas) y repite"
	}
	return ""
}

// pruebaConexion: qué revisar cuando una base de datos no responde.
func pruebaConexion(motor, entorno string) string {
	servicio := "PostgreSQL"
	switch motor {
	case "mysql":
		servicio = "MySQL"
	case "sqlserver":
		servicio = "SQL Server"
	}
	return fmt.Sprintf("revisa que %s esté corriendo y que la cadena sea correcta (marga env ls); cámbiala con marga env add %s \"…\" y pruébala con marga env probar %s",
		servicio, entorno, entorno)
}

// ---------------------------------------------------------------- Modo local

// esLocal: el proyecto no usa servidor. Su historial vive en git (migrations/) y
// en la tabla marga_history de cada base; push, pull y log no tienen nada que hacer.
func esLocal(p *proyecto.Proyecto) bool { return p.Config.Servidor == "" || p.Config.RepoID == 0 }

var errModoLocal = sugerencia.Nuevo("este proyecto no usa servidor (modo local): su historial vive en git y en marga_history de cada base",
	"con git, cámbiate de rama con git checkout y Marga la sigue; para compartir el historial con un servidor: marga link --repo ID")

// avisarSiLocal explica que push/pull no aplican en modo local. Devuelve true si
// el proyecto es local (el comando ya terminó, sin error: no hay nada que hacer).
func avisarSiLocal(c *cobra.Command, accion string) (bool, error) {
	p, err := proyectoActual()
	if err != nil {
		return false, err
	}
	if !esLocal(p) {
		return false, nil
	}
	linea(c, "%s Este proyecto no usa servidor (modo local): `marga %s` no tiene nada que hacer.", sAcento.Render("✓"), accion)
	linea(c, "  El historial vive en git (%s/) y en la tabla marga_history de cada base.", filepath.ToSlash(p.Config.Migraciones))
	linea(c, "  Para compartirlo con tu equipo: haz commit de %s/ en git y quien lo clone corre marga up.", filepath.ToSlash(p.Config.Migraciones))
	linea(c, "%s", sTenue.Render("  Si quieres usar un servidor de Marga: marga login y luego marga link --repo ID"))
	return true, nil
}

// ---------------------------------------------------------------- Otro marga en el PATH

// otrosMarga devuelve los ejecutables llamados marga del PATH, en el orden en que
// la terminal los buscaría, y cuál es este (-1 si este no está en el PATH).
func otrosMarga() (rutas []string, este int) {
	este = -1
	yo, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(yo); err == nil {
		yo = r
	}
	nombre := "marga"
	if runtime.GOOS == "windows" {
		nombre = "marga.exe"
	}
	vistos := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		ruta := filepath.Join(dir, nombre)
		st, err := os.Stat(ruta)
		if err != nil || st.IsDir() {
			continue
		}
		real := ruta
		if r, err := filepath.EvalSymlinks(ruta); err == nil {
			real = r
		}
		clave := strings.ToLower(filepath.Clean(real))
		if vistos[clave] {
			continue
		}
		vistos[clave] = true
		if yo != "" && strings.EqualFold(filepath.Clean(real), filepath.Clean(yo)) {
			este = len(rutas)
		}
		rutas = append(rutas, ruta)
	}
	return rutas, este
}

// avisoOtroMarga: si hay otro marga que la terminal usaría antes que este, lo dice.
func avisoOtroMarga() string {
	rutas, este := otrosMarga()
	if len(rutas) == 0 || este == 0 {
		return ""
	}
	return fmt.Sprintf(". Ojo: la terminal usa primero %s; si es una versión vieja, bórralo o pon esta carpeta antes en el PATH (marga version lo revisa)", rutas[0])
}

// esErrorSinConexion: el servidor no respondió (para decidir si ofrecer el modo local).
func esErrorSinConexion(err error) bool {
	var re *remoto.Error
	return errors.As(err, &re) && (re.SinConexion() || (re.Status == 404 && strings.Contains(re.Msg, "no hay un servidor de Marga")))
}
