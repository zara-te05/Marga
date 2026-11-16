package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/datos/remoto"
	"github.com/zara-te05/Marga/internal/datos/store"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
)

var initFlags struct {
	servidor string
	repo     int64
	crear    string
	motor    string
	dir      string
	local    bool
	nombre   string
}

const servidorLocal = "http://localhost:8080"

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Crea un proyecto de Marga en la carpeta actual",
	Long: `Crea la carpeta .marga/ (como .git/) y la carpeta de migraciones.

Sin opciones y en una terminal, te pregunta lo necesario: el nombre del proyecto,
la cadena de conexión de tu base (y la prueba), si usarás un servidor y, si la base
ya tiene tablas, si quieres adoptarla en el mismo paso.

Con opciones no pregunta nada (para scripts y CI):

  marga init --local                          proyecto sin servidor: el historial vive en git
  marga init --repo 3                         vinculado al proyecto 3 del servidor
  marga init --crear "Inventario"             crea el proyecto en el servidor y lo vincula

Fuera de una terminal (CI, tuberías) o con MARGA_NO_INTERACTIVO=1, marga init sin
opciones es igual a marga init --local.`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		raiz, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(raiz, proyecto.Dir)); err == nil {
			return sugerencia.Nuevof("no hace falta volver a iniciarlo: sigue con marga status", "ya existe un proyecto de Marga en %s", raiz)
		}
		if initFlags.local && (initFlags.repo > 0 || initFlags.crear != "" || c.Flags().Changed("servidor")) {
			return sugerencia.Nuevo("--local no va con --repo, --crear ni --servidor",
				"marga init --local (sin servidor) o marga init --crear \"Nombre\" (con servidor)")
		}
		if initFlags.repo > 0 && initFlags.crear != "" {
			return sugerencia.Nuevo("--repo y --crear no van juntos", "marga init --repo ID para un proyecto que ya existe, o marga init --crear \"Nombre\" para uno nuevo")
		}
		if c.Flags().NFlag() == 0 && interactivo(c) {
			return initGuiado(c, raiz)
		}
		return initConFlags(c, raiz)
	},
}

// interactivo: se puede preguntar (terminal de verdad, no CI ni pruebas).
func interactivo(c *cobra.Command) bool {
	if os.Getenv("MARGA_NO_INTERACTIVO") != "" || os.Getenv("CI") != "" {
		return false
	}
	return esTerminal() && c.InOrStdin() == os.Stdin && c.OutOrStdout() == os.Stdout
}

// ---------------------------------------------------------------- Con flags (scripts y CI)

func initConFlags(c *cobra.Command, raiz string) error {
	cfg := proyecto.Config{Nombre: initFlags.nombre, Motor: initFlags.motor, Migraciones: initFlags.dir}
	if cfg.Nombre == "" {
		cfg.Nombre = filepath.Base(raiz)
	}
	var nombreRepo string
	if initFlags.repo > 0 || initFlags.crear != "" {
		cfg.Servidor = remoto.NormalizarServidor(initFlags.servidor)
		cli, _, err := remoto.Conectado(cfg.Servidor)
		if initFlags.crear != "" {
			if err != nil {
				return conRepetir(err, "marga init --crear "+comillas(initFlags.crear))
			}
			r, err := crearRepo(cli, initFlags.crear, cfg.Motor, cfg.Servidor)
			if err != nil {
				return err
			}
			cfg.RepoID, nombreRepo, cfg.Nombre = r.ID, r.Nombre, r.Nombre
		} else {
			cfg.RepoID = initFlags.repo
			if err == nil {
				r, err := cli.Repo(cfg.RepoID)
				if err != nil {
					var re *remoto.Error
					if errors.As(err, &re) && (re.Status == http.StatusNotFound || re.Status == http.StatusForbidden) && re.Prueba == "" {
						return sugerencia.Con(err, "revisa el ID en la web (página Resumen del proyecto) o pide acceso a quien lo administra")
					}
					return err
				}
				nombreRepo, cfg.Nombre = r.Nombre, r.Nombre
				if !c.Flags().Changed("motor") && r.Motor != "" {
					cfg.Motor = r.Motor
				}
			} else {
				linea(c, "%s Aún no inicias sesión en %s; no pude verificar el proyecto %d (marga login lo hace después).",
					sAviso.Render("!"), cfg.Servidor, cfg.RepoID)
			}
		}
	}

	p, err := proyecto.Iniciar(raiz, cfg)
	if err != nil {
		return err
	}
	mostrarCreado(c, p, nombreRepo, false)
	linea(c, "")
	linea(c, "Siguiente paso:")
	linea(c, "  marga env add dev %s", proyecto.EjemploURL)
	return nil
}

