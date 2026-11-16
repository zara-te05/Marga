package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zara-te05/Marga/internal/config"
	"github.com/zara-te05/Marga/internal/database"
	"github.com/zara-te05/Marga/internal/pruebabd"
	"github.com/zara-te05/Marga/internal/store"
)

// servidor levanta Marga completo (BD nueva + migraciones + HTTP) para una prueba.
func servidor(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	url := pruebabd.Nueva(t)
	ctx := context.Background()
	db, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db, time.Hour)
	cfg := config.Config{SessionTTL: time.Hour, DiffTTL: time.Hour}
	srv := httptest.NewServer(New(cfg, st).Handler())
	t.Cleanup(srv.Close)
	return srv, st
}

type cliente struct {
	t     *testing.T
	base  string
	token string
}

// pedir hace una petición JSON y decodifica la respuesta en out (si no es nil).
func (c *cliente) pedir(metodo, ruta string, body any, quiero int, out any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(metodo, c.base+ruta, r)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != quiero {
		c.t.Fatalf("%s %s: %d (esperaba %d)\n%s", metodo, ruta, res.StatusCode, quiero, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: respuesta no es JSON: %v\n%s", metodo, ruta, err, data)
		}
	}
}

func registrar(t *testing.T, base, nombre, email string) *cliente {
	t.Helper()
	c := &cliente{t: t, base: base}
	var r struct{ Token string }
	c.pedir("POST", "/api/auth/register", map[string]string{"nombre": nombre, "email": email, "password": "clave-segura-1"}, 201, &r)
	c.token = r.Token
	return c
}

type commit struct {
	Hash         string `json:"hash"`
	Mensaje      string `json:"mensaje"`
	Checksum     string `json:"checksum"`
	Aprobaciones int    `json:"aprobaciones"`
	Riesgo       []struct {
		Regla string `json:"regla"`
	} `json:"riesgo"`
}

func (c *cliente) commit(repo int64, rama, mensaje, up, down string) commit {
	c.t.Helper()
	var cm commit
	c.pedir("POST", fmt.Sprintf("/api/repos/%d/commits", repo),
		map[string]any{"branch": rama, "mensaje": mensaje, "sql_up": up, "sql_down": down}, 201, &cm)
	return cm
}

func (c *cliente) log(repo int64, rama string) []commit {
	c.t.Helper()
	var h struct{ Commits []commit }
	c.pedir("GET", fmt.Sprintf("/api/repos/%d/commits?branch=%s", repo, rama), nil, 200, &h)
	return h.Commits
}

type resultadoMerge struct {
	Tipo       string `json:"tipo"`
	Aplicado   bool   `json:"aplicado"`
	Conflictos []struct {
		Tabla string `json:"tabla"`
	} `json:"conflictos"`
	Renumeradas []struct{ Antes, Despues string } `json:"renumeradas"`
}

func (c *cliente) fusionar(repo int64, desde, hacia string, simular, forzar bool) resultadoMerge {
	c.t.Helper()
	var r resultadoMerge
	c.pedir("POST", fmt.Sprintf("/api/repos/%d/fusionar", repo),
		map[string]any{"desde": desde, "hacia": hacia, "simular": simular, "forzar": forzar}, 200, &r)
	return r
}

