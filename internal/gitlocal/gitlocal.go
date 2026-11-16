// Package gitlocal lee lo mínimo de un repositorio git sin ejecutar el binario git:
// la rama actual, si una rama existe y de qué rama se salió al crear otra.
// Leer los archivos de .git es instantáneo y funciona aunque git no esté en el PATH.
package gitlocal

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Repo es un repositorio git encontrado subiendo desde una carpeta.
type Repo struct {
	Raiz      string // carpeta de trabajo (donde está .git)
	gitDir    string // .git, o la carpeta de un worktree
	commonDir string // donde viven refs/ y packed-refs (igual a gitDir salvo en worktrees)
}

// ErrSinGit: la carpeta no está dentro de un repositorio git.
var ErrSinGit = errors.New("no es un repositorio git")

// Buscar sube desde `desde` hasta encontrar .git (carpeta, o archivo en worktrees y submódulos).
func Buscar(desde string) (*Repo, error) {
	dir, err := filepath.Abs(desde)
	if err != nil {
		return nil, err
	}
	for {
		ruta := filepath.Join(dir, ".git")
		if st, err := os.Stat(ruta); err == nil {
			r := &Repo{Raiz: dir, gitDir: ruta}
			if !st.IsDir() {
				// Worktree o submódulo: el archivo dice "gitdir: <ruta>".
				b, err := os.ReadFile(ruta)
				if err != nil {
					return nil, err
				}
				g := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
				if !filepath.IsAbs(g) {
					g = filepath.Join(dir, g)
				}
				r.gitDir = filepath.Clean(g)
			}
			r.commonDir = r.gitDir
			if b, err := os.ReadFile(filepath.Join(r.gitDir, "commondir")); err == nil {
				c := strings.TrimSpace(string(b))
				if !filepath.IsAbs(c) {
					c = filepath.Join(r.gitDir, c)
				}
				r.commonDir = filepath.Clean(c)
			}
			return r, nil
		}
		padre := filepath.Dir(dir)
		if padre == dir {
			return nil, ErrSinGit
		}
		dir = padre
	}
}

// RamaActual devuelve la rama de HEAD. separado=true si HEAD apunta a un commit
// (detached HEAD: git checkout <hash>, un rebase en curso…).
func (r *Repo) RamaActual() (rama string, separado bool, err error) {
	b, err := os.ReadFile(filepath.Join(r.gitDir, "HEAD"))
	if err != nil {
		return "", false, err
	}
	head := strings.TrimSpace(string(b))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		if rama, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			return rama, false, nil
		}
		return "", true, nil
	}
	return "", true, nil
}

// Existe indica si hay una rama local con ese nombre (refs/heads o packed-refs).
func (r *Repo) Existe(rama string) bool {
	if rama == "" {
		return false
	}
	if st, err := os.Stat(filepath.Join(r.commonDir, "refs", "heads", filepath.FromSlash(rama))); err == nil && !st.IsDir() {
		return true
	}
	for _, ref := range r.packedRefs() {
		if ref == "refs/heads/"+rama {
			return true
		}
	}
	return false
}

// Ramas lista las ramas locales.
func (r *Repo) Ramas() []string {
	vistas := map[string]bool{}
	base := filepath.Join(r.commonDir, "refs", "heads")
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if rel, err := filepath.Rel(base, p); err == nil {
				vistas[filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	for _, ref := range r.packedRefs() {
		if rama, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			vistas[rama] = true
		}
	}
	out := make([]string, 0, len(vistas))
	for k := range vistas {
		out = append(out, k)
	}
	return out
}

func (r *Repo) packedRefs() []string {
	f, err := os.Open(filepath.Join(r.commonDir, "packed-refs"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || l[0] == '#' || l[0] == '^' {
			continue
		}
		if _, ref, ok := strings.Cut(l, " "); ok {
			out = append(out, strings.TrimSpace(ref))
		}
	}
	return out
}

// Origen intenta saber de qué rama se salió la primera vez que se entró a `rama`,
// leyendo el reflog de HEAD ("checkout: moving from main to feature/facturas").
// Devuelve "" si no lo sabe (reflog desactivado, rama traída de un remoto…).
func (r *Repo) Origen(rama string) string {
	f, err := os.Open(filepath.Join(r.gitDir, "logs", "HEAD"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sufijo := " to " + rama
	for sc.Scan() { // el reflog va del más viejo al más nuevo: la primera entrada es la creación
		_, msg, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		desde, ok := strings.CutPrefix(msg, "checkout: moving from ")
		if !ok || !strings.HasSuffix(desde, sufijo) {
			continue
		}
		desde = strings.TrimSuffix(desde, sufijo)
		if desde != rama && !strings.Contains(desde, " ") {
			return desde
		}
	}
	return ""
}
