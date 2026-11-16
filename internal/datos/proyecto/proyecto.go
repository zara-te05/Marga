// Package proyecto maneja la carpeta .marga/ de un proyecto (como .git/):
// configuración, entornos y archivos de migración.
package proyecto

import (
	"github.com/zara-te05/Marga/internal/modelos/checksum"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"

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
	// Nombre del proyecto (lo pregunta marga init; en el servidor es el nombre del repo).
	Nombre      string `json:"nombre,omitempty"`
	Servidor    string `json:"servidor,omitempty"`
	RepoID      int64  `json:"repo_id,omitempty"`
	Rama        string `json:"rama"`
	Motor       string `json:"motor"`
	Migraciones string `json:"migraciones"` // carpeta relativa a la raíz
	// Repetibles: vistas, funciones… (un archivo por objeto, se reaplica al cambiar).
	Repetibles string `json:"repetibles,omitempty"` // por defecto "sql"
	// Seeds: datos de prueba para los entornos personales.
	Seeds string `json:"seeds,omitempty"` // por defecto "seeds"
}

// Entorno se guarda en .marga/entornos.json, que NO se versiona (tiene contraseñas).
type Entorno struct {
	URL   string `json:"url"` // cadena de conexión o "$VARIABLE"
	Motor string `json:"motor"`
	// Compartido: la usa todo el equipo (staging, prod). Si es false es la base
	// personal de quien ejecuta la CLI y se reporta como "dev@usuario".
	Compartido bool `json:"compartido"`

	// Solo en memoria (no se guardan). Los llena la CLI cuando un entorno personal
	// sigue la rama de git: la URL apunta entonces a la base de esa rama.
	RamaGit string `json:"-"` // rama de git de la base ("" si el entorno no sigue ramas)
	Base    string `json:"-"` // base real que se usa (tienda__feature_facturas)
	clave   string // carpeta de sus snapshots
}

// ConRama devuelve el entorno apuntando a la base de una rama. `clave` separa sus
// snapshots de los de la base principal (cada base tiene su propia historia).
func (e Entorno) ConRama(url, rama, base, clave string) Entorno {
	e.URL, e.RamaGit, e.Base, e.clave = url, rama, base, clave
	return e
}

// ClaveSnapshots es la carpeta de .marga/snapshots/ de esta base: el nombre del
// entorno para la base principal, o "dev@tienda__feature_facturas" para una de rama.
func (e Entorno) ClaveSnapshots(nombre string) string {
	if e.clave != "" {
		return e.clave
	}
	return nombre
}

var reEntornoPersonal = regexp.MustCompile(`(?i)^(dev|local|desarrollo|development)$`)

// EsPersonalPorNombre: "dev" o "local" se asumen personales; el resto, compartidos.
func EsPersonalPorNombre(nombre string) bool { return reEntornoPersonal.MatchString(nombre) }

type Proyecto struct {
	Raiz   string
	Config Config
}

var ErrSinProyecto = sugerencia.Nuevo("no hay un proyecto de Marga aquí (ni en carpetas superiores)",
	"marga init (o cámbiate con cd a la carpeta del proyecto)")

// EjemploURL es la cadena de conexión de ejemplo que se muestra en las sugerencias.
const EjemploURL = `"postgres://usuario:clave@localhost:5432/mi_bd?sslmode=disable"`

