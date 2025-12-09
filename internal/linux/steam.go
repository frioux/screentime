package linux

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SteamDetector detects currently running Steam games
type SteamDetector struct {
	mu        sync.RWMutex
	nameCache map[string]string // appID -> game name
	client    *http.Client
}

// SteamGame represents a running Steam game
type SteamGame struct {
	AppID string
	Name  string
}

// NewSteamDetector creates a new Steam game detector
func NewSteamDetector() *SteamDetector {
	return &SteamDetector{
		nameCache: make(map[string]string),
		client:    &http.Client{Timeout: 5 * time.Second},
	}
}

var steamEventRe = regexp.MustCompile(`^.*AppID (\d+) state changed : (.*),$`)

// Detect returns the currently running Steam game, if any
func (s *SteamDetector) Detect() (*SteamGame, error) {
	logPath, err := steamLogPath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No Steam installed
		}
		return nil, fmt.Errorf("open steam log: %w", err)
	}
	defer f.Close()

	appID, err := s.runningGameFromLog(f)
	if err != nil {
		return nil, err
	}
	if appID == "" {
		return nil, nil
	}

	name, err := s.lookupGameName(appID)
	if err != nil {
		// If we can't look up the name, just use the ID
		name = "Steam Game " + appID
	}

	return &SteamGame{AppID: appID, Name: name}, nil
}

func steamLogPath() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(u.HomeDir, ".local", "share", "Steam", "logs", "content_log.txt"), nil
}

func (s *SteamDetector) runningGameFromLog(r io.Reader) (string, error) {
	var currentAppID string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		m := steamEventRe.FindSubmatch(scanner.Bytes())
		if m == nil {
			continue
		}

		appID := string(m[1])
		events := strings.Split(string(m[2]), ",")

		isRunning := false
		for _, e := range events {
			if strings.TrimSpace(e) == "App Running" {
				isRunning = true
				break
			}
		}

		if isRunning {
			currentAppID = appID
		} else if currentAppID == appID {
			// This app stopped running
			currentAppID = ""
		}
	}

	return currentAppID, scanner.Err()
}

func (s *SteamDetector) lookupGameName(appID string) (string, error) {
	// Check cache first
	s.mu.RLock()
	if name, ok := s.nameCache[appID]; ok {
		s.mu.RUnlock()
		return name, nil
	}
	s.mu.RUnlock()

	// Query Steam API
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "https://store.steampowered.com/api/appdetails/?filters=basic&appids=" + appID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var details map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name string `json:"name"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return "", err
	}

	info, ok := details[appID]
	if !ok || !info.Success {
		return "", fmt.Errorf("no data for appID %s", appID)
	}

	name := info.Data.Name

	// Cache the result
	s.mu.Lock()
	s.nameCache[appID] = name
	s.mu.Unlock()

	return name, nil
}

