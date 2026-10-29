package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// layoutApp is testApp in layout id, every chart with fake bars: BTC's
// hours, the others the markets and the bars given.
func layoutApp(id string, charts ...[2]string) *application {
	a := testApp()
	a.setLayout(id)
	for i, ch := range charts {
		a.on(a.cells[i+1], func() { a.open(ch[0], ch[1]) })
	}
	for _, c := range a.cells[:a.shownCells()] {
		iv, _ := intervalOf(c.interval)
		c.loading, c.bars, c.atStart, c.stream = false, fakeBars(600, iv.ms), true, live
	}
	return a
}

// TradingView's layouts, all 41 and the one chart: each shows its charts
// once, numbered from 0, and is told apart by its name.
func TestLayoutKinds(t *testing.T) {
	seen, count := map[string]bool{}, 0
	for n, kinds := range layoutKinds {
		for _, k := range kinds {
			if seen[k.id] {
				t.Errorf("%s twice", k.id)
			}
			seen[k.id] = true
			count++
			root, shows := layoutOf(k.id)
			if shows != n+1 {
				t.Errorf("%s shows %d charts, want %d", k.id, shows, n+1)
			}
			var charts []int
			var nodes []int
			var walk func(sp *split)
			walk = func(sp *split) {
				if len(sp.kids) == 0 {
					charts = append(charts, sp.chart)
					return
				}
				nodes = append(nodes, sp.node)
				if len(sp.kids) < 2 {
					t.Errorf("%s: a split of %d", k.id, len(sp.kids))
				}
				for _, kid := range sp.kids {
					if len(kid.kids) > 0 && kid.across == sp.across {
						t.Errorf("%s: a split inside one the same way", k.id)
					}
					walk(kid)
				}
			}
			walk(root)
			slices.Sort(charts)
			for i, c := range charts {
				if c != i {
					t.Errorf("%s has charts %v", k.id, charts)
					break
				}
			}
			for i, nd := range nodes {
				if nd != i {
					t.Errorf("%s numbers its splits %v", k.id, nodes)
					break
				}
			}
		}
	}
	if count != 42 {
		t.Errorf("%d layouts, want TradingView's 41 and one chart", count)
	}
	if _, n := layoutOf("nonsense"); n != 1 {
		t.Error("an unknown layout is not one chart")
	}
	// As TradingView's diagrams have them.
	for id, want := range map[string]string{"2-1": "r(c(0,2),1)", "4": "r(c(0,2),c(1,3))", "4s-l": "c(r(1,2,3),0)", "6c": "r(c(0,1),c(2,3),c(4,5))", "8": "r(c(0,2,4,6),c(1,3,5,7))"} {
		root, _ := layoutOf(id)
		if got := treeText(root); got != want {
			t.Errorf("%s is %s, want %s", id, got, want)
		}
	}
}

func treeText(sp *split) string {
	if len(sp.kids) == 0 {
		return string(rune('0' + sp.chart))
	}
	s := "r("
	if sp.across {
		s = "c("
	}
	for i, k := range sp.kids {
		if i > 0 {
			s += ","
		}
		s += treeText(k)
	}
	return s + ")"
}

