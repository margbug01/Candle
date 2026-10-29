package pine

import (
	"math"
	"strings"
	"testing"

	"candle/market"
)

func run(t *testing.T, src string, bars []market.Bar) (*Script, *Machine) {
	t.Helper()
	s, err := Compile(src)
	if err != nil {
		e := err.(*Error)
		t.Fatalf("line %d: %v", Line(src, e.Start), err)
	}
	m := s.Run(nil, bars, nil, 0, Env{IntervalMs: 3_600_000, Period: "60", DayOf: func(t int64) int64 { return t / 86_400_000 }})
	if m.Err != nil {
		t.Fatal(m.Err)
	}
	return s, m
}

func closes(cs ...float64) []market.Bar {
	bars := make([]market.Bar, len(cs))
	for i, c := range cs {
		bars[i] = market.Bar{T: int64(i) * 3_600_000, O: c, H: c + 1, L: c - 1, C: c, V: 10 * float64(i+1)}
	}
	return bars
}

func near(a, b float64) bool {
	return math.IsNaN(a) && math.IsNaN(b) || math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b))
}

func checkPlot(t *testing.T, m *Machine, j int, want ...float64) {
	t.Helper()
	got := m.Plots[j]
	for i, w := range want {
		if !near(got[i], w) {
			t.Fatalf("plot %d: %v, want %v", j, got[:len(want)], want)
		}
	}
}

var na = math.NaN()

// Errors name what is wrong, on the line it is on.
func TestErrors(t *testing.T) {
	for _, c := range []struct{ src, msg string }{
		{"plot(close)", "缺少 indicator"},
		{"indicator(\"x\")\nplot(ta.nosuch(close))", "2:Candle 不认识函数 ta.nosuch"},
		{"indicator(\"x\")\nx = request.security(\"AAPL\", \"D\", close)", "2:别的品种"},
		{"strategy(\"x\")\nplot(close)", "1:策略"},
		{"indicator(\"x\")\nvar a = array.new<float>()", "2:array"},
		{"indicator(\"x\")\n    plot(close)", "2:缩进"},
		{"indicator(\"x\")\ns = \"abc", "2:引号"},
		{"indicator(\"x\")\nx := 1", "2:还没声明"},
		{"indicator(\"x\")\nx = 1\nx = 2", "3:已经声明过"},
		{"indicator(\"x\")\nif close > open\n    plot(close)", "3:最外层"},
		{"indicator(\"x\")\nhline(close)", "2:hline 的价格"},
		{"indicator(\"x\")\n[a, b] = ta.macd(close, 12, 26, 9)", "2:3 个值"},
		{"indicator(\"x\")\nf(x) => f(x)\nplot(f(close))", "递归"},
		{"indicator(\"x\")\nplot(close + \"a\")", "2:文字和数值"},
		{"indicator(\"x\")\nplot(ta.sma(close))", "2:少了参数"},
		{"indicator(\"x\")\nplot(close", "2:少了 )"},
		{"indicator(\"x\")\nimport foo/bar/1", "2:import"},
		{"indicator(\"x\")\nbreak", "2:循环"},
	} {
		_, err := Compile(c.src)
		if err == nil {
			t.Errorf("%q compiled", c.src)
			continue
		}
		e := err.(*Error)
		msg := e.Msg
		if line, rest, ok := strings.Cut(c.msg, ":"); ok && len(line) == 1 {
			if got := Line(c.src, e.Start); string(rune('0'+got)) != line {
				t.Errorf("%q: error on line %d, want %s (%s)", c.src, got, line, msg)
			}
			c.msg = rest
		}
		if !strings.Contains(msg, c.msg) {
			t.Errorf("%q: %q, want %q", c.src, msg, c.msg)
		}
	}
}

