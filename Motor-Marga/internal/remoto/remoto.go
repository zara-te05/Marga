// Package remoto es el cliente de la API de Marga Sun y guarda las
// credenciales del usuario en ~/.marga/credenciales.json.
package remoto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------- Credenciales

type Credencial struct {
	Token  string `json:"token"`
	Email  string `json:"email"`
	Nombre string `json:"nombre"`
}

func rutaCredenciales() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".marga", "credenciales.json"), nil
}

// NormalizarServidor quita la barra final: "http://x:8080/" -> "http://x:8080".
func NormalizarServidor(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }

func LeerCredenciales() (map[string]Credencial, error) {
	m := map[string]Credencial{}
	ruta, err := rutaCredenciales()
	if err != nil {
		return m, err
	}
	b, err := os.ReadFile(ruta)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

func GuardarCredenciales(m map[string]Credencial) error {
	ruta, err := rutaCredenciales()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ruta), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(ruta, b, 0o600)
}

// ---------------------------------------------------------------- Cliente

type Cliente struct {
	Base  string
	Token string
	http  *http.Client
}

func Nuevo(base, token string) *Cliente {
	return &Cliente{Base: NormalizarServidor(base), Token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// Conectado devuelve un cliente con las credenciales guardadas para ese servidor.
func Conectado(servidor string) (*Cliente, Credencial, error) {
	servidor = NormalizarServidor(servidor)
	m, err := LeerCredenciales()
	if err != nil {
		return nil, Credencial{}, err
	}
	c, ok := m[servidor]
	if !ok {
		return nil, c, fmt.Errorf("no has iniciado sesión en %s. Usa `marga login --servidor %s`", servidor, servidor)
	}
	return Nuevo(servidor, c.Token), c, nil
}

type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func (c *Cliente) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+"/api"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "marga-cli")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("no hay conexión con el servidor %s", c.Base)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 50<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = res.Status
		}
		if res.StatusCode == http.StatusUnauthorized && c.Token != "" {
			e.Error += " — vuelve a iniciar sesión con `marga login`"
		}
		return &Error{res.StatusCode, e.Error}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ---------------------------------------------------------------- Tipos de la API

type Usuario struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Email  string `json:"email"`
	Rol    string `json:"rol"`
}

type Repo struct {
	ID          int64  `json:"id"`
	Nombre      string `json:"nombre"`
	Slug        string `json:"slug"`
	Motor       string `json:"motor"`
	Entorno     string `json:"entorno"`
	Permiso     string `json:"permiso"`
	Propietario string `json:"propietario"`
}

type Commit struct {
	Numero    int       `json:"numero"`
	Hash      string    `json:"hash"`
	HashCorto string    `json:"hash_corto"`
	Mensaje   string    `json:"mensaje"`
	Fecha     time.Time `json:"fecha"`
	Autor     *string   `json:"autor"`
	PadreHash *string   `json:"padre_hash"`
	SQLUp     string    `json:"sql_up"`
	SQLDown   string    `json:"sql_down"`
}

func (c Commit) PrimeraLinea() string { return strings.SplitN(c.Mensaje, "\n", 2)[0] }

func (c Commit) NombreAutor() string {
	if c.Autor == nil {
		return "—"
	}
	return *c.Autor
}

type Historial struct {
	Branch  string   `json:"branch"`
	Head    *string  `json:"head"`
	Total   int      `json:"total"`
	Commits []Commit `json:"commits"`
}

type NuevoSnapshot struct {
	Tabla     string `json:"tabla"`
	DDL       string `json:"ddl"`
	Eliminada bool   `json:"eliminada,omitempty"`
}

type NuevoCommit struct {
	Branch        string          `json:"branch"`
	Mensaje       string          `json:"mensaje"`
	SQLUp         string          `json:"sql_up"`
	SQLDown       string          `json:"sql_down"`
	PadreEsperado *string         `json:"padre_esperado"`
	Snapshots     []NuevoSnapshot `json:"snapshots,omitempty"`
}

// ---------------------------------------------------------------- Reporte de entornos

type MigracionAplicada struct {
	Version     string    `json:"version"`
	Nombre      string    `json:"nombre"`
	Checksum    string    `json:"checksum"`
	Commit      *string   `json:"commit"`
	AplicadaEn  time.Time `json:"aplicada_en"`
	DuracionMs  int       `json:"duracion_ms"`
	AplicadaPor string    `json:"aplicada_por"`
}

type ReporteEntorno struct {
	Motor      string              `json:"motor"`
	Compartido bool                `json:"compartido"`
	Aplicadas  []MigracionAplicada `json:"aplicadas"`
	Esquema    any                 `json:"esquema"`
	Drift      []string            `json:"drift"`
}

// ReportarEntorno envía al servidor el estado real de un entorno.
func (c *Cliente) ReportarEntorno(repo int64, nombre string, r ReporteEntorno) error {
	return c.do("PUT", fmt.Sprintf("/repos/%d/entornos/%s", repo, url.PathEscape(nombre)), r, nil)
}

// ---------------------------------------------------------------- Llamadas

// Login devuelve un token de sesión temporal.
func (c *Cliente) Login(email, password string) (string, Usuario, error) {
	var r struct {
		Token   string  `json:"token"`
		Usuario Usuario `json:"usuario"`
	}
	err := c.do("POST", "/auth/login", map[string]string{"email": email, "password": password}, &r)
	return r.Token, r.Usuario, err
}

func (c *Cliente) Logout() error { return c.do("POST", "/auth/logout", map[string]string{}, nil) }

// CrearToken crea un token de acceso personal (para la CLI) y lo devuelve una sola vez.
func (c *Cliente) CrearToken(nombre string) (string, error) {
	var r struct {
		Token string `json:"token"`
	}
	err := c.do("POST", "/tokens", map[string]string{"nombre": nombre}, &r)
	return r.Token, err
}

func (c *Cliente) RevocarTokenActual() error { return c.do("DELETE", "/tokens/actual", nil, nil) }

func (c *Cliente) Yo() (Usuario, error) {
	var u Usuario
	return u, c.do("GET", "/auth/me", nil, &u)
}

func (c *Cliente) Repo(id int64) (Repo, error) {
	var r Repo
	return r, c.do("GET", fmt.Sprintf("/repos/%d", id), nil, &r)
}

func (c *Cliente) CrearRepo(nombre, motor string) (Repo, error) {
	var r Repo
	return r, c.do("POST", "/repos", map[string]string{"nombre": nombre, "motor": motor}, &r)
}

func (c *Cliente) Historial(repo int64, rama string) (Historial, error) {
	var h Historial
	q := url.Values{"branch": {rama}, "limit": {"500"}}
	return h, c.do("GET", fmt.Sprintf("/repos/%d/commits?%s", repo, q.Encode()), nil, &h)
}

func (c *Cliente) Commit(repo int64, hash string) (Commit, error) {
	var cm Commit
	return cm, c.do("GET", fmt.Sprintf("/repos/%d/commits/%s", repo, hash), nil, &cm)
}

func (c *Cliente) CrearCommit(repo int64, n NuevoCommit) (Commit, error) {
	var cm Commit
	return cm, c.do("POST", fmt.Sprintf("/repos/%d/commits", repo), n, &cm)
}