// New charts show the active one's market, bars and indicators, theirs to
// change; a layout with fewer hides the rest, which stop asking for bars
// and come back as they were; the settings keep them all.
func TestSetLayout(t *testing.T) {
	a := withIndicators("ma")
	a.file = filepath.Join(t.TempDir(), "settings.json")
	a.setLayout("4")
	if len(a.cells) != 4 || a.shownCells() != 4 {
		t.Fatalf("%d charts, %d shown", len(a.cells), a.shownCells())
	}
	for _, c := range a.cells[1:] {
		if c.symbol != "BTCUSDT" || c.interval != "1h" || len(c.indicators) != 1 {
			t.Fatalf("a new chart has %s %s and %d indicators", c.symbol, c.interval, len(c.indicators))
		}
	}
	a.cells[1].indicators[0].Params[0] = 99
	if a.cells[0].indicators[0].Params[0] == 99 {
		t.Fatal("the charts share their indicators")
	}
	if f := a.feeds[a.feedKey("BTCUSDT", "1h")]; len(a.feeds) != 1 || len(f.cells) != 4 {
		t.Fatalf("%d feeds for four charts of BTC's hours", len(a.feeds))
	}
	a.on(a.cells[2], func() { a.open("ETHUSDT", "5m") })
	a.setActive(3)
	a.on(a.activeCell(), func() { a.setLayout("2h") }) // as the menu does, once the frame is built
	if a.cell != a.cells[0] {
		t.Fatal("the hidden chart is still the one acted on")
	}
	if a.shownCells() != 2 || a.active != 0 || a.cells[2].share != nil || a.cells[3].share != nil {
		t.Fatalf("hidden charts: active %d, feeds %v %v", a.active, a.cells[2].share, a.cells[3].share)
	}
	if len(a.feeds) != 1 || len(a.feeds[a.feedKey("BTCUSDT", "1h")].cells) != 2 {
		t.Fatalf("feeds %v after hiding the charts", slices.Collect(func(yield func(string) bool) {
			for k := range a.feeds {
				yield(k)
			}
		}))
	}
	a.setLayout("4")
	if c := a.cells[2]; c.symbol != "ETHUSDT" || c.interval != "5m" || c.share == nil || len(a.cells) != 4 {
		t.Fatalf("the third chart came back as %s %s", c.symbol, c.interval)
	}
	a.setActive(2)
	s := loadSettings(a.file)
	if s.Layout != "4" || len(s.Charts) != 4 || s.Charts[2].Symbol != "ETHUSDT" || s.Active != 2 || s.Charts[1].Indicators[0].Params[0] != 99 {
		t.Fatalf("read back %s, %d charts, active %d: %+v", s.Layout, len(s.Charts), s.Active, s.Charts)
	}
	data, _ := os.ReadFile(a.file)
	var old struct{ Symbol, Interval string }
	if json.Unmarshal(data, &old); old.Symbol != "ETHUSDT" || old.Interval != "5m" {
		t.Fatalf("an older version reads %+v", old)
	}
	a.setLayout("s")
	if a.set.Layout != "" || a.shownCells() != 1 || a.active != 0 {
		t.Fatalf("one chart: %q, %d shown, active %d", a.set.Layout, a.shownCells(), a.active)
	}
}

