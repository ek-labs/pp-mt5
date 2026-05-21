package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/safety"
)

// ── symbols / quote / book ───────────────────────────────────────────────────

func newSymbolsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "symbols", Short: "Symbol catalog"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List visible symbols (optionally filtered)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	list.Flags().String("filter", "", "Glob filter, e.g. EUR*")
	cmd.AddCommand(list)
	cmd.AddCommand(&cobra.Command{
		Use:   "info <SYMBOL>",
		Short: "Full symbol_info() dump",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	})
	return cmd
}

func newQuoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quote <SYMBOL>",
		Short: "Last tick: bid/ask/last/spread/time",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
}

func newBookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "book <SYMBOL>",
		Short: "Market Depth snapshot (requires broker to support DOM)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
}

// ── positions / orders ───────────────────────────────────────────────────────

func newPositionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "positions", Short: "Open positions"}
	list := &cobra.Command{
		Use:   "list",
		Short: "Print all open positions",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	list.Flags().String("filter", "", "SQL-like WHERE clause, e.g. \"symbol='EURUSD' AND profit<0\"")
	cmd.AddCommand(list)
	return cmd
}

func newOrdersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "orders", Short: "Active (pending) orders"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Print all active orders",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	})
	return cmd
}

// ── order (writes — go through safety) ───────────────────────────────────────

func newOrderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "order", Short: "Place/preview a new order"}
	cmd.AddCommand(newOrderCheckCmd())
	cmd.AddCommand(newOrderSendCmd())
	return cmd
}

func newOrderCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Preview margin + validity (MT5 order_check) — never sends",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImpl("Phase 5")
		},
	}
	addOrderFlags(cmd)
	return cmd
}

func newOrderSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Place a market or pending order (DRY-RUN by default; requires safety hash)",
		Long: `Place an order on the connected broker.

Safety flow:
  1. First run prints a SHA-256 hash of the canonical request and exits with code 6
     (safety-rejected) if the user did not also confirm.
  2. To actually send, re-run with --confirm <hash> within 60 seconds.
  3. If the broker account is live, MT5_LIVE=1 AND --i-understand-this-is-live
     must both be set. Either missing → exit 6.
  4. Per-command guardrails from ~/.config/mt5-pp-cli/config.toml apply
     (max_volume_per_order, max_open_positions, max_daily_loss, kill_switch_file).
  5. Successful send is appended to ~/.local/share/mt5-pp-cli/audit.jsonl.

Example:
  pp-mt5 order send --symbol EURUSD --side buy --volume 0.10 --sl 1.0800 --tp 1.1000
  # prints hash, exits 6
  pp-mt5 order send --symbol EURUSD --side buy --volume 0.10 --sl 1.0800 --tp 1.1000 --confirm <hash> --i-understand-this-is-live`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := safety.PrecheckWrite(cmd); err != nil {
				return &ExitErr{Code: ExitSafetyRejected, Err: err}
			}
			return notImpl("Phase 7")
		},
	}
	addOrderFlags(cmd)
	safety.AddWriteFlags(cmd)
	return cmd
}

func addOrderFlags(cmd *cobra.Command) {
	cmd.Flags().String("symbol", "", "Symbol (required)")
	cmd.Flags().String("side", "", "buy | sell | buy_limit | sell_limit | buy_stop | sell_stop")
	cmd.Flags().Float64("volume", 0, "Lot size (required)")
	cmd.Flags().Float64("price", 0, "Limit/stop price (pending orders only)")
	cmd.Flags().Float64("sl", 0, "Stop loss price")
	cmd.Flags().Float64("tp", 0, "Take profit price")
	cmd.Flags().Int64("magic", 0, "EA magic number")
	cmd.Flags().String("comment", "", "Order comment")
	cmd.Flags().Int("deviation", 20, "Max price deviation in points")
	cmd.Flags().String("type-filling", "", "IOC | FOK | RETURN (broker-supported subset)")
	cmd.Flags().String("type-time", "", "GTC | DAY | SPECIFIED | SPECIFIED_DAY")
}

// ── position (writes) ────────────────────────────────────────────────────────

func newPositionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "position", Short: "Operate on an open position by ticket"}

	closeCmd := &cobra.Command{
		Use:   "close <ticket>",
		Short: "Close a position (DRY-RUN by default; safety hash required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := safety.PrecheckWrite(cmd); err != nil {
				return &ExitErr{Code: ExitSafetyRejected, Err: err}
			}
			return notImpl("Phase 7")
		},
	}
	closeCmd.Flags().Float64("partial", 0, "Close only this fraction of volume (0<x<1)")
	safety.AddWriteFlags(closeCmd)
	cmd.AddCommand(closeCmd)

	modify := &cobra.Command{
		Use:   "modify <ticket>",
		Short: "Modify SL/TP on an open position (DRY-RUN by default; safety hash required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := safety.PrecheckWrite(cmd); err != nil {
				return &ExitErr{Code: ExitSafetyRejected, Err: err}
			}
			return notImpl("Phase 7")
		},
	}
	modify.Flags().Float64("sl", 0, "New stop loss")
	modify.Flags().Float64("tp", 0, "New take profit")
	safety.AddWriteFlags(modify)
	cmd.AddCommand(modify)

	return cmd
}

// ── close all (hero command's engine) ────────────────────────────────────────

func newCloseAllCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Bulk operations across positions",
		Long:  "Bulk close positions matching a SQL predicate. Powers the /pp-mt5 close all losing positions ... hero command.",
	}
	all := &cobra.Command{
		Use:   "all",
		Short: "Close every position matching --filter (SQL WHERE clause)",
		Long: `Close every open position matching a SQL-style WHERE clause against the positions table.

Examples:
  pp-mt5 close all --filter "profit < -50"                        # all losers worse than -50
  pp-mt5 close all --filter "symbol like 'XAU%' AND magic = 0"    # all manual gold positions
  pp-mt5 close all --filter "1=1"                                 # everything (very explicit)

Safety: prints the resolved ticket list, total exposure, and a SHA-256 hash, then exits 6.
Re-run with --confirm <hash> --i-understand-this-is-live to actually fire.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := safety.PrecheckWrite(cmd); err != nil {
				return &ExitErr{Code: ExitSafetyRejected, Err: err}
			}
			filter, _ := cmd.Flags().GetString("filter")
			if filter == "" {
				return &ExitErr{Code: ExitUsage, Err: fmt.Errorf("--filter is required (use \"1=1\" for everything; explicit is the point)")}
			}
			return notImpl("Phase 7")
		},
	}
	all.Flags().String("filter", "", "SQL WHERE clause against the positions table (required)")
	safety.AddWriteFlags(all)
	cmd.AddCommand(all)
	return cmd
}

// ── risk preview ─────────────────────────────────────────────────────────────

func newRiskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "risk", Short: "Risk preview tools"}
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Join order_calc_margin + order_calc_profit at ±N pips + current account state",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	preview.Flags().String("symbol", "", "Symbol (required)")
	preview.Flags().String("side", "buy", "buy | sell")
	preview.Flags().Float64("volume", 0, "Lot size (required)")
	preview.Flags().IntSlice("pips", []int{-50, -25, 25, 50, 100}, "Pip offsets to project P&L at")
	cmd.AddCommand(preview)
	return cmd
}