// ErrSinEntornos: el proyecto todavía no tiene ninguna base configurada.
var ErrSinEntornos = sugerencia.Nuevo("este proyecto todavía no tiene entornos (bases de datos)", "marga env add dev "+EjemploURL)

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
		return nil, sugerencia.Nuevof("no hace falta volver a iniciarlo: sigue con marga status",
			"ya existe un proyecto de Marga en %s", raiz)
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
	gi := "# Generado por Marga: las cadenas de conexión y el estado de esta máquina no se versionan\nentornos.json\nlocal.json\nsnapshots/\n"
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
			return "", Entorno{}, ErrSinEntornos
		case 1:
			for k, v := range m {
				return k, v, nil
			}
		default:
			nombres := NombresEntornos(m)
			return "", Entorno{}, sugerencia.Nuevof("agrega --env "+nombres[0]+" (o el que quieras de: "+strings.Join(nombres, ", ")+")",
				"hay %d entornos y no dijiste cuál usar", len(m))
		}
	}
	e, ok := m[nombre]
	if !ok {
		if len(m) == 0 {
			return "", Entorno{}, sugerencia.Nuevof("marga env add "+nombre+" "+EjemploURL, "el entorno %q no existe: este proyecto todavía no tiene entornos", nombre)
		}
		return "", Entorno{}, sugerencia.Nuevof("marga env ls para ver los que hay ("+strings.Join(NombresEntornos(m), ", ")+")", "el entorno %q no existe", nombre)
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
			return "", sugerencia.Nuevof("define la variable antes de correr marga (en PowerShell: $env:"+strings.TrimPrefix(e.URL, "$")+" = \"postgres://...\")",
				"la variable de entorno %s está vacía", e.URL)
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
// Se versiona (lo comparte el equipo) como un archivo por migración en
// .marga/remoto/<id> con el hash: así dos ramas que envían migraciones distintas
// nunca chocan en git (un solo remoto.json chocaba en cada merge).
// Los proyectos con el remoto.json de antes se pasan al formato nuevo al guardar.
func (p *Proyecto) Remoto() (map[string]string, error) {
	m := map[string]string{}
	if err := p.leerJSON("remoto.json", &m); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(p.ruta("remoto"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(p.ruta("remoto"), e.Name()))
		if err != nil {
			return nil, err
		}
		if h := strings.TrimSpace(string(b)); h != "" {
			m[e.Name()] = h
		}
	}
	return m, nil
}

var reIDRemoto = regexp.MustCompile(`^[0-9]+_[a-z0-9_]+$`)

func (p *Proyecto) GuardarRemoto(m map[string]string) error {
	dir := p.ruta("remoto")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for id, h := range m {
		if !reIDRemoto.MatchString(id) {
			continue
		}
		ruta := filepath.Join(dir, id)
		if b, err := os.ReadFile(ruta); err == nil && strings.TrimSpace(string(b)) == h {
			continue
		}
		if err := os.WriteFile(ruta, []byte(h+"\n"), 0o644); err != nil {
			return err
		}
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if _, ok := m[e.Name()]; !ok && !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	if err := os.Remove(p.ruta("remoto.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
func Normalizar(sql string) string { return checksum.Normalizar(sql) }

// Checksum es la identidad de la migración (la misma que calcula el servidor).
func Checksum(sql string) string { return checksum.SQL(sql) }

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

// ErrVersionRepetida: dos migraciones distintas usan la misma versión. Pasa al
// juntar en git dos ramas que crearon, cada una, su 0007.
type ErrVersionRepetida struct {
	Version string
	Nombres []string
}

func (e *ErrVersionRepetida) Error() string {
	return fmt.Sprintf("hay %d migraciones con la versión %s (%s). Pasa al juntar ramas que crearon la misma versión; arréglalo con: marga renumerar",
		len(e.Nombres), e.Version, strings.Join(e.Nombres, ", "))
}

// Migraciones lee la carpeta de migraciones ordenada por versión.
// Si dos migraciones comparten versión devuelve *ErrVersionRepetida.
func (p *Proyecto) Migraciones() ([]Migracion, error) {
	todas, err := p.TodasLasMigraciones()
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(todas); i++ {
		if versionNum(todas[i].Version) == versionNum(todas[i-1].Version) {
			e := &ErrVersionRepetida{Version: todas[i].Version}
			for _, m := range todas {
				if versionNum(m.Version) == versionNum(e.Version) {
					e.Nombres = append(e.Nombres, m.ID())
				}
			}
			return nil, e
		}
	}
	return todas, nil
}

// TodasLasMigraciones es como Migraciones pero acepta versiones repetidas
// (la usa `marga renumerar` para arreglarlas). Ordena por versión y nombre.
func (p *Proyecto) TodasLasMigraciones() ([]Migracion, error) {
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
		clave := m[1] + "_" + m[2]
		mig, ok := por[clave]
		if !ok {
			mig = &Migracion{Version: m[1], Nombre: m[2]}
			por[clave] = mig
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
	sort.Slice(out, func(i, j int) bool {
		if a, b := versionNum(out[i].Version), versionNum(out[j].Version); a != b {
			return a < b
		}
		return out[i].Nombre < out[j].Nombre
	})
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
