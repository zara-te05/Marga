package checksum_test

import (
	"testing"

	"github.com/zara-te05/Marga/internal/modelos/checksum"
)

func TestMismoChecksumConSaltosDeWindows(t *testing.T) {
	unix := "CREATE TABLE a (id int);\nCREATE TABLE b (id int);\n"
	windows := "CREATE TABLE a (id int);\r\nCREATE TABLE b (id int);\r\n\r\n  "
	if checksum.SQL(unix) != checksum.SQL(windows) {
		t.Fatal("CRLF y espacios finales no deberían cambiar el checksum")
	}
	if checksum.SQL(unix) == checksum.SQL("CREATE TABLE a (id bigint);\n") {
		t.Fatal("SQL distinto debe dar checksum distinto")
	}
	if got := checksum.Normalizar("x\r\n\n\n"); got != "x\n" {
		t.Fatalf("Normalizar = %q", got)
	}
	if len(checksum.SQL("")) != 64 {
		t.Fatal("el checksum debe ser sha256 en hex")
	}
}
