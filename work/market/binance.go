package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Binance's public market-data mirror needs no key and, unlike api.binance.com,
// answers from mainland China without a proxy. It serves spot markets only.
const (
	binanceREST = "https://data-api.binance.vision"
	binanceWS   = "wss://data-stream.binance.vision"
)

// Binance reads spot markets. A client Timeout bounds REST requests and, for
// streams, only the handshake.
type Binance struct{ HTTP *http.Client }

// Klines returns up to limit (at most 1000) bars opening at or before end, oldest
// first. end is Unix ms, 0 for now. To page back, pass the first bar's T-1.
func (b Binance) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	q := url.Values{"symbol": {symbol}, "interval": {interval}, "limit": {strconv.Itoa(limit)}}
	if end > 0 {
		q.Set("endTime", strconv.FormatInt(end, 10))
	}
	body, err := get(ctx, b.HTTP, binanceREST+"/api/v3/klines?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("binance klines: %w", err)
	}
	return parseKlines(body)
}

// ServerTime is Binance's clock in Unix ms, for lining a bar-close countdown up
// with the exchange instead of this machine's clock.
func (b Binance) ServerTime(ctx context.Context) (int64, error) {
	body, err := get(ctx, b.HTTP, binanceREST+"/api/v3/time")
	if err != nil {
		return 0, fmt.Errorf("binance time: %w", err)
	}
	var v struct {
		ServerTime int64 `json:"serverTime"`
	}
	err = json.Unmarshal(body, &v)
	return v.ServerTime, err
}

// Symbol is what names a market and rounds its prices.
type Symbol struct {
	Name, Base, Quote string
	Tick              float64 // the smallest price step
	Desc              string  // what it is, where the name does not say: "纳指 E-mini", "Apple Inc."
}

// Symbols looks markets up by name. One unknown name fails the whole lookup.
// (Every market at once is 6 MB; this is a few KB each.)
func (b Binance) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	q := url.Values{}
	if len(names) == 1 {
		q.Set("symbol", names[0])
	} else {
		list, _ := json.Marshal(names)
		q.Set("symbols", string(list))
	}
	body, err := get(ctx, b.HTTP, binanceREST+"/api/v3/exchangeInfo?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("binance symbols: %w", err)
	}
	return parseSymbols(body)
}

func parseSymbols(body []byte) ([]Symbol, error) {
	var r struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
			Filters    []struct {
				FilterType string `json:"filterType"`
				TickSize   string `json:"tickSize"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := make([]Symbol, 0, len(r.Symbols))
	for _, s := range r.Symbols {
		sym := Symbol{Name: s.Symbol, Base: s.BaseAsset, Quote: s.QuoteAsset}
		for _, f := range s.Filters {
			if f.FilterType == "PRICE_FILTER" {
				sym.Tick, _ = strconv.ParseFloat(f.TickSize, 64)
			}
		}
		out = append(out, sym)
	}
	return out, nil
}

// Ticker is a market's last 24 hours.
type Ticker struct {
	Symbol      string
	Open, Last  float64
	QuoteVolume float64 // in the quote asset
}

// Change is the move over the 24 hours, as a fraction.
func (t Ticker) Change() float64 {
	if t.Open == 0 {
		return 0
	}
	return t.Last/t.Open - 1
}

// Tickers returns every market traded in the last two days, about 1 MB.
func (b Binance) Tickers(ctx context.Context) ([]Ticker, error) {
	body, err := get(ctx, b.HTTP, binanceREST+"/api/v3/ticker/24hr?type=MINI")
	if err != nil {
		return nil, fmt.Errorf("binance tickers: %w", err)
	}
	return parseTickers(body, time.Now().Add(-48*time.Hour).UnixMilli())
}

// The list keeps delisted markets too, frozen at their last day: those closed
// before since, or without a trade, are dropped.
func parseTickers(body []byte, since int64) ([]Ticker, error) {
	var rows []struct {
		Symbol      string `json:"symbol"`
		Open        string `json:"openPrice"`
		Last        string `json:"lastPrice"`
		QuoteVolume string `json:"quoteVolume"`
		CloseTime   int64  `json:"closeTime"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	out := make([]Ticker, 0, len(rows))
	for _, r := range rows {
		t, err := ticker(r.Symbol, r.Open, r.Last, r.QuoteVolume)
		if err != nil {
			return nil, err
		}
		if r.CloseTime >= since && t.QuoteVolume > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

func ticker(symbol, open, last, quoteVolume string) (Ticker, error) {
	t := Ticker{Symbol: symbol}
	var err error
	for _, f := range []struct {
		s   string
		dst *float64
	}{{open, &t.Open}, {last, &t.Last}, {quoteVolume, &t.QuoteVolume}} {
		if *f.dst, err = strconv.ParseFloat(f.s, 64); err != nil {
			return Ticker{}, fmt.Errorf("ticker %s: %w", symbol, err)
		}
	}
	return t, nil
}

// Each row is [openTime, "open", "high", "low", "close", "volume", closeTime, ...].
func parseKlines(body []byte) ([]Bar, error) {
	var rows [][]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	bars := make([]Bar, 0, len(rows))
	for _, r := range rows {
		if len(r) < 6 {
			return nil, fmt.Errorf("kline row has %d fields", len(r))
		}
		t, ok := r[0].(float64)
		if !ok {
			return nil, fmt.Errorf("kline open time %v", r[0])
		}
		var s [5]string
		for i := range s {
			s[i], _ = r[i+1].(string)
		}
		bar, err := ohlcv(int64(t), s)
		if err != nil {
			return nil, fmt.Errorf("kline %d: %w", int64(t), err)
		}
		bars = append(bars, bar)
	}
	return bars, nil
}

// Stream calls fn for every update of the current bar, about every 2 seconds,
// until ctx ends or the connection drops, which Binance does to every
// connection after 24 hours, so callers reconnect. The same T keeps arriving
// with new values until closed is true. at is Binance's event time in Unix ms.
func (b Binance) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	err := b.stream(ctx, "/ws/"+strings.ToLower(symbol)+"@kline_"+interval, func(data []byte) error {
		bar, closed, at, err := parseKlineEvent(data)
		if err == nil {
			fn(bar, closed, at)
		}
		return err
	})
	return fmt.Errorf("binance stream: %w", err)
}

// Watch calls fn with a market's last 24 hours as it changes, about once a
// second each, until ctx ends or the connection drops.
func (b Binance) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	streams := make([]string, len(symbols))
	for i, s := range symbols {
		streams[i] = strings.ToLower(s) + "@miniTicker"
	}
	err := b.stream(ctx, "/stream?streams="+strings.Join(streams, "/"), func(data []byte) error {
		t, err := parseMiniTicker(data)
		if err == nil {
			fn(t)
		}
		return err
	})
	return fmt.Errorf("binance watch: %w", err)
}

// silence is how long a stream may say nothing before it counts as dead: a
// connection whose network went away can otherwise wait on a read forever.
const silence = 60 * time.Second

func (b Binance) stream(ctx context.Context, path string, fn func([]byte) error) error {
	return streamFrom(ctx, b.HTTP, binanceWS+path, fn)
}

// streamFrom reads the WebSocket at u, handing each message to fn, until ctx
// ends, the connection drops, it says nothing for a while or fn fails.
func streamFrom(ctx context.Context, client *http.Client, u string, fn func([]byte) error) error {
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return err
	}
	defer c.CloseNow()
	for {
		rctx, cancel := context.WithTimeout(ctx, silence)
		_, data, err := c.Read(rctx)
		cancel()
		if err != nil {
			return err
		}
		if err := fn(data); err != nil {
			return err
		}
	}
}

