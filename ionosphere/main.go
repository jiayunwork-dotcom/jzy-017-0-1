package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	cfg := LoadConfig()

	var store Store
	if cfg.DatabaseURL != "" {
		db, err := openDBWithRetry(cfg.DatabaseURL, 30, time.Second)
		if err != nil {
			log.Fatalf("database unavailable: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := Migrate(ctx, db); err != nil {
			cancel()
			log.Fatalf("schema migration failed: %v", err)
		}
		cancel()
		store = NewPGStore(db)
		log.Printf("connected to PostgreSQL")
	} else {
		log.Printf("WARNING: DATABASE_URL not set, using in-memory store (calculations will not persist)")
		store = NewMemoryStore()
	}
	defer store.Close()

	srv := NewServer(cfg, store)
	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		sig := <-ch
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	log.Printf("ionosphere TEC service %s listening on %s (ground=%gm top=%gm tol=%g)",
		version, cfg.Addr(), cfg.GroundAltitude, cfg.TopHeight, cfg.TECTolerance)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

// openDBWithRetry waits for the database to accept connections, which keeps
// startup robust when the database container is still initialising.
func openDBWithRetry(url string, attempts int, delay time.Duration) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return db, nil
		}
		log.Printf("waiting for database (%d/%d): %v", i+1, attempts, err)
		time.Sleep(delay)
	}
	_ = db.Close()
	return nil, err
}
