// marga-server: la web y la API de Marga (el front va embebido en el binario).
//
//	go run ./cmd/marga-server            -> migra la BD y levanta el servidor en :8080
//	go run ./cmd/marga-server -seed      -> además crea datos de demostración
//	go run ./cmd/marga-server -migrate   -> solo aplica migraciones y termina
//	marga-server -restablecer ana@x.com  -> enlace para que Ana cambie su contraseña
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zara-te05/Marga/internal/controladores/api"
	"github.com/zara-te05/Marga/internal/datos/database"
	"github.com/zara-te05/Marga/internal/datos/store"
	"github.com/zara-te05/Marga/internal/modelos/version"
	"github.com/zara-te05/Marga/internal/servicios/config"
	"github.com/zara-te05/Marga/internal/servicios/security"
)

func main() {
	seed := flag.Bool("seed", false, "crear datos de demostración")
	soloMigrar := flag.Bool("migrate", false, "solo aplicar migraciones y salir")
	verVersion := flag.Bool("version", false, "muestra la versión y termina")
	restablecer := flag.String("restablecer", "", "imprime un enlace para que EMAIL cambie su contraseña y termina")
	flag.Parse()
	if *verVersion {
		fmt.Println("marga-server", version.Texto())
		return
	}

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("✗ %v\n  Revisa MARGA_DB_URL (variable de entorno o archivo .env)", err)
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
	if *restablecer != "" {
		u, err := st.UsuarioPorEmail(ctx, *restablecer)
		if err != nil {
			log.Fatalf("✗ %s: %v", *restablecer, err)
		}
		enlace, err := api.EnlaceRestablecer(ctx, st, api.New(cfg, st).URLPublica(), u)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Enlace para %s (vale 1 hora, un solo uso):\n  %s\n", u.Email, enlace)
		return
	}
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
		front := "embebido"
		if cfg.WebDir != "" {
			front = cfg.WebDir
		}
		log.Printf("✓ Marga %s en http://localhost%s  (front: %s)", version.Texto(), cfg.Addr, front)
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
