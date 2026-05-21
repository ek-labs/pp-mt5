package cli

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/bridge"
	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/safety"
	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/store"
)

// ── symbols ──────────────────────────────────────────────────────────────────
//
// `symbols list` reads the local mirror (run `pp-mt5 sync symbols` first).
// `symbols info` is a live bridge call — spread, bid, ask move in real time.

func newSymbolsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "symbols", Short: "Symbol catalog"}

	var filter string
	list := &cobra.Command{
		Use:   "list",
		Short: "List symbols from the local mirror (optionally --filter EUR*)",
		Long: `List the broker's tradeable symbols from the local mirror.

Reads from the symbols table — run 'pp-mt5 sync symbols' first if it's empty.
--filter is a glob pattern translated to SQL LIKE (* → %, ? → _).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := store.OpenAndMigrate("")
			if err != nil {
				return &ExitErr{Code: ExitConfig, Err: err}
			}
			defer db.Close()

			q := "SELECT symbol, description, digits, point, spread, volume_min, volume_max, volume_step, base_currency, profit_currency FROM symbols"
			var rowsArgs []any
			if filter != "" {
				q += " WHERE symbol LIKE ?"
				rowsArgs = append(rowsArgs, globToLike(filter))
			}
			q += " ORDER BY symbol"

			rows, err := db.QueryContext(cmd.Context(), q, rowsArgs...)
			if err != nil {
				return err
			}
			defer rows.Close()

			type sym struct {
				Symbol         string  `json:"symbol"`
				Description    string  `json:"description"`
				Digits         int     `json:"digits"`
				Point          float64 `json:"point"`
				Spread         int     `json:"spread"`
				VolumeMin      float64 `json:"volume_min"`
				VolumeMax      float64 `json:"volume_max"`
				VolumeStep     float64 `json:"volume_step"`
				BaseCurrency   string  `json:"base_currency"`
				ProfitCurrency string  `json:"profit_currency"`
			}
			var out []sym
			for rows.Next() {
				var s sym
				if err := rows.Scan(&s.Symbol, &s.Description, &s.Digits, &s.Point, &s.Spread,
					&s.VolumeMin, &s.VolumeMax, &s.VolumeStep, &s.BaseCurrency, &s.ProfitCurrency); err != nil {
					return err
				}
				out = append(out, s)
			}
			if len(out) == 0 {
				if filter != "" {
					var total int
					_ = db.QueryRowContext(cmd.Context(), "SELECT count(*) FROM symbols").Scan(&total)
					fmt.Fprintf(cmd.ErrOrStderr(), "no symbols match %q (mirror has %d total)\n", filter, total)
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "mirror is empty — run `pp-mt5 sync symbols` first.")
				}
			}
			return emit(cmd, out, func(w io.Writer, v any) {
				items := v.([]sym)
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "SYMBOL\tDESCRIPTION\tDIGITS\tSPREAD\tVMIN\tVMAX\tBASE/QUOTE")
				fmt.Fprintln(tw, "──────\t───────────\t──────\t──────\t────\t────\t──────────")
				for _, s := range items {
					fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%g\t%g\t%s/%s\n",
						s.Symbol, truncate(s.Description, 40), s.Digits, s.Spread,
						s.VolumeMin, s.VolumeMax, s.BaseCurrency, s.ProfitCurrency)
				}
				tw.Flush()
				fmt.Fprintf(w, "\n(%d symbol%s)\n", len(items), pluralS(len(items)))
			})
		},
	}
	list.Flags().StringVar(&filter, "filter", "", "Glob pattern, e.g. EUR* or *USD")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "info <SYMBOL>",
		Short: "Full live symbol_info() dump",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			info, err := b.SymbolInfo(args[0])
			if err != nil {
				return mapBridgeErr(err)
			}
			return emit(cmd, info, printSymbolInfo)
		},
	})

	return cmd
}

func printSymbolInfo(w io.Writer, v any) {
	s := v.(*bridge.SymbolInfoFull)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"name", s.Name},
		{"description", s.Description},
		{"base / profit", s.Currency + " / " + s.CurrencyProfit},
		{"digits", fmt.Sprintf("%d", s.Digits)},
		{"point", fmt.Sprintf("%g", s.Point)},
		{"bid", fmt.Sprintf("%.*f", s.Digits, s.Bid)},
		{"ask", fmt.Sprintf("%.*f", s.Digits, s.Ask)},
		{"spread (pts)", fmt.Sprintf("%d (float=%v)", s.Spread, s.SpreadFloat)},
		{"contract size", fmt.Sprintf("%g", s.TradeContractSize)},
		{"tick size", fmt.Sprintf("%g", s.TradeTickSize)},
		{"tick value", fmt.Sprintf("%g", s.TradeTickValue)},
		{"volume min/step/max", fmt.Sprintf("%g / %g / %g", s.VolumeMin, s.VolumeStep, s.VolumeMax)},
		{"trade mode", fmt.Sprintf("%d", s.TradeMode)},
		{"selected", fmt.Sprintf("%v (visible=%v)", s.Select, s.Visible)},
		{"last tick", time.Unix(s.Time, 0).Format(time.RFC3339)},
	}
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", r[0], r[1])
	}
	tw.Flush()
}

// ── quote ────────────────────────────────────────────────────────────────────

type quoteOut struct {
	Symbol    string  `json:"symbol"`
	Bid       float64 `json:"bid"`
	Ask       float64 `json:"ask"`
	Last      float64 `json:"last"`
	SpreadPts int     `json:"spread_pts"`
	Time      int64   `json:"time"`
	Digits    int     `json:"digits"`
}

func newQuoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quote <SYMBOL>",
		Short: "Last tick: bid / ask / last / spread / time",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			info, err := b.SymbolInfo(args[0])
			if err != nil {
				return mapBridgeErr(err)
			}
			tick, err := b.SymbolInfoTick(args[0])
			if err != nil {
				return mapBridgeErr(err)
			}
			if tick.Time == 0 && info.Bid > 0 {
				// Some brokers return symbol_info_tick zero on a fresh select;
				// the symbol_info fields are populated synchronously. Fall back.
				tick.Bid, tick.Ask, tick.Last, tick.Time = info.Bid, info.Ask, info.Last, info.Time
			}
			out := &quoteOut{
				Symbol: args[0], Bid: tick.Bid, Ask: tick.Ask, Last: tick.Last,
				SpreadPts: int((tick.Ask - tick.Bid) / info.Point),
				Time:      tick.Time, Digits: info.Digits,
			}
			return emit(cmd, out, func(w io.Writer, v any) {
				q := v.(*quoteOut)
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintf(tw, "symbol\t%s\n", q.Symbol)
				fmt.Fprintf(tw, "bid\t%.*f\n", q.Digits, q.Bid)
				fmt.Fprintf(tw, "ask\t%.*f\n", q.Digits, q.Ask)
				fmt.Fprintf(tw, "last\t%.*f\n", q.Digits, q.Last)
				fmt.Fprintf(tw, "spread\t%d pts\n", q.SpreadPts)
				if q.Time > 0 {
					fmt.Fprintf(tw, "time\t%s\n", time.Unix(q.Time, 0).Format(time.RFC3339))
				} else {
					fmt.Fprintf(tw, "time\t(no recent tick — symbol may not be tradeable right now)\n")
				}
				tw.Flush()
			})
		},
	}
}

// ── book (DOM) ───────────────────────────────────────────────────────────────

func newBookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "book <SYMBOL>",
		Short: "Market Depth snapshot (requires broker DOM support)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			items, err := b.MarketBookGet(args[0])
			if err != nil {
				return mapBridgeErr(err)
			}
			if len(items) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "no depth returned — broker may not provide DOM for this symbol")
			}
			return emit(cmd, items, func(w io.Writer, v any) {
				rows := v.([]bridge.BookItem)
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "SIDE\tPRICE\tVOLUME")
				fmt.Fprintln(tw, "────\t─────\t──────")
				for _, r := range rows {
					fmt.Fprintf(tw, "%s\t%g\t%g\n", bookSideName(r.Type), r.Price, r.Volume)
				}
				tw.Flush()
			})
		},
	}
}

// ── positions list ───────────────────────────────────────────────────────────

func newPositionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "positions", Short: "Open positions"}
	var (
		filter string
		symbol string
	)
	list := &cobra.Command{
		Use:   "list",
		Short: "Print all open positions (live snapshot)",
		Long: `Live snapshot of currently open positions (no mirror read).

--symbol restricts to one symbol at the bridge call boundary.
--filter is a glob pattern matched in-process against symbol and comment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			req := map[string]any{}
			if symbol != "" {
				req["symbol"] = symbol
			}
			ps, err := b.PositionsGet(req)
			if err != nil {
				return mapBridgeErr(err)
			}
			ps = filterPositions(ps, filter)
			return emit(cmd, ps, printPositions)
		},
	}
	list.Flags().StringVar(&symbol, "symbol", "", "Restrict to one symbol")
	list.Flags().StringVar(&filter, "filter", "", "Glob match on symbol or comment (e.g. EUR*)")
	cmd.AddCommand(list)
	return cmd
}

