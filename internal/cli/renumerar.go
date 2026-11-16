package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/proyecto"
	"github.com/zara-te05/Marga/internal/remoto"
)

var renumerarSimular bool

var renumerarCmd = &cobra.Command{
	Use:   "renumerar",
	Short: "Arregla migraciones con la misma versión (pasa al juntar ramas en git)",
	Long: `Si tú y un compañero crearon una 0007 en ramas distintas, al juntarlas en git
quedan dos archivos 0007 y Marga se detiene. renumerar deja la versión a la que
ya estaba en la rama principal y mueve las demás al final (0008, 0009…).

Cuál se queda (en este orden):
  1. la que ya está en la rama principal del servidor
  2. la que ya estaba en la rama principal de git antes de juntar
  3. la que ya se envió al servidor (.marga/remoto/)
  4. la que se agregó antes a git

Si ya habías aplicado en tu base la que se movió, marga up solo actualiza el
historial: no la vuelve a ejecutar.`,
	Example: `  git merge main          (o git merge feature/facturas)
  marga renumerar --simular
  marga renumerar
  git add migrations && git commit`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		p, err := proyectoActual()
		if err != nil {
			return err
		}
		todas, err := p.TodasLasMigraciones()
		if err != nil {
			return err
		}
		porVersion := map[int][]proyecto.Migracion{}
		maxV := 0
		for _, m := range todas {
			n := versionNum(m.Version)
			porVersion[n] = append(porVersion[n], m)
			maxV = max(maxV, n)
		}
		var repetidas []int
		for v, ms := range porVersion {
			if len(ms) > 1 {
				repetidas = append(repetidas, v)
			}
		}
		if len(repetidas) == 0 {
			linea(c, "%s No hay versiones repetidas en %s", sAcento.Render("✓"), relativo(p, p.DirMigraciones()))
			return nil
		}
		sort.Ints(repetidas)

		pr := prioridadesRenumerar(p)
		type mov struct {
			m     proyecto.Migracion
			nueva string
		}
		var movs []mov
		for _, v := range repetidas {
			ms := porVersion[v]
			sort.SliceStable(ms, func(i, j int) bool { return pr.antes(ms[i], ms[j]) })
			linea(c, "Versión %s: se queda %s %s", sNegri.Render(ms[0].Version), sNegri.Render(ms[0].ID()), sTenue.Render("("+pr.motivo(ms[0])+")"))
			for _, m := range ms[1:] {
				maxV++
				nueva := fmt.Sprintf("%0*d", max(4, len(m.Version)), maxV)
				movs = append(movs, mov{m, nueva})
				linea(c, "    %s %s → %s", sAcento.Render("↻"), m.ID(), sNegri.Render(nueva+"_"+m.Nombre))
			}
		}
		if renumerarSimular {
			linea(c, "%s", sTenue.Render("(simulación: no se renombró nada)"))
			return nil
		}
		mapa, err := p.Remoto()
		if err != nil {
			return err
		}
		for _, mv := range movs {
			if err := p.Renombrar(mv.m, mv.nueva, mv.m.Nombre); err != nil {
				return fmt.Errorf("no se pudo renombrar %s: %w", mv.m.ID(), err)
			}
			if h, ok := mapa[mv.m.ID()]; ok {
				delete(mapa, mv.m.ID())
				mapa[mv.nueva+"_"+mv.m.Nombre] = h
			}
		}
		if err := p.GuardarRemoto(mapa); err != nil {
			return err
		}
		linea(c, "%s %s", sAcento.Render("✓"), plural(len(movs), "migración renumerada", "migraciones renumeradas"))
		linea(c, "Siguiente: marga up (actualiza el historial de tu base) y haz commit en git de %s/", filepath.ToSlash(relativo(p, p.DirMigraciones())))
		return nil
	},
}

