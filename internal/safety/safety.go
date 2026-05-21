// Package safety enforces the write-command guardrails documented in the spec.
//
// Defense in depth — every write command goes through this package:
//
//  1. Live-mode gate: MT5_LIVE=1 in env AND --i-understand-this-is-live on the
//     command must BOTH be present, otherwise the request is rejected before
//     anything reaches the Python bridge.
//
//  2. Hash-confirm dry-run: on first invocation the command computes a
//     deterministic SHA-256 over the canonical request and prints it. The
//     user must re-invoke with --confirm <hash> within 60 seconds for the
//     command to actually fire. Time-bounded confirms can't be replayed.
//
//  3. Per-command config guardrails from ~/.config/mt5-pp-cli/config.toml:
//     max_volume_per_order, max_open_positions, max_daily_loss, kill_switch_file.
//     A kill switch file's existence rejects all writes unconditionally.
//
//  4. Audit log append to ~/.local/share/mt5-pp-cli/audit.jsonl. Every write
//     request, hash, and response is recorded. Never deleted by the CLI.
//
// PAPER mode (MT5_PAPER=1, set on first run if no config exists) routes the
// confirmed request to the Python bridge but tags it as paper trading so the
// bridge can either short-circuit or send to a demo account.
package safety

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// Mode enumerates the three trading modes.
type Mode int

const (
	ModePaper Mode = iota // MT5_PAPER=1 or unset and live not unlocked
	ModeLive              // MT5_LIVE=1 AND --i-understand-this-is-live
	ModeDryRun            // --dry-run explicitly set
)

// Window is how long a printed hash remains valid for --confirm.
const Window = 60 * time.Second

// CurrentMode reports the trading mode the current process is in based on env
// vars only (per-command flags are checked separately by PrecheckWrite).
func CurrentMode() Mode {
	if os.Getenv("MT5_LIVE") == "1" {
		return ModeLive
	}
	return ModePaper
}

// ModeDescription is a one-line human description for `pp-mt5 doctor`.
func ModeDescription() string {
	if CurrentMode() == ModeLive {
		return "MT5_LIVE=1 — live trading ENABLED; per-command --i-understand-this-is-live still required for every write"
	}
	return "PAPER (default) — writes go to a demo account or short-circuit. Set MT5_LIVE=1 to unlock live."
}

// AddWriteFlags adds --confirm and --i-understand-this-is-live to a command.
// Call once per write command in its constructor.
func AddWriteFlags(cmd *cobra.Command) {
	cmd.Flags().String("confirm", "", "SHA-256 hash from a prior dry-run; arms the actual send")
	cmd.Flags().Bool("i-understand-this-is-live", false, "Required (with MT5_LIVE=1) for live writes")
}

// PrecheckWrite runs the safety preflight. Returns an error to be wrapped in
// a cli.ExitErr{Code: ExitSafetyRejected}. Returns nil if the request is OK to
// proceed to the bridge.
//
// This function only enforces the rules that don't need the bridge. The
// per-command guardrails (max_volume_per_order, kill_switch_file, etc.) live
// in CheckGuardrails which the caller invokes after building the request.
func PrecheckWrite(cmd *cobra.Command) error {
	if isLiveAccountIntent(cmd) {
		if CurrentMode() != ModeLive {
			return errors.New("live writes require MT5_LIVE=1 in the environment")
		}
		live, _ := cmd.Flags().GetBool("i-understand-this-is-live")
		if !live {
			return errors.New("live writes also require --i-understand-this-is-live on the command line")
		}
	}
	return nil
}

// isLiveAccountIntent is a stub that returns true today. Phase 7 will infer
// account.tradeMode from the bridge and skip safety checks for demo accounts.
// Bias to safe: if we don't know, assume live.
func isLiveAccountIntent(cmd *cobra.Command) bool {
	if os.Getenv("MT5_PAPER") == "1" {
		return false
	}
	return true
}

// Hash returns the canonical SHA-256 hash of a request. The request must be
// JSON-marshalable. Determinism comes from json.Marshal with sorted keys; the
// timestamp is rounded to the Window so two invocations inside the same window
// produce the same hash (so the user can copy/paste the hash they just saw).
func Hash(request any) (string, error) {
	canon, err := canonicalize(request)
	if err != nil {
		return "", err
	}
	bucket := time.Now().UTC().Truncate(Window).Unix()
	payload := struct {
		Bucket  int64           `json:"bucket"`
		Request json.RawMessage `json:"request"`
	}{bucket, canon}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// Verify returns true if 'provided' matches the current hash (this window) or
// the previous one (so a user has the full Window to type the hash, not zero).
func Verify(provided string, request any) (bool, error) {
	canon, err := canonicalize(request)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	for _, t := range []time.Time{now, now.Add(-Window)} {
		bucket := t.Truncate(Window).Unix()
		payload := struct {
			Bucket  int64           `json:"bucket"`
			Request json.RawMessage `json:"request"`
		}{bucket, canon}
		buf, _ := json.Marshal(payload)
		sum := sha256.Sum256(buf)
		if hex.EncodeToString(sum[:]) == provided {
			return true, nil
		}
	}
	return false, nil
}

func canonicalize(v any) (json.RawMessage, error) {
	// json.Marshal already sorts map keys; for structs the field order in Go
	// is stable across runs. That's enough for our determinism guarantee.
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	return buf, nil
}

// Guardrails come from ~/.config/mt5-pp-cli/config.toml; loaded in Phase 6.
type Guardrails struct {
	MaxVolumePerOrder float64 `toml:"max_volume_per_order"`
	MaxOpenPositions  int     `toml:"max_open_positions"`
	MaxDailyLoss      float64 `toml:"max_daily_loss"`
	KillSwitchFile    string  `toml:"kill_switch_file"`
}

// CheckGuardrails enforces the per-command thresholds. Phase 6 will fully
// implement and unit-test this; for now it just enforces the kill switch
// because that one's the highest-impact and trivial.
func CheckGuardrails(g Guardrails, req map[string]any) error {
	if g.KillSwitchFile != "" {
		if _, err := os.Stat(g.KillSwitchFile); err == nil {
			return fmt.Errorf("kill switch file present: %s — all writes refused", g.KillSwitchFile)
		}
	}
	return nil
}
