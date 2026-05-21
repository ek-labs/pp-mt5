// Package store opens the local SQLite mirror and runs migrations from
// library/trading/mt5/migrations/. The mirror is the source of truth for
// stats, replay, backtest, and `pp-mt5 sql`; the bridge fills it via sync.
//
// Path: $XDG_DATA_HOME/mt5-pp-cli/store.db, falling back to:
//
//	Windows: %LOCALAPPDATA%\mt5-pp-cli\store.db
//	Mac:     ~/Library/Application Support/mt5-pp-cli/store.db
//	Linux:   ~/.local/share/mt5-pp-cli/store.db
//
// SQLite driver is modernc.org/sqlite — pure Go, no cgo dependency.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite"
)

// DefaultPath returns the platform-specific path to the store database.
func DefaultPath() string {
	if env := os.Getenv("MT5_PP_STORE"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "mt5-pp-cli", "store.db")
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "mt5-pp-cli", "store.db")
		}
		return filepath.Join(home, "AppData", "Local", "mt5-pp-cli", "store.db")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "mt5-pp-cli", "store.db")
	default:
		return filepath.Join(home, ".local", "share", "mt5-pp-cli", "store.db")
	}
}

// Open returns a connection to the store, creating the parent directory and
// running pending migrations on first call. Idempotent.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("mkdir store dir: %w", err)
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return db, nil
}

// AuditPath returns where audit.jsonl lives (sibling of store.db).
func AuditPath() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "audit.jsonl")
}
