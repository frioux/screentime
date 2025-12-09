package linux

import (
	"strings"

	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/BurntSushi/xgbutil/icccm"
)

// WindowInfo contains information about the active window
type WindowInfo struct {
	Title    string
	Class    string // WM_CLASS instance name (e.g., "firefox", "chromium")
	Instance string // WM_CLASS class name
}

// WindowDetector detects the currently active window
type WindowDetector struct {
	x *xgbutil.XUtil
}

// NewWindowDetector creates a new window detector
func NewWindowDetector() (*WindowDetector, error) {
	x, err := xgbutil.NewConn()
	if err != nil {
		return nil, err
	}
	return &WindowDetector{x: x}, nil
}

// Detect returns information about the currently active window
func (w *WindowDetector) Detect() (*WindowInfo, error) {
	windowID, err := ewmh.ActiveWindowGet(w.x)
	if err != nil {
		return nil, err
	}

	// Window ID 0 means no window is focused
	if windowID == 0 {
		return nil, nil
	}

	info := &WindowInfo{}

	// Get window title
	name, err := ewmh.WmNameGet(w.x, windowID)
	if err != nil {
		// Fall back to legacy WM_NAME
		name, _ = icccm.WmNameGet(w.x, windowID)
	}
	info.Title = name

	// Get WM_CLASS (contains instance and class name)
	wmClass, err := icccm.WmClassGet(w.x, windowID)
	if err == nil {
		info.Instance = wmClass.Instance // e.g., "firefox"
		info.Class = wmClass.Class       // e.g., "Firefox"
	}

	return info, nil
}

// Close closes the X connection
func (w *WindowDetector) Close() {
	if w.x != nil {
		w.x.Conn().Close()
	}
}

// IsBrowser returns true if the window class indicates a web browser
func (info *WindowInfo) IsBrowser() bool {
	if info == nil {
		return false
	}
	lower := strings.ToLower(info.Instance)
	browsers := []string{"firefox", "chromium", "chrome", "brave", "brave-browser", "vivaldi", "opera"}
	for _, b := range browsers {
		if lower == b || strings.Contains(lower, b) {
			return true
		}
	}
	return false
}

// IsIdle returns true if the window indicates an idle state
func (info *WindowInfo) IsIdle(patterns []string) bool {
	if info == nil {
		return true // No window = idle
	}
	
	titleLower := strings.ToLower(info.Title)
	for _, pattern := range patterns {
		if strings.Contains(titleLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// IsIgnored returns true if the window should be ignored
func (info *WindowInfo) IsIgnored(ignoredWindows []string) bool {
	if info == nil {
		return false
	}
	
	instanceLower := strings.ToLower(info.Instance)
	classLower := strings.ToLower(info.Class)
	
	for _, ignored := range ignoredWindows {
		ignoredLower := strings.ToLower(ignored)
		if instanceLower == ignoredLower || classLower == ignoredLower {
			return true
		}
	}
	return false
}

