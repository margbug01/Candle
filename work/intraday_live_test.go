package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"candle/market"
)

// TestIntradayLive opens the minutes of a few days of each feed's, asking
// the feeds themselves through the system proxy. CANDLE_LIVE=1 turns it on.
func TestIntradayLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = market.SystemProxy
	hc := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	feeds := map[string]market.Feed{
		"BTCUSDT": market.Binance{HTTP: hc}, "US:AAPL": market.Yahoo{HTTP: hc},
		"CME:ES": market.Yahoo{HTTP: hc}, "SSE:600519": market.Eastmoney{HTTP: hc},
	}
	ctx := context.Background()
	for sym, feed := range feeds {
		days, err := feed.Klines(ctx, sym, "1d", 0, 120)
		if err != nil {
			t.Fatalf("%s days: %v", sym, err)
		}
		for _, back := range []int{1, 5, 30, 100} {
			b := days[len(days)-back]
			from, open, to, minutes := dayWindow(sym, b.T)
			bars, iv, err := dayBars(ctx, feed, sym, from, open, to)
			if err != nil {
				t.Fatalf("%s %d days back: %v", sym, back, err)
			}
			line := sym + " " + time.UnixMilli(b.T).Format("01-02 15:04") + ": " + itoa(len(bars)) + " of " + iv + ", a day " + itoa(minutes) + " min"
			if len(bars) > 0 {
				lo, hi := bars[0].L, bars[0].H
				for _, m := range bars {
					lo, hi = min(lo, m.L), max(hi, m.H)
				}
				// The minutes' high and low beside the day's: the same where the window is right.
				line += ", " + time.UnixMilli(bars[0].T).Format("01-02 15:04") + " to " + time.UnixMilli(bars[len(bars)-1].T).Format("01-02 15:04") +
					", high " + formatPrice(hi, 2) + "/" + formatPrice(b.H, 2) + ", low " + formatPrice(lo, 2) + "/" + formatPrice(b.L, 2)
			}
			t.Log(line)
		}
	}
}
