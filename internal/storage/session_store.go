package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

type SessionStore struct {
	db *DB
}

func NewSessionStore(db *DB) *SessionStore {
	return &SessionStore{db: db}
}

// PollUpdate represents the normalized state for a device at a point in time.
type PollUpdate struct {
	DeviceID  string
	AppID     string
	AppName   string
	State     string // "active", "idle", "offline"
	Timestamp time.Time
}

type CurrentSession struct {
	DeviceID     string
	AppID        string
	AppName      string
	StartTime    time.Time
	LastSeenTime time.Time
	State        string
}

type Session struct {
	ID             int64
	DeviceID       string
	AppID          string
	AppName        string
	StartTime      time.Time
	EndTime        time.Time
	DurationSecs   int64
	EndReason      string
}

type UsageEntry struct {
	DeviceID      string
	AppID         string
	AppName       string
	TotalSeconds  int64
}

// CloseStaleCurrentSessions closes any rows left in current_sessions at startup.
func (s *SessionStore) CloseStaleCurrentSessions(ctx context.Context, now time.Time) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT device_id, app_id, app_name, start_time, last_seen_time, state
			FROM current_sessions`)
		if err != nil {
			return fmt.Errorf("query current_sessions: %w", err)
		}
		defer rows.Close()

		type rowData struct {
			deviceID     string
			appID        string
			appName      string
			startTime    time.Time
			lastSeenTime time.Time
			state        string
		}
		var rowsData []rowData

		for rows.Next() {
			var r rowData
			if err := rows.Scan(&r.deviceID, &r.appID, &r.appName, &r.startTime, &r.lastSeenTime, &r.state); err != nil {
				return fmt.Errorf("scan current_sessions: %w", err)
			}
			rowsData = append(rowsData, r)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate current_sessions: %w", err)
		}

		for _, r := range rowsData {
			end := r.lastSeenTime
			if end.After(now) {
				end = now
			}
			if end.Before(r.startTime) {
				end = r.startTime
			}
			dur := end.Sub(r.startTime).Seconds()
			if dur < 0 {
				dur = 0
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO sessions (device_id, app_id, app_name, start_time, end_time, duration_seconds, end_reason)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				r.deviceID, r.appID, r.appName, r.startTime, end, int64(dur), "agent_restart",
			); err != nil {
				return fmt.Errorf("insert session from current_sessions: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM current_sessions`); err != nil {
			return fmt.Errorf("delete current_sessions: %w", err)
		}

		return nil
	})
}

// ApplyPoll updates sessions based on a PollUpdate.
func (s *SessionStore) ApplyPoll(ctx context.Context, p PollUpdate) error {
	if p.DeviceID == "" {
		return fmt.Errorf("poll update missing device_id")
	}

	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var cur *CurrentSession

		row := tx.QueryRowContext(ctx, `
			SELECT device_id, app_id, app_name, start_time, last_seen_time, state
			FROM current_sessions
			WHERE device_id = ?`, p.DeviceID)

		var cs CurrentSession
		err := row.Scan(&cs.DeviceID, &cs.AppID, &cs.AppName, &cs.StartTime, &cs.LastSeenTime, &cs.State)
		if err == sql.ErrNoRows {
			cur = nil
		} else if err != nil {
			return fmt.Errorf("scan current_session: %w", err)
		} else {
			cur = &cs
		}

		switch p.State {
		case "active":
			if p.AppID == "" || p.AppName == "" {
				// nothing useful to do
				return nil
			}

			if cur == nil {
				// start new current session
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO current_sessions (device_id, app_id, app_name, start_time, last_seen_time, state)
					VALUES (?, ?, ?, ?, ?, 'active')`,
					p.DeviceID, p.AppID, p.AppName, p.Timestamp, p.Timestamp,
				); err != nil {
					return fmt.Errorf("insert current_session: %w", err)
				}
				return nil
			}

			if cur.AppID != p.AppID {
				// close old session
				if err := endSessionTx(ctx, tx, cur, p.Timestamp, "app_change"); err != nil {
					return err
				}
				// start new current session
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO current_sessions (device_id, app_id, app_name, start_time, last_seen_time, state)
					VALUES (?, ?, ?, ?, ?, 'active')`,
					p.DeviceID, p.AppID, p.AppName, p.Timestamp, p.Timestamp,
				); err != nil {
					return fmt.Errorf("insert new current_session: %w", err)
				}
				return nil
			}

			// same app: just update last_seen_time
			if _, err := tx.ExecContext(ctx, `
				UPDATE current_sessions
				SET last_seen_time = ?
				WHERE device_id = ?`,
				p.Timestamp, p.DeviceID,
			); err != nil {
				return fmt.Errorf("update current_session last_seen: %w", err)
			}

		case "idle", "offline":
			if cur == nil {
				return nil
			}
			if err := endSessionTx(ctx, tx, cur, p.Timestamp, p.State); err != nil {
				return err
			}
		default:
			// unknown state; ignore
			log.Printf("unknown poll state %q for device %s", p.State, p.DeviceID)
		}

		return nil
	})
}

