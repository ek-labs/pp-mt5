"""mt5_bridge.py — Python subprocess that fronts the MetaTrader5 package.

Protocol: line-delimited JSON-RPC over stdin/stdout.

  Request  → {"id": 1, "method": "account_info", "params": {}}
  Response ← {"id": 1, "result": {...}}
           ← {"id": 1, "error": {"code": "STR", "message": "..."}}

Error codes (mirror Go's bridge.Err* sentinels):
  NOT_LOGGED_IN          mt5.account_info() returned None
  TERMINAL_DOWN          mt5.initialize() returned False
  BROKER_REJECTED        retcode != TRADE_RETCODE_DONE
  RATE_LIMITED           detected via consecutive timeouts
  PYTHON_MT5_MISSING     `import MetaTrader5` failed at startup
  INVALID_PARAMS         malformed params dict
  INTERNAL               any uncaught exception (also logged to stderr)

Functions covered (full MetaTrader5 API surface — Phase 1 fills in the bodies):
  initialize, login, shutdown, last_error, version,
  account_info, terminal_info,
  symbols_total, symbols_get, symbol_info, symbol_info_tick, symbol_select,
  market_book_add, market_book_get, market_book_release,
  copy_rates_from, copy_rates_from_pos, copy_rates_range,
  copy_ticks_from, copy_ticks_range,
  orders_total, orders_get,
  order_calc_margin, order_calc_profit, order_check, order_send,
  positions_total, positions_get,
  history_orders_total, history_orders_get,
  history_deals_total, history_deals_get.

Run standalone for a quick smoke test:
  py -3 mt5_bridge.py --self-test
"""

from __future__ import annotations

import json
import sys
import traceback
from typing import Any, Callable, Dict

# Lazy import so --self-test works without MetaTrader5 installed for syntax checks.
mt5 = None  # type: ignore


def _load_mt5() -> str | None:
    """Return None on success, an error-code string on failure."""
    global mt5
    try:
        import MetaTrader5 as _mt5  # type: ignore
        mt5 = _mt5
        return None
    except ImportError:
        return "PYTHON_MT5_MISSING"


# ── handlers ────────────────────────────────────────────────────────────────
# Each handler takes a params dict, returns a JSON-serializable result, and
# either raises BridgeError on a known failure or lets the dispatcher turn
# unknown exceptions into INTERNAL.

class BridgeError(Exception):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


def _to_dict(obj: Any) -> Any:
    """Convert a MetaTrader5 named-tuple result into a plain dict."""
    if obj is None:
        return None
    if hasattr(obj, "_asdict"):
        return obj._asdict()
    if isinstance(obj, (list, tuple)):
        return [_to_dict(x) for x in obj]
    return obj


def h_initialize(params: dict) -> Any:
    ok = mt5.initialize(**params)
    if not ok:
        raise BridgeError("TERMINAL_DOWN", "mt5.initialize() returned False — terminal not running?")
    return {"ok": True}


def h_login(params: dict) -> Any:
    ok = mt5.login(**params)
    if not ok:
        raise BridgeError("NOT_LOGGED_IN", "mt5.login() failed — check account/server/password")
    return {"ok": True}


def h_shutdown(params: dict) -> Any:
    mt5.shutdown()
    return {"ok": True}


def h_last_error(params: dict) -> Any:
    return list(mt5.last_error())


def h_version(params: dict) -> Any:
    return list(mt5.version())


def h_account_info(params: dict) -> Any:
    info = mt5.account_info()
    if info is None:
        raise BridgeError("NOT_LOGGED_IN", "account_info() is None")
    return _to_dict(info)


def h_terminal_info(params: dict) -> Any:
    info = mt5.terminal_info()
    if info is None:
        raise BridgeError("TERMINAL_DOWN", "terminal_info() is None")
    return _to_dict(info)


# Symbols
def h_symbols_total(params: dict) -> Any: return mt5.symbols_total()
def h_symbols_get(params: dict) -> Any: return _to_dict(mt5.symbols_get(**params))
def h_symbol_info(params: dict) -> Any: return _to_dict(mt5.symbol_info(params["symbol"]))
def h_symbol_info_tick(params: dict) -> Any: return _to_dict(mt5.symbol_info_tick(params["symbol"]))
def h_symbol_select(params: dict) -> Any: return {"ok": mt5.symbol_select(params["symbol"], params.get("enable", True))}


# Market book
def h_market_book_add(params: dict) -> Any: return {"ok": mt5.market_book_add(params["symbol"])}
def h_market_book_get(params: dict) -> Any: return _to_dict(mt5.market_book_get(params["symbol"]))
def h_market_book_release(params: dict) -> Any: return {"ok": mt5.market_book_release(params["symbol"])}


# Bars + ticks
def h_copy_rates_from(params: dict) -> Any: return _to_dict(mt5.copy_rates_from(**params))
def h_copy_rates_from_pos(params: dict) -> Any: return _to_dict(mt5.copy_rates_from_pos(**params))
def h_copy_rates_range(params: dict) -> Any: return _to_dict(mt5.copy_rates_range(**params))
def h_copy_ticks_from(params: dict) -> Any: return _to_dict(mt5.copy_ticks_from(**params))
def h_copy_ticks_range(params: dict) -> Any: return _to_dict(mt5.copy_ticks_range(**params))


