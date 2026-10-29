package market

import (
	"context"
	"os"
	"testing"
	"time"
)

// Eastmoney itself, with CANDLE_LIVE=1: an A share's, an index's and a Hong
// Kong share's bars of every size, opening when their markets trade, and
// their quotes and names; and Yahoo's VIX and Korean bars.
func TestListedLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE=1 asks Eastmoney and Yahoo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e := Eastmoney{}
	// Gently: Eastmoney shuts out an address asking much in a hurry, for an
	// hour or so (sixty-odd requests in ten seconds did).
	for _, sym := range []string{"SSE:600795", "SSE:000001", "HKEX:00700", "HKEX:HSI"} {
		in, _ := InstrumentOf(sym)
		for _, iv := range []string{"1m", "5m", "15m", "1h", "4h", "1d", "1w"} {
			time.Sleep(time.Second)
			bars, err := e.Klines(ctx, sym, iv, 0, 300)
			if err != nil || len(bars) == 0 {
				t.Fatalf("%s %s: %d bars, %v", sym, iv, len(bars), err)
			}
			for i, b := range bars {
				if i > 0 && b.T <= bars[i-1].T || b.H < b.L || b.H < max(b.O, b.C) || b.L > min(b.O, b.C) {
					t.Fatalf("%s %s bar %d: %+v after %+v", sym, iv, i, b, bars[max(i-1, 0)])
				}
				n := time.UnixMilli(b.T).In(in.Loc)
				if iv != "1w" && iv != "1d" {
					tod := time.Duration(n.Hour())*time.Hour + time.Duration(n.Minute())*time.Minute
					in := false
					for _, s := range InstrumentOrZero(sym).sessions() {
						in = in || tod >= s.open && tod < s.close
					}
					if !in {
						t.Fatalf("%s %s bar %d opens at %s, outside the sessions", sym, iv, i, n.Format("01-02 15:04"))
					}
				}
			}
			last := bars[len(bars)-1]
			t.Logf("%s %-3s %4d bars, first %s, last %s %.3f", sym, iv, len(bars), time.UnixMilli(bars[0].T).In(in.Loc).Format("2006-01-02 15:04"), time.UnixMilli(last.T).In(in.Loc).Format("01-02 15:04"), last.C)
		}
	}
	q, err := e.Quotes(ctx, []string{"SSE:600795", "SSE:000001", "HKEX:HSI", "HKEX:00700"})
	if err != nil || len(q) != 4 {
		t.Fatalf("quotes: %+v %v", q, err)
	}
	syms, err := e.Symbols(ctx, "SSE:600795", "HKEX:00700", "HKEX:HSI")
	if err != nil || syms[0].Desc != "国电电力" || syms[1].Tick != 0.001 || syms[2].Desc != "恒生指数" {
		t.Fatalf("symbols: %+v %v", syms, err)
	}
	t.Logf("quotes %+v; symbols %+v", q, syms)
	y := Yahoo{}
	for _, sym := range []string{"CBOE:VIX", "KRX:000660"} {
		for _, iv := range []string{"5m", "1h", "4h", "1d"} {
			bars, err := y.Klines(ctx, sym, iv, 0, 100)
			if err != nil || len(bars) == 0 {
				t.Fatalf("%s %s: %d bars, %v", sym, iv, len(bars), err)
			}
			in, _ := InstrumentOf(sym)
			t.Logf("%s %-3s %4d bars, last %s %.2f", sym, iv, len(bars), time.UnixMilli(bars[len(bars)-1].T).In(in.Loc).Format("01-02 15:04"), bars[len(bars)-1].C)
		}
	}
}

// InstrumentOrZero is InstrumentOf's market, or none.
func InstrumentOrZero(id string) Instrument { in, _ := InstrumentOf(id); return in }