func endSessionTx(ctx context.Context, tx *sql.Tx, cur *CurrentSession, end time.Time, reason string) error {
	if end.Before(cur.StartTime) {
		end = cur.StartTime
	}
	dur := end.Sub(cur.StartTime).Seconds()
	if dur < 0 {
		dur = 0
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (device_id, app_id, app_name, start_time, end_time, duration_seconds, end_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cur.DeviceID, cur.AppID, cur.AppName, cur.StartTime, end, int64(dur), reason,
	); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM current_sessions WHERE device_id = ?`, cur.DeviceID); err != nil {
		return fmt.Errorf("delete current_session: %w", err)
	}
	return nil
}

// GetCurrentSessions returns all active current_sessions.
func (s *SessionStore) GetCurrentSessions(ctx context.Context) ([]CurrentSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, app_id, app_name, start_time, last_seen_time, state
		FROM current_sessions`)
	if err != nil {
		return nil, fmt.Errorf("query current_sessions: %w", err)
	}
	defer rows.Close()

	var out []CurrentSession
	for rows.Next() {
		var cs CurrentSession
		if err := rows.Scan(&cs.DeviceID, &cs.AppID, &cs.AppName, &cs.StartTime, &cs.LastSeenTime, &cs.State); err != nil {
			return nil, fmt.Errorf("scan current_session: %w", err)
		}
		out = append(out, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current_sessions: %w", err)
	}
	return out, nil
}

// GetSessions returns historic sessions, optionally filtered.
func (s *SessionStore) GetSessions(ctx context.Context, deviceID *string, since, until *time.Time) ([]Session, error) {
	q := `
		SELECT id, device_id, app_id, app_name, start_time, end_time, duration_seconds, end_reason
		FROM sessions
		WHERE 1=1`
	var args []any

	if deviceID != nil {
		q += " AND device_id = ?"
		args = append(args, *deviceID)
	}
	if since != nil {
		q += " AND start_time >= ?"
		args = append(args, *since)
	}
	if until != nil {
		q += " AND start_time < ?"
		args = append(args, *until)
	}
	q += " ORDER BY start_time ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var se Session
		if err := rows.Scan(
			&se.ID, &se.DeviceID, &se.AppID, &se.AppName,
			&se.StartTime, &se.EndTime, &se.DurationSecs, &se.EndReason,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, se)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return out, nil
}

// GetUsageBetween aggregates usage per device/app between [start, end).
func (s *SessionStore) GetUsageBetween(
	ctx context.Context,
	start, end time.Time,
	deviceID *string,
) ([]UsageEntry, error) {
	if !start.Before(end) {
		return nil, nil
	}

	type key struct {
		deviceID string
		appID    string
		appName  string
	}
	agg := make(map[key]int64)

	// Closed sessions
	q := `
		SELECT device_id, app_id, app_name, start_time, end_time
		FROM sessions
		WHERE end_time > ? AND start_time < ?`
	args := []any{start, end}
	if deviceID != nil {
		q += " AND device_id = ?"
		args = append(args, *deviceID)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query sessions for usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var device, appID, appName string
		var sStart, sEnd time.Time
		if err := rows.Scan(&device, &appID, &appName, &sStart, &sEnd); err != nil {
			return nil, fmt.Errorf("scan session for usage: %w", err)
		}
		eStart := maxTime(start, sStart)
		eEnd := minTime(end, sEnd)
		if eEnd.After(eStart) {
			secs := int64(eEnd.Sub(eStart).Seconds())
			if secs < 0 {
				secs = 0
			}
			k := key{deviceID: device, appID: appID, appName: appName}
			agg[k] += secs
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions for usage: %w", err)
	}

	// Current sessions
	qCur := `
		SELECT device_id, app_id, app_name, start_time, last_seen_time
		FROM current_sessions`
	var argsCur []any
	if deviceID != nil {
		qCur += " WHERE device_id = ?"
		argsCur = append(argsCur, *deviceID)
	}

	rowsCur, err := s.db.QueryContext(ctx, qCur, argsCur...)
	if err != nil {
		return nil, fmt.Errorf("query current_sessions for usage: %w", err)
	}
	defer rowsCur.Close()

	now := end

	for rowsCur.Next() {
		var device, appID, appName string
		var sStart, sLast time.Time
		if err := rowsCur.Scan(&device, &appID, &appName, &sStart, &sLast); err != nil {
			return nil, fmt.Errorf("scan current_session for usage: %w", err)
		}
		sEnd := now
		if sLast.Before(sEnd) {
			sEnd = sLast
		}
		eStart := maxTime(start, sStart)
		eEnd := minTime(end, sEnd)
		if eEnd.After(eStart) {
			secs := int64(eEnd.Sub(eStart).Seconds())
			if secs < 0 {
				secs = 0
			}
			k := key{deviceID: device, appID: appID, appName: appName}
			agg[k] += secs
		}
	}
	if err := rowsCur.Err(); err != nil {
		return nil, fmt.Errorf("iterate current_sessions for usage: %w", err)
	}

	var out []UsageEntry
	for k, secs := range agg {
		out = append(out, UsageEntry{
			DeviceID:     k.deviceID,
			AppID:        k.appID,
			AppName:      k.appName,
			TotalSeconds: secs,
		})
	}

	return out, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
