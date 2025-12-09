package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"screentime-agent/internal/storage"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	var endpoints []string

	register := func(pattern string, handler func(http.ResponseWriter, *http.Request)) {
		endpoints = append(endpoints, pattern)
		mux.HandleFunc(pattern, handler)
	}

	register("/healthz", s.handleHealthz)
	register("/status", s.handleStatus)
	register("/sessions", s.handleSessions)
	register("/usage/today", s.handleUsageToday)

	// Root endpoint lists all endpoints (including itself)
	endpoints = append([]string{"/"}, endpoints...)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		resp := struct {
			Endpoints []string `json:"endpoints"`
		}{
			Endpoints: endpoints,
		}
		writeJSON(w, resp)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cur, err := s.store.GetCurrentSessions(ctx)
	if err != nil {
		http.Error(w, "failed to get status", http.StatusInternalServerError)
		return
	}

	resp := struct {
		Devices []struct {
			DeviceID     string    `json:"device_id"`
			AppID        string    `json:"app_id"`
			AppName      string    `json:"app_name"`
			State        string    `json:"state"`
			StartTime    time.Time `json:"start_time"`
			LastSeenTime time.Time `json:"last_seen_time"`
		} `json:"devices"`
	}{}

	for _, cs := range cur {
		resp.Devices = append(resp.Devices, struct {
			DeviceID     string    `json:"device_id"`
			AppID        string    `json:"app_id"`
			AppName      string    `json:"app_name"`
			State        string    `json:"state"`
			StartTime    time.Time `json:"start_time"`
			LastSeenTime time.Time `json:"last_seen_time"`
		}{
			DeviceID:     cs.DeviceID,
			AppID:        cs.AppID,
			AppName:      cs.AppName,
			State:        cs.State,
			StartTime:    cs.StartTime,
			LastSeenTime: cs.LastSeenTime,
		})
	}

	writeJSON(w, resp)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	var deviceID *string
	if v := q.Get("device_id"); v != "" {
		deviceID = &v
	}

	parseTimePtr := func(name string) (*time.Time, error) {
		v := q.Get(name)
		if v == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, err
		}
		return &t, nil
	}

	since, err := parseTimePtr("since")
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	until, err := parseTimePtr("until")
	if err != nil {
		http.Error(w, "invalid until parameter", http.StatusBadRequest)
		return
	}

	sessions, err := s.store.GetSessions(ctx, deviceID, since, until)
	if err != nil {
		http.Error(w, "failed to get sessions", http.StatusInternalServerError)
		return
	}

	resp := struct {
		Sessions []storage.Session `json:"sessions"`
	}{
		Sessions: sessions,
	}

	writeJSON(w, resp)
}

func (s *Server) handleUsageToday(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	var deviceID *string
	if v := q.Get("device_id"); v != "" {
		deviceID = &v
	}

	dayStartHour := s.cfg.DayStartHour
	if v := q.Get("start_hour"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h >= 0 && h < 24 {
			dayStartHour = h
		}
	}

	// Temporarily override day_start_hour for this calculation
	origHour := s.cfg.DayStartHour
	s.cfg.DayStartHour = dayStartHour
	dayStart, nowLocal := s.cfg.ComputeDayWindow(ctx, s.loc)
	s.cfg.DayStartHour = origHour

	dayStartUTC := dayStart.UTC()
	nowUTC := nowLocal.UTC()

	entries, err := s.store.GetUsageBetween(ctx, dayStartUTC, nowUTC, deviceID)
	if err != nil {
		http.Error(w, "failed to compute usage", http.StatusInternalServerError)
		return
	}

	// Group by device
	type appUsage struct {
		AppID        string `json:"app_id"`
		AppName      string `json:"app_name"`
		TotalSeconds int64  `json:"total_seconds"`
		Duration     struct {
			Hours   int64 `json:"hours"`
			Minutes int64 `json:"minutes"`
		} `json:"duration"`
	}

	type deviceUsage struct {
		DeviceID string      `json:"device_id"`
		Apps     []appUsage  `json:"apps"`
	}

	deviceMap := make(map[string][]appUsage)

	for _, e := range entries {
		hours := e.TotalSeconds / 3600
		minutes := (e.TotalSeconds % 3600) / 60

		au := appUsage{
			AppID:        e.AppID,
			AppName:      e.AppName,
			TotalSeconds: e.TotalSeconds,
		}
		au.Duration.Hours = hours
		au.Duration.Minutes = minutes

		deviceMap[e.DeviceID] = append(deviceMap[e.DeviceID], au)
	}

	var devices []deviceUsage
	for devID, apps := range deviceMap {
		devices = append(devices, deviceUsage{
			DeviceID: devID,
			Apps:     apps,
		})
	}

	resp := struct {
		DayStart    time.Time    `json:"day_start"`
		Now         time.Time    `json:"now"`
		DeviceUsage []deviceUsage `json:"device_usage"`
	}{
		DayStart:    dayStart,
		Now:         nowLocal,
		DeviceUsage: devices,
	}

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
