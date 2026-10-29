package main

import (
	"image"
	"image/color"
	"math"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// countPixels is how many pixels of r f takes.
func countPixels(img *image.RGBA, r ui.Rect, f func(color.RGBA) bool) (n int) {
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if f(img.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

func iabs(v int) int { return max(v, -v) }

// green and red are pixels of the bars' own colors, at any alpha on white;
// not the lines' blues and oranges.
func green(c color.RGBA) bool {
	r, g, b := int(c.R), int(c.G), int(c.B)
	return g-r > 25 && b-r > 20 && iabs(g-b) < 40
}

func red(c color.RGBA) bool {
	r, g, b := int(c.R), int(c.G), int(c.B)
	return r-g > 25 && r-b > 25 && iabs(g-b) < 30
}

func near8(c color.RGBA, want ui.Color, tol int) bool {
	return iabs(int(c.R)-int(want.R))+iabs(int(c.G)-int(want.G))+iabs(int(c.B)-int(want.B)) < tol
}

// White rising candles and black falling ones on white, as one may set
// them: MACD's and VOL's bars keep colors of their own, and the volume under
// the candles gives way where the candles' color would be lost.
func TestBarsShowWhateverTheCandles(t *testing.T) {
	a := withIndicators("macd", "vol")
	st := &a.set.Chart
	st.Up, st.Down, st.BorderUp, st.Background = hexColor(ui.Hex("#ffffff")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#ffffff"))
	tt := newTester(a, false)
	img := tt.Image()
	_, panes := a.layout(a.boxH - timeH)
	for i, pn := range panes {
		r := ui.Rect{X: chartLeft(a), Y: chartTop(a) + pn.y + 24, W: a.plotW, H: pn.h - 26}
		if g, rd := countPixels(img, r, green), countPixels(img, r, red); g < 100 || rd < 100 {
			t.Errorf("%s: %d green pixels, %d red", a.indicators[pn.idx].Kind, g, rd)
		}
		_ = i
	}
	vol := ui.Rect{X: chartLeft(a), Y: chartTop(a) + a.plotH*0.84, W: a.plotW, H: a.plotH * 0.15}
	if g := countPixels(img, vol, green); g < 100 {
		t.Errorf("the rising volume under the candles shows %d green pixels", g)
	}
}

// MACD's bars go behind its lines: DIF runs on through every column.
func TestPaneLinesOverTheirBars(t *testing.T) {
	a := withIndicators("macd")
	macd := &a.indicators[0]
	macd.Colors[1] = hexColor{R: 1} // DEA unseen, so as not to cross DIF
	macd.Widths[0] = 2
	tt := newTester(a, true)
	img := tt.Image()
	_, panes := a.layout(a.boxH - timeH)
	top, bottom := int(chartTop(a)+panes[0].y)+1, int(chartTop(a)+panes[0].y+panes[0].h)-1
	n := len(a.bars)
	right := int(chartLeft(a) + a.view.x(n-1, a.plotW, n))
	dif := ui.Hex("#2962ff")
	var missing []int
	for x := int(chartLeft(a)) + 300; x < right; x++ { // right of the legend, which is DIF's color too
		found := false
		for y := top; y < bottom && !found; y++ {
			found = near8(img.RGBAAt(x, y), dif, 60)
		}
		if !found {
			missing = append(missing, x)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("DIF is cut in %d columns, as at x=%v", len(missing), missing[:min(len(missing), 8)])
	}
}

// A pane's price axis scales its prices, and only them, as the candles'
// axis does theirs: the wheel and a drag on it, then the pane dragging them
// up and down too, until a double click on it, or on the pane.
func TestPaneAxisScalesItsPane(t *testing.T) {
	a := withIndicators("macd", "rsi")
	tt := newTester(a, true)
	rel := func(got, want float64) bool { return math.Abs(got-want) <= math.Abs(want)*1e-6 }
	_, panes := a.layout(a.boxH - timeH)
	macd, rsi := a.indicators[0].ID, a.indicators[1].ID
	ax, y := chartLeft(a)+a.plotW+30, chartTop(a)+panes[0].y+panes[0].h/2
	tt.Move(ax, y)
	tt.Frame()
	if tt.Cursor() != ui.CursorResizeNS {
		t.Fatalf("on MACD's axis the cursor is %v", tt.Cursor())
	}
	shown, spacing := a.paneViews[macd].shown, a.view.spacing
	at := shown.at(y-chartTop(a)-panes[0].y, a.paneViews[macd].ax)
	tt.Scroll(ax, y, 0, notch)
	tt.Frame()
	pv := a.paneViews[macd]
	if !pv.scale.manual || !rel(pv.scale.hi-pv.scale.lo, (shown.hi-shown.lo)*zoomPerNotch) {
		t.Fatalf("a notch down on MACD's axis: %+v, from %+v", pv.scale, shown)
	}
	if got := pv.scale.at(y-chartTop(a)-panes[0].y, pv.ax); !rel(got, at) {
		t.Fatalf("the value under the pointer moved from %v to %v", at, got)
	}
	if pv.shown.lo != pv.scale.lo || pv.shown.hi != pv.scale.hi {
		t.Fatalf("the pane shows %+v, scaled to %+v", pv.shown, pv.scale)
	}
	if a.scale.manual || a.view.spacing != spacing || a.paneViews[rsi].scale.manual {
		t.Fatal("the wheel on MACD's axis scaled more than MACD")
	}

	span := pv.scale.hi - pv.scale.lo
	tt.Advance(time.Second)
	tt.Press(ax, y)
	tt.Move(ax, y+30)
	tt.Move(ax, y+60)
	tt.Release(ax, y+60)
	tt.Frame()
	if got := pv.scale.hi - pv.scale.lo; !rel(got, span*math.Exp(60.0/axisDrag)) || a.drag != dragNone {
		t.Fatalf("dragging MACD's axis 60 down: span %v, want %v", got, span*math.Exp(60.0/axisDrag))
	}

	lo, off := pv.scale.lo, a.view.offset
	x := chartLeft(a) + 400
	tt.Advance(time.Second)
	tt.Press(x, y)
	tt.Move(x, y+20)
	tt.Move(x, y+40)
	tt.Release(x, y+40)
	tt.Frame()
	if pv.scale.lo <= lo || a.scale.manual || a.view.offset != off {
		t.Fatalf("dragging the pane 40 down: lo %v (was %v), candles by hand %v", pv.scale.lo, lo, a.scale.manual)
	}

	tt.Advance(time.Second)
	tt.ClickAt(ax, y)
	tt.ClickAt(ax, y)
	tt.Frame()
	if pv.scale.manual {
		t.Fatal("a double click on MACD's axis left its prices by hand")
	}
	tt.Advance(time.Second)
	tt.Scroll(ax, y, 0, notch)
	tt.Frame()
	tt.Advance(time.Second)
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if a.paneViews[macd].scale.manual {
		t.Fatal("a double click in the pane left its prices by hand")
	}
}

// The newest value of every indicator is labelled on its axis in its color,
// unless the settings say not to.
func TestValueTags(t *testing.T) {
	a := withIndicators("ma", "rsi")
	ma, rsi := &a.indicators[0], &a.indicators[1]
	ma.Params[0] = 60
	tt := newTester(a, true)
	n := len(a.bars)
	maY := chartTop(a) + a.shown.y(a.seriesFor(ma).at(0, n-1), a.axis)
	if lastY := chartTop(a) + a.shown.y(a.bars[n-1].C, a.axis); math.Abs(float64(maY-lastY)) < 40 {
		t.Fatalf("MA60's label would sit under the last price's: %v, %v", maY, lastY)
	}
	_, panes := a.layout(a.boxH - timeH)
	pv := a.paneViews[rsi.ID]
	rsiY := chartTop(a) + panes[0].y + pv.shown.y(a.seriesFor(rsi).at(0, n-1), pv.ax)
	x := int(chartLeft(a) + a.plotW + 3)
	check := func(on bool) {
		t.Helper()
		img := tt.Image()
		if got := near8(img.RGBAAt(x, int(maY)), ma.color(&indKinds[0], 0), 30); got != on {
			t.Errorf("labels %v: MA's on the axis %v", on, got)
		}
		k, _ := kindOf("rsi")
		if got := near8(img.RGBAAt(x, int(rsiY)), rsi.color(k, 0), 30); got != on {
			t.Errorf("labels %v: RSI's on its axis %v", on, got)
		}
	}
	check(true)
	a.set.Chart.IndValues = false
	tt.Frame()
	tt.Frame()
	check(false)
}

// An offset draws MA that many bars to the right: the legend reads, at a
// bar, the value from that many bars before, and the prices fit where it is
// drawn.
func TestOffset(t *testing.T) {
	a := withIndicators("ma")
	ma := &a.indicators[0]
	ma.Offset = 3
	tt := newTester(a, true)
	n := len(a.bars)
	s := a.seriesFor(ma)
	k, _ := kindOf("ma")
	if !tt.HasText(a.valueText(k, s.at(0, n-4))) || tt.HasText(a.valueText(k, s.at(0, n-1))) {
		t.Fatal("the legend does not read the value from three bars before")
	}
	if _, ok := tt.Find("MA 5 偏移 3"); !ok {
		t.Fatal("the legend does not name the offset")
	}
	from, to := a.view.span(a.plotW, n)
	ma.Offset = 40 // far enough for what is drawn to differ from the bars in sight
	lo, hi := a.overlayRange(from, to, math.Inf(1), math.Inf(-1), false)
	wlo, whi := math.Inf(1), math.Inf(-1)
	for i := from - 40; i <= to-40; i++ {
		wlo, whi = min(wlo, s.at(0, i)), max(whi, s.at(0, i))
	}
	ulo, uhi := math.Inf(1), math.Inf(-1)
	for i := from; i <= to; i++ {
		ulo, uhi = min(ulo, s.at(0, i)), max(uhi, s.at(0, i))
	}
	if ulo == wlo && uhi == whi {
		t.Fatal("the test cannot tell an offset from none")
	}
	if lo != wlo || hi != whi {
		t.Fatalf("the prices fit %v–%v, want %v–%v", lo, hi, wlo, whi)
	}
}

// The settings pick the source and the offset.
func TestIndicatorSourceAndOffsetSettings(t *testing.T) {
	a := withIndicators("ma")
	tt := newTester(a, true)
	ma := &a.indicators[0]
	hoverRow(t, tt, "MA 5")
	click(t, tt, "指标设置")
	click(t, tt, "MA 源")
	click(t, tt, "最高价")
	r, _ := tt.Find("MA 偏移")
	tt.ClickAt(r.X+30, r.Y+r.H/2)
	tt.Frame()
	press(tt, ui.Cmd, ui.KeyA)
	tt.Type("3")
	tt.Frame()
	tt.Frame()
	click(t, tt, "确认")
	if ma.Source != "high" || ma.Offset != 3 {
		t.Fatalf("after 确认: %+v", *ma)
	}
	if _, ok := tt.Find("MA 5 最高价 偏移 3"); !ok {
		t.Fatal("the legend does not read MA 5 最高价 偏移 3")
	}
	k, _ := kindOf("ma")
	if got, want := a.seriesFor(ma).at(0, 9), sma(source(a.bars, "high"), 9, 5); got != want {
		t.Fatalf("MA of the highs: %v, want %v", got, want)
	}
	_ = k
}

func TestPaneShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	// Light, white rising and black falling candles, EMA26 and MACD.
	a := withIndicators("ema", "ema", "macd")
	a.indicators = a.indicators[1:]
	st := &a.set.Chart
	st.Up, st.Down, st.BorderUp, st.Background, st.Volume = hexColor(ui.Hex("#ffffff")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#ffffff")), false
	tt := newTester(a, false)
	shot(t, tt, "white-candles-light")

	a = withIndicators("ma", "boll", "macd", "rsi")
	a.indicators[1].Offset = 10
	tt = newTester(a, true)
	_, panes := a.layout(a.boxH - timeH)
	ax, y := chartLeft(a)+a.plotW+30, chartTop(a)+panes[0].y+panes[0].h/2
	tt.Scroll(ax, y, 0, -2*notch)
	tt.Frame()
	tt.Move(chartLeft(a)+500, y)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "pane-scaled-dark")
	hoverRow(t, tt, "MA 5")
	click(t, tt, "指标设置")
	shot(t, tt, "ma-settings-dark")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	hoverRow(t, tt, "MACD 12 26 9")
	click(t, tt, "指标设置")
	shot(t, tt, "macd-settings-dark")
}

// Labels that would cover one another move apart, in order, and stay on
// the axis.
func TestSpreadTags(t *testing.T) {
	got := spreadTags([]axisTag{{y: 105, text: "b"}, {y: 100, text: "a"}, {y: 300, text: "c"}, {y: 108, text: "x"}}, 10, 400)
	want := []float32{100, 120, 140, 300}
	for i, tg := range got {
		if tg.y != want[i] {
			t.Fatalf("spread to %+v", got)
		}
	}
	got = spreadTags([]axisTag{{y: 395}, {y: 398}, {y: 400}}, 10, 400)
	if got[0].y != 360 || got[1].y != 380 || got[2].y != 400 {
		t.Fatalf("at the bottom, spread to %+v", got)
	}
}

// The indicators' labels make way for the labels that stay put: into the
// room on the side their value is, apart from one another there.
func TestPlaceTags(t *testing.T) {
	last := fixedLabel{top: 190, bottom: 224, at: 200} // with the countdown under the price
	ys := func(tags []axisTag) (out []float32) {
		for _, tg := range tags {
			out = append(out, tg.y)
		}
		return out
	}
	for _, c := range []struct {
		name  string
		tags  []float32
		fixed []fixedLabel
		want  []float32
	}{
		{"clear of it", []float32{100, 300}, []fixedLabel{last}, []float32{100, 300}},
		{"under the price", []float32{210}, []fixedLabel{last}, []float32{234}},
		{"over the price", []float32{195}, []fixedLabel{last}, []float32{180}},
		{"two under", []float32{205, 201}, []fixedLabel{last}, []float32{234, 254}},
		{"one each side", []float32{198, 202}, []fixedLabel{last}, []float32{180, 234}},
		{"touching from above", []float32{175}, []fixedLabel{last}, []float32{175}},
		// No room below: above it instead, pushing the one there up.
		{"at the bottom", []float32{365, 395}, []fixedLabel{{top: 380, bottom: 400, at: 390}}, []float32{350, 370}},
		// Between the last price and the highest, too little room for one: past them.
		{"squeezed", []float32{232}, []fixedLabel{last, {top: 236, bottom: 256, at: 246}}, []float32{180}},
		{"none fixed", []float32{105, 100}, nil, []float32{100, 120}},
	} {
		var tags []axisTag
		for _, y := range c.tags {
			tags = append(tags, axisTag{y: y})
		}
		got := ys(placeTags(tags, c.fixed, 10, 390))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: placed at %v, want %v", c.name, got, c.want)
		}
	}
}

// On the chart: an MA of one bar is the last price, and its label shows
// whole under the last price's label, not under it.
func TestIndicatorLabelClearsTheLastPrice(t *testing.T) {
	a := testApp()
	k, _ := kindOf("ma")
	a.addIndicator(k)
	ind := &a.indicators[len(a.indicators)-1]
	ind.Params = []float64{1}
	ind.Colors = []hexColor{hexColor(ui.Hex("#ff00ff"))}
	tt := newTester(a, true)
	axisX := int(chartLeft(a) + a.plotW)
	img := tt.Image()
	n := count(img, image.Rect(axisX+1, titleBarHeight, axisX+axisW, titleBarHeight+int(a.plotH)), color.RGBA{255, 0, 255, 255})
	if want := 20 * (axisW - 2) * 6 / 10; n < want { // the label, less its text
		t.Fatalf("%d of the MA label's pixels show, want %d", n, want)
	}
	shot(t, tt, "labels-clear")
}