// crearRepo crea el proyecto en el servidor. Si ya tienes uno con ese nombre
// (mismo slug), el error dice cuál es para vincularlo con --repo.
func crearRepo(cli *remoto.Cliente, nombre, motor, srv string) (remoto.Repo, error) {
	r, err := cli.CrearRepo(nombre, motor)
	if err == nil {
		return r, nil
	}
	if existente, ok := repoConSlug(cli, nombre, err); ok {
		conSrv := ""
		if srv != servidorLocal {
			conSrv = " --servidor " + srv
		}
		return r, sugerencia.Nuevof(fmt.Sprintf("marga init --repo %d%s para usar ese, o elige otro nombre con --crear", existente.ID, conSrv),
			"ya tienes un proyecto con ese nombre en el servidor: %s (repo %d, slug %s)", existente.Nombre, existente.ID, existente.Slug)
	}
	var re *remoto.Error
	if errors.As(err, &re) && re.Status == http.StatusConflict {
		return r, sugerencia.Con(err, "elige otro nombre con --crear, o vincula el que ya existe con --repo ID (lo ves en la web)")
	}
	return r, err
}

// repoConSlug: si err es "ya existe ese slug", busca tu proyecto que lo tiene.
func repoConSlug(cli *remoto.Cliente, nombre string, err error) (remoto.Repo, bool) {
	var re *remoto.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict {
		return remoto.Repo{}, false
	}
	slug := store.Slugify(nombre)
	repos, errL := cli.Repos()
	if errL != nil {
		return remoto.Repo{}, false
	}
	for _, r := range repos {
		if r.Slug == slug {
			return r, true
		}
	}
	return remoto.Repo{}, false
}

func mostrarCreado(c *cobra.Command, p *proyecto.Proyecto, nombreRepo string, conEntorno bool) {
	linea(c, "%s Proyecto de Marga creado en %s", sAcento.Render("✓"), p.Raiz)
	linea(c, "  %s  configuración (versiónala con git)", relleno(".marga/config.json", 22))
	linea(c, "  %s  aquí van tus migraciones", relleno(filepath.ToSlash(p.Config.Migraciones)+"/", 22))
	if conEntorno {
		linea(c, "  %s  entorno dev (no se versiona: tiene tu contraseña)", relleno(".marga/entornos.json", 22))
	}
	if nombreRepo != "" {
		linea(c, "  vinculado a %s (repo %d) en %s", sNegri.Render(nombreRepo), p.Config.RepoID, p.Config.Servidor)
		return
	}
	linea(c, "  %s", sTenue.Render("modo local: sin servidor. El historial vive en git ("+filepath.ToSlash(p.Config.Migraciones)+"/) y en marga_history de cada base."))
	if gitDe(p) == nil {
		linea(c, "  %s", sTenue.Render("Tip: git init para versionar las migraciones y que Marga siga tus ramas."))
	}
}

// ---------------------------------------------------------------- Guiado

