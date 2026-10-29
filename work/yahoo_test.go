package main

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"candle/market"
)

// The search finds every source's markets: a coin's pair before a stock of
// the same code, futures by code or Chinese name, stocks by code, Chinese
// name or a word of their name, the popular first.
func TestSearchAll(t *testing.T) {
	pairs := []market.Ticker{{Symbol: "BTCUSDT", QuoteVolume: 9e9}, {Symbol: "WBTCUSDT", QuoteVolume: 1e8}, {Symbol: "ESUSDT", QuoteVolume: 1e6}}
	stocks := market.WithPopular([]market.Instrument{
		{ID: "US:BTC", Code: "BTC", Name: "Grayscale Bitcoin Mini Trust"},
		{ID: "US:APLE", Code: "APLE", Name: "Apple Hospitality REIT, Inc."},
		{ID: "US:AAPL", Code: "AAPL", Name: "Apple Inc."},
		{ID: "US:AAPY", Code: "AAPY", Name: "Kurv Yield Premium Strategy Apple (AAPL) ETF"},
		{ID: "US:SNAP", Code: "SNAP", Name: "Snap Inc."},
		{ID: "US:A", Code: "A", Name: "Agilent Technologies, Inc."},
		{ID: "US:AA", Code: "AA", Name: "Alcoa Corporation"},
		{ID: "US:QQQ", Code: "QQQ", Name: "Invesco QQQ Trust, Series 1"},
	})
	ids := func(ts []market.Ticker) (s []string) {
		for _, t := range ts {
			s = append(s, t.Symbol)
		}
		return s
	}
	for _, c := range []struct {
		q    string
		n    int
		want []string
	}{
		{"btc", 10, []string{"BTCUSDT", "US:BTC", "WBTCUSDT"}},
		{"nq", 10, []string{"CME:NQ", "CME:MNQ"}},
		{"MNQ", 10, []string{"CME:MNQ"}},
		{"es", 10, []string{"ESUSDT", "CME:ES", "CME:MES", "US:NTES"}}, // not SERIES nor TechnologiES
		{"纳指", 10, []string{"CME:NQ", "CME:MNQ", "US:QQQ", "US:TQQQ", "US:SQQQ"}},
		{"苹果", 10, []string{"US:AAPL"}},
		{"阿里", 10, []string{"US:BABA"}},
		// Apple Inc. and Apple Hospitality start with the word, the popular
		// first; the ETF holds it further in.
		{"apple", 10, []string{"US:AAPL", "US:APLE", "US:AAPY"}},
		{"aapl", 10, []string{"US:AAPL", "US:AAPY"}},
		// Of the codes starting with A, the popular, then the shortest.
		{"a", 6, []string{"US:A", "US:AAPL", "US:AMZN", "US:AMD", "US:AVGO", "US:ARM"}},
		{"snap", 10, []string{"US:SNAP"}},
		{"", 10, nil},
	} {
		if got := ids(searchAll(c.q, pairs, market.Futures, stocks, c.n)); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.q, got, c.want)
		}
	}
}

// yahooApp is testApp showing hourly NQ bars on the 0.25 tick.
func yahooApp(sym, iv string, ivMs int64) *application {
	a := testApp()
	a.open(sym, iv)
	bars := fakeBars(600, ivMs)
	for i := range bars {
		b := &bars[i]
		for _, p := range []*float64{&b.O, &b.H, &b.L, &b.C} {
			*p = float64(int(*p*4)) / 4
		}
	}
	a.loading, a.bars, a.atStart, a.stream = false, bars, true, live
	return a
}

