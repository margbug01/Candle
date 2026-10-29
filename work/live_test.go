package main

import (
	"net/http"
	"os"
	"testing"
	"time"

	"candle/market"
	"candle/pine"
)

// TestLive runs the app against Binance, without a window: what the
// background work posts runs here, as the main thread would run it.
// CANDLE_LIVE=1 turns it on.
func TestLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = market.SystemProxy
	queue := make(chan func(), 1024)
	a := newApplication(defaultSettings(), "", market.Binance{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}},
		func(f func()) { queue <- f })
	// run drains the queue until done says so, or fails after d.
	run := func(what string, d time.Duration, done func() bool) {
		t.Helper()
		deadline := time.After(d)
		for !done() {
			select {
			case f := <-queue:
				f()
			case <-deadline:
				t.Fatalf("%s: not after %v", what, d)
			}
		}
	}
	for _, key := range []string{"ma", "boll", "macd", "rsi"} {
		k, _ := kindOf(key)
		a.addIndicator(k)
	}
	// worked reports where every indicator began working out, and checks it
	// came out as working out all of them does.
	worked := func(what string) (from []int) {
		t.Helper()
		for i := range a.set.Indicators {
			ind := &a.set.Indicators[i]
			s := a.seriesFor(ind)
			k, _ := kindOf(ind.Kind)
			var fresh series
			fresh.update(k, ind.Params, ind.Source, a.bars, a.gen, pine.Env{}, "")
			for j := range k.plots {
				same(t, what+" "+ind.Kind, s.out[j], fresh.out[j], 0)
			}
			from = append(from, s.from)
		}
		return from
	}
	t0 := time.Now()
	a.start()
	run("first page", 20*time.Second, func() bool { return len(a.bars) > 0 || a.err != "" })
	if a.err != "" {
		t.Fatal(a.err)
	}
	t.Logf("first page: %d bars in %v", len(a.bars), time.Since(t0).Round(time.Millisecond))
	worked("first page")
	run("live bar", 20*time.Second, func() bool { return a.stream == live })
	for _, from := range worked("live bar") {
		if from < len(a.bars)-2 {
			t.Fatalf("a live bar worked the indicators out from bar %d of %d", from, len(a.bars))
		}
	}
	t.Logf("stream live after %v; newest bar %s", time.Since(t0).Round(time.Millisecond), time.UnixMilli(a.bars[len(a.bars)-1].T).Format("01-02 15:04"))
	run("watchlist", 20*time.Second, func() bool { return len(a.quotes) == len(a.set.Watch) && len(a.infos) >= len(a.set.Watch) })
	// The icons come from binance.com, which takes the system proxy from mainland China.
	run("BTC's icon", 30*time.Second, func() bool { return a.icon("BTC") != nil })
	if w, h := a.icons.bitmaps["BTC"].Size(); w < 32 || h < 32 {
		t.Fatalf("BTC's icon is %dx%d", w, h)
	}
	t.Logf("%d icons listed; BTC's after %v", len(a.icons.urls), time.Since(t0).Round(time.Millisecond))
	run("all markets and the clock", 60*time.Second, func() bool { return a.allState >= 2 && a.clock != 0 })
	if a.allState != 2 {
		t.Fatal("the market list did not load")
	}
	t.Logf("BTCUSDT %+v; %d quotes, %d markets, Binance clock %+dms; all after %v",
		a.infos["BTCUSDT"], len(a.quotes), len(a.all), a.clock, time.Since(t0).Round(time.Millisecond))

	// Scrolled to the first bar, the page before it comes in, the view staying on the same bars.
	a.plotW = 900
	a.view.pan(1e6)
	a.view.clamp(a.plotW, len(a.bars))
	first := a.bars[0].T
	at := a.view.index(100, a.plotW, len(a.bars))
	a.loadOlder()
	run("older page", 20*time.Second, func() bool { return !a.older })
	if len(a.bars) != 2000 || a.bars[1000].T != first {
		t.Fatalf("after the older page: %d bars, bar 1000 opens %d, want %d", len(a.bars), a.bars[1000].T, first)
	}
	if got := a.view.index(100, a.plotW, len(a.bars)); got-at < 999.9 || got-at > 1000.1 {
		t.Fatalf("the view moved: bar %v under x=100, was %v before 1000 bars came in front", got, at)
	}
	if g := gapCount(a.bars, a.intervalMs()); g != 0 {
		t.Fatalf("%d gaps across the page boundary", g)
	}
	for _, from := range worked("older page") {
		if from != 0 {
			t.Fatalf("an older page worked the indicators out from bar %d", from)
		}
	}

	// A switch drops what the old symbol still sends.
	a.open("ETHUSDT", "1d")
	run("ETH daily", 20*time.Second, func() bool { return len(a.bars) > 0 })
	if len(a.bars) != 1000 || a.atStart {
		t.Fatalf("ETH daily, listed in 2017: %d bars from %s, atStart %v", len(a.bars), time.UnixMilli(a.bars[0].T).UTC(), a.atStart)
	}
	a.view = defaultView()
	tt := newTester(a, true)
	for range 20 {
		select {
		case f := <-queue:
			f()
		default:
		}
		tt.Frame()
	}
	tt.Move(560, 400)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "live-eth-1d")
}

