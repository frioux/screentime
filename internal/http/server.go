package http

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"screentime-agent/internal/config"
	"screentime-agent/internal/storage"
)

type Server struct {
	cfg        *config.Config
	store      *storage.SessionStore
	loc        *time.Location
	httpServer *http.Server
}

func NewServer(cfg *config.Config, store *storage.SessionStore) (*Server, error) {
	loc, err := cfg.ResolveLocation()
	if err != nil {
		return nil, fmt.Errorf("resolve timezone: %w", err)
	}

	s := &Server{
		cfg:   cfg,
		store: store,
		loc:   loc,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    cfg.HTTPListen,
		Handler: mux,
	}

	return s, nil
}

// Start runs the HTTP server until ctx is canceled or the server fails.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		log.Printf("HTTP server listening on %s", s.cfg.HTTPListen)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		// Shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		log.Printf("HTTP server shut down gracefully")
		return nil
	case err := <-errCh:
		return fmt.Errorf("http server error: %w", err)
	}
}