// A future reads as its code and what it is, with its exchange and how late
// it comes; its prices take its tick's decimals; its icon is a letter, not a
// coin of Binance's to look up.
func TestFutureOnTheChart(t *testing.T) {
	var spawned int
	a := yahooApp("CME:NQ", "1h", 3_600_000)
	a.set.Watch = []string{"CME:NQ", "CME:MNQ"} // no coins of Binance's to show
	a.spawn = func(func()) { spawned++ }
	tt := newTester(a, true)
	for _, want := range []string{" 纳指 E-mini", "  ·  1小时  ·  CME · 晚 10 分钟", "晚 10 分"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q on the chart", want)
		}
	}
	if tt.HasText("实时") {
		t.Fatal("ten minutes late, and the status says live")
	}
	if got := a.priceText("CME:NQ", 29737.5); got != "29737.50" {
		t.Fatalf("NQ at %s", got)
	}
	if a.icons.listing || a.icons.urls != nil || spawned != 0 {
		t.Fatalf("looked for a coin icon for NQ: listing %v, %d started", a.icons.listing, spawned)
	}
	if base, quote := a.names("CME:MNQ"); base != "MNQ" || quote != "" || a.after("CME:MNQ") != " 微型纳指" {
		t.Fatalf("MNQ reads %q %q %q", base, quote, a.after("CME:MNQ"))
	}
	if a.volumeUnit("CME:NQ") != "手" || a.volumeUnit("US:AAPL") != "股" || a.volumeUnit("BTCUSDT") != "BTC" {
		t.Fatal("volume units")
	}
	shot(t, tt, "future")
}

// A stock's day lines open its sessions, at 9:30 New York time, whatever the
// chart's zone; a future's at 18:00 the evening before.
func TestSessionLines(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	day := func(d int, h, m int) int64 { return time.Date(2026, 10, d, h, m, 0, 0, ny).UnixMilli() }
	var bars []market.Bar
	for d := 26; d <= 28; d++ {
		for k := range 7 { // 9:30 … 15:30
			bars = append(bars, market.Bar{T: day(d, 9, 30) + int64(k)*3_600_000})
		}
	}
	a := testApp()
	a.symbol = "US:AAPL"
	a.set.Chart.Zone = "Asia/Shanghai"
	got := dayBreaks(bars, 0, len(bars)-1, 3_600_000, a.dayOf())
	if !slices.Equal(got, []dayBreak{{7, 0}, {14, 0}}) {
		t.Fatalf("stock: %v", got)
	}
	// Hourly NQ from Tuesday 15:00 to Wednesday 3:00, no 17:00 hour.
	bars = nil
	for h := day(27, 15, 0); h <= day(28, 3, 0); h += 3_600_000 {
		if time.UnixMilli(h).In(ny).Hour() != 17 {
			bars = append(bars, market.Bar{T: h})
		}
	}
	a.symbol = "CME:NQ"
	if got := dayBreaks(bars, 0, len(bars)-1, 3_600_000, a.dayOf()); len(got) != 1 || time.UnixMilli(bars[got[0].i].T).In(ny).Hour() != 18 {
		t.Fatalf("future: %v", got)
	}
}

// The watchlist's quotes come from both: Binance's pairs on its stream, the
// rest from Yahoo, each asked for once.
func TestByFeed(t *testing.T) {
	got := byFeed([]string{"CME:NQ", "BTCUSDT", "US:AAPL", "ETHUSDT"})
	if len(got) != 2 || !slices.Equal(got[0], []string{"BTCUSDT", "ETHUSDT"}) || !slices.Equal(got[1], []string{"CME:NQ", "US:AAPL"}) {
		t.Fatalf("%v", got)
	}
	a := testApp()
	if _, ok := a.feedOf("CME:MNQ").(market.Yahoo); !ok {
		t.Fatal("MNQ from Binance")
	}
	if _, ok := a.feedOf("BTCUSDT").(market.Binance); !ok {
		t.Fatal("BTCUSDT from Yahoo")
	}
}

// What Yahoo found shows in the search with the prices it came with; picking
// a stock puts it on the watchlist and the chart.
func TestSearchFindsStocksAndFutures(t *testing.T) {
	a := testApp()
	a.usFound["apple"] = []market.Instrument{{ID: "US:AAPL", Code: "AAPL", Ticker: "AAPL", Name: "Apple Inc."}}
	a.usNames["US:AAPL"] = "Apple Inc."
	a.quotes["US:AAPL"] = market.Ticker{Symbol: "US:AAPL", Open: 347.1, Last: 343.83}
	if got := a.priceText("US:AAPX", 42.2); got != "42.20" { // not looked up yet: cents
		t.Fatalf("AAPX at %s", got)
	}
	tt := newTester(a, true)
	a.query = "apple"
	tt.Frame()
	tt.Frame()
	if !tt.HasText(" 苹果") || !tt.HasText("-0.94%") || !tt.HasText("343.83") {
		t.Fatal("Apple is not in the results with its move")
	}
	shot(t, tt, "search-stock")
	tt.ClickAt(1320-watchW/2, titleBarHeight+28+44+20)
	tt.Frame()
	tt.Frame()
	if a.symbol != "US:AAPL" || a.set.Watch[0] != "US:AAPL" || a.query != "" {
		t.Fatalf("after picking Apple: showing %s, watch %v", a.symbol, a.set.Watch)
	}
	a.query = "微型"
	tt.Frame()
	tt.Frame()
	if !tt.HasText(" 微型纳指") || !tt.HasText(" 微型标普") {
		t.Fatal("the micro futures are not found by name")
	}
}

