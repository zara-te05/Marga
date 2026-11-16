package proyecto

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Script es un archivo .sql que no es una migración:
//   - repetible (carpeta sql/): define un objeto completo (una vista, una función)
//     y se vuelve a aplicar cada vez que el archivo cambia.
//   - seed (carpeta seeds/): datos de prueba para las bases personales.
type Script struct {
	Archivo  string // ruta dentro de su carpeta, con "/": "vistas/clientes_activos.sql"
	SQL      string
	Checksum string
}

func (p *Proyecto) DirRepetibles() string {
	d := p.Config.Repetibles
	if d == "" {
		d = "sql"
	}
	return filepath.Join(p.Raiz, d)
}

func (p *Proyecto) DirSeeds() string {
	d := p.Config.Seeds
	if d == "" {
		d = "seeds"
	}
	return filepath.Join(p.Raiz, d)
}

// Repetibles lee sql/**/*.sql en orden alfabético de ruta (numera los archivos,
// p. ej. 01_funciones/…, 02_vistas/…, si uno depende de otro).
func (p *Proyecto) Repetibles() ([]Script, error) { return leerScripts(p.DirRepetibles()) }

// Seeds lee seeds/**/*.sql en orden alfabético de ruta.
func (p *Proyecto) Seeds() ([]Script, error) { return leerScripts(p.DirSeeds()) }

func leerScripts(dir string) ([]Script, error) {
	var out []Script
	err := filepath.WalkDir(dir, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && ruta == dir {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".sql") {
			return nil
		}
		b, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, ruta)
		if err != nil {
			return err
		}
		sql := Normalizar(string(b))
		if EsVacio(sql) {
			return nil
		}
		out = append(out, Script{Archivo: filepath.ToSlash(rel), SQL: sql, Checksum: Checksum(sql)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Archivo < out[j].Archivo })
	return out, nil
}

// EscribirScript crea un archivo nuevo dentro de dir (no sobrescribe).
func EscribirScript(dir, rel, contenido string) (string, error) {
	ruta := filepath.Join(dir, filepath.FromSlash(rel))
	if _, err := os.Stat(ruta); err == nil {
		return "", fmt.Errorf("ya existe %s", ruta)
	}
	if err := os.MkdirAll(filepath.Dir(ruta), 0o755); err != nil {
		return "", err
	}
	return ruta, os.WriteFile(ruta, []byte(contenido), 0o644)
}

// SiguienteSeed devuelve el siguiente número para un seed: "0003".
func (p *Proyecto) SiguienteSeed() string {
	seeds, _ := p.Seeds()
	max := 0
	for _, s := range seeds {
		base := filepath.Base(s.Archivo)
		if i := strings.IndexByte(base, '_'); i > 0 {
			max = maxInt(max, versionNum(base[:i]))
		}
	}
	return fmt.Sprintf("%04d", max+1)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
