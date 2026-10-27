package main

import (
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// dragHandle drags point i of the selected drawing dy DIPs down.
func dragHandle(tt *ui.Tester, a *application, i int, dy float32) {
	tt.Advance(time.Second) // not a double click on what was just clicked
	x, y := a.mapper().xy(drawings(a)[0].Pts[i])
	wx, wy := win(a, x, y)
	tt.Press(wx, wy)
	tt.Move(wx, wy+dy/2)
	tt.Move(wx, wy+dy)
	tt.Release(wx, wy+dy)
	tt.Frame()
	tt.Frame()
}

// One click places a long position at 2:1; dragging its points moves the
// take-profit and the stop-loss up and down only, each kept on its side of
// the entry, and the ratio follows.
func TestLongPosition(t *testing.T) {
	a, tt := drawApp(t)
	a.file = filepath.Join(t.TempDir(), "settings.json")
	m := a.mapper()
	entry := m.snap(300, 300, false)
	click(t, tt, "多头盈亏比")
	clickPlot(tt, a, 300, 300)
	ds := drawings(a)
	if len(ds) != 1 || ds[0].Kind != "long" || a.tool != "" || a.sel != ds[0].ID {
		t.Fatalf("one click drew %+v, tool %q", ds, a.tool)
	}
	d := ds[0]
	if d.Pts[0] != entry || d.Pts[1].P <= entry.P || d.Pts[2].P >= entry.P || d.Pts[1].T != entry.T || d.Pts[2].T != entry.T {
		t.Fatalf("points %+v, entry %+v", d.Pts, entry)
	}
	if r, _ := riskReward(&d); math.Abs(r-2) > 1e-9 || d.Pts[3].T != m.timeAt(m.idx(entry.T)+positionBars) || d.Pts[3].P != entry.P {
		t.Fatalf("ratio %v, right edge %+v", r, d.Pts[3])
	}
	if !tt.HasText("盈亏比 2.00") {
		t.Fatal("the bar does not read the ratio")
	}

	target := d.Pts[1]
	dragHandle(tt, a, 1, -40)
	d = drawings(a)[0]
	if d.Pts[1].P <= target.P || d.Pts[1].T != entry.T {
		t.Fatalf("dragging the take-profit 40 up: %+v, was %+v", d.Pts[1], target)
	}
	if r, _ := riskReward(&d); r <= 2 || !tt.HasText(ratioText(&d)) {
		t.Fatalf("a farther take-profit, ratio %v", r)
	}
	press(tt, ui.Cmd, ui.KeyZ)
	if r, _ := riskReward(&drawings(a)[0]); math.Abs(r-2) > 1e-9 {
		t.Fatalf("Ctrl+Z left the ratio at %v", r)
	}

	_, ys := a.mapper().xy(drawings(a)[0].Pts[2])
	_, ye := a.mapper().xy(entry)
	dragHandle(tt, a, 2, ye-ys-30) // 30 DIPs above the entry
	d = drawings(a)[0]
	if d.Pts[2].P != entry.P || !tt.HasText("盈亏比 —") {
		t.Fatalf("a stop-loss dragged above the entry: %+v", d.Pts[2])
	}
	if s := loadSettings(a.file); len(s.Drawings["BTCUSDT"]) != 1 || len(s.Drawings["BTCUSDT"][0].Pts) != 4 {
		t.Fatalf("saved %+v", s.Drawings)
	}
}

func TestShortPosition(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "空头盈亏比")
	clickPlot(tt, a, 300, 300)
	d := drawings(a)[0]
	if d.Kind != "short" || d.Pts[1].P >= d.Pts[0].P || d.Pts[2].P <= d.Pts[0].P {
		t.Fatalf("a short position %+v", d)
	}
	right := d.Pts[3]
	dragHandle(tt, a, 3, 25) // the right edge stays at the entry's price
	if got := drawings(a)[0].Pts[3]; got.P != d.Pts[0].P || got.T != right.T {
		t.Fatalf("dragging the right edge down moved it to %+v", got)
	}
}

func TestPositionShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	for _, dark := range []bool{true, false} {
		a, tt := drawApp(t)
		tt.SetDark(dark)
		click(t, tt, "多头盈亏比")
		clickPlot(tt, a, 260, 330)
		click(t, tt, "空头盈亏比")
		clickPlot(tt, a, 620, 200)
		shot(t, tt, map[bool]string{true: "position-dark", false: "position-light"}[dark])
	}
	a, tt := whiteCandlesApp()
	click(t, tt, "多头盈亏比")
	clickPlot(tt, a, 260, 330)
	click(t, tt, "空头盈亏比")
	clickPlot(tt, a, 620, 200)
	click(t, tt, "价格区间")
	clickPlot(tt, a, 420, 420)
	clickPlot(tt, a, 520, 300)
	shot(t, tt, "position-white-candles")
}

