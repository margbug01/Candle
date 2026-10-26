package main

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// Technical indicators, after TradingView's: MA, EMA and BOLL over the
// candles, MACD, RSI, KDJ and volume in panes of their own below them. The
// settings keep one list for every market; each one's values are kept and
// brought up to date from the newest bar on.

// How a plot draws its values.
const (
	plotLine   = iota
	plotHist   // bars up or down from zero, as MACD's histogram
	plotVolume // bars up from the pane's bottom, colored as the candles
)

type plotDef struct {
	name  string // as the legend and the settings name it
	color string // the default
	style int
}

type paramDef struct {
	name              string
	def, lo, hi, step float64
}

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
	calc             func(b []market.Bar, p []float64, out [][]float64, from int)
}

var indKinds = []indKind{
	{key: "ma", short: "MA", name: "移动平均线", alias: "均线 SMA", decimals: -1,
		params:  []paramDef{{"周期", 5, 1, 1000, 1}},
		presets: []float64{5, 10, 20, 30, 60, 120},
		plots:   []plotDef{{"MA", "#f0b90b", plotLine}}, calc: calcMA},
	{key: "ema", short: "EMA", name: "指数移动平均线", alias: "均线", decimals: -1,
		params:  []paramDef{{"周期", 12, 1, 1000, 1}},
		presets: []float64{12, 26, 50, 100, 200},
		plots:   []plotDef{{"EMA", "#f0b90b", plotLine}}, calc: calcEMA},
	{key: "boll", short: "BOLL", name: "布林带", alias: "BB Bollinger", decimals: -1,
		params: []paramDef{{"周期", 20, 1, 500, 1}, {"倍数", 2, 0.1, 10, 0.1}},
		plots:  []plotDef{{"中轨", "#ff6d00", plotLine}, {"上轨", "#2962ff", plotLine}, {"下轨", "#2962ff", plotLine}},
		fill:   []int{1, 2}, calc: calcBOLL},
	{key: "macd", short: "MACD", name: "指数平滑异同移动平均线", pane: true, decimals: -1,
		params: []paramDef{{"快线", 12, 1, 500, 1}, {"慢线", 26, 1, 500, 1}, {"信号线", 9, 1, 500, 1}},
		plots:  []plotDef{{"DIF", "#2962ff", plotLine}, {"DEA", "#ff6d00", plotLine}, {"柱", "", plotHist}},
		state:  2, levels: []float64{0}, calc: calcMACD},
	{key: "rsi", short: "RSI", name: "相对强弱指数", pane: true, decimals: 2,
		params: []paramDef{{"周期", 14, 1, 500, 1}},
		plots:  []plotDef{{"RSI", "#7e57c2", plotLine}},
		state:  2, levels: []float64{70, 30}, band: true, calc: calcRSI},
	{key: "kdj", short: "KDJ", name: "随机指标", alias: "Stochastic", pane: true, decimals: 2,
		params: []paramDef{{"周期", 9, 1, 500, 1}, {"K 平滑", 3, 1, 100, 1}, {"D 平滑", 3, 1, 100, 1}},
		plots:  []plotDef{{"K", "#2962ff", plotLine}, {"D", "#ff9800", plotLine}, {"J", "#e040fb", plotLine}},
		levels: []float64{80, 20}, calc: calcKDJ},
	{key: "vol", short: "VOL", name: "成交量", alias: "Volume", pane: true, decimals: -2,
		params: []paramDef{{"均线周期", 20, 0, 500, 1}},
		plots:  []plotDef{{"成交量", "", plotVolume}, {"均线", "#2962ff", plotLine}}, calc: calcVOL},
}

// lineColors take turns among the instances of a one-line kind.
var lineColors = []string{"#f0b90b", "#e040fb", "#2962ff", "#00bcd4", "#ff7043", "#66bb6a"}

func kindOf(key string) (*indKind, bool) {
	for i := range indKinds {
		if indKinds[i].key == key {
			return &indKinds[i], true
		}
	}
	return nil, false
}

// indicator is one on the chart, as settings.json keeps it.
type indicator struct {
	ID     int64
	Kind   string
	Params []float64
	Colors []hexColor // by plot; unset, the kind's
	Widths []float64  // by plot
	Hidden bool
	Height float64 `json:",omitempty"` // a pane's share of the chart's height
}

const (
	defaultPaneHeight = 0.2
	maxLineWidth      = 4
)

