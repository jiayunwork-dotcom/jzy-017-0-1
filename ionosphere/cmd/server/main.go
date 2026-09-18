// Command server runs the Chapman-layer ionosphere TEC HTTP service with a
// PostgreSQL persistence backend.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ionosphere/internal/api"
	"ionosphere/internal/config"
	"ionosphere/internal/persistence"
)

func main() {
	cfg := config.FromEnv()
	logger := log.New(os.Stdout, "ionosphere ", log.LstdFlags|log.Lmsgprefix)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Printf("connecting to PostgreSQL (up to 60s)...")
	store, err := persistence.EnsureAtLeast(ctx, cfg.DatabaseURL, 60*time.Second)
	if err != nil {
		log.Fatalf("database unavailable: %v", err)
	}
	defer store.Close()
	logger.Printf("database ready, schema migrated")

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewServer(cfg, store).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s (top=%.0f m, tol=%g, TECU=%g)", cfg.HTTPAddr, cfg.TopAltitude, cfg.Tolerance, cfg.TECU)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
