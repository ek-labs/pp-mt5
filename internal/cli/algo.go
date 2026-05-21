package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"math"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/store"
)

// ── history (mirror reads) ───────────────────────────────────────────────────

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "history", Short: "Historical orders + deals from the local mirror"}
	cmd.AddCommand(newHistoryDealsCmd())
	cmd.AddCommand(newHistoryOrdersCmd())
	return cmd
}

func newHistoryDealsCmd() *cobra.Command {
	var (
		from, to string
		symbol   string
		magic    int64
	)
	cmd := &cobra.Command{
		Use:   "deals",
		Short: "Print historical deals from the local mirror (filter by --from/--to/--symbol/--magic)",
		Long: `Reads from the deals table — run 'pp-mt5 sync deals --from ... --to ...' first.

Outputs human table or JSON (auto-switches in non-TTY; --human-friendly forces).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromT, toT, err := parseRange(from, to)
			if err != nil {
				return &ExitErr{Code: ExitUsage, Err: err}
			}
			db, err := store.OpenAndMigrate("")
			if err != nil {
				return &ExitErr{Code: ExitConfig, Err: err}
			}
			defer db.Close()

			q := "SELECT ticket, time_ms, symbol, type, entry, volume, price, profit, commission, swap, fee, magic, position_id, comment FROM deals WHERE time_ms BETWEEN ? AND ?"
			args2 := []any{fromT.UnixMilli(), toT.UnixMilli()}
			if symbol != "" {
				q += " AND symbol = ?"
				args2 = append(args2, symbol)
			}
			if magic != 0 {
				q += " AND magic = ?"
				args2 = append(args2, magic)
			}
			q += " ORDER BY time_ms ASC"

			rows, err := db.QueryContext(cmd.Context(), q, args2...)
			if err != nil {
				return err
			}
			defer rows.Close()

			type d struct {
				Ticket     int64   `json:"ticket"`
				TimeMS     int64   `json:"time_ms"`
				Symbol     string  `json:"symbol"`
				Type       string  `json:"type"`
				Entry      string  `json:"entry"`
				Volume     float64 `json:"volume"`
				Price      float64 `json:"price"`
				Profit     float64 `json:"profit"`
				Commission float64 `json:"commission"`
				Swap       float64 `json:"swap"`
				Fee        float64 `json:"fee"`
				Magic      int64   `json:"magic"`
				PositionID int64   `json:"position_id"`
				Comment    string  `json:"comment"`
			}
			var out []d
			for rows.Next() {
				var x d
				if err := rows.Scan(&x.Ticket, &x.TimeMS, &x.Symbol, &x.Type, &x.Entry,
					&x.Volume, &x.Price, &x.Profit, &x.Commission, &x.Swap, &x.Fee,
					&x.Magic, &x.PositionID, &x.Comment); err != nil {
					return err
				}
				out = append(out, x)
			}
			if len(out) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "no deals in [%s, %s]. Did you `pp-mt5 sync deals`?\n", from, to)
			}
			return emit(cmd, out, func(w io.Writer, v any) {
				items := v.([]d)
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "TIME\tTICKET\tSYMBOL\tTYPE\tENTRY\tVOLUME\tPRICE\tP&L\tCOMM\tSWAP\tMAGIC")
				fmt.Fprintln(tw, "────\t──────\t──────\t────\t─────\t──────\t─────\t───\t────\t────\t─────")
				for _, x := range items {
					fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%g\t%g\t%+.2f\t%+.2f\t%+.2f\t%d\n",
						time.UnixMilli(x.TimeMS).Format("2006-01-02 15:04:05"),
						x.Ticket, x.Symbol, x.Type, x.Entry,
						x.Volume, x.Price, x.Profit, x.Commission, x.Swap, x.Magic)
				}
				tw.Flush()
				fmt.Fprintf(w, "\n(%d deal%s)\n", len(items), pluralS(len(items)))
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "30d", "ISO date or relative")
	cmd.Flags().StringVar(&to, "to", "now", "ISO date or relative")
	cmd.Flags().StringVar(&symbol, "symbol", "", "Restrict to one symbol")
	cmd.Flags().Int64Var(&magic, "magic", 0, "Restrict to one EA magic number")
	return cmd
}

func newHistoryOrdersCmd() *cobra.Command {
	var (
		from, to string
		symbol   string
	)
	cmd := &cobra.Command{
		Use:   "orders",
		Short: "Print historical orders from the local mirror",
		RunE: func(cmd *cobra.Command, args []string) error {
			fromT, toT, err := parseRange(from, to)
			if err != nil {
				return &ExitErr{Code: ExitUsage, Err: err}
			}
			db, err := store.OpenAndMigrate("")
			if err != nil {
				return &ExitErr{Code: ExitConfig, Err: err}
			}
			defer db.Close()
			q := "SELECT ticket, time_setup_ms, time_done_ms, symbol, type, state, volume_initial, price_open, sl, tp, magic, comment FROM history_orders WHERE time_setup_ms BETWEEN ? AND ?"
			args2 := []any{fromT.UnixMilli(), toT.UnixMilli()}
			if symbol != "" {
				q += " AND symbol = ?"
				args2 = append(args2, symbol)
			}
			q += " ORDER BY time_setup_ms ASC"
			rows, err := db.QueryContext(cmd.Context(), q, args2...)
			if err != nil {
				return err
			}
			defer rows.Close()
			type o struct {
				Ticket    int64   `json:"ticket"`
				Setup     int64   `json:"time_setup_ms"`
				Done      int64   `json:"time_done_ms"`
				Symbol    string  `json:"symbol"`
				Type      string  `json:"type"`
				State     string  `json:"state"`
				Volume    float64 `json:"volume_initial"`
				PriceOpen float64 `json:"price_open"`
				SL        float64 `json:"sl"`
				TP        float64 `json:"tp"`
				Magic     int64   `json:"magic"`
				Comment   string  `json:"comment"`
			}
			var out []o
			for rows.Next() {
				var x o
				if err := rows.Scan(&x.Ticket, &x.Setup, &x.Done, &x.Symbol, &x.Type, &x.State,
					&x.Volume, &x.PriceOpen, &x.SL, &x.TP, &x.Magic, &x.Comment); err != nil {
					return err
				}
				out = append(out, x)
			}
			if len(out) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "no orders in [%s, %s]. Did you `pp-mt5 sync history-orders`?\n", from, to)
			}
			return emit(cmd, out, func(w io.Writer, v any) {
				items := v.([]o)
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "SETUP\tDONE\tTICKET\tSYMBOL\tTYPE\tSTATE\tVOLUME\tPRICE\tSL\tTP\tMAGIC")
				fmt.Fprintln(tw, "─────\t────\t──────\t──────\t────\t─────\t──────\t─────\t──\t──\t─────")
				for _, x := range items {
					fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%g\t%g\t%g\t%g\t%d\n",
						time.UnixMilli(x.Setup).Format("2006-01-02 15:04"),
						time.UnixMilli(x.Done).Format("2006-01-02 15:04"),
						x.Ticket, x.Symbol, x.Type, x.State, x.Volume, x.PriceOpen, x.SL, x.TP, x.Magic)
				}
				tw.Flush()
				fmt.Fprintf(w, "\n(%d order%s)\n", len(items), pluralS(len(items)))
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "30d", "ISO date or relative")
	cmd.Flags().StringVar(&to, "to", "now", "ISO date or relative")
	cmd.Flags().StringVar(&symbol, "symbol", "", "Restrict to one symbol")
	return cmd
}

// ── stats summary ───────────────────────────────────────────────────────────
//
// Closed-trade aggregates derived from the deals table. Group deals by
// position_id, sum (profit + commission + swap + fee) per position. From
// that timeline we derive every classic stat: win rate, profit factor,
// expectancy, max drawdown, plus a daily-P&L Sharpe/Sortino.
//
// Sharpe/Sortino here are computed on daily P&L $ (not return %). That's
// honest given we don't have a per-day equity timeline; future phases can
// upgrade to true return-based versions when account snapshots are mirrored.

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stats", Short: "Trading statistics over historical deals"}

	var since string
	summary := &cobra.Command{
		Use:   "summary",
		Short: "Sharpe (daily $), Sortino, win rate, profit factor, max DD, expectancy",
		Long: `Aggregate statistics across closed positions in the local mirror.

A "closed position" is a position_id whose deals include an 'out' entry.
Per-position P&L = sum(profit + commission + swap + fee) across all its deals.

Reads from the deals table — run 'pp-mt5 sync deals --from ...' first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromT, err := parseSince(since)
			if err != nil {
				return &ExitErr{Code: ExitUsage, Err: err}
			}
			db, err := store.OpenAndMigrate("")
			if err != nil {
				return &ExitErr{Code: ExitConfig, Err: err}
			}
			defer db.Close()
			s, err := computeStatsSummary(cmd.Context(), db, fromT.UnixMilli(), time.Now().UnixMilli())
			if err != nil {
				return err
			}
			return emit(cmd, s, printStatsSummary)
		},
	}
	summary.Flags().StringVar(&since, "since", "30d", "Window: ISO date or relative (e.g. 90d, 1y)")
	cmd.AddCommand(summary)

	for _, sub := range []struct{ use, short, phase string }{
		{"by-symbol", "Stats grouped by symbol", "Phase 4"},
		{"by-hour", "Stats grouped by hour of day", "Phase 4"},
		{"by-day-of-week", "Stats grouped by weekday", "Phase 4"},
		{"by-magic", "Stats grouped by EA magic number", "Phase 4"},
		{"streaks", "Longest winning/losing runs and post-streak behavior", "Phase 4"},
		{"drawdown", "Every drawdown period: depth, duration, recovery time", "Phase 4"},
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

// StatsSummary is the JSON-shaped output of `stats summary`.
type StatsSummary struct {
	FromMS         int64   `json:"from_ms"`
	ToMS           int64   `json:"to_ms"`
	TotalTrades    int     `json:"total_trades"`
	Wins           int     `json:"wins"`
	Losses         int     `json:"losses"`
	BreakEven      int     `json:"break_even"`
	WinRate        float64 `json:"win_rate"`
	NetProfit      float64 `json:"net_profit"`
	GrossProfit    float64 `json:"gross_profit"`
	GrossLoss      float64 `json:"gross_loss"`
	ProfitFactor   float64 `json:"profit_factor"`
	AvgWin         float64 `json:"avg_win"`
	AvgLoss        float64 `json:"avg_loss"`
	Expectancy     float64 `json:"expectancy"`
	LargestWin     float64 `json:"largest_win"`
	LargestLoss    float64 `json:"largest_loss"`
	MaxDrawdown    float64 `json:"max_drawdown"`     // peak-to-trough $ on the realized equity curve
	MaxDrawdownPct float64 `json:"max_drawdown_pct"` // % of peak
	SharpeDaily    float64 `json:"sharpe_daily_pnl"` // daily-P&L Sharpe (annualized × √252)
	SortinoDaily   float64 `json:"sortino_daily_pnl"`
	TradingDays    int     `json:"trading_days"`
}

func computeStatsSummary(ctx context.Context, db *sql.DB, fromMS, toMS int64) (*StatsSummary, error) {
	// Per-position aggregation done in SQL.
	rows, err := db.QueryContext(ctx, `
		SELECT
		  position_id,
		  MIN(time_ms) AS first_time,
		  MAX(time_ms) AS last_time,
		  SUM(profit + commission + swap + fee) AS pnl,
		  SUM(CASE WHEN entry='out' THEN 1 ELSE 0 END) AS outs
		FROM deals
		WHERE time_ms BETWEEN ? AND ?
		  AND position_id <> 0
		GROUP BY position_id
		HAVING outs > 0
		ORDER BY last_time ASC
	`, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type trade struct {
		ID       int64
		FirstMS  int64
		LastMS   int64
		PnL      float64
	}
	var trades []trade
	for rows.Next() {
		var t trade
		var outs int
		if err := rows.Scan(&t.ID, &t.FirstMS, &t.LastMS, &t.PnL, &outs); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}

	s := &StatsSummary{FromMS: fromMS, ToMS: toMS, TotalTrades: len(trades)}
	if len(trades) == 0 {
		return s, nil
	}

	// Pass 1: P&L distribution + extremes.
	var winSum, lossSum float64
	for _, t := range trades {
		switch {
		case t.PnL > 0:
			s.Wins++
			winSum += t.PnL
			if t.PnL > s.LargestWin {
				s.LargestWin = t.PnL
			}
		case t.PnL < 0:
			s.Losses++
			lossSum += t.PnL
			if t.PnL < s.LargestLoss {
				s.LargestLoss = t.PnL
			}
		default:
			s.BreakEven++
		}
	}
	s.GrossProfit = winSum
	s.GrossLoss = -lossSum // make it positive
	s.NetProfit = winSum + lossSum
	if s.Wins+s.Losses > 0 {
		s.WinRate = float64(s.Wins) / float64(s.Wins+s.Losses)
	}
	if s.GrossLoss > 0 {
		s.ProfitFactor = s.GrossProfit / s.GrossLoss
	} else if s.GrossProfit > 0 {
		s.ProfitFactor = math.Inf(1)
	}
	if s.Wins > 0 {
		s.AvgWin = winSum / float64(s.Wins)
	}
	if s.Losses > 0 {
		s.AvgLoss = lossSum / float64(s.Losses) // negative
	}
	s.Expectancy = s.NetProfit / float64(s.TotalTrades)

	// Pass 2: realized equity curve + drawdown.
	var peak, equity float64
	for _, t := range trades {
		equity += t.PnL
		if equity > peak {
			peak = equity
		}
		dd := peak - equity
		if dd > s.MaxDrawdown {
			s.MaxDrawdown = dd
			if peak > 0 {
				s.MaxDrawdownPct = dd / peak * 100
			}
		}
	}

	// Pass 3: daily-P&L vector for Sharpe / Sortino.
	dayPnL := map[string]float64{}
	for _, t := range trades {
		key := time.UnixMilli(t.LastMS).UTC().Format("2006-01-02")
		dayPnL[key] += t.PnL
	}
	s.TradingDays = len(dayPnL)
	if s.TradingDays >= 2 {
		days := make([]float64, 0, s.TradingDays)
		for _, v := range dayPnL {
			days = append(days, v)
		}
		s.SharpeDaily = annualized(meanStd(days))
		s.SortinoDaily = annualized(meanDownsideStd(days))
	}
	return s, nil
}

func meanStd(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range xs {
		sum += v
	}
	mean := sum / float64(len(xs))
	var ssd float64
	for _, v := range xs {
		ssd += (v - mean) * (v - mean)
	}
	return mean, math.Sqrt(ssd / float64(len(xs)-1))
}

// meanDownsideStd returns mean over all xs but std over only negative entries.
func meanDownsideStd(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	negs := xs[:0:0]
	for _, v := range xs {
		sum += v
		if v < 0 {
			negs = append(negs, v)
		}
	}
	mean := sum / float64(len(xs))
	if len(negs) < 2 {
		return mean, 0
	}
	var ssd float64
	for _, v := range negs {
		ssd += v * v
	}
	return mean, math.Sqrt(ssd / float64(len(negs)-1))
}

func annualized(mean, std float64) float64 {
	if std == 0 {
		return 0
	}
	return mean / std * math.Sqrt(252)
}

func printStatsSummary(w io.Writer, v any) {
	s := v.(*StatsSummary)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"window",          fmt.Sprintf("%s → %s", time.UnixMilli(s.FromMS).Format("2006-01-02"), time.UnixMilli(s.ToMS).Format("2006-01-02"))},
		{"trades",          fmt.Sprintf("%d (wins=%d losses=%d be=%d)", s.TotalTrades, s.Wins, s.Losses, s.BreakEven)},
		{"win rate",        fmt.Sprintf("%.1f%%", s.WinRate*100)},
		{"net profit",      fmt.Sprintf("%+.2f", s.NetProfit)},
		{"gross profit",    fmt.Sprintf("%+.2f", s.GrossProfit)},
		{"gross loss",      fmt.Sprintf("-%.2f", s.GrossLoss)},
		{"profit factor",   formatPF(s.ProfitFactor)},
		{"avg win / loss",  fmt.Sprintf("%+.2f / %+.2f", s.AvgWin, s.AvgLoss)},
		{"expectancy",      fmt.Sprintf("%+.2f", s.Expectancy)},
		{"largest win/loss", fmt.Sprintf("%+.2f / %+.2f", s.LargestWin, s.LargestLoss)},
		{"max drawdown",    fmt.Sprintf("-%.2f (%.1f%% of peak)", s.MaxDrawdown, s.MaxDrawdownPct)},
		{"trading days",    fmt.Sprintf("%d", s.TradingDays)},
		{"Sharpe (daily $, ann.)",  fmt.Sprintf("%.2f", s.SharpeDaily)},
		{"Sortino (daily $, ann.)", fmt.Sprintf("%.2f", s.SortinoDaily)},
	}
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", r[0], r[1])
	}
	tw.Flush()
	if s.TotalTrades == 0 {
		fmt.Fprintln(w, "\nNo trades in window. Run `pp-mt5 sync deals --from ...` first.")
	}
}

func formatPF(v float64) string {
	if math.IsInf(v, 1) {
		return "∞ (no losses)"
	}
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", v)
}

// ── still-stub commands (Phase 4) ───────────────────────────────────────────

func newRMultiplesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "r-multiples",
		Short: "Express each closed trade as a multiple of risk",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 4") },
	}
	cmd.Flags().Float64("risk-per-trade", 0.01, "Risk per trade as fraction of equity at entry")
	return cmd
}

func newCorrelationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "correlation",
		Short: "Rolling correlation matrix across symbols",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 4") },
	}
	cmd.Flags().StringSlice("symbols", nil, "Symbols (required)")
	cmd.Flags().String("window", "30d", "Window")
	cmd.Flags().String("tf", "H1", "Timeframe")
	return cmd
}

func newMagicCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "magic", Short: "EA magic-number tools"}
	cmd.AddCommand(&cobra.Command{
		Use:   "audit",
		Short: "Group deals by magic number; surface dead and runaway EAs",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImpl("Phase 4") },
	})
	return cmd
}
