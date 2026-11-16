// Package config lee la configuración desde variables de entorno y,
// si existe, desde un archivo .env junto al ejecutable (útil en Windows).
package config

import (
	"bufio"
	"os"
	"strings"
	"time"

	"github.com/zara-te05/Marga/internal/servicios/correo"
)

type Config struct {
	DatabaseURL  string        // MARGA_DB_URL
	Addr         string        // MARGA_ADDR
	WebDir       string        // MARGA_WEB_DIR: carpeta del front en disco; vacío = el front embebido
	SessionTTL   time.Duration // MARGA_SESSION_TTL (ej. 72h)
	DiffTTL      time.Duration // MARGA_DIFF_TTL   (ej. 24h)
	SecureCookie bool          // MARGA_SECURE_COOKIE=true detrás de HTTPS
	URLPublica   string        // MARGA_URL_PUBLICA: https://marga.tu-dominio.com (enlaces de correo)
	ConfiarProxy bool          // MARGA_CONFIAR_PROXY=true: leer la IP real de X-Forwarded-For
	SMTP         correo.Config // MARGA_SMTP_*: para enviar enlaces de restablecimiento
}

func Load() Config {
	loadDotEnv(".env")

	cfg := Config{
		DatabaseURL:  get("MARGA_DB_URL", "postgres://marga:marga@localhost:5432/marga?sslmode=disable"),
		Addr:         get("MARGA_ADDR", ":8080"),
		WebDir:       get("MARGA_WEB_DIR", ""),
		SessionTTL:   duration("MARGA_SESSION_TTL", 72*time.Hour),
		DiffTTL:      duration("MARGA_DIFF_TTL", 24*time.Hour),
		SecureCookie: get("MARGA_SECURE_COOKIE", "false") == "true",
		URLPublica:   strings.TrimRight(get("MARGA_URL_PUBLICA", ""), "/"),
		ConfiarProxy: get("MARGA_CONFIAR_PROXY", "false") == "true",
		SMTP: correo.Config{
			Host:     get("MARGA_SMTP_HOST", ""),
			Puerto:   get("MARGA_SMTP_PUERTO", "587"),
			Usuario:  get("MARGA_SMTP_USUARIO", ""),
			Password: get("MARGA_SMTP_PASSWORD", ""),
			De:       get("MARGA_SMTP_DE", ""),
		},
	}
	return cfg
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
