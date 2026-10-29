package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"candle/market"
)

// perpApp is testApp with perpetuals to search and BTC's on the chart.
func perpApp() *application {
	a := testApp()
	a.perps = []market.Ticker{
		{Symbol: "BTCUSDT.P", Open: 79000, Last: 79950, QuoteVolume: 2e10},
		{Symbol: "ETHUSDT.P", Open: 2300, Last: 2320, QuoteVolume: 9e9},
		{Symbol: "1000PEPEUSDT.P", Open: 0.004, Last: 0.0041, QuoteVolume: 3e8},
		{Symbol: "NVDAUSDT.P", Open: 214, Last: 216, QuoteVolume: 5e7},
	}
	a.perpState = 2
	a.infos["NVDAUSDT.P"] = market.Symbol{Name: "NVDAUSDT.P", Base: "NVDA", Quote: "USDT", Tick: 0.01, Desc: "股票永续"}
	a.infos["BTCUSDT.P"] = market.Symbol{Name: "BTCUSDT.P", Base: "BTC", Quote: "USDT", Tick: 0.1}
	a.searchGen++
	return a
}

// A perpetual is its pair and 永续, from Binance's perpetuals feed, in a
// section of its own; the legend has its mark price and funding.
func TestPerpOnTheChart(t *testing.T) {
	a := perpApp()
	a.open("BTCUSDT.P", "1h")
	a.loading, a.bars, a.stream = false, fakeBars(600, 3_600_000), live
	if _, ok := a.feedOf("BTCUSDT.P").(market.Perp); !ok {
		t.Fatal("BTCUSDT.P not from the perpetuals feed")
	}
	if _, ok := a.barFeed("BTCUSDT.P").(market.Perp); !ok {
		t.Fatal("BTCUSDT.P's bars not from the perpetuals feed")
	}
	if base, quote := a.names("ETHUSDT.P"); base != "ETH" || quote != "USDT" || a.after("ETHUSDT.P") != "/USDT 永续" || a.after("NVDAUSDT.P") != "/USDT 股票永续" {
		t.Fatalf("names: %q %q %q %q", base, quote, a.after("ETHUSDT.P"), a.after("NVDAUSDT.P"))
	}
	if venue("BTCUSDT.P") != "币安永续" || sectionOf("BTCUSDT.P") != "perp" || !a.countsDown(3_600_000) {
		t.Fatal("venue, section or countdown")
	}
	a.funding, a.fundingOf = market.Funding{Mark: 79953.4, Rate: 0.00004967, Next: time.Now().Add(56*time.Minute + 49*time.Second).UnixMilli()}, "BTCUSDT.P"
	tt := newTester(a, true)
	for _, want := range []string{" 永续", "  ·  1小时  ·  币安永续", "标记 79953.4  ·  资金费率 +0.0050%  00:56:4"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in the legend", want)
		}
	}
	shot(t, tt, "perp")
	// A number left over is no coin: 100000X stays.
	for in, want := range map[string]string{"1000PEPE": "PEPE", "1MBABYDOGE": "BABYDOGE", "1000000MOG": "MOG", "10000SATS": "SATS", "1INCH": "1INCH", "BTC": "BTC", "1000": "1000", "100000X": "100000X"} {
		if got := iconCoin(in); got != want {
			t.Errorf("iconCoin(%s) = %s", in, got)
		}
	}
}

