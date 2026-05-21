package bridge

// Typed wrappers for methods used by `pp-mt5 sync`. Loose maps in many places
// because MT5 keeps adding fields broker-by-broker — strict structs would
// reject unknown keys; the SQL schema only stores what it cares about.

// SymbolInfo mirrors mt5.symbol_info()._asdict() — only the fields the store
// schema persists; MT5 returns many more.
type SymbolInfo struct {
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Digits         int     `json:"digits"`
	Point          float64 `json:"point"`
	Spread         int     `json:"spread"`
	TradeContractSize float64 `json:"trade_contract_size"`
	VolumeMin      float64 `json:"volume_min"`
	VolumeMax      float64 `json:"volume_max"`
	VolumeStep     float64 `json:"volume_step"`
	TradeMode      int     `json:"trade_mode"`
	TradeCalcMode  int     `json:"trade_calc_mode"`
	CurrencyBase   string  `json:"currency_base"`
	CurrencyProfit string  `json:"currency_profit"`
	MarginInitial  float64 `json:"margin_initial"`
}

// Position mirrors mt5.positions_get() row.
type Position struct {
	Ticket       int64   `json:"ticket"`
	Time         int64   `json:"time"`       // unix seconds
	TimeUpdate   int64   `json:"time_update"`
	Type         int     `json:"type"`        // 0=buy, 1=sell
	Magic        int64   `json:"magic"`
	Identifier   int64   `json:"identifier"`
	Reason       int     `json:"reason"`
	Volume       float64 `json:"volume"`
	PriceOpen    float64 `json:"price_open"`
	SL           float64 `json:"sl"`
	TP           float64 `json:"tp"`
	PriceCurrent float64 `json:"price_current"`
	Swap         float64 `json:"swap"`
	Profit       float64 `json:"profit"`
	Symbol       string  `json:"symbol"`
	Comment      string  `json:"comment"`
}

// Order mirrors mt5.orders_get() row (active orders).
type Order struct {
	Ticket         int64   `json:"ticket"`
	TimeSetup      int64   `json:"time_setup"`
	TimeSetupMSC   int64   `json:"time_setup_msc"`
	TimeExpiration int64   `json:"time_expiration"`
	Type           int     `json:"type"`
	TypeTime       int     `json:"type_time"`
	TypeFilling    int     `json:"type_filling"`
	State          int     `json:"state"`
	Magic          int64   `json:"magic"`
	PositionID     int64   `json:"position_id"`
	VolumeInitial  float64 `json:"volume_initial"`
	VolumeCurrent  float64 `json:"volume_current"`
	PriceOpen      float64 `json:"price_open"`
	SL             float64 `json:"sl"`
	TP             float64 `json:"tp"`
	PriceCurrent   float64 `json:"price_current"`
	Symbol         string  `json:"symbol"`
	Comment        string  `json:"comment"`
}

// Deal mirrors mt5.history_deals_get() row.
type Deal struct {
	Ticket     int64   `json:"ticket"`
	Order      int64   `json:"order"`
	Time       int64   `json:"time"`
	TimeMSC    int64   `json:"time_msc"`
	Type       int     `json:"type"`
	Entry      int     `json:"entry"`    // 0=in 1=out 2=inout 3=out_by
	Magic      int64   `json:"magic"`
	PositionID int64   `json:"position_id"`
	Volume     float64 `json:"volume"`
	Price      float64 `json:"price"`
	Commission float64 `json:"commission"`
	Swap       float64 `json:"swap"`
	Profit     float64 `json:"profit"`
	Fee        float64 `json:"fee"`
	Symbol     string  `json:"symbol"`
	Comment    string  `json:"comment"`
}

