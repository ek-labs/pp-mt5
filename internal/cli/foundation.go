package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/bridge"
	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/safety"
	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/store"
)

// defaultInit returns an InitializeOptions seeded with MT5_PATH (if set) and
// the given MT5-side timeout in ms. Used by every command that just needs to
// attach to an already-running terminal.
func defaultInit(timeoutMs int) bridge.InitializeOptions {
	o := bridge.InitializeOptions{Timeout: timeoutMs}
	if p := terminalPathFromEnv(); p != "" {
		o.Path = p
	}
	return o
}

// terminalPathFromEnv resolves MT5_PATH to an absolute terminal64.exe path,
// accepting either a directory or a full .exe path. Returns "" if unset/missing.
func terminalPathFromEnv() string {
	env := os.Getenv("MT5_PATH")
	if env == "" {
		return ""
	}
	if fi, err := os.Stat(env); err == nil {
		if fi.IsDir() {
			cand := filepath.Join(env, "terminal64.exe")
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
			return ""
		}
		return env
	}
	return ""
}

// ── doctor ───────────────────────────────────────────────────────────────────

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify Python, MetaTrader5 package, terminal, login, and network path",
		Long: `Run a full preflight check of the mt5-pp-cli install.

Each check prints PASS, FAIL, or SKIP with the exact remediation hint.
If anything red blocks live commands, doctor reports it before you discover
it inside a write command.`,
		RunE: runDoctor,
	}
}

type checkResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // pass | fail | skip
	Detail    string `json:"detail,omitempty"`
	Remediate string `json:"remediate,omitempty"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	results := []checkResult{}

	// 1. Python on PATH
	py, err := bridge.FindPython()
	if err != nil {
		results = append(results, checkResult{
			Name: "python", Status: "fail", Detail: err.Error(),
			Remediate: "Install Python 3.10+ from https://python.org or via 'winget install Python.Python.3.13'",
		})
	} else {
		results = append(results, checkResult{Name: "python", Status: "pass", Detail: py})
	}

	// 2. MetaTrader5 package importable + (3) bridge spawn
	var b *bridge.Bridge
	if err == nil {
		// Drain bridge stderr into our own stderr so unhandled tracebacks surface.
		b, err = bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 20 * time.Second})
		if err != nil {
			results = append(results, checkResult{
				Name: "bridge spawn", Status: "fail", Detail: err.Error(),
				Remediate: "Check that 'py -3' works and that mt5_bridge.py parses (run: py -3 -c \"import MetaTrader5\")",
			})
		}
	}
	if b != nil {
		defer b.Close()
	}

	// 3. OS check (informational)
	osStatus := "pass"
	osDetail := runtime.GOOS
	osRemediate := ""
	if runtime.GOOS != "windows" {
		osStatus = "skip"
		osDetail = runtime.GOOS + " (live commands disabled; replay/sql/stats/backtest still work against the local mirror)"
	}
	results = append(results, checkResult{Name: "host OS == windows", Status: osStatus, Detail: osDetail, Remediate: osRemediate})

	// 4. MetaTrader5 package + 5. terminal initialize.
	// Pass MT5_PATH if set so initialize() can spawn terminal64.exe if it isn't
	// already running. mt5 timeout is 5s so it fails fast; bridge wraps at 20s.
	if b != nil {
		err := b.Initialize(defaultInit(5000))
		switch {
		case err == nil:
			results = append(results, checkResult{Name: "MetaTrader5 package", Status: "pass"})
			results = append(results, checkResult{Name: "mt5.initialize()", Status: "pass"})
		case errors.Is(err, bridge.ErrMT5PkgMissing):
			results = append(results, checkResult{
				Name: "MetaTrader5 package", Status: "fail", Detail: err.Error(),
				Remediate: "py -3 -m pip install MetaTrader5  (Windows only — Mac/Linux can't use live commands)",
			})
			results = append(results, checkResult{Name: "mt5.initialize()", Status: "skip", Detail: "blocked by previous failure"})
		case errors.Is(err, bridge.ErrTerminalDown):
			results = append(results, checkResult{Name: "MetaTrader5 package", Status: "pass"})
			results = append(results, checkResult{
				Name: "mt5.initialize()", Status: "fail", Detail: err.Error(),
				Remediate: "Start the MetaTrader 5 terminal (terminal64.exe) and log into any account, then re-run doctor",
			})
		default:
			// A bridge timeout almost always means MT5 wasn't running and
			// auto-spawn either didn't try or didn't return. Translate.
			results = append(results, checkResult{Name: "MetaTrader5 package", Status: "pass"})
			results = append(results, checkResult{
				Name: "mt5.initialize()", Status: "fail", Detail: err.Error(),
				Remediate: "Start MetaTrader 5 (open the terminal and log in once), or set MT5_PATH to terminal64.exe so initialize() can auto-spawn it",
			})
		}
	}

	// 6. account_info() — proves login + broker reachable
	if b != nil {
		acc, err := b.AccountInfo()
		switch {
		case err == nil:
			results = append(results, checkResult{
				Name: "account_info()", Status: "pass",
				Detail: fmt.Sprintf("login=%d server=%s mode=%s currency=%s balance=%.2f",
					acc.Login, acc.Server, acc.TradeModeName(), acc.Currency, acc.Balance),
			})
		case errors.Is(err, bridge.ErrNotLoggedIn):
			results = append(results, checkResult{
				Name: "account_info()", Status: "fail", Detail: "no account logged in",
				Remediate: "Log into a broker in the MT5 terminal, or run: pp-mt5 connect login --account ... --server ... --password-env MT5_PASSWORD",
			})
		default:
			results = append(results, checkResult{Name: "account_info()", Status: "skip", Detail: err.Error()})
		}
	}

	// 7. store.db writable
	dbPath := store.DefaultPath()
	if db, err := store.Open(dbPath); err != nil {
		results = append(results, checkResult{
			Name: "store.db writable", Status: "fail", Detail: err.Error(),
			Remediate: "Ensure parent directory is writable: " + dbPath,
		})
	} else {
		_ = db.Ping()
		_ = db.Close()
		results = append(results, checkResult{Name: "store.db writable", Status: "pass", Detail: dbPath})
	}

	// 8. safety mode (informational)
	results = append(results, checkResult{
		Name: "safety mode", Status: "pass", Detail: safety.ModeDescription(),
	})

	// emit
	if jsonFlag(cmd) {
		enc := json.NewEncoder(out)
		if !compactFlag(cmd) {
			enc.SetIndent("", "  ")
		}
		_ = enc.Encode(results)
	} else {
		printChecks(out, results)
	}

	// Exit non-zero if anything blocks live commands; SKIPs are informational.
	for _, r := range results {
		if r.Status == "fail" {
			return &ExitErr{Code: ExitTerminalDown, Err: fmt.Errorf("doctor: %s — see remediation above", r.Name)}
		}
	}
	return nil
}

func printChecks(w io.Writer, results []checkResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAIL")
	fmt.Fprintln(tw, "─────\t──────\t──────")
	for _, r := range results {
		mark := "?"
		switch r.Status {
		case "pass":
			mark = "✓ pass"
		case "fail":
			mark = "✗ FAIL"
		case "skip":
			mark = "~ skip"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Name, mark, r.Detail)
	}
	tw.Flush()

	hasFail := false
	for _, r := range results {
		if r.Status == "fail" && r.Remediate != "" {
			if !hasFail {
				fmt.Fprintln(w, "\nRemediation:")
				hasFail = true
			}
			fmt.Fprintf(w, "  %s → %s\n", r.Name, r.Remediate)
		}
	}
}

// ── connect ──────────────────────────────────────────────────────────────────

func newConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Manage MT5 broker connections",
		Long:  "Manage broker login state. Credentials are read from env vars referenced by --password-env; never stored on disk by this CLI.",
	}
	cmd.AddCommand(newConnectLoginCmd())
	cmd.AddCommand(newConnectLogoutCmd())
	cmd.AddCommand(newConnectStatusCmd())
	return cmd
}

func newConnectLoginCmd() *cobra.Command {
	var (
		account     int64
		server      string
		passwordEnv string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a broker account via the Python bridge",
		Long: `Log in to a broker account. The password is read from the named env
var (never echoed, never stored). Example:

  $env:MT5_PASSWORD = "..."
  pp-mt5 connect login --account 12345678 --server "Broker-Live" --password-env MT5_PASSWORD