// prioridad decide qué migración conserva la versión cuando hay repetidas.
type prioridad struct {
	enServidor map[string]bool    // checksums en la rama principal del servidor
	enGit      map[string]bool    // archivos (.up.sql) en la rama principal de git antes de juntar
	enviadas   map[string]bool    // ids en .marga/remoto/
	agregada   func(string) int64 // fecha en que el archivo entró a git (0 = no está en git)
	dir        string
}

func (pr prioridad) nivel(m proyecto.Migracion) int {
	switch {
	case pr.enServidor[m.Checksum]:
		return 0
	case pr.enGit[m.ID()+".up.sql"]:
		return 1
	case pr.enviadas[m.ID()]:
		return 2
	}
	return 3
}

func (pr prioridad) motivo(m proyecto.Migracion) string {
	return [...]string{"ya está en la rama principal del servidor", "ya estaba en la rama principal de git",
		"ya se envió al servidor", "se agregó antes a git"}[pr.nivel(m)]
}

func (pr prioridad) antes(a, b proyecto.Migracion) bool {
	if na, nb := pr.nivel(a), pr.nivel(b); na != nb {
		return na < nb
	}
	ta, tb := pr.agregada(a.ID()+".up.sql"), pr.agregada(b.ID()+".up.sql")
	if ta == 0 {
		ta = time.Now().Unix() + 1 // sin commit: la más nueva
	}
	if tb == 0 {
		tb = time.Now().Unix() + 1
	}
	if ta != tb {
		return ta < tb
	}
	return a.Nombre < b.Nombre
}

func prioridadesRenumerar(p *proyecto.Proyecto) prioridad {
	pr := prioridad{enServidor: map[string]bool{}, enGit: map[string]bool{}, enviadas: map[string]bool{},
		agregada: func(string) int64 { return 0 }, dir: p.DirMigraciones()}
	if mapa, err := p.Remoto(); err == nil {
		for id := range mapa {
			pr.enviadas[id] = true
		}
	}
	principal := "main"
	repo := gitDe(p)
	if repo != nil {
		if local, err := p.Local(); err == nil {
			principal, _ = ramaPrincipal(p, repo, local)
		}
	}
	if p.Config.Servidor != "" && p.Config.RepoID != 0 {
		if cli, _, err := remoto.Conectado(p.Config.Servidor); err == nil {
			if h, err := cli.Historial(p.Config.RepoID, principal); err == nil {
				for _, cm := range h.Commits {
					pr.enServidor[cm.Checksum] = true
				}
			}
		}
	}
	if repo == nil {
		return pr
	}
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", repo.Raiz}, args...)...).Output()
		return string(out), err
	}
	rel, err := filepath.Rel(repo.Raiz, p.DirMigraciones())
	if err != nil {
		return pr
	}
	rel = filepath.ToSlash(rel)
	// La rama principal "antes de juntar": si estás en ella y HEAD es un merge, su primer padre.
	ref := principal
	if actual, sep, _ := repo.RamaActual(); !sep && actual == principal {
		ref = "HEAD"
		if _, err := git("rev-parse", "--verify", "--quiet", "HEAD^2"); err == nil {
			ref = "HEAD^1"
		}
	}
	if out, err := git("ls-tree", "--name-only", ref+":"+rel); err == nil {
		for _, f := range strings.Fields(out) {
			pr.enGit[f] = true
		}
	}
	fechas := map[string]int64{}
	if out, err := git("log", "--diff-filter=A", "--format=@%ct", "--name-only", "--", rel); err == nil {
		var t int64
		for _, l := range strings.Split(out, "\n") {
			l = strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(l, "@"):
				t, _ = strconv.ParseInt(l[1:], 10, 64)
			case l != "":
				fechas[filepath.Base(l)] = t // el log va del más nuevo al más viejo: queda la primera vez
			}
		}
	}
	pr.agregada = func(f string) int64 { return fechas[f] }
	return pr
}

func init() {
	renumerarCmd.Flags().BoolVar(&renumerarSimular, "simular", false, "muestra qué se renombraría sin cambiar nada")
}
