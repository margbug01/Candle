package main

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"candle/market"
	"candle/pine"
)

// Technical indicators, after TradingView's: MA, EMA and BOLL over the
// candles, MACD, RSI, KDJ and volume in panes of their own below them. The
// settings keep one list for every market; each one's values are kept and
// brought up to date from the newest bar on.

// How a plot draws its values.
const (
	plotLine   = iota
	plotHist   // bars up or down from zero, as MACD's histogram
	plotVolume // bars up from the pane's bottom, rising or falling as the candles
)

// The colors of the bars, of the position boxes and of the price range,
// TradingView's: of their own, not the candles', which can be lost on the
// background (white rising candles on white).
const (
	barUp   = "#26a69a"
	barDown = "#ef5350"
)

type plotDef struct {
	name  string // as the legend and the settings name it
	color string // the default; a bar plot's above zero, or rising
	down  string // a bar plot's below zero, or falling
	style int

	// A script's plots: their width, whether they are worked out only
	// (display.none), and whether their color changes from bar to bar.
	width           float64
	hidden, dynamic bool
}

func line(name, color string) plotDef { return plotDef{name: name, color: color, style: plotLine} }

func pnum(name string, def, lo, hi, step float64) paramDef {
	return paramDef{name: name, def: def, lo: lo, hi: hi, step: step}
}

type paramDef struct {
	name              string
	def, lo, hi, step float64
	kind              int      // paramNumber…
	opts              []option // a choice's, keyed by place
	tip               string
}

// What a parameter is.
const (
	paramNumber    = iota
	paramBool      // 1 or 0
	paramChoice    // the place of one of opts
	paramColor     // a color packed as pine.Pack does
	paramFixed     // a script's text input without choices: not set
	paramNumChoice // a number, one of opts, keyed by its text
)

// indKind is a kind of indicator.
type indKind struct {
	key, short, name string
	alias            string // more words the search finds it by
	pane             bool   // drawn in a pane of its own, else over the candles
	params           []paramDef
	presets          []float64 // first params, one per instance, so that MA comes as MA5, MA10…
	plots            []plotDef
	state            int       // arrays of working values kept after the plots'
	levels           []float64 // dashed lines across the pane
	band             bool      // the space between the two levels shaded
	fill             []int     // two plots the space between is shaded
	decimals         int       // the values': -1 the prices', -2 volumes, else fixed
	source, offset   bool      // what it is worked out from can be picked; it can be drawn shifted
	calc             func(b []market.Bar, src func(int) float64, p []float64, out [][]float64, from int)

	category string       // the section of the list of indicators it is in
	script   *pine.Script // a Pine script's, which works it out in place of calc
	custom   string       // a script of the user's: its file; else ""
	broken   string       // why a script of the user's cannot run: it has an error, or is gone
	rev      int64        // a script of the user's: its file's time and size when read
}

// The sections of the list of indicators, in order.
var categories = []string{"自定义", "均线", "通道", "趋势", "震荡", "波动", "成交量"}

// sources are what a kind can be worked out from, after TradingView's.
var sources = []option{{"", "收盘价"}, {"open", "开盘价"}, {"high", "最高价"}, {"low", "最低价"},
	{"hl2", "(高+低)/2"}, {"hlc3", "(高+低+收)/3"}, {"ohlc4", "(开+高+低+收)/4"}}

// source is bar i's value as key picks it.
func source(b []market.Bar, key string) func(int) float64 {
	switch key {
	case "open":
		return func(i int) float64 { return b[i].O }
	case "high":
		return func(i int) float64 { return b[i].H }
	case "low":
		return func(i int) float64 { return b[i].L }
	case "hl2":
		return func(i int) float64 { return (b[i].H + b[i].L) / 2 }
	case "hlc3":
		return func(i int) float64 { return (b[i].H + b[i].L + b[i].C) / 3 }
	case "ohlc4":
		return func(i int) float64 { return (b[i].O + b[i].H + b[i].L + b[i].C) / 4 }
	}
	return func(i int) float64 { return b[i].C }
}