// The position is what loses the risk, a part of the account, at the
// stop-loss: 1% of 1000 by default.
func TestPositionSize(t *testing.T) {
	d := drawing{Kind: "long", Pts: []point{{P: 100}, {P: 110}, {P: 95}, {P: 100}}}
	if q, ok := positionSize(&d); !ok || math.Abs(q-2) > 1e-12 {
		t.Fatalf("1%% of 1000 over 5: %v", q)
	}
	d.Account, d.Risk = 5000, 2
	if q, _ := positionSize(&d); math.Abs(q-20) > 1e-12 {
		t.Fatalf("2%% of 5000 over 5: %v", q)
	}
	d.Pts[2].P = 100
	if _, ok := positionSize(&d); ok {
		t.Fatal("sized without a stop-loss")
	}
	for q, want := range map[float64]string{0.024: "0.02400", 12.3: "12.30", 1234.6: "1235", 2: "2.000"} {
		if got := qtyText(q); got != want {
			t.Errorf("qtyText(%v) = %q, want %q", q, got, want)
		}
	}
}

// The bar of a selected position sets its account, as one step to undo.
func TestPositionAccount(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "多头盈亏比")
	clickPlot(tt, a, 300, 300)
	r, ok := tt.Find("账户金额")
	if !ok || a.accountPick.v != defaultAccount {
		t.Fatal("no account in the bar")
	}
	tt.ClickAt(r.X+30, r.Y+r.H/2)
	tt.Frame()
	press(tt, ui.Cmd, ui.KeyA)
	for _, ch := range "2500" {
		tt.Type(string(ch))
		tt.Frame()
	}
	if d := drawings(a)[0]; d.Account != 2500 {
		t.Fatalf("typed 2500: account %v", d.Account)
	}
	clickPlot(tt, a, 700, 60)
	press(tt, ui.Cmd, ui.KeyZ)
	if d := drawings(a)[0]; d.Account != 0 {
		t.Fatalf("one Ctrl+Z after typing the account: %v", d.Account)
	}
}

// whiteCandlesApp is testApp with light, white rising and black
// falling candles on white.
func whiteCandlesApp() (*application, *ui.Tester) {
	a := testApp()
	a.set.Magnet = false
	st := &a.set.Chart
	st.Up, st.Down, st.BorderUp, st.Background, st.Volume = hexColor(ui.Hex("#ffffff")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#ffffff")), false
	return a, newTester(a, false)
}

// The position's zones and labels, and the price range, take colors of
// their own, as TradingView's: the candles' white would be lost on white.
func TestPositionColorsShowWhateverTheCandles(t *testing.T) {
	a, tt := whiteCandlesApp()
	click(t, tt, "多头盈亏比")
	clickPlot(tt, a, 300, 300)
	clickPlot(tt, a, 700, 60) // unselected, so the bar is not in the way
	img := tt.Image()
	plot := ui.Rect{X: chartLeft(a), Y: chartTop(a), W: a.plotW, H: a.plotH}
	white := ui.Hex("#ffffff")
	up, down := ui.Hex(barUp), ui.Hex(barDown)
	blend := func(c ui.Color, alpha float32) ui.Color { return white.Mix(c, alpha) }
	count := func(want ui.Color, tol int) int {
		return countPixels(img, plot, func(c color.RGBA) bool { return near8(c, want, tol) })
	}
	for _, f := range []struct {
		what string
		c    ui.Color
		tol  int
	}{{"the profit's zone", blend(up, 0.2), 12}, {"the loss's zone", blend(down, 0.2), 12}, {"the take-profit's label", up, 30}, {"the stop-loss's label", down, 30}} {
		if n := count(f.c, f.tol); n < 300 {
			t.Errorf("%s: %d pixels of its color", f.what, n)
		}
	}

	a, tt = whiteCandlesApp()
	click(t, tt, "价格区间")
	x0, y0 := win(a, 200, 350)
	x1, y1 := win(a, 320, 200)
	tt.Press(x0, y0)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
	tt.Frame()
	clickPlot(tt, a, 700, 60)
	img = tt.Image()
	if n := countPixels(img, plot, func(c color.RGBA) bool { return near8(c, up, 30) }); n < 300 {
		t.Errorf("a rising price range: %d pixels of its color", n)
	}
}
