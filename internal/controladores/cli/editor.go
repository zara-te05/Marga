package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// comandoEditor arma el comando para abrir `archivo` en el editor de la persona:
// $VISUAL o $EDITOR (pueden traer argumentos, p. ej. "code --wait"); si no hay,
// el Bloc de notas en Windows y nano o vi en lo demás. Como git con `git commit`.
func comandoEditor(archivo string) (*exec.Cmd, error) {
	ed := strings.TrimSpace(os.Getenv("VISUAL"))
	if ed == "" {
		ed = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	var partes []string
	switch {
	case ed != "":
		partes = strings.Fields(ed)
	case runtime.GOOS == "windows":
		partes = []string{"notepad"}
	default:
		for _, e := range []string{"nano", "vi"} {
			if _, err := exec.LookPath(e); err == nil {
				partes = []string{e}
				break
			}
		}
	}
	if len(partes) == 0 {
		return nil, errors.New("no encontré un editor: define la variable EDITOR (por ejemplo, \"code --wait\" o \"nvim\")")
	}
	// VS Code y similares regresan de inmediato si no se les pide esperar.
	if b := strings.ToLower(filepath.Base(partes[0])); (b == "code" || b == "code.cmd" || b == "cursor" || b == "subl") && !strings.Contains(ed, "-w") {
		partes = append(partes, "--wait")
	}
	cmd := exec.Command(partes[0], append(partes[1:], archivo)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd, nil
}

// editarTexto abre `inicial` en el editor (en un archivo .sql temporal) y devuelve lo guardado.
func editarTexto(inicial string) (string, error) {
	f, err := os.CreateTemp("", "marga-*.sql")
	if err != nil {
		return "", err
	}
	ruta := f.Name()
	defer os.Remove(ruta)
	if _, err := f.WriteString(inicial); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	cmd, err := comandoEditor(ruta)
	if err != nil {
		return "", err
	}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(ruta)
	return string(b), err
}