var indKinds = []indKind{
	{key: "ma", short: "MA", name: "移动平均线", alias: "均线 SMA", decimals: -1, source: true, offset: true, category: "均线",
		params:  []paramDef{pnum("周期", 5, 1, 1000, 1)},
		presets: []float64{5, 10, 20, 30, 60, 120},
		plots:   []plotDef{line("MA", "#f0b90b")}, calc: calcMA},
	{key: "ema", short: "EMA", name: "指数移动平均线", alias: "均线", decimals: -1, source: true, offset: true, category: "均线",
		params:  []paramDef{pnum("周期", 12, 1, 1000, 1)},
		presets: []float64{12, 26, 50, 100, 200},
		plots:   []plotDef{line("EMA", "#f0b90b")}, calc: calcEMA},
	{key: "boll", short: "BOLL", name: "布林带", alias: "BB Bollinger", decimals: -1, source: true, offset: true, category: "通道",
		params: []paramDef{pnum("周期", 20, 1, 500, 1), pnum("倍数", 2, 0.1, 10, 0.1)},
		plots:  []plotDef{line("中轨", "#ff6d00"), line("上轨", "#2962ff"), line("下轨", "#2962ff")},
		fill:   []int{1, 2}, calc: calcBOLL},
	{key: "macd", short: "MACD", name: "指数平滑异同移动平均线", pane: true, decimals: -1, source: true, category: "震荡",
		params: []paramDef{pnum("快线", 12, 1, 500, 1), pnum("慢线", 26, 1, 500, 1), pnum("信号线", 9, 1, 500, 1)},
		plots:  []plotDef{line("DIF", "#2962ff"), line("DEA", "#ff6d00"), {name: "柱", color: barUp, down: barDown, style: plotHist}},
		state:  2, levels: []float64{0}, calc: calcMACD},
	{key: "rsi", short: "RSI", name: "相对强弱指数", pane: true, decimals: 2, source: true, category: "震荡",
		params: []paramDef{pnum("周期", 14, 1, 500, 1)},
		plots:  []plotDef{line("RSI", "#7e57c2")},
		state:  2, levels: []float64{70, 30}, band: true, calc: calcRSI},
	{key: "kdj", short: "KDJ", name: "随机指标", alias: "Stochastic", pane: true, decimals: 2, category: "震荡",
		params: []paramDef{pnum("周期", 9, 1, 500, 1), pnum("K 平滑", 3, 1, 100, 1), pnum("D 平滑", 3, 1, 100, 1)},
		plots:  []plotDef{line("K", "#2962ff"), line("D", "#ff9800"), line("J", "#e040fb")},
		levels: []float64{80, 20}, calc: calcKDJ},
	{key: "vol", short: "VOL", name: "成交量", alias: "Volume", pane: true, decimals: -2, category: "成交量",
		params: []paramDef{pnum("均线周期", 20, 0, 500, 1)},
		plots:  []plotDef{{name: "成交量", color: barUp, down: barDown, style: plotVolume}, line("均线", "#2962ff")}, calc: calcVOL},
}

// lineColors take turns among the instances of a one-line kind.
var lineColors = []string{"#f0b90b", "#e040fb", "#2962ff", "#00bcd4", "#ff7043", "#66bb6a"}

// kindOf is the kind keyed key: built in, or a script of the user's. A
// script of the user's that is gone is a kind that draws nothing, so that
// the indicator keeps its settings till it comes back.
func kindOf(key string) (*indKind, bool) {
	for i := range indKinds {
		if indKinds[i].key == key {
			return &indKinds[i], true
		}
	}
	if k := customKind(key); k != nil {
		return k, true
	}
	return nil, false
}

// indicator is one on the chart, as settings.json keeps it.
type indicator struct {
	ID     int64
	Kind   string
	Params []float64
	Colors []hexColor // by plot; unset, the kind's
	Downs  []hexColor // by plot, a bar plot's second color
	Widths []float64  // by plot
	Hidden bool
	Height float64 `json:",omitempty"` // a pane's share of the chart's height
	Source string  `json:",omitempty"` // a key of sources, "" the close
	Offset float64 `json:",omitempty"` // bars the plots are drawn to the right
}

// fit is v as the parameter can take it.
func (pd paramDef) fit(v float64) float64 {
	switch pd.kind {
	case paramBool:
		if v != 0 {
			return 1
		}
		return 0
	case paramChoice:
		return min(max(math.Round(v), 0), float64(len(pd.opts)-1))
	case paramColor:
		if _, _, _, _, ok := pine.Unpack(v); ok {
			return v
		}
		return pd.def
	case paramFixed:
		return pd.def
	case paramNumChoice:
		if slices.ContainsFunc(pd.opts, func(o option) bool { return o.key == strconv.FormatFloat(v, 'f', -1, 64) }) {
			return v
		}
		return pd.def
	}
	return min(max(v, pd.lo), pd.hi)
}

const (
	defaultPaneHeight = 0.2
	maxLineWidth      = 4
	maxOffset         = 500
)

