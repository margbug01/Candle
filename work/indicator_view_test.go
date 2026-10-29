package main

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// withIndicators is testApp with indicators of keys, made as 指标 makes them.
func withIndicators(keys ...string) *application {
	a := testApp()
	for _, key := range keys {
		k, _ := kindOf(key)
		a.addIndicator(k)
	}
	return a
}

// chartTop is where the chart starts in a test window: under the title bar.
func chartTop(a *application) float32 { return 820 - a.boxH }

// hoverRow puts the pointer on the legend row of the indicator named name,
// for its buttons.
func hoverRow(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	r, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no legend row %q", name)
	}
	tt.Move(r.X+8, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
}

// Clicking a kind adds one, the list staying open; another MA comes as MA10;
// typing and Enter adds the first kind found; a pane's kind gets a pane.
func TestAddIndicatorsFromTheList(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	mainH := a.plotH
	click(t, tt, "指标")
	if !a.indList {
		t.Fatal("指标 opened nothing")
	}
	click(t, tt, "添加 MA")
	tt.Advance(time.Second) // two clicks, not a double click
	click(t, tt, "添加 MA")
	if len(a.set.Indicators) != 2 || a.set.Indicators[0].Params[0] != 5 || a.set.Indicators[1].Params[0] != 10 || !a.indList {
		t.Fatalf("after two clicks: %+v, list open %v", a.set.Indicators, a.indList)
	}
	if !tt.HasText("已添加 2") {
		t.Fatal("the list does not count what was added")
	}
	click(t, tt, "搜索指标")
	tt.Type("macd")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if n := len(a.set.Indicators); n != 3 || a.set.Indicators[2].Kind != "macd" {
		t.Fatalf("typing macd and Enter: %+v", a.set.Indicators)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	if a.indList {
		t.Fatal("Escape left the list open")
	}
	for _, name := range []string{"MA 5", "MA 10", "MACD 12 26 9"} {
		if _, ok := tt.Find(name); !ok {
			t.Fatalf("no legend row %q", name)
		}
	}
	if a.plotH >= mainH-100 || a.boxH-timeH-a.plotH < 100 {
		t.Fatalf("with MACD the candles take %v of %v", a.plotH, a.boxH-timeH)
	}
}

// The search finds kinds by their Chinese names too.
func TestSearchKinds(t *testing.T) {
	all := "MA EMA BOLL MACD RSI KDJ VOL WMA HMA VWMA VWAP KC DC Ichimoku SAR SuperTrend DMI Aroon StochRSI CCI WR ROC TRIX BIAS ATR OBV MFI CMF"
	for q, want := range map[string]string{"": all, "布林": "BOLL", "均线": "MA EMA MACD WMA HMA VWMA", "rsi": "RSI StochRSI",
		"抛物线": "SAR", "超级": "SuperTrend", "ichimoku": "Ichimoku", "成交量": "VOL VWMA VWAP OBV MFI CMF", "zz": ""} {
		var got []string
		for _, k := range searchKinds(q) {
			got = append(got, k.short)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%q finds %v, want %s", q, got, want)
		}
	}
}

// A row's buttons hide, set up and delete its indicator, and stay where they
// are while the values beside them change as the pointer goes to them.
func TestLegendButtons(t *testing.T) {
	a := withIndicators("ma", "macd")
	tt := newTester(a, true)
	if tt.HasText("指标设置") {
		t.Fatal("buttons show without the pointer on a row")
	}
	hoverRow(t, tt, "MACD 12 26 9")
	gear, _ := tt.Find("指标设置")
	tt.Move(gear.X+gear.W/2, gear.Y+gear.H/2)
	tt.Frame()
	if moved, _ := tt.Find("指标设置"); moved != gear {
		t.Fatalf("the settings button moved from %v to %v under the pointer", gear, moved)
	}
	click(t, tt, "隐藏指标")
	macd := a.indicatorByID(a.set.Indicators[1].ID)
	if !macd.Hidden || !tt.HasText("显示指标") {
		t.Fatal("隐藏指标 left it shown")
	}
	click(t, tt, "显示指标")
	if macd.Hidden {
		t.Fatal("显示指标 left it hidden")
	}
	hoverRow(t, tt, "MA 5")
	id := a.set.Indicators[0].ID
	a.seriesFor(&a.set.Indicators[0])
	click(t, tt, "删除指标")
	if len(a.set.Indicators) != 1 || a.set.Indicators[0].Kind != "macd" || a.series[id] != nil {
		t.Fatalf("after 删除指标: %+v, cache kept %v", a.set.Indicators, a.series[id] != nil)
	}
}

// The settings change the parameters and the lines at once; 取消 puts them
// back, 确认 keeps them.
func TestIndicatorSettings(t *testing.T) {
	a := withIndicators("ma")
	tt := newTester(a, true)
	ma := &a.set.Indicators[0]
	edit := func() {
		t.Helper()
		hoverRow(t, tt, "MA "+ma.paramText())
		click(t, tt, "指标设置")
		if !a.indDialog {
			t.Fatal("指标设置 opened nothing")
		}
		r, _ := tt.Find("MA 周期")
		tt.ClickAt(r.X+30, r.Y+r.H/2) // the middle is the stepper's
		tt.Frame()
		press(tt, ui.Cmd, ui.KeyA)
		for _, ch := range "30" { // a key at a time, as typed
			tt.Type(string(ch))
			tt.Frame()
		}
		click(t, tt, "线宽 3")
	}
	edit()
	if ma.Params[0] != 30 || ma.Widths[0] != 3 {
		t.Fatalf("while editing: %+v", *ma)
	}
	if got := a.seriesFor(ma).at(0, 29); math.IsNaN(got) {
		t.Fatal("MA30 has no value on the 30th bar")
	}
	click(t, tt, "取消")
	if ma.Params[0] != 5 || ma.Widths[0] != 1 || a.indDialog {
		t.Fatalf("after 取消: %+v", *ma)
	}
	edit()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	if ma.Params[0] != 5 || a.indDialog {
		t.Fatalf("after Escape: %+v", *ma)
	}
	edit()
	click(t, tt, "确认")
	if ma.Params[0] != 30 || ma.Widths[0] != 3 || a.indDialog {
		t.Fatalf("after 确认: %+v", *ma)
	}
	if _, ok := tt.Find("MA 30"); !ok {
		t.Fatal("the legend does not read MA 30")
	}
}

// The line above a pane drags up and down, the candles keeping minMainH.
func TestPaneSeparatorDrags(t *testing.T) {
	a := withIndicators("macd", "rsi")
	tt := newTester(a, true)
	h := a.boxH - timeH
	_, panes := a.layout(h)
	x, y := chartLeft(a)+300, chartTop(a)+panes[1].y
	tt.Move(x, y+1)
	tt.Frame()
	if tt.Cursor() != ui.CursorResizeRow {
		t.Fatalf("on the line above RSI the cursor is %v", tt.Cursor())
	}
	before := a.view
	tt.Press(x, y+1)
	tt.Move(x, y-30)
	tt.Move(x, y-59)
	tt.Release(x, y-59)
	tt.Frame()
	_, after := a.layout(h)
	if d := after[1].h - panes[1].h; math.Abs(float64(d-60)) > 0.5 || math.Abs(float64(after[0].h-panes[0].h+60)) > 0.5 {
		t.Fatalf("dragged 60 up: MACD %v → %v, RSI %v → %v", panes[0].h, after[0].h, panes[1].h, after[1].h)
	}
	if a.view != before || a.drag != dragNone {
		t.Fatal("dragging the line moved the chart")
	}
	y = chartTop(a) + after[0].y
	tt.Press(x, y)
	tt.Move(x, chartTop(a)+10)
	tt.Release(x, chartTop(a)+10)
	tt.Frame()
	if a.plotH < minMainH-0.5 {
		t.Fatalf("the candles were squeezed to %v", a.plotH)
	}
	if len(a.set.Indicators) != 2 {
		t.Fatalf("dragging the lines left %+v", a.set.Indicators)
	}
}

// In a pane the chart drags, zooms and resets as over the candles, and the
// crosshair runs down to the time axis, labelled on the pane's axis.
func TestPaneInput(t *testing.T) {
	a := withIndicators("macd")
	tt := newTester(a, true)
	_, panes := a.layout(a.boxH - timeH)
	py := chartTop(a) + panes[0].y + panes[0].h/2
	x := chartLeft(a) + 400

	a.scale = prices{manual: true, lo: 70000, hi: 90000}
	off := a.view.offset
	tt.Press(x, py)
	tt.Move(x+40, py+30)
	tt.Move(x+80, py+30)
	tt.Release(x+80, py+30)
	tt.Frame()
	if !near(a.view.offset, off-80/a.view.spacing) || a.scale.lo != 70000 {
		t.Fatalf("dragging the pane: offset %v, want %v; prices %+v", a.view.offset, off-80/a.view.spacing, a.scale)
	}
	spacing := a.view.spacing
	tt.Scroll(x, py, 0, notch)
	tt.Frame()
	if near(a.view.spacing, spacing) {
		t.Fatal("the wheel over the pane did not zoom")
	}
	spacing = a.view.spacing
	tt.Scroll(chartLeft(a)+a.plotW+30, py, 0, notch)
	tt.Frame()
	if a.view.spacing != spacing || a.scale.lo != 70000 {
		t.Fatal("the wheel over the pane's axis scaled something")
	}
	tt.Advance(time.Second)
	tt.ClickAt(x, py)
	tt.ClickAt(x, py)
	tt.Frame()
	if a.view.offset != a.home() || a.view.spacing != defaultView().spacing {
		t.Fatalf("a double click in the pane left %+v", a.view)
	}

	tt.Move(x, py)
	tt.Frame()
	tt.Frame()
	img := tt.Image()
	timeRow := ui.Rect{X: x - 30, Y: 820 - timeH + 2, W: 60, H: timeH - 4}
	if box := inkBox(img, timeRow); box[0] < 40 {
		t.Fatalf("no time under the crosshair in the pane: ink %v", box)
	}
	// Beside the axis's numbers, which start 8 DIPs in: the label's fill.
	c := img.RGBAAt(int(chartLeft(a)+a.plotW+5), int(py))
	if luma(ui.RGBA(c.R, c.G, c.B, 1)) < luma(paletteFor(true).bg)+0.05 {
		t.Fatalf("no label on the pane's axis at the crosshair: %v", c)
	}
}

// The time axis stays under the panes, where the labels are.
func TestTimeAxisUnderThePanes(t *testing.T) {
	a := withIndicators("macd", "rsi")
	tt := newTester(a, true)
	if box := inkBox(tt.Image(), ui.Rect{X: chartLeft(a), Y: 820 - timeH + 2, W: a.plotW, H: timeH - 4}); float32(box[0]) < a.plotW/2 {
		t.Fatalf("the time axis holds ink %v", box)
	}
}

// Fitted to the bars in sight, the prices make room for the overlays too.
func TestOverlaysWidenThePrices(t *testing.T) {
	a := withIndicators("boll")
	newTester(a, true)
	from, to := a.view.span(a.plotW, len(a.bars))
	lo, hi := priceRange(a.bars[from : to+1])
	s := a.seriesFor(&a.set.Indicators[0])
	for i := from; i <= to; i++ {
		lo, hi = min(lo, s.at(2, i)), max(hi, s.at(1, i))
	}
	if a.shown.lo != lo || a.shown.hi != hi {
		t.Fatalf("prices %v–%v, want the bands' %v–%v", a.shown.lo, a.shown.hi, lo, hi)
	}
	a.set.Indicators[0].Hidden = true
	newTester(a, true)
	if l, h := priceRange(a.bars[from : to+1]); a.shown.lo != l || a.shown.hi != h {
		t.Fatal("a hidden BOLL still widens the prices")
	}
}

// A tick of the newest bar works out only that bar; older ones arriving in
// front work out them all.
func TestLiveBarsUpdateTheTail(t *testing.T) {
	a := withIndicators("ema", "macd")
	tt := newTester(a, true)
	n := len(a.bars)
	last := a.bars[n-1]
	last.C *= 1.002
	a.addBars([]market.Bar{last})
	tt.Frame()
	for _, ind := range a.set.Indicators {
		if s := a.series[ind.ID]; s.from != n-1 {
			t.Fatalf("%s began at %d after a tick, want %d", ind.Kind, s.from, n-1)
		}
	}
	older := fakeBars(n+300, a.intervalMs())[:300]
	a.bars = append(older, a.bars...)
	tt.Frame()
	for _, ind := range a.set.Indicators {
		if s := a.series[ind.ID]; s.from != 0 || s.n != n+300 {
			t.Fatalf("%s began at %d over %d bars after older ones came", ind.Kind, s.from, s.n)
		}
	}
}

// Keys for the drawings wait while an indicator dialog is open.
func TestDialogsKeepDrawingKeys(t *testing.T) {
	a, tt := drawApp(t)
	a.set.Indicators = withIndicators("ma").set.Indicators
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	click(t, tt, "指标")
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	if len(drawings(a)) != 1 {
		t.Fatal("Delete in the indicator list deleted the drawing")
	}
}

// A frame of 3000 bars with every kind on, the pointer moving, drawn by
// the tests' software renderer: the window's GPU is quicker.
func TestFrameWith3000Bars(t *testing.T) {
	keys := []string{"ma", "ma", "ema", "boll", "macd", "rsi", "kdj", "vol"}
	timeFrames := func(keys ...string) time.Duration {
		a := withIndicators(keys...)
		a.bars, a.view.spacing = fakeBars(3000, 3_600_000), minSpacing
		tt := newTester(a, true)
		t0 := time.Now()
		for i := range 20 {
			tt.Move(chartLeft(a)+float32(100+20*i), 300)
			tt.Frame()
		}
		return time.Since(t0) / 20
	}
	bare, all := timeFrames(), timeFrames(keys...)
	t.Logf("a frame: %v without indicators, %v with %d", bare, all, len(keys))
	if all > 250*time.Millisecond {
		t.Fatalf("a frame takes %v", all)
	}
}

func TestIndicatorShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	a := withIndicators("ma", "ma", "boll", "macd", "rsi")
	tt := newTester(a, true)
	tt.Move(chartLeft(a)+500, 820-timeH-40)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "indicators-dark")

	a = withIndicators("ema", "ema", "kdj", "vol")
	a.view.spacing = 5
	tt = newTester(a, false)
	tt.Move(chartLeft(a)+640, 300)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "indicators-light")

	a = withIndicators("ma", "macd")
	tt = newTester(a, true)
	hoverRow(t, tt, "MACD 12 26 9")
	shot(t, tt, "indicators-hover-row")
	click(t, tt, "指标设置")
	shot(t, tt, "indicator-settings")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	click(t, tt, "指标")
	shot(t, tt, "indicators-dialog")

	a = withIndicators("boll", "macd")
	tt = newTester(a, false)
	hoverRow(t, tt, "BOLL 20 2")
	click(t, tt, "指标设置")
	shot(t, tt, "indicator-settings-light")
}
