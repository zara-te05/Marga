package api_test

import (
	"github.com/zara-te05/Marga/internal/controladores/api"
	"testing"
	"time"
)

func TestLimitador(t *testing.T) {
	ahora := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := api.NuevoLimitador(3, 10*time.Minute)
	l.Ahora = func() time.Time { return ahora }
	for i := 0; i < 3; i++ {
		if l.Espera("ana") != 0 {
			t.Fatalf("intento %d no debía bloquearse", i+1)
		}
		l.Registrar("ana")
		ahora = ahora.Add(time.Minute)
	}
	if e := l.Espera("ana"); e != 7*time.Minute {
		t.Fatalf("debe esperar 7 min (hasta que caduque el primero), no %v", e)
	}
	if l.Espera("beto") != 0 {
		t.Fatal("cada clave lleva su propia cuenta")
	}
	ahora = ahora.Add(7 * time.Minute)
	if l.Espera("ana") != 0 {
		t.Fatal("al salir de la ventana se permite de nuevo")
	}
	l.Registrar("ana")
	l.Limpiar("ana")
	if len(l.Eventos) != 0 {
		t.Fatal("Limpiar debe olvidar la clave")
	}
}
