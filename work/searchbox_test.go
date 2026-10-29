package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"candle/market"

	"github.com/egoist/mygo/ui"
)

const searchLabel = "搜索交易对、期货、美股"

// The search goes by the Chinese names of coins, of what TradFi perpetuals
// trade and of US stocks, Eastmoney's among them; a stock found by its
// Chinese name comes before the perpetual following it.
func TestSearchChinese(t *testing.T) {
	pairs := []market.Ticker{
		{Symbol: "BTCUSDT", QuoteVolume: 9e9}, {Symbol: "BTCTRY", QuoteVolume: 1e9}, {Symbol: "BIOUSDT", QuoteVolume: 1e7},
		{Symbol: "BNBUSDT", QuoteVolume: 1e9}, {Symbol: "BTCUSDT.P", QuoteVolume: 1e10}, {Symbol: "NVDAUSDT.P", QuoteVolume: 1e8},
		{Symbol: "SKHYNIXUSDT.P", QuoteVolume: 1e8}, {Symbol: "SKUUUSDT.P", QuoteVolume: 1e6}, {Symbol: "SNDKUSDT.P", QuoteVolume: 1e7},
		{Symbol: "XAUUSDT.P", QuoteVolume: 1e9}, {Symbol: "TENCENTUSDT.P", QuoteVolume: 1e6}, {Symbol: "HK0700USDT.P", QuoteVolume: 1e6},
		{Symbol: "CLUSDT.P", QuoteVolume: 1e7}, {Symbol: "ZTSTUSDT.P", QuoteVolume: 1e9},
	}
	stocks := market.WithPopular([]market.Instrument{
		{ID: "US:SNDK", Code: "SNDK", Name: "Sandisk Corporation"},
		{ID: "US:NVDA", Code: "NVDA", Name: "NVIDIA Corporation"},
	})
	found := map[string][]market.Instrument{
		"海力士": {
			{ID: "US:YSKH", Code: "YSKH", Alias: "海力士期权收益策略ETF-YieldMax"},
			{ID: "US:SKUU", Code: "SKUU", Alias: "二倍做多海力士ETF-GraniteShares"},
			{ID: "US:SKHY", Code: "SKHY", Alias: "SK海力士"},
		},
		"闪迪":   {{ID: "US:SNDK", Code: "SNDK", Alias: "闪迪"}},
		"测试科技": {{ID: "US:ZTST", Code: "ZTST", Alias: "测试科技"}},
		"比特币":  {{ID: "US:IBIT", Code: "IBIT", Alias: "比特币ETF-iShares"}, {ID: "US:BITB", Code: "BITB", Name: "Bitwise Bitcoin ETF"}},
	}
	ids := func(q string, n int) (s []string) {
		for _, h := range searchAll(q, pairs, market.Futures, stocks, found[q], n) {
			s = append(s, h.Symbol)
		}
		return s
	}
	for _, c := range []struct {
		q    string
		n    int
		want []string
	}{
		// SK海力士 itself, then the perpetuals by the name, then the ETFs
		// holding it, the leveraged one's perpetual after its stock.
		{"海力士", 10, []string{"US:SKHY", "SKHYNIXUSDT.P", "US:YSKH", "US:SKUU", "SKUUUSDT.P"}},
		{"闪迪", 10, []string{"US:SNDK", "SNDKUSDT.P"}},
		// One not among the popular too, and its perpetual by Eastmoney's name.
		{"测试科技", 10, []string{"US:ZTST", "ZTSTUSDT.P"}},
		{"英伟达", 10, []string{"US:NVDA", "NVDAUSDT.P"}},
		{"英伟", 10, []string{"US:NVDA", "NVDAUSDT.P"}},
		// The coin in every money, its perpetual, the ETF named after it,
		// then what Eastmoney found by what the name does not show.
		{"比特币", 10, []string{"BTCUSDT", "BTCTRY", "BTCUSDT.P", "US:IBIT", "US:BITB"}},
		{"btb", 10, []string{"BTCUSDT", "BTCUSDT.P", "US:IBIT", "BTCTRY"}},
		{"bitebi", 10, []string{"BTCUSDT", "BTCUSDT.P", "US:IBIT", "BTCTRY"}},
		{"hailishi", 10, []string{"SKHYNIXUSDT.P", "US:SKHY"}},
		{"黄金", 10, []string{"XAUUSDT.P", "CME:GC", "US:GLD"}},
		{"腾讯", 10, []string{"HK0700USDT.P", "TENCENTUSDT.P", "US:TME"}},
	} {
		if got := ids(c.q, c.n); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.q, got, c.want)
		}
	}
	// Two letters: the codes starting with them before the pinyin (BIO, then
	// 比特币); a syllable inside a coin's name is not its start.
	bi := ids("bi", 50)
	if i, j := slices.Index(bi, "BIOUSDT"), slices.Index(bi, "BTCUSDT"); i < 0 || j < 0 || i > j {
		t.Errorf("bi: %v", bi)
	}
	if te := ids("te", 50); slices.Contains(te, "BTCUSDT") || !slices.Contains(te, "US:TSLA") {
		t.Errorf("te: %v", te)
	}
	if cl := ids("cl", 50); len(cl) < 2 || cl[0] != "CLUSDT.P" || cl[1] != "CME:CL" {
		t.Errorf("cl: %v", cl)
	}
}

