package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Binance's USDⓈ-M perpetual futures: perpetual contracts on coins, and on
// stocks and gold too (Binance calls those TradFi perpetuals). Their API has
// no mirror like data-api.binance.vision: fapi.binance.com and
// fstream.binance.com take a proxy from mainland China (the system's). The
// streams of market data are under /market: the old /ws and /stream still
// take a connection but send nothing on it.
const (
	perpREST = "https://fapi.binance.com"
	perpWS   = "wss://fstream.binance.com"
)

// IsPerp is whether Candle's name for a market is a perpetual's: its
// symbol and ".P", as TradingView writes it, "BTCUSDT.P".
func IsPerp(id string) bool { return len(id) > 2 && strings.HasSuffix(id, ".P") }

// perpSymbol is Binance's symbol for a perpetual Candle names id.
func perpSymbol(id string) string { return strings.TrimSuffix(id, ".P") }

// Perp reads Binance's perpetuals, by Candle's names for them. A client
// Timeout bounds REST requests and, for streams, only the handshake.
type Perp struct {
	HTTP     *http.Client
	Base, WS string // the hosts, "" for Binance's; for the tests
}

func (p Perp) rest() string {
	if p.Base != "" {
		return p.Base
	}
	return perpREST
}

func (p Perp) ws() string {
	if p.WS != "" {
		return p.WS
	}
	return perpWS
}

// Klines returns up to limit bars opening at or before end, as Binance.Klines.
func (p Perp) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	q := url.Values{"symbol": {perpSymbol(symbol)}, "interval": {interval}, "limit": {strconv.Itoa(limit)}}
	if end > 0 {
		q.Set("endTime", strconv.FormatInt(end, 10))
	}
	body, err := get(ctx, p.HTTP, p.rest()+"/fapi/v1/klines?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("binance perp klines: %w", err)
	}
	return parseKlines(body)
}

// Stream follows the current bar as Binance.Stream does.
func (p Perp) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	err := streamFrom(ctx, p.HTTP, p.ws()+"/market/ws/"+strings.ToLower(perpSymbol(symbol))+"@kline_"+interval, func(data []byte) error {
		bar, closed, at, err := parseKlineEvent(data)
		if err == nil {
			fn(bar, closed, at)
		}
		return err
	})
	return fmt.Errorf("binance perp stream: %w", err)
}

// Watch follows the markets' last 24 hours as Binance.Watch does.
func (p Perp) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	streams := make([]string, len(symbols))
	for i, s := range symbols {
		streams[i] = strings.ToLower(perpSymbol(s)) + "@miniTicker"
	}
	err := streamFrom(ctx, p.HTTP, p.ws()+"/market/stream?streams="+strings.Join(streams, "/"), func(data []byte) error {
		t, err := parseMiniTicker(data)
		if err == nil {
			t.Symbol += ".P"
			fn(t)
		}
		return err
	})
	return fmt.Errorf("binance perp watch: %w", err)
}

// Symbols looks perpetuals up. Binance answers with all of them at once
// (50 KB compressed); one unknown name fails the lookup.
func (p Perp) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	body, err := get(ctx, p.HTTP, p.rest()+"/fapi/v1/exchangeInfo")
	if err != nil {
		return nil, fmt.Errorf("binance perp symbols: %w", err)
	}
	all, err := parsePerpSymbols(body)
	if err != nil {
		return nil, fmt.Errorf("binance perp symbols: %w", err)
	}
	var out []Symbol
	for _, n := range names {
		i := slices.IndexFunc(all, func(s Symbol) bool { return s.Name == n })
		if i < 0 {
			return nil, fmt.Errorf("binance perp: no %s", n)
		}
		out = append(out, all[i])
	}
	return out, nil
}

// parsePerpSymbols keeps the perpetuals trading: coins' and TradFi's, not
// the quarterly contracts nor those settling.
func parsePerpSymbols(body []byte) ([]Symbol, error) {
	var r struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			Contract   string `json:"contractType"`
			Status     string `json:"status"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
			Underlying string `json:"underlyingType"`
			Filters    []struct {
				FilterType string `json:"filterType"`
				TickSize   string `json:"tickSize"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	var out []Symbol
	for _, s := range r.Symbols {
		if s.Status != "TRADING" || (s.Contract != "PERPETUAL" && s.Contract != "TRADIFI_PERPETUAL") {
			continue
		}
		sym := Symbol{Name: s.Symbol + ".P", Base: s.BaseAsset, Quote: s.QuoteAsset}
		switch s.Underlying {
		case "EQUITY":
			sym.Desc = "股票永续"
		case "COMMODITY":
			sym.Desc = "商品永续"
		}
		for _, f := range s.Filters {
			if f.FilterType == "PRICE_FILTER" {
				sym.Tick, _ = strconv.ParseFloat(f.TickSize, 64)
			}
		}
		out = append(out, sym)
	}
	return out, nil
}

// Tickers returns every perpetual traded in the last two days, for the
// search; the quarterly contracts (BTCUSDT_261225) are left out.
func (p Perp) Tickers(ctx context.Context) ([]Ticker, error) {
	body, err := get(ctx, p.HTTP, p.rest()+"/fapi/v1/ticker/24hr")
	if err != nil {
		return nil, fmt.Errorf("binance perp tickers: %w", err)
	}
	all, err := parseTickers(body, time.Now().Add(-48*time.Hour).UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("binance perp tickers: %w", err)
	}
	out := all[:0]
	for _, t := range all {
		if !strings.Contains(t.Symbol, "_") {
			t.Symbol += ".P"
			out = append(out, t)
		}
	}
	return out, nil
}

// Funding is a perpetual's mark price and funding: the rate the next
// settlement pays, as a fraction, longs to shorts when positive, and when.
type Funding struct {
	Mark, Rate float64
	Next       int64 // Unix ms
}

// Funding asks for a perpetual's mark price and funding.
func (p Perp) Funding(ctx context.Context, symbol string) (Funding, error) {
	body, err := get(ctx, p.HTTP, p.rest()+"/fapi/v1/premiumIndex?symbol="+url.QueryEscape(perpSymbol(symbol)))
	if err != nil {
		return Funding{}, fmt.Errorf("binance funding: %w", err)
	}
	return parseFunding(body)
}

func parseFunding(body []byte) (Funding, error) {
	var r struct {
		Mark string `json:"markPrice"`
		Rate string `json:"lastFundingRate"`
		Next int64  `json:"nextFundingTime"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Funding{}, err
	}
	var f Funding
	var err error
	if f.Mark, err = strconv.ParseFloat(r.Mark, 64); err != nil {
		return Funding{}, fmt.Errorf("mark price %q", r.Mark)
	}
	if f.Rate, err = strconv.ParseFloat(r.Rate, 64); err != nil {
		return Funding{}, fmt.Errorf("funding rate %q", r.Rate)
	}
	f.Next = r.Next
	return f, nil
}
