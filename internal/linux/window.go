package linux

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

// WindowInfo contains information about the active window
type WindowInfo struct {
	Title    string
	Class    string // WM_CLASS instance name (e.g., "firefox", "chromium")
	Instance string // WM_CLASS class name
}

// CompositorType represents the detected Wayland compositor
type CompositorType int

const (
	CompositorUnknown CompositorType = iota
	CompositorGNOME
	CompositorKDE
)

// WindowDetector detects the currently active window
type WindowDetector struct {
	conn       *dbus.Conn
	compositor CompositorType
}

// NewWindowDetector creates a new window detector
func NewWindowDetector() (*WindowDetector, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to session bus: %w", err)
	}

	detector := &WindowDetector{conn: conn}
	detector.compositor = detector.detectCompositor()

	if detector.compositor == CompositorUnknown {
		conn.Close()
		return nil, fmt.Errorf("unsupported compositor: could not detect GNOME or KDE")
	}

	return detector, nil
}

// detectCompositor determines which Wayland compositor is running
func (w *WindowDetector) detectCompositor() CompositorType {
	// Check XDG_CURRENT_DESKTOP first
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	desktopLower := strings.ToLower(desktop)

	if strings.Contains(desktopLower, "gnome") {
		return CompositorGNOME
	}
	if strings.Contains(desktopLower, "kde") || strings.Contains(desktopLower, "plasma") {
		return CompositorKDE
	}

	// Fallback: check if DBus services are available
	if w.isDBusServiceAvailable("org.gnome.Shell") {
		return CompositorGNOME
	}
	if w.isDBusServiceAvailable("org.kde.KWin") {
		return CompositorKDE
	}

	return CompositorUnknown
}

// isDBusServiceAvailable checks if a DBus service is available
func (w *WindowDetector) isDBusServiceAvailable(service string) bool {
	obj := w.conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var names []string
	err := obj.Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		return false
	}
	for _, name := range names {
		if name == service {
			return true
		}
	}
	return false
}

// Detect returns information about the currently active window
func (w *WindowDetector) Detect() (*WindowInfo, error) {
	switch w.compositor {
	case CompositorGNOME:
		return w.detectGNOME()
	case CompositorKDE:
		return w.detectKDE()
	default:
		return nil, fmt.Errorf("unsupported compositor")
	}
}

// detectGNOME gets active window info using GNOME Shell's Eval method
func (w *WindowDetector) detectGNOME() (*WindowInfo, error) {
	obj := w.conn.Object("org.gnome.Shell", "/org/gnome/Shell")

	// JavaScript to get active window info
	script := `
		(function() {
			const win = global.display.focus_window;
			if (!win) return JSON.stringify({});
			return JSON.stringify({
				title: win.get_title() || '',
				wmClass: win.get_wm_class() || ''
			});
		})()
	`

	var success bool
	var result string
	err := obj.Call("org.gnome.Shell.Eval", 0, script).Store(&success, &result)
	if err != nil {
		return nil, fmt.Errorf("gnome shell eval: %w", err)
	}

	if !success {
		return nil, fmt.Errorf("gnome shell eval failed: %s", result)
	}

	// Parse the JSON result
	var data struct {
		Title   string `json:"title"`
		WMClass string `json:"wmClass"`
	}

	// The result is a JSON string, need to unquote it first
	var jsonStr string
	if err := json.Unmarshal([]byte(result), &jsonStr); err != nil {
		// Try parsing directly if it's not double-encoded
		jsonStr = result
	}

	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("parse gnome result: %w", err)
	}

	// No focused window
	if data.Title == "" && data.WMClass == "" {
		return nil, nil
	}

	return &WindowInfo{
		Title:    data.Title,
		Class:    data.WMClass,
		Instance: strings.ToLower(data.WMClass),
	}, nil
}

