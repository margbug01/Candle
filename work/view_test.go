package main

import (
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// fakeBars is a random walk of n bars ivMs apart, the newest one open now,
// the same every run.
func fakeBars(n int, ivMs int64) []market.Bar {
	r := rand.New(rand.NewPCG(1, 2))
	end := time.Now().UnixMilli() / ivMs * ivMs
	bars := make([]market.Bar, n)
	price := 80000.0
	for i := range bars {
		o := price
		c := o * (1 + (r.Float64()-0.5)*0.012)
		bars[i] = market.Bar{T: end - int64(n-1-i)*ivMs, O: o, C: c,
			H: max(o, c) * (1 + r.Float64()*0.004), L: min(o, c) * (1 - r.Float64()*0.004), V: 200 + r.Float64()*1800}
		price = c
	}
	return bars
}

func init() { applyTheme = func(string) {} }

// testApp shows fake hourly BTC bars without touching the network.
func testApp() *application {
	a := newApplication(defaultSettings(), "", market.Binance{}, func(f func()) { f() })
	a.spawn = func(func()) {}
	a.open("BTCUSDT", "1h")
	a.loading, a.bars, a.atStart, a.stream = false, fakeBars(600, 3_600_000), true, live
	for i, s := range a.set.Watch {
		base, _ := strings.CutSuffix(s, "USDT")
		a.infos[s] = market.Symbol{Name: s, Base: base, Quote: "USDT", Tick: 0.0001}
		a.quotes[s] = market.Ticker{Symbol: s, Open: 1, Last: 1 + float64(i%3-1)*0.021, QuoteVolume: 1e6}
	}
	a.infos["BTCUSDT"] = market.Symbol{Name: "BTCUSDT", Base: "BTC", Quote: "USDT", Tick: 0.01}
	a.quotes["BTCUSDT"] = market.Ticker{Symbol: "BTCUSDT", Open: 79000, Last: a.bars[599].C, QuoteVolume: 2e9}
	a.all = []market.Ticker{
		{Symbol: "PEPEUSDT", Open: 0.000004, Last: 0.00000412, QuoteVolume: 5e7},
		{Symbol: "PEPEFDUSD", Open: 0.000004, Last: 0.00000411, QuoteVolume: 1e6},
		{Symbol: "BTCUSDT", Open: 79000, Last: 80000, QuoteVolume: 2e9},
	}
	a.allState = 2
	return a
}

// chartLeft is where the chart starts in a test window 1320 wide: right of
// the drawing tools.
func chartLeft(a *application) float32 { return 1320 - watchW - axisW - a.plotW }

func newTester(a *application, dark bool) *ui.Tester {
	tt := ui.NewTester(a.render, 1320, 820)
	tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Right: 138})
	tt.SetDark(dark)
	tt.Frame()
	tt.Frame()
	return tt
}

func click(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	if err := tt.Click(s); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
}

// shot saves what the tester shows, for a person to look at, when CANDLE_SHOTS
// names a folder.
func shot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("CANDLE_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestDragPans(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	before := a.view.offset
	tt.Press(500, 300)
	tt.Move(560, 300)
	tt.Move(620, 300)
	tt.Release(620, 300)
	tt.Frame()
	if want := before - 120/a.view.spacing; !near(a.view.offset, want) {
		t.Fatalf("offset %v after dragging 120 DIPs, want %v", a.view.offset, want)
	}
	if a.drag != dragNone {
		t.Fatal("still dragging after the release")
	}
}

func TestWheelZoomsAroundThePointer(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	n := len(a.bars)
	left := chartLeft(a)
	at := a.view.index(400, a.plotW, n)
	tt.Scroll(left+400, 300, 0, notch)
	tt.Frame()
	if want := defaultView().spacing / zoomPerNotch; !near(a.view.spacing, want) {
		t.Fatalf("spacing %v after a notch down, want %v", a.view.spacing, want)
	}
	if got := a.view.index(400, a.plotW, n); !near(got, at) {
		t.Fatalf("the bar under the pointer moved from %v to %v", at, got)
	}
	spacing := a.view.spacing
	tt.Scroll(left+a.plotW+20, 300, 0, notch)
	tt.Frame()
	if a.view.spacing != spacing {
		t.Fatal("the wheel over the price axis zoomed")
	}
}

func TestLatestButton(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	if tt.HasText("回到最新") {
		t.Fatal("the button shows at the newest bar")
	}
	a.view.offset = -200
	tt.Frame()
	tt.Frame()
	click(t, tt, "回到最新")
	if a.view.offset != defaultView().offset || a.drag != dragNone {
		t.Fatalf("offset %v drag %v after the click", a.view.offset, a.drag)
	}
}

