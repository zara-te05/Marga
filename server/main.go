// Marga Sun · servidor web
//
//	go run .            -> migra la BD y levanta el servidor en :8080
//	go run . -seed      -> además crea datos de demostración
//	go run . -migrate   -> solo aplica migraciones y termina
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zarat/Marga-Sun/server/internal/api"
	"github.com/zarat/Marga-Sun/server/internal/config"
	"github.com/zarat/Marga-Sun/server/internal/database"
	"github.com/zarat/Marga-Sun/server/internal/security"
	"github.com/zarat/Marga-Sun/server/internal/store"
)

func main() {
	seed := flag.Bool("seed", false, "crear datos de demostración")
	soloMigrar := flag.Bool("migrate", false, "solo aplicar migraciones y salir")
	flag.Parse()

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("✗ %v\n  Revisa MARGA_DB_URL en server/.env", err)
	}
	defer db.Close()
	log.Println("✓ conectado a PostgreSQL")

	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("✗ migraciones: %v", err)
	}
	if *soloMigrar {
		return
	}

	st := store.New(db, cfg.DiffTTL)
	if *seed {
		h, err := security.HashPassword("marga1234")
		if err != nil {
			log.Fatal(err)
		}
		if err := st.Seed(ctx, h); err != nil {
			log.Fatalf("✗ seed: %v", err)
		}
	}

	// Limpieza periódica de sesiones y diffs caducados.
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if n, err := st.Purgar(ctx); err == nil && n > 0 {
				log.Printf("limpieza: %d registros expirados eliminados", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(cfg, st).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("✓ Marga Sun en http://localhost%s  (front: %s)", cfg.Addr, cfg.WebDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("apagando...")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}
