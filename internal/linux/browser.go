package linux

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"screentime-agent/pkg/mozlz4"
)

// BrowserDetector detects the active browser tab URL
type BrowserDetector struct {
	firefoxRecoveryPath string
}

// NewBrowserDetector creates a new browser detector
func NewBrowserDetector(firefoxProfile string) *BrowserDetector {
	return &BrowserDetector{
		firefoxRecoveryPath: firefoxProfile,
	}
}

// BrowserTab represents the active browser tab
type BrowserTab struct {
	URL    string
	Title  string
	Domain string
}

// firefoxSession represents the structure of Firefox's recovery.jsonlz4
type firefoxSession struct {
	Windows []struct {
		Selected int `json:"selected"` // 1-indexed
		Tabs     []struct {
			Index   int `json:"index"`
			Entries []struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"entries"`
		} `json:"tabs"`
	} `json:"windows"`
	SelectedWindow int `json:"selectedWindow"` // 1-indexed
}

// DetectFirefox gets the active tab from Firefox
func (b *BrowserDetector) DetectFirefox() (*BrowserTab, error) {
	recoveryPath := b.firefoxRecoveryPath
	if recoveryPath == "" {
		var err error
		recoveryPath, err = DefaultFirefoxRecoveryPath()
		if err != nil {
			return nil, err
		}
	}

	f, err := os.Open(recoveryPath)
	if err != nil {
		return nil, fmt.Errorf("open recovery file: %w", err)
	}
	defer f.Close()

	reader, err := mozlz4.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create mozlz4 reader: %w", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read recovery data: %w", err)
	}

	var session firefoxSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}

	// Find the selected window (1-indexed)
	selectedWindowIdx := session.SelectedWindow - 1
	if selectedWindowIdx < 0 || selectedWindowIdx >= len(session.Windows) {
		if len(session.Windows) > 0 {
			selectedWindowIdx = 0
		} else {
			return nil, fmt.Errorf("no windows in session")
		}
	}

	window := session.Windows[selectedWindowIdx]

	// Find the selected tab (1-indexed)
	selectedTabIdx := window.Selected - 1
	if selectedTabIdx < 0 || selectedTabIdx >= len(window.Tabs) {
		if len(window.Tabs) > 0 {
			selectedTabIdx = 0
		} else {
			return nil, fmt.Errorf("no tabs in window")
		}
	}

	tab := window.Tabs[selectedTabIdx]
	if len(tab.Entries) == 0 {
		return nil, fmt.Errorf("no entries in tab")
	}

	// Get the current entry (last in the history)
	entry := tab.Entries[len(tab.Entries)-1]

	// Extract domain from URL
	domain := extractDomain(entry.URL)

	return &BrowserTab{
		URL:    entry.URL,
		Title:  entry.Title,
		Domain: domain,
	}, nil
}

func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	
	host := parsed.Hostname()
	
	// Remove www. prefix for cleaner matching
	host = strings.TrimPrefix(host, "www.")
	
	return host
}

// DetectChromium gets the active tab from Chromium-based browsers
// This is a placeholder - Chromium doesn't have a simple session file like Firefox
func (b *BrowserDetector) DetectChromium() (*BrowserTab, error) {
	// Chromium stores session data in LevelDB format which is more complex to parse.
	// For now, we'll return nil and fall back to window title.
	// Future enhancement: use native messaging or browser extension
	return nil, nil
}

// FindChromiumSessionPath attempts to find the Chromium session path
func FindChromiumSessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Try common locations
	paths := []string{
		filepath.Join(home, ".config", "chromium", "Default", "Current Session"),
		filepath.Join(home, ".config", "google-chrome", "Default", "Current Session"),
		filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser", "Default", "Current Session"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("no chromium session found")
}