# Orders / positions / history
def h_orders_total(params: dict) -> Any: return mt5.orders_total()
def h_orders_get(params: dict) -> Any: return _to_dict(mt5.orders_get(**params))
def h_order_calc_margin(params: dict) -> Any: return mt5.order_calc_margin(**params)
def h_order_calc_profit(params: dict) -> Any: return mt5.order_calc_profit(**params)
def h_order_check(params: dict) -> Any: return _to_dict(mt5.order_check(params["request"]))
def h_order_send(params: dict) -> Any:
    res = mt5.order_send(params["request"])
    d = _to_dict(res)
    if d and d.get("retcode") not in (mt5.TRADE_RETCODE_DONE, mt5.TRADE_RETCODE_PLACED):
        raise BridgeError("BROKER_REJECTED", f"retcode={d.get('retcode')} comment={d.get('comment')}")
    return d


def h_positions_total(params: dict) -> Any: return mt5.positions_total()
def h_positions_get(params: dict) -> Any: return _to_dict(mt5.positions_get(**params))
def h_history_orders_total(params: dict) -> Any: return mt5.history_orders_total(**params)
def h_history_orders_get(params: dict) -> Any: return _to_dict(mt5.history_orders_get(**params))
def h_history_deals_total(params: dict) -> Any: return mt5.history_deals_total(**params)
def h_history_deals_get(params: dict) -> Any: return _to_dict(mt5.history_deals_get(**params))


HANDLERS: Dict[str, Callable[[dict], Any]] = {
    "initialize": h_initialize, "login": h_login, "shutdown": h_shutdown,
    "last_error": h_last_error, "version": h_version,
    "account_info": h_account_info, "terminal_info": h_terminal_info,
    "symbols_total": h_symbols_total, "symbols_get": h_symbols_get,
    "symbol_info": h_symbol_info, "symbol_info_tick": h_symbol_info_tick,
    "symbol_select": h_symbol_select,
    "market_book_add": h_market_book_add, "market_book_get": h_market_book_get,
    "market_book_release": h_market_book_release,
    "copy_rates_from": h_copy_rates_from, "copy_rates_from_pos": h_copy_rates_from_pos,
    "copy_rates_range": h_copy_rates_range,
    "copy_ticks_from": h_copy_ticks_from, "copy_ticks_range": h_copy_ticks_range,
    "orders_total": h_orders_total, "orders_get": h_orders_get,
    "order_calc_margin": h_order_calc_margin, "order_calc_profit": h_order_calc_profit,
    "order_check": h_order_check, "order_send": h_order_send,
    "positions_total": h_positions_total, "positions_get": h_positions_get,
    "history_orders_total": h_history_orders_total,
    "history_orders_get": h_history_orders_get,
    "history_deals_total": h_history_deals_total,
    "history_deals_get": h_history_deals_get,
}


# ── dispatcher ──────────────────────────────────────────────────────────────


def _serve_one(line: str) -> str:
    """Process one JSON request line; return one JSON response line."""
    try:
        req = json.loads(line)
    except json.JSONDecodeError as e:
        return json.dumps({"id": None, "error": {"code": "INVALID_PARAMS", "message": f"bad json: {e}"}})
    rid = req.get("id")
    method = req.get("method", "")
    params = req.get("params") or {}
    handler = HANDLERS.get(method)
    if handler is None:
        return json.dumps({"id": rid, "error": {"code": "INVALID_PARAMS", "message": f"unknown method {method!r}"}})
    if mt5 is None:
        return json.dumps({"id": rid, "error": {"code": "PYTHON_MT5_MISSING", "message": "MetaTrader5 package not loaded"}})
    try:
        result = handler(params)
        return json.dumps({"id": rid, "result": result}, default=str)
    except BridgeError as e:
        return json.dumps({"id": rid, "error": {"code": e.code, "message": e.message}})
    except Exception as e:  # noqa: BLE001
        traceback.print_exc(file=sys.stderr)
        return json.dumps({"id": rid, "error": {"code": "INTERNAL", "message": str(e)}})


def serve() -> None:
    """Read JSON-RPC requests from stdin until EOF, write responses to stdout."""
    err = _load_mt5()
    if err:
        # Keep the loop alive so the caller can read the error per-request and
        # decide whether to give up. doctor pings this and surfaces the code.
        pass
    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        sys.stdout.write(_serve_one(line) + "\n")
        sys.stdout.flush()


def self_test() -> int:
    """Print loadability diagnostics and exit."""
    err = _load_mt5()
    print(json.dumps({
        "python": sys.version.split()[0],
        "platform": sys.platform,
        "mt5_loaded": err is None,
        "mt5_error": err,
        "handlers": sorted(HANDLERS.keys()),
        "handler_count": len(HANDLERS),
    }, indent=2))
    return 0 if err is None else 1


if __name__ == "__main__":
    if "--self-test" in sys.argv:
        sys.exit(self_test())
    serve()
