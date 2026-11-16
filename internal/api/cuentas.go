package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zara-te05/Marga/internal/correo"
	"github.com/zara-te05/Marga/internal/security"
	"github.com/zara-te05/Marga/internal/store"
)

func itoa(n int) string { return strconv.Itoa(n) }

const ttlRestablecer = time.Hour

// EnlaceRestablecer crea un enlace de un solo uso para cambiar la contraseña.
// Lo usan POST /api/auth/recuperar y `marga-server -restablecer EMAIL`.
func EnlaceRestablecer(ctx context.Context, st *store.Store, base string, u store.Usuario) (string, error) {
	tok, err := security.NewToken()
	if err != nil {
		return "", err
	}
	if err := st.CrearRestablecimiento(ctx, u.ID, security.HashToken(tok), ttlRestablecer); err != nil {
		return "", err
	}
	// El token va después de # para que no llegue a los logs de ningún servidor ni proxy.
	return strings.TrimRight(base, "/") + "/pages/restablecer.html#token=" + tok, nil
}

// URLPublica es la dirección con la que la gente abre Marga (para los enlaces de correo).
// No se arma con la cabecera Host de la petición: un atacante podría falsearla y
// recibir el enlace de otra persona.
func (s *Server) URLPublica() string {
	if s.cfg.URLPublica != "" {
		return s.cfg.URLPublica
	}
	if strings.HasPrefix(s.cfg.Addr, ":") {
		return "http://localhost" + s.cfg.Addr
	}
	return "http://" + s.cfg.Addr
}

func (s *Server) recuperar(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &body) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	ip := s.ipDe(r)
	if demasiados(w, s.lim.recuperarIP, ip, s.lim.recuperarEmail, email) {
		return
	}
	s.lim.recuperarIP.Registrar(ip)
	s.lim.recuperarEmail.Registrar(email)

	// La respuesta es la misma exista o no la cuenta (no revela quién está registrado),
	// y el envío va aparte para que tampoco lo delate el tiempo de respuesta.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := s.store.UsuarioPorEmail(ctx, email)
		if err != nil {
			if !errors.Is(err, store.ErrNoEncontrado) {
				log.Printf("recuperar: %v", err)
			}
			return
		}
		enlace, err := EnlaceRestablecer(ctx, s.store, s.URLPublica(), u)
		if err != nil {
			log.Printf("recuperar: %v", err)
			return
		}
		if !s.cfg.SMTP.Activo() {
			log.Printf("⚠ SMTP sin configurar. Enlace para restablecer la contraseña de %s (vale 1 hora):\n    %s", u.Email, enlace)
			return
		}
		cuerpo := "Hola " + u.Nombre + ",\n\n" +
			"Alguien (ojalá tú) pidió restablecer tu contraseña de Marga. Abre este enlace para elegir una nueva:\n\n" +
			enlace + "\n\nEl enlace vale una hora y solo se puede usar una vez. Si no lo pediste, ignora este correo: tu contraseña no cambia.\n"
		if err := correo.Enviar(s.cfg.SMTP, u.Email, "Restablece tu contraseña de Marga", cuerpo); err != nil {
			log.Printf("✗ no se pudo enviar el correo a %s: %v", u.Email, err)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mensaje": "Si ese email tiene cuenta, te enviamos un enlace para restablecer la contraseña."})
}

func (s *Server) restablecer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	ip := s.ipDe(r)
	if demasiados(w, s.lim.recuperarIP, ip) {
		return
	}
	if err := security.ValidarPassword(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := security.HashPassword(body.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	u, err := s.store.Restablecer(r.Context(), security.HashToken(strings.TrimSpace(body.Token)), hash)
	if err != nil {
		if errors.Is(err, store.ErrInvalido) {
			s.lim.recuperarIP.Registrar(ip) // probar tokens al azar también cuenta
		}
		s.fail(w, err)
		return
	}
	s.lim.loginEmail.Limpiar(strings.ToLower(u.Email))
	s.iniciarSesion(w, r, u, http.StatusOK)
}

func (s *Server) cambiarPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Actual string `json:"actual"`
		Nueva  string `json:"nueva"`
	}
	if !decode(w, r, &body) {
		return
	}
	tok := tokenDe(r)
	if strings.HasPrefix(tok, store.PrefijoToken) {
		writeError(w, http.StatusForbidden, "la contraseña se cambia desde la web, no con un token de acceso")
		return
	}
	u := usuario(r)
	clave := strconv.FormatInt(u.ID, 10)
	if demasiados(w, s.lim.cambioPassword, clave) {
		return
	}
	if !security.CheckPassword(u.PasswordHash, body.Actual) {
		s.lim.cambioPassword.Registrar(clave)
		writeError(w, http.StatusBadRequest, "la contraseña actual no es correcta")
		return
	}
	if err := security.ValidarPassword(body.Nueva); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := security.HashPassword(body.Nueva)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.CambiarPassword(r.Context(), u.ID, hash, security.HashToken(tok)); err != nil {
		s.fail(w, err)
		return
	}
	s.lim.cambioPassword.Limpiar(clave)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// cabeceras de seguridad para todas las respuestas.
func (s *Server) cabeceras(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src 'self' https://fonts.gstatic.com",
		"img-src 'self' data:",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}, "; ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", csp)
		if s.cfg.SecureCookie { // detrás de HTTPS
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