// What Candle does not draw is skipped, with a warning, not an error.
func TestSkipsDrawings(t *testing.T) {
	s, m := run(t, `//@version=5
indicator("x", overlay = true)
var label lb = na
lb := label.new(bar_index, high, str.format("{0}", close), style = label.style_label_down)
label.delete(lb[1])
lb.set_text("a")
var table tb = table.new(position.top_right, 2, 2)
plot(close)
alertcondition(close > open, "up")
barcolor(close > open ? color.green : na)
`, closes(1, 2, 3))
	if len(s.Warnings) == 0 || !strings.Contains(s.Warnings[0].Msg, "label") {
		t.Fatalf("warnings %v", s.Warnings)
	}
	checkPlot(t, m, 0, 1, 2, 3)
}

// Scripts of version 4 run: study, the functions without their namespaces,
// input's type.
func TestVersion4(t *testing.T) {
	s, m := run(t, `//@version=4
study("v4")
len = input(2, title = "Length", type = input.integer)
plot(sma(close, len))
plot(iff(close > 2, 1, 0))
plot(tr)
`, closes(1, 2, 3, 5))
	if s.Inputs[0].Kind != "int" || s.Inputs[0].Title != "Length" {
		t.Fatalf("inputs %+v", s.Inputs)
	}
	checkPlot(t, m, 0, na, 1.5, 2.5, 4)
	checkPlot(t, m, 1, 0, 0, 1, 1)
	checkPlot(t, m, 2, na, 2, 2, 3)
}

// The language: var, history, functions with their own state, if, switch,
// for, while, tuples, strings.
func TestLanguage(t *testing.T) {
	_, m := run(t, `//@version=6
indicator("x")
mode = input.string("sum", "模式", options = ["sum", "max"])
var float total = 0
total += close
var string s = ""
s := s + "a"
ago(x) => x[1]
f(src, n = 2) =>
    acc = 0.0
    for i = 0 to n - 1
        acc += src[i]
    acc
g = switch mode
    "sum" => f(close)
    "max" => math.max(close, close[1])
    => na
k = 0
while k < 10
    k += 1
    if k == 3
        break
odd = 0
for i = 1 to 9 by 2
    if i == 5
        continue
    odd += i
[a, b] = if close > 2
    [close, 1]
else
    [na, 0]
plot(total)
plot(ago(close))
plot(g)
plot(k)
plot(odd)
plot(a)
plot(b)
plot(str.length(s))
plot((close - open)[1])
plot(nz(ago(close), -1))
plot(fixnan(close > 2 ? close : na))
plot(5 / 2)
plot(math.round(2.345, 2))
`, closes(1, 2, 3, 4))
	checkPlot(t, m, 0, 1, 3, 6, 10)
	checkPlot(t, m, 1, na, 1, 2, 3)
	checkPlot(t, m, 2, na, 3, 5, 7)
	checkPlot(t, m, 3, 3, 3, 3, 3)
	checkPlot(t, m, 4, 20, 20, 20, 20)
	checkPlot(t, m, 5, na, na, 3, 4)
	checkPlot(t, m, 6, 0, 0, 1, 1)
	checkPlot(t, m, 8, na, 0, 0, 0)
	checkPlot(t, m, 9, -1, 1, 2, 3)
	checkPlot(t, m, 10, na, na, 3, 4)
	checkPlot(t, m, 11, 2.5)
	checkPlot(t, m, 12, 2.35)
}

// Each call of a function keeps its own history: two moving averages in one
// function, called twice, are four.
func TestCallsKeepTheirOwnState(t *testing.T) {
	_, m := run(t, `indicator("x")
ma(src) => ta.sma(src, 2)
plot(ma(close))
plot(ma(close * 10))
`, closes(1, 3, 5))
	checkPlot(t, m, 0, na, 2, 4)
	checkPlot(t, m, 1, na, 20, 40)
}