// The charts on one market in one interval share its feed: one joining
// takes the bars the other has, and what comes after; the feed stops with
// the last to leave, and one whose first page did not come is let go of.
func TestSharedFeed(t *testing.T) {
	a := testApp()
	a.setLayout("2h")
	one, two := a.cells[0], a.cells[1]
	if two.loading || len(two.bars) != len(one.bars) || &two.bars[0] == &one.bars[0] {
		t.Fatal("the second chart did not take the first one's bars, its own copy")
	}
	f := one.share
	stopped := false
	f.stop = func() { stopped = true }
	a.on(one, func() { a.open("ETHUSDT", "1h") })
	if stopped || two.share != f || len(f.cells) != 1 {
		t.Fatal("the feed stopped with a chart still on it")
	}
	a.on(two, func() { a.open("SOLUSDT", "1h") })
	if !stopped || a.feeds[f.key] != nil {
		t.Fatal("the feed went on with no chart on it")
	}

	// A feed brings its bars to every chart on it.
	b := testApp()
	b.setLayout("2h")
	posts := make(chan func(), 8)
	b.post = func(fn func()) { posts <- fn }
	for _, c := range b.cells[:2] {
		b.on(c, func() { b.open("ETHUSDT", "5m") })
	}
	g := b.cells[0].share
	if b.cells[1].share != g || !b.cells[1].loading {
		t.Fatal("the second chart did not wait on the first one's feed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bars := fakeBars(10, 300_000)
	go b.feed(ctx, g, fakeFeed{bars: bars}, "ETHUSDT", "5m", false)
	(<-posts)() // the first page
	(<-posts)() // the stream's bar
	for i, c := range b.cells[:2] {
		if c.loading || len(c.bars) != 10 || c.stream != live || c.bars[9].V != 1 {
			t.Fatalf("chart %d: %d bars, stream %d, the newest %+v", i, len(c.bars), c.stream, c.bars[len(c.bars)-1])
		}
	}
	cancel()

	// A first page that did not come: the next to open starts over.
	h := testApp()
	h.post = func(fn func()) { fn() }
	h.on(h.cell, func() { h.open("SOLUSDT", "1h") })
	fs := h.share
	h.feed(context.Background(), fs, fakeFeed{err: errors.New("no")}, "SOLUSDT", "1h", false)
	if h.err == "" || h.feeds[fs.key] != nil {
		t.Fatalf("err %q, the feed kept", h.err)
	}
	h.setLayout("2h")
	if h.cells[1].share == fs || h.cells[1].err != "" {
		t.Fatal("a new chart took the feed whose first page did not come")
	}
}

// fakeFeed answers Klines with bars, or err, and streams the newest bar
// once more, its volume 1.
type fakeFeed struct {
	market.Feed
	bars []market.Bar
	err  error
}

func (f fakeFeed) Klines(context.Context, string, string, int64, int) ([]market.Bar, error) {
	return slices.Clone(f.bars), f.err
}

func (f fakeFeed) Stream(ctx context.Context, _, _ string, fn func(market.Bar, bool, int64)) error {
	b := f.bars[len(f.bars)-1]
	b.V = 1
	fn(b, false, 0)
	<-ctx.Done()
	return ctx.Err()
}

// The pointer going down on a chart makes it the one the top bar acts on;
// on another place it does not.
func TestPressActivates(t *testing.T) {
	a := layoutApp("2h", [2]string{"ETHUSDT", "1h"})
	tt := newTester(a, true)
	r := a.cells[1].bounds
	tt.Press(r.X+100, r.Y+200)
	tt.Release(r.X+100, r.Y+200)
	tt.Frame()
	if a.active != 1 || a.cell != a.cells[1] {
		t.Fatalf("active %d after a click on the second chart", a.active)
	}
	click(t, tt, "5分")
	if a.cells[1].interval != "5m" || a.cells[0].interval != "1h" {
		t.Fatalf("the intervals: %s %s", a.cells[0].interval, a.cells[1].interval)
	}
	click(t, tt, "SOL/USDT") // the watchlist
	if a.cells[1].symbol != "SOLUSDT" || a.cells[0].symbol != "BTCUSDT" || a.active != 1 {
		t.Fatalf("the markets: %s %s, active %d", a.cells[0].symbol, a.cells[1].symbol, a.active)
	}
	r = a.cells[0].bounds
	tt.Press(r.X+100, r.Y+200)
	tt.Release(r.X+100, r.Y+200)
	tt.Frame()
	if a.active != 0 {
		t.Fatal("the first chart did not take a click")
	}
}

// Synced, the charts follow the active one's market or bars; not, they
// keep theirs. Switched on, they take the active one's at once.
func TestSyncMarketsAndBars(t *testing.T) {
	a := layoutApp("3h", [2]string{"ETHUSDT", "5m"}, [2]string{"SOLUSDT", "1d"})
	a.pick("XRPUSDT")
	if got := symbols(a); !slices.Equal(got, []string{"XRPUSDT 1h", "ETHUSDT 5m", "SOLUSDT 1d"}) {
		t.Fatalf("unsynced: %v", got)
	}
	tt := newTester(a, true)
	a.layoutMenu = true
	tt.Frame()
	click(t, tt, "品种")
	if !a.set.Sync.Symbol {
		t.Fatal("not synced")
	}
	if got := symbols(a); !slices.Equal(got, []string{"XRPUSDT 1h", "XRPUSDT 5m", "XRPUSDT 1d"}) {
		t.Fatalf("synced on: %v", got)
	}
	a.layoutMenu = true
	tt.Frame()
	click(t, tt, "周期")
	if got := symbols(a); !slices.Equal(got, []string{"XRPUSDT 1h", "XRPUSDT 1h", "XRPUSDT 1h"}) {
		t.Fatalf("bars synced on: %v", got)
	}
	a.setActive(2)
	a.pick("DOGEUSDT")
	a.openSynced(a.symbol, "15m")
	if got := symbols(a); !slices.Equal(got, []string{"DOGEUSDT 15m", "DOGEUSDT 15m", "DOGEUSDT 15m"}) {
		t.Fatalf("synced: %v", got)
	}
	if len(a.feeds) != 1 {
		t.Fatalf("%d feeds for three charts of one market in one interval", len(a.feeds))
	}
}

func symbols(a *application) (out []string) {
	for _, c := range a.cells[:a.shownCells()] {
		out = append(out, c.symbol+" "+c.interval)
	}
	return out
}

// The crosshair on one chart shows on the others at its time, the price
// too on one of the same market, unless the layout does not sync it.
func TestSyncCrosshair(t *testing.T) {
	a := layoutApp("3v", [2]string{"BTCUSDT", "15m"}, [2]string{"ETHUSDT", "1d"})
	tt := newTester(a, true)
	r := a.cells[0].bounds
	tt.Move(r.X+a.cells[0].plotW-80, r.Y+60) // a few hours back: in sight on the 15 minutes
	tt.Frame()
	tt.Frame()
	if a.cross.from != a.cells[0] || math.IsNaN(a.cross.price) {
		t.Fatalf("the crosshair is on %v at %v", a.cross.from, a.cross.price)
	}
	for _, c := range a.cells[1:3] {
		a.on(c, func() {
			i, ok := a.syncedBar()
			if !ok || a.bars[i].T > a.cross.t || a.bars[i].T+a.intervalMs() <= a.cross.t {
				t.Fatalf("%s %s: bar %d, %v", a.symbol, a.interval, i, ok)
			}
			if got := a.legendBar(0, 0, false); got != i || a.interval == "15m" && i == len(a.bars)-1 {
				t.Fatalf("%s %s: the legend reads bar %d, the crosshair's is %d", a.symbol, a.interval, got, i)
			}
		})
	}
	// Drawn: the line down the 15 minutes and, on BTC, across at the price,
	// with its label on the axis.
	img := tt.Image()
	c := a.cells[1]
	var lx float32
	a.on(c, func() {
		i, _ := a.syncedBar()
		v := a.view
		v.clamp(a.plotW, len(a.bars))
		lx = c.bounds.X + v.x(i, a.plotW, len(a.bars))
	})
	cross := paletteFor(true).cross
	found := false
	for y := int(c.bounds.Y) + 20; y < int(c.bounds.Y+c.plotH) && !found; y++ {
		for dx := -1; dx <= 1; dx++ {
			if px := img.At(int(lx)+dx, y); colorNear(px, cross) {
				found = true
			}
		}
	}
	if !found {
		t.Error("no crosshair down the second chart")
	}
	a.set.Sync.Crosshair = false
	a.on(a.cells[1], func() {
		if _, ok := a.syncedBar(); ok {
			t.Error("the crosshair synced unsynced")
		}
	})
	tt.Move(r.X+r.W+100, r.Y-20) // off the charts
	tt.Frame()
	if a.cross.from != nil {
		t.Error("a crosshair with the pointer off the charts")
	}
}

func colorNear(c interface{ RGBA() (r, g, b, a uint32) }, want ui.Color) bool {
	r, g, b, _ := c.RGBA()
	d := func(x uint32, y uint8) bool { return math.Abs(float64(x>>8)-float64(y)) < 24 }
	return d(r, want.R) && d(g, want.G) && d(b, want.B)
}

// Synced, the time follows a chart dragged: one in bars as long to the
// same place and zoom, one in others to the time at the right edge; back
// at the newest bar they all are.
func TestSyncTime(t *testing.T) {
	a := layoutApp("3h", [2]string{"BTCUSDT", "1h"}, [2]string{"ETHUSDT", "5m"})
	a.set.Sync.Time = true
	tt := newTester(a, true)
	one, same, five := a.cells[0], a.cells[1], a.cells[2]
	r := one.bounds
	tt.Scroll(r.X+100, r.Y+100, 0, notch) // zoom out
	tt.Press(r.X+100, r.Y+100)
	tt.Move(r.X+180, r.Y+100)
	tt.Move(r.X+260, r.Y+100)
	tt.Release(r.X+260, r.Y+100)
	tt.Frame()
	if one.view.offset > -5 {
		t.Fatalf("not dragged: offset %v", one.view.offset)
	}
	if same.view != one.view {
		t.Fatalf("the chart of the same bars at %+v, the dragged one at %+v", same.view, one.view)
	}
	edge := timeAt(one.bars, one.view.right(len(one.bars)), 3_600_000)
	at := timeAt(five.bars, five.view.right(len(five.bars)), 300_000)
	if d := at - edge; d < -300_000 || d > 300_000 {
		t.Fatalf("the 5 minutes' right edge %v from the hours'", time.Duration(d)*time.Millisecond)
	}
	if five.view.spacing != defaultView().spacing {
		t.Fatal("the 5 minutes took the hours' zoom")
	}
	a.on(one, func() { a.resetView() })
	if same.view.offset != a.home() || five.view.offset != a.home() {
		t.Fatalf("back at the newest bar: %v %v", same.view.offset, five.view.offset)
	}
	a.set.Sync.Time = false
	tt.Press(r.X+100, r.Y+100)
	tt.Move(r.X+200, r.Y+100)
	tt.Release(r.X+200, r.Y+100)
	tt.Frame()
	if same.view.offset != a.home() {
		t.Fatal("the time synced unsynced")
	}
}

// The line between two charts drags, each keeping minCellW, and the
// layout keeps where it is.
func TestSplitterDrags(t *testing.T) {
	a := layoutApp("2h", [2]string{"ETHUSDT", "1h"})
	a.file = filepath.Join(t.TempDir(), "settings.json")
	tt := newTester(a, true)
	l, r := a.cells[0].bounds, a.cells[1].bounds
	x, y := l.X+l.W+splitGap/2, l.Y+200
	tt.Press(x, y)
	tt.Move(x+50, y)
	tt.Frame()
	tt.Move(x+100, y)
	tt.Frame()
	tt.Release(x+100, y)
	tt.Frame()
	tt.Frame()
	if got := a.cells[0].bounds.W; math.Abs(float64(got-l.W-100)) > 1 {
		t.Fatalf("the left chart %v wide after dragging 100 more, was %v", got, l.W)
	}
	if got := a.cells[1].bounds.W; math.Abs(float64(got-r.W+100)) > 1 {
		t.Fatalf("the right chart %v wide, was %v", got, r.W)
	}
	if s := loadSettings(a.file); len(s.Splits["2h"]) != 1 || s.Splits["2h"][0][0] <= s.Splits["2h"][0][1] {
		t.Fatalf("kept %v", s.Splits)
	}
	x = a.cells[0].bounds.X + a.cells[0].bounds.W + splitGap/2
	tt.Press(x, y)
	tt.Move(x+1000, y)
	tt.Frame()
	tt.Release(x+1000, y)
	tt.Frame()
	tt.Frame()
	if got := a.cells[1].bounds.W; math.Abs(float64(got-minCellW)) > 1 {
		t.Fatalf("the right chart %v wide dragged over, want %v", got, minCellW)
	}
}

// A chart maximizes and back, the layout as it was.
func TestMaximize(t *testing.T) {
	a := layoutApp("4", [2]string{"ETHUSDT", "1h"}, [2]string{"SOLUSDT", "1h"}, [2]string{"XRPUSDT", "1h"})
	tt := newTester(a, true)
	r := a.cells[2].bounds
	tt.Move(r.X+100, r.Y+100)
	tt.Frame()
	tt.Frame()
	click(t, tt, "最大化这个图表")
	if a.maxed != 2 || a.active != 2 {
		t.Fatalf("maximized %d, active %d", a.maxed, a.active)
	}
	if got := a.cells[2].bounds; got.W < 2*r.W || got.H < 2*r.H-10 {
		t.Fatalf("maximized it is %+v, was %+v", got, r)
	}
	click(t, tt, "还原布局")
	if a.maxed != -1 || a.cells[2].bounds != r {
		t.Fatalf("restored %d: %+v", a.maxed, a.cells[2].bounds)
	}
}

// Each chart has indicators of its own: added on the active one.
func TestIndicatorsPerChart(t *testing.T) {
	a := layoutApp("2v", [2]string{"ETHUSDT", "1h"})
	a.setActive(1)
	k, _ := kindOf("rsi")
	a.addIndicator(k)
	if len(a.cells[1].indicators) != 1 || len(a.cells[0].indicators) != 0 {
		t.Fatalf("indicators %d and %d", len(a.cells[0].indicators), len(a.cells[1].indicators))
	}
	tt := newTester(a, true)
	shot(t, tt, "layout-2v-rsi")
	if _, panes := a.on2(a.cells[1]); len(panes) != 1 {
		t.Fatal("the second chart has no RSI pane")
	}
	if _, panes := a.on2(a.cells[0]); len(panes) != 0 {
		t.Fatal("the first chart has a pane")
	}
}

// on2 is the panes of c's chart.
func (a *application) on2(c *cell) (mainH float32, panes []paneBox) {
	a.on(c, func() { mainH, panes = a.layout(a.boxH - timeH) })
	return mainH, panes
}

// The trading hours switched reload every future on the charts, each with
// a feed for those hours, and nothing else.
func TestRTHReopensFutures(t *testing.T) {
	a := layoutApp("3h", [2]string{"CME:NQ", "1h"}, [2]string{"CME:NQ", "1h"})
	gens := []int{a.cells[0].gen, a.cells[1].gen, a.cells[2].gen}
	before := a.cells[1].share
	a.set.FuturesRTH = true
	a.reopenFutures()
	if a.cells[0].gen != gens[0] || a.cells[1].gen == gens[1] || a.cells[2].gen == gens[2] {
		t.Fatal("not the futures reloaded")
	}
	if f := a.cells[1].share; f == before || f != a.cells[2].share || f.key != "CME:NQ 1h rth" {
		t.Fatalf("the futures' feed %q", a.cells[1].share.key)
	}
}

func TestLayoutShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	a := layoutApp("4", [2]string{"ETHUSDT", "5m"}, [2]string{"SOLUSDT", "1d"}, [2]string{"BTCUSDT", "15m"})
	a.infos["ETHUSDT"] = market.Symbol{Name: "ETHUSDT", Base: "ETH", Quote: "USDT", Tick: 0.01}
	tt := newTester(a, true)
	tt.Move(400, 300)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "layout-4")
	a.layoutMenu = true
	tt.Frame()
	tt.Frame()
	shot(t, tt, "layout-menu")
	b := layoutApp("8", [2]string{"ETHUSDT", "5m"}, [2]string{"SOLUSDT", "1d"}, [2]string{"BTCUSDT", "15m"},
		[2]string{"XRPUSDT", "1h"}, [2]string{"DOGEUSDT", "4h"}, [2]string{"ADAUSDT", "1w"}, [2]string{"SUIUSDT", "1m"})
	tt = newTester(b, false)
	tt.Move(300, 200)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "layout-8")
	c := layoutApp("3s", [2]string{"ETHUSDT", "5m"}, [2]string{"SOLUSDT", "1d"})
	tt = newTester(c, true)
	shot(t, tt, "layout-3s")
}
