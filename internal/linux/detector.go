package linux

import (
	"fmt"
	"log"
)

// Activity represents the current activity on the machine
type Activity struct {
	ID    string // e.g., "steam:12345", "browser:homework", "window:code"
	Name  string // Human-readable name
	State string // "active", "idle", "offline"
}

// Detector orchestrates activity detection with priority:
// 1. Steam game (if running)
// 2. Browser tab (if browser is focused)
// 3. Window title (fallback)
type Detector struct {
	config   *Config
	steam    *SteamDetector
	window   *WindowDetector
	browser  *BrowserDetector
	category *Categorizer
}

// NewDetector creates a new activity detector
func NewDetector(cfg *Config) (*Detector, error) {
	window, err := NewWindowDetector()
	if err != nil {
		return nil, fmt.Errorf("create window detector: %w", err)
	}

	return &Detector{
		config:   cfg,
		steam:    NewSteamDetector(),
		window:   window,
		browser:  NewBrowserDetector(cfg.FirefoxProfile),
		category: NewCategorizer(cfg.Categories),
	}, nil
}

// Detect returns the current activity
func (d *Detector) Detect() Activity {
	// Priority 1: Check for running Steam game
	game, err := d.steam.Detect()
	if err != nil {
		log.Printf("steam detection error: %v", err)
	}
	if game != nil {
		return Activity{
			ID:    fmt.Sprintf("steam:%s", game.AppID),
			Name:  game.Name,
			State: "active",
		}
	}

	// Get the active window for further detection
	windowInfo, err := d.window.Detect()
	if err != nil {
		log.Printf("window detection error: %v", err)
		return Activity{
			ID:    "unknown",
			Name:  "Unknown",
			State: "offline",
		}
	}

	// No window focused = idle
	if windowInfo == nil {
		return Activity{
			ID:    "idle:no-window",
			Name:  "No Window",
			State: "idle",
		}
	}

	// Check if window indicates idle state (screensaver, lock screen, etc.)
	if windowInfo.IsIdle(d.config.IdleWindowPatterns) {
		return Activity{
			ID:    "idle:screensaver",
			Name:  windowInfo.Title,
			State: "idle",
		}
	}

	// Check if window should be ignored
	if windowInfo.IsIgnored(d.config.IgnoredWindows) {
		return Activity{
			ID:    "idle:ignored",
			Name:  windowInfo.Title,
			State: "idle",
		}
	}

	// Priority 2: If browser is focused, get the active tab
	if windowInfo.IsBrowser() {
		tab, err := d.browser.DetectFirefox()
		if err != nil {
			log.Printf("firefox detection error: %v", err)
		}
		if tab != nil && tab.Domain != "" {
			category := d.category.Categorize(tab.Domain)
			return Activity{
				ID:    fmt.Sprintf("browser:%s", category),
				Name:  tab.Domain,
				State: "active",
			}
		}
		// Couldn't get tab info, fall through to window title
	}

	// Priority 3: Fall back to window title
	return Activity{
		ID:    fmt.Sprintf("window:%s", windowInfo.Instance),
		Name:  windowInfo.Title,
		State: "active",
	}
}

// Close cleans up resources
func (d *Detector) Close() {
	if d.window != nil {
		d.window.Close()
	}
}