func initGuiado(c *cobra.Command, raiz string) error {
	linea(c, "Vamos a crear un proyecto de Marga en %s", sNegri.Render(raiz))
	linea(c, "%s", sTenue.Render("Enter acepta lo que está entre [corchetes]. Para scripts: marga init --help"))
	linea(c, "")

	// 1. Nombre
	nombre, err := preguntarDefecto("Nombre del proyecto", filepath.Base(raiz))
	if err != nil {
		return err
	}

	// 2. Base de datos
	linea(c, "")
	linea(c, "Cadena de conexión de tu base de desarrollo %s", sTenue.Render("(Enter para hacerlo después)"))
	linea(c, "  %s", sTenue.Render("ejemplo: postgres://usuario:clave@localhost:5432/mi_bd?sslmode=disable"))
	url, motor, conecta, err := pedirBase(c)
	if err != nil {
		return err
	}

	// 3. Servidor
	linea(c, "")
	cfg := proyecto.Config{Nombre: nombre, Motor: motor}
	var nombreRepo string
	usarServidor, err := confirmar("¿Compartirás el historial con un servidor de Marga?", false)
	if err != nil {
		return err
	}
	if usarServidor {
		srv, repo, err := elegirServidor(c, nombre, motor)
		if err != nil {
			return err
		}
		if repo != nil {
			cfg.Servidor, cfg.RepoID, nombreRepo = srv, repo.ID, repo.Nombre
		}
	}

	// Crear
	linea(c, "")
	p, err := proyecto.Iniciar(raiz, cfg)
	if err != nil {
		return err
	}
	if url != "" {
		ents := map[string]proyecto.Entorno{"dev": {URL: url, Motor: motor, Compartido: false}}
		if err := p.GuardarEntornos(ents); err != nil {
			return err
		}
	}
	mostrarCreado(c, p, nombreRepo, url != "")
	if url == "" {
		linea(c, "")
		linea(c, "Siguiente paso:")
		linea(c, "  marga env add dev %s", proyecto.EjemploURL)
		return nil
	}
	if !conecta {
		linea(c, "")
		linea(c, "%s La base no respondió. Cuando esté arriba:", sAviso.Render("!"))
		linea(c, "  prueba: marga env probar dev")
		return nil
	}

	// 4. ¿Adoptar lo que ya tiene la base?
	return ofrecerAdoptar(c)
}

// pedirBase pide la cadena de conexión y la prueba hasta que funcione o la persona
// decida seguir. Devuelve url "" si la deja para después.
func pedirBase(c *cobra.Command) (url, motor string, conecta bool, err error) {
	for {
		url, err = preguntarDefecto("URL", "")
		if err != nil || url == "" {
			return "", initFlags.motor, false, err
		}
		if motor = motorDeURL(url); motor == "" {
			if motor, err = preguntarDefecto("Motor (postgresql, mysql o sqlserver)", "postgresql"); err != nil {
				return "", "", false, err
			}
		}
		driver, errD := driverDe(motor)
		if errD != nil {
			imprimirError(out(c), errD)
			continue
		}
		e := proyecto.Entorno{URL: url}
		real, errU := e.URLReal()
		if errU == nil {
			errU = ProbarConexion(driver, real)
		}
		if errU == nil {
			linea(c, "  %s Conexión correcta (%s)", sAcento.Render("✓"), e.URLOculta())
			return url, motor, true, nil
		}
		linea(c, "  %s No se pudo conectar: %v", sError.Render("✗"), errU)
		linea(c, "  %s", sTenue.Render("prueba: revisa que la base esté corriendo, el usuario, la contraseña y el nombre de la base"))
		otra, err := confirmar("¿La escribes de nuevo?", true)
		if err != nil {
			return "", "", false, err
		}
		if otra {
			continue
		}
		guardar, err := confirmar("¿La guardo así para corregirla después?", true)
		if err != nil || !guardar {
			return "", motor, false, err
		}
		return url, motor, false, nil
	}
}

// elegirServidor pregunta la URL del servidor, inicia sesión si hace falta y crea
// (o vincula) el proyecto. repo nil: la persona decidió seguir en modo local.
func elegirServidor(c *cobra.Command, nombre, motor string) (string, *remoto.Repo, error) {
	for {
		srv, err := preguntarDefecto("URL del servidor", servidorPorDefecto(""))
		if err != nil {
			return "", nil, err
		}
		srv = remoto.NormalizarServidor(srv)
		if _, err := remoto.Nuevo(srv, "").Salud(); err != nil {
			imprimirError(out(c), err)
			otra, err2 := confirmar("¿Pruebas con otra URL? (no: sigue en modo local, puedes vincularlo después con marga link)", true)
			if err2 != nil {
				return "", nil, err2
			}
			if otra {
				continue
			}
			return "", nil, nil
		}
		cli, _, err := remoto.Conectado(srv)
		if err != nil {
			linea(c, "Inicia sesión en %s %s", srv, sTenue.Render("(si no tienes cuenta, regístrate en la web)"))
			email, err := preguntar("Email: ")
			if err != nil {
				return "", nil, err
			}
			clave, err := pedirClave()
			if err != nil {
				return "", nil, err
			}
			if err := iniciarSesion(c, srv, email, clave); err != nil {
				imprimirError(out(c), err)
				continue
			}
			if cli, _, err = remoto.Conectado(srv); err != nil {
				return "", nil, err
			}
		}
		repo, err := crearOVincular(c, cli, nombre, motor)
		if err != nil {
			return "", nil, err
		}
		return srv, repo, nil
	}
}

