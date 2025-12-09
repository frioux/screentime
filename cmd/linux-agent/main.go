package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"screentime-agent/internal/linux"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var configPath string
	var listen string
	var printConfig bool

	defaultConfigPath, _ := linux.DefaultConfigPath()

	flag.StringVar(&configPath, "config", defaultConfigPath, "path to config file")
	flag.StringVar(&listen, "listen", "", "override listen address (e.g., :8060)")
	flag.BoolVar(&printConfig, "print-config", false, "print default config and exit")
	flag.Parse()

	if printConfig {
		cfg := linux.DefaultConfig()
		fmt.Println(`{
  "listen": ":8060",
  "hostname": "my-linux-machine",
  "categories": {
    "homework": {
      "domains": ["docs.google.com", "classroom.google.com", "khanacademy.org"],
      "domain_suffixes": [".edu"]
    },
    "entertainment": {
      "domains": ["youtube.com", "netflix.com", "twitch.tv", "reddit.com"]
    }
  },
  "idle_window_patterns": ["screensaver", "lock screen", "xscreensaver"],
  "ignored_windows": [],
  "firefox_profile": ""
}`)
		_ = cfg // silence unused warning
		return nil
	}

	// Load config
	cfg, err := linux.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Override listen address if specified
	if listen != "" {
		cfg.Listen = listen
	}

	// Create detector
	detector, err := linux.NewDetector(cfg)
	if err != nil {
		return fmt.Errorf("create detector: %w", err)
	}
	defer detector.Close()

	// Create and start server
	server := linux.NewServer(cfg, detector)

	// Handle shutdown signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	}
}