func gapCount(bars []market.Bar, ms int64) (n int) {
	for i := 1; i < len(bars); i++ {
		if bars[i].T-bars[i-1].T != ms {
			n++
		}
	}
	return n
}

// TestLiveYahoo runs the app on Yahoo's futures and stocks, through the
// system proxy: a chart that loads and follows, the watchlist's quotes and
// the search. CANDLE_LIVE=1 turns it on.
func TestLiveYahoo(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = market.SystemProxy
	queue := make(chan func(), 1024)
	set := defaultSettings()
	set.Watch = []string{"CME:NQ", "CME:MNQ", "US:AAPL", "BTCUSDT"}
	a := newApplication(set, "", market.Binance{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}, func(f func()) { queue <- f })
	run := func(what string, d time.Duration, done func() bool) {
		t.Helper()
		deadline := time.After(d)
		for !done() {
			select {
			case f := <-queue:
				f()
			case <-deadline:
				t.Fatalf("%s: not after %v", what, d)
			}
		}
	}
	t0 := time.Now()
	a.open("CME:MNQ", "5m")
	a.watchQuotes()
	run("MNQ bars", 30*time.Second, func() bool { return len(a.bars) > 0 || a.err != "" })
	if a.err != "" {
		t.Fatal(a.err)
	}
	last := a.bars[len(a.bars)-1]
	t.Logf("MNQ 5m: %d bars in %v, newest %s %+v", len(a.bars), time.Since(t0).Round(time.Millisecond), time.UnixMilli(last.T).Format("01-02 15:04"), last)
	if len(a.bars) != 1000 || a.atStart {
		t.Fatalf("MNQ 5m: %d bars, at the start %v", len(a.bars), a.atStart)
	}
	run("MNQ following", 20*time.Second, func() bool { return a.stream == live })
	run("quotes", 30*time.Second, func() bool {
		return a.quotes["CME:NQ"].Last > 0 && a.quotes["US:AAPL"].Last > 0 && a.quotes["BTCUSDT"].Last > 0 && a.infos["US:AAPL"].Desc != ""
	})
	t.Logf("quotes after %v: NQ %+v, AAPL %+v (%s, tick %v)", time.Since(t0).Round(time.Millisecond), a.quotes["CME:NQ"], a.quotes["US:AAPL"], a.infos["US:AAPL"].Desc, a.infos["US:AAPL"].Tick)
	a.query = "apple"
	a.searchUS("apple")
	run("search", 30*time.Second, func() bool { return a.usFound["apple"] != nil })
	hits := a.findings("apple", 5)
	if len(hits) == 0 || hits[0].Symbol != "US:AAPL" {
		t.Fatalf("apple: %+v", hits)
	}
	// Scrolled to the first bar, the page before it comes in.
	a.plotW = 900
	a.view.pan(1e6)
	a.view.clamp(a.plotW, len(a.bars))
	first := a.bars[0].T
	a.loadOlder()
	run("older page", 30*time.Second, func() bool { return !a.older })
	if len(a.bars) <= 1000 || a.bars[len(a.bars)-1000].T != first {
		t.Fatalf("after the older page: %d bars", len(a.bars))
	}
	zero := 0
	for _, b := range a.bars {
		if b.V == 0 {
			zero++
		}
	}
	t.Logf("%d bars, %d with no volume", len(a.bars), zero)
	a.view = defaultView()
	tt := newTester(a, true)
	for range 20 {
		select {
		case f := <-queue:
			f()
		default:
		}
		tt.Frame()
	}
	shot(t, tt, "live-mnq-5m")
}
