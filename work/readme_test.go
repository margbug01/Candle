package main

import (
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// readmeBars are n made-up bars ivMs apart, the newest open now: a rise in
// waves, closing at 80076.68, the volume heavier on the bigger moves.
func readmeBars(n int, ivMs int64) []market.Bar {
	r := rand.New(rand.NewPCG(26, 10))
	end := time.Now().UnixMilli() / ivMs * ivMs
	bars := make([]market.Bar, n)
	price := 70000.0
	for i := range bars {
		drift := 0.0004 + 0.0013*math.Sin(float64(i)/23) + 0.0005*math.Sin(float64(i)/6)
		o := price
		c := o * (1 + drift + (r.Float64()-0.5)*0.008)
		bars[i] = market.Bar{T: end - int64(n-1-i)*ivMs, O: o, C: c,
			H: max(o, c) * (1 + r.Float64()*r.Float64()*0.006), L: min(o, c) * (1 - r.Float64()*r.Float64()*0.006),
			V: (250 + 700*r.Float64()) * (1 + 150*math.Abs(c/o-1))}
		price = c
	}
	f := 80076.68 / bars[n-1].C
	for i := range bars {
		b := &bars[i]
		b.O, b.H, b.L, b.C = b.O*f, b.H*f, b.L*f, b.C*f
	}
	return bars
}

// readmeApp is testApp on readmeBars, with indicators of keys and the
// watchlist's quotes as they might be.
func readmeApp(keys ...string) *application {
	a := withIndicators(keys...)
	a.bars = readmeBars(600, 3_600_000)
	last := a.bars[len(a.bars)-1].C
	for _, q := range []struct {
		sym        string
		last, move float64
		tick       float64
	}{
		{"BTCUSDT", last, 0.0124, 0.01}, {"ETHUSDT", 2318.55, -0.0147, 0.01}, {"SOLUSDT", 101.06, 0.0231, 0.01},
		{"BNBUSDT", 740.24, -0.0064, 0.01}, {"XRPUSDT", 1.2866, 0.0085, 0.0001}, {"DOGEUSDT", 0.07692, -0.0025, 0.00001},
		{"SUIUSDT", 0.9106, 0.0312, 0.0001}, {"ADAUSDT", 0.2031, -0.0136, 0.0001},
	} {
		base := q.sym[:len(q.sym)-4]
		a.infos[q.sym] = market.Symbol{Name: q.sym, Base: base, Quote: "USDT", Tick: q.tick}
		a.quotes[q.sym] = market.Ticker{Symbol: q.sym, Open: q.last / (1 + q.move), Last: q.last, QuoteVolume: 1e8}
	}
	return a
}

// TestReadmeShots draws the README's pictures at twice the size, from
// made-up bars and quotes:
//
//	CANDLE_README=<folder> go test -run TestReadmeShots
//	python ../tools/shots.py <folder> ../docs/screenshots
func TestReadmeShots(t *testing.T) {
	dir := os.Getenv("CANDLE_README")
	if dir == "" {
		t.Skip("CANDLE_README names no folder")
	}
	shoot := func(name string, a *application, dark bool, act func(tt *ui.Tester)) {
		t.Helper()
		tt := ui.NewTester(a.render, 1320, 820)
		tt.SetScale(2)
		tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Right: 138})
		tt.SetDark(dark)
		tt.Frame()
		tt.Frame()
		act(tt)
		tt.Frame()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	away := func(tt *ui.Tester) { tt.Move(1319, 819) } // no hover in the picture

	for _, dark := range []bool{false, true} {
		a := readmeApp("ma", "ma", "vol", "macd")
		a.set.Indicators[1].Params[0] = 20
		a.set.Chart.Volume = false // VOL has a pane of its own
		shoot(map[bool]string{false: "main-light", true: "main-dark"}[dark], a, dark, func(tt *ui.Tester) {
			tt.Move(chartLeft(a)+610, chartTop(a)+170)
		})
	}

	a := readmeApp("boll", "rsi", "kdj")
	shoot("indicators-dark", a, true, func(tt *ui.Tester) {
		hoverRow(t, tt, "BOLL 20 2")
		tt.Move(chartLeft(a)+700, chartTop(a)+a.plotH+60)
	})

	a = readmeApp()
	a.view.spacing = 10
	a.set.Drawings = map[string][]drawing{"BTCUSDT": readmeDrawings(a.bars)}
	a.nextID, a.sel = 10, 4
	shoot("drawings-light", a, false, away)

	a = readmeApp("ma", "macd")
	shoot("indicators-list-dark", a, true, func(tt *ui.Tester) {
		click(t, tt, "指标")
		away(tt)
	})

	a = readmeApp("ema", "rsi")
	shoot("settings-light", a, false, func(tt *ui.Tester) {
		click(t, tt, "图表设置")
		click(t, tt, "坐标")
		away(tt)
	})
}

// readmeDrawings are drawings on the last 88 bars, about what spacing 10 shows: a
// ray along the falling highs, the support at the lowest low with its name,
// and a long position taken three bars after it.
func readmeDrawings(b []market.Bar) []drawing {
	n := len(b)
	hi, lo := n-88, n-88
	for i := n - 88; i < n; i++ {
		if b[i].H > b[hi].H {
			hi = i
		}
		if b[i].L < b[lo].L {
			lo = i
		}
	}
	j := hi + 20 // the next high the ray rests on
	for i := hi + 20; i < lo; i++ {
		if b[i].H > b[j].H {
			j = i
		}
	}
	pt := func(i int, p float64) point { return point{T: b[i].T, P: p} }
	entry := b[lo+3].C
	stop := b[lo].L * 0.998
	return []drawing{
		{ID: 1, Kind: "ray", Pts: []point{pt(hi, b[hi].H), pt(j, b[j].H)}, Width: 2},
		{ID: 2, Kind: "hline", Pts: []point{pt(lo, b[lo].L)}, Width: 1, Color: hexColor(ui.Hex("#ab47bc"))},
		{ID: 3, Kind: "text", Pts: []point{pt(n-80, b[lo].L*1.004)}, Width: 2, Text: "前低支撑", Color: hexColor(ui.Hex("#ab47bc"))},
		{ID: 4, Kind: "long", Width: 1, Account: 10000, Pts: []point{pt(lo+3, entry), pt(lo+3, entry+2*(entry-stop)), pt(lo+3, stop), {T: b[lo+3].T + positionBars*(b[1].T-b[0].T), P: entry}}},
	}
}
