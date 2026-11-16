// Package api expone la API REST de Marga y sirve el front (index.html, pages/, assets/).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/zara-te05/Marga/internal/config"
	"github.com/zara-te05/Marga/internal/store"
	"github.com/zara-te05/Marga/web"
)

type Server struct {
	cfg   config.Config
	store *store.Store
	lim   *limites
}

func New(cfg config.Config, st *store.Store) *Server {
	return &Server{cfg: cfg, store: st, lim: nuevosLimites()}
}

// Handler arma el router completo (Go 1.22+: patrones con método y {comodines}).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- Salud y autenticación
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.auth(s.me))
	mux.HandleFunc("POST /api/auth/recuperar", s.recuperar)
	mux.HandleFunc("POST /api/auth/restablecer", s.restablecer)
	mux.HandleFunc("POST /api/auth/password", s.auth(s.cambiarPassword))

	// --- Tokens de acceso personales (CLI / CI)
	mux.HandleFunc("GET /api/tokens", s.auth(s.listarTokens))
	mux.HandleFunc("POST /api/tokens", s.auth(s.crearToken))
	mux.HandleFunc("DELETE /api/tokens/{id}", s.auth(s.revocarToken)) // {id} o "actual"

	// --- Equipos
	mux.HandleFunc("GET /api/equipos", s.listarEquipos) // público: lo usa el registro
	mux.HandleFunc("POST /api/equipos", s.auth(s.crearEquipo))

	// --- Repositorios (proyectos)
	mux.HandleFunc("GET /api/repos", s.auth(s.listarRepos))
	mux.HandleFunc("POST /api/repos", s.auth(s.crearRepo))
	mux.HandleFunc("GET /api/repos/{id}", s.repo("lectura", s.obtenerRepo))
	mux.HandleFunc("PATCH /api/repos/{id}", s.repo("admin", s.actualizarRepo))
	mux.HandleFunc("DELETE /api/repos/{id}", s.repo("admin", s.eliminarRepo))

	// --- Colaboradores
	mux.HandleFunc("GET /api/repos/{id}/colaboradores", s.repo("lectura", s.listarColaboradores))
	mux.HandleFunc("POST /api/repos/{id}/colaboradores", s.repo("admin", s.agregarColaborador))
	mux.HandleFunc("DELETE /api/repos/{id}/colaboradores/{uid}", s.repo("admin", s.quitarColaborador))

	// --- Branches
	mux.HandleFunc("GET /api/repos/{id}/branches", s.repo("lectura", s.listarBranches))
	mux.HandleFunc("POST /api/repos/{id}/branches", s.repo("escritura", s.crearBranch))
	mux.HandleFunc("DELETE /api/repos/{id}/branches/{nombre...}", s.repo("escritura", s.eliminarBranch))

	// --- Commits / migraciones
	mux.HandleFunc("GET /api/repos/{id}/commits", s.repo("lectura", s.listarCommits))
	mux.HandleFunc("POST /api/repos/{id}/commits", s.repo("escritura", s.crearCommit))
	mux.HandleFunc("GET /api/repos/{id}/commits/{hash}", s.repo("lectura", s.obtenerCommit))
	mux.HandleFunc("GET /api/repos/{id}/commits/{hash}/diff", s.repo("lectura", s.diffCommit))
	mux.HandleFunc("POST /api/repos/{id}/commits/{hash}/revert", s.repo("escritura", s.revertirCommit))
	mux.HandleFunc("GET /api/repos/{id}/diff", s.repo("lectura", s.diff))
	mux.HandleFunc("GET /api/repos/{id}/buscar", s.repo("lectura", s.buscar))
	mux.HandleFunc("GET /api/repos/{id}/esquema", s.repo("lectura", s.esquema))
	mux.HandleFunc("GET /api/repos/{id}/auditoria", s.repo("admin", s.auditoria))

	// --- Entornos (estado real que reporta la CLI)
	mux.HandleFunc("GET /api/repos/{id}/entornos", s.repo("lectura", s.listarEntornos))
	mux.HandleFunc("GET /api/repos/{id}/entornos/{nombre}", s.repo("lectura", s.obtenerEntorno))
	mux.HandleFunc("PUT /api/repos/{id}/entornos/{nombre}", s.repo("escritura", s.reportarEntorno))
	mux.HandleFunc("DELETE /api/repos/{id}/entornos/{nombre}", s.repo("admin", s.eliminarEntorno))
	mux.HandleFunc("PATCH /api/repos/{id}/entornos/{nombre}", s.repo("admin", s.configurarEntorno))

	// --- Revisiones y fusión de ramas
	mux.HandleFunc("POST /api/repos/{id}/commits/{hash}/revisiones", s.repo("escritura", s.revisar))
	mux.HandleFunc("DELETE /api/repos/{id}/commits/{hash}/revisiones", s.repo("escritura", s.quitarRevision))
	mux.HandleFunc("POST /api/repos/{id}/fusionar", s.repo("escritura", s.fusionar))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "ruta de API no encontrada")
	})

	// --- Front estático (solo index.html, pages/ y assets/; nunca server/ ni .env)
	s.static(mux)

	return chain(mux, recoverer, logger, s.cabeceras, csrfGuard)
}

