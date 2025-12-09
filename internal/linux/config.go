package linux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Category defines URL matching rules for a category
type Category struct {
	Domains        []string `json:"domains,omitempty"`
	DomainSuffixes []string `json:"domain_suffixes,omitempty"`
}

// Config holds the Linux agent configuration
type Config struct {
	Listen             string              `json:"listen"`
	Hostname           string              `json:"hostname"`
	Categories         map[string]Category `json:"categories"`
	IdleWindowPatterns []string            `json:"idle_window_patterns"`
	IgnoredWindows     []string            `json:"ignored_windows"`
	FirefoxProfile     string              `json:"firefox_profile,omitempty"`
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Listen:   ":8060",
		Hostname: "",
		Categories: map[string]Category{
			"homework": {
				Domains:        []string{"docs.google.com", "classroom.google.com", "khanacademy.org"},
				DomainSuffixes: []string{".edu"},
			},
			"entertainment": {
				Domains: []string{"youtube.com", "netflix.com", "twitch.tv", "reddit.com"},
			},
		},
		IdleWindowPatterns: []string{"screensaver", "lock screen", "xscreensaver"},
		IgnoredWindows:     []string{},
	}
}

// DefaultConfigPath returns the default path for the config file
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "screentime-agent", "config.json"), nil
}

// LoadConfig loads the config from the given path
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// DefaultFirefoxRecoveryPath finds the Firefox recovery.jsonlz4 file
func DefaultFirefoxRecoveryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	mozDir := filepath.Join(home, ".mozilla", "firefox")
	entries, err := os.ReadDir(mozDir)
	if err != nil {
		return "", fmt.Errorf("read firefox directory: %w", err)
	}

	// Look for default profile (ends with .default or .default-release)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".default" || 
		   len(name) > 16 && name[len(name)-16:] == ".default-release" {
			recoveryPath := filepath.Join(mozDir, name, "sessionstore-backups", "recovery.jsonlz4")
			if _, err := os.Stat(recoveryPath); err == nil {
				return recoveryPath, nil
			}
		}
	}

	return "", fmt.Errorf("no Firefox profile with recovery file found")
}


