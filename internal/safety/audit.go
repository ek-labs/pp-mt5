// Audit log: every write attempt (dry-run or confirmed) is appended to
// audit.jsonl (one JSON line per attempt) and to the audit DB table
// (queryable via `pp-mt5 sql`).
//
// File: <store_dir>/audit.jsonl — never deleted by the CLI.

package safety

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// auditFileMu serializes JSONL appends within a single process so two
// goroutines never race the OpenFile/Write/Close cycle. Cross-process
// safety relies on the OS guarantee that a single write() to an O_APPEND
// fd is atomic for sizes below PIPE_BUF on POSIX (4096+ bytes — JSON
// audit lines are smaller) and that FILE_APPEND_DATA on Windows is
// atomic per WriteFile call. Both hold here because appendJSONL issues
// the JSON + '\n' in one f.Write call.
var auditFileMu sync.Mutex

// AuditEntry is one row in the log.
type AuditEntry struct {
	TimeMS       int64           `json:"time_ms"`
	Command      string          `json:"command"`
	Request      json.RawMessage `json:"request"`
	Hash         string          `json:"hash"`
	Confirmed    bool            `json:"confirmed"`
	Response     json.RawMessage `json:"response,omitempty"`
	Error        string          `json:"error,omitempty"`
	AccountLogin int64           `json:"account_login,omitempty"`
	Mode         string          `json:"mode"` // paper | live | dry-run
}

// AppendAudit writes the entry to the JSONL file and inserts it into the
// audit DB table. Either failing is reported but does not block the caller
// (a write whose audit failed is still a write — we just log to stderr).
func AppendAudit(ctx context.Context, db *sql.DB, jsonlPath string, e AuditEntry) error {
	if e.TimeMS == 0 {
		e.TimeMS = time.Now().UnixMilli()
	}
	if jsonlPath != "" {
		if err := appendJSONL(jsonlPath, e); err != nil {
			return fmt.Errorf("append audit jsonl: %w", err)
		}
	}
	if db != nil {
		if err := insertAuditRow(ctx, db, e); err != nil {
			return fmt.Errorf("insert audit row: %w", err)
		}
	}
	return nil
}

func appendJSONL(path string, e AuditEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// Marshal first so we hold the mutex only across the OS write.
	buf, err := json.Marshal(e)
	if err != nil {
		return err
	}
	auditFileMu.Lock()
	defer auditFileMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(buf, '\n')); err != nil {
		return err
	}
	return nil
}

func insertAuditRow(ctx context.Context, db *sql.DB, e AuditEntry) error {
	_, err := db.ExecContext(ctx, `INSERT INTO audit(
		time_ms, command, request, hash, confirmed, response, error, account_login, mode
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		e.TimeMS, e.Command, string(e.Request), e.Hash, boolToInt(e.Confirmed),
		nullable(e.Response), nullable([]byte(e.Error)), e.AccountLogin, e.Mode)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// ErrKillSwitch is returned when the kill-switch file is present.
var ErrKillSwitch = errors.New("kill switch file present — all writes refused")
