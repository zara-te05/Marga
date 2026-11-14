package cmd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zarat/Motor-Marga/internal/proyecto"
	"github.com/zarat/Motor-Marga/internal/remoto"
)

var initFlags struct {
	servidor string
	repo     int64
	crear    string
	motor    string
	dir      string
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Crea un proyecto de Marga en la carpeta actual",
	Long: `Crea la carpeta .marga/ (como .git/) y la carpeta de migraciones.

  marga init                                  proyecto solo local
  marga init --repo 3                         vinculado al proyecto 3 del servidor
  marga init --crear "Inventario"             crea el proyecto en el servidor y lo vincula`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		raiz, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg := proyecto.Config{Motor: initFlags.motor, Migraciones: initFlags.dir}

		if initFlags.repo > 0 && initFlags.crear != "" {
			return errors.New("usa --repo o --crear, no los dos")
		}
		var nombreRepo string
		if initFlags.repo > 0 || initFlags.crear != "" {
			cfg.Servidor = remoto.NormalizarServidor(initFlags.servidor)
			cli, _, err := remoto.Conectado(cfg.Servidor)
			if initFlags.crear != "" {
				if err != nil {
					return err
				}
				r, err := cli.CrearRepo(initFlags.crear, cfg.Motor)
				if err != nil {
					return err
				}
				cfg.RepoID, nombreRepo = r.ID, r.Nombre
			} else {
				cfg.RepoID = initFlags.repo
				if err == nil {
					r, err := cli.Repo(cfg.RepoID)
					if err != nil {
						return err
					}
					nombreRepo = r.Nombre
					if cfg.Motor == "" {
						cfg.Motor = r.Motor
					}
				} else {
					linea(c, "%s %s", sAviso.Render("!"), "Aún no inicias sesión; no pude verificar el proyecto en el servidor.")
				}
			}
		}

		p, err := proyecto.Iniciar(raiz, cfg)
		if err != nil {
			return err
		}
		linea(c, "%s Proyecto de Marga creado en %s", sAcento.Render("✓"), raiz)
		linea(c, "  %s  configuración (versiónala con git)", relleno(".marga/config.json", 22))
		linea(c, "  %s  aquí van tus migraciones", relleno(filepath.ToSlash(p.Config.Migraciones)+"/", 22))
		if nombreRepo != "" {
			linea(c, "  vinculado a %s (repo %d) en %s", sNegri.Render(nombreRepo), p.Config.RepoID, p.Config.Servidor)
		}
		linea(c, "")
		linea(c, "Siguiente paso:")
		linea(c, "  marga env add dev \"postgres://usuario:clave@localhost:5432/mi_bd?sslmode=disable\"")
		return nil
	},
}

var linkFlags struct {
	servidor string
	repo     int64
}

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Vincula el proyecto local con un proyecto del servidor",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		if linkFlags.repo <= 0 {
			return errors.New("indica el proyecto con --repo ID (lo ves en la página Resumen)")
		}
		srv := remoto.NormalizarServidor(linkFlags.servidor)
		cli, _, err := remoto.Conectado(srv)
		if err != nil {
			return err
		}
		r, err := cli.Repo(linkFlags.repo)
		if err != nil {
			return err
		}
		p.Config.Servidor, p.Config.RepoID = srv, r.ID
		if err := p.Guardar(); err != nil {
			return err
		}
		linea(c, "%s Vinculado a %s (repo %d) en %s", sAcento.Render("✓"), sNegri.Render(r.Nombre), r.ID, srv)
		return nil
	},
}

func init() {
	f := initCmd.Flags()
	f.StringVar(&initFlags.servidor, "servidor", "http://localhost:8080", "URL del servidor de Marga")
	f.Int64Var(&initFlags.repo, "repo", 0, "ID de un proyecto existente en el servidor")
	f.StringVar(&initFlags.crear, "crear", "", "crea un proyecto nuevo en el servidor con este nombre")
	f.StringVar(&initFlags.motor, "motor", "postgresql", "motor de base de datos")
	f.StringVar(&initFlags.dir, "dir", "migrations", "carpeta de migraciones")

	lf := linkCmd.Flags()
	lf.StringVar(&linkFlags.servidor, "servidor", "http://localhost:8080", "URL del servidor de Marga")
	lf.Int64Var(&linkFlags.repo, "repo", 0, "ID del proyecto en el servidor")
}
