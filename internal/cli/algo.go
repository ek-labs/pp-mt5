package cli

import "github.com/spf13/cobra"

// ── history ──────────────────────────────────────────────────────────────────

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "history", Short: "Historical orders + deals from local mirror"}

	deals := &cobra.Command{
		Use:   "deals",
		Short: "All historical deals from local mirror",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	deals.Flags().String("from", "", "ISO date or relative (e.g. 30d)")
	deals.Flags().String("to", "today", "ISO date or relative")
	cmd.AddCommand(deals)

	orders := &cobra.Command{
		Use:   "orders",
		Short: "All historical orders from local mirror",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	orders.Flags().String("from", "", "ISO date or relative")
	orders.Flags().String("to", "today", "ISO date or relative")
	cmd.AddCommand(orders)

	return cmd
}

// ── stats ────────────────────────────────────────────────────────────────────

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stats", Short: "Trading statistics over historical deals"}

	summary := &cobra.Command{
		Use:   "summary",
		Short: "Sharpe, Sortino, win rate, profit factor, max DD, avg R, expectancy",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	summary.Flags().String("since", "30d", "Window: ISO date or relative")
	cmd.AddCommand(summary)

	for _, sub := range []struct{ use, short string }{
		{"by-symbol", "Stats grouped by symbol"},
		{"by-hour", "Stats grouped by hour of day"},
		{"by-day-of-week", "Stats grouped by weekday"},
		{"by-magic", "Stats grouped by EA magic number"},
		{"streaks", "Longest winning/losing runs and post-streak behavior"},
		{"drawdown", "Every drawdown period: depth, duration, recovery time"},
	} {
		s := sub
		cmd.AddCommand(&cobra.Command{
			Use:   s.use,
			Short: s.short,
			RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
		})
	}
	return cmd
}

func newRMultiplesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "r-multiples",
		Short: "Express each closed trade as a multiple of risk",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	cmd.Flags().Float64("risk-per-trade", 0.01, "Risk per trade as fraction of equity at entry")
	return cmd
}

func newCorrelationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "correlation",
		Short: "Rolling correlation matrix across symbols",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	}
	cmd.Flags().StringSlice("symbols", nil, "Symbols (required)")
	cmd.Flags().String("window", "30d", "Window: ISO duration or relative")
	cmd.Flags().String("tf", "H1", "Timeframe")
	return cmd
}

func newMagicCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "magic", Short: "EA magic-number tools"}
	cmd.AddCommand(&cobra.Command{
		Use:   "audit",
		Short: "Group deals by magic number; surface dead and runaway EAs",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 3") },
	})
	return cmd
}
