package terminal

// Temas de color de `marga ui`, `marga sql` y el DDL que imprime la CLI.
//
// Hay varios Temas incluidos y cada color se puede cambiar en ~/.marga/tema.json:
//
//	{
//	  "tema": "noche",
//	  "colores": { "clave": "#FF79C6", "cadena": "#F1FA8C" }
//	}
//
// El tema se elige, de mayor a menor prioridad, con --tema, con la variable
// MARGA_TEMA o con ese archivo. En `marga ui`, la tecla t pasa al siguiente y lo guarda.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tema reúne los colores de la interfaz y del resaltado de SQL (en hex: "#38BDF8").
type Tema struct {
	Nombre      string
	Descripcion string

	// Interfaz
	Acento    string // títulos, foco, la marca
	SobreAcen string // texto encima del acento (píldoras)
	Texto     string
	Tenue     string // texto secundario
	Borde     string
	Seleccion string // fondo de la fila elegida
	Ok        string
	Aviso     string
	Error     string

	// SQL
	Clave      string   // SELECT, CREATE, WHERE…
	TipoDato   string   // integer, text, numeric…
	Cadena     string   // 'texto'
	Numero     string   // 42, 3.14
	Comentario string   // -- y /* */
	Funcion    string   // nombre( …
	Operador   string   // = < > + - * / || ::
	Puntuacion string   // , ; .
	Comillas   string   // "identificador"
	Parametro  string   // $1, :nombre
	Parentesis []string // ( [ { por nivel de anidación: se repiten en ciclo
}

// claves de tema.json → campo
func (t *Tema) campos() map[string]*string {
	return map[string]*string{
		"acento": &t.Acento, "sobre_acento": &t.SobreAcen, "texto": &t.Texto, "tenue": &t.Tenue,
		"borde": &t.Borde, "seleccion": &t.Seleccion, "ok": &t.Ok, "aviso": &t.Aviso, "error": &t.Error,
		"clave": &t.Clave, "tipo": &t.TipoDato, "cadena": &t.Cadena, "numero": &t.Numero,
		"comentario": &t.Comentario, "funcion": &t.Funcion, "operador": &t.Operador,
		"puntuacion": &t.Puntuacion, "comillas": &t.Comillas, "parametro": &t.Parametro,
	}
}

var Temas = map[string]Tema{
	"marga": {
		Nombre: "marga", Descripcion: "el de siempre: azul cielo sobre pizarra",
		Acento: "#38BDF8", SobreAcen: "#0B1220", Texto: "#E2E8F0", Tenue: "#64748B", Borde: "#334155",
		Seleccion: "#1E293B", Ok: "#4ADE80", Aviso: "#FBBF24", Error: "#F87171",
		Clave: "#C084FC", TipoDato: "#2DD4BF", Cadena: "#A3E635", Numero: "#FB923C", Comentario: "#64748B",
		Funcion: "#60A5FA", Operador: "#F472B6", Puntuacion: "#94A3B8", Comillas: "#FDE68A", Parametro: "#FB923C",
		Parentesis: []string{"#FACC15", "#F472B6", "#38BDF8"},
	},
	"noche": {
		Nombre: "noche", Descripcion: "violetas y rosas, de alto contraste",
		Acento: "#BD93F9", SobreAcen: "#191A21", Texto: "#F8F8F2", Tenue: "#6272A4", Borde: "#44475A",
		Seleccion: "#343746", Ok: "#50FA7B", Aviso: "#F1FA8C", Error: "#FF5555",
		Clave: "#FF79C6", TipoDato: "#8BE9FD", Cadena: "#F1FA8C", Numero: "#BD93F9", Comentario: "#6272A4",
		Funcion: "#50FA7B", Operador: "#FF79C6", Puntuacion: "#9EA3C0", Comillas: "#FFB86C", Parametro: "#FFB86C",
		Parentesis: []string{"#FFB86C", "#8BE9FD", "#BD93F9"},
	},
	"bosque": {
		Nombre: "bosque", Descripcion: "verdes y ocres, suave para muchas horas",
		Acento: "#A7C080", SobreAcen: "#1E2326", Texto: "#D3C6AA", Tenue: "#7A8478", Borde: "#4F5B58",
		Seleccion: "#2E383C", Ok: "#A7C080", Aviso: "#DBBC7F", Error: "#E67E80",
		Clave: "#E69875", TipoDato: "#83C092", Cadena: "#A7C080", Numero: "#D699B6", Comentario: "#7A8478",
		Funcion: "#7FBBB3", Operador: "#E67E80", Puntuacion: "#9DA9A0", Comillas: "#DBBC7F", Parametro: "#D699B6",
		Parentesis: []string{"#DBBC7F", "#7FBBB3", "#D699B6"},
	},
	"atardecer": {
		Nombre: "atardecer", Descripcion: "naranjas y rosas cálidos",
		Acento: "#FF9E64", SobreAcen: "#1A1B26", Texto: "#E6E1CF", Tenue: "#7A7F99", Borde: "#3B3F5C",
		Seleccion: "#292E42", Ok: "#9ECE6A", Aviso: "#E0AF68", Error: "#F7768E",
		Clave: "#F7768E", TipoDato: "#7DCFFF", Cadena: "#9ECE6A", Numero: "#FF9E64", Comentario: "#6B7089",
		Funcion: "#E0AF68", Operador: "#BB9AF7", Puntuacion: "#A9B1D6", Comillas: "#7DCFFF", Parametro: "#FF9E64",
		Parentesis: []string{"#E0AF68", "#BB9AF7", "#7DCFFF"},
	},
	"papel": {
		Nombre: "papel", Descripcion: "para terminales con fondo claro",
		Acento: "#0369A1", SobreAcen: "#FFFFFF", Texto: "#1F2937", Tenue: "#6B7280", Borde: "#CBD5E1",
		Seleccion: "#E0F2FE", Ok: "#15803D", Aviso: "#B45309", Error: "#B91C1C",
		Clave: "#7C3AED", TipoDato: "#0F766E", Cadena: "#15803D", Numero: "#C2410C", Comentario: "#9CA3AF",
		Funcion: "#1D4ED8", Operador: "#BE185D", Puntuacion: "#6B7280", Comillas: "#92400E", Parametro: "#C2410C",
		Parentesis: []string{"#B45309", "#BE185D", "#0369A1"},
	},
	"mono": {
		Nombre: "mono", Descripcion: "sin colores: solo negritas y tonos de gris",
		Acento: "#FFFFFF", SobreAcen: "#000000", Texto: "#E5E5E5", Tenue: "#8A8A8A", Borde: "#5A5A5A",
		Seleccion: "#303030", Ok: "#FFFFFF", Aviso: "#D0D0D0", Error: "#FFFFFF",
		Clave: "#FFFFFF", TipoDato: "#C8C8C8", Cadena: "#B0B0B0", Numero: "#C8C8C8", Comentario: "#7A7A7A",
		Funcion: "#E5E5E5", Operador: "#C8C8C8", Puntuacion: "#9A9A9A", Comillas: "#C8C8C8", Parametro: "#C8C8C8",
		Parentesis: []string{"#FFFFFF", "#B0B0B0"},
	},
}

