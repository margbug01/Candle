package main

import (
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
	press(tt, ui.Ctrl, ui.KeyZ)
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
}