// encoding/json falls back to a case-insensitive match when no key matches
// exactly, and Binance uses pairs like t/T, l/L, v/V, e/E for different things.
// Both halves of each pair are declared so neither lands on the other's field.
type klineEvent struct {
	Type  string `json:"e"`
	Event int64  `json:"E"`
	K     struct {
		Open    int64  `json:"t"`
		CloseAt int64  `json:"T"`
		O       string `json:"o"`
		H       string `json:"h"`
		L       string `json:"l"`
		LastID  int64  `json:"L"`
		C       string `json:"c"`
		V       string `json:"v"`
		TakerV  string `json:"V"`
		Final   bool   `json:"x"`
	} `json:"k"`
}

func parseKlineEvent(data []byte) (Bar, bool, int64, error) {
	var m klineEvent
	if err := json.Unmarshal(data, &m); err != nil {
		return Bar{}, false, 0, err
	}
	if m.Type != "kline" {
		return Bar{}, false, 0, fmt.Errorf("unexpected message %.200s", data)
	}
	k := m.K
	bar, err := ohlcv(k.Open, [5]string{k.O, k.H, k.L, k.C, k.V})
	return bar, k.Final, m.Event, err
}

// A combined stream wraps each event as {"stream": ..., "data": event}.
type miniTickerEvent struct {
	Data struct {
		Type   string `json:"e"`
		Event  int64  `json:"E"`
		Symbol string `json:"s"`
		Close  string `json:"c"`
		Open   string `json:"o"`
		Quote  string `json:"q"`
	} `json:"data"`
}

func parseMiniTicker(data []byte) (Ticker, error) {
	var m miniTickerEvent
	if err := json.Unmarshal(data, &m); err != nil {
		return Ticker{}, err
	}
	d := m.Data
	if d.Type != "24hrMiniTicker" {
		return Ticker{}, fmt.Errorf("unexpected message %.200s", data)
	}
	return ticker(d.Symbol, d.Open, d.Close, d.Quote)
}

func ohlcv(t int64, s [5]string) (Bar, error) {
	b := Bar{T: t}
	for i, dst := range [5]*float64{&b.O, &b.H, &b.L, &b.C, &b.V} {
		v, err := strconv.ParseFloat(s[i], 64)
		if err != nil {
			return Bar{}, err
		}
		*dst = v
	}
	return b, nil
}
