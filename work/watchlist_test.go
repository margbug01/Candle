package main

import (
	"slices"
	"testing"

	"candle/market"
)

// mixedApp is testApp watching coins, futures and stocks, mixed up.
func mixedApp() *application {
	a := testApp()
	a.set.Watch = []string{"BTCUSDT", "CME:NQ", "US:NVDA", "ETHUSDT", "CME:MNQ", "US:AAPL", "SOLUSDT"}
	for i, s := range a.set.Watch {
		if market.IsYahoo(s) {
			a.quotes[s] = market.Ticker{Symbol: s, Open: 100, Last: 100 + float64(i)}
		}
	}
	return a
}

func syms(rows []watchRow) (out []string) {
	for _, r := range rows {
		if r.sym == "" {
			out = append(out, "#"+r.sec.key)
		} else {
			out = append(out, r.sym)
		}
	}
	return out
}

// The watchlist parts by market, in the order the sections were put in, a
// folded one showing its heading only.
func TestWatchRows(t *testing.T) {
	a := mixedApp()
	if got, want := syms(a.watchRows()), []string{"#crypto", "BTCUSDT", "ETHUSDT", "SOLUSDT", "#futures", "CME:NQ", "CME:MNQ", "#us", "US:NVDA", "US:AAPL"}; !slices.Equal(got, want) {
		t.Fatalf("rows %v", got)
	}
	a.set.Sections, a.set.Folded = []string{"us", "nonsense", "crypto"}, []string{"crypto"}
	if got, want := syms(a.watchRows()), []string{"#us", "US:NVDA", "US:AAPL", "#crypto", "#futures", "CME:NQ", "CME:MNQ"}; !slices.Equal(got, want) {
		t.Fatalf("rows %v", got)
	}
	if r := a.watchRows()[3]; r.n != 3 {
		t.Fatalf("the folded coins count %d", r.n)
	}
	a.set.Watch = []string{"CME:NQ"}
	if got := syms(a.watchRows()); !slices.Equal(got, []string{"#futures", "CME:NQ"}) {
		t.Fatalf("an empty section shows: %v", got)
	}
}

// A market dragged goes where it drops in its section: to its top dropped
// above, its bottom below; the others stay where they are.
func TestMoveWatch(t *testing.T) {
	for _, c := range []struct {
		sym  string
		to   int // row it goes before
		want []string
	}{
		{"ETHUSDT", 1, []string{"ETHUSDT", "CME:NQ", "US:NVDA", "BTCUSDT", "CME:MNQ", "US:AAPL", "SOLUSDT"}},
		{"BTCUSDT", 4, []string{"ETHUSDT", "CME:NQ", "US:NVDA", "SOLUSDT", "CME:MNQ", "US:AAPL", "BTCUSDT"}},
		{"BTCUSDT", 10, []string{"ETHUSDT", "CME:NQ", "US:NVDA", "SOLUSDT", "CME:MNQ", "US:AAPL", "BTCUSDT"}}, // among the stocks: its bottom
		{"CME:MNQ", 0, []string{"BTCUSDT", "CME:MNQ", "US:NVDA", "ETHUSDT", "CME:NQ", "US:AAPL", "SOLUSDT"}},  // above all: its top
		{"US:AAPL", 8, []string{"BTCUSDT", "CME:NQ", "US:AAPL", "ETHUSDT", "CME:MNQ", "US:NVDA", "SOLUSDT"}},
	} {
		a := mixedApp()
		rows := a.watchRows()
		from := slices.IndexFunc(rows, func(r watchRow) bool { return r.sym == c.sym })
		a.moveWatch(rows, from, c.to)
		if !slices.Equal(a.set.Watch, c.want) {
			t.Errorf("%s before row %d: %v", c.sym, c.to, a.set.Watch)
		}
	}
}

func TestMoveSection(t *testing.T) {
	a := mixedApp()
	a.moveSection("us", "crypto")
	if !slices.Equal(a.set.Sections, []string{"us", "crypto", "perp", "futures"}) {
		t.Fatalf("us dropped on crypto: %v", a.set.Sections)
	}
	a.moveSection("us", "futures")
	if !slices.Equal(a.set.Sections, []string{"crypto", "perp", "futures", "us"}) {
		t.Fatalf("us dropped on futures: %v", a.set.Sections)
	}
}

// On the screen: a click on a heading folds it and another opens it; a
// row dragged below another in its section goes there; a heading dragged
// onto another takes its place; a market picked from the search opens its
// folded section.
func TestWatchlistOnScreen(t *testing.T) {
	a := mixedApp()
	tt := newTester(a, true)
	shot(t, tt, "watch-sections")
	click(t, tt, "期货")
	if !slices.Equal(a.set.Folded, []string{"futures"}) || tt.HasText(" 微型纳指") {
		t.Fatalf("folded %v, MNQ shows %v", a.set.Folded, tt.HasText(" 微型纳指"))
	}
	shot(t, tt, "watch-folded")
	click(t, tt, "期货")
	if len(a.set.Folded) != 0 || !tt.HasText(" 微型纳指") {
		t.Fatal("not opened again")
	}
	drag := func(from, to string, below bool) {
		t.Helper()
		f, ok1 := tt.Find(from)
		g, ok2 := tt.Find(to)
		if !ok1 || !ok2 {
			t.Fatalf("no %q or %q", from, to)
		}
		x, y := f.X+f.W/2, f.Y+f.H/2
		ty := g.Y + g.H*0.25
		if below {
			ty = g.Y + g.H*0.75
		}
		tt.Press(x, y)
		for i := 1; i <= 8; i++ {
			tt.Move(x, y+(ty-y)*float32(i)/8)
			tt.Frame()
		}
		tt.Release(x, ty)
		tt.Frame()
		tt.Frame()
	}
	// (BTC/USDT is the chart's too, in the top bar: ETH is the watchlist's alone.)
	drag("ETH/USDT", "SOL/USDT", true)
	if got := a.set.Watch; got[3] != "SOLUSDT" || got[len(got)-1] != "ETHUSDT" {
		t.Fatalf("ETH dragged below SOL: %v", got)
	}
	drag("AAPL 苹果", "加密货币", false)
	if got := a.set.Watch; got[2] != "US:AAPL" || got[5] != "US:NVDA" {
		t.Fatalf("AAPL dragged above the coins, to the top of its own section: %v", got)
	}
	drag("美股", "加密货币", false)
	if got := syms(a.watchRows())[0]; got != "#us" {
		t.Fatalf("the stocks dragged above the coins: %v", a.set.Sections)
	}
	shot(t, tt, "watch-moved")
	a.fold("futures")
	a.pick("CME:ES")
	if len(a.set.Folded) != 0 || a.set.Watch[0] != "CME:ES" {
		t.Fatalf("picked ES: folded %v, watch %v", a.set.Folded, a.set.Watch)
	}
}
