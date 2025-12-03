package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type DeviceConfig struct {
	ID                  string   `json:"id"`
	BaseURL             string   `json:"base_url"`
	PollIntervalSeconds int      `json:"poll_interval_seconds"`
	Tags                []string `json:"tags,omitempty"`
}

type Config struct {
	DatabasePath string         `json:"database_path"`
	HTTPListen   string         `json:"http_listen"`
	DayStartHour int            `json:"day_start_hour"`
	Timezone     string         `json:"timezone"`
	Devices      []DeviceConfig `json:"devices"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Defaults
	if cfg.HTTPListen == "" {
		cfg.HTTPListen = ":8080"
	}
	if cfg.DayStartHour == 0 {
		cfg.DayStartHour = 7
	}

	// Basic validation
	if cfg.DatabasePath == "" {
		return nil, fmt.Errorf("database_path is required")
	}
	if len(cfg.Devices) == 0 {
		return nil, fmt.Errorf("at least one device is required")
	}
	for i, d := range cfg.Devices {
		if d.ID == "" {
			return nil, fmt.Errorf("devices[%d].id is required", i)
		}
		if d.BaseURL == "" {
			return nil, fmt.Errorf("devices[%d].base_url is required", i)
		}
		if d.PollIntervalSeconds <= 0 {
			return nil, fmt.Errorf("devices[%d].poll_interval_seconds must be > 0", i)
		}
	}

	return &cfg, nil
}

// ResolveLocation returns the time.Location for the config timezone or system local.
func (c *Config) ResolveLocation() (*time.Location, error) {
	if c.Timezone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", c.Timezone, err)
	}
	return loc, nil
}

// ComputeDayWindow computes the start of the "current day" and now, adjusted so that
// if current time is before the configured day start hour, the window is from
// yesterday's day_start to now.
func (c *Config) ComputeDayWindow(ctx context.Context, loc *time.Location) (time.Time, time.Time) {
	now := time.Now().In(loc)
	year, month, day := now.Date()
	dayStart := time.Date(year, month, day, c.DayStartHour, 0, 0, 0, loc)
	if now.Before(dayStart) {
		dayStart = dayStart.AddDate(0, 0, -1)
	}
	return dayStart, now
}
