package market

import (
	"context"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"
)

// TestPerpLive asks Binance's perpetuals API itself, through the system
// proxy. CANDLE_LIVE=1 turns it on.
func TestPerpLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = SystemProxy
	p := Perp{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
	ctx := context.Background()
	t0 := time.Now()
	bars, err := p.Klines(ctx, "BTCUSDT.P", "1h", 0, 1000)
	if err != nil || len(bars) != 1000 {
		t.Fatalf("klines: %d, %v", len(bars), err)
	}
	t.Logf("klines: %d in %v, last %+v", len(bars), time.Since(t0).Round(time.Millisecond), bars[999])
	older, err := p.Klines(ctx, "BTCUSDT.P", "1h", bars[0].T-1, 1000)
	if err != nil || len(older) != 1000 || older[999].T >= bars[0].T {
		t.Fatalf("older: %d, %v", len(older), err)
	}
	syms, err := p.Symbols(ctx, "BTCUSDT.P", "1000PEPEUSDT.P", "NVDAUSDT.P")
	if err != nil || len(syms) != 3 || syms[0].Tick <= 0 || syms[1].Base != "1000PEPE" || syms[2].Desc != "股票永续" {
		t.Fatalf("symbols: %+v, %v", syms, err)
	}
	t.Logf("symbols: %+v", syms)
	if _, err := p.Symbols(ctx, "NOSUCHUSDT.P"); err == nil {
		t.Fatal("an unknown perpetual looked up")
	}
	tk, err := p.Tickers(ctx)
	if err != nil || !slices.ContainsFunc(tk, func(x Ticker) bool { return x.Symbol == "BTCUSDT.P" }) || slices.ContainsFunc(tk, func(x Ticker) bool { return !IsPerp(x.Symbol) }) {
		t.Fatalf("tickers: %d, %v", len(tk), err)
	}
	t.Logf("tickers: %d", len(tk))
	f, err := p.Funding(ctx, "BTCUSDT.P")
	if err != nil || f.Mark <= 0 || f.Next < time.Now().UnixMilli() {
		t.Fatalf("funding: %+v, %v", f, err)
	}
	t.Logf("funding: %+v, next in %v", f, time.Until(time.UnixMilli(f.Next)).Round(time.Second))
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	n := 0
	t0 = time.Now()
	p.Stream(sctx, "BTCUSDT.P", "1m", func(b Bar, _ bool, at int64) {
		if n++; n == 1 {
			t.Logf("stream: first bar after %v: %+v", time.Since(t0).Round(time.Millisecond), b)
		}
		if n == 3 {
			cancel()
		}
	})
	cancel()
	if n < 3 {
		t.Fatalf("stream: %d bars", n)
	}
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	got := map[string]bool{}
	p.Watch(wctx, []string{"BTCUSDT.P", "ETHUSDT.P"}, func(x Ticker) {
		got[x.Symbol] = true
		if len(got) == 2 {
			cancel()
		}
	})
	cancel()
	if !got["BTCUSDT.P"] || !got["ETHUSDT.P"] {
		t.Fatalf("watch: %v", got)
	}
}
