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
| 2 | SQLite store: open, run migrations, `sync all`, `sql` | ⬜ | `migrations/0001_init.sql` is complete; store.Open() is implemented; needs migrations runner and sync orchestration. |
| 3 | Read commands: symbols, quote, book, positions list, orders list, history, stats summary, risk preview | ⬜ | All commands wired with flags; handlers return notImpl. |
| 4 | Algo stats: by-symbol/hour/dow/magic, streaks, drawdown, r-multiples, correlation, magic audit | ⬜ | Same — wired, stubbed. |
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

- Real database connections in handlers (Phase 2).
- Audit log writes (Phase 6).
- Any actual order/position/close write path (Phase 7).
- MCP tools (Phase 10).
- Integration tests against a real MT5 demo (Phase 11; operator credentials needed first).

Anything else is a bug — open an issue or fix in place.
