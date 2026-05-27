// Package store opens the local SQLite mirror and runs migrations.
// The mirror is the source of truth for stats, replay, backtest, and
// `pp-mt5 sql`; the bridge fills it via sync.
//
// Path resolution (first hit wins):
//
//	$MT5_PP_STORE
//	$XDG_DATA_HOME/pp-mt5/store.db
//	Windows: %LOCALAPPDATA%\pp-mt5\store.db
//	Mac:     ~/Library/Application Support/pp-mt5/store.db
//	Linux:   ~/.local/share/pp-mt5/store.db
//
// SQLite driver is modernc.org/sqlite — pure Go, no cgo.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DefaultPath returns the platform-specific path to the store database.
func DefaultPath() string {
	if env := os.Getenv("MT5_PP_STORE"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pp-mt5", "store.db")
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "pp-mt5", "store.db")
		}
		return filepath.Join(home, "AppData", "Local", "pp-mt5", "store.db")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "pp-mt5", "store.db")
	default:
		return filepath.Join(home, ".local", "share", "pp-mt5", "store.db")
	}
}

// AuditPath returns where audit.jsonl lives (sibling of store.db).
func AuditPath() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "audit.jsonl")
}

// Open returns a connection to the store, creating the parent directory.
// Use OpenAndMigrate if you also want to apply pending migrations.
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

// OpenReadOnly opens the store with mode=ro. SQLite refuses every write at the
// engine level — including writes hidden inside a CTE (`WITH x AS (...) DELETE
// FROM ...`) — so callers that route untrusted SQL (e.g. the MCP server's
// mt5_sql tool) can give the DB to the user without auditing each query.
//
// Errors out if the file doesn't exist yet — read-only mode can't create the
// file, and silently creating an empty mirror would mask "you forgot to sync"
// as "your query returned no rows".
func OpenReadOnly(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultPath()
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("store not initialized at %s — run `pp-mt5 sync all` first", path)
		}
		return nil, err
	}
	// mode=ro: SQLite opens the file read-only and refuses any write.
	// immutable=1 would be stricter still but breaks concurrent writers on the
	// same file. WAL mode already lets a writer and many readers coexist.
	dsn := "file:" + path + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open store ro: %w", err)
	}
	return db, nil
}

// OpenAndMigrate opens the store and applies any pending migrations from the
// embedded migrations directory. Idempotent.
func OpenAndMigrate(path string) (*sql.DB, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies every pending migration. Each migration runs in its own
// transaction so a partial apply rolls back cleanly; the runner re-checks
// applied versions INSIDE the transaction so a concurrent process that
// raced ahead doesn't cause a duplicate-column/-table error.
//
// Migration files are pure DDL/DML — no inline BEGIN/COMMIT and no manual
// INSERT INTO schema_migrations. The runner owns bookkeeping.
func Migrate(ctx context.Context, db *sql.DB) error {
	files, err := listMigrations()
	if err != nil {
		return err
	}

	// Bootstrap schema_migrations before we can query for applied versions.
	// IF NOT EXISTS makes this safe to re-run against any state.
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now') * 1000)
	)`); err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	for _, m := range files {
		body, err := fs.ReadFile(migrationsFS, m.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.Name, err)
		}
		if err := applyOneMigration(ctx, db, m, string(body)); err != nil {
			return err
		}
	}
	return nil
}

// applyOneMigration runs a single migration inside an immediate transaction.
// The pre-check inside the tx is the lock: under SQLite WAL the BEGIN
// IMMEDIATE acquires the reserved lock; a concurrent process trying the
// same migration will block on busy_timeout (configured in the DSN), then
// see the already-applied row and skip.
func applyOneMigration(ctx context.Context, db *sql.DB, m migration, body string) error {
	// BEGIN IMMEDIATE acquires the reserved lock up front so concurrent
	// readers don't slip in between our pre-check and the DDL.
	if _, err := db.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("acquire migration lock for %s: %w", m.Name, err)
	}
	rollback := func() { _, _ = db.ExecContext(ctx, `ROLLBACK`) }

	// Re-check under the lock — another process may have applied this
	// while we were waiting for the busy_timeout window.
	var seen int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = ?`, m.Version).Scan(&seen)
	if err == nil {
		// Already applied by a concurrent process; nothing to do.
		_, _ = db.ExecContext(ctx, `COMMIT`)
		return nil
	}
	if err != sql.ErrNoRows {
		rollback()
		return fmt.Errorf("re-check migration %s under lock: %w", m.Name, err)
	}

	if _, err := db.ExecContext(ctx, body); err != nil {
		rollback()
		return fmt.Errorf("apply migration %s: %w", m.Name, err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO schema_migrations(version) VALUES (?)`, m.Version); err != nil {
		rollback()
		return fmt.Errorf("record migration %s: %w", m.Name, err)
	}
	if _, err := db.ExecContext(ctx, `COMMIT`); err != nil {
		rollback()
		return fmt.Errorf("commit migration %s: %w", m.Name, err)
	}
	return nil
}

type migration struct {
	Version int
	Name    string
	path    string
}

func listMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// Filename convention: NNNN_name.sql
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: missing NNNN_ prefix", e.Name())
		}
		ver, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %s: bad version prefix: %w", e.Name(), err)
		}
		out = append(out, migration{Version: ver, Name: e.Name(), path: "migrations/" + e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

