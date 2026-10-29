package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"candle/market"
)

// interval is a bar size the toolbar offers, as Binance names it.
type interval struct {
	key, label string
	ms         int64
}

var intervals = []interval{
	{"1m", "1分", 60_000}, {"5m", "5分", 300_000}, {"15m", "15分", 900_000},
	{"1h", "1小时", 3_600_000}, {"4h", "4小时", 14_400_000},
	{"1d", "日线", 86_400_000}, {"1w", "周线", 604_800_000},
}

func intervalOf(key string) (interval, bool) {
	i := slices.IndexFunc(intervals, func(iv interval) bool { return iv.key == key })
	if i < 0 {
		return interval{}, false
	}
	return intervals[i], true
}

const (
	axisW      = 80 // the price axis on the right
	timeH      = 28 // the time axis at the bottom
	minSpacing = 1.5
	maxSpacing = 48
	keepBars   = 4 // bars that stay in sight however far it is scrolled
)

// view is where the chart looks: how many DIPs a bar takes and how far, in
// bars, the newest bar sits from the right edge. Counting from the newest bar
// keeps the view still as older pages arrive in front.
type view struct {
	spacing float32
	offset  float32
}

func defaultView() view { return view{spacing: 8, offset: 6} }

// right is the index of the bar at the right edge, fractional.
func (v view) right(n int) float32 { return float32(n-1) + v.offset }

// x is the center of bar i in a plot w wide.
func (v view) x(i int, w float32, n int) float32 { return v.xf(float32(i), w, n) }

// xf is x for a fractional bar.
func (v view) xf(i, w float32, n int) float32 {
	return w - (v.right(n)-i)*v.spacing - v.spacing/2
}

// index is the bar at x, fractional: the inverse of x.
func (v view) index(x, w float32, n int) float32 {
	return v.right(n) - (w-v.spacing/2-x)/v.spacing
}

// span is the first and last bar in sight, inclusive; to < from when none is.
func (v view) span(w float32, n int) (from, to int) {
	from = max(int(math.Floor(float64(v.index(0, w, n)))), 0)
	to = min(int(math.Ceil(float64(v.index(w, w, n)))), n-1)
	return from, to
}

// pan moves the bars dx DIPs to the right, toward older ones.
func (v *view) pan(dx float32) { v.offset -= dx / v.spacing }

// zoom widens the bars by f, keeping the bar under x where it is.
func (v *view) zoom(f, x, w float32, n int) {
	at := v.index(x, w, n)
	v.spacing = min(max(v.spacing*f, minSpacing), maxSpacing)
	v.offset = at + (w-v.spacing/2-x)/v.spacing - float32(n-1)
}

// clamp keeps a few bars in sight, however far it is scrolled either way.
func (v *view) clamp(w float32, n int) {
	v.spacing = min(max(v.spacing, minSpacing), maxSpacing)
	lo, hi := float32(keepBars-n), w/v.spacing-keepBars
	v.offset = min(max(v.offset, lo), max(hi, lo))
}

// axis is how a plot lays prices out: its height, the band the candles take
// as fractions of it from the top, and whether the scale is logarithmic.
type axis struct {
	h, top, bottom float32
	log            bool
}

func (m axis) band() float32 { return m.h * (m.bottom - m.top) }

// t is where price p sits along the scale: p, or its logarithm.
func (m axis) t(p float64) float64 {
	if m.log {
		return math.Log10(max(p, 1e-12))
	}
	return p
}

// price is the inverse of t.
func (m axis) price(t float64) float64 {
	if m.log {
		return math.Pow(10, t)
	}
	return t
}

// prices is the range the candle band spans. Left automatic it fits the bars
// in sight; scaled or moved by hand (manual) it stays where it is put.
type prices struct {
	manual bool
	lo, hi float64
}

// y is the height of price p in the plot.
func (s prices) y(p float64, m axis) float32 {
	lo, hi := m.t(s.lo), m.t(s.hi)
	return m.h*m.top + float32((hi-m.t(p))/(hi-lo))*m.band()
}

// at is the price at height y in the plot: the inverse of y.
func (s prices) at(y float32, m axis) float64 {
	lo, hi := m.t(s.lo), m.t(s.hi)
	return m.price(hi - float64((y-m.h*m.top)/m.band())*(hi-lo))
}

// zoom widens the range by f along the scale, keeping price p where it is:
// at least a hundred-thousandth of the prices, at most twenty times them, or
// eight decades on a log scale.
func (s *prices) zoom(f, p float64, m axis) {
	lo, hi, tp := m.t(s.lo), m.t(s.hi), m.t(p)
	ref := max(math.Abs(s.lo), math.Abs(s.hi))
	least, most := ref*1e-5, ref*20
	if m.log {
		least, most = 1e-5/math.Ln10, 8
	}
	f = min(max((hi-lo)*f, least), most) / (hi - lo)
	s.lo, s.hi = m.price(tp-(tp-lo)*f), m.price(tp+(hi-tp)*f)
}