// detectKDE gets active window info using KWin's DBus interface
func (w *WindowDetector) detectKDE() (*WindowInfo, error) {
	obj := w.conn.Object("org.kde.KWin", "/KWin")

	// Use KWin scripting to get window info
	script := `
		(function() {
			const client = workspace.activeWindow;
			if (!client) return JSON.stringify({});
			return JSON.stringify({
				title: client.caption || '',
				wmClass: client.resourceClass || ''
			});
		})()
	`

	// First, load and run the script
	var scriptId int32
	err := obj.Call("org.kde.kwin.Scripting.loadScript", 0, "", "screentime-query").Store(&scriptId)

	// Alternative approach: use the activeClient method directly if available
	// Try getting active client info via properties
	var activeWindowId int32
	err = obj.Call("org.kde.KWin.activeClient", 0).Store(&activeWindowId)
	if err != nil {
		// KWin 6 uses different method names
		// Try the newer API
		return w.detectKDEViaScript(script)
	}

	if activeWindowId == 0 {
		return nil, nil
	}

	// Get window properties via the scripting interface
	return w.detectKDEViaScript(script)
}

// detectKDEViaScript runs a KWin script to get window info
func (w *WindowDetector) detectKDEViaScript(script string) (*WindowInfo, error) {
	// For KDE, we'll use qdbus-style calls
	// First try the newer KWin 6 scripting approach
	obj := w.conn.Object("org.kde.KWin", "/Scripting")

	// Create a temporary script
	var scriptId int32
	call := obj.Call("org.kde.kwin.Scripting.loadDeclarativeScript", 0, script, "screentime")
	if call.Err != nil {
		// Try alternative: call org.kde.KWin methods directly
		return w.detectKDEViaProperties()
	}
	call.Store(&scriptId)

	// Run and get result
	scriptObj := w.conn.Object("org.kde.KWin", dbus.ObjectPath(fmt.Sprintf("/Scripting/Script%d", scriptId)))
	var result string
	err := scriptObj.Call("org.kde.kwin.Script.run", 0).Store(&result)
	if err != nil {
		return w.detectKDEViaProperties()
	}

	// Unload the script
	obj.Call("org.kde.kwin.Scripting.unloadScript", 0, "screentime")

	var data struct {
		Title   string `json:"title"`
		WMClass string `json:"wmClass"`
	}
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return nil, fmt.Errorf("parse kde result: %w", err)
	}

	if data.Title == "" && data.WMClass == "" {
		return nil, nil
	}

	return &WindowInfo{
		Title:    data.Title,
		Class:    data.WMClass,
		Instance: strings.ToLower(data.WMClass),
	}, nil
}

// detectKDEViaProperties uses KWin's window properties interface
func (w *WindowDetector) detectKDEViaProperties() (*WindowInfo, error) {
	// Use the org.kde.KWin interface to query active window
	obj := w.conn.Object("org.kde.KWin", "/KWin")

	// Get the caption of the active window
	var caption string
	err := obj.Call("org.kde.KWin.caption", 0).Store(&caption)
	if err != nil {
		// Try alternative approach using supportInformation
		return w.detectKDEViaSupportInfo()
	}

	return &WindowInfo{
		Title:    caption,
		Class:    "",
		Instance: "",
	}, nil
}

// detectKDEViaSupportInfo parses supportInformation to get active window
func (w *WindowDetector) detectKDEViaSupportInfo() (*WindowInfo, error) {
	obj := w.conn.Object("org.kde.KWin", "/KWin")

	var info string
	err := obj.Call("org.kde.KWin.supportInformation", 0).Store(&info)
	if err != nil {
		return nil, fmt.Errorf("kde support info: %w", err)
	}

	// Parse the support information for active window details
	// This is a fallback - the info contains window list
	lines := strings.Split(info, "\n")
	var inActiveWindow bool
	var title, wmClass string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Active Window:") || strings.Contains(line, "active: true") {
			inActiveWindow = true
			continue
		}
		if inActiveWindow {
			if strings.HasPrefix(line, "caption:") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "caption:"))
			}
			if strings.HasPrefix(line, "resourceClass:") {
				wmClass = strings.TrimSpace(strings.TrimPrefix(line, "resourceClass:"))
			}
			if strings.HasPrefix(line, "Window #") || line == "" {
				if title != "" || wmClass != "" {
					break
				}
			}
		}
	}

	if title == "" && wmClass == "" {
		return nil, nil
	}

	return &WindowInfo{
		Title:    title,
		Class:    wmClass,
		Instance: strings.ToLower(wmClass),
	}, nil
}

// Close closes the DBus connection
func (w *WindowDetector) Close() {
	if w.conn != nil {
		w.conn.Close()
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
