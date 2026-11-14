package migrador

import (
	"testing"

	"github.com/zara-te05/Marga/internal/proyecto"
)

func mig(v, n, up string) proyecto.Migracion {
	return proyecto.Migracion{Version: v, Nombre: n, Up: up, Checksum: proyecto.Checksum(up)}
}

func ap(v, n, up string) Aplicada {
	return Aplicada{Version: v, Nombre: n, Checksum: proyecto.Checksum(up)}
}

func situaciones(fs []Fila) map[string]Situacion {
	m := map[string]Situacion{}
	for _, f := range fs {
		m[f.ID()] = f.Situacion
	}
	return m
}

func TestComparar(t *testing.T) {
	locales := []proyecto.Migracion{
		mig("0001", "a", "A"),
		mig("0002", "b", "B editado"), // se editó después de aplicarse
		mig("0003", "c", "C"),         // se aplicó como 0004 en otra rama y luego se renumeró
		mig("0005", "e", "E"),         // nueva
	}
	aplicadas := []Aplicada{
		ap("0001", "a", "A"),
		ap("0002", "b", "B"),
		ap("0004", "c", "C"),
		ap("0009", "z", "Z"), // no hay archivo
	}
	got := situaciones(Comparar(locales, aplicadas))
	want := map[string]Situacion{
		"0001_a": Aplicada_,
		"0002_b": Modificada,
		"0003_c": Renumerada,
		"0005_e": Pendiente,
		"0009_z": Huerfana,
	}
	for id, s := range want {
		if got[id] != s {
			t.Errorf("%s: %q, esperaba %q", id, got[id], s)
		}
	}
	if len(got) != len(want) {
		t.Errorf("filas de más: %v", got)
	}
}

func TestCompararIntercambio(t *testing.T) {
	// Tras un merge 0011 y 0012 intercambian número: ambas son "renumeradas", ninguna "modificada".
	locales := []proyecto.Migracion{mig("0011", "y", "Y"), mig("0012", "x", "X")}
	aplicadas := []Aplicada{ap("0011", "x", "X"), ap("0012", "y", "Y")}
	for _, f := range Comparar(locales, aplicadas) {
		if f.Situacion != Renumerada {
			t.Errorf("%s: %s, esperaba renumerada", f.ID(), f.Situacion)
		}
	}
}

func TestPendienteFueraDeOrden(t *testing.T) {
	fs := Comparar([]proyecto.Migracion{mig("0001", "a", "A"), mig("0002", "b", "B"), mig("0003", "c", "C")},
		[]Aplicada{ap("0001", "a", "A"), ap("0003", "c", "C")})
	for _, f := range fs {
		if f.Version == "0002" && (f.Situacion != Pendiente || !f.FueraDeOrden) {
			t.Fatalf("0002 debe ser pendiente fuera de orden: %+v", f)
		}
	}
}
