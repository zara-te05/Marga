package proyecto

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Local se guarda en .marga/local.json, que NO se versiona: es el estado de
// esta máquina. Hoy guarda qué base de datos usa cada rama de git en cada
// entorno personal (dev, local).
type Local struct {
	// RamaPrincipal es la rama de git a la que pertenece la base de la cadena de
	// conexión del entorno (normalmente main). Las demás ramas usan una copia.
	RamaPrincipal string `json:"rama_principal,omitempty"`
	// Entornos: entorno -> rama -> base de esa rama.
	Entornos map[string]map[string]*BaseRama `json:"entornos,omitempty"`
}

// BaseRama es la base de datos de una rama en un entorno personal.
type BaseRama struct {
	Base      string    `json:"base"`            // nombre de la base en el servidor de PostgreSQL
	Desde     string    `json:"desde,omitempty"` // rama de la que se copió ("" si se creó vacía)
	Vacia     bool      `json:"vacia,omitempty"` // se creó vacía y se aplicaron las migraciones
	CreadaEn  time.Time `json:"creada_en"`
	UltimoUso time.Time `json:"ultimo_uso"`
}

func (p *Proyecto) Local() (*Local, error) {
	l := &Local{}
	if err := p.leerJSON("local.json", l); err != nil {
		return nil, err
	}
	if l.Entornos == nil {
		l.Entornos = map[string]map[string]*BaseRama{}
	}
	return l, nil
}

func (p *Proyecto) GuardarLocal(l *Local) error {
	p.asegurarIgnorado("local.json")
	return p.escribirJSON("local.json", l, 0o600)
}

// Base devuelve la base de una rama en un entorno (nil si no tiene).
func (l *Local) Base(entorno, rama string) *BaseRama {
	if m := l.Entornos[entorno]; m != nil {
		return m[rama]
	}
	return nil
}

func (l *Local) PonerBase(entorno, rama string, b *BaseRama) {
	if l.Entornos[entorno] == nil {
		l.Entornos[entorno] = map[string]*BaseRama{}
	}
	l.Entornos[entorno][rama] = b
}

func (l *Local) QuitarBase(entorno, rama string) {
	if m := l.Entornos[entorno]; m != nil {
		delete(m, rama)
		if len(m) == 0 {
			delete(l.Entornos, entorno)
		}
	}
}

// Ramas lista las ramas con base propia en un entorno, ordenadas.
func (l *Local) Ramas(entorno string) []string {
	var out []string
	for r := range l.Entornos[entorno] {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- Snapshots por base

// CopiarSnapshots duplica los snapshots de una base en otra (al copiar una base,
// la copia tiene la misma historia, así que sus snapshots son los mismos).
func (p *Proyecto) CopiarSnapshots(desde, hacia string) error {
	origen := filepath.Join(p.Raiz, Dir, "snapshots", desde)
	ents, err := os.ReadDir(origen)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	destino := filepath.Join(p.Raiz, Dir, "snapshots", hacia)
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return err
	}
	p.asegurarIgnorado("snapshots/")
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(origen, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destino, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// BorrarSnapshots elimina los snapshots de una base (al borrar la base de una rama).
func (p *Proyecto) BorrarSnapshots(clave string) error {
	if clave == "" || filepath.Base(clave) != clave {
		return nil
	}
	return os.RemoveAll(filepath.Join(p.Raiz, Dir, "snapshots", clave))
}
