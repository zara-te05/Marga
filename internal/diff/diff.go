// Package diff calcula diferencias línea a línea (LCS) entre dos textos DDL.
package diff

import (
	"fmt"
	"strings"
)

type Linea struct {
	Tipo  string `json:"tipo"` // "+", "-" o " "
	Texto string `json:"texto"`
	NumA  int    `json:"a,omitempty"` // nº de línea en el texto original
	NumB  int    `json:"b,omitempty"` // nº de línea en el texto nuevo
}

const maxCeldas = 4_000_000 // límite de memoria para la tabla LCS

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Lineas devuelve la secuencia completa de líneas iguales, borradas y añadidas.
func Lineas(antes, despues string) []Linea {
	a, b := splitLines(antes), splitLines(despues)
	n, m := len(a), len(b)

	if n*m > maxCeldas { // demasiado grande: todo fuera, todo dentro
		out := make([]Linea, 0, n+m)
		for i, l := range a {
			out = append(out, Linea{"-", l, i + 1, 0})
		}
		for j, l := range b {
			out = append(out, Linea{"+", l, 0, j + 1})
		}
		return out
	}

	// lcs[i][j] = longitud LCS de a[i:] y b[j:]
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	out := make([]Linea, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, Linea{" ", a[i], i + 1, j + 1})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, Linea{"-", a[i], i + 1, 0})
			i++
		default:
			out = append(out, Linea{"+", b[j], 0, j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, Linea{"-", a[i], i + 1, 0})
	}
	for ; j < m; j++ {
		out = append(out, Linea{"+", b[j], 0, j + 1})
	}
	return out
}

// Contar devuelve (añadidas, eliminadas).
func Contar(ls []Linea) (int, int) {
	var add, del int
	for _, l := range ls {
		switch l.Tipo {
		case "+":
			add++
		case "-":
			del++
		}
	}
	return add, del
}

// Unificado produce un texto estilo `git diff` (solo líneas cambiadas + contexto).
func Unificado(antes, despues string) string {
	ls := Lineas(antes, despues)
	add, del := Contar(ls)
	if add == 0 && del == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "@@ +%d -%d @@\n", add, del)
	for _, l := range ls {
		sb.WriteString(l.Tipo)
		sb.WriteString(l.Texto)
		sb.WriteByte('\n')
	}
	return sb.String()
}