func TestCountsDown(t *testing.T) {
	a := testApp()
	for _, c := range []struct {
		sym  string
		ivMs int64
		want bool
	}{{"BTCUSDT", 86_400_000, true}, {"CME:NQ", 3_600_000, true}, {"CME:NQ", 86_400_000, false}, {"US:AAPL", 604_800_000, false}} {
		a.symbol = c.sym
		if got := a.countsDown(c.ivMs); got != c.want {
			t.Errorf("%s every %dms: counts down %v", c.sym, c.ivMs, got)
		}
	}
}

// burst is a feed whose quotes all come at once, then nothing until ctx ends,
// as Yahoo's every ten seconds.
type burst struct{ market.Feed }

func (burst) Watch(ctx context.Context, syms []string, fn func(market.Ticker)) error {
	for _, s := range syms {
		fn(market.Ticker{Symbol: s, Open: 1, Last: 2})
	}
	<-ctx.Done()
	return ctx.Err()
}

// Every quote of a burst is passed on within half a second, not only the first.
func TestFollowPassesEveryQuote(t *testing.T) {
	got := make(chan map[string]market.Ticker, 10)
	a := testApp()
	a.quotes = map[string]market.Ticker{}
	a.post = func(f func()) { f(); got <- maps.Clone(a.quotes) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.follow(ctx, burst{}, []string{"CME:NQ", "CME:MNQ", "US:AAPL"})
	select {
	case q := <-got:
		if len(q) != 3 {
			t.Fatalf("passed on %v", q)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing passed on")
	}
}

// A future's chart chooses its hours, ETH or RTH: RTH reloads the bars from
// a feed in the regular hours and says so in the legend; days have no such
// choice; a coin's chart has no buttons.
func TestSessionButtons(t *testing.T) {
	a := yahooApp("CME:NQ", "1h", 3_600_000)
	a.spawn = func(f func()) {}
	tt := newTester(a, true)
	if !tt.HasText("ETH") || !tt.HasText("RTH") || tt.HasText(" · RTH") {
		t.Fatal("no ETH and RTH buttons, or RTH said before it is chosen")
	}
	gen := a.gen
	click(t, tt, "RTH")
	tt.Frame()
	if !a.set.FuturesRTH || a.gen == gen || !a.loading {
		t.Fatalf("RTH chosen: setting %v, reloaded %v", a.set.FuturesRTH, a.gen != gen)
	}
	if y, ok := a.barFeed("CME:NQ").(market.Yahoo); !ok || !y.RTH {
		t.Fatal("the bars come from a feed of every hour")
	}
	a.loading, a.bars = false, fakeBars(600, 3_600_000)
	a.quotes["CME:NQ"] = market.Ticker{Symbol: "CME:NQ", Open: 29900, Last: 29677.75}
	tt.Frame()
	if !tt.HasText(" · RTH") {
		t.Fatal("the legend does not say RTH")
	}
	// After the regular hours the last bar is old: the top bar has the quote.
	if !tt.HasText("29677.75") {
		t.Fatal("the top bar shows the regular hours' last close, not the price now")
	}
	shot(t, tt, "future-rth")
	a.open("CME:NQ", "1d")
	a.loading, a.bars = false, fakeBars(600, 86_400_000)
	tt.Frame()
	if tt.HasText(" · RTH") || a.rth() {
		t.Fatal("days in the regular hours")
	}
	a.open("BTCUSDT", "1h")
	tt.Frame()
	if tt.HasText("RTH") {
		t.Fatal("RTH on a coin's chart")
	}
}
