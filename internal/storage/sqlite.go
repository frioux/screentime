package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func NewDB(ctx context.Context, path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable foreign keys (good practice, even if not heavily used here)
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		log.Printf("warning: failed to enable foreign_keys pragma: %v", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	wrapped := &DB{DB: db}
	if err := wrapped.runMigrations(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return wrapped, nil
}

func (db *DB) runMigrations(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			app_id TEXT NOT NULL,
			app_name TEXT NOT NULL,
			start_time DATETIME NOT NULL,
			end_time DATETIME NOT NULL,
			duration_seconds INTEGER NOT NULL,
			end_reason TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_device_time
		 ON sessions(device_id, start_time);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_app_time
		 ON sessions(app_name, start_time);`,
		`CREATE TABLE IF NOT EXISTS current_sessions (
			device_id TEXT PRIMARY KEY,
			app_id TEXT NOT NULL,
			app_name TEXT NOT NULL,
			start_time DATETIME NOT NULL,
			last_seen_time DATETIME NOT NULL,
			state TEXT NOT NULL
		);`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("run migration: %w", err)
		}
	}
	return nil
}

// WithTx executes fn inside a transaction.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("rollback error: %v", rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