// The ta functions, against what their definitions work out by hand.
func TestTa(t *testing.T) {
	bars := closes(5, 3, 8, 6, 9, 9, 4)
	_, m := run(t, `indicator("x")
plot(ta.wma(close, 3))
plot(ta.stdev(close, 3))
plot(ta.stdev(close, 3, false))
plot(ta.highestbars(close, 3))
plot(ta.lowestbars(close, 3))
plot(ta.pivothigh(close, 1, 1))
plot(ta.crossover(close, 6.5) ? 1 : 0)
plot(ta.barssince(close > 8))
plot(ta.valuewhen(close < 5, bar_index, 0))
plot(ta.cum(close))
plot(ta.change(close, 2))
plot(ta.vwma(close, 2))
plot(ta.atr(2))
plot(ta.roc(close, 1))
plot(ta.rising(close, 1) ? 1 : 0)
`, bars)
	wma := func(a, b, c float64) float64 { return (a + 2*b + 3*c) / 6 }
	checkPlot(t, m, 0, na, na, wma(5, 3, 8), wma(3, 8, 6), wma(8, 6, 9), wma(6, 9, 9), wma(9, 9, 4))
	sd := func(xs ...float64) (float64, float64) {
		mu := (xs[0] + xs[1] + xs[2]) / 3
		s := 0.0
		for _, x := range xs {
			s += (x - mu) * (x - mu)
		}
		return math.Sqrt(s / 3), math.Sqrt(s / 2)
	}
	b1, u1 := sd(5, 3, 8)
	checkPlot(t, m, 1, na, na, b1)
	checkPlot(t, m, 2, na, na, u1)
	// The newest of equal highs: 9, 9 at bars 4 and 5.
	checkPlot(t, m, 3, na, na, 0, -1, 0, 0, -1)
	checkPlot(t, m, 4, na, na, -1, -2, -1, -2, 0)
	checkPlot(t, m, 5, na, na, na, 8, na, na, na)
	checkPlot(t, m, 6, 0, 0, 1, 0, 1, 0, 0)
	checkPlot(t, m, 7, na, na, na, na, 0, 0, 1)
	checkPlot(t, m, 8, na, 1, 1, 1, 1, 1, 6)
	checkPlot(t, m, 9, 5, 8, 16, 22, 31, 40, 44)
	checkPlot(t, m, 10, na, na, 3, 3, 1, 3, -5)
	checkPlot(t, m, 11, na, (5*10+3*20)/30.0)
	// TR: high - low (2) on the first bar, then the gap to the close before.
	trs := []float64{2, 3, 6, 3, 4, 1, 6}
	checkPlot(t, m, 12, na, (trs[0]+trs[1])/2, ((trs[0]+trs[1])/2+trs[2])/2)
	checkPlot(t, m, 13, na, -40, 100*5/3.0)
	checkPlot(t, m, 14, 0, 0, 1, 0, 1, 0, 0)
}

// VWAP starts again with each trading day, and its bands are the standard
// deviation of the prices about it, by volume.
func TestVWAP(t *testing.T) {
	bars := closes(10, 20, 30, 40)
	for i := range bars {
		bars[i].T = int64(i) * 43_200_000 // two bars a day
		bars[i].H, bars[i].L = bars[i].C, bars[i].C
	}
	_, m := run(t, `indicator("x", overlay = true)
plot(ta.vwap)
[v, u, l] = ta.vwap(close, timeframe.change("D"), 1)
plot(u)
`, bars)
	w := (10*10 + 20*20) / 30.0
	sd := math.Sqrt((10*10*10+20*20*20)/30.0 - w*w)
	checkPlot(t, m, 0, 10, w, 30, (30*30+40*40)/70.0)
	checkPlot(t, m, 1, 10, w+sd)
}