func TestFlujoCompleto(t *testing.T) {
	srv, _ := servidor(t)
	ana := registrar(t, srv.URL, "Ana", "ana@ejemplo.com")
	beto := registrar(t, srv.URL, "Beto", "beto@ejemplo.com")
	anonimo := &cliente{t: t, base: srv.URL}

	// --- Sesión y permisos básicos
	var yo struct{ Usuario struct{ Rol string } }
	var me map[string]any
	ana.pedir("GET", "/api/auth/me", nil, 200, &me)
	_ = yo
	anonimo.pedir("GET", "/api/repos", nil, 401, nil)
	anonimo.pedir("POST", "/api/auth/login", map[string]string{"email": "ana@ejemplo.com", "password": "mala"}, 401, nil)
	ana.pedir("POST", "/api/auth/register", map[string]string{"nombre": "Otra", "email": "ana@ejemplo.com", "password": "clave-segura-1"}, 409, nil)

	// --- Proyecto
	var repo struct{ ID int64 }
	ana.pedir("POST", "/api/repos", map[string]string{"nombre": "Tienda", "motor": "postgresql", "visibilidad": "privado"}, 201, &repo)
	r := repo.ID
	beto.pedir("GET", fmt.Sprintf("/api/repos/%d", r), nil, 404, nil) // privado: no lo ve

	// --- Historial en main
	c1 := ana.commit(r, "main", "0001_clientes", "CREATE TABLE clientes (id serial PRIMARY KEY, nombre text);", "DROP TABLE clientes;")
	ana.commit(r, "main", "0002_pedidos", "CREATE TABLE pedidos (id serial PRIMARY KEY, cliente_id int);", "DROP TABLE pedidos;")
	if l := ana.log(r, "main"); len(l) != 2 || l[0].Mensaje != "0002_pedidos" {
		t.Fatalf("log de main: %+v", l)
	}
	if c1.Checksum == "" {
		t.Fatal("el commit debe traer checksum")
	}
	// Prefijo de hash y nombre de migración resuelven el mismo commit.
	var porPrefijo, porNombre commit
	ana.pedir("GET", fmt.Sprintf("/api/repos/%d/commits/%s", r, c1.Hash[:8]), nil, 200, &porPrefijo)
	ana.pedir("GET", fmt.Sprintf("/api/repos/%d/commits/0001_clientes", r), nil, 200, &porNombre)
	if porPrefijo.Hash != c1.Hash || porNombre.Hash != c1.Hash {
		t.Fatal("no resolvió el commit por prefijo o por nombre")
	}
	// padre_esperado viejo: 409 (como un push no fast-forward).
	ana.pedir("POST", fmt.Sprintf("/api/repos/%d/commits", r), map[string]any{
		"branch": "main", "mensaje": "0003_x", "sql_up": "SELECT 1;", "padre_esperado": c1.Hash}, 409, nil)

	// Análisis de riesgo incluido en el commit.
	peligroso := ana.commit(r, "main", "0003_limpieza", "DROP TABLE pedidos;", "")
	if len(peligroso.Riesgo) == 0 || peligroso.Riesgo[0].Regla != "borra-tabla" {
		t.Fatalf("esperaba borra-tabla: %+v", peligroso.Riesgo)
	}

	// --- Revisiones: nadie aprueba lo suyo; Beto necesita ser colaborador.
	ana.pedir("POST", fmt.Sprintf("/api/repos/%d/colaboradores", r), map[string]string{"email": "beto@ejemplo.com", "permiso": "escritura"}, 201, nil)
	ruta := fmt.Sprintf("/api/repos/%d/commits/%s/revisiones", r, peligroso.Hash)
	ana.pedir("POST", ruta, map[string]string{"estado": "aprobado"}, 403, nil)
	beto.pedir("POST", ruta, map[string]string{"estado": "cambios"}, 400, nil) // pedir cambios exige comentario
	beto.pedir("POST", ruta, map[string]string{"estado": "aprobado", "comentario": "ok"}, 200, nil)
	var revisado commit
	ana.pedir("GET", fmt.Sprintf("/api/repos/%d/commits/%s", r, peligroso.Hash), nil, 200, &revisado)
	if revisado.Aprobaciones != 1 {
		t.Fatalf("aprobaciones = %d", revisado.Aprobaciones)
	}

	// --- Ramas y fusión
	rama := func(nombre, desde string) {
		ana.pedir("POST", fmt.Sprintf("/api/repos/%d/branches", r), map[string]string{"nombre": nombre, "desde": desde}, 201, nil)
	}

	// fast-forward: la rama avanzó y main no.
	rama("feature/ff", "main")
	ana.commit(r, "feature/ff", "0004_productos", "CREATE TABLE productos (id serial PRIMARY KEY);", "DROP TABLE productos;")
	if m := ana.fusionar(r, "feature/ff", "main", true, false); m.Tipo != "fast_forward" || m.Aplicado {
		t.Fatalf("simulación ff: %+v", m)
	}
	if len(ana.log(r, "main")) != 3 {
		t.Fatal("la simulación no debe cambiar main")
	}
	if m := ana.fusionar(r, "feature/ff", "main", false, false); m.Tipo != "fast_forward" || !m.Aplicado {
		t.Fatalf("ff: %+v", m)
	}
	if m := ana.fusionar(r, "feature/ff", "main", false, false); m.Tipo != "al_dia" {
		t.Fatalf("segunda vez debe estar al día: %+v", m)
	}

	// conflicto: ambas ramas tocan clientes.
	rama("feature/rfc", "main")
	ana.commit(r, "feature/rfc", "0005_rfc", "ALTER TABLE clientes ADD COLUMN rfc text;", "ALTER TABLE clientes DROP COLUMN rfc;")
	ana.commit(r, "main", "0005_email", "ALTER TABLE clientes ADD COLUMN email text;", "ALTER TABLE clientes DROP COLUMN email;")
	m := ana.fusionar(r, "feature/rfc", "main", false, false)
	if m.Tipo != "conflicto" || m.Aplicado || len(m.Conflictos) != 1 || m.Conflictos[0].Tabla != "clientes" {
		t.Fatalf("esperaba conflicto en clientes: %+v", m)
	}
	// --forzar: se copia encima y se renumera 0005 → 0006.
	m = ana.fusionar(r, "feature/rfc", "main", false, true)
	if m.Tipo != "copia" || !m.Aplicado || len(m.Renumeradas) != 1 || !strings.HasPrefix(m.Renumeradas[0].Despues, "0006") {
		t.Fatalf("esperaba copia renumerada: %+v", m)
	}
	main := ana.log(r, "main")
	if !strings.HasPrefix(main[0].Mensaje, "0006_rfc") {
		t.Fatalf("la punta de main debería ser 0006_rfc: %q", main[0].Mensaje)
	}
	// Volver a fusionar no duplica (la identidad es el checksum).
	if m := ana.fusionar(r, "feature/rfc", "main", false, false); m.Tipo != "al_dia" {
		t.Fatalf("re-fusión: %+v", m)
	}

	// --- Entornos: la CLI reporta qué tiene aplicado; el servidor calcula lo pendiente.
	ana.pedir("PUT", fmt.Sprintf("/api/repos/%d/entornos/staging", r), map[string]any{
		"motor": "postgresql", "compartido": true,
		"aplicadas": []map[string]any{{"version": "0001", "nombre": "clientes", "checksum": c1.Checksum, "aplicada_en": time.Now(), "aplicada_por": "ana"}},
		"drift":     []string{},
	}, 200, nil)
	var ents []struct {
		Nombre     string
		Aplicadas  int
		Pendientes int
	}
	ana.pedir("GET", fmt.Sprintf("/api/repos/%d/entornos?rama=main", r), nil, 200, &ents)
	if len(ents) != 1 || ents[0].Aplicadas != 1 || ents[0].Pendientes != len(main)-1 {
		t.Fatalf("entornos: %+v (main tiene %d commits)", ents, len(main))
	}
	// Solo un admin del proyecto puede proteger el entorno.
	beto.pedir("PATCH", fmt.Sprintf("/api/repos/%d/entornos/staging", r), map[string]bool{"requiere_aprobacion": true}, 403, nil)
	ana.pedir("PATCH", fmt.Sprintf("/api/repos/%d/entornos/staging", r), map[string]bool{"requiere_aprobacion": true}, 200, nil)

	// --- Auditoría (solo admin)
	beto.pedir("GET", fmt.Sprintf("/api/repos/%d/auditoria", r), nil, 403, nil)
	var aud []map[string]any
	ana.pedir("GET", fmt.Sprintf("/api/repos/%d/auditoria", r), nil, 200, &aud)
	if len(aud) == 0 {
		t.Fatal("la auditoría no registró nada")
	}
}