// OrdenTemas: el orden en que t los recorre.
var OrdenTemas = []string{"marga", "noche", "bosque", "atardecer", "papel", "mono"}

const PorDefecto = "marga"

// Archivo es ~/.marga/tema.json.
func Archivo() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".marga", "tema.json")
}

type Config struct {
	Tema    string            `json:"tema"`
	Colores map[string]string `json:"colores,omitempty"`
}

func LeerConfig() Config {
	var c Config
	if b, err := os.ReadFile(Archivo()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// Cargar devuelve el tema elegido (bandera > MARGA_TEMA > tema.json) con los
// colores de tema.json encima. Un nombre desconocido es un error con la lista.
func Cargar(bandera string) (Tema, error) {
	cfg := LeerConfig()
	nombre := strings.ToLower(strings.TrimSpace(bandera))
	if nombre == "" {
		nombre = strings.ToLower(strings.TrimSpace(os.Getenv("MARGA_TEMA")))
	}
	if nombre == "" {
		nombre = strings.ToLower(cfg.Tema)
	}
	if nombre == "" {
		nombre = PorDefecto
	}
	t, ok := Temas[nombre]
	if !ok {
		return Temas[PorDefecto], fmt.Errorf("no conozco el tema %q; hay: %s", nombre, strings.Join(OrdenTemas, ", "))
	}
	t.Parentesis = append([]string{}, t.Parentesis...)
	campos := t.campos()
	claves := make([]string, 0, len(cfg.Colores))
	for k := range cfg.Colores {
		claves = append(claves, k)
	}
	sort.Strings(claves)
	for _, k := range claves {
		v := strings.TrimSpace(cfg.Colores[k])
		switch {
		case k == "parentesis":
			if ps := strings.Fields(strings.ReplaceAll(v, ",", " ")); len(ps) > 0 {
				t.Parentesis = ps
			}
		case campos[k] != nil && v != "":
			*campos[k] = v
		}
	}
	return t, nil
}

// GuardarNombre escribe el tema elegido en tema.json sin tocar los colores propios.
func GuardarNombre(nombre string) error {
	ruta := Archivo()
	if ruta == "" {
		return fmt.Errorf("no encontré la carpeta de tu usuario")
	}
	cfg := LeerConfig()
	cfg.Tema = nombre
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(ruta), 0o700); err != nil {
		return err
	}
	return os.WriteFile(ruta, append(b, '\n'), 0o600)
}

func Siguiente(actual string) string {
	for i, n := range OrdenTemas {
		if n == actual {
			return OrdenTemas[(i+1)%len(OrdenTemas)]
		}
	}
	return OrdenTemas[0]
}

// color convierte un color del tema en uno de lipgloss.
func Color(c string) lipgloss.Color { return lipgloss.Color(c) }

// Bandera: --tema de `marga sql`.
var Bandera string

// Actual: el tema de `marga sql` y del DDL que imprime la CLI (marga esquema, \d).
// Se carga una vez; si el nombre es desconocido se usa el de siempre.
var Actual = func() func() Tema {
	var t *Tema
	return func() Tema {
		if t == nil {
			x, _ := Cargar(Bandera)
			t = &x
		}
		return *t
	}
}()