// SAR and SuperTrend turn with the trend: below a rising market, above a
// falling one.
func TestTrendFollowers(t *testing.T) {
	var cs []float64
	for i := 0; i < 40; i++ {
		cs = append(cs, 100+float64(i))
	}
	for i := 0; i < 40; i++ {
		cs = append(cs, 140-float64(i)*2)
	}
	bars := closes(cs...)
	_, m := run(t, `indicator("x", overlay = true)
plot(ta.sar(0.02, 0.02, 0.2))
[st, dir] = ta.supertrend(3, 10)
plot(st)
plot(dir)
`, bars)
	if v := m.Plots[0][30]; !(v < bars[30].L) {
		t.Fatalf("SAR %v above a rising market (low %v)", v, bars[30].L)
	}
	if v := m.Plots[0][75]; !(v > bars[75].H) {
		t.Fatalf("SAR %v below a falling market (high %v)", v, bars[75].H)
	}
	if m.Plots[2][30] != -1 || m.Plots[2][75] != 1 {
		t.Fatalf("SuperTrend's direction %v then %v", m.Plots[2][30], m.Plots[2][75])
	}
	if !(m.Plots[1][30] < bars[30].C) || !(m.Plots[1][75] > bars[75].C) {
		t.Fatal("SuperTrend on the wrong side")
	}
}

// What a script draws: colors by bar, offsets, levels, fills, backgrounds,
// shapes, plots worked out but not drawn.
func TestOutputs(t *testing.T) {
	s, m := run(t, `indicator("x", precision = 3)
lvl = input.int(50, "线")
p1 = plot(close, "c", color = close > 2 ? color.green : color.red, offset = 2, linewidth = 3)
p2 = plot(open, "o", display = display.none)
h = hline(lvl, "h", color = #112233, linestyle = hline.style_dotted)
fill(p1, p2, color = color.new(color.blue, 80))
bgcolor(close > 2 ? color.yellow : na)
plotshape(close > 2, "s", shape.triangleup, location.belowbar, color.lime, text = "买")
`, closes(1, 2, 3))
	if s.Precision != 3 || s.Plots[0].Width != 3 || !s.Plots[0].Dynamic || !s.Plots[1].Hidden || s.Hlines[0].Style != LineDotted {
		t.Fatalf("script %+v", s)
	}
	if m.PlotOffset[0] != 2 || m.HlinePrice[0] != 50 || s.Hlines[0].Color != 0x112233ff {
		t.Fatalf("offset %v, level %v", m.PlotOffset, m.HlinePrice)
	}
	if uint32(m.PlotColors[0][0]) != colors["red"] || uint32(m.PlotColors[0][2]) != colors["green"] {
		t.Fatalf("colors %v", m.PlotColors[0])
	}
	if _, _, _, a, _ := Unpack(m.FillColors[0][1]); a != 51 {
		t.Fatalf("fill alpha %d", a)
	}
	if !math.IsNaN(m.BgColors[0][0]) || uint32(m.BgColors[0][2]) != colors["yellow"] {
		t.Fatalf("backgrounds %v", m.BgColors[0])
	}
	if m.ShapeVals[0][1] != 0 || m.ShapeVals[0][2] != 1 || s.Shapes[0].Text != "买" || s.Shapes[0].Location != LocBelowBar {
		t.Fatalf("shapes %v %+v", m.ShapeVals[0], s.Shapes[0])
	}
	// The input set otherwise moves the level.
	m = s.Run(nil, closes(1, 2, 3), []float64{70}, 0, Env{})
	if m.HlinePrice[0] != 70 {
		t.Fatalf("level %v", m.HlinePrice[0])
	}
}

// A loop that does not end is stopped, with an error, not a hang.
func TestEndlessLoop(t *testing.T) {
	s, err := Compile("indicator(\"x\")\nk = 0\nwhile true\n    k += 1\nplot(k)")
	if err != nil {
		t.Fatal(err)
	}
	m := s.Run(nil, closes(1, 2), nil, 0, Env{})
	if m.Err == nil || !strings.Contains(m.Err.Error(), "循环") {
		t.Fatalf("error %v", m.Err)
	}
}
