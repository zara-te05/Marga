// Package config lee la configuración desde variables de entorno y,
// si existe, desde un archivo .env junto al ejecutable (útil en Windows).
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL  string        // MARGA_DB_URL
	Addr         string        // MARGA_ADDR
	WebDir       string        // MARGA_WEB_DIR: carpeta con index.html, pages/ y assets/
	SessionTTL   time.Duration // MARGA_SESSION_TTL (ej. 72h)
	DiffTTL      time.Duration // MARGA_DIFF_TTL   (ej. 24h)
	SecureCookie bool          // MARGA_SECURE_COOKIE=true detrás de HTTPS
}

func Load() Config {
	loadDotEnv(".env")

	cfg := Config{
		DatabaseURL:  get("MARGA_DB_URL", "postgres://marga:marga@localhost:5432/marga?sslmode=disable"),
		Addr:         get("MARGA_ADDR", ":8080"),
		WebDir:       get("MARGA_WEB_DIR", detectWebDir()),
		SessionTTL:   duration("MARGA_SESSION_TTL", 72*time.Hour),
		DiffTTL:      duration("MARGA_DIFF_TTL", 24*time.Hour),
		SecureCookie: get("MARGA_SECURE_COOKIE", "false") == "true",
	}
	return cfg
}

// detectWebDir busca la carpeta del front: primero "..", luego ".".
func detectWebDir() string {
	for _, c := range []string{"..", "."} {
		if _, err := os.Stat(filepath.Join(c, "pages")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ".."
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(get(key, "")); err == nil {
		return d
	}
	return def
}

// loadDotEnv carga KEY=VALUE sin sobrescribir variables ya definidas.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