// fix fits what a damaged or older file holds to the kind, and reports
// whether the kind is one this program knows.
func (ind *indicator) fix() bool {
	k, ok := kindOf(ind.Kind)
	if !ok {
		return false
	}
	ps := make([]float64, len(k.params))
	for j, pd := range k.params {
		ps[j] = pd.def
		if j < len(ind.Params) && !math.IsNaN(ind.Params[j]) {
			ps[j] = min(max(ind.Params[j], pd.lo), pd.hi)
		}
	}
	ind.Params = ps
	cs, ws := make([]hexColor, len(k.plots)), make([]float64, len(k.plots))
	for j := range k.plots {
		if j < len(ind.Colors) {
			cs[j] = ind.Colors[j]
		}
		ws[j] = 1
		if j < len(ind.Widths) && ind.Widths[j] >= 1 && ind.Widths[j] <= maxLineWidth {
			ws[j] = math.Round(ind.Widths[j])
		}
	}
	ind.Colors, ind.Widths = cs, ws
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
	ind.Params, ind.Colors, ind.Widths = slices.Clone(ind.Params), slices.Clone(ind.Colors), slices.Clone(ind.Widths)
	return ind
}

// paramText is the parameters as the legend lists them.
func (ind *indicator) paramText() string {
	parts := make([]string, len(ind.Params))
	for i, p := range ind.Params {
		parts[i] = strconv.FormatFloat(p, 'f', -1, 64)
	}
	return strings.Join(parts, " ")
}

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
	kind       string
	params     []float64
	gen, n     int
	first      int64
	prev, last market.Bar // the bars before and at the newest one it had
	out        [][]float64
	from       int // where the last update began, for the tests
}

func (s *series) update(k *indKind, params []float64, bars []market.Bar, gen int) {
	n := len(bars)
	from := 0
	if s.kind == k.key && slices.Equal(s.params, params) && s.gen == gen && s.n > 0 && n >= s.n &&
		bars[0].T == s.first && bars[s.n-1].T == s.last.T && (s.n < 2 || bars[s.n-2] == s.prev) {
		if n == s.n && bars[n-1] == s.last {
			return
		}
		from = s.n - 1
	}
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
		k.calc(bars, params, s.out, from)
		s.first, s.last = bars[0].T, bars[n-1]
	}
	if n > 1 {
		s.prev = bars[n-2]
	}
	s.kind, s.params, s.gen, s.n, s.from = k.key, slices.Clone(params), gen, n, from
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

func closes(b []market.Bar) func(int) float64 { return func(i int) float64 { return b[i].C } }

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

func calcMA(b []market.Bar, p []float64, out [][]float64, from int) {
	n, c := iparam(p, 0), closes(b)
	for i := from; i < len(b); i++ {
		out[0][i] = sma(c, i, n)
	}
}

func calcEMA(b []market.Bar, p []float64, out [][]float64, from int) {
	n, c := iparam(p, 0), closes(b)
	alpha := 2 / float64(n+1)
	for i := from; i < len(b); i++ {
		out[0][i] = smooth(out[0], c, i, n, alpha)
	}
}

// calcBOLL: the middle band is the SMA, the others that and the standard
// deviation times the multiplier above and below it.
func calcBOLL(b []market.Bar, p []float64, out [][]float64, from int) {
	n, mult, c := iparam(p, 0), p[1], closes(b)
	for i := from; i < len(b); i++ {
		mid := sma(c, i, n)
		dev := nan
		if !math.IsNaN(mid) {
			dev = mult * stdev(c, i, n, mid)
		}
		out[0][i], out[1][i], out[2][i] = mid, mid+dev, mid-dev
	}
}

// calcMACD: DIF is the fast EMA less the slow one, DEA the EMA of DIF, and
// the histogram DIF less DEA. It keeps the two EMAs after its plots.
func calcMACD(b []market.Bar, p []float64, out [][]float64, from int) {
	fast, slow, signal, c := iparam(p, 0), iparam(p, 1), iparam(p, 2), closes(b)
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
// to the next, which it keeps after its plot.
func calcRSI(b []market.Bar, p []float64, out [][]float64, from int) {
	n := iparam(p, 0)
	rsi, up, down := out[0], out[1], out[2]
	change := func(i int) float64 {
		if i == 0 {
			return nan
		}
		return b[i].C - b[i-1].C
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
func calcKDJ(b []market.Bar, p []float64, out [][]float64, from int) {
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
func calcVOL(b []market.Bar, p []float64, out [][]float64, from int) {
	n := iparam(p, 0)
	v := func(i int) float64 { return b[i].V }
	for i := from; i < len(b); i++ {
		out[0][i], out[1][i] = b[i].V, nan
		if n > 0 {
			out[1][i] = sma(v, i, n)
		}
	}
}
