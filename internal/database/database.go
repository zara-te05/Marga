// Package database abre el pool de conexiones a PostgreSQL y aplica las
// migraciones propias de Marga (embebidas en el binario).
package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open crea el pool y espera hasta 30 s a que la BD responda
// (útil cuando PostgreSQL arranca al mismo tiempo, p. ej. con Docker).
func Open(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("configuración de conexión inválida: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	deadline := time.Now().Add(30 * time.Second)
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(pctx)
		cancel()
		if err == nil {
			return db, nil
		}
		if time.Now().After(deadline) {
			db.Close()
			return nil, fmt.Errorf("PostgreSQL no respondió: %w", err)
		}
		log.Printf("esperando a PostgreSQL... (%v)", err)
		time.Sleep(2 * time.Second)
	}
}

type migration struct {
	version  string
	nombre   string
	sql      string
	checksum string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		version, nombre, _ := strings.Cut(base, "_")
		sum := sha256.Sum256(b)
		out = append(out, migration{version, nombre, string(b), hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// Migrate aplica en orden las migraciones pendientes, cada una en su propia
// transacción, y avisa si alguna ya aplicada fue modificada (checksum).
func Migrate(ctx context.Context, db *sql.DB) error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Bloqueo para que dos instancias no migren a la vez.
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(727427)`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(727427)`)

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     VARCHAR(32) PRIMARY KEY,
			nombre      TEXT        NOT NULL,
			checksum    CHAR(64)    NOT NULL,
			aplicada_en TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return err
	}

	applied := map[string]string{}
	rows, err := conn.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v, c string
		if err := rows.Scan(&v, &c); err != nil {
			rows.Close()
			return err
		}
		applied[v] = c
	}
	rows.Close()

	for _, m := range migs {
		if sum, ok := applied[m.version]; ok {
			if sum != m.checksum {
				log.Printf("⚠ la migración %s_%s cambió después de aplicarse (checksum distinto)", m.version, m.nombre)
			}
			continue
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migración %s_%s: %w", m.version, m.nombre, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, nombre, checksum) VALUES ($1, $2, $3)`,
			m.version, m.nombre, m.checksum); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("✓ migración aplicada: %s_%s", m.version, m.nombre)
	}
	return nil
}
