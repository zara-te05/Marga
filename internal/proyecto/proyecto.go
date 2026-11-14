// Package proyecto maneja la carpeta .marga/ de un proyecto (como .git/):
// configuración, entornos y archivos de migración.
package proyecto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const Dir = ".marga"

// Config se guarda en .marga/config.json y SÍ se versiona con el proyecto.
type Config struct {
	Servidor    string `json:"servidor,omitempty"`
	RepoID      int64  `json:"repo_id,omitempty"`
	Rama        string `json:"rama"`
	Motor       string `json:"motor"`
	Migraciones string `json:"migraciones"` // carpeta relativa a la raíz
}

// Entorno se guarda en .marga/entornos.json, que NO se versiona (tiene contraseñas).
type Entorno struct {
	URL   string `json:"url"` // cadena de conexión o "$VARIABLE"
	Motor string `json:"motor"`
	// Compartido: la usa todo el equipo (staging, prod). Si es false es la base
	// personal de quien ejecuta la CLI y se reporta como "dev@usuario".
	Compartido bool `json:"compartido"`
}

var reEntornoPersonal = regexp.MustCompile(`(?i)^(dev|local|desarrollo|development)$`)

// EsPersonalPorNombre: "dev" o "local" se asumen personales; el resto, compartidos.
func EsPersonalPorNombre(nombre string) bool { return reEntornoPersonal.MatchString(nombre) }

type Proyecto struct {
	Raiz   string
	Config Config
}

var ErrSinProyecto = errors.New("no hay un proyecto de Marga aquí (ni en carpetas superiores). Usa `marga init`")

// Buscar sube desde `desde` hasta encontrar una carpeta .marga/.
func Buscar(desde string) (*Proyecto, error) {
	dir, err := filepath.Abs(desde)
	if err != nil {
		return nil, err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, Dir)); err == nil && st.IsDir() {
			p := &Proyecto{Raiz: dir}
			return p, p.leerJSON("config.json", &p.Config)
		}
		padre := filepath.Dir(dir)
		if padre == dir {
			return nil, ErrSinProyecto
		}
		dir = padre
	}
}

// Iniciar crea .marga/ y la carpeta de migraciones en `raiz`.
func Iniciar(raiz string, cfg Config) (*Proyecto, error) {
	if _, err := os.Stat(filepath.Join(raiz, Dir)); err == nil {
		return nil, fmt.Errorf("ya existe un proyecto de Marga en %s", raiz)
	}
	if cfg.Rama == "" {
		cfg.Rama = "main"
	}
	if cfg.Motor == "" {
		cfg.Motor = "postgresql"
	}
	if cfg.Migraciones == "" {
		cfg.Migraciones = "migrations"
	}
	p := &Proyecto{Raiz: raiz, Config: cfg}
	if err := os.MkdirAll(filepath.Join(raiz, Dir), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.DirMigraciones(), 0o755); err != nil {
		return nil, err
	}
	gi := "# Generado por Marga: las cadenas de conexión no se versionan\nentornos.json\nsnapshots/\n"
	if err := os.WriteFile(filepath.Join(raiz, Dir, ".gitignore"), []byte(gi), 0o644); err != nil {
		return nil, err
	}
	return p, p.Guardar()
}

func (p *Proyecto) ruta(nombre string) string { return filepath.Join(p.Raiz, Dir, nombre) }

func (p *Proyecto) leerJSON(nombre string, v any) error {
	b, err := os.ReadFile(p.ruta(nombre))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s está dañado: %w", p.ruta(nombre), err)
	}
	return nil
}

func (p *Proyecto) escribirJSON(nombre string, v any, perm os.FileMode) error {
	b, _ := json.MarshalIndent(v, "", "  ")
	return os.WriteFile(p.ruta(nombre), append(b, '\n'), perm)
}

func (p *Proyecto) Guardar() error { return p.escribirJSON("config.json", p.Config, 0o644) }

func (p *Proyecto) DirMigraciones() string { return filepath.Join(p.Raiz, p.Config.Migraciones) }

// ---------------------------------------------------------------- Entornos

func (p *Proyecto) Entornos() (map[string]Entorno, error) {
	m := map[string]Entorno{}
	return m, p.leerJSON("entornos.json", &m)
}

func (p *Proyecto) GuardarEntornos(m map[string]Entorno) error {
	return p.escribirJSON("entornos.json", m, 0o600)
}

