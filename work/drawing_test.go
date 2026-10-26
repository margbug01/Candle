package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

func TestMapperTimes(t *testing.T) {
	const h = 3_600_000
	bars := fakeBars(10, h)
	m := mapper{bars: bars, iv: h}
	for _, f := range []float64{0, 3, 9, 12, -4} {
		if got := m.idx(m.timeAt(f)); math.Abs(got-f) > 1e-9 {
			t.Errorf("idx(timeAt(%v)) = %v", f, got)
		}
	}
	if got := m.timeAt(12); got != bars[9].T+3*h {
		t.Errorf("three bars past the newest: %d, want %d", got, bars[9].T+3*h)
	}
	// An hourly anchor on a daily chart sits inside its day.
	days := fakeBars(5, 86_400_000)
	md := mapper{bars: days, iv: 86_400_000}
	if got := md.idx(days[2].T + 6*h); math.Abs(got-2.25) > 1e-9 {
		t.Errorf("06:00 into the third day is bar %v, want 2.25", got)
	}
}

func TestSnapMagnet(t *testing.T) {
	bars := []market.Bar{{T: 0, O: 100, H: 110, L: 90, C: 105}, {T: 60_000, O: 105, H: 120, L: 100, C: 115}}
	m := mapper{v: view{spacing: 20, offset: 0}, w: 200, h: 400, bars: bars, iv: 60_000,
		sc: prices{lo: 80, hi: 130}, ax: axis{h: 400, top: 0, bottom: 1}}
	x := m.v.x(1, m.w, 2) + 3
	y := m.y(118.5) // 4 DIPs from the high
	if got := m.snap(x, y, true); got != (point{T: 60_000, P: 120}) {
		t.Fatalf("near the high: %+v", got)
	}
	if got := m.snap(x, y, false); got.T != 60_000 || math.Abs(got.P-118.5) > 1e-4 {
		t.Fatalf("without the magnet: %+v", got)
	}
	if got := m.snap(x, m.y(85), true); math.Abs(got.P-85) > 1e-4 { // float32 DIPs
		t.Fatalf("far from every price the magnet took it to %v", got.P)
	}
}

// win is where x, y of the plot is in a test window.
func win(a *application, x, y float32) (float32, float32) {
	return chartLeft(a) + x, 820 - timeH - a.plotH + y
}

func drawApp(t *testing.T) (*application, *ui.Tester) {
	a := testApp()
	a.set.Magnet = false
	tt := newTester(a, true)
	return a, tt
}

func clickPlot(tt *ui.Tester, a *application, x, y float32) {
	wx, wy := win(a, x, y)
	tt.Move(wx, wy)
	tt.Frame()
	tt.ClickAt(wx, wy)
	tt.Frame()
	tt.Frame()
}

func drawings(a *application) []drawing { return a.set.Drawings[a.symbol] }

func TestDrawTrendLineByClicks(t *testing.T) {
	a, tt := drawApp(t)
	m := a.mapper()
	want := []point{m.snap(300, 200, false), m.snap(500, 120, false)}
	click(t, tt, "趋势线")
	clickPlot(tt, a, 300, 200)
	if a.creating == nil || len(drawings(a)) != 0 {
		t.Fatal("one click finished a trend line")
	}
	clickPlot(tt, a, 500, 120)
	ds := drawings(a)
	if len(ds) != 1 || ds[0].Kind != "trend" || ds[0].Pts[0] != want[0] || ds[0].Pts[1] != want[1] {
		t.Fatalf("drew %+v, want points %+v", ds, want)
	}
	if a.tool != "" || a.creating != nil || a.sel != ds[0].ID {
		t.Fatalf("after drawing: tool %q, creating %v, selected %d", a.tool, a.creating != nil, a.sel)
	}
	if !tt.HasText("删除画线") {
		t.Fatal("no bar for the selected drawing")
	}
	// Beside the bar, across the top of the plot, the pointer reaches the chart.
	clickPlot(tt, a, 20, 20)
	if a.sel != 0 {
		t.Fatal("a click left of the bar did not reach the chart")
	}
	clickPlot(tt, a, 400, 160)
	if a.sel != ds[0].ID {
		t.Fatal("a click on the line did not select it")
	}
	click(t, tt, "删除画线")
	if len(drawings(a)) != 0 {
		t.Fatal("the bar's delete button left the line")
	}
}