// The box searches as an input method composes: letters as typed, Pinyin
// on or not, its syllables run together, and the Chinese once chosen. The
// keys pressed meanwhile are the input method's.
func TestSearchComposes(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	click(t, tt, searchLabel)
	if active, noIME := tt.TextInputActive(); !active || noIME {
		t.Fatalf("the search box: text input %v, input method off %v", active, noIME)
	}
	if r, ok := tt.TextCaret(); !ok || r.H <= 0 {
		t.Fatalf("no caret for the candidates: %v %v", r, ok)
	}
	compose := func(s string) {
		t.Helper()
		tt.Compose(s, len([]rune(s)))
		tt.Frame()
		tt.Frame()
	}
	compose("sndk")
	if a.query != "" || a.searchQuery() != "sndk" || !tt.HasText(" 闪迪") {
		t.Fatalf("composing sndk: query %q, searching %q, SNDK shown %v", a.query, a.searchQuery(), tt.HasText(" 闪迪"))
	}
	press(tt, 0, ui.KeyEnter)
	if a.symbol != "BTCUSDT" || a.qb.compose != "sndk" {
		t.Fatalf("Enter while composing opened %s", a.symbol)
	}
	compose("shan'di")
	if a.searchQuery() != "shandi" || !tt.HasText(" 闪迪") {
		t.Fatalf("shan'di: %q", a.searchQuery())
	}
	tt.Compose("", 0)
	tt.Type("闪迪")
	tt.Frame()
	tt.Frame()
	if a.query != "闪迪" || a.qb.compose != "" || a.qb.caret != 2 || !tt.HasText(" 闪迪") {
		t.Fatalf("闪迪 chosen: query %q, composing %q, caret %d", a.query, a.qb.compose, a.qb.caret)
	}
	// What is composed goes in at the caret, in place of the selection.
	tt.Command("selectAll")
	compose("ywd")
	if a.query != "" || a.searchQuery() != "ywd" || !tt.HasText(" 英伟达") {
		t.Fatalf("composing over the selection: query %q, searching %q", a.query, a.searchQuery())
	}
	tt.Compose("", 0)
	tt.Type("nv")
	tt.Key(0, ui.KeyLeft)
	compose("a")
	if a.searchQuery() != "nav" {
		t.Fatalf("at the caret: %q", a.searchQuery())
	}
	// Input methods drop their composition with the focus.
	tt.ClickAt(200, 300)
	tt.Frame()
	tt.Frame()
	if a.qb.compose != "" || a.searchQuery() != "nv" {
		t.Fatalf("after the focus left: composing %q, searching %q", a.qb.compose, a.searchQuery())
	}
}