// fix fits what a damaged or older file holds to the kind, and reports
// whether the kind is one this program knows.
func (ind *indicator) fix() bool {
	k, ok := kindOf(ind.Kind)
	if !ok {
		return false
	}
	if k.broken != "" { // kept as it is till the script runs again
		if ind.Height < 0.02 || ind.Height > 0.9 || math.IsNaN(ind.Height) {
			ind.Height = defaultPaneHeight
		}
		return true
	}
	ps := make([]float64, len(k.params))
	for j, pd := range k.params {
		ps[j] = pd.def
		if j < len(ind.Params) && !math.IsNaN(ind.Params[j]) {
			ps[j] = pd.fit(ind.Params[j])
		}
	}
	ind.Params = ps
	cs, ds, ws := make([]hexColor, len(k.plots)), make([]hexColor, len(k.plots)), make([]float64, len(k.plots))
	for j := range k.plots {
		if j < len(ind.Colors) {
			cs[j] = ind.Colors[j]
		}
		if j < len(ind.Downs) {
			ds[j] = ind.Downs[j]
		}
		ws[j] = min(max(math.Round(k.plots[j].width), 1), maxLineWidth)
		if j < len(ind.Widths) && ind.Widths[j] >= 1 && ind.Widths[j] <= maxLineWidth {
			ws[j] = math.Round(ind.Widths[j])
		}
	}
	ind.Colors, ind.Downs, ind.Widths = cs, ds, ws
	if !k.source || !slices.ContainsFunc(sources, func(o option) bool { return o.key == ind.Source }) {
		ind.Source = ""
	}
	if !k.offset || math.IsNaN(ind.Offset) {
		ind.Offset = 0
	}
	ind.Offset = min(max(math.Round(ind.Offset), -maxOffset), maxOffset)
	switch {
	case !k.pane:
		ind.Height = 0
	case ind.Height < 0.02 || ind.Height > 0.9 || math.IsNaN(ind.Height):
		ind.Height = defaultPaneHeight
	}
	return true
}

// fixIndicators drops what cannot be drawn and gives every one an ID of its own.
func fixIndicators(list []indicator) []indicator {
	out := make([]indicator, 0, len(list))
	seen := map[int64]bool{}
	var top int64
	for _, ind := range list {
		top = max(top, ind.ID)
	}
	for _, ind := range list {
		if !ind.fix() {
			continue
		}
		if ind.ID <= 0 || seen[ind.ID] {
			top++
			ind.ID = top
		}
		seen[ind.ID] = true
		out = append(out, ind)
	}
	return out
}

func cloneIndicator(ind indicator) indicator {
	ind.Params, ind.Colors, ind.Downs, ind.Widths = slices.Clone(ind.Params), slices.Clone(ind.Colors), slices.Clone(ind.Downs), slices.Clone(ind.Widths)
	return ind
}

// paramText is the parameters as the legend lists them, the source and the
// offset when not the default.
func (ind *indicator) paramText() string {
	k, _ := kindOf(ind.Kind)
	parts := make([]string, 0, len(ind.Params))
	for i, p := range ind.Params {
		if k != nil && i < len(k.params) && k.broken == "" {
			switch pd := k.params[i]; pd.kind {
			case paramBool, paramColor, paramFixed:
				continue // as TradingView's legend: the numbers and the choices
			case paramChoice:
				parts = append(parts, labelOf(pd.opts, strconv.Itoa(int(p))))
				continue
			}
		}
		parts = append(parts, strconv.FormatFloat(p, 'f', -1, 64))
	}
	if ind.Source != "" {
		parts = append(parts, labelOf(sources, ind.Source))
	}
	if ind.Offset != 0 {
		parts = append(parts, "偏移 "+strconv.FormatFloat(ind.Offset, 'f', -1, 64))
	}
	return strings.Join(parts, " ")
}

// shift is how many bars to the right the plots are drawn.
func (ind *indicator) shift() int { return int(ind.Offset) }

// newIndicator is a kind with its defaults; MA and EMA take the first of
// their presets the list does not have yet, and the next color.
func newIndicator(k *indKind, id int64, list []indicator) indicator {
	ind := indicator{ID: id, Kind: k.key}
	ind.fix()
	same := 0
	used := map[float64]bool{}
	for _, o := range list {
		if o.Kind == k.key {
			same++
			used[o.Params[0]] = true
		}
	}
	for _, p := range k.presets {
		if !used[p] {
			ind.Params[0] = p
			break
		}
	}
	if len(k.presets) > 0 {
		ind.Colors[0] = hexColor(ui.Hex(lineColors[same%len(lineColors)]))
	}
	return ind
}

