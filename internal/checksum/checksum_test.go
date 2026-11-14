package checksum

import "testing"

func TestMismoChecksumConSaltosDeWindows(t *testing.T) {
	unix := "CREATE TABLE a (id int);\nCREATE TABLE b (id int);\n"
	windows := "CREATE TABLE a (id int);\r\nCREATE TABLE b (id int);\r\n\r\n  "
	if SQL(unix) != SQL(windows) {
		t.Fatal("CRLF y espacios finales no deberían cambiar el checksum")
	}
	if SQL(unix) == SQL("CREATE TABLE a (id bigint);\n") {
		t.Fatal("SQL distinto debe dar checksum distinto")
	}
	if got := Normalizar("x\r\n\n\n"); got != "x\n" {
		t.Fatalf("Normalizar = %q", got)
	}
	if len(SQL("")) != 64 {
		t.Fatal("el checksum debe ser sha256 en hex")
	}
}
