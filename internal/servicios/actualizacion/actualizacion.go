// Package actualizacion averigua si hay una versión de marga más nueva que la
// instalada (consulta el último release de GitHub) y cómo actualizarla según
// por dónde se instaló (Scoop, winget, Homebrew o a mano).
package actualizacion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo de GitHub donde GoReleaser publica los releases.
const Repo = "zara-te05/Marga"

// URLUltima es la API que dice cuál es el último release. Se puede cambiar en pruebas.
var URLUltima = "https://api.github.com/repos/" + Repo + "/releases/latest"

// Release es lo que nos interesa del último release.
type Release struct {
	Version string `json:"tag_name"`
	URL     string `json:"html_url"`
}

// Ultima consulta el último release publicado. Tarda como mucho `espera`.
func Ultima(ctx context.Context, espera time.Duration) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, espera)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URLUltima, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "marga-cli")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub respondió %s", res.Status)
	}
	var r Release
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return Release{}, err
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("GitHub no dijo cuál es la última versión")
	}
	return r, nil
}

// Desactivada: MARGA_SIN_AVISO_VERSION=1 (o CI) apaga la consulta, para scripts sin red.
func Desactivada() bool {
	return os.Getenv("MARGA_SIN_AVISO_VERSION") != "" || os.Getenv("CI") != ""
}

// MasNueva dice si `otra` es una versión posterior a `actual` ("v1.2.3", "1.2.3-rc.1").
// Si alguna no se puede leer (por ejemplo "dev"), devuelve false.
func MasNueva(otra, actual string) bool {
	a, okA := partes(otra)
	b, okB := partes(actual)
	if !okA || !okB {
		return false
	}
	for i := 0; i < 3; i++ {
		if a.n[i] != b.n[i] {
			return a.n[i] > b.n[i]
		}
	}
	// Misma versión base: la estable es posterior a sus pre-releases (1.2.0 > 1.2.0-rc.1).
	if a.pre == "" || b.pre == "" {
		return a.pre == "" && b.pre != ""
	}
	return a.pre > b.pre
}

type semver struct {
	n   [3]int
	pre string
}

func partes(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, " +"); i >= 0 { // "1.2.3 (abc1234)" o metadatos de compilación
		v = v[:i]
	}
	var s semver
	base, pre, _ := strings.Cut(v, "-")
	s.pre = pre
	campos := strings.Split(base, ".")
	if len(campos) == 0 || len(campos) > 3 {
		return s, false
	}
	for i, c := range campos {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 {
			return s, false
		}
		s.n[i] = n
	}
	return s, true
}

// ComoActualizar devuelve el comando para actualizar según dónde está el ejecutable.
func ComoActualizar(ejecutable string) string {
	r := strings.ToLower(strings.ReplaceAll(filepath.ToSlash(ejecutable), `\`, "/"))
	switch {
	case strings.Contains(r, "/scoop/"):
		return "scoop update marga"
	case strings.Contains(r, "/winget/"), strings.Contains(r, "/microsoft/winget"):
		return "winget upgrade zara-te05.Marga"
	case strings.Contains(r, "/cellar/"), strings.Contains(r, "/homebrew/"), strings.Contains(r, "/linuxbrew/"):
		return "brew upgrade marga"
	case strings.Contains(r, "/go/bin/"):
		return "go install github.com/zara-te05/Marga/cmd/marga@latest"
	}
	return ""
}