Note: the MT5 terminal itself remembers the active login between processes,
so subsequent pp-mt5 calls reconnect to that session without re-logging in.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if account == 0 {
				return &ExitErr{Code: ExitUsage, Err: fmt.Errorf("--account is required")}
			}
			if server == "" {
				return &ExitErr{Code: ExitUsage, Err: fmt.Errorf("--server is required")}
			}
			if passwordEnv == "" {
				return &ExitErr{Code: ExitUsage, Err: fmt.Errorf("--password-env is required (env var name, not the password)")}
			}
			password := os.Getenv(passwordEnv)
			if password == "" {
				return &ExitErr{Code: ExitConfig, Err: fmt.Errorf("env var %q is empty", passwordEnv)}
			}

			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 30 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()

			if err := b.Initialize(bridge.InitializeOptions{Login: account, Password: password, Server: server, Timeout: 20000}); err != nil {
				return mapBridgeErr(err)
			}
			acc, err := b.AccountInfo()
			if err != nil {
				return mapBridgeErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "logged in: %d @ %s (%s, %s, balance=%.2f)\n",
				acc.Login, acc.Server, acc.TradeModeName(), acc.Currency, acc.Balance)
			return nil
		},
	}
	cmd.Flags().Int64Var(&account, "account", 0, "Broker account number (required)")
	cmd.Flags().StringVar(&server, "server", "", "Broker server name (required)")
	cmd.Flags().StringVar(&passwordEnv, "password-env", "MT5_PASSWORD", "Env var that holds the password")
	return cmd
}

func newConnectLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Disconnect this bridge from the terminal (terminal keeps its session)",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr()})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(5000)); err != nil {
				return mapBridgeErr(err)
			}
			if err := b.Shutdown(); err != nil {
				return mapBridgeErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "bridge shutdown OK (terminal session retained)")
			return nil
		},
	}
}

func newConnectStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current login + connection state",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(5000)); err != nil {
				return mapBridgeErr(err)
			}
			acc, err := b.AccountInfo()
			if err != nil {
				return mapBridgeErr(err)
			}
			term, _ := b.TerminalInfo()
			fmt.Fprintf(cmd.OutOrStdout(), "account: %d @ %s (%s, %s)\n", acc.Login, acc.Server, acc.TradeModeName(), acc.Currency)
			if term != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "terminal: build=%d connected=%v trade_allowed=%v ping=%d\n",
					term.Build, term.Connected, term.TradeAllowed, term.PingLast)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "safety: %s\n", safety.ModeDescription())
			return nil
		},
	}
}

// ── account / terminal ───────────────────────────────────────────────────────

func newAccountCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Account-level info"}
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Print account balance, equity, margin, leverage, currency, trade mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			acc, err := b.AccountInfo()
			if err != nil {
				return mapBridgeErr(err)
			}
			return emit(cmd, acc, printAccount)
		},
	})
	return cmd
}

func newTerminalCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "terminal", Short: "MT5 terminal info"}
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Print terminal build, data path, connected/trade-allowed flags",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			term, err := b.TerminalInfo()
			if err != nil {
				return mapBridgeErr(err)
			}
			return emit(cmd, term, printTerminal)
		},
	})
	return cmd
}

func printAccount(w io.Writer, v any) {
	a := v.(*bridge.AccountInfo)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"login", fmt.Sprintf("%d", a.Login)},
		{"name", a.Name},
		{"server", a.Server},
		{"company", a.Company},
		{"mode", a.TradeModeName()},
		{"currency", a.Currency},
		{"leverage", fmt.Sprintf("1:%d", a.Leverage)},
		{"balance", fmt.Sprintf("%.2f %s", a.Balance, a.Currency)},
		{"equity", fmt.Sprintf("%.2f %s", a.Equity, a.Currency)},
		{"profit", fmt.Sprintf("%+.2f %s", a.Profit, a.Currency)},
		{"margin", fmt.Sprintf("%.2f %s", a.Margin, a.Currency)},
		{"margin_free", fmt.Sprintf("%.2f %s", a.MarginFree, a.Currency)},
		{"margin_level", fmt.Sprintf("%.2f%%", a.MarginLevel)},
		{"trade_allowed", fmt.Sprintf("%v", a.TradeAllowed)},
		{"trade_expert", fmt.Sprintf("%v", a.TradeExpert)},
	}
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", r[0], r[1])
	}
	tw.Flush()
}

func printTerminal(w io.Writer, v any) {
	t := v.(*bridge.TerminalInfo)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"name", t.Name},
		{"company", t.Company},
		{"build", fmt.Sprintf("%d", t.Build)},
		{"language", t.Language},
		{"connected", fmt.Sprintf("%v", t.Connected)},
		{"trade_allowed", fmt.Sprintf("%v", t.TradeAllowed)},
		{"dlls_allowed", fmt.Sprintf("%v", t.DLLsAllowed)},
		{"ping_last_us", fmt.Sprintf("%d", t.PingLast)},
		{"path", t.Path},
		{"data_path", t.DataPath},
		{"commondata_path", t.CommondataPath},
	}
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", r[0], r[1])
	}
	tw.Flush()
}