// HistoryOrder mirrors mt5.history_orders_get() row.
type HistoryOrder struct {
	Ticket        int64   `json:"ticket"`
	TimeSetup     int64   `json:"time_setup"`
	TimeDone      int64   `json:"time_done"`
	Type          int     `json:"type"`
	State         int     `json:"state"`
	Magic         int64   `json:"magic"`
	PositionID    int64   `json:"position_id"`
	VolumeInitial float64 `json:"volume_initial"`
	VolumeCurrent float64 `json:"volume_current"`
	PriceOpen     float64 `json:"price_open"`
	SL            float64 `json:"sl"`
	TP            float64 `json:"tp"`
	Symbol        string  `json:"symbol"`
	Comment       string  `json:"comment"`
}

// Bar mirrors one row of mt5.copy_rates_range(). 'time' is unix seconds.
type Bar struct {
	Time        int64   `json:"time"`
	Open        float64 `json:"open"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Close       float64 `json:"close"`
	TickVolume  int64   `json:"tick_volume"`
	Spread      int     `json:"spread"`
	RealVolume  int64   `json:"real_volume"`
}

// Tick mirrors one row of mt5.copy_ticks_range(). 'time' is unix seconds;
// 'time_msc' is milliseconds.
type Tick struct {
	Time       int64   `json:"time"`
	TimeMSC    int64   `json:"time_msc"`
	Bid        float64 `json:"bid"`
	Ask        float64 `json:"ask"`
	Last       float64 `json:"last"`
	Volume     float64 `json:"volume"`
	Flags      int     `json:"flags"`
	VolumeReal float64 `json:"volume_real"`
}

// ── high-level wrappers ──────────────────────────────────────────────────────

// SymbolsGet returns the visible symbol catalog. Optional `group` filter
// (e.g. "EUR*") is passed straight through.
func (b *Bridge) SymbolsGet(group string) ([]SymbolInfo, error) {
	params := map[string]any{}
	if group != "" {
		params["group"] = group
	}
	var out []SymbolInfo
	if err := b.Call("symbols_get", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PositionsGet returns currently open positions, optionally filtered.
func (b *Bridge) PositionsGet(filter map[string]any) ([]Position, error) {
	if filter == nil {
		filter = map[string]any{}
	}
	var out []Position
	if err := b.Call("positions_get", filter, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// OrdersGet returns active (pending) orders.
func (b *Bridge) OrdersGet(filter map[string]any) ([]Order, error) {
	if filter == nil {
		filter = map[string]any{}
	}
	var out []Order
	if err := b.Call("orders_get", filter, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// HistoryDealsGet returns historical deals between from/to (unix seconds).
func (b *Bridge) HistoryDealsGet(fromUnix, toUnix int64) ([]Deal, error) {
	var out []Deal
	if err := b.Call("history_deals_get", map[string]any{
		"date_from": fromUnix, "date_to": toUnix,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// HistoryOrdersGet returns historical orders between from/to (unix seconds).
func (b *Bridge) HistoryOrdersGet(fromUnix, toUnix int64) ([]HistoryOrder, error) {
	var out []HistoryOrder
	if err := b.Call("history_orders_get", map[string]any{
		"date_from": fromUnix, "date_to": toUnix,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CopyRatesRange returns bars for [from,to] at the given timeframe ("M5", "H1"...).
func (b *Bridge) CopyRatesRange(symbol, timeframe string, fromUnix, toUnix int64) ([]Bar, error) {
	var out []Bar
	if err := b.Call("copy_rates_range", map[string]any{
		"symbol": symbol, "timeframe": timeframe,
		"date_from": fromUnix, "date_to": toUnix,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CopyTicksRange returns ticks for [from,to]. flags = "all" | "info" | "trade".
func (b *Bridge) CopyTicksRange(symbol string, fromUnix, toUnix int64, flags string) ([]Tick, error) {
	if flags == "" {
		flags = "all"
	}
	var out []Tick
	if err := b.Call("copy_ticks_range", map[string]any{
		"symbol": symbol,
		"date_from": fromUnix, "date_to": toUnix,
		"flags": flags,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