// crearOVincular crea el proyecto en el servidor; si ya existe uno con ese nombre,
// ofrece vincularse a él o elegir otro nombre.
func crearOVincular(c *cobra.Command, cli *remoto.Cliente, nombre, motor string) (*remoto.Repo, error) {
	for {
		r, err := cli.CrearRepo(nombre, motor)
		if err == nil {
			linea(c, "  %s Proyecto %s creado en el servidor (repo %d)", sAcento.Render("✓"), sNegri.Render(r.Nombre), r.ID)
			return &r, nil
		}
		existente, ok := repoConSlug(cli, nombre, err)
		if !ok {
			return nil, err
		}
		linea(c, "  %s Ya tienes un proyecto %s en el servidor (repo %d).", sAviso.Render("!"), sNegri.Render(existente.Nombre), existente.ID)
		usar, err := confirmar("¿Vinculo esta carpeta a ese proyecto?", true)
		if err != nil {
			return nil, err
		}
		if usar {
			return &existente, nil
		}
		if nombre, err = preguntarDefecto("Otro nombre para el proyecto nuevo", ""); err != nil {
			return nil, err
		}
		if nombre == "" {
			return nil, sugerencia.Nuevo("no se creó el proyecto", "marga init otra vez, con otro nombre")
		}
	}
}

// ofrecerAdoptar: si la base ya tiene tablas y nunca usó Marga, ofrece adoptarla.
func ofrecerAdoptar(c *cobra.Command) error {
	ctx := context.Background()
	_, _, nombre, _, m, err := conectar(ctx, c)
	if err != nil {
		linea(c, "%s No pude revisar la base: %v", sAviso.Render("!"), err)
		return nil
	}
	aplicadas, errH := m.Historial(ctx)
	real, errE := m.EsquemaReal(ctx)
	m.Close()
	linea(c, "")
	switch {
	case errH != nil || errE != nil:
		linea(c, "Siguiente paso: marga status")
		return nil
	case len(aplicadas) > 0:
		linea(c, "%s La base ya tiene migraciones de Marga aplicadas.", sAcento.Render("✓"))
		linea(c, "Siguiente paso: marga status")
		return nil
	case vacio(real):
		linea(c, "La base está vacía. Siguiente paso:")
		linea(c, "  marga new \"crear tabla clientes\"   escribe la primera migración")
		linea(c, "  marga up                           y aplícala")
		return nil
	}
	linea(c, "La base %s ya tiene %s.", sNegri.Render(nombre), resumenEsquema(real))
	linea(c, "%s", sTenue.Render("Adoptarla crea la migración 0001 con ese esquema (comprobada en una base temporal) y la marca como aplicada."))
	si, err := confirmar("¿La adopto ahora?", true)
	if err != nil {
		return err
	}
	if !si {
		linea(c, "Cuando quieras: marga adoptar")
		return nil
	}
	ResetFlags(adoptarCmd)
	if err := adoptarCmd.RunE(c, nil); err != nil {
		return sugerencia.Con(fmt.Errorf("el proyecto quedó creado, pero no se pudo adoptar la base: %w", err), conRepetirTexto(sugerencia.De(err), "marga adoptar"))
	}
	return nil
}

// ---------------------------------------------------------------- Ayudas