// ── sync / sql (still phase 2; keep stubs identical to scaffold) ─────────────

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Mirror MT5 data into the local SQLite store",
		Long: `Pull symbols, bars, ticks, orders, deals, and positions into the local SQLite store.

The mirror is the source of truth for stats, replay, backtest, and sql. First
sync of a large account can take minutes; subsequent runs are incremental.`,
	}
	var since string
	syncAll := &cobra.Command{
		Use:   "all",
		Short: "Bulk pull every symbol's bars + ticks + every order/deal since --since",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = since
			return notImpl("Phase 2")
		},
	}
	syncAll.Flags().StringVar(&since, "since", "2020-01-01", "ISO date (YYYY-MM-DD) or relative (30d, 2y)")
	syncAll.Flags().Bool("no-ticks", false, "Skip tick history (orders of magnitude faster)")
	syncAll.Flags().StringSlice("only-symbols", nil, "Restrict sync to these symbols (comma-separated)")
	cmd.AddCommand(syncAll)

	for _, sub := range []struct{ use, short, phase string }{
		{"symbols", "Sync the symbols table only", "Phase 2"},
		{"bars --symbol EURUSD --tf M5", "Sync bars for one symbol+timeframe", "Phase 2"},
		{"ticks --symbol EURUSD", "Sync ticks for one symbol", "Phase 2"},
		{"deals", "Sync historical deals only", "Phase 2"},
		{"orders", "Sync historical orders only", "Phase 2"},
		{"positions", "Snapshot current open positions", "Phase 2"},
	} {
		s := sub
		cmd.AddCommand(&cobra.Command{
			Use:   s.use,
			Short: s.short,
			RunE:  func(cmd *cobra.Command, args []string) error { return notImpl(s.phase) },
		})
	}
	return cmd
}

func newSQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql \"<query>\"",
		Short: "Run an arbitrary SQL query against the local mirror",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 2") },
	}
	cmd.Flags().Bool("write", false, "Allow INSERT/UPDATE/DELETE statements")
	return cmd
}

// ── shared helpers ───────────────────────────────────────────────────────────

// mapBridgeErr converts a bridge sentinel into an ExitErr with the right code.
func mapBridgeErr(err error) error {
	switch {
	case errors.Is(err, bridge.ErrNotLoggedIn):
		return &ExitErr{Code: ExitAuth, Err: err}
	case errors.Is(err, bridge.ErrTerminalDown):
		return &ExitErr{Code: ExitTerminalDown, Err: err}
	case errors.Is(err, bridge.ErrBrokerRejected):
		return &ExitErr{Code: ExitBrokerRejected, Err: err}
	case errors.Is(err, bridge.ErrRateLimited):
		return &ExitErr{Code: ExitRateLimited, Err: err}
	case errors.Is(err, bridge.ErrMT5PkgMissing):
		return &ExitErr{Code: ExitConfig, Err: err}
	default:
		return err
	}
}

// emit prints the value as JSON if --json/--agent is set, otherwise calls human.
func emit(cmd *cobra.Command, v any, human func(io.Writer, any)) error {
	w := cmd.OutOrStdout()
	if jsonFlag(cmd) {
		enc := json.NewEncoder(w)
		if !compactFlag(cmd) {
			enc.SetIndent("", "  ")
		}
		return enc.Encode(v)
	}
	human(w, v)
	return nil
}

func jsonFlag(cmd *cobra.Command) bool {
	// Explicit human-friendly always wins.
	if b, _ := cmd.Flags().GetBool("human-friendly"); b {
		return false
	}
	if b, _ := cmd.Flags().GetBool("json"); b {
		return true
	}
	if b, _ := cmd.Flags().GetBool("agent"); b {
		return true
	}
	// auto: emit JSON when stdout isn't a TTY
	if !isTerminal(cmd.OutOrStdout()) {
		return true
	}
	return false
}

func compactFlag(cmd *cobra.Command) bool {
	if b, _ := cmd.Flags().GetBool("compact"); b {
		return true
	}
	if b, _ := cmd.Flags().GetBool("agent"); b {
		return true
	}
	return false
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
