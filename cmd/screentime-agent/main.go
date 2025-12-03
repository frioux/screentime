package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"screentime-agent/internal/config"
	"screentime-agent/internal/http"
	"screentime-agent/internal/poller"
	"screentime-agent/internal/storage"
)

func main() {
	cfgPath := flag.String("config", "config.json", "Path to JSON config file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load config
	cfg, err := config.LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Initialize SQLite
	db, err := storage.NewDB(ctx, cfg.DatabasePath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}()

	store := storage.NewSessionStore(db)

	// Close any stale current_sessions on startup
	now := time.Now().UTC()
	if err := store.CloseStaleCurrentSessions(ctx, now); err != nil {
		log.Fatalf("failed to close stale current sessions: %v", err)
	}

	// Start pollers
	runner := poller.NewRunner(cfg.Devices, store)
	runner.Start(ctx)

	// Start HTTP server (blocks until ctx is canceled or server fails)
	server, err := http.NewServer(cfg, store)
	if err != nil {
		log.Fatalf("failed to create HTTP server: %v", err)
	}

	if err := server.Start(ctx); err != nil {
		log.Printf("HTTP server stopped with error: %v", err)
		os.Exit(1)
	}
}
