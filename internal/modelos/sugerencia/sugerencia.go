// Package sugerencia le pone a un error la siguiente acción que puede intentar
// la persona. La CLI la imprime debajo del error como "prueba: …".
//
//	return sugerencia.Nuevo("no hay entornos", `marga env add dev "postgres://..."`)
//
// El mensaje del error no incluye la sugerencia (así se puede envolver con %w sin
// repetirla); se recupera con De, que recorre toda la cadena de errores.
package sugerencia

import (
	"errors"
	"fmt"
)

// ConSugerencia lo cumple cualquier error que sepa cuál es el siguiente paso.
type ConSugerencia interface {
	error
	Sugerencia() string
}

// Error es un error con la acción que conviene probar después.
type Error struct {
	Err    error
	Prueba string
}

func (e *Error) Error() string      { return e.Err.Error() }
func (e *Error) Unwrap() error      { return e.Err }
func (e *Error) Sugerencia() string { return e.Prueba }

// Nuevo crea un error con su sugerencia.
func Nuevo(msg, prueba string) error { return &Error{Err: errors.New(msg), Prueba: prueba} }

// Nuevof es Nuevo con formato en el mensaje (acepta %w).
func Nuevof(prueba, format string, a ...any) error {
	return &Error{Err: fmt.Errorf(format, a...), Prueba: prueba}
}

// Con añade una sugerencia a un error existente. Si err es nil devuelve nil, y si
// ya traía una sugerencia, la nueva la reemplaza (es la más cercana a la persona).
func Con(err error, prueba string) error {
	if err == nil {
		return nil
	}
	return &Error{Err: err, Prueba: prueba}
}

// De devuelve la sugerencia más externa de la cadena de errores ("" si no hay).
func De(err error) string {
	var s ConSugerencia
	for err != nil {
		if errors.As(err, &s) {
			if t := s.Sugerencia(); t != "" {
				return t
			}
			err = errors.Unwrap(s)
			continue
		}
		return ""
	}
	return ""
}
