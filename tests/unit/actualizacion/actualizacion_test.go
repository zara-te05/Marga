package actualizacion_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/servicios/actualizacion"
)

func TestMasNueva(t *testing.T) {
	casos := []struct {
		otra, actual string
		quiero       bool
	}{
		{"v0.6.0", "v0.5.9", true},
		{"v0.5.10", "v0.5.9", true},
		{"v1.0.0", "0.9.0", true},
		{"v0.5.0", "v0.5.0", false},
		{"v0.5.0", "v0.6.0", false},
		{"v0.6.0", "v0.6.0-rc.1", true},
		{"v0.6.0-rc.1", "v0.6.0", false},
		{"v0.6.0", "dev", false},
		{"v0.6.0", "v0.5.0 (abc1234)", true},
	}
	for _, c := range casos {
		if got := actualizacion.MasNueva(c.otra, c.actual); got != c.quiero {
			t.Errorf("MasNueva(%q, %q) = %v", c.otra, c.actual, got)
		}
	}
}

func TestComoActualizar(t *testing.T) {
	casos := map[string]string{
		`C:\Users\ana\scoop\apps\marga\current\marga.exe`:                     "scoop update marga",
		`C:\Users\ana\AppData\Local\Microsoft\WinGet\Packages\zara\marga.exe`: "winget upgrade zara-te05.Marga",
		"/opt/homebrew/Cellar/marga/0.6.0/bin/marga":                          "brew upgrade marga",
		`C:\Users\ana\go\bin\marga.exe`:                                       "go install github.com/zara-te05/Marga/cmd/marga@latest",
		`C:\herramientas\marga.exe`:                                           "",
	}
	for ruta, quiero := range casos {
		if got := actualizacion.ComoActualizar(ruta); got != quiero {
			t.Errorf("ComoActualizar(%q) = %q, quiero %q", ruta, got, quiero)
		}
	}
}

func TestUltima(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.7.0","html_url":"https://github.com/zara-te05/Marga/releases/tag/v0.7.0"}`))
	}))
	defer srv.Close()
	antes := actualizacion.URLUltima
	actualizacion.URLUltima = srv.URL
	defer func() { actualizacion.URLUltima = antes }()
	r, err := actualizacion.Ultima(context.Background(), time.Second)
	if err != nil || r.Version != "v0.7.0" {
		t.Fatalf("%+v %v", r, err)
	}
}
