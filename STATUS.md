# pp-mt5 — Build Status

The CLI is being built in 11 phases per the design spec. This file tracks where we are so any session can pick up without re-deriving context. Update when crossing a phase boundary.

## Legend

- ✅ done and tested
- 🟡 partial — works but not feature-complete
- ⬜ not started (stubs return `not implemented` pointing at this file)

## Phases

| # | Phase                                       | Status | Notes |
|---|---------------------------------------------|--------|-------|
| 0 | Scaffold (go.mod, command tree, README, schema, manifest, skill, MCP stub, helper TODO) | ✅ | Builds + `go vet` clean. Safety hash/window/kill-switch unit-tested. |
| 1 | Python bridge real implementation; `doctor`, `connect login`, `account info`, `terminal info`, `connect status`, `connect logout` | ✅ | Go-side `bridge.Bridge` spawns Python via embedded `mt5_bridge.py` (`go:embed`), line-delimited JSON-RPC, per-call timeout, sentinel-error mapping. Typed wrappers for `Initialize/Login/Shutdown/AccountInfo/TerminalInfo/Version`. `doctor` runs 7 checks, each with structured remediation. `account info`/`terminal info`/`connect status` print human tables or JSON depending on `--json/--agent/--human-friendly`/TTY detection. End-to-end smoke tested against a JustMarkets demo account. |
| 2 | SQLite store: open, run migrations, `sync all`, `sync symbols/positions/orders/deals/history-orders/bars/ticks`, `sql` | ✅ | Migrations runner with `go:embed` of `internal/store/migrations/*.sql`. Bridge wrappers for `SymbolsGet/PositionsGet/OrdersGet/HistoryDealsGet/HistoryOrdersGet/CopyRatesRange/CopyTicksRange`. Sync orchestration with upsert semantics. Bridge auto-`symbol_select` before bars/ticks fetches. `pp-mt5 sql` is read-only by default; `--write` opt-in. End-to-end verified: 301 symbols, 120 H1 bars (EURUSD.s), 3969 ticks pulled and queryable. |
| 3 | Read commands: `symbols list/info`, `quote`, `book`, `positions list`, `orders list`, `order check`, `history deals/orders`, `stats summary`, `risk preview` | ✅ | Bridge wrappers added: `SymbolInfo`, `SymbolInfoTick`, `MarketBookGet`, `OrderCalcMargin/Profit`. Python bridge now auto-`symbol_select`s on info/tick reads (parity with bars/ticks). `symbols list` reads mirror; `info`/`quote`/`book` are live. `positions list`/`orders list` are live snapshots with optional `--symbol` and `--filter` (in-process glob). `history deals/orders` read from mirror with `--from/--to/--symbol/--magic`. `stats summary` computes per-position P&L from deals: win rate, gross/net profit, profit factor, expectancy, max DD ($/%), Sharpe & Sortino on daily-P&L vector. `order check` works (read-only preview). `risk preview` projects margin + P&L at ±pips. Verified against JustMarkets demo: 301 symbols mirrored, live EURUSD.s spread 6pts, risk preview at 0.10 lots shows 384 ZAR margin + ±100 pip P&L of ±1654.82 ZAR. |
| 4 | Algo stats: by-symbol/hour/dow/magic, streaks, drawdown, r-multiples, correlation, magic audit | ✅ | All eight commands compute from the mirror. Grouped stats share a single `computeStatsBy(groupExpr)` CTE that builds per-position P&L then aggregates. Streaks detects runs, top-N each side, current-open streak, and post-streak reversal rate. Drawdown walks the realized equity curve and lists every peak→trough→recovery period with depths/durations and an open-drawdown carry-forward. R-multiples joins deals → history_orders for SL-based risk, falls back to `--risk-per-trade × --balance` when SL is missing. Correlation pulls closes from `bars_<TF>`, log-returns, Pearson matrix on intersected timestamps (EURUSD.s/GBPUSD.s H1 7d = +0.77 on the demo). Magic audit groups by EA magic, flags dead (>N days idle) and runaway (bottom-5 7-day P&L). Verified against seeded data: 6 trades, win rate 66.7%, max DD 41.00 (78.8%), Sharpe 5.28. |
| 5 | Safety layer: live-mode gate, hash-confirm, guardrails, audit log | 🟡 | Live-mode gate + hash + window + kill-switch all implemented and unit-tested in `internal/safety`. Audit log + remaining guardrails (`max_volume_per_order`, `max_open_positions`, `max_daily_loss`) are Phase 6. |
| 6 | Config loading from `~/.config/mt5-pp-cli/config.toml`; guardrail wiring; audit log writer | ⬜ | go-toml/v2 in go.mod; needs `internal/config` package + safety.CheckGuardrails completion + store.AuditPath plumbed. |
| 7 | Write commands: `order send`, `position close`, `position modify`, `close all` | ⬜ | All commands wired with safety preflight + flags; handlers return notImpl. |
| 8 | Hero command flow: `close all --filter "..."` end-to-end + README walkthrough | ⬜ | Surface exists; once Phase 7 lands wire the SQL→tickets→bulk-close path. |
| 9 | Quant commands: bars/ticks copy, features build, calendar, replay, backtest | ⬜ | All wired with flags; need parquet writer (Apache Arrow Go) and event-loop replay/backtest. |
| 10 | `mt5-pp-mcp` MCP server: mirror command tree as MCP tools | ⬜ | Binary exists as a stub that prints "not yet implemented". Need `internal/mcp` + mark3labs/mcp-go integration. |
| 11 | Integration tests against MT5 demo account (env-gated; `MT5_PAPER=1`); release polish; final README | ⬜ | Needs operator-provided `MT5_ACCOUNT`, `MT5_SERVER`, `MT5_PASSWORD` (demo). |

## Picking up next session

The next agent should:

1. `cd library/trading/mt5 && go build ./... && go test ./...` — confirm the scaffold still builds.
2. Read this file + spec in the original task message.
3. Pick the next ⬜ phase. Implementation order should respect dependencies — Phase 1 (bridge) blocks 3, 4, 7; Phase 2 (store) blocks 3, 8, 9; Phase 5/6 (safety/config) block 7.
4. When a phase moves from ⬜ → 🟡 or 🟡 → ✅, update this file in the same commit.

## What's intentionally NOT in the scaffold
- Audit log writes (Phase 6).
- Any actual order/position/close write path (Phase 7).
- MCP tools (Phase 10).
- Integration tests against a real MT5 demo (Phase 11; operator credentials needed first).

Anything else is a bug — open an issue or fix in place.