// Entorno resuelve un entorno por nombre; si nombre está vacío y solo hay uno, usa ese.
func (p *Proyecto) Entorno(nombre string) (string, Entorno, error) {
	m, err := p.Entornos()
	if err != nil {
		return "", Entorno{}, err
	}
	if nombre == "" {
		switch len(m) {
		case 0:
			return "", Entorno{}, errors.New("no hay entornos. Agrega uno con `marga env add dev \"postgres://...\"`")
		case 1:
			for k, v := range m {
				return k, v, nil
			}
		default:
			return "", Entorno{}, fmt.Errorf("hay %d entornos; elige uno con --env (%s)", len(m), strings.Join(NombresEntornos(m), ", "))
		}
	}
	e, ok := m[nombre]
	if !ok {
		return "", Entorno{}, fmt.Errorf("el entorno %q no existe (usa `marga env ls`)", nombre)
	}
	return nombre, e, nil
}

func NombresEntornos(m map[string]Entorno) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// URLReal expande "$VARIABLE" para no guardar contraseñas (útil en CI).
func (e Entorno) URLReal() (string, error) {
	if strings.HasPrefix(e.URL, "$") {
		v := os.Getenv(strings.TrimPrefix(e.URL, "$"))
		if v == "" {
			return "", fmt.Errorf("la variable de entorno %s está vacía", e.URL)
		}
		return v, nil
	}
	return e.URL, nil
}

// URLOculta esconde la contraseña para mostrarla en pantalla.
func (e Entorno) URLOculta() string {
	return regexp.MustCompile(`(://[^:/@]+:)[^@]*@`).ReplaceAllString(e.URL, "${1}****@")
}

// ---------------------------------------------------------------- Mapa con el servidor

// Remoto relaciona cada migración local con su commit en el servidor.
// Se guarda en .marga/remoto.json y se versiona (lo comparte el equipo).
func (p *Proyecto) Remoto() (map[string]string, error) {
	m := map[string]string{}
	return m, p.leerJSON("remoto.json", &m)
}

func (p *Proyecto) GuardarRemoto(m map[string]string) error {
	return p.escribirJSON("remoto.json", m, 0o644)
}

// ---------------------------------------------------------------- Migraciones

type Migracion struct {
	Version        string // "0007"
	Nombre         string // "add_phone_to_users"
	Up, Down       string
	Checksum       string // sha256 del UP normalizado
	SinTransaccion bool   // "-- marga:sin-transaccion" en el UP
}

func (m Migracion) ID() string { return m.Version + "_" + m.Nombre }