// The funding is asked for while the perpetual is on the chart, and goes
// to the chart it was asked for only.
func TestFollowFunding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"markPrice":"79953.4","lastFundingRate":"0.0001","nextFundingTime":1793260800000}`)
	}))
	defer srv.Close()
	a := testApp()
	got := make(chan func(), 4)
	a.post = func(f func()) { got <- f }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.followFunding(ctx, a.gen, market.Perp{HTTP: srv.Client(), Base: srv.URL}, "BTCUSDT.P")
	(<-got)()
	if a.fundingOf != "BTCUSDT.P" || a.funding.Rate != 0.0001 || a.funding.Mark != 79953.4 {
		t.Fatalf("funding %+v of %s", a.funding, a.fundingOf)
	}
	a.gen++ // another chart
	a.fundingOf = ""
	go a.followFunding(ctx, a.gen-1, market.Perp{HTTP: srv.Client(), Base: srv.URL}, "BTCUSDT.P")
	(<-got)()
	if a.fundingOf != "" {
		t.Fatal("an old chart's funding taken")
	}
}

// Perpetuals are found as their pairs are, after them; 1000PEPE as pepe.
func TestSearchFindsPerps(t *testing.T) {
	a := perpApp()
	ids := func(ts []market.Ticker) (s []string) {
		for _, x := range ts {
			s = append(s, x.Symbol)
		}
		return s
	}
	a.all = append(a.all, market.Ticker{Symbol: "PEPECOINUSDT", Open: 1, Last: 1, QuoteVolume: 1e9},
		market.Ticker{Symbol: "1000PEPEXUSDT", Open: 1, Last: 1, QuoteVolume: 1e9})
	a.searchGen++
	for q, want := range map[string][]string{
		"btc":      {"BTCUSDT", "BTCUSDT.P"},
		"pepe":     {"PEPEUSDT", "PEPEFDUSD", "1000PEPEUSDT.P", "PEPECOINUSDT"}, // the perpetual as pepe's own
		"nvda":     {"US:NVDA", "NVDAUSDT.P"},                                   // the stock before Binance's perpetual on it
		"1000pepe": {"1000PEPEUSDT.P", "1000PEPEXUSDT"},                         // the perpetual's own coin as it reads
	} {
		if got := ids(a.findings(q, 10)); !slices.Equal(got[:min(len(got), len(want))], want) {
			t.Errorf("%s: %v, want %v first", q, got, want)
		}
	}
	got := byFeed([]string{"BTCUSDT.P", "BTCUSDT", "US:AAPL", "ETHUSDT.P"})
	if len(got) != 3 || !slices.Equal(got[1], []string{"BTCUSDT.P", "ETHUSDT.P"}) {
		t.Fatalf("by feed: %v", got)
	}
}

// The search shows each section's first few, the section of the best match
// first; 全部 shows a section whole, 全部结果 goes back; Enter opens the
// best of what shows.
func TestSearchGroups(t *testing.T) {
	a := perpApp()
	var many []market.Ticker
	for i := range 6 {
		many = append(many, market.Ticker{Symbol: fmt.Sprintf("BTC%cUSDT", 'A'+i), Open: 1, Last: 1, QuoteVolume: float64(1e6 - i)})
	}
	a.all = append(a.all, many...)
	a.usList = market.WithPopular([]market.Instrument{{ID: "US:BTC", Code: "BTC", Ticker: "BTC", Name: "Grayscale Bitcoin Mini Trust", Tick: 0.01}})
	for i := range 6 {
		code := fmt.Sprintf("BTC%c", 'A'+i)
		a.usList = append(a.usList, market.Instrument{ID: "US:" + code, Code: code, Ticker: code, Tick: 0.01})
	}
	a.searchGen++
	tt := newTester(a, true)
	a.query = "btc"
	tt.Frame()
	tt.Frame()
	var keys []string
	for _, g := range a.foundFor("btc").groups {
		keys = append(keys, g.sec.key)
	}
	if !slices.Equal(keys, []string{"crypto", "perp", "us"}) {
		t.Fatalf("sections %v", keys)
	}
	// Prices are asked for what shows: the stocks' first four.
	priced := 0
	for id := range a.priced {
		if strings.HasPrefix(id, "US:") {
			priced++
		}
	}
	if priced != searchPeek {
		t.Fatalf("prices asked for %d stocks", priced)
	}
	if !tt.HasText("永续合约") || !tt.HasText("美股") || tt.HasText("BTCEUSDT") {
		t.Fatal("the sections, or more than their first few")
	}
	if _, ok := tt.Find("查看全部永续合约"); ok {
		t.Fatal("全部 on a section that shows whole")
	}
	shot(t, tt, "search-groups")
	click(t, tt, "查看全部加密货币")
	if a.searchKind != "crypto" || tt.HasText("永续合约") || !tt.HasText("‹ 全部结果") {
		t.Fatalf("the coins whole: %q", a.searchKind)
	}
	if _, ok := tt.Find("BTCF/USDT"); !ok {
		t.Fatal("the sixth coin does not show")
	}
	shot(t, tt, "search-whole")
	click(t, tt, "返回全部结果")
	if a.searchKind != "" || !tt.HasText("永续合约") {
		t.Fatal("not back to every section")
	}
	a.searchKind = "perp"
	if a.firstFinding() != "BTCUSDT.P" {
		t.Fatalf("Enter in the perpetuals opens %s", a.firstFinding())
	}
	a.searchKind = "nonsense"
	tt.Frame()
	if a.searchKind != "" {
		t.Fatal("a section with nothing found kept")
	}
	a.query = "nvda"
	tt.Frame()
	tt.Frame()
	if g := a.foundFor("nvda").groups; g[0].sec.key != "us" || g[1].sec.key != "perp" {
		t.Fatalf("nvda: %v first", g[0].sec.key)
	}
	a.perpState = 3
	tt.Frame()
	if !tt.HasText("币安永续没连上（国内要开代理）") {
		t.Fatal("no word that the perpetuals did not come")
	}
	a.pick("ETHUSDT.P")
	if a.query != "" || a.set.Watch[0] != "ETHUSDT.P" || a.symbol != "ETHUSDT.P" {
		t.Fatalf("picked ETHUSDT.P: %v %s", a.set.Watch[:2], a.symbol)
	}
}

// A section added since the order was kept goes after the one it follows at
// first: the perpetuals after the coins.
func TestSectionOrderTakesNewSections(t *testing.T) {
	a := testApp()
	for _, c := range []struct{ kept, want []string }{
		{nil, []string{"crypto", "perp", "futures", "us"}},
		{[]string{"us", "crypto", "futures"}, []string{"us", "crypto", "perp", "futures"}},
		{[]string{"futures", "us"}, []string{"crypto", "perp", "futures", "us"}},
		{[]string{"perp", "us", "crypto", "futures"}, []string{"perp", "us", "crypto", "futures"}},
	} {
		a.set.Sections = c.kept
		var got []string
		for _, s := range a.sectionOrder() {
			got = append(got, s.key)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("kept %v: %v, want %v", c.kept, got, c.want)
		}
	}
}

// Pairs of the coin itself come first, in whatever money, the busiest in
// dollars first: BTCIDR's rupiah count through USDTIDR's price, not as so
// many rupiah; money with no price counts nothing.
func TestSearchRanksPairsInDollars(t *testing.T) {
	pairs := []market.Ticker{
		{Symbol: "BTCJPY", Last: 1.2e7, QuoteVolume: 1e12},
		{Symbol: "BTCIDR", Last: 1.3e9, QuoteVolume: 1e12}, // 62.5M dollars
		{Symbol: "BTCUSDC", Last: 79900, QuoteVolume: 2e9},
		{Symbol: "BTCUSDT", Last: 79950, QuoteVolume: 9e9},
		{Symbol: "ETHBTC", Last: 0.029, QuoteVolume: 3000}, // 240M dollars, not BTC's own
		{Symbol: "USDTIDR", Last: 16000, QuoteVolume: 1e10},
		{Symbol: "BTCDOMUSDT.P", Last: 5584, QuoteVolume: 1e7},
	}
	var got []string
	for _, h := range searchAll("btc", pairs, nil, nil, nil, 10) {
		got = append(got, h.Symbol)
	}
	if want := []string{"BTCUSDT", "BTCUSDC", "BTCIDR", "BTCJPY", "BTCDOMUSDT.P", "ETHBTC"}; !slices.Equal(got, want) {
		t.Fatalf("btc: %v, want %v", got, want)
	}
	if b, q := splitPair("BTCFDUSD"); b != "BTC" || q != "FDUSD" {
		t.Fatalf("BTCFDUSD is %s in %s", b, q)
	}
	if b, q := splitPair("ETHBTC"); b != "ETH" || q != "BTC" {
		t.Fatalf("ETHBTC is %s in %s", b, q)
	}
}