// series is an indicator's values on the bars, by plot then by bar, NaN
// where there is none. It remembers what it was worked out from: while the
// bars before the newest one it had stay the same, only that one and the
// ones after it are worked out again.
type series struct {
	kind, src  string
	params     []float64
	script     *pine.Script // the script it was worked out by, for a script's kind
	vm         *pine.Machine
	env        string // what the script knew of the chart
	gen, n     int
	first      int64
	prev, last market.Bar // the bars before and at the newest one it had
	out        [][]float64
	from       int // where the last update began, for the tests
}

func (s *series) update(k *indKind, params []float64, src string, bars []market.Bar, gen int, env pine.Env, envKey string) {
	n := len(bars)
	from := 0
	if s.kind == k.key && s.src == src && slices.Equal(s.params, params) && s.gen == gen && s.n > 0 && n >= s.n &&
		s.script == k.script && (k.script == nil || s.env == envKey) &&
		bars[0].T == s.first && bars[s.n-1].T == s.last.T && (s.n < 2 || bars[s.n-2] == s.prev) {
		if n == s.n && bars[n-1] == s.last {
			return
		}
		from = s.n - 1
	}
	if k.script != nil || k.broken != "" {
		s.out = nil
		if k.script != nil {
			s.vm = k.script.Run(s.vm, bars, params, from, env)
			s.out = s.vm.Plots
		}
		s.remember(k, params, src, bars, gen, from)
		s.script, s.env = k.script, envKey
		return
	}
	s.script, s.vm = nil, nil
	if len(s.out) != len(k.plots)+k.state {
		s.out, from = make([][]float64, len(k.plots)+k.state), 0
	}
	for j, o := range s.out {
		if cap(o) < n {
			grown := make([]float64, n, n+n/4+64)
			copy(grown, o[:from])
			o = grown
		}
		s.out[j] = o[:n]
	}
	if n > 0 {
		k.calc(bars, source(bars, src), params, s.out, from)
	}
	s.remember(k, params, src, bars, gen, from)
}

// remember keeps what the values were worked out from.
func (s *series) remember(k *indKind, params []float64, src string, bars []market.Bar, gen, from int) {
	n := len(bars)
	if n > 0 {
		s.first, s.last = bars[0].T, bars[n-1]
	}
	if n > 1 {
		s.prev = bars[n-2]
	}
	s.kind, s.src, s.params, s.gen, s.n, s.from = k.key, src, slices.Clone(params), gen, n, from
}

// at is plot j's value at bar i, NaN for none.
func (s *series) at(j, i int) float64 {
	if s == nil || j >= len(s.out) || i < 0 || i >= len(s.out[j]) {
		return math.NaN()
	}
	return s.out[j][i]
}

// The formulas, as TradingView's ta functions work them out.

var nan = math.NaN()

func iparam(p []float64, j int) int { return int(math.Round(p[j])) }

// sma is the mean of src's n values up to i: NaN before there are n, or when
// one of them is NaN.
func sma(src func(int) float64, i, n int) float64 {
	if n < 1 || i < n-1 {
		return nan
	}
	s := 0.0
	for j := i - n + 1; j <= i; j++ {
		s += src(j)
	}
	return s / float64(n)
}

// smooth is ta.ema at bar i for alpha 2/(n+1), ta.rma for 1/n: the SMA of
// the first n values, then each value weighed in by alpha.
func smooth(out []float64, src func(int) float64, i, n int, alpha float64) float64 {
	if i > 0 && !math.IsNaN(out[i-1]) {
		return alpha*src(i) + (1-alpha)*out[i-1]
	}
	return sma(src, i, n)
}

// stdev is the population standard deviation of src's n values up to i
// about their mean, as ta.stdev works it out.
func stdev(src func(int) float64, i, n int, mean float64) float64 {
	s := 0.0
	for j := i - n + 1; j <= i; j++ {
		d := src(j) - mean
		s += d * d
	}
	return math.Sqrt(s / float64(n))
}

func calcMA(b []market.Bar, src func(int) float64, p []float64, out [][]float64, from int) {
	n := iparam(p, 0)
	for i := from; i < len(b); i++ {
		out[0][i] = sma(src, i, n)
	}
}

func calcEMA(b []market.Bar, src func(int) float64, p []float64, out [][]float64, from int) {
	n := iparam(p, 0)
	alpha := 2 / float64(n+1)
	for i := from; i < len(b); i++ {
		out[0][i] = smooth(out[0], src, i, n, alpha)
	}
}

