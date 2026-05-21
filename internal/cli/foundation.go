package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/bridge"
	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/safety"
)

// ── doctor ───────────────────────────────────────────────────────────────────
//
// `pp-mt5 doctor` is the single source of truth for "is the install healthy?"
// It must explain each failure clearly enough that an agent or a tired trader
// can fix it without reading docs. Live MT5 is Windows-only, the terminal must
// be running, and we need a logged-in account before any other command works.

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify Python, MetaTrader5 package, terminal, login, and network path",
		Long: `Run a full preflight check of the mt5-pp-cli install.

Checks:
  1. Python 3.10+ is reachable on PATH
  2. The 'MetaTrader5' Python package is installed in that Python
  3. Host OS is Windows (live MT5 is Windows-only)
  4. The MT5 terminal process is running
  5. mt5.initialize() succeeds
  6. mt5.login(...) succeeds (if a profile is selected)
  7. mt5.account_info() returns a real account
  8. Local SQLite store at ~/.local/share/mt5-pp-cli/store.db is writable
  9. Safety: prints whether you are currently in MT5_LIVE mode

On any failure, prints a one-line root cause AND a specific remediation hint
(install command, env var to set, file to create, etc.).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "doctor is scaffolded — full implementation lands in Phase 1.")
			fmt.Fprintln(os.Stderr, "What it will check (and how to fix each):")
			for _, line := range []string{
				"  [ ] python3 on PATH                  → install Python 3.10+",
				"  [ ] MetaTrader5 package importable   → py -3 -m pip install MetaTrader5",
				"  [ ] OS == Windows for live commands  → Mac/Linux supports replay/sql/stats/backtest only",
				"  [ ] terminal64.exe running           → start MetaTrader 5",
				"  [ ] bridge spawn succeeds            → see bridge/mt5_bridge.py",
				"  [ ] mt5.initialize() == True         → bridge.initialize",
				"  [ ] mt5.login(...) succeeds          → pp-mt5 connect login",
				"  [ ] account_info() != nil            → broker server reachable",
				"  [ ] store.db writable                → ~/.local/share/mt5-pp-cli/",
				"  [*] safety mode:                     " + safety.ModeDescription(),
			} {
				fmt.Fprintln(os.Stderr, line)
			}
			fmt.Fprintln(os.Stderr, "\nBridge module loads:", bridge.SelfTest())
			return notImpl("Phase 1")
		},
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
		Long: `Log in to a broker account.

The password is read from the environment variable named by --password-env.
It is never logged, stored, or echoed. Examples:

  $env:MT5_PASSWORD = "..."   # PowerShell
  pp-mt5 connect login --account 12345678 --server "Broker-Live" --password-env MT5_PASSWORD`,
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
			if os.Getenv(passwordEnv) == "" {
				return &ExitErr{Code: ExitConfig, Err: fmt.Errorf("env var %q is empty", passwordEnv)}
			}
			return notImpl("Phase 1")
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
		Short: "Disconnect from the current broker session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImpl("Phase 1")
		},
	}
}

func newConnectStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current login + connection state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImpl("Phase 1")
		},
	}
}

// ── account / terminal ───────────────────────────────────────────────────────

func newAccountCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Account-level info"}
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Print account balance, equity, margin, leverage, currency, trade mode",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 1") },
	})
	return cmd
}

func newTerminalCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "terminal", Short: "MT5 terminal info"}
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Print terminal build, data path, connected/trade-allowed flags",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 1") },
	})
	return cmd
}

// ── sync / sql ───────────────────────────────────────────────────────────────

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Mirror MT5 data into the local SQLite store",
		Long: `Pull symbols, bars, ticks, orders, deals, and positions into ~/.local/share/mt5-pp-cli/store.db.

The mirror is the source of truth for stats, replay, backtest, and sql.
First sync of a large account can take minutes; subsequent runs are incremental.`,
	}
	var since string
	syncAll := &cobra.Command{
		Use:   "all",
		Short: "Bulk pull every symbol's bars + ticks + every order/deal since --since",
		Long: `Fetch:
  - All visible symbols, snapshot to symbols table
  - All bars across timeframes M1..MN1 since --since
  - All ticks since --since (large — consider --no-ticks)
  - All historical orders + deals since --since
  - Current positions snapshot

Resumable: re-runs continue where the previous run left off. Safe to interrupt.`,
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
		Long: `Execute a read-only or write SQL statement against ~/.local/share/mt5-pp-cli/store.db.

Tables:
  symbols, ticks, bars_M1, bars_M5, bars_M15, bars_M30,
  bars_H1, bars_H4, bars_D1, bars_W1, bars_MN1,
  orders, history_orders, positions, deals,
  backtests, calendar_events, features,
  audit (append-only),
  schema_migrations.

Read-only by default. Pass --write to allow INSERT/UPDATE/DELETE.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImpl("Phase 2")
		},
	}
	cmd.Flags().Bool("write", false, "Allow INSERT/UPDATE/DELETE statements")
	return cmd
}
