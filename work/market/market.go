// Package market turns quote and exchange APIs into one candle shape.
package market

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Bar is one candle. T is the open time in Unix milliseconds.
type Bar struct {
	T          int64
	O, H, L, C float64
	V          float64
}

// A Feed is where a market's bars and quotes come from: Binance for its spot
// pairs and its perpetuals, Yahoo for CME futures and US stocks.
type Feed interface {
	Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error)
	Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error
	Symbols(ctx context.Context, names ...string) ([]Symbol, error)
	Watch(ctx context.Context, symbols []string, fn func(Ticker)) error
}

var (
	_ Feed = Binance{}
	_ Feed = Yahoo{}
	_ Feed = Perp{}
)

func get(ctx context.Context, c *http.Client, u string) ([]byte, error) {
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Yahoo answers 429 to curl's and Go's default user agents.
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if strings.Contains(u, ".eastmoney.com/") || strings.Contains(u, "/api/qt/") {
		// As Eastmoney's own quote pages ask: it shuts out who asks otherwise
		// and too much.
		req.Header.Set("Referer", "https://quote.eastmoney.com/")
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %.300s", res.Status, body)
	}
	return body, nil
}