// motorDeURL adivina el motor por la forma de la cadena ("" si no se sabe).
func motorDeURL(u string) string {
	l := strings.ToLower(strings.TrimSpace(u))
	switch {
	case strings.HasPrefix(l, "postgres://"), strings.HasPrefix(l, "postgresql://"):
		return "postgresql"
	case strings.HasPrefix(l, "sqlserver://"):
		return "sqlserver"
	case strings.Contains(l, "@tcp("), strings.Contains(l, "@unix("):
		return "mysql"
	}
	return ""
}

func preguntarDefecto(etiqueta, defecto string) (string, error) {
	if defecto != "" {
		etiqueta += " [" + defecto + "]"
	}
	s, err := preguntar(etiqueta + ": ")
	if err != nil {
		return "", sugerencia.Nuevo("cancelado", "vuelve a correr marga init, o usa opciones para no responder nada: marga init --help")
	}
	if s == "" {
		return defecto, nil
	}
	return s, nil
}

func confirmar(pregunta string, defecto bool) (bool, error) {
	op := "[s/N]"
	if defecto {
		op = "[S/n]"
	}
	for {
		s, err := preguntar(pregunta + " " + op + " ")
		if err != nil {
			return false, sugerencia.Nuevo("cancelado", "vuelve a correr marga init, o usa opciones para no responder nada: marga init --help")
		}
		switch strings.ToLower(s) {
		case "":
			return defecto, nil
		case "s", "si", "sí", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func comillas(s string) string { return "\"" + s + "\"" }

// conRepetir: la sugerencia del error y, después, repetir el comando.
func conRepetir(err error, comando string) error {
	return sugerencia.Con(err, conRepetirTexto(sugerencia.De(err), comando))
}

func conRepetirTexto(prueba, comando string) string {
	if prueba == "" {
		return comando
	}
	return prueba + " y repite " + comando
}

// ---------------------------------------------------------------- link

var linkFlags struct {
	servidor string
	repo     int64
}

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Vincula el proyecto local con un proyecto del servidor",
	Long: `Pasa un proyecto en modo local a usar un servidor de Marga (push, pull, log…).

  marga link --repo 3
  marga link --repo 3 --servidor https://marga.miempresa.com`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		if linkFlags.repo <= 0 {
			return sugerencia.Nuevo("falta el proyecto del servidor", "marga link --repo ID (el ID lo ves en la web, en la página Resumen)")
		}
		srv := remoto.NormalizarServidor(linkFlags.servidor)
		cli, _, err := remoto.Conectado(srv)
		if err != nil {
			return conRepetir(err, fmt.Sprintf("marga link --repo %d", linkFlags.repo))
		}
		r, err := cli.Repo(linkFlags.repo)
		if err != nil {
			return err
		}
		p.Config.Servidor, p.Config.RepoID = srv, r.ID
		if p.Config.Nombre == "" {
			p.Config.Nombre = r.Nombre
		}
		if err := p.Guardar(); err != nil {
			return err
		}
		linea(c, "%s Vinculado a %s (repo %d) en %s", sAcento.Render("✓"), sNegri.Render(r.Nombre), r.ID, srv)
		linea(c, "Siguiente paso: marga push")
		return nil
	},
}

func init() {
	f := initCmd.Flags()
	f.BoolVar(&initFlags.local, "local", false, "proyecto sin servidor: no pide login; el historial vive en git y en marga_history")
	f.StringVar(&initFlags.nombre, "nombre", "", "nombre del proyecto (por defecto, el de la carpeta)")
	f.StringVar(&initFlags.servidor, "servidor", servidorLocal, "URL del servidor de Marga")
	f.Int64Var(&initFlags.repo, "repo", 0, "ID de un proyecto existente en el servidor")
	f.StringVar(&initFlags.crear, "crear", "", "crea un proyecto nuevo en el servidor con este nombre")
	f.StringVar(&initFlags.motor, "motor", "postgresql", "motor de base de datos")
	f.StringVar(&initFlags.dir, "dir", "migrations", "carpeta de migraciones")

	lf := linkCmd.Flags()
	lf.StringVar(&linkFlags.servidor, "servidor", servidorLocal, "URL del servidor de Marga")
	lf.Int64Var(&linkFlags.repo, "repo", 0, "ID del proyecto en el servidor")
}
