package database_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zara-te05/Marga/internal/datos/database"
	"github.com/zara-te05/Marga/tests/apoyo/pruebabd"
)

// Un servidor instalado con los archivos de migración en CRLF (Windows) guardó
// checksums de esos bytes. Con los mismos archivos en LF no debe avisar que
// "cambiaron": solo cambiaron los saltos de línea.
func TestChecksumIgnoraSaltosDeLinea(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, pruebabd.Nueva(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Checksums como los dejaba la versión anterior con los archivos en CRLF.
	archivos, _ := filepath.Glob("../../../internal/datos/database/migrations/*.sql")
	if len(archivos) == 0 {
		t.Fatal("no encontré las migraciones del servidor")
	}
	for _, a := range archivos {
		b, _ := os.ReadFile(a)
		crlf := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n", "\r\n")
		s := sha256.Sum256([]byte(crlf))
		version, _, _ := strings.Cut(filepath.Base(a), "_")
		if _, err := db.Exec(`UPDATE schema_migrations SET checksum = $1 WHERE version = $2`, hex.EncodeToString(s[:]), version); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "cambió después de aplicarse") {
		t.Fatalf("no debía avisar por saltos de línea:\n%s", buf.String())
	}
	// Y quedó al día: la siguiente vez tampoco avisa, y un cambio de verdad sí.
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum = repeat('0', 64) WHERE version = '0001'`); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "cambió después de aplicarse"); n != 1 {
		t.Fatalf("un cambio real debe avisar una vez, avisó %d:\n%s", n, buf.String())
	}
}
