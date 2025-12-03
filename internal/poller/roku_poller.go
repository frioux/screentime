package poller

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type PollResult struct {
	DeviceID  string
	AppID     string
	AppName   string
	State     string // "active", "idle", "offline"
	Timestamp time.Time
}

type RokuPoller struct {
	deviceID string
	baseURL  string
	client   *http.Client
}

func NewRokuPoller(deviceID, baseURL string) *RokuPoller {
	return &RokuPoller{
		deviceID: deviceID,
		baseURL:  strings.TrimRight(baseURL, "/"),
		client:   &http.Client{},
	}
}

type activeAppResponse struct {
	XMLName xml.Name `xml:"active-app"`
	App     struct {
		ID   string `xml:"id,attr"`
		Name string `xml:",chardata"`
	} `xml:"app"`
}

// Poll queries /query/active-app and returns a PollResult.
// Network errors are mapped to State="offline" with no error returned.
func (p *RokuPoller) Poll(ctx context.Context) (PollResult, error) {
	now := time.Now().UTC()
	res := PollResult{
		DeviceID:  p.deviceID,
		State:     "offline",
		Timestamp: now,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/query/active-app", nil)
	if err != nil {
		return res, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		// offline
		return res, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// treat non-200 as offline
		return res, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return res, fmt.Errorf("read active-app response: %w", err)
	}

	var a activeAppResponse
	if err := xml.Unmarshal(body, &a); err != nil {
		return res, fmt.Errorf("unmarshal active-app response: %w", err)
	}

	appID := strings.TrimSpace(a.App.ID)
	appName := strings.TrimSpace(a.App.Name)

	res.AppID = appID
	res.AppName = appName

	if appName == "" || isIdleAppName(appName) {
		res.State = "idle"
	} else {
		res.State = "active"
	}

	return res, nil
}

func isIdleAppName(name string) bool {
	l := strings.ToLower(strings.TrimSpace(name))
	switch l {
	case "roku", "home", "screensaver", "roku home":
		return true
	default:
		return false
	}
}