func printPositions(w io.Writer, v any) {
	rows := v.([]bridge.Position)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TICKET\tSYMBOL\tTYPE\tVOLUME\tPRICE_OPEN\tPRICE_NOW\tSL\tTP\tPROFIT\tSWAP\tMAGIC\tOPENED")
	fmt.Fprintln(tw, "──────\t──────\t────\t──────\t──────────\t─────────\t──\t──\t──────\t────\t─────\t──────")
	for _, p := range rows {
		typ := "buy"
		if p.Type == 1 {
			typ = "sell"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%g\t%g\t%g\t%g\t%g\t%+.2f\t%+.2f\t%d\t%s\n",
			p.Ticket, p.Symbol, typ, p.Volume, p.PriceOpen, p.PriceCurrent,
			p.SL, p.TP, p.Profit, p.Swap, p.Magic, time.Unix(p.Time, 0).Format(time.RFC3339))
	}
	tw.Flush()
	fmt.Fprintf(w, "\n(%d position%s)\n", len(rows), pluralS(len(rows)))
}

func filterPositions(ps []bridge.Position, glob string) []bridge.Position {
	if glob == "" {
		return ps
	}
	out := make([]bridge.Position, 0, len(ps))
	for _, p := range ps {
		if globMatch(glob, p.Symbol) || globMatch(glob, p.Comment) {
			out = append(out, p)
		}
	}
	return out
}