// ---------------------------------------------------------------- Middleware

type middleware func(http.Handler) http.Handler

func chain(h http.Handler, ms ...middleware) http.Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		h = ms[i](h)
	}
	return h
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }

func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("PANIC %v\n%s", v, debug.Stack())
				writeError(w, http.StatusInternalServerError, "error interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// csrfGuard: las peticiones que modifican datos deben ser JSON o llevar
// Authorization. Un formulario de otro sitio no puede enviar ninguna de las dos
// sin pasar por CORS, así que la cookie de sesión no se puede abusar.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") && r.Header.Get("Authorization") == "" {
				writeError(w, http.StatusUnsupportedMediaType, "usa Content-Type: application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- Contexto de usuario / repo

type ctxKey int

const (
	keyUsuario ctxKey = iota
	keyRepo
	keyPermiso
)

func usuario(r *http.Request) store.Usuario { return r.Context().Value(keyUsuario).(store.Usuario) }
func repoID(r *http.Request) int64          { return r.Context().Value(keyRepo).(int64) }
func permiso(r *http.Request) string        { return r.Context().Value(keyPermiso).(string) }

const cookieSesion = "marga_sesion"

func tokenDe(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie(cookieSesion); err == nil {
		return c.Value
	}
	return ""
}

// auth exige sesión válida (cookie del navegador o Bearer de la CLI).
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := tokenDe(r)
		if tok == "" {
			writeError(w, http.StatusUnauthorized, "inicia sesión para continuar")
			return
		}
		var u store.Usuario
		var err error
		if strings.HasPrefix(tok, store.PrefijoToken) { // token personal de la CLI
			u, err = s.store.UsuarioDeToken(r.Context(), hashToken(tok))
		} else {
			u, err = s.store.UsuarioDeSesion(r.Context(), hashToken(tok))
		}
		if err != nil {
			if errors.Is(err, store.ErrNoEncontrado) {
				writeError(w, http.StatusUnauthorized, "sesión expirada o token inválido")
				return
			}
			s.fail(w, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), keyUsuario, u)))
	}
}

// repo = auth + verifica que el usuario tenga al menos `minimo` sobre {id}.
func (s *Server) repo(minimo string, h http.HandlerFunc) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id de repositorio inválido")
			return
		}
		p, err := s.store.Permiso(r.Context(), usuario(r), id)
		if err != nil {
			s.fail(w, err)
			return
		}
		if p == "" { // no revelamos que existe
			writeError(w, http.StatusNotFound, "repositorio no encontrado")
			return
		}
		if !store.Alcanza(p, minimo) {
			writeError(w, http.StatusForbidden, "necesitas permiso de "+minimo+" en este repositorio")
			return
		}
		ctx := context.WithValue(r.Context(), keyRepo, id)
		ctx = context.WithValue(ctx, keyPermiso, p)
		h(w, r.WithContext(ctx))
	})
}

// ---------------------------------------------------------------- Respuestas

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail traduce errores del store a códigos HTTP.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var se *store.Error
	msg := err.Error()
	switch {
	case errors.Is(err, store.ErrNoEncontrado):
		if !errors.As(err, &se) {
			msg = "no encontrado"
		}
		writeError(w, http.StatusNotFound, msg)
	case errors.Is(err, store.ErrConflicto):
		writeError(w, http.StatusConflict, msg)
	case errors.Is(err, store.ErrInvalido):
		writeError(w, http.StatusBadRequest, msg)
	case errors.Is(err, store.ErrProhibido):
		writeError(w, http.StatusForbidden, msg)
	default:
		log.Printf("error: %v", err)
		writeError(w, http.StatusInternalServerError, "error interno del servidor")
	}
}

const maxBody = 5 << 20 // 5 MB: suficiente para migraciones grandes

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return false
	}
	return true
}

// ---------------------------------------------------------------- Front estático

func (s *Server) static(mux *http.ServeMux) {
	var front fs.FS = web.FS
	if dir := s.cfg.WebDir; dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "pages")); err != nil {
			log.Printf("⚠ no encuentro el front en %q; uso el embebido", dir)
		} else {
			front = os.DirFS(dir)
		}
	}
	files := http.FileServerFS(front)
	noCache := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			h.ServeHTTP(w, r)
		})
	}
	mux.Handle("GET /pages/", noCache(files))
	mux.Handle("GET /assets/", noCache(files))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pages/proyectos.html", http.StatusFound)
	})
	mux.HandleFunc("GET /index.html", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pages/proyectos.html", http.StatusFound)
	})
}
