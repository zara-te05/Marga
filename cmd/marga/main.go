// Comando marga: la CLI de Marga. Sin argumentos muestra la ayuda, como cualquier CLI;
// el panel interactivo es `marga ui` y la consola de SQL es `marga sql`.
package main

import "github.com/zara-te05/Marga/internal/controladores/cli"

func main() {
	cli.Execute()
}