// ── orders list ──────────────────────────────────────────────────────────────

func newOrdersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "orders", Short: "Active (pending) orders"}
	var symbol string
	list := &cobra.Command{
		Use:   "list",
		Short: "Print all active orders (live snapshot)",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			req := map[string]any{}
			if symbol != "" {
				req["symbol"] = symbol
			}
			os, err := b.OrdersGet(req)
			if err != nil {
				return mapBridgeErr(err)
			}
			return emit(cmd, os, printOrders)
		},
	}
	list.Flags().StringVar(&symbol, "symbol", "", "Restrict to one symbol")
	cmd.AddCommand(list)
	return cmd
}

func printOrders(w io.Writer, v any) {
	rows := v.([]bridge.Order)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TICKET\tSYMBOL\tTYPE\tVOLUME\tPRICE_OPEN\tSL\tTP\tSTATE\tSETUP")
	fmt.Fprintln(tw, "──────\t──────\t────\t──────\t──────────\t──\t──\t─────\t─────")
	for _, o := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%g\t%g\t%g\t%g\t%s\t%s\n",
			o.Ticket, o.Symbol, orderTypeNameInt(o.Type), o.VolumeCurrent, o.PriceOpen,
			o.SL, o.TP, orderStateNameInt(o.State), time.Unix(o.TimeSetup, 0).Format(time.RFC3339))
	}
	tw.Flush()
	fmt.Fprintf(w, "\n(%d order%s)\n", len(rows), pluralS(len(rows)))
}

// ── order check (preview, read-only) + order send (Phase 7) ─────────────────

func newOrderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "order", Short: "Place/preview a new order"}
	cmd.AddCommand(newOrderCheckCmd())
	cmd.AddCommand(newOrderSendCmd())
	return cmd
}