// The box edits as a field does: the caret, a selection, by word and to
// the ends, the clipboard, Undo, Escape and Enter.
func TestSearchBoxEdits(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	click(t, tt, searchLabel)
	key := func(mods ui.Modifiers, k ui.Key, n int) {
		for range n {
			tt.Key(mods, k)
		}
		tt.Frame()
	}
	check := func(step, q string, caret, anchor int) {
		t.Helper()
		if a.query != q || a.qb.caret != caret || a.qb.anchor != anchor {
			t.Fatalf("%s: %q caret %d anchor %d, want %q %d %d", step, a.query, a.qb.caret, a.qb.anchor, q, caret, anchor)
		}
	}
	tt.Type("btcusdt")
	tt.Type("\r") // Enter's character, on Windows
	tt.Type("\t")
	check("typed", "btcusdt", 7, 7)
	key(0, ui.KeyLeft, 4)
	tt.Type("/")
	check("a slash typed", "btc/usdt", 4, 4)
	key(ui.Shift, ui.KeyHome, 1)
	check("Shift+Home", "btc/usdt", 0, 4)
	tt.Type("eth/")
	check("typed over the selection", "eth/usdt", 4, 4)
	key(ui.Cmd, ui.KeyA, 1)
	key(ui.Cmd, ui.KeyC, 1)
	if tt.Clipboard() != "eth/usdt" {
		t.Fatalf("copied %q", tt.Clipboard())
	}
	key(0, ui.KeyRight, 1)
	check("Right ends the selection", "eth/usdt", 8, 8)
	key(0, ui.KeyBackspace, 4)
	check("Backspace", "eth/", 4, 4)
	key(ui.Cmd, ui.KeyZ, 1) // the four, one step
	check("Undo", "eth/usdt", 8, 8)
	key(ui.Cmd, ui.KeyZ, 1)
	check("Undo again", "btc/usdt", 0, 4)
	key(ui.Cmd|ui.Shift, ui.KeyZ, 1)
	check("Redo", "eth/usdt", 8, 8)
	key(0, ui.KeyEnd, 1)
	tt.Type(" x")
	key(wordMod, ui.KeyBackspace, 1)
	check("a word deleted", "eth/usdt ", 9, 9)
	key(wordMod, ui.KeyLeft, 1)
	check("a word back", "eth/usdt ", 0, 0)
	key(wordMod, ui.KeyDelete, 1)
	check("a word forward deleted", " ", 0, 0)
	tt.SetClipboard("NVDA\n")
	key(ui.Cmd, ui.KeyV, 1)
	check("pasted", "NVDA  ", 5, 5)
	if a.searchQuery() != "NVDA" || !tt.HasText(" 英伟达") {
		t.Fatalf("searching %q", a.searchQuery())
	}
	key(ui.Cmd, ui.KeyX, 1) // nothing selected: nothing cut
	check("Cut, nothing selected", "NVDA  ", 5, 5)
	key(0, ui.KeyEscape, 1)
	check("Escape", "", 0, 0)
	tt.Type("pepe")
	tt.Frame()
	key(0, ui.KeyEnter, 1)
	tt.Frame()
	if a.symbol != "PEPEUSDT" || a.query != "" {
		t.Fatalf("Enter: showing %s, query %q", a.symbol, a.query)
	}
}

// A click puts the caret where it is, Shift+click and a drag select, a
// double click selects all; the app setting the query puts it at the end.
func TestSearchBoxPointer(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	click(t, tt, searchLabel)
	tt.Type("abcdef")
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find(searchLabel)
	if !ok || len(a.qb.xs) != 7 {
		t.Fatalf("box %v %v, %d places", r, ok, len(a.qb.xs))
	}
	x := func(i int) float32 { return r.X + a.qb.xs[i] - a.qb.scroll + 0.5 }
	y := r.Y + r.H/2
	tt.ClickAt(x(2), y)
	tt.Frame()
	if a.qb.caret != 2 || a.qb.anchor != 2 {
		t.Fatalf("click: caret %d anchor %d", a.qb.caret, a.qb.anchor)
	}
	tt.ClickAtWith(ui.Shift, x(5), y)
	tt.Frame()
	if a.qb.caret != 5 || a.qb.anchor != 2 {
		t.Fatalf("Shift+click: caret %d anchor %d", a.qb.caret, a.qb.anchor)
	}
	tt.Press(x(1), y)
	tt.Move(x(3), y)
	tt.Move(x(4), y)
	tt.Release(x(4), y)
	tt.Frame()
	if a.qb.caret != 4 || a.qb.anchor != 1 {
		t.Fatalf("drag: caret %d anchor %d", a.qb.caret, a.qb.anchor)
	}
	a.searchInput(ui.InputEvent{Kind: ui.InputPointerDown, Clicks: 2})
	a.searchInput(ui.InputEvent{Kind: ui.InputPointerUp})
	if a.qb.caret != 6 || a.qb.anchor != 0 {
		t.Fatalf("double click: caret %d anchor %d", a.qb.caret, a.qb.anchor)
	}
	a.query = "nvda"
	tt.Frame()
	if a.qb.caret != 4 || a.qb.anchor != 4 || len(a.qb.undo) != 0 {
		t.Fatalf("set by the app: caret %d anchor %d, %d to undo", a.qb.caret, a.qb.anchor, len(a.qb.undo))
	}
	// A long query scrolls to keep the caret in sight.
	tt.Type(strings.Repeat("x", 80))
	tt.Frame()
	tt.Frame()
	if a.qb.scroll <= 0 || a.qb.xs[a.qb.caret]-a.qb.scroll > r.W {
		t.Fatalf("scrolled %v, caret at %v of %v", a.qb.scroll, a.qb.xs[a.qb.caret], r.W)
	}
}

