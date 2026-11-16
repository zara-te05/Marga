package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limitador cuenta eventos por clave (IP, email…) en una ventana deslizante.
// Vive en memoria: con una sola instancia de marga-server es suficiente.
type limitador struct {
	mu      sync.Mutex
	max     int
	ventana time.Duration
	Eventos map[string][]time.Time
	Ahora   func() time.Time
}

func NuevoLimitador(max int, ventana time.Duration) *limitador {
	return &limitador{max: max, ventana: ventana, Eventos: map[string][]time.Time{}, Ahora: time.Now}
}

// vigentes descarta lo que salió de la ventana. Debe llamarse con mu tomado.
func (l *limitador) vigentes(clave string) []time.Time {
	limite := l.Ahora().Add(-l.ventana)
	ev := l.Eventos[clave]
	i := 0
	for i < len(ev) && !ev[i].After(limite) {
		i++
	}
	ev = ev[i:]
	if len(ev) == 0 {
		delete(l.Eventos, clave)
	} else {
		l.Eventos[clave] = ev
	}
	return ev
}

// Espera devuelve 0 si se permite otro intento, o cuánto falta para el siguiente.
func (l *limitador) Espera(clave string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	ev := l.vigentes(clave)
	if len(ev) < l.max {
		return 0
	}
	return ev[0].Add(l.ventana).Sub(l.Ahora())
}

func (l *limitador) Registrar(clave string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Eventos[clave] = append(l.vigentes(clave), l.Ahora())
	if len(l.Eventos) > 50_000 { // que un ataque con muchas claves no agote la memoria
		for k := range l.Eventos {
			l.vigentes(k)
		}
	}
}

func (l *limitador) Limpiar(clave string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.Eventos, clave)
}

// limites agrupa los limitadores del servidor.
type limites struct {
	loginEmail, loginIP *limitador // intentos fallidos
	registroIP          *limitador
	recuperarIP         *limitador
	recuperarEmail      *limitador
	cambioPassword      *limitador // intentos fallidos con la contraseña actual
}

func nuevosLimites() *limites {
	return &limites{
		loginEmail:     NuevoLimitador(8, 15*time.Minute),
		loginIP:        NuevoLimitador(40, 15*time.Minute),
		registroIP:     NuevoLimitador(10, time.Hour),
		recuperarIP:    NuevoLimitador(10, time.Hour),
		recuperarEmail: NuevoLimitador(3, time.Hour),
		cambioPassword: NuevoLimitador(5, 15*time.Minute),
	}
}

// demasiados responde 429 si alguno de los pares (limitador, clave) está agotado.
func demasiados(w http.ResponseWriter, pares ...any) bool {
	var espera time.Duration
	for i := 0; i+1 < len(pares); i += 2 {
		if e := pares[i].(*limitador).Espera(pares[i+1].(string)); e > espera {
			espera = e
		}
	}
	if espera <= 0 {
		return false
	}
	min := int(espera.Minutes()) + 1
	w.Header().Set("Retry-After", itoa(int(espera.Seconds())+1))
	msg := "demasiados intentos; espera 1 minuto"
	if min > 1 {
		msg = "demasiados intentos; espera " + itoa(min) + " minutos"
	}
	writeError(w, http.StatusTooManyRequests, msg)
	return true
}

// ipDe devuelve la IP del cliente. Detrás de un proxy (Caddy, nginx) solo se
// confía en X-Forwarded-For si MARGA_CONFIAR_PROXY=true; si no, cualquiera podría
// inventarse una IP distinta en cada intento.
func (s *Server) ipDe(r *http.Request) string {
	if s.cfg.ConfiarProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			partes := strings.Split(xff, ",")
			return strings.TrimSpace(partes[len(partes)-1]) // la que agregó nuestro proxy
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
