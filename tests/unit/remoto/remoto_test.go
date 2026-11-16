package remoto_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/datos/remoto"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
)

// Lo que encontramos en las pruebas manuales: en el puerto había otra cosa (o la
// web sola) y respondía 404 en texto plano. El error debe decir qué pasa y qué hacer.
func TestNoEsServidorDeMarga(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler()) // "404 page not found", sin JSON
	defer srv.Close()
	_, err := remoto.Nuevo(srv.URL, "").CrearRepo("Tienda", "postgresql")
	if err == nil || !strings.Contains(err.Error(), "no hay un servidor de Marga") {
		t.Fatalf("error: %v", err)
	}
	if p := sugerencia.De(err); !strings.Contains(p, "marga-server") || !strings.Contains(p, "/api/health") {
		t.Fatalf("sugerencia: %q", p)
	}
}

func TestServidorApagado(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // ya no escucha
	_, err := remoto.Nuevo(url, "").Salud()
	var re *remoto.Error
	if err == nil || !strings.Contains(err.Error(), "no hay conexión") {
		t.Fatalf("error: %v", err)
	}
	if ok := asError(err, &re); !ok || !re.SinConexion() {
		t.Fatalf("debía ser SinConexion: %#v", err)
	}
	if p := sugerencia.De(err); !strings.Contains(p, "marga-server") || !strings.Contains(p, "--local") {
		t.Fatalf("sugerencia: %q", p)
	}
}

func TestServidorViejoYSesionVencida(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth/me" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"token inválido"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"ruta de API no encontrada"}`))
	}))
	defer srv.Close()
	_, err := remoto.Nuevo(srv.URL, "tok").Repos()
	if p := sugerencia.De(err); !strings.Contains(p, "actualiza marga-server") {
		t.Fatalf("servidor viejo: %v / %q", err, p)
	}
	_, err = remoto.Nuevo(srv.URL, "tok").Yo()
	if p := sugerencia.De(err); !strings.Contains(p, "marga login --servidor "+srv.URL) {
		t.Fatalf("sesión vencida: %v / %q", err, p)
	}
}

func asError(err error, re **remoto.Error) bool {
	e, ok := err.(*remoto.Error)
	*re = e
	return ok
}