// pan moves the prices dy DIPs down the plot, following the pointer.
func (s *prices) pan(dy float32, m axis) {
	lo, hi := m.t(s.lo), m.t(s.hi)
	d := float64(dy/m.band()) * (hi - lo)
	s.lo, s.hi = m.price(lo+d), m.price(hi+d)
}

// priceTicks are round prices from lo to hi, about n of them: evenly apart,
// or on a log scale 1, 2 and 5 of a decade, thinner or denser as they fit.
// A log scale spanning little is near enough even to take even steps.
func priceTicks(lo, hi float64, n int, log bool) []float64 {
	var out []float64
	if !log || lo <= 0 || float64(n)/math.Log10(hi/lo) > 12 {
		step := niceStep(hi-lo, n)
		for i := math.Ceil(lo / step); i*step <= hi; i++ {
			v := i * step
			if v == 0 { // not -0, which reads "-0"
				v = 0
			}
			out = append(out, v)
		}
		return out
	}
	per := float64(n) / math.Log10(hi/lo) // ticks a decade has room for
	marks, every := []float64{1}, 1
	switch {
	case per >= 7:
		marks = []float64{1, 1.5, 2, 3, 4, 5, 7}
	case per >= 4:
		marks = []float64{1, 2, 3, 5}
	case per >= 2.5:
		marks = []float64{1, 2, 5}
	case per >= 1.5:
		marks = []float64{1, 3}
	default:
		every = int(math.Ceil(1 / per))
	}
	for d := int(math.Floor(math.Log10(lo))); d <= int(math.Ceil(math.Log10(hi))); d++ {
		if d%every != 0 {
			continue
		}
		for _, m := range marks {
			if p := m * math.Pow(10, float64(d)); p >= lo && p <= hi {
				out = append(out, p)
			}
		}
	}
	return out
}

// stepDecimals is how many decimals show step whole.
func stepDecimals(step float64) int {
	for d := 0; d < 10; d++ {
		v := step * math.Pow(10, float64(d))
		if math.Abs(v-math.Round(v)) < 1e-6*max(v, 1) {
			return d
		}
	}
	return 10
}

// priceRange is the lowest low and the highest high, widened when flat.
func priceRange(bars []market.Bar) (lo, hi float64) {
	lo, hi = bars[0].L, bars[0].H
	for _, b := range bars {
		lo, hi = min(lo, b.L), max(hi, b.H)
	}
	if hi <= lo {
		d := max(math.Abs(hi)*0.01, 1e-8)
		lo, hi = lo-d, hi+d
	}
	return lo, hi
}

// niceStep is the round step (1, 2, 2.5 or 5 times a power of ten) that cuts
// span into at most n parts.
func niceStep(span float64, n int) float64 {
	raw := span / float64(max(n, 1))
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5} {
		if m*mag >= raw*(1-1e-9) {
			return m * mag
		}
	}
	return 10 * mag
}

// decimals is how many decimals prices with this smallest step show.
func decimals(tick float64) int { return max(0, int(math.Round(-math.Log10(tick)))) }

// autoDecimals guesses, from a price, the decimals of a market whose step is
// not known yet: about five significant digits, two to ten decimals.
func autoDecimals(p float64) int {
	if p <= 0 {
		return 2
	}
	return min(max(4-int(math.Floor(math.Log10(p))), 2), 10)
}

func formatPrice(p float64, d int) string { return strconv.FormatFloat(p, 'f', d, 64) }

func formatSigned(v float64, d int) string {
	if v < 0 {
		return "-" + formatPrice(-v, d)
	}
	return "+" + formatPrice(v, d)
}

func formatVolume(v float64) string {
	for _, u := range []struct {
		n      float64
		suffix string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "K"}} {
		if v >= u.n {
			return strconv.FormatFloat(v/u.n, 'f', 2, 64) + u.suffix
		}
	}
	if v >= 100 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// countdown is the time left until a bar closes.
