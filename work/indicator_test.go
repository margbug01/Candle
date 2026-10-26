package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"candle/market"
)

func barsOfCloses(cs ...float64) []market.Bar {
	out := make([]market.Bar, len(cs))
	for i, c := range cs {
		out[i] = market.Bar{T: int64(i) * 60_000, O: c, H: c, L: c, C: c, V: 1}
	}
	return out
}

// values works kind out over all of b with params p.
func values(t *testing.T, key string, b []market.Bar, p ...float64) [][]float64 {
	t.Helper()
	k, ok := kindOf(key)
	if !ok {
		t.Fatalf("no kind %q", key)
	}
	var s series
	s.update(k, p, b, 1)
	return s.out
}

// same checks got against want to within tol, NaN only where want is NaN.
func same(t *testing.T, what string, got, want []float64, tol float64) {
	t.Helper()
	if len(got) < len(want) {
		t.Fatalf("%s: %d values, want %d", what, len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if math.IsNaN(w) != math.IsNaN(g) || !math.IsNaN(w) && math.Abs(g-w) > tol {
			t.Fatalf("%s[%d] = %v, want %v", what, i, g, w)
		}
	}
}

func nans(n int, then ...float64) []float64 {
	out := make([]float64, n, n+len(then))
	for i := range out {
		out[i] = math.NaN()
	}
	return append(out, then...)
}

// Wilder's RSI on StockCharts' worked example, which TradingView's ta.rsi
// gives to the cent.
func TestRSIMatchesStockCharts(t *testing.T) {
	b := barsOfCloses(44.3389, 44.0902, 44.1497, 43.6124, 44.2778, 44.8264, 45.0955, 45.4245, 45.8433, 46.0826, 45.8931,
		46.0328, 45.6140, 46.2820, 46.2820, 46.0028, 46.0328, 46.4116, 46.2222, 45.6439, 46.2122, 46.2521, 45.7137,
		46.4515, 45.7835, 45.3548, 44.0288, 44.1783, 44.2181, 44.5672, 43.4205, 42.6628, 43.1314)
	want := nans(14, 70.53, 66.32, 66.55, 69.41, 66.36, 57.97, 62.93, 63.26, 56.06, 62.38, 54.71, 50.42, 39.99,
		41.46, 41.87, 45.46, 37.30, 33.08, 37.77)
	same(t, "RSI14", values(t, "rsi", b, 14)[0], want, 0.005)
}

// The EMA starts as the SMA: StockCharts' 10-day example.
func TestEMAMatchesStockCharts(t *testing.T) {
	b := barsOfCloses(22.27, 22.19, 22.08, 22.17, 22.18, 22.13, 22.23, 22.43, 22.24, 22.29, 22.15, 22.39, 22.38, 22.61,
		23.36, 24.05, 23.75, 23.83, 23.95, 23.63, 23.82, 23.87, 23.65, 23.19, 23.10, 23.33, 22.68, 23.10, 22.40, 22.17)
	want := nans(9, 22.22, 22.21, 22.24, 22.27, 22.33, 22.52, 22.80, 22.97, 23.13, 23.28, 23.34, 23.43, 23.51, 23.53,
		23.47, 23.40, 23.39, 23.26, 23.23, 23.08, 22.92)
	same(t, "EMA10", values(t, "ema", b, 10)[0], want, 0.005)
}

// Small enough to work out by hand.
func TestSmallCases(t *testing.T) {
	b := barsOfCloses(10, 11, 12, 11, 13, 14)
	same(t, "MA3", values(t, "ma", b, 3)[0], nans(2, 11, 34.0/3, 12, 38.0/3), 1e-12)
	same(t, "EMA3", values(t, "ema", b, 3)[0], nans(2, 11, 11, 12, 13), 1e-12)
	same(t, "RSI3", values(t, "rsi", b, 3)[0], nans(3, 200.0/3, 250.0/3, 100-100/8.25), 1e-9)

	// The population deviation of 10, 11, 12 is √(2/3); a sample's would be 1.
	boll := values(t, "boll", b, 3, 2)
	sd := math.Sqrt(2.0 / 3)
	same(t, "BOLL mid", boll[0], nans(2, 11, 34.0/3), 1e-12)
	same(t, "BOLL up", boll[1], nans(2, 11+2*sd, 12.276142374915397), 1e-9)
	same(t, "BOLL down", boll[2], nans(2, 11-2*sd, 10.390524291751271), 1e-9)

	// MACD 2/3/2: fast EMA 10.5, 11.5, 11.17…; slow 11, 11, 12, 13.
	macd := values(t, "macd", b, 2, 3, 2)
	same(t, "DIF", macd[0], nans(2, 0.5, 1.0/6, 7.0/18, 25.0/54), 1e-9)
	same(t, "DEA", macd[1], nans(3, 1.0/3, 10.0/27, 35.0/81), 1e-9)
	same(t, "MACD hist", macd[2], nans(3, 1.0/6-1.0/3, 7.0/18-10.0/27, 25.0/54-35.0/81), 1e-9)

	// KDJ 3/3/3: RSV 75, 33.3, 100; K and D start from 0.
	kb := []market.Bar{{H: 10, L: 8, C: 9}, {H: 11, L: 9, C: 10}, {H: 12, L: 9, C: 11}, {H: 12, L: 10, C: 10}, {H: 13, L: 11, C: 13}}
	kdj := values(t, "kdj", kb, 3, 3, 3)
	same(t, "K", kdj[0], nans(2, 25, 250.0/9, 1400.0/27), 1e-9)
	same(t, "D", kdj[1], nans(2, 25.0/3, 400.0/27, 2200.0/81), 1e-9)
	same(t, "J", kdj[2], nans(2, 175.0/3, 1450.0/27, 8200.0/81), 1e-9)

	vol := values(t, "vol", []market.Bar{{V: 1}, {V: 2}, {V: 6}}, 2)
	same(t, "VOL", vol[0], []float64{1, 2, 6}, 0)
	same(t, "VOL MA", vol[1], nans(1, 1.5, 4), 1e-12)
	same(t, "VOL without MA", values(t, "vol", []market.Bar{{V: 1}}, 0)[1], nans(1), 0)
}

// MACD 12/26/9 has its first DIF on the 26th bar and its first DEA, the SMA
// of nine DIFs, on the 34th; RSI says 100 with no falls and 0 with no rises;
// KDJ reads a period without range as halfway.
func TestEdges(t *testing.T) {
	b := fakeBars(60, 60_000)
	macd := values(t, "macd", b, 12, 26, 9)
	if !math.IsNaN(macd[0][24]) || math.IsNaN(macd[0][25]) || !math.IsNaN(macd[1][32]) || math.IsNaN(macd[1][33]) {
		t.Fatalf("DIF %v %v, DEA %v %v", macd[0][24], macd[0][25], macd[1][32], macd[1][33])
	}
	var sum float64
	for i := 25; i <= 33; i++ {
		sum += macd[0][i]
	}
	if math.Abs(macd[1][33]-sum/9) > 1e-9 {
		t.Fatalf("the first DEA is %v, want the mean of nine DIFs %v", macd[1][33], sum/9)
	}
	if r := values(t, "rsi", barsOfCloses(1, 2, 3, 4), 2)[0]; r[3] != 100 {
		t.Fatalf("rising only: RSI %v", r[3])
	}
	if r := values(t, "rsi", barsOfCloses(4, 3, 2, 1), 2)[0]; r[3] != 0 {
		t.Fatalf("falling only: RSI %v", r[3])
	}
	if k := values(t, "kdj", barsOfCloses(5, 5, 5), 3, 3, 3)[0]; k[2] != 50.0/3 {
		t.Fatalf("flat: K %v, want 50/3", k[2])
	}
}

func allIndicators() []indicator {
	list := []indicator{}
	for i := range indKinds {
		list = append(list, newIndicator(&indKinds[i], int64(i+1), list))
	}
	return list
}

// A new or changed newest bar is worked out from the bar before it on, and
// comes out as working out all of them does; older bars in front, another
// market or other parameters work out everything.
func TestSeriesUpdatesOnlyTheTail(t *testing.T) {
	all := fakeBars(3001, 60_000)
	bars := append([]market.Bar(nil), all[1000:2999]...)
	for _, ind := range allIndicators() {
		k, _ := kindOf(ind.Kind)
		var s series
		check := func(what string, from int) {
			t.Helper()
			s.update(k, ind.Params, bars, 1)
			if s.from != from {
				t.Fatalf("%s %s: began at %d, want %d", ind.Kind, what, s.from, from)
			}
			var fresh series
			fresh.update(k, ind.Params, bars, 1)
			for j := range k.plots {
				same(t, ind.Kind+" "+what, s.out[j], fresh.out[j], 0)
			}
		}
		check("first", 0)
		n := len(bars)
		bars[n-1].C *= 1.01
		bars[n-1].H = max(bars[n-1].H, bars[n-1].C)
		check("newest bar changed", n-1)
		s.update(k, ind.Params, bars, 1)
		if s.from != n-1 {
			t.Fatal("unchanged bars were worked out again")
		}
		bars = append(bars, all[2999:3001]...)
		check("two new bars", n-1)
		bars = append(append([]market.Bar(nil), all[:1000]...), bars...)
		check("older bars in front", 0)
		s.update(k, ind.Params, bars, 2)
		if s.from != 0 {
			t.Fatalf("%s: another market began at %d", ind.Kind, s.from)
		}
		p := append([]float64(nil), ind.Params...)
		p[0]++
		s.update(k, p, bars, 2)
		if s.from != 0 {
			t.Fatalf("%s: other parameters began at %d", ind.Kind, s.from)
		}
		bars = bars[:len(bars)-2] // as at the start of the next kind
		bars = bars[1000:]
	}
}

// Working out every kind on 3000 bars takes a few milliseconds; a tick of
// the newest bar a few microseconds.
func TestIndicatorsAreQuick(t *testing.T) {
	bars := fakeBars(3000, 60_000)
	list := allIndicators()
	ss := make([]series, len(list))
	t0 := time.Now()
	for i, ind := range list {
		k, _ := kindOf(ind.Kind)
		ss[i].update(k, ind.Params, bars, 1)
	}
	full := time.Since(t0)
	t0 = time.Now()
	const ticks = 200
	for range ticks {
		bars[len(bars)-1].C += 0.5
		for i, ind := range list {
			k, _ := kindOf(ind.Kind)
			ss[i].update(k, ind.Params, bars, 1)
		}
	}
	tick := time.Since(t0) / ticks
	t.Logf("3000 bars, %d indicators: all %v, a tick %v", len(list), full, tick)
	if full > 100*time.Millisecond || tick > 2*time.Millisecond {
		t.Fatalf("all %v, a tick %v", full, tick)
	}
}

func TestFixIndicators(t *testing.T) {
	list := fixIndicators([]indicator{
		{ID: 3, Kind: "ma", Params: []float64{-4}},
		{ID: 3, Kind: "macd", Params: []float64{5}, Colors: []hexColor{{R: 1, A: 255}}, Widths: []float64{9, 2}, Height: 7},
		{ID: 1, Kind: "supertrend"},
		{Kind: "boll", Params: []float64{20, math.NaN()}},
	})
	if len(list) != 3 {
		t.Fatalf("kept %+v", list)
	}
	ma, macd, boll := list[0], list[1], list[2]
	if ma.Params[0] != 1 || len(ma.Colors) != 1 || ma.Widths[0] != 1 || ma.Height != 0 {
		t.Fatalf("MA %+v", ma)
	}
	if macd.ID == ma.ID || boll.ID == ma.ID || boll.ID == macd.ID || boll.ID == 0 {
		t.Fatalf("IDs %d %d %d", ma.ID, macd.ID, boll.ID)
	}
	if len(macd.Params) != 3 || macd.Params[0] != 5 || macd.Params[1] != 26 || len(macd.Colors) != 3 || !macd.Colors[0].set() ||
		macd.Widths[0] != 1 || macd.Widths[1] != 2 || macd.Height != defaultPaneHeight {
		t.Fatalf("MACD %+v", macd)
	}
	if boll.Params[1] != 2 {
		t.Fatalf("BOLL %+v", boll)
	}
}

// Another MA comes as MA10 in another color, and so on.
func TestNewIndicatorTakesTheNextPreset(t *testing.T) {
	k, _ := kindOf("ma")
	var list []indicator
	for i := range 3 {
		list = append(list, newIndicator(k, int64(i+1), list))
	}
	if list[0].Params[0] != 5 || list[1].Params[0] != 10 || list[2].Params[0] != 20 || list[0].Colors[0] == list[1].Colors[0] {
		t.Fatalf("MAs %+v", list)
	}
	rsi, _ := kindOf("rsi")
	if r := newIndicator(rsi, 9, list); r.Params[0] != 14 || r.Height != defaultPaneHeight || r.Colors[0].set() {
		t.Fatalf("RSI %+v", r)
	}
}

// The list is kept in the settings file, for every market; a file from
// before it reads as it did.
func TestIndicatorsAreKept(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	os.WriteFile(old, []byte(`{"Symbol":"ETHUSDT","Interval":"4h","Drawings":{"ETHUSDT":[{"ID":4,"Kind":"hline","Pts":[{"T":1,"P":2}]}]}}`), 0o644)
	s := loadSettings(old)
	if len(s.Indicators) != 0 || s.Symbol != "ETHUSDT" || len(s.Drawings["ETHUSDT"]) != 1 || s.Chart != defaultStyle() {
		t.Fatalf("old file: %+v", s)
	}
	s.Indicators = allIndicators()
	s.Indicators[1].Hidden = true
	file := filepath.Join(dir, "settings.json")
	if err := saveSettings(file, s); err != nil {
		t.Fatal(err)
	}
	back := loadSettings(file)
	if len(back.Indicators) != len(indKinds) || !back.Indicators[1].Hidden || back.Indicators[0].Colors[0] != s.Indicators[0].Colors[0] {
		t.Fatalf("read back %+v", back.Indicators)
	}
}