func newOrderCheckCmd() *cobra.Command {
	var (
		symbol, side string
		volume       float64
		price        float64
		sl, tp       float64
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Preview margin + validity (mt5.order_check) — never sends",
		RunE: func(cmd *cobra.Command, args []string) error {
			if symbol == "" || side == "" || volume == 0 {
				return &ExitErr{Code: ExitUsage, Err: fmt.Errorf("--symbol, --side, --volume are required")}
			}
			action, err := parseOrderSide(side)
			if err != nil {
				return &ExitErr{Code: ExitUsage, Err: err}
			}
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 10 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			// order_check requires the full request struct, not the helper calls.
			// Build it and call via the low-level Call so we don't have to add
			// another typed wrapper here.
			tick, _ := b.SymbolInfoTick(symbol)
			if price == 0 && tick != nil {
				if action == 0 {
					price = tick.Ask
				} else {
					price = tick.Bid
				}
			}
			req := map[string]any{
				"action": 1, // TRADE_ACTION_DEAL (market). Pending types Phase 7.
				"symbol": symbol, "volume": volume, "type": action,
				"price": price, "deviation": 20,
			}
			if sl > 0 {
				req["sl"] = sl
			}
			if tp > 0 {
				req["tp"] = tp
			}
			var res map[string]any
			if err := b.Call("order_check", map[string]any{"request": req}, &res); err != nil {
				return mapBridgeErr(err)
			}
			return emit(cmd, res, func(w io.Writer, v any) {
				m := v.(map[string]any)
				keys := make([]string, 0, len(m))
				for k := range m {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				for _, k := range keys {
					fmt.Fprintf(tw, "%s\t%v\n", k, m[k])
				}
				tw.Flush()
			})
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "Symbol (required)")
	cmd.Flags().StringVar(&side, "side", "", "buy | sell (required)")
	cmd.Flags().Float64Var(&volume, "volume", 0, "Lot size (required)")
	cmd.Flags().Float64Var(&price, "price", 0, "Override execution price (default: current ask/bid)")
	cmd.Flags().Float64Var(&sl, "sl", 0, "Stop loss")
	cmd.Flags().Float64Var(&tp, "tp", 0, "Take profit")
	return cmd
}

func newOrderSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Place a market or pending order (DRY-RUN by default; requires safety hash)",
		Long: `Place an order on the connected broker.

Safety flow:
  1. First run prints a SHA-256 hash of the canonical request and exits with code 6.
  2. To actually send, re-run with --confirm <hash> within 60 seconds.
  3. Live writes additionally require MT5_LIVE=1 AND --i-understand-this-is-live.
  4. Per-command guardrails from ~/.config/mt5-pp-cli/config.toml apply.
  5. Successful send is appended to ~/.local/share/mt5-pp-cli/audit.jsonl.

Implementation lands in Phase 7. For preview today, use 'pp-mt5 order check'.`,
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

// ── position close/modify (writes; Phase 7) ──────────────────────────────────

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

func newCloseAllCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "close", Short: "Bulk operations across positions"}
	all := &cobra.Command{
		Use:   "all",
		Short: "Close every position matching --filter (SQL WHERE clause)",
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
//
// Joins order_calc_margin + order_calc_profit at ±N pips + current account
// state. Strictly a read; never goes through safety.

// riskProjection / riskPreview are package-level so the closure's type
// assertion matches what we build (anonymous local types make assertions fail).
type riskProjection struct {
	PipsOffset  int     `json:"pips"`
	PriceClose  float64 `json:"price_close"`
	Profit      float64 `json:"profit"`
	EquityAfter float64 `json:"equity_after"`
}

type riskPreview struct {
	Symbol          string           `json:"symbol"`
	Side            string           `json:"side"`
	Volume          float64          `json:"volume"`
	Entry           float64          `json:"entry_price"`
	Digits          int              `json:"digits"`
	PipSize         float64          `json:"pip_size"`
	Margin          float64          `json:"margin"`
	Equity          float64          `json:"equity"`
	MarginFreeAfter float64          `json:"margin_free_after"`
	Currency        string           `json:"currency"`
	Projections     []riskProjection `json:"projections"`
}

func newRiskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "risk", Short: "Risk preview tools"}

	var (
		symbol, side string
		volume       float64
		pips         []int
	)
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Project margin + P&L at ±N pips for a hypothetical position",
		RunE: func(cmd *cobra.Command, args []string) error {
			if symbol == "" || volume == 0 {
				return &ExitErr{Code: ExitUsage, Err: fmt.Errorf("--symbol and --volume are required")}
			}
			action, err := parseOrderSide(side)
			if err != nil {
				return &ExitErr{Code: ExitUsage, Err: err}
			}
			b, err := bridge.New(bridge.Options{Stderr: cmd.ErrOrStderr(), CallTimeout: 15 * time.Second})
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Initialize(defaultInit(10000)); err != nil {
				return mapBridgeErr(err)
			}
			info, err := b.SymbolInfo(symbol)
			if err != nil {
				return mapBridgeErr(err)
			}
			acc, err := b.AccountInfo()
			if err != nil {
				return mapBridgeErr(err)
			}
			entry := info.Ask
			if action == 1 {
				entry = info.Bid
			}
			margin, err := b.OrderCalcMargin(action, symbol, volume, entry)
			if err != nil {
				return mapBridgeErr(err)
			}

			pipSize := pointPipSize(info.Digits, info.Point)

			projs := make([]riskProjection, 0, len(pips))
			direction := 1.0
			if action == 1 {
				direction = -1.0
			}
			for _, p := range pips {
				priceClose := entry + direction*float64(p)*pipSize
				profit, err := b.OrderCalcProfit(action, symbol, volume, entry, priceClose)
				if err != nil {
					return mapBridgeErr(err)
				}
				projs = append(projs, riskProjection{
					PipsOffset:  p,
					PriceClose:  priceClose,
					Profit:      profit,
					EquityAfter: acc.Equity + profit,
				})
			}
			out := &riskPreview{
				Symbol: symbol, Side: side, Volume: volume,
				Entry: entry, Digits: info.Digits, PipSize: pipSize,
				Margin: margin, Equity: acc.Equity,
				MarginFreeAfter: acc.MarginFree - margin,
				Currency:        acc.Currency, Projections: projs,
			}
			return emit(cmd, out, printRiskPreview)
		},
	}
	preview.Flags().StringVar(&symbol, "symbol", "", "Symbol (required)")
	preview.Flags().StringVar(&side, "side", "buy", "buy | sell")
	preview.Flags().Float64Var(&volume, "volume", 0, "Lot size (required)")
	preview.Flags().IntSliceVar(&pips, "pips", []int{-100, -50, -25, 25, 50, 100}, "Pip offsets to project P&L at")
	cmd.AddCommand(preview)
	return cmd
}