func countdown(ms int64) string {
	s := max(ms, 0) / 1000
	d, h, m, sec := s/86400, s/3600%24, s/60%60, s%60
	switch {
	case d > 0:
		return fmt.Sprintf("%d天%02d时", d, h)
	case h > 0:
		return fmt.Sprintf("%02d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

// A tick labels bar i on the time axis. Its level is what changed there:
// 1 the time of day, 2 the day, 3 the month, 4 the year.
type tick struct {
	i     int
	text  string
	level int
}

// A labelStep groups times into buckets; a label goes where the bucket changes.
type labelStep struct {
	approx int64 // seconds, roughly
	bucket func(t time.Time) int64
}

// seconds steps count wall-clock time in the bars' zone, so 6 hours falls on
// 00:00, 06:00, 12:00 and 18:00 there.
func seconds(d int64) labelStep {
	return labelStep{d, func(t time.Time) int64 {
		_, off := t.Zone()
		return (t.Unix() + int64(off)) / d
	}}
}

func months(k int64) labelStep {
	return labelStep{k * 30 * 86400, func(t time.Time) int64 { return (int64(t.Year())*12 + int64(t.Month()) - 1) / k }}
}

var labelSteps = []labelStep{
	seconds(60), seconds(300), seconds(900), seconds(1800), seconds(3600), seconds(7200),
	seconds(3 * 3600), seconds(6 * 3600), seconds(12 * 3600), seconds(86400), seconds(2 * 86400), seconds(3 * 86400),
	// the 1st and the 16th
	{15 * 86400, func(t time.Time) int64 { return (int64(t.Year())*12+int64(t.Month()))*2 + int64(min(t.Day(), 16)/16) }},
	months(1), months(3), months(6), months(12), months(24), months(60), months(120),
}

// minLabelGap is the least room, in DIPs, between two time labels.
const minLabelGap = 72

// timeTicks picks the labels for bars from..to, ivMs apart and spacing DIPs
// apart, telling time in loc: the finest round step that leaves room,
// labelled where it changes.
// Where two labels crowd, the one marking the bigger change stays.
func timeTicks(bars []market.Bar, from, to int, ivMs int64, spacing float32, loc *time.Location, clock12 bool) []tick {
	iv := ivMs / 1000
	step := labelSteps[len(labelSteps)-1]
	for _, s := range labelSteps {
		if s.approx >= iv && float32(s.approx)/float32(iv)*spacing >= minLabelGap {
			step = s
			break
		}
	}
	var out []tick
	for i := max(from, 1); i <= to; i++ {
		a, b := time.UnixMilli(bars[i-1].T).In(loc), time.UnixMilli(bars[i].T).In(loc)
		if step.bucket(a) == step.bucket(b) {
			continue
		}
		t := tick{i: i}
		switch {
		case a.Year() != b.Year():
			t.level, t.text = 4, strconv.Itoa(b.Year())
		case a.Month() != b.Month():
			t.level, t.text = 3, fmt.Sprintf("%d月", b.Month())
		case a.YearDay() != b.YearDay():
			t.level, t.text = 2, fmt.Sprintf("%d日", b.Day())
		default:
			t.level, t.text = 1, clockText(b, clock12)
		}
		if n := len(out); n > 0 && float32(i-out[n-1].i)*spacing < minLabelGap {
			if t.level > out[n-1].level {
				out[n-1] = t
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

// A dayBreak is where a day begins among the bars: in bar i, frac of the way
// from its open to its close.
type dayBreak struct {
	i    int
	frac float32
}

// dayBreaks are where days begin, at midnight in loc, among bars from..to,
// ivMs long. A day beginning while a bar is open, as a 12-hour bar opening
// at 20:00 is, is marked inside it; one in a gap of the data, at the open
// of the bar after the gap.
func dayBreaks(bars []market.Bar, from, to int, ivMs int64, loc *time.Location) []dayBreak {
	var out []dayBreak
	for i := max(from, 1); i <= min(to+1, len(bars)-1); i++ {
		a, b := time.UnixMilli(bars[i-1].T).In(loc), time.UnixMilli(bars[i].T).In(loc)
		if a.YearDay() == b.YearDay() && a.Year() == b.Year() {
			continue
		}
		m := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, loc).UnixMilli()
		if prev := bars[i-1].T; m > prev && m < prev+ivMs {
			out = append(out, dayBreak{i - 1, float32(m-prev) / float32(ivMs)})
		} else {
			out = append(out, dayBreak{i, 0})
		}
	}
	return out
}

// merge puts got into have by open time, replacing bars it already has, and
// reports how many went past the end.
func merge(have, got []market.Bar) ([]market.Bar, int) {
	added := 0
	for _, b := range got {
		n := len(have)
		switch {
		case n == 0 || b.T > have[n-1].T:
			have = append(have, b)
			added++
		case b.T == have[n-1].T:
			have[n-1] = b
		default:
			i := sort.Search(n, func(i int) bool { return have[i].T >= b.T })
			if have[i].T == b.T {
				have[i] = b
			} else {
				have = slices.Insert(have, i, b)
			}
		}
	}
	return have, added
}

// search finds markets by name: the name itself, then names starting with
// the query, then names holding it, USDT markets first, busiest first.
func search(all []market.Ticker, query string, limit int) []market.Ticker {
	q := strings.ToUpper(strings.NewReplacer("/", "", "-", "", " ", "").Replace(query))
	if q == "" {
		return nil
	}
	type hit struct {
		t    market.Ticker
		rank int
	}
	var hits []hit
	for _, t := range all {
		var rank int
		switch {
		case t.Symbol == q:
			rank = 0
		case strings.HasPrefix(t.Symbol, q):
			rank = 1
		case strings.Contains(t.Symbol, q):
			rank = 2
		default:
			continue
		}
		if rank > 0 && !strings.HasSuffix(t.Symbol, "USDT") {
			rank += 2
		}
		hits = append(hits, hit{t, rank})
	}
	slices.SortFunc(hits, func(a, b hit) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return cmp.Compare(b.t.QuoteVolume, a.t.QuoteVolume)
	})
	out := make([]market.Ticker, 0, min(len(hits), limit))
	for _, h := range hits[:min(len(hits), limit)] {
		out = append(out, h.t)
	}
	return out
}
