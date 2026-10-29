package main

import (
	"math"
	"slices"
	"testing"
	"time"

	"candle/market"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestZoomKeepsTheBarUnderThePointer(t *testing.T) {
	v := defaultView()
	const w, n = 900, 1000
	at := v.index(300, w, n)
	v.zoom(1.5, 300, w, n)
	if got := v.index(300, w, n); !near(got, at) || v.spacing != 12 {
		t.Fatalf("index %v spacing %v, want %v and 12", got, v.spacing, at)
	}
}

func TestPanAndClamp(t *testing.T) {
	v := defaultView()
	const w, n = 800, 1000
	v.pan(80)
	if !near(v.offset, -4) {
		t.Fatalf("panning 10 bars right left the offset at %v", v.offset)
	}
	v.pan(1e6)
	v.clamp(w, n)
	if from, to := v.span(w, n); from != 0 || to < keepBars-1 {
		t.Fatalf("scrolled to the first bar, %d..%d is in sight", from, to)
	}
	v.pan(-1e6)
	v.clamp(w, n)
	if from, to := v.span(w, n); to != n-1 || to-from+1 < keepBars {
		t.Fatalf("scrolled past the newest bar, %d..%d is in sight", from, to)
	}
}

func TestNiceStep(t *testing.T) {
	for _, c := range []struct {
		span float64
		n    int
		want float64
	}{{1000, 10, 100}, {3, 10, 0.5}, {7, 3, 2.5}, {0.000123, 5, 0.000025}, {1450, 12, 200}} {
		if got := niceStep(c.span, c.n); math.Abs(got-c.want) > c.want*1e-9 {
			t.Errorf("niceStep(%v, %d) = %v, want %v", c.span, c.n, got, c.want)
		}
	}
}

func TestTimeTicks(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	bars := func(start time.Time, n int, iv time.Duration) []market.Bar {
		out := make([]market.Bar, n)
		for i := range out {
			out[i].T = start.Add(time.Duration(i) * iv).UnixMilli()
		}
		return out
	}
	texts := func(ts []tick) (s []string) {
		for _, t := range ts {
			s = append(s, t.text)
		}
		return s
	}
	// Hourly bars 20 DIPs apart: 6-hour steps, the day at midnight.
	hourly := bars(time.Date(2026, 10, 25, 20, 0, 0, 0, loc), 12, time.Hour)
	if got := timeTicks(hourly, 0, 11, 3_600_000, 20, loc, false); !slices.Equal(texts(got), []string{"26日", "06:00"}) || got[0].i != 4 || got[0].level != 2 {
		t.Fatalf("hourly ticks %+v", got)
	}
	// Zoomed far out, hourly bars still get a label every few days.
	week := bars(time.Date(2026, 10, 18, 0, 0, 0, 0, loc), 200, time.Hour)
	if got := timeTicks(week, 0, 199, 3_600_000, 2.5, loc, false); len(got) < 3 {
		t.Fatalf("8 days of hourly bars got %d labels: %+v", len(got), got)
	}
	// Daily bars, opening at 08:00 here, 6 DIPs apart: half months.
	daily := bars(time.Date(2026, 9, 20, 8, 0, 0, 0, loc), 60, 24*time.Hour)
	if got := texts(timeTicks(daily, 0, 59, 86_400_000, 6, loc, false)); !slices.Equal(got, []string{"10月", "16日", "11月", "16日"}) {
		t.Fatalf("daily ticks %q", got)
	}
}

func TestMerge(t *testing.T) {
	bar := func(t int64, c float64) market.Bar { return market.Bar{T: t, O: c, H: c, L: c, C: c} }
	got, added := merge([]market.Bar{bar(1, 1), bar(2, 2), bar(3, 3)}, []market.Bar{bar(3, 30), bar(4, 4)})
	if added != 1 || len(got) != 4 || got[2].C != 30 || got[3].T != 4 {
		t.Fatalf("got %+v added %d", got, added)
	}
	got, added = merge(got, []market.Bar{bar(0, 0), bar(2, 20)})
	if added != 0 || len(got) != 5 || got[0].T != 0 || got[2].C != 20 {
		t.Fatalf("got %+v added %d", got, added)
	}
}

func TestNewBarsMoveTheViewOnlyAtTheNewestBar(t *testing.T) {
	a := testApp()
	const w = 900
	next := func() []market.Bar {
		last := a.bars[len(a.bars)-1]
		return []market.Bar{{T: last.T + 3_600_000, O: 1, H: 1, L: 1, C: 1}}
	}
	a.view.offset = -50
	at := a.view.index(300, w, len(a.bars))
	a.addBars(next())
	if got := a.view.index(300, w, len(a.bars)); !near(got, at) {
		t.Fatalf("looking back, a new bar moved the view from bar %v to %v", at, got)
	}
	a.view.offset = 6
	a.addBars(next())
	if a.view.offset != 6 {
		t.Fatalf("at the newest bar the view should follow it, offset %v", a.view.offset)
	}
}

func TestPrices(t *testing.T) {
	s := prices{lo: 100, hi: 200}
	m := axis{h: 500, top: 0.08, bottom: 0.78} // a band 350 DIPs tall
	if got := s.at(s.y(150, m), m); math.Abs(got-150) > 1e-9 {
		t.Fatalf("at(y(150)) = %v", got)
	}
	p := s.at(120, m)
	s.zoom(2, p, m)
	if got := s.at(120, m); math.Abs(got-p) > 1e-9 || math.Abs(s.hi-s.lo-200) > 1e-9 {
		t.Fatalf("zooming moved the anchor to %v or got a span of %v", got, s.hi-s.lo)
	}
	s.pan(35, m)                                          // a tenth of the band: a tenth of the 200 span
	if got := s.at(120, m); math.Abs(got-(p+20)) > 1e-5 { // float32 DIPs
		t.Fatalf("after panning 35 DIPs down, %v at the same height, want %v", got, p+20)
	}
	ref := max(math.Abs(s.lo), math.Abs(s.hi))
	s.zoom(1e-9, s.lo, m)
	if span := s.hi - s.lo; math.Abs(span-ref*1e-5) > ref*1e-12 {
		t.Fatalf("zoomed all the way in, the span is %v, want %v", span, ref*1e-5)
	}
}

// On a log scale equal ratios take equal heights, and zooming and panning
// keep to that.
func TestPricesLog(t *testing.T) {
	s := prices{lo: 1000, hi: 100000}
	m := axis{h: 500, top: 0.1, bottom: 0.9, log: true}
	if a, b := s.y(1000, m)-s.y(10000, m), s.y(10000, m)-s.y(100000, m); math.Abs(float64(a-b)) > 1e-3 {
		t.Fatalf("a decade takes %v DIPs at the bottom and %v at the top", a, b)
	}
	p := s.at(200, m)
	s.zoom(0.5, p, m)
	if got := s.at(200, m); math.Abs(got/p-1) > 1e-6 || math.Abs(math.Log10(s.hi/s.lo)-1) > 1e-9 {
		t.Fatalf("zooming in moved the anchor %v to %v, or spans %v decades", p, got, math.Log10(s.hi/s.lo))
	}
	s.pan(40, m) // a tenth of the band: a tenth of a decade
	if got := s.at(200, m); math.Abs(math.Log10(got/p)-0.1) > 1e-5 {
		t.Fatalf("panning 40 DIPs down moved %v to %v", p, got)
	}
	if s.zoom(1e9, p, m); math.Log10(s.hi/s.lo) > 8+1e-9 || s.lo <= 0 {
		t.Fatalf("zoomed all the way out: %v to %v", s.lo, s.hi)
	}
}

func TestPriceTicks(t *testing.T) {
	if got := priceTicks(78000, 80500, 5, false); !slices.Equal(got, []float64{78000, 78500, 79000, 79500, 80000, 80500}) {
		t.Fatalf("linear: %v", got)
	}
	if got := priceTicks(3000, 120000, 6, true); !slices.Equal(got, []float64{5000, 10000, 20000, 50000, 100000}) {
		t.Fatalf("log, 1.6 decades: %v", got)
	}
	if got := priceTicks(3000, 3e7, 5, true); !slices.Equal(got, []float64{1e4, 1e5, 1e6, 1e7}) {
		t.Fatalf("log, many decades: %v", got)
	}
	// Within a factor of two a log scale takes even steps.
	if got := priceTicks(70000, 80000, 6, true); !slices.Equal(got, []float64{70000, 72000, 74000, 76000, 78000, 80000}) {
		t.Fatalf("log, narrow: %v", got)
	}
	for step, want := range map[float64]int{1: 0, 0.5: 1, 2.5: 1, 0.25: 2, 0.000025: 6} {
		if got := stepDecimals(step); got != want {
			t.Errorf("stepDecimals(%v) = %d, want %d", step, got, want)
		}
	}
}

func TestCrossTime(t *testing.T) {
	ms := time.Date(2026, 10, 26, 4, 15, 0, 0, time.UTC).UnixMilli() // Monday
	st := defaultStyle()
	st.Zone = "Asia/Shanghai"
	if got := st.crossTime(ms, 3_600_000); got != "周一 2026-10-26 12:15" {
		t.Fatalf("default: %q", got)
	}
	st.Weekday, st.DateFormat, st.Clock12, st.Zone = false, "2006年1月2日", true, "America/New_York"
	if got := st.crossTime(ms, 3_600_000); got != "2026年10月26日 上午12:15" {
		t.Fatalf("New York, 12 hours: %q", got)
	}
	if got := st.crossTime(ms, 86_400_000); got != "2026年10月26日" {
		t.Fatalf("a daily bar: %q", got)
	}
}

func TestCountdown(t *testing.T) {
	for ms, want := range map[int64]string{59_500: "00:59", 3_725_000: "01:02:05", 2*86_400_000 + 3*3_600_000: "2天03时", -5: "00:00"} {
		if got := countdown(ms); got != want {
			t.Errorf("countdown(%d) = %q, want %q", ms, got, want)
		}
	}
}

func TestDecimals(t *testing.T) {
	for tick, want := range map[float64]int{0.01: 2, 1e-8: 8, 1: 0, 10: 0, 0.1: 1, 0.25: 2, 0.005: 3} {
		if got := decimals(tick); got != want {
			t.Errorf("decimals(%v) = %d, want %d", tick, got, want)
		}
	}
	for p, want := range map[float64]int{79703: 2, 2.31: 4, 0.0000041: 10, 0: 2} {
		if got := autoDecimals(p); got != want {
			t.Errorf("autoDecimals(%v) = %d, want %d", p, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	all := []market.Ticker{
		{Symbol: "ETHBTC", QuoteVolume: 1e3}, {Symbol: "BTTCUSDT", QuoteVolume: 1e7}, {Symbol: "BTCFDUSD", QuoteVolume: 5e8},
		{Symbol: "WBTCUSDT", QuoteVolume: 1e8}, {Symbol: "BTCUSDT", QuoteVolume: 9e9},
	}
	names := func(ts []market.Ticker) (s []string) {
		for _, t := range ts {
			s = append(s, t.Symbol)
		}
		return s
	}
	if got := names(searchAll("btc", all, nil, nil, 10)); !slices.Equal(got, []string{"BTCUSDT", "BTCFDUSD", "WBTCUSDT", "ETHBTC"}) {
		t.Fatalf("btc: %q", got)
	}
	if got := names(searchAll(" btc/usdt", all, nil, nil, 1)); !slices.Equal(got, []string{"BTCUSDT"}) {
		t.Fatalf("btc/usdt: %q", got)
	}
}

// A scale across zero reads 0 there, not -0.
func TestPriceTicksZero(t *testing.T) {
	for _, v := range priceTicks(-12, 110, 4, false) {
		if v == 0 && math.Signbit(v) {
			t.Fatalf("%v holds -0, which reads %q", priceTicks(-12, 110, 4, false), formatPrice(v, 0))
		}
	}
}