// The price axis scales like TradingView's: the wheel and a drag on it, the
// chart then dragging the prices too, until a double click or 自动.
func TestPriceAxis(t *testing.T) {
	a := testApp()
	a.set.Chart.Buttons = "always"
	tt := newTester(a, true)
	top := float32(820) - timeH - a.plotH // where the chart starts in the window
	ax, y := chartLeft(a)+a.plotW+30, float32(300)
	if a.scale.manual {
		t.Fatal("the prices start out by hand")
	}
	shown := a.shown
	at := shown.at(y-top, a.axis)
	tt.Scroll(ax, y, 0, notch)
	tt.Frame()
	rel := func(got, want float64) bool { return math.Abs(got-want) <= math.Abs(want)*1e-6 }
	if !a.scale.manual || !rel(a.scale.hi-a.scale.lo, (shown.hi-shown.lo)*zoomPerNotch) {
		t.Fatalf("after a notch down on the axis: %+v, from %+v", a.scale, shown)
	}
	if got := a.scale.at(y-top, a.axis); !rel(got, at) {
		t.Fatalf("the price under the pointer moved from %v to %v", at, got)
	}
	span := a.scale.hi - a.scale.lo
	tt.Press(ax, 300)
	tt.Move(ax, 360)
	tt.Release(ax, 360)
	tt.Frame()
	if got := a.scale.hi - a.scale.lo; !rel(got, span*math.Exp(60.0/axisDrag)) || a.drag != dragNone {
		t.Fatalf("dragging the axis 60 DIPs down: span %v, want %v", got, span*math.Exp(60.0/axisDrag))
	}
	lo := a.scale.lo
	tt.Press(500, 300)
	tt.Move(500, 340)
	tt.Release(500, 340)
	tt.Frame()
	if a.scale.lo <= lo {
		t.Fatal("dragging the chart down with the prices set by hand left them")
	}
	click(t, tt, "自动")
	if a.scale.manual {
		t.Fatal("自动 left the prices by hand")
	}
	click(t, tt, "自动")
	if !a.scale.manual {
		t.Fatal("自动 did not hold the prices")
	}
	a.view.offset = -40
	a.chartInput(ui.InputEvent{Kind: ui.InputPointerDown, X: ax, Y: 100, Clicks: 2})
	if a.scale.manual || a.view.offset != -40 {
		t.Fatalf("a double click on the axis: manual %v, offset %v", a.scale.manual, a.view.offset)
	}
	tt.Scroll(ax, y, 0, notch)
	tt.Frame()
	click(t, tt, "4小时")
	if a.scale.manual {
		t.Fatal("another bar size kept the prices set by hand")
	}
}

func TestIntervalAndWatchlist(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	click(t, tt, "4小时")
	if a.interval != "4h" || a.symbol != "BTCUSDT" || len(a.bars) != 0 {
		t.Fatalf("after 4小时: %s %s, %d bars", a.symbol, a.interval, len(a.bars))
	}
	click(t, tt, "ETH/USDT")
	if a.symbol != "ETHUSDT" || a.interval != "4h" {
		t.Fatalf("after ETH: %s %s", a.symbol, a.interval)
	}
	r, ok := tt.Find("SOL/USDT")
	if !ok {
		t.Fatal("no SOL row")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
	click(t, tt, "移出自选")
	if strings.Contains(strings.Join(a.set.Watch, ","), "SOLUSDT") || a.symbol != "ETHUSDT" {
		t.Fatalf("after removing SOL: watch %v, showing %s", a.set.Watch, a.symbol)
	}
}

func TestSearchPicks(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	tt.ClickAt(1320-watchW/2, titleBarHeight+28)
	tt.Type("pepe")
	tt.Frame()
	tt.Frame()
	shot(t, tt, "search")
	click(t, tt, "PEPE/USDT")
	if a.symbol != "PEPEUSDT" || a.set.Watch[0] != "PEPEUSDT" || a.query != "" {
		t.Fatalf("after picking PEPE: showing %s, watch %v, query %q", a.symbol, a.set.Watch, a.query)
	}
}

func TestShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	for _, dark := range []bool{true, false} {
		name := map[bool]string{true: "dark", false: "light"}[dark]
		a := testApp()
		tt := newTester(a, dark)
		shot(t, tt, name)
		tt.Move(640, 420)
		tt.Frame()
		tt.Frame()
		shot(t, tt, name+"-hover")
	}
	a := testApp()
	a.view.spacing = 2.5
	tt := newTester(a, true)
	shot(t, tt, "dark-zoomed-out")
}