// calcBOLL: the middle band is the SMA, the others that and the standard
// deviation times the multiplier above and below it.
func calcBOLL(b []market.Bar, src func(int) float64, p []float64, out [][]float64, from int) {
	n, mult := iparam(p, 0), p[1]
	for i := from; i < len(b); i++ {
		mid := sma(src, i, n)
		dev := nan
		if !math.IsNaN(mid) {
			dev = mult * stdev(src, i, n, mid)
		}
		out[0][i], out[1][i], out[2][i] = mid, mid+dev, mid-dev
	}
}

// calcMACD: DIF is the fast EMA less the slow one, DEA the EMA of DIF, and
// the histogram DIF less DEA. It keeps the two EMAs after its plots.
func calcMACD(b []market.Bar, c func(int) float64, p []float64, out [][]float64, from int) {
	fast, slow, signal := iparam(p, 0), iparam(p, 1), iparam(p, 2)
	dif, dea, hist, ef, es := out[0], out[1], out[2], out[3], out[4]
	difAt := func(i int) float64 { return dif[i] }
	for i := from; i < len(b); i++ {
		ef[i] = smooth(ef, c, i, fast, 2/float64(fast+1))
		es[i] = smooth(es, c, i, slow, 2/float64(slow+1))
		dif[i] = ef[i] - es[i]
		dea[i] = smooth(dea, difAt, i, signal, 2/float64(signal+1))
		hist[i] = dif[i] - dea[i]
	}
}

// calcRSI is Wilder's: the RMAs of the rises and of the falls from one close
// (or other source) to the next, which it keeps after its plot.
func calcRSI(b []market.Bar, src func(int) float64, p []float64, out [][]float64, from int) {
	n := iparam(p, 0)
	rsi, up, down := out[0], out[1], out[2]
	change := func(i int) float64 {
		if i == 0 {
			return nan
		}
		return src(i) - src(i-1)
	}
	rise := func(i int) float64 { return math.Max(change(i), 0) }
	fall := func(i int) float64 { return -math.Min(change(i), 0) }
	for i := from; i < len(b); i++ {
		up[i] = smooth(up, rise, i, n, 1/float64(n))
		down[i] = smooth(down, fall, i, n, 1/float64(n))
		switch {
		case math.IsNaN(up[i]) || math.IsNaN(down[i]):
			rsi[i] = nan
		case down[i] == 0:
			rsi[i] = 100
		case up[i] == 0:
			rsi[i] = 0
		default:
			rsi[i] = 100 - 100/(1+up[i]/down[i])
		}
	}
}

// calcKDJ is TradingView's KDJ script: RSV is where the close sits between
// the lowest low and the highest high of the period, K its moving average
// (1/m1 of the new value, the rest the last K, starting from 0), D that of K,
// and J 3K - 2D. A period without any range counts as halfway.
func calcKDJ(b []market.Bar, _ func(int) float64, p []float64, out [][]float64, from int) {
	n, m1, m2 := iparam(p, 0), float64(iparam(p, 1)), float64(iparam(p, 2))
	kk, dd, jj := out[0], out[1], out[2]
	nz := func(v float64) float64 {
		if math.IsNaN(v) {
			return 0
		}
		return v
	}
	for i := from; i < len(b); i++ {
		kk[i], dd[i], jj[i] = nan, nan, nan
		if i < n-1 {
			continue
		}
		hh, ll := b[i].H, b[i].L
		for j := i - n + 1; j < i; j++ {
			hh, ll = max(hh, b[j].H), min(ll, b[j].L)
		}
		rsv := 50.0
		if hh > ll {
			rsv = 100 * (b[i].C - ll) / (hh - ll)
		}
		pk, pd := 0.0, 0.0
		if i > 0 {
			pk, pd = nz(kk[i-1]), nz(dd[i-1])
		}
		kk[i] = (rsv + (m1-1)*pk) / m1
		dd[i] = (kk[i] + (m2-1)*pd) / m2
		jj[i] = 3*kk[i] - 2*dd[i]
	}
}

// calcVOL is the volume and its SMA, none for a period of 0.
func calcVOL(b []market.Bar, _ func(int) float64, p []float64, out [][]float64, from int) {
	n := iparam(p, 0)
	v := func(i int) float64 { return b[i].V }
	for i := from; i < len(b); i++ {
		out[0][i], out[1][i] = b[i].V, nan
		if n > 0 {
			out[1][i] = sma(v, i, n)
		}
	}
}

// barAt is bar i of those the values were worked out on, the newest for
// one past it.
func (s *series) barAt(i int) market.Bar {
	if s.vm == nil || s.vm.Bars() == 0 {
		return market.Bar{H: math.NaN(), L: math.NaN()}
	}
	return s.vm.Bar(min(max(i, 0), s.vm.Bars()-1))
}
