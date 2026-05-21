package cli

import "github.com/spf13/cobra"

// ── bars / ticks copy + export ───────────────────────────────────────────────

func newBarsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bars", Short: "Bar (OHLCV) operations"}

	copyCmd := &cobra.Command{
		Use:   "copy",
		Short: "Copy bars from local mirror to parquet/csv/jsonl",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	}
	copyCmd.Flags().String("symbol", "", "Symbol (required)")
	copyCmd.Flags().String("tf", "H1", "Timeframe: M1..MN1")
	copyCmd.Flags().String("from", "", "ISO date or relative")
	copyCmd.Flags().String("to", "today", "ISO date or relative")
	copyCmd.Flags().String("out", "", "Output spec: parquet:path | csv:path | jsonl:path | -")
	cmd.AddCommand(copyCmd)

	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Bulk export many symbols + timeframes at once",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	}
	exportCmd.Flags().String("tf", "M1", "Timeframe: M1..MN1")
	exportCmd.Flags().String("symbols", "*", "Glob filter (e.g. 'EUR*,XAU*')")
	exportCmd.Flags().String("since", "2y", "ISO date or relative")
	exportCmd.Flags().String("out", "parquet", "Output format: parquet | csv | jsonl")
	cmd.AddCommand(exportCmd)

	return cmd
}

func newTicksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ticks", Short: "Tick operations"}
	copyCmd := &cobra.Command{
		Use:   "copy",
		Short: "Copy ticks from local mirror",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	}
	copyCmd.Flags().String("symbol", "", "Symbol (required)")
	copyCmd.Flags().String("from", "", "ISO date/time")
	copyCmd.Flags().String("to", "now", "ISO date/time")
	copyCmd.Flags().String("flag", "all", "all | info | trade")
	copyCmd.Flags().String("out", "", "Output spec: parquet:path | csv:path | -")
	cmd.AddCommand(copyCmd)
	return cmd
}

func newFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "features", Short: "Derived feature engineering on the local mirror"}
	cmd.AddCommand(&cobra.Command{
		Use:   "build",
		Short: "Derive returns, log-returns, ATR, RSI, realized vol into the features table",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	})
	return cmd
}

// ── economic calendar ────────────────────────────────────────────────────────

func newCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "calendar", Short: "Economic calendar (synced into local mirror)"}
	cmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Sync upcoming + recent economic events into calendar_events",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	})
	near := &cobra.Command{
		Use:   "near",
		Short: "Print events near the current time (or --at)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	}
	near.Flags().String("event", "", "Event name filter (e.g. NFP, FOMC)")
	near.Flags().String("window", "1h", "Window radius (e.g. 30m, 1h, 2d)")
	near.Flags().String("at", "now", "Anchor time (default: now)")
	cmd.AddCommand(near)
	return cmd
}

// ── replay ──────────────────────────────────────────────────────────────────

func newReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Stream historical bars/ticks from the local mirror to stdout",
		Long: `Tick-accurate replay engine, fed from the local mirror.

Outputs JSONL on stdout — one event per line. Designed to be piped into a
backtest harness or visualization. Works offline once data is synced.`,
		RunE: func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	}
	cmd.Flags().String("symbol", "", "Symbol (required)")
	cmd.Flags().String("from", "", "ISO date/time")
	cmd.Flags().String("to", "", "ISO date/time")
	cmd.Flags().String("speed", "real", "Playback speed: real | 10x | 100x | max")
	cmd.Flags().String("granularity", "tick", "tick | bar:M1 | bar:M5 ...")
	return cmd
}

// ── backtest ────────────────────────────────────────────────────────────────

func newBacktestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "backtest", Short: "Event-loop backtester over the local mirror"}
	run := &cobra.Command{
		Use:   "run",
		Short: "Run a strategy.py file against historical data; persist result to backtests table",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	}
	run.Flags().String("strategy", "", "Path to a Python strategy file (required)")
	run.Flags().String("symbol", "", "Symbol (required)")
	run.Flags().String("tf", "H1", "Timeframe")
	run.Flags().String("from", "", "Start ISO date")
	run.Flags().String("to", "today", "End ISO date")
	run.Flags().Float64("deposit", 10000, "Starting equity")
	cmd.AddCommand(run)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List past backtests stored in the backtests table",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 9") },
	})
	return cmd
}

// ── helper EA (Phase 2 stub) ────────────────────────────────────────────────

func newHelperCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "helper", Short: "MQL5 helper EA for live trade events (Phase 2)"}
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Scaffold the helper EA directory tree (Phase 2 stub today)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImpl("Phase 2 helper EA — see library/trading/mt5/helper/TODO.md for the design and what v2 will deliver")
		},
	})
	return cmd
}

// ── watch (tail) — Phase 2 ──────────────────────────────────────────────────

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "watch", Short: "Tail live MT5 events"}
	trades := &cobra.Command{
		Use:   "trades",
		Short: "Tail trade events (Phase 2; requires helper EA)",
		Long: `Stream trade events as they happen.

v1 workaround until the helper EA lands: poll 'pp-mt5 positions list --json' on
an interval (every 1-5 seconds) and diff. The helper EA in Phase 2 replaces
polling with an OnTradeTransaction event stream.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImpl("Phase 2 (helper EA). v1 workaround: poll positions list on an interval.")
		},
	}
	trades.Flags().Bool("tail", true, "Stream until interrupted")
	trades.Flags().Duration("poll", 0, "v1 workaround: poll positions list at this interval instead")
	cmd.AddCommand(trades)
	return cmd
}
