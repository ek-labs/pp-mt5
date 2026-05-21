# pp-mt5 · MetaTrader 5 — Printing Press CLI

> One CLI that turns MetaTrader 5 into a forensic record of every decision you ever made in markets — plus a tick-accurate replay engine — and makes both queryable from the shell.

```text
$ pp-mt5 close all losing positions over 50 pips and tighten stops on the rest
```

Behind that one line: fetch positions, filter by pip P&L, close losers, tighten SL on winners — one round trip, one safety hash, one audit row. Every write command goes through the same path.

> ⚠️ **Status: scaffold (Phase 0 complete).** Command surface is wired but most handlers return `not implemented` and point at the phase that delivers them. See [STATUS.md](./STATUS.md) for what works today and what's next.

---

## Limitations (read first)

- **Windows-only host for live MT5.** The `MetaTrader5` Python package only runs on Windows. Mac/Linux users can still run `pp-mt5 sql`, `replay`, `stats`, and `backtest` against a synced mirror — they just can't pull live data or send orders without a Windows MT5 host.
- **Python subprocess overhead.** ~50ms per round trip to the bridge. The local SQLite mirror exists precisely to keep this off the hot path — `sql`, `stats`, `replay`, and `backtest` answer in microseconds, not 50ms.
- **Helper EA required for trade event streaming** (`pp-mt5 watch trades --tail`). Phase 2. v1 workaround: poll `positions list` on an interval. See [helper/TODO.md](./helper/TODO.md).
- **Sibling CLI to `pp-mt5-backtester`.** This tool is for live + algo + quant workflows. For headless `terminal64.exe + .ini` strategy-tester runs, use [`library/finance/mt5-backtester/`](../../finance/mt5-backtester/). Different binary, different transport, no overlap.

---

## Install

```bash
go install github.com/mvanhorn/printing-press-library/library/trading/mt5/cmd/mt5-pp-cli@latest
py -3 -m pip install MetaTrader5      # Windows only
```

Verify:

```bash
pp-mt5 doctor
```

`doctor` checks Python, the MetaTrader5 package, terminal state, login, store writability, and current safety mode — and prints the exact remediation for any failure.

---

## Auth

The CLI never reads or stores passwords. Pass an env var name with `--password-env`:

```powershell
$env:MT5_PASSWORD = "..."
pp-mt5 connect login --account 12345678 --server "Broker-Live" --password-env MT5_PASSWORD
```

For headless / CI use, save a profile to `~/.config/mt5-pp-cli/config.toml`:

```toml
[profiles.demo]
account     = 12345678
server      = "Broker-Demo"
password_env = "MT5_DEMO_PASSWORD"

[profiles.live]
account     = 87654321
server      = "Broker-Live"
password_env = "MT5_LIVE_PASSWORD"
```

Then `pp-mt5 --profile demo account info`.

---

## Quick start

```bash
pp-mt5 doctor
pp-mt5 connect login --account 12345678 --server Broker-Demo --password-env MT5_PASSWORD
pp-mt5 sync all --since 2024-01-01      # one-time mirror; subsequent runs are incremental
pp-mt5 stats summary --since 30d        # 50µs from the local mirror, no bridge call
pp-mt5 sql "select symbol, sum(profit) p from deals group by symbol order by p"
```

---

## Unique features

### Hero: compound writes from a SQL predicate

```bash
$ pp-mt5 close all --filter "profit < 0"

close all candidates (filter: profit < 0)
TICKET      SYMBOL    TYPE  VOLUME  CURRENT P&L
──────      ──────    ────  ──────  ───────────
2329038588  EURUSD.s  buy   0.01    -8.73

realized P&L if closed now: -8.73  (1 position)

hash: 9bf362c74fcee60a30dc6965942a5434304b9559248b5bb690d5148022297b54  (60s window; covers tickets + filter, not live P&L)
to execute:  pp-mt5 close all --filter "profit < 0" --confirm 9bf362c74...

# re-run with --confirm <hash>
$ pp-mt5 close all --filter "profit < 0" --confirm 9bf362c74...

TICKET      RETCODE  COMMENT
──────      ───────  ───────
2329038588  10009    Request executed
```

Behind the scenes: snapshot live positions → SELECT WHERE filter → resolve ticket list → print candidates and projected P&L → SHA-256 of the **intent** (tickets + filter, not live profit) → exit 6 → on `--confirm` execute each close sequentially, audit per ticket. The `--filter` is your safety harness — explicit, version-controllable, survives transcript replay. The intent hash survives a few seconds of price ticks; the broker's deviation absorbs slippage at execution.

### Local SQLite mirror

`~/.local/share/mt5-pp-cli/store.db` (path varies by OS). Tables: `symbols`, `ticks`, `bars_M1`..`bars_MN1`, `orders`, `history_orders`, `positions`, `deals`, `calendar_events`, `features`, `backtests`, `audit`, `schema_migrations`. Query directly:

```bash
pp-mt5 sql "select * from deals where time_ms > strftime('%s','now','-30 days')*1000"
```

### Safety layer (non-negotiable)

Defense in depth — every write goes through:

