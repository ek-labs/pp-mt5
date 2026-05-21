// Package bridge is the Go side of the JSON-RPC link to bridge/mt5_bridge.py.
//
// Protocol (line-delimited JSON over stdio):
//
//	→ {"id":1,"method":"account_info","params":{}}
//	← {"id":1,"result":{...}}
//	← {"id":1,"error":{"code":"NOT_LOGGED_IN","message":"..."}}
//
// The bridge process is spawned once per CLI invocation and torn down on exit.
// Long-running operations (sync, replay, watch) keep it alive for the whole
// session; one-shot reads spawn-call-exit.
//
// Phase 1 will deliver: spawn, call, close, plus typed wrappers for the
// foundation commands (initialize, login, account_info, terminal_info,
// symbol_info, symbols_get, copy_rates_*, copy_ticks_*, positions_get,
// orders_get, history_orders_get, history_deals_get, order_check, order_send).
//
// Today this package contains only the skeleton needed for `doctor` to load
// the bridge module and report a useful self-test.
package bridge

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrNotLoggedIn et al. are sentinel errors the bridge returns. Callers
// pattern-match these to produce the right exit code in cli.ExitErr.
var (
	ErrNotLoggedIn      = errors.New("mt5: not logged in")
	ErrTerminalDown     = errors.New("mt5: terminal not running or not reachable")
	ErrBrokerRejected   = errors.New("mt5: broker rejected order")
	ErrRateLimited      = errors.New("mt5: rate limited by broker")
	ErrPythonMissing    = errors.New("python: not found on PATH")
	ErrMT5PkgMissing    = errors.New("python: MetaTrader5 package not installed")
)

// SelfTest is a cheap diagnostic used by `pp-mt5 doctor` before the full
// bridge is wired. It only verifies that we can find Python and that the
// bridge script file exists where we expect.
func SelfTest() string {
	py, err := findPython()
	if err != nil {
		return "python NOT found — " + err.Error()
	}
	script, err := bridgeScriptPath()
	if err != nil {
		return fmt.Sprintf("python OK (%s) but bridge script: %s", py, err)
	}
	if runtime.GOOS != "windows" {
		return fmt.Sprintf("python=%s bridge=%s (non-Windows host: live commands disabled, mirror-only commands work)", py, script)
	}
	return fmt.Sprintf("python=%s bridge=%s", py, script)
}

// findPython resolves the Python interpreter we will spawn. On Windows we
// prefer the `py` launcher; elsewhere we try `python3` then `python`.
func findPython() (string, error) {
	candidates := []string{"py", "python3", "python"}
	if runtime.GOOS != "windows" {
		candidates = []string{"python3", "python"}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", ErrPythonMissing
}

// bridgeScriptPath returns where mt5_bridge.py is expected to live. In dev,
// it sits next to the binary under bridge/mt5_bridge.py. Installed builds
// will embed it via go:embed in Phase 1; for now we just report the dev path.
func bridgeScriptPath() (string, error) {
	exe, err := exec.LookPath("mt5-pp-cli")
	if err != nil {
		// Allow dev runs from `go run` — return the source-tree path so
		// SelfTest at least reports something useful.
		return "bridge/mt5_bridge.py (resolved at runtime in Phase 1)", nil
	}
	dir := filepath.Dir(exe)
	return filepath.Join(dir, "bridge", "mt5_bridge.py"), nil
}
