package main

import (
	"math"
	"testing"

	"candle/pine"
)

// The Pine interpreter works out the indicators Candle has in Go, which
// match TradingView's, to the same values: its history, its moving
// averages seeded as TradingView's, its state kept by bar.
func TestPineMatchesTheBuiltIns(t *testing.T) {
	bars := fakeBars(600, 3_600_000)
	for _, c := range []struct {
		kind   string
		params []float64
		src    string
	}{
		{"ma", []float64{20}, `indicator("x", overlay = true)
plot(ta.sma(close, 20))`},
		{"ema", []float64{12}, `indicator("x", overlay = true)
plot(ta.ema(close, 12))`},
		{"boll", []float64{20, 2}, `indicator("x", overlay = true)
[m, u, l] = ta.bb(close, 20, 2)
plot(m)
plot(u)
plot(l)`},
		{"macd", []float64{12, 26, 9}, `indicator("x")
[d, s, h] = ta.macd(close, 12, 26, 9)
plot(d)
plot(s)
plot(h)`},
		{"rsi", []float64{14}, `indicator("x")
plot(ta.rsi(close, 14))`},
		{"kdj", []float64{9, 3, 3}, `indicator("x")
// TradingView's KDJ script, as Candle's Go has it
ilong = 9
isig = 3
bcwsma(s, l, m) =>
    _bcwsma = float(na)
    _s = s
    _l = l
    _m = m
    _bcwsma := (_m * _s + (_l - _m) * nz(_bcwsma[1])) / _l
    _bcwsma
c = close
h = ta.highest(high, ilong)
l = ta.lowest(low, ilong)
RSV = 100 * ((c - l) / (h - l))
pK = bcwsma(RSV, isig, 1)
pD = bcwsma(pK, isig, 1)
pJ = 3 * pK - 2 * pD
plot(pK)
plot(pD)
plot(pJ)`},
		{"vol", []float64{20}, `indicator("x")
plot(volume)
plot(ta.sma(volume, 20))`},
	} {
		k, _ := kindOf(c.kind)
		want := make([][]float64, len(k.plots)+k.state)
		for j := range want {
			want[j] = make([]float64, len(bars))
		}
		k.calc(bars, source(bars, ""), c.params, want, 0)
		s, err := pine.Compile(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		m := s.Run(nil, bars, nil, 0, pine.Env{})
		if m.Err != nil {
			t.Fatalf("%s: %v", c.kind, m.Err)
		}
		for j := range k.plots {
			for i := range bars {
				g, w := m.Plots[j][i], want[j][i]
				if math.IsNaN(g) && math.IsNaN(w) {
					continue
				}
				if math.IsNaN(g) != math.IsNaN(w) || math.Abs(g-w) > 1e-9*math.Max(1, math.Abs(w)) {
					// KDJ's Go starts K and D from the first full period: let Pine's start from 0 settle.
					if c.kind == "kdj" && i < 60 {
						continue
					}
					t.Fatalf("%s plot %d bar %d: Pine %v, Go %v", c.kind, j, i, g, w)
				}
			}
		}
	}
}

// What a script draws reaches the chart's kinds: its inputs as parameters
// of the right kinds, its plots, whether it goes over the candles.
func TestScriptKinds(t *testing.T) {
	kc, _ := kindOf("kc")
	if kc.pane || kc.script == nil || kc.category != "通道" || len(kc.plots) != 3 {
		t.Fatalf("KC %+v", kc)
	}
	var kinds []int
	for _, p := range kc.params {
		kinds = append(kinds, p.kind)
	}
	if want := []int{paramNumber, paramNumber, paramChoice, paramBool, paramChoice, paramNumber}; !equalInts(kinds, want) {
		t.Fatalf("KC's parameters are %v, want %v", kinds, want)
	}
	st, _ := kindOf("supertrend")
	if !st.plots[2].hidden || st.plots[0].hidden {
		t.Fatalf("SuperTrend's plots %+v", st.plots)
	}
	obv, _ := kindOf("obv")
	if !obv.pane || obv.decimals != -2 {
		t.Fatalf("OBV %+v", obv)
	}
	for _, sh := range shippedScripts {
		if k, ok := kindOf(sh.file); !ok || k.short == "" || k.category == "" {
			t.Errorf("%s: %+v", sh.file, k)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