// The keys the box edits with are its own while it has the focus: they
// leave the selected drawing alone, and work on it again once it has not.
func TestSearchLeavesDrawingKeys(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	if a.sel == 0 || len(drawings(a)) != 1 {
		t.Fatal("no line selected")
	}
	click(t, tt, searchLabel)
	tt.Type("bt")
	tt.Frame()
	press(tt, 0, ui.KeyBackspace)
	press(tt, 0, ui.KeyDelete)
	press(tt, ui.Cmd, ui.KeyC)
	if len(drawings(a)) != 1 || a.sel == 0 || a.clip != nil || a.query != "b" {
		t.Fatalf("keys in the box: %d drawings, selected %d, copied %v, query %q", len(drawings(a)), a.sel, a.clip != nil, a.query)
	}
	press(tt, 0, ui.KeyEscape)
	if a.query != "" || a.sel == 0 {
		t.Fatalf("Escape: query %q, selected %d", a.query, a.sel)
	}
	click(t, tt, "自选")
	press(tt, 0, ui.KeyDelete)
	if len(drawings(a)) != 0 {
		t.Fatal("Delete outside the box left the line")
	}
}

// A query in Chinese asks Eastmoney a moment after the typing stops, once
// a query; what it finds shows with the Chinese name. Letters are not
// asked, and a query whose answer did not come is asked again.
func TestSearchAsksEastmoney(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch q := r.URL.Query().Get("input"); {
		case strings.Contains(r.URL.Path, "/api/suggest/get"):
			mu.Lock()
			asked = append(asked, q)
			mu.Unlock()
			if q == "坏了" {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `{"QuotationCodeTable":{"Data":[{"Code":"ZTST","Name":"测试科技","PinYin":"CSKJ","MktNum":"105","SecurityTypeName":"美股"},{"Code":"600000","Name":"浦发银行","MktNum":"1"}],"Status":0}}`)
		case strings.Contains(r.URL.Path, "/search"):
			fmt.Fprint(w, `{"quotes":[]}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	asks := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
	queue := make(chan func(), 100)
	a := testApp()
	a.east = market.Eastmoney{HTTP: srv.Client(), Base: srv.URL}
	a.yahoo = market.Yahoo{HTTP: srv.Client(), Base: srv.URL}
	a.usFailed = time.Now() // no Nasdaq list to look through
	a.spawn = func(f func()) { go f() }
	a.post = func(f func()) { queue <- f }
	tt := newTester(a, true)
	until := func(what string, done func() bool) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for !done() {
			select {
			case f := <-queue:
				f()
			case <-deadline:
				t.Fatalf("waiting for %s", what)
			}
		}
	}
	wait := func(d time.Duration) {
		for end := time.After(d); ; {
			select {
			case f := <-queue:
				f()
				continue
			case <-end:
			}
			return
		}
	}
	a.query = "测试"
	tt.Frame()
	a.query = "测试科技"
	tt.Frame()
	if !tt.HasText("正在搜索「测试科技」…") {
		t.Fatal("no word of the search")
	}
	until("an answer", func() bool { return a.zhFound["测试科技"] != nil })
	wait(300 * time.Millisecond)
	tt.Frame()
	tt.Frame()
	if got := asks(); !slices.Equal(got, []string{"测试科技"}) {
		t.Fatalf("asked %q", got)
	}
	if hits := a.findings("测试科技", 5); len(hits) != 1 || hits[0].Symbol != "US:ZTST" || !tt.HasText(" 测试科技") || a.after("US:ZTST") != " 测试科技" {
		t.Fatalf("found %v, shown %v, %q", hits, tt.HasText(" 测试科技"), a.after("US:ZTST"))
	}
	a.query = "ztst"
	tt.Frame()
	wait(300 * time.Millisecond)
	a.query = "测试科技"
	tt.Frame()
	wait(300 * time.Millisecond)
	if got := asks(); len(got) != 1 {
		t.Fatalf("asked again: %q", got)
	}
	a.query = "坏了"
	tt.Frame()
	until("the failure", func() bool { return len(asks()) == 2 && a.zhAsking == "" })
	if a.zhFound["坏了"] != nil {
		t.Fatal("kept a failure")
	}
	a.query = "坏"
	tt.Frame()
	wait(200 * time.Millisecond)
	a.query = "坏了"
	tt.Frame()
	until("asking again", func() bool { return len(asks()) == 4 && a.zhAsking == "" })
	if got := asks(); got[3] != "坏了" {
		t.Fatalf("asked %q", got)
	}
}
