// Package bridge is the Go side of the JSON-RPC link to bridge/mt5_bridge.py.
//
// One Bridge corresponds to one running python subprocess. Lifecycle:
//
//	b, err := bridge.New(bridge.Options{})
//	defer b.Close()
//	var acc bridge.AccountInfo
//	if err := b.Call("account_info", nil, &acc); err != nil { ... }
//
// Each pp-mt5 invocation typically spawns one bridge, executes a handful of
// calls, and tears it down on exit. Long-running operations (sync, replay,
// watch) keep the same bridge alive for the whole session.
//
// Protocol (line-delimited JSON-RPC over stdio):
//
//	→ {"id":1,"method":"account_info","params":{}}
//	← {"id":1,"result":{...}}
//	← {"id":1,"error":{"code":"NOT_LOGGED_IN","message":"..."}}
package bridge

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed mt5_bridge.py
var bridgeScript []byte

// ── sentinel errors ──────────────────────────────────────────────────────────
//
// Callers pattern-match on these to map to the right cli.ExitErr.Code.

var (
	ErrNotLoggedIn      = errors.New("mt5: not logged in")
	ErrTerminalDown     = errors.New("mt5: terminal not running or not reachable")
	ErrBrokerRejected   = errors.New("mt5: broker rejected order")
	ErrRateLimited      = errors.New("mt5: rate limited by broker")
	ErrPythonMissing    = errors.New("python: not found on PATH")
	ErrMT5PkgMissing    = errors.New("python: MetaTrader5 package not installed")
	ErrInvalidParams    = errors.New("bridge: invalid params")
	ErrBridgeInternal   = errors.New("bridge: internal error")
)

// errFromCode maps a bridge error code string to a Go sentinel.
func errFromCode(code, message string) error {
	switch code {
	case "NOT_LOGGED_IN":
		return fmt.Errorf("%w: %s", ErrNotLoggedIn, message)
	case "TERMINAL_DOWN":
		return fmt.Errorf("%w: %s", ErrTerminalDown, message)
	case "BROKER_REJECTED":
		return fmt.Errorf("%w: %s", ErrBrokerRejected, message)
	case "RATE_LIMITED":
		return fmt.Errorf("%w: %s", ErrRateLimited, message)
	case "PYTHON_MT5_MISSING":
		return fmt.Errorf("%w: %s", ErrMT5PkgMissing, message)
	case "INVALID_PARAMS":
		return fmt.Errorf("%w: %s", ErrInvalidParams, message)
	default:
		return fmt.Errorf("%w (%s): %s", ErrBridgeInternal, code, message)
	}
}

// ── Bridge ───────────────────────────────────────────────────────────────────

type Options struct {
	// PythonPath overrides interpreter resolution. Default: auto-detect.
	PythonPath string
	// ScriptPath overrides where the bridge script is read from. When empty,
	// the embedded script is written to a temp file and used. Setting this is
	// only useful when iterating on the Python script during development.
	ScriptPath string
	// Stderr receives the subprocess's stderr. nil discards it.
	Stderr io.Writer
	// CallTimeout bounds a single Call. 0 = no per-call timeout.
	CallTimeout time.Duration
}

type Bridge struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex // serializes writes; the protocol is line-oriented
	closed  atomic.Bool
	nextID  atomic.Int64
	timeout time.Duration
}

type request struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// New spawns a python subprocess and returns a Bridge ready for Call().
func New(opts Options) (*Bridge, error) {
	py := opts.PythonPath
	if py == "" {
		p, err := FindPython()
		if err != nil {
			return nil, err
		}
		py = p
	}
	script := opts.ScriptPath
	if script == "" {
		p, err := materializeScript()
		if err != nil {
			return nil, err
		}
		script = p
	}

	cmd := exec.Command(py, script)
	cmd.Stderr = opts.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn python bridge (%s %s): %w", py, script, err)
	}

	return &Bridge{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		timeout: opts.CallTimeout,
	}, nil
}

// Close shuts the bridge down. Safe to call multiple times.
func (b *Bridge) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = b.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- b.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		_ = b.cmd.Process.Kill()
		<-done
		return errors.New("bridge: timed out, killed subprocess")
	}
}

// Call invokes a named method on the bridge and decodes the result into out.
// out may be nil to discard the result. Pass params=nil for handlers that take
// no parameters; otherwise any JSON-encodable value works.
func (b *Bridge) Call(method string, params any, out any) error {
	if b.closed.Load() {
		return errors.New("bridge: closed")
	}
	id := b.nextID.Add(1)
	req := request{ID: id, Method: method, Params: params}

	buf, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("bridge marshal: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, err := b.stdin.Write(append(buf, '\n')); err != nil {
		return fmt.Errorf("bridge write: %w", err)
	}

	type readResult struct {
		line []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := b.stdout.ReadBytes('\n')
		ch <- readResult{line, err}
	}()

	var line []byte
	if b.timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
		defer cancel()
		select {
		case r := <-ch:
			line, err = r.line, r.err
		case <-ctx.Done():
			return fmt.Errorf("bridge: call %s timed out after %s", method, b.timeout)
		}
	} else {
		r := <-ch
		line, err = r.line, r.err
	}
	if err != nil && len(line) == 0 {
		return fmt.Errorf("bridge read (%s): %w", method, err)
	}

	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("bridge: unparseable response for %s: %w (raw: %s)", method, err, line)
	}
	if resp.Error != nil {
		return errFromCode(resp.Error.Code, resp.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("bridge: decode result for %s: %w (raw: %s)", method, err, resp.Result)
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// FindPython resolves the Python interpreter we will spawn. On Windows we
// prefer the `py` launcher (handles versioned installs); elsewhere we try
// `python3` then `python`.
func FindPython() (string, error) {
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

// materializeScript writes the embedded bridge script to a stable temp path
// and returns it. Stable filename means re-runs reuse the same file (lets the
// user inspect or attach a debugger).
func materializeScript() (string, error) {
	dir := filepath.Join(os.TempDir(), "pp-mt5")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mt5_bridge.py")
	// Only overwrite if missing or stale (size mismatch). Avoids races between
	// concurrent processes appending to the same path.
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(bridgeScript)) {
		return path, nil
	}
	if err := os.WriteFile(path, bridgeScript, 0644); err != nil {
		return "", fmt.Errorf("write embedded bridge script: %w", err)
	}
	return path, nil
}

// SelfTest is a cheap diagnostic used by `pp-mt5 doctor`. It reports what we
// can determine without actually spawning the bridge.
func SelfTest() string {
	py, err := FindPython()
	if err != nil {
		return "python NOT found — " + err.Error()
	}
	script, err := materializeScript()
	if err != nil {
		return fmt.Sprintf("python OK (%s) but bridge script: %s", py, err)
	}
	suffix := ""
	if runtime.GOOS != "windows" {
		suffix = " (non-Windows host: live commands disabled, mirror-only commands work)"
	}
	return fmt.Sprintf("python=%s bridge=%s%s", py, script, suffix)
}
