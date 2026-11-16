package migrador_test

import (
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"testing"

	"github.com/zara-te05/Marga/internal/datos/proyecto"
)

func mig(v, n, up string) proyecto.Migracion {
	return proyecto.Migracion{Version: v, Nombre: n, Up: up, Checksum: proyecto.Checksum(up)}
}

func ap(v, n, up string) migrador.Aplicada {
	return migrador.Aplicada{Version: v, Nombre: n, Checksum: proyecto.Checksum(up)}
}

func situaciones(fs []migrador.Fila) map[string]migrador.Situacion {
	m := map[string]migrador.Situacion{}
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
	aplicadas := []migrador.Aplicada{
		ap("0001", "a", "A"),
		ap("0002", "b", "B"),
		ap("0004", "c", "C"),
		ap("0009", "z", "Z"), // no hay archivo
	}
	got := situaciones(migrador.Comparar(locales, aplicadas))
	want := map[string]migrador.Situacion{
		"0001_a": migrador.Aplicada_,
		"0002_b": migrador.Modificada,
		"0003_c": migrador.Renumerada,
		"0005_e": migrador.Pendiente,
		"0009_z": migrador.Huerfana,
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
	aplicadas := []migrador.Aplicada{ap("0011", "x", "X"), ap("0012", "y", "Y")}
	for _, f := range migrador.Comparar(locales, aplicadas) {
		if f.Situacion != migrador.Renumerada {
			t.Errorf("%s: %s, esperaba renumerada", f.ID(), f.Situacion)
		}
	}
}

func TestPendienteFueraDeOrden(t *testing.T) {
	fs := migrador.Comparar([]proyecto.Migracion{mig("0001", "a", "A"), mig("0002", "b", "B"), mig("0003", "c", "C")},
		[]migrador.Aplicada{ap("0001", "a", "A"), ap("0003", "c", "C")})
	for _, f := range fs {
		if f.Version == "0002" && (f.Situacion != migrador.Pendiente || !f.FueraDeOrden) {
			t.Fatalf("0002 debe ser pendiente fuera de orden: %+v", f)
		}
	}
}