func TestTokensPersonales(t *testing.T) {
	srv, _ := servidor(t)
	ana := registrar(t, srv.URL, "Ana", "ana@ejemplo.com")
	var tok struct{ Token string }
	ana.pedir("POST", "/api/tokens", map[string]string{"nombre": "CI"}, 201, &tok)
	if !strings.HasPrefix(tok.Token, "marga_pat_") {
		t.Fatalf("token = %q", tok.Token)
	}
	ci := &cliente{t: t, base: srv.URL, token: tok.Token}
	ci.pedir("GET", "/api/repos", nil, 200, nil)
	ci.pedir("DELETE", "/api/tokens/actual", nil, 200, nil)
	ci.pedir("GET", "/api/repos", nil, 401, nil)
}

func TestProteccionCSRF(t *testing.T) {
	srv, _ := servidor(t)
	registrar(t, srv.URL, "Ana", "ana@ejemplo.com")
	// Con cookie pero sin JSON (como un formulario de otro sitio): se rechaza.
	jar := &http.Client{}
	login, _ := http.NewRequest("POST", srv.URL+"/api/auth/login", strings.NewReader(`{"email":"ana@ejemplo.com","password":"clave-segura-1"}`))
	login.Header.Set("Content-Type", "application/json")
	res, err := jar.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var galleta *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == cookieSesion {
			galleta = c
		}
	}
	if galleta == nil || !galleta.HttpOnly {
		t.Fatal("el login debe poner una cookie HttpOnly")
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/repos", strings.NewReader("nombre=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(galleta)
	res, err = jar.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode < 400 || res.StatusCode >= 500 {
		t.Fatalf("un POST sin JSON con cookie debe rechazarse, dio %d", res.StatusCode)
	}
}

