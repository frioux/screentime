package poller

import (
	"context"
	"log"
	"time"

	"screentime-agent/internal/config"
	"screentime-agent/internal/storage"
)

type Runner struct {
	devices []config.DeviceConfig
	store   *storage.SessionStore
}

func NewRunner(devices []config.DeviceConfig, store *storage.SessionStore) *Runner {
	return &Runner{
		devices: devices,
		store:   store,
	}
}

func (r *Runner) Start(ctx context.Context) {
	for _, d := range r.devices {
		dev := d
		go r.runDevice(ctx, dev)
	}
}

func (r *Runner) runDevice(ctx context.Context, d config.DeviceConfig) {
	poller := NewRokuPoller(d.ID, d.BaseURL)
	interval := time.Duration(d.PollIntervalSeconds) * time.Second

	doPoll := func() {
		pollCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		result, err := poller.Poll(pollCtx)
		if err != nil {
			log.Printf("device %s poll error: %v", d.ID, err)
			return
		}

		update := storage.PollUpdate{
			DeviceID:  result.DeviceID,
			AppID:     result.AppID,
			AppName:   result.AppName,
			State:     result.State,
			Timestamp: result.Timestamp,
		}

		if err := r.store.ApplyPoll(ctx, update); err != nil {
			log.Printf("device %s apply poll error: %v", d.ID, err)
		}
	}

	// Initial poll
	doPoll()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doPoll()
		}
	}
}