var reArchivo = regexp.MustCompile(`^(\d{4,})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Normalizar unifica saltos de línea (Windows/Linux) para que el checksum no cambie.
func Normalizar(sql string) string {
	sql = strings.ReplaceAll(sql, "\r\n", "\n")
	return strings.TrimRight(sql, " \t\n") + "\n"
}

func Checksum(sql string) string {
	s := sha256.Sum256([]byte(Normalizar(sql)))
	return hex.EncodeToString(s[:])
}

// EsVacio indica si el SQL solo tiene comentarios o espacios.
func EsVacio(sql string) bool {
	for _, l := range strings.Split(sql, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "--") {
			return false
		}
	}
	return true
}

// Migraciones lee la carpeta de migraciones ordenada por versión.
func (p *Proyecto) Migraciones() ([]Migracion, error) {
	entradas, err := os.ReadDir(p.DirMigraciones())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	por := map[string]*Migracion{}
	for _, e := range entradas {
		if e.IsDir() {
			continue
		}
		m := reArchivo.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(p.DirMigraciones(), e.Name()))
		if err != nil {
			return nil, err
		}
		mig, ok := por[m[1]]
		if !ok {
			mig = &Migracion{Version: m[1], Nombre: m[2]}
			por[m[1]] = mig
		} else if mig.Nombre != m[2] {
			return nil, fmt.Errorf("hay dos migraciones con la versión %s (%s y %s); renombra una", m[1], mig.Nombre, m[2])
		}
		if m[3] == "up" {
			mig.Up = Normalizar(string(b))
		} else {
			mig.Down = Normalizar(string(b))
		}
	}
	out := make([]Migracion, 0, len(por))
	for _, m := range por {
		if m.Up == "" {
			return nil, fmt.Errorf("a la migración %s le falta el archivo .up.sql", m.ID())
		}
		m.Checksum = Checksum(m.Up)
		m.SinTransaccion = strings.Contains(m.Up, "marga:sin-transaccion")
		if EsVacio(m.Down) {
			m.Down = ""
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return versionNum(out[i].Version) < versionNum(out[j].Version) })
	return out, nil
}

func versionNum(v string) int { n, _ := strconv.Atoi(v); return n }

var reSlug = regexp.MustCompile(`[^a-z0-9]+`)

var sinAcentos = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// Slug convierte "Agregar teléfono" en "agregar_telefono".
func Slug(s string) string {
	s = sinAcentos.Replace(strings.ToLower(s))
	s = strings.Trim(reSlug.ReplaceAllString(s, "_"), "_")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "_")
	}
	return s
}

// SiguienteVersion devuelve la versión siguiente a la mayor existente.
func (p *Proyecto) SiguienteVersion() (string, error) {
	migs, err := p.Migraciones()
	if err != nil {
		return "", err
	}
	max := 0
	for _, m := range migs {
		if n := versionNum(m.Version); n > max {
			max = n
		}
	}
	return fmt.Sprintf("%04d", max+1), nil
}

// EscribirMigracion crea los archivos .up.sql y .down.sql.
func (p *Proyecto) EscribirMigracion(version, nombre, up, down string) (string, string, error) {
	if err := os.MkdirAll(p.DirMigraciones(), 0o755); err != nil {
		return "", "", err
	}
	base := filepath.Join(p.DirMigraciones(), version+"_"+nombre)
	for _, f := range []string{base + ".up.sql", base + ".down.sql"} {
		if _, err := os.Stat(f); err == nil {
			return "", "", fmt.Errorf("ya existe %s", f)
		}
	}
	if err := os.WriteFile(base+".up.sql", []byte(up), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(base+".down.sql", []byte(down), 0o644); err != nil {
		return "", "", err
	}
	return base + ".up.sql", base + ".down.sql", nil
}

// RenombrarVersion cambia el número de una migración (sus dos archivos).
func (p *Proyecto) RenombrarVersion(m Migracion, nueva string) error {
	return p.Renombrar(m, nueva, m.Nombre)
}

// Renombrar cambia versión y nombre de una migración: sus archivos y sus snapshots.
func (p *Proyecto) Renombrar(m Migracion, version, nombre string) error {
	nuevoID := version + "_" + nombre
	base := filepath.Join(p.DirMigraciones(), m.ID())
	dest := filepath.Join(p.DirMigraciones(), nuevoID)
	for _, ext := range []string{".up.sql", ".down.sql"} {
		if _, err := os.Stat(base + ext); err != nil {
			continue
		}
		if _, err := os.Stat(dest + ext); err == nil {
			return fmt.Errorf("ya existe %s%s", dest, ext)
		}
		if err := os.Rename(base+ext, dest+ext); err != nil {
			return err
		}
	}
	for _, env := range p.EntornosConSnapshots() {
		_ = os.Rename(p.rutaSnapshot(env, m.ID()), p.rutaSnapshot(env, nuevoID))
	}
	return nil
}

// ---------------------------------------------------------------- Snapshots locales

// Al aplicar una migración, la CLI guarda el esquema resultante en
// .marga/snapshots/<entorno>/<id>.json (por entorno: cada base puede diferir).
// `marga push` los usa para enviar al servidor el DDL de las tablas que cambió.

func (p *Proyecto) rutaSnapshot(entorno, id string) string {
	return filepath.Join(p.Raiz, Dir, "snapshots", entorno, id+".json")
}

func (p *Proyecto) GuardarSnapshot(entorno, id string, v any) error {
	if err := os.MkdirAll(filepath.Join(p.Raiz, Dir, "snapshots", entorno), 0o755); err != nil {
		return err
	}
	p.asegurarIgnorado("snapshots/")
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(p.rutaSnapshot(entorno, id), b, 0o644)
}

// LeerSnapshot devuelve false si esa migración no tiene snapshot en ese entorno.
func (p *Proyecto) LeerSnapshot(entorno, id string, v any) (bool, error) {
	b, err := os.ReadFile(p.rutaSnapshot(entorno, id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(b, v)
}

// EntornosConSnapshots lista los entornos que tienen snapshots guardados.
func (p *Proyecto) EntornosConSnapshots() []string {
	ents, _ := os.ReadDir(filepath.Join(p.Raiz, Dir, "snapshots"))
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// asegurarIgnorado agrega una línea a .marga/.gitignore si falta (proyectos creados antes).
func (p *Proyecto) asegurarIgnorado(linea string) {
	ruta := filepath.Join(p.Raiz, Dir, ".gitignore")
	b, _ := os.ReadFile(ruta)
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) == linea {
			return
		}
	}
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		b = append(b, '\n')
	}
	_ = os.WriteFile(ruta, append(b, []byte(linea+"\n")...), 0o644)
}