func TestFrontEmbebido(t *testing.T) {
	srv, _ := servidor(t)
	for ruta, quiero := range map[string]int{
		"/pages/login.html":         200,
		"/assets/js/marga-api.js":   200,
		"/web.go":                   404,
		"/.env":                     404,
		"/pages/../internal/api.go": 404,
	} {
		res, err := http.Get(srv.URL + ruta)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != quiero {
			t.Errorf("%s: %d, esperaba %d", ruta, res.StatusCode, quiero)
		}
	}
}

func TestLimiteDeIntentosDeLogin(t *testing.T) {
	srv, _ := servidor(t)
	registrar(t, srv.URL, "Ana", "ana@ejemplo.com")
	anon := &cliente{t: t, base: srv.URL}
	for i := 0; i < 8; i++ {
		anon.pedir("POST", "/api/auth/login", map[string]string{"email": "ana@ejemplo.com", "password": "mala"}, 401, nil)
	}
	// Ni con la contraseña correcta: la cuenta queda en pausa un rato.
	anon.pedir("POST", "/api/auth/login", map[string]string{"email": "ANA@ejemplo.com", "password": "clave-segura-1"}, 429, nil)
}

func TestRestablecerYCambiarPassword(t *testing.T) {
	srv, st := servidor(t)
	ana := registrar(t, srv.URL, "Ana", "ana@ejemplo.com")
	anon := &cliente{t: t, base: srv.URL}

	// La respuesta no revela si la cuenta existe.
	var r1, r2 map[string]any
	anon.pedir("POST", "/api/auth/recuperar", map[string]string{"email": "ana@ejemplo.com"}, 200, &r1)
	anon.pedir("POST", "/api/auth/recuperar", map[string]string{"email": "nadie@ejemplo.com"}, 200, &r2)
	if r1["mensaje"] != r2["mensaje"] {
		t.Fatal("recuperar debe responder igual exista o no el email")
	}

	u, err := st.UsuarioPorEmail(context.Background(), "ana@ejemplo.com")
	if err != nil {
		t.Fatal(err)
	}
	enlace, err := EnlaceRestablecer(context.Background(), st, "https://marga.ejemplo.com", u)
	if err != nil || !strings.HasPrefix(enlace, "https://marga.ejemplo.com/pages/restablecer.html#token=") {
		t.Fatalf("enlace = %q, %v", enlace, err)
	}
	tok := enlace[strings.Index(enlace, "token=")+6:]

	anon.pedir("POST", "/api/auth/restablecer", map[string]string{"token": tok, "password": "corta"}, 400, nil)
	anon.pedir("POST", "/api/auth/restablecer", map[string]string{"token": "inventado", "password": "nueva-clave-99"}, 400, nil)
	var ses struct{ Token string }
	anon.pedir("POST", "/api/auth/restablecer", map[string]string{"token": tok, "password": "nueva-clave-99"}, 200, &ses)
	// Un solo uso.
	anon.pedir("POST", "/api/auth/restablecer", map[string]string{"token": tok, "password": "otra-clave-99"}, 400, nil)
	// La sesión que Ana tenía abierta se cerró; la contraseña vieja ya no sirve.
	ana.pedir("GET", "/api/auth/me", nil, 401, nil)
	anon.pedir("POST", "/api/auth/login", map[string]string{"email": "ana@ejemplo.com", "password": "clave-segura-1"}, 401, nil)

	// Cambiar la contraseña desde la sesión nueva.
	nueva := &cliente{t: t, base: srv.URL, token: ses.Token}
	nueva.pedir("POST", "/api/auth/password", map[string]string{"actual": "mal", "nueva": "tercera-clave-1"}, 400, nil)
	nueva.pedir("POST", "/api/auth/password", map[string]string{"actual": "nueva-clave-99", "nueva": "tercera-clave-1"}, 200, nil)
	nueva.pedir("GET", "/api/auth/me", nil, 200, nil) // la sesión desde la que cambió sigue viva
	anon.pedir("POST", "/api/auth/login", map[string]string{"email": "ana@ejemplo.com", "password": "tercera-clave-1"}, 200, nil)

	// Un token de la CLI no puede cambiar la contraseña.
	var pat struct{ Token string }
	nueva.pedir("POST", "/api/tokens", map[string]string{"nombre": "CI"}, 201, &pat)
	(&cliente{t: t, base: srv.URL, token: pat.Token}).pedir("POST", "/api/auth/password", map[string]string{"actual": "tercera-clave-1", "nueva": "cuarta-clave-1"}, 403, nil)
}

func TestCabecerasDeSeguridad(t *testing.T) {
	srv, _ := servidor(t)
	res, err := http.Get(srv.URL + "/pages/login.html")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if res.Header.Get(h) == "" {
			t.Errorf("falta la cabecera %s", h)
		}
	}
}