func printRiskPreview(w io.Writer, v any) {
	r := v.(*riskPreview)
	fmt.Fprintf(w, "%s %s %g lots @ %.*f  pip=%g %s\n",
		r.Symbol, r.Side, r.Volume, r.Digits, r.Entry, r.PipSize, r.Currency)
	fmt.Fprintf(w, "margin:  %.2f %s   (current equity %.2f → margin_free after entry %.2f)\n\n",
		r.Margin, r.Currency, r.Equity, r.MarginFreeAfter)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PIPS\tPRICE\tP&L\tEQUITY_AFTER")
	fmt.Fprintln(tw, "────\t─────\t───\t────────────")
	for _, p := range r.Projections {
		fmt.Fprintf(tw, "%+d\t%.*f\t%+.2f %s\t%.2f %s\n",
			p.PipsOffset, r.Digits, p.PriceClose, p.Profit, r.Currency, p.EquityAfter, r.Currency)
	}
	tw.Flush()
}

// ── small helpers ────────────────────────────────────────────────────────────

// parseOrderSide returns the MT5 ORDER_TYPE int. Phase 3 only needs buy/sell.
func parseOrderSide(side string) (int, error) {
	switch strings.ToLower(side) {
	case "", "buy":
		return 0, nil
	case "sell":
		return 1, nil
	case "buy_limit":
		return 2, nil
	case "sell_limit":
		return 3, nil
	case "buy_stop":
		return 4, nil
	case "sell_stop":
		return 5, nil
	}
	return -1, fmt.Errorf("unknown side %q (want buy/sell/buy_limit/sell_limit/buy_stop/sell_stop)", side)
}

func orderTypeNameInt(t int) string {
	switch t {
	case 0:
		return "buy"
	case 1:
		return "sell"
	case 2:
		return "buy_limit"
	case 3:
		return "sell_limit"
	case 4:
		return "buy_stop"
	case 5:
		return "sell_stop"
	}
	return fmt.Sprintf("type_%d", t)
}

func orderStateNameInt(s int) string {
	switch s {
	case 0:
		return "started"
	case 1:
		return "placed"
	case 2:
		return "canceled"
	case 3:
		return "partial"
	case 4:
		return "filled"
	case 5:
		return "rejected"
	case 6:
		return "expired"
	}
	return fmt.Sprintf("state_%d", s)
}

func bookSideName(t int) string {
	switch t {
	case 1, 3, 5, 7:
		return "sell"
	case 2, 4, 6, 8:
		return "buy"
	}
	return fmt.Sprintf("type_%d", t)
}

// pointPipSize: for 3/5-digit FX (broker fractional pip) the pip is 10×point.
// For 2/4-digit, the pip == point.
func pointPipSize(digits int, point float64) float64 {
	if digits == 3 || digits == 5 {
		return point * 10
	}
	return point
}

// globToLike converts a fnmatch-style glob to a SQL LIKE pattern.
func globToLike(g string) string {
	r := strings.NewReplacer("*", "%", "?", "_")
	return r.Replace(g)
}

// globMatch evaluates a glob against a string using path.Match semantics.
func globMatch(pattern, s string) bool {
	ok, _ := path.Match(pattern, s)
	return ok
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}