func TestDrawRectangleByDragging(t *testing.T) {
	a, tt := drawApp(t)
	m := a.mapper()
	click(t, tt, "矩形")
	x0, y0 := win(a, 200, 150)
	x1, y1 := win(a, 420, 300)
	tt.Press(x0, y0)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
	tt.Frame()
	ds := drawings(a)
	if len(ds) != 1 || ds[0].Pts[0] != m.snap(200, 150, false) || ds[0].Pts[1] != m.snap(420, 300, false) {
		t.Fatalf("dragged a rectangle: %+v", ds)
	}
	if a.view.offset != a.home() {
		t.Fatalf("drawing panned the chart to %v", a.view.offset)
	}
}

func TestSelectMoveAndDelete(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	ds := drawings(a)
	if len(ds) != 1 || ds[0].Kind != "hline" {
		t.Fatalf("one click drew %+v", ds)
	}
	price := ds[0].Pts[0].P
	clickPlot(tt, a, 300, 320)
	if a.sel != 0 {
		t.Fatal("a click beside the line kept it selected")
	}
	clickPlot(tt, a, 600, 202)
	if a.sel != ds[0].ID {
		t.Fatal("a click on the line did not select it")
	}
	x, y := win(a, 600, 201)
	tt.Press(x, y)
	tt.Move(x+10, y+30)
	tt.Move(x+20, y+60)
	tt.Release(x+20, y+60)
	tt.Frame()
	m := a.mapper()
	if got, want := drawings(a)[0].Pts[0].P, m.sc.at(m.y(price)+60, m.ax); math.Abs(got-want) > math.Abs(want)*1e-9 {
		t.Fatalf("dragged 60 DIPs down: price %v, want %v", got, want)
	}
	if a.view.offset != a.home() {
		t.Fatal("dragging the line panned the chart")
	}
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	if len(drawings(a)) != 0 || a.sel != 0 {
		t.Fatalf("Delete left %+v", drawings(a))
	}
}

func TestDragAPoint(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "趋势线")
	clickPlot(tt, a, 300, 200)
	clickPlot(tt, a, 500, 120)
	first := drawings(a)[0].Pts[0]
	x, y := win(a, 500, 120)
	tt.Press(x, y)
	tt.Move(x+40, y+40)
	tt.Move(x+80, y+80)
	tt.Release(x+80, y+80)
	tt.Frame()
	d := drawings(a)[0]
	if d.Pts[0] != first || d.Pts[1] != a.mapper().snap(580, 200, false) {
		t.Fatalf("dragging the end moved %+v", d.Pts)
	}
}

func TestChannelTakesThreePoints(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "平行通道")
	clickPlot(tt, a, 200, 250)
	clickPlot(tt, a, 500, 150)
	if a.creating == nil {
		t.Fatal("two clicks finished a channel")
	}
	clickPlot(tt, a, 350, 300)
	ds := drawings(a)
	if len(ds) != 1 || len(ds[0].Pts) != 3 {
		t.Fatalf("drew %+v", ds)
	}
	// 300 is 100 below where the first line passes x=350; snapping to bars moves it a little.
	if off := channelOffset(a.mapper(), &ds[0]); math.Abs(float64(off-100)) > 3 {
		t.Fatalf("the second line runs %v DIPs below the first, want 100", off)
	}
}

// A text is written in the bar right away, and typing there deletes letters,
// not the text.
func TestTextIsEditedInPlace(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "文字")
	clickPlot(tt, a, 300, 200)
	tt.Frame()
	tt.Type("止损")
	tt.Frame()
	tt.Frame()
	d := drawings(a)
	if len(d) != 1 || !strings.Contains(d[0].Text, "止损") {
		t.Fatalf("typed into the text: %+v", d)
	}
	n := len([]rune(d[0].Text))
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	tt.Frame()
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	tt.Frame()
	if d := drawings(a); len(d) != 1 || len([]rune(d[0].Text)) != n-1 {
		t.Fatalf("Backspace and Delete while typing: %+v", d)
	}
}

func TestEscapeDropsHalfADrawing(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "斐波那契回撤")
	clickPlot(tt, a, 300, 200)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.creating != nil || a.tool != "" || len(drawings(a)) != 0 {
		t.Fatalf("after Escape: creating %v, tool %q, %d drawings", a.creating != nil, a.tool, len(drawings(a)))
	}
}

