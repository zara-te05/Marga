package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zarat/Marga-Sun/server/internal/security"
	"github.com/zarat/Marga-Sun/server/internal/store"
)

func hashToken(t string) string { return security.HashToken(t) }

// ---------------------------------------------------------------- Salud

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.DB.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "bd": "sin conexión"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bd": "conectada"})
}

// ---------------------------------------------------------------- Auth

type credenciales struct {
	Nombre   string `json:"nombre"`
	Email    string `json:"email"`
	Password string `json:"password"`
	EquipoID *int64 `json:"equipo_id"`
}

func (s *Server) iniciarSesion(w http.ResponseWriter, r *http.Request, u store.Usuario, status int) {
	tok, err := security.NewToken()
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.CrearSesion(r.Context(), security.HashToken(tok), u.ID, s.cfg.SessionTTL); err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieSesion, Value: tok, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.SecureCookie,
		MaxAge: int(s.cfg.SessionTTL.Seconds()),
	})
	// El token también va en el cuerpo para clientes sin cookies (la CLI de Marga Moon).
	writeJSON(w, status, map[string]any{"usuario": u, "token": tok, "expira_en": time.Now().Add(s.cfg.SessionTTL)})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var c credenciales
	if !decode(w, r, &c) {
		return
	}
	if err := security.ValidarPassword(c.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := security.HashPassword(c.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	u, err := s.store.CrearUsuario(r.Context(), c.Nombre, c.Email, hash, c.EquipoID)
	if err != nil {
		s.fail(w, err)
		return
	}
	_ = store.Auditar(r.Context(), s.store.DB, u.ID, nil, "usuario.registrar", map[string]any{"email": u.Email})
	s.iniciarSesion(w, r, u, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credenciales
	if !decode(w, r, &c) {
		return
	}
	u, err := s.store.UsuarioPorEmail(r.Context(), c.Email)
	if err != nil && !errors.Is(err, store.ErrNoEncontrado) {
		s.fail(w, err)
		return
	}
	// Mismo mensaje y coste aunque el email no exista (no filtra usuarios).
	ok := security.CheckPassword(u.PasswordHash, c.Password)
	if err != nil {
		security.CheckPassword("pbkdf2-sha256$600000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", c.Password)
	}
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "email o contraseña incorrectos")
		return
	}
	_ = store.Auditar(r.Context(), s.store.DB, u.ID, nil, "usuario.login", nil)
	s.iniciarSesion(w, r, u, http.StatusOK)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if tok := tokenDe(r); tok != "" {
		_ = s.store.BorrarSesion(r.Context(), security.HashToken(tok))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieSesion, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, usuario(r))
}

// ---------------------------------------------------------------- Equipos

func (s *Server) listarEquipos(w http.ResponseWriter, r *http.Request) {
	eq, err := s.store.ListarEquipos(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, eq)
}

func (s *Server) crearEquipo(w http.ResponseWriter, r *http.Request) {
	if usuario(r).Rol != "admin" {
		writeError(w, http.StatusForbidden, "solo un admin puede crear equipos")
		return
	}
	var body struct {
		Nombre string `json:"nombre"`
	}
	if !decode(w, r, &body) {
		return
	}
	eq, err := s.store.CrearEquipo(r.Context(), body.Nombre)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, eq)
}

// ---------------------------------------------------------------- Repositorios

func (s *Server) listarRepos(w http.ResponseWriter, r *http.Request) {
	rs, err := s.store.ListarRepos(r.Context(), usuario(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (s *Server) crearRepo(w http.ResponseWriter, r *http.Request) {
	if usuario(r).Rol == "lector" {
		writeError(w, http.StatusForbidden, "los usuarios lectores no pueden crear proyectos")
		return
	}
	var d store.DatosRepo
	if !decode(w, r, &d) {
		return
	}
	repo, err := s.store.CrearRepo(r.Context(), usuario(r), d)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) obtenerRepo(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.ObtenerRepo(r.Context(), usuario(r), repoID(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) actualizarRepo(w http.ResponseWriter, r *http.Request) {
	var d store.DatosRepo
	if !decode(w, r, &d) {
		return
	}
	repo, err := s.store.ActualizarRepo(r.Context(), usuario(r), repoID(r), d)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) eliminarRepo(w http.ResponseWriter, r *http.Request) {
	if err := s.store.EliminarRepo(r.Context(), usuario(r), repoID(r)); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- Colaboradores

func (s *Server) listarColaboradores(w http.ResponseWriter, r *http.Request) {
	cs, err := s.store.ListarColaboradores(r.Context(), repoID(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *Server) agregarColaborador(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email   string `json:"email"`
		Permiso string `json:"permiso"`
	}
	if !decode(w, r, &body) {
		return
	}
	c, err := s.store.AgregarColaborador(r.Context(), usuario(r), repoID(r), body.Email, body.Permiso)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) quitarColaborador(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id de usuario inválido")
		return
	}
	if err := s.store.QuitarColaborador(r.Context(), usuario(r), repoID(r), uid); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- Branches

func (s *Server) listarBranches(w http.ResponseWriter, r *http.Request) {
	bs, err := s.store.ListarBranches(r.Context(), repoID(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

func (s *Server) crearBranch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nombre string `json:"nombre"`
		Desde  string `json:"desde"`
	}
	if !decode(w, r, &body) {
		return
	}
	b, err := s.store.CrearBranch(r.Context(), usuario(r), repoID(r), strings.TrimSpace(body.Nombre), body.Desde)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) eliminarBranch(w http.ResponseWriter, r *http.Request) {
	if err := s.store.EliminarBranch(r.Context(), usuario(r), repoID(r), r.PathValue("nombre")); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- Commits

func (s *Server) listarCommits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	h, err := s.store.ListarCommits(r.Context(), repoID(r), q.Get("branch"),
		store.ParseLimite(q.Get("limit"), 50, 500), store.ParseLimite(q.Get("offset"), 0, 1_000_000))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) crearCommit(w http.ResponseWriter, r *http.Request) {
	var n store.NuevoCommit
	if !decode(w, r, &n) {
		return
	}
	c, err := s.store.CrearCommit(r.Context(), usuario(r), repoID(r), n)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) obtenerCommit(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.ObtenerCommit(r.Context(), repoID(r), r.PathValue("hash"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) diffCommit(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.DiffDeCommit(r.Context(), repoID(r), r.PathValue("hash"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) revertirCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Branch string `json:"branch"`
	}
	if r.ContentLength > 0 && !decode(w, r, &body) {
		return
	}
	c, err := s.store.Revertir(r.Context(), usuario(r), repoID(r), r.PathValue("hash"), body.Branch)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("to") == "" {
		writeError(w, http.StatusBadRequest, "indica ?to=<hash> (y opcionalmente ?from=<hash>)")
		return
	}
	d, err := s.store.Diff(r.Context(), repoID(r), q.Get("from"), q.Get("to"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) buscar(w http.ResponseWriter, r *http.Request) {
	res, err := s.store.Buscar(r.Context(), repoID(r), r.URL.Query().Get("q"), 30)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) auditoria(w http.ResponseWriter, r *http.Request) {
	ev, err := s.store.ListarAuditoria(r.Context(), repoID(r), store.ParseLimite(r.URL.Query().Get("limit"), 100, 1000))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) esquema(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.EsquemaActual(r.Context(), repoID(r), r.URL.Query().Get("ref"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}