1. **Live-mode gate.** Both `MT5_LIVE=1` env AND `--i-understand-this-is-live` flag required for any live write. Either missing → exit 6.
2. **Hash-confirm.** First invocation prints `SHA-256` of the canonical request and exits 6. Re-run with `--confirm <hash>` within 60s.
3. **Per-command guardrails** from `~/.config/mt5-pp-cli/config.toml`: `max_volume_per_order`, `max_open_positions`, `max_daily_loss`, `kill_switch_file` (a single touched file refuses all writes).
4. **Audit log** appended to `~/.local/share/mt5-pp-cli/audit.jsonl` — every write, hash, response. Never deleted by the CLI.

### Tick-accurate replay

```bash
pp-mt5 replay --symbol EURUSD --from 2024-06-01 --to 2024-06-02 --speed 100x | your-strategy.py
```

Streams from the local mirror. Works offline. `--granularity tick|bar:M1|bar:M5...`.

### Quant export

```bash
pp-mt5 bars export --tf M1 --symbols "EUR*,XAU*" --since 2y --out parquet
```

Parquet, CSV, or JSONL. Reads the mirror; never touches the bridge.

---

## Command reference

See `pp-mt5 --help`. Categories:

**Foundation**
`doctor`, `connect login|logout|status`, `account info`, `terminal info`, `sync all|symbols|bars|ticks|deals|orders|positions`, `sql`

**Live**
`symbols list|info`, `quote`, `book`, `positions list`, `orders list`, `order check|send`, `position close|modify`, `close all --filter ...`, `risk preview`

**Algo**
`history deals|orders`, `stats summary|by-symbol|by-hour|by-day-of-week|by-magic|streaks|drawdown`, `r-multiples`, `correlation`, `magic audit`

**Quant**
`bars copy|export`, `ticks copy`, `features build`, `calendar sync|near`, `replay`, `backtest run|list`

**Phase 2**
`helper install`, `watch trades`

---

## Output formats

Default: human-friendly table in a TTY; auto-switches to JSON when piped. Force with:

| Flag                | Behaviour                                                            |
|---------------------|----------------------------------------------------------------------|
| `--json`            | JSON regardless of TTY                                               |
| `--agent`           | `--json --compact --no-color --no-input --yes`                       |
| `--select a,b.c`    | Cherry-pick fields from JSON output                                  |
| `--dry-run`         | Preview without executing; for writes implies the safety hash flow   |
| `--human-friendly`  | Force tables even when piped                                         |

Errors go to **stderr**, data to **stdout**.

---

## Exit codes

| Code | Meaning                              |
|------|--------------------------------------|
| 0    | OK                                   |
| 2    | Usage                                |
| 3    | Not found (symbol, ticket, deal, …)  |
| 4    | Auth                                 |
| 5    | Broker rejected                      |
| 6    | Safety-layer rejected                |
| 7    | Rate limited                         |
| 10   | Config                               |
| 11   | MT5 terminal unreachable             |

Agents need 5 vs 6 distinct: `5` is "broker said no" (re-try might help), `6` is "you didn't pass the safety gate" (you must change the command, not retry).

---

## Agent usage

Pass `--agent` to any command. Designed for tool-using LLMs:

```bash
pp-mt5 positions list --agent
pp-mt5 stats summary --since 30d --agent --select win_rate,profit_factor,max_dd_pct
```

For writes, an agent should:

1. Compose the command with `--dry-run`.
2. Capture the printed hash from stdout.
3. Re-invoke with `--confirm <hash> --i-understand-this-is-live` only after surfacing the dry-run summary to the human and getting explicit approval.
4. Never set `MT5_LIVE=1` from inside the agent process — that's a user-only action.

---

## Claude Code integration

A skill ships at [`SKILL.md`](./SKILL.md). After install, `/pp-mt5` is available in Claude Code with full command-tree awareness, the safety semantics, and exit-code meanings baked in.

---

## Claude Desktop MCPB

`mt5-pp-mcp` (Phase 10) exposes the command tree over MCP. Install:

```bash
go install github.com/mvanhorn/printing-press-library/library/trading/mt5/cmd/mt5-pp-mcp@latest
claude mcp add mt5-pp-mcp -- mt5-pp-mcp
```

---

## Troubleshooting

| Symptom                                       | Likely cause + fix                                                           |
|-----------------------------------------------|------------------------------------------------------------------------------|
| `doctor` says Python not found                | Install Python 3.10+; ensure `py -3` works on Windows                        |
| `MetaTrader5 package missing`                 | `py -3 -m pip install MetaTrader5` — Windows only                            |
| `terminal not running`                        | Start MetaTrader 5 and log in once before invoking the CLI                   |
| `exit 4 — auth`                               | Wrong account/server/password; verify in the terminal first                  |
| `exit 6 — safety-layer rejected`              | Missing `MT5_LIVE=1` env, missing `--i-understand-this-is-live`, expired hash, or kill switch file present |
| `exit 11 — terminal unreachable`              | `mt5.initialize()` returned False; restart the terminal                      |
| Hash mismatch                                 | The 60-second window expired or your command flags changed — re-run dry-run |

---

## Sources & inspiration

- [MQL5 Python integration docs](https://www.mql5.com/en/docs/python_metatrader5) — the API surface this CLI wraps
- [printingpress.dev](https://printingpress.dev) — design philosophy
- [`library/media-and-entertainment/youtube/`](../../media-and-entertainment/youtube/) — structural template (read-only reference)
- Peter Steinberger's `discrawl` and `gogcli` — the local-mirror playbook the press is built on
- `library/finance/mt5-backtester/` — sibling CLI for headless Strategy Tester runs