// Drawings belong to their market and are kept in the settings file.
func TestDrawingsAreKeptByMarket(t *testing.T) {
	a, tt := drawApp(t)
	a.file = filepath.Join(t.TempDir(), "settings.json")
	click(t, tt, "垂直线")
	clickPlot(tt, a, 300, 200)
	if s := loadSettings(a.file); len(s.Drawings["BTCUSDT"]) != 1 || s.Drawings["BTCUSDT"][0].Kind != "vline" {
		t.Fatalf("saved %+v", s.Drawings)
	}
	a.open("ETHUSDT", "1h")
	a.loading, a.bars = false, fakeBars(600, 3_600_000)
	tt.Frame()
	tt.Frame()
	if len(drawings(a)) != 0 || a.sel != 0 {
		t.Fatalf("ETH shows BTC's drawings: %+v", drawings(a))
	}
	a2 := newApplication(loadSettings(a.file), a.file, market.Binance{}, func(f func()) { f() })
	if a2.newID() <= a.set.Drawings["BTCUSDT"][0].ID {
		t.Fatal("a new drawing reuses a kept one's ID")
	}
	os.Remove(a.file)
}

func TestTrashAsksTwice(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	click(t, tt, "删除全部画线")
	if len(drawings(a)) != 1 {
		t.Fatal("one click on the bin deleted the drawings")
	}
	click(t, tt, "删除全部画线")
	if len(drawings(a)) != 0 {
		t.Fatal("two clicks on the bin left the drawings")
	}
}

// sampleDrawings is one of each, on fake hourly bars.
func sampleDrawings(a *application) []drawing {
	b := a.bars
	n := len(b)
	pt := func(i int, p float64) point { return point{T: b[i].T, P: p} }
	hi, lo := b[n-60].H, b[n-60].L
	for _, x := range b[n-90:] {
		hi, lo = max(hi, x.H), min(lo, x.L)
	}
	mid := (hi + lo) / 2
	return []drawing{
		{ID: 1, Kind: "trend", Pts: []point{pt(n-88, lo), pt(n-50, hi)}, Width: 2},
		{ID: 2, Kind: "ray", Pts: []point{pt(n-80, hi), pt(n-40, mid)}, Width: 2, Color: hexColor(ui.Hex("#f0b90b"))},
		{ID: 3, Kind: "hline", Pts: []point{pt(n-30, mid)}, Width: 1, Color: hexColor(ui.Hex("#ab47bc"))},
		{ID: 4, Kind: "hray", Pts: []point{pt(n-70, lo)}, Width: 2, Color: hexColor(ui.Hex("#ef5350"))},
		{ID: 5, Kind: "vline", Pts: []point{pt(n-45, mid)}, Width: 1},
		{ID: 6, Kind: "channel", Pts: []point{pt(n-35, lo), pt(n-10, mid), pt(n-25, lo-(hi-lo)*0.2)}, Width: 2, Color: hexColor(ui.Hex("#26a69a"))},
		{ID: 7, Kind: "rect", Pts: []point{pt(n-86, hi), pt(n-72, mid)}, Width: 1, Color: hexColor(ui.Hex("#ff9800"))},
		{ID: 8, Kind: "fib", Pts: []point{pt(n-62, lo), pt(n-48, hi)}, Width: 1},
		{ID: 9, Kind: "range", Pts: []point{pt(n-20, mid), pt(n-6, hi)}, Width: 2},
		{ID: 10, Kind: "text", Pts: []point{pt(n-84, lo)}, Width: 2, Text: "这里是支撑"},
	}
}

func TestDrawingShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	for _, dark := range []bool{true, false} {
		a := testApp()
		a.view.spacing = 10
		a.set.Drawings = map[string][]drawing{"BTCUSDT": sampleDrawings(a)}
		a.nextID = 10
		a.sel = 6
		tt := newTester(a, dark)
		x, y := win(a, 640, 260)
		tt.Move(x, y)
		tt.Frame()
		tt.Frame()
		shot(t, tt, map[bool]string{true: "drawings-dark", false: "drawings-light"}[dark])
	}
	a := testApp()
	a.view.spacing = 10
	a.set.Drawings = map[string][]drawing{"BTCUSDT": sampleDrawings(a)}
	a.nextID = 10
	tt := newTester(a, true)
	click(t, tt, "文字")
	clickPlot(tt, a, 700, 400)
	shot(t, tt, "drawings-text")
}
