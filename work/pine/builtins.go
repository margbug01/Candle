package pine

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// bfn is a built-in function: its parameters by name, in order, and how a
// call of it compiles.
type bfn struct {
	params  []string
	pure    bool // the same arguments give the same value: constant for constants
	global  bool // only at the top level of the script
	compile func(c *compiler, a []cx, x *callX) (cx, error)
}

var nan = math.NaN()

func given(a cx) bool { return a.k != kMissing }

// need checks that the arguments named are given, and are numbers.
func need(x *callX, a []cx, params []string, names ...string) error {
	for _, n := range names {
		i := slices.Index(params, n)
		if i < 0 || i >= len(a) || !given(a[i]) {
			return errAt(x.t, "%s 少了参数 %s", x.fn, n)
		}
		if a[i].k != kFloat {
			return errAt(a[i].tok, "%s 的参数 %s 要是数值", x.fn, n)
		}
	}
	return nil
}

// or is a, or def when not given.
func or(a cx, def float64) cx {
	if !given(a) {
		return konstX(def)
	}
	return a
}

func length(v float64) int {
	if math.IsNaN(v) {
		return 0
	}
	return int(math.Round(v))
}

// record keeps v's values by bar in a buffer of its own.
func (c *compiler) record(v cx) (func(m *Machine) float64, int) {
	b := c.newBuf(false)
	return func(m *Machine) float64 {
		x := v.f(m)
		m.buf[b][m.i] = x
		return x
	}, b
}

// win is the last n values of buffer b up to the bar, nil while fewer.
func win(m *Machine, b, n int) []float64 {
	if n < 1 || m.i < n-1 {
		return nil
	}
	return m.buf[b][m.i-n+1 : m.i+1]
}

func past(m *Machine, b, k int) float64 {
	if k < 0 || k > m.i {
		return nan
	}
	return m.buf[b][m.i-k]
}

// rolling is a function of a source's last length values: f is given
// them, oldest first, and none while there are fewer. One argument is the
// length, the source then def ("high" for ta.highest).
func rolling(def string, f func(w []float64) float64) *bfn {
	params := []string{"source", "length"}
	return &bfn{params: params, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
		src, ln := a[0], a[1]
		if def != "" && !given(ln) {
			src, ln = barX(def), a[0]
		}
		if !given(src) || !given(ln) {
			return cx{}, errAt(x.t, "%s 少了参数", x.fn)
		}
		rec, b := c.record(src)
		return numX(func(m *Machine) float64 {
			rec(m)
			w := win(m, b, length(ln.f(m)))
			if w == nil {
				return nan
			}
			return f(w)
		}), nil
	}}
}

func anyNaN(w []float64) bool {
	for _, v := range w {
		if math.IsNaN(v) {
			return true
		}
	}
	return false
}

func mean(w []float64) float64 {
	s := 0.0
	for _, v := range w {
		s += v
	}
	return s / float64(len(w))
}

func wmaOf(w []float64) float64 {
	n := len(w)
	s, norm := 0.0, 0.0
	for j, v := range w {
		wt := float64(j + 1)
		s += v * wt
		norm += wt
	}
	_ = n
	return s / norm
}

// smoothing is ta.ema (alpha 2/(n+1)) or ta.rma (1/n): seeded with the SMA
// of the first n values, then each value weighed in by alpha.
func smoothing(alpha func(n int) float64) *bfn {
	return &bfn{params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
		if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
			return cx{}, err
		}
		return c.smooth(a[0], a[1], alpha), nil
	}}
}

func emaAlpha(n int) float64 { return 2 / float64(n+1) }
func rmaAlpha(n int) float64 { return 1 / float64(n) }

func (c *compiler) smooth(src, ln cx, alpha func(n int) float64) cx {
	rec, b := c.record(src)
	o := c.newBuf(false)
	return numX(func(m *Machine) float64 {
		v := rec(m)
		n := length(ln.f(m))
		out := m.buf[o]
		if m.i > 0 && !math.IsNaN(out[m.i-1]) && n >= 1 {
			al := alpha(n)
			out[m.i] = al*v + (1-al)*out[m.i-1]
		} else if w := win(m, b, n); w != nil && !anyNaN(w) {
			out[m.i] = mean(w)
		} else {
			out[m.i] = nan
		}
		return out[m.i]
	})
}

func (c *compiler) wma(src, ln cx) cx {
	rec, b := c.record(src)
	return numX(func(m *Machine) float64 {
		rec(m)
		w := win(m, b, length(ln.f(m)))
		if w == nil || anyNaN(w) {
			return nan
		}
		return wmaOf(w)
	})
}

func (c *compiler) sma(src, ln cx) cx {
	rec, b := c.record(src)
	return numX(func(m *Machine) float64 {
		rec(m)
		w := win(m, b, length(ln.f(m)))
		if w == nil || anyNaN(w) {
			return nan
		}
		return mean(w)
	})
}

func barX(key string) cx {
	v := numX(func(m *Machine) float64 { return m.source(key) })
	v.hist = func(m *Machine, k int) float64 {
		if k > m.i {
			return nan
		}
		return sourceOf(m.bars[m.i-k], key)
	}
	v.source = key
	return v
}

// trX is the true range; na on the first bar unless handleNa, then high - low.
func trX(handleNa bool) cx {
	return numX(func(m *Machine) float64 { return trueRange(m, handleNa) })
}

func trueRange(m *Machine, handleNa bool) float64 {
	b := m.bars[m.i]
	if m.i == 0 {
		if handleNa {
			return b.H - b.L
		}
		return nan
	}
	pc := m.bars[m.i-1].C
	return math.Max(b.H-b.L, math.Max(math.Abs(b.H-pc), math.Abs(b.L-pc)))
}

func pure(params []string, f func(a []cx) func(m *Machine) float64) *bfn {
	return &bfn{params: params, pure: true, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
		for i, v := range a {
			if given(v) && v.k != kFloat {
				return cx{}, errAt(v.tok, "%s 的参数 %s 要是数值", x.fn, params[min(i, len(params)-1)])
			}
		}
		return numX(f(a)), nil
	}}
}

func math1(fn func(float64) float64) *bfn {
	return pure([]string{"number"}, func(a []cx) func(m *Machine) float64 {
		return func(m *Machine) float64 { return fn(a[0].f(m)) }
	})
}

// fold folds the numbers given, as math.max does: na when one is.
func fold(fn func(a, b float64) float64, avg bool) *bfn {
	return &bfn{params: []string{"number0", "number1", "number…"}, pure: true, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
		var xs []cx
		for _, v := range a {
			if given(v) {
				if v.k != kFloat {
					return cx{}, errAt(v.tok, "%s 的参数要是数值", x.fn)
				}
				xs = append(xs, v)
			}
		}
		if len(xs) < 2 {
			return cx{}, errAt(x.t, "%s 至少要两个数", x.fn)
		}
		return numX(func(m *Machine) float64 {
			r := xs[0].f(m)
			for _, v := range xs[1:] {
				y := v.f(m)
				if math.IsNaN(r) || math.IsNaN(y) {
					return nan
				}
				r = fn(r, y)
			}
			if avg {
				return r / float64(len(xs))
			}
			return r
		}), nil
	}}
}

var builtinFuncs map[string]*bfn

var v4Inputs = map[string]string{"study": "indicator", "iff": "iff", "security": "request.security"}

func init() {
	builtinFuncs = map[string]*bfn{
		"na": pure([]string{"x"}, func(a []cx) func(m *Machine) float64 {
			return func(m *Machine) float64 { return boolF(math.IsNaN(a[0].f(m))) }
		}),
		"nz": pure([]string{"source", "replacement"}, func(a []cx) func(m *Machine) float64 {
			r := or(a[1], 0)
			return func(m *Machine) float64 {
				if v := a[0].f(m); !math.IsNaN(v) {
					return v
				}
				return r.f(m)
			}
		}),
		"iff": pure([]string{"condition", "then", "_else"}, func(a []cx) func(m *Machine) float64 {
			return func(m *Machine) float64 {
				if truth(a[0].f(m)) {
					return a[1].f(m)
				}
				return a[2].f(m)
			}
		}),
		"int": pure([]string{"x"}, func(a []cx) func(m *Machine) float64 {
			return func(m *Machine) float64 { return math.Trunc(a[0].f(m)) }
		}),
		"float": pure([]string{"x"}, func(a []cx) func(m *Machine) float64 { return a[0].f }),
		"color": pure([]string{"x"}, func(a []cx) func(m *Machine) float64 { return a[0].f }),
		"bool": pure([]string{"x"}, func(a []cx) func(m *Machine) float64 {
			return func(m *Machine) float64 { return boolF(truth(a[0].f(m))) }
		}),
		"fixnan": {params: []string{"source"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source"}, "source"); err != nil {
				return cx{}, err
			}
			o := c.newBuf(false)
			return numX(func(m *Machine) float64 {
				v := a[0].f(m)
				if math.IsNaN(v) && m.i > 0 {
					v = m.buf[o][m.i-1]
				}
				m.buf[o][m.i] = v
				return v
			}), nil
		}},

		"math.abs":       math1(math.Abs),
		"math.acos":      math1(math.Acos),
		"math.asin":      math1(math.Asin),
		"math.atan":      math1(math.Atan),
		"math.ceil":      math1(math.Ceil),
		"math.cos":       math1(math.Cos),
		"math.exp":       math1(math.Exp),
		"math.floor":     math1(math.Floor),
		"math.log":       math1(math.Log),
		"math.log10":     math1(math.Log10),
		"math.sin":       math1(math.Sin),
		"math.sqrt":      math1(math.Sqrt),
		"math.tan":       math1(math.Tan),
		"math.todegrees": math1(func(v float64) float64 { return v * 180 / math.Pi }),
		"math.toradians": math1(func(v float64) float64 { return v * math.Pi / 180 }),
		"math.sign": math1(func(v float64) float64 {
			switch {
			case v > 0:
				return 1
			case v < 0:
				return -1
			}
			return v
		}),
		"math.pow": pure([]string{"base", "exponent"}, func(a []cx) func(m *Machine) float64 {
			return func(m *Machine) float64 { return math.Pow(a[0].f(m), a[1].f(m)) }
		}),
		"math.round": pure([]string{"number", "precision"}, func(a []cx) func(m *Machine) float64 {
			if !given(a[1]) {
				return func(m *Machine) float64 { return roundHalfUp(a[0].f(m)) }
			}
			return func(m *Machine) float64 {
				p := math.Pow(10, math.Round(a[1].f(m)))
				return roundHalfUp(a[0].f(m)*p) / p
			}
		}),
		"math.round_to_mintick": {params: []string{"number"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return numX(func(m *Machine) float64 {
				t := m.tick()
				return roundHalfUp(a[0].f(m)/t) * t
			}), nil
		}},
		"math.max": fold(math.Max, false),
		"math.min": fold(math.Min, false),
		"math.avg": fold(func(a, b float64) float64 { return a + b }, true),
		"math.sum": rolling("", func(w []float64) float64 {
			if anyNaN(w) {
				return nan
			}
			s := 0.0
			for _, v := range w {
				s += v
			}
			return s
		}),

		"ta.sma": rolling("", func(w []float64) float64 {
			if anyNaN(w) {
				return nan
			}
			return mean(w)
		}),
		"ta.ema": smoothing(emaAlpha),
		"ta.rma": smoothing(rmaAlpha),
		"ta.wma": rolling("", func(w []float64) float64 {
			if anyNaN(w) {
				return nan
			}
			return wmaOf(w)
		}),
		"ta.stdev": {params: []string{"source", "length", "biased"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.deviation(a, x, true)
		}},
		"ta.variance": {params: []string{"source", "length", "biased"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.deviation(a, x, false)
		}},
		"ta.dev": rolling("", func(w []float64) float64 {
			if anyNaN(w) {
				return nan
			}
			mu := mean(w)
			s := 0.0
			for _, v := range w {
				s += math.Abs(v - mu)
			}
			return s / float64(len(w))
		}),
		"ta.median": rolling("", func(w []float64) float64 {
			s := slices.DeleteFunc(slices.Clone(w), math.IsNaN)
			if len(s) == 0 {
				return nan
			}
			sort.Float64s(s)
			if len(s)%2 == 1 {
				return s[len(s)/2]
			}
			return (s[len(s)/2-1] + s[len(s)/2]) / 2
		}),
		"ta.highest": rolling("high", func(w []float64) float64 { return extreme(w, 1) }),
		"ta.lowest":  rolling("low", func(w []float64) float64 { return extreme(w, -1) }),
		"ta.highestbars": rolling("high", func(w []float64) float64 {
			return extremeAt(w, 1)
		}),
		"ta.lowestbars": rolling("low", func(w []float64) float64 {
			return extremeAt(w, -1)
		}),
		"ta.range": rolling("", func(w []float64) float64 { return extreme(w, 1) - extreme(w, -1) }),
		"ta.percentrank": rolling("", func(w []float64) float64 {
			cur := w[len(w)-1]
			if math.IsNaN(cur) {
				return nan
			}
			k := 0
			for _, v := range w[:len(w)-1] {
				if v <= cur {
					k++
				}
			}
			return 100 * float64(k) / float64(len(w)-1)
		}),
		"ta.vwma": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			src := a[0]
			pv := c.sma(numX(func(m *Machine) float64 { return src.f(m) * m.bars[m.i].V }), a[1])
			v := c.sma(numX(func(m *Machine) float64 { return m.bars[m.i].V }), a[1])
			return numX(func(m *Machine) float64 {
				p, q := pv.f(m), v.f(m)
				if q == 0 {
					return nan
				}
				return p / q
			}), nil
		}},
		"ta.hma": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			ln := a[1]
			half := c.wma(a[0], numX(func(m *Machine) float64 { return math.Floor(ln.f(m) / 2) }))
			full := c.wma(a[0], ln)
			diff := numX(func(m *Machine) float64 { return 2*half.f(m) - full.f(m) })
			return c.wma(diff, numX(func(m *Machine) float64 { return math.Round(math.Sqrt(ln.f(m))) })), nil
		}},
		"ta.alma": {params: []string{"series", "length", "offset", "sigma", "floor"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"series", "length", "offset", "sigma"}, "series", "length", "offset", "sigma"); err != nil {
				return cx{}, err
			}
			rec, b := c.record(a[0])
			fl := or(a[4], 0)
			return numX(func(m *Machine) float64 {
				rec(m)
				n := length(a[1].f(m))
				w := win(m, b, n)
				if w == nil || anyNaN(w) {
					return nan
				}
				off, sigma := a[2].f(m), a[3].f(m)
				mm := off * float64(n-1)
				if truth(fl.f(m)) {
					mm = math.Floor(mm)
				}
				s := float64(n) / sigma
				norm, sum := 0.0, 0.0
				for i := 0; i < n; i++ {
					wt := math.Exp(-((float64(i) - mm) * (float64(i) - mm)) / (2 * s * s))
					norm += wt
					sum += w[i] * wt
				}
				return sum / norm
			}), nil
		}},
		"ta.swma": {params: []string{"source"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source"}, "source"); err != nil {
				return cx{}, err
			}
			rec, b := c.record(a[0])
			return numX(func(m *Machine) float64 {
				rec(m)
				w := win(m, b, 4)
				if w == nil {
					return nan
				}
				return w[0]/6 + w[1]*2/6 + w[2]*2/6 + w[3]/6
			}), nil
		}},
		"ta.linreg": {params: []string{"source", "length", "offset"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			rec, b := c.record(a[0])
			off := or(a[2], 0)
			return numX(func(m *Machine) float64 {
				rec(m)
				n := length(a[1].f(m))
				w := win(m, b, n)
				if w == nil || anyNaN(w) {
					return nan
				}
				var sx, sy, sxx, sxy float64
				for j, v := range w {
					fx := float64(j)
					sx, sy, sxx, sxy = sx+fx, sy+v, sxx+fx*fx, sxy+fx*v
				}
				fn := float64(n)
				d := fn*sxx - sx*sx
				slope := 0.0
				if d != 0 {
					slope = (fn*sxy - sx*sy) / d
				}
				intercept := (sy - slope*sx) / fn
				return intercept + slope*(fn-1-off.f(m))
			}), nil
		}},
		"ta.correlation": {params: []string{"source1", "source2", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source1", "source2", "length"}, "source1", "source2", "length"); err != nil {
				return cx{}, err
			}
			ra, ba := c.record(a[0])
			rb, bb := c.record(a[1])
			return numX(func(m *Machine) float64 {
				ra(m)
				rb(m)
				n := length(a[2].f(m))
				wa, wb := win(m, ba, n), win(m, bb, n)
				if wa == nil || anyNaN(wa) || anyNaN(wb) {
					return nan
				}
				ma, mb := mean(wa), mean(wb)
				var sab, saa, sbb float64
				for j := range wa {
					da, db := wa[j]-ma, wb[j]-mb
					sab, saa, sbb = sab+da*db, saa+da*da, sbb+db*db
				}
				if saa == 0 || sbb == 0 {
					return nan
				}
				return sab / math.Sqrt(saa*sbb)
			}), nil
		}},
		"ta.change": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.change(a, x, func(v, old float64) float64 { return v - old })
		}},
		"ta.mom": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			return c.change(a, x, func(v, old float64) float64 { return v - old })
		}},
		"ta.roc": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			return c.change(a, x, func(v, old float64) float64 {
				if old == 0 {
					return nan
				}
				return 100 * (v - old) / old
			})
		}},
		"ta.rsi": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			rec, b := c.record(a[0])
			up := c.smooth(numX(func(m *Machine) float64 { return math.Max(rec(m)-past(m, b, 1), 0) }), a[1], rmaAlpha)
			down := c.smooth(numX(func(m *Machine) float64 { return -math.Min(m.buf[b][m.i]-past(m, b, 1), 0) }), a[1], rmaAlpha)
			return numX(func(m *Machine) float64 {
				u := up.f(m)
				d := down.f(m)
				switch {
				case math.IsNaN(u) || math.IsNaN(d):
					return nan
				case d == 0:
					return 100
				case u == 0:
					return 0
				}
				return 100 - 100/(1+u/d)
			}), nil
		}},
		"ta.tr": {params: []string{"handle_na"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			h := or(a[0], 0)
			return numX(func(m *Machine) float64 { return trueRange(m, truth(h.f(m))) }), nil
		}},
		"ta.atr": {params: []string{"length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"length"}, "length"); err != nil {
				return cx{}, err
			}
			return c.smooth(trX(true), a[0], rmaAlpha), nil
		}},
		"ta.cum": {params: []string{"source"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source"}, "source"); err != nil {
				return cx{}, err
			}
			return c.cum(a[0]), nil
		}},
		"ta.max": {params: []string{"source"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.allTime(a[0], math.Max), nil
		}},
		"ta.min": {params: []string{"source"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.allTime(a[0], math.Min), nil
		}},
		"ta.cross": {params: []string{"source1", "source2"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.cross(a, x, func(p, q float64) bool { return p > 0 && q <= 0 || p < 0 && q >= 0 })
		}},
		"ta.crossover": {params: []string{"source1", "source2"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.cross(a, x, func(p, q float64) bool { return p > 0 && q <= 0 })
		}},
		"ta.crossunder": {params: []string{"source1", "source2"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.cross(a, x, func(p, q float64) bool { return p < 0 && q >= 0 })
		}},
		"ta.rising": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.trend(a, x, 1)
		}},
		"ta.falling": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.trend(a, x, -1)
		}},
		"ta.barssince": {params: []string{"condition"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"condition"}, "condition"); err != nil {
				return cx{}, err
			}
			last := c.newBuf(false)
			return numX(func(m *Machine) float64 {
				at := nan
				if m.i > 0 {
					at = m.buf[last][m.i-1]
				}
				if truth(a[0].f(m)) {
					at = float64(m.i)
				}
				m.buf[last][m.i] = at
				return float64(m.i) - at
			}), nil
		}},
		"ta.valuewhen": {params: []string{"condition", "source", "occurrence"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"condition", "source"}, "condition", "source"); err != nil {
				return cx{}, err
			}
			rc, bc := c.record(a[0])
			rs, bs := c.record(a[1])
			occ := or(a[2], 0)
			return numX(func(m *Machine) float64 {
				rc(m)
				rs(m)
				k := length(occ.f(m))
				for j := m.i; j >= 0; j-- {
					if truth(m.buf[bc][j]) {
						if k == 0 {
							return m.buf[bs][j]
						}
						k--
					}
				}
				return nan
			}), nil
		}},
		"ta.pivothigh": {params: []string{"source", "leftbars", "rightbars"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.pivot(a, x, "high", 1)
		}},
		"ta.pivotlow": {params: []string{"source", "leftbars", "rightbars"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			return c.pivot(a, x, "low", -1)
		}},
		"ta.macd": {params: []string{"source", "fastlen", "slowlen", "siglen"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "fastlen", "slowlen", "siglen"}, "source", "fastlen", "slowlen", "siglen"); err != nil {
				return cx{}, err
			}
			fast, slow := c.smooth(a[0], a[1], emaAlpha), c.smooth(a[0], a[2], emaAlpha)
			db := c.newBuf(false)
			dif := numX(func(m *Machine) float64 { return m.buf[db][m.i] })
			sig := c.smooth(dif, a[3], emaAlpha)
			return cx{k: kTuple, n: 3, tu: func(m *Machine) []float64 {
				d := fast.f(m) - slow.f(m)
				m.buf[db][m.i] = d
				s := sig.f(m)
				return []float64{d, s, d - s}
			}}, nil
		}},
		"ta.stoch": {params: []string{"source", "high", "low", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "high", "low", "length"}, "source", "high", "low", "length"); err != nil {
				return cx{}, err
			}
			rh, bh := c.record(a[1])
			rl, bl := c.record(a[2])
			return numX(func(m *Machine) float64 {
				rh(m)
				rl(m)
				v := a[0].f(m)
				n := length(a[3].f(m))
				wh, wl := win(m, bh, n), win(m, bl, n)
				if wh == nil {
					return nan
				}
				hi, lo := extreme(wh, 1), extreme(wl, -1)
				if hi == lo || math.IsNaN(hi) || math.IsNaN(lo) {
					return nan
				}
				return 100 * (v - lo) / (hi - lo)
			}), nil
		}},
		"ta.cci": {params: []string{"source", "length"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
				return cx{}, err
			}
			rec, b := c.record(a[0])
			return numX(func(m *Machine) float64 {
				v := rec(m)
				w := win(m, b, length(a[1].f(m)))
				if w == nil || anyNaN(w) {
					return nan
				}
				mu := mean(w)
				dev := 0.0
				for _, u := range w {
					dev += math.Abs(u - mu)
				}
				dev /= float64(len(w))
				if dev == 0 {
					return 0
				}
				return (v - mu) / (0.015 * dev)
			}), nil
		}},
		"ta.vwap": {params: []string{"source", "anchor", "stdev_mult"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"source"}, "source"); err != nil {
				return cx{}, err
			}
			return c.vwap(a[0], a[1], a[2]), nil
		}},

		"str.tostring": {params: []string{"value", "format"}, pure: true, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			v := a[0]
			if v.k == kStr {
				return v, nil
			}
			return cx{k: kStr, s: func(m *Machine) string { return numText(v.f(m)) }}, nil
		}},
		"str.format": {params: []string{"formatString", "arg…"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if !given(a[0]) || a[0].k != kStr {
				return cx{}, errAt(x.t, "str.format 的第一个参数要是文字")
			}
			return cx{k: kStr, s: func(m *Machine) string {
				s := a[0].s(m)
				for j, v := range a[1:] {
					t := ""
					switch v.k {
					case kStr:
						t = v.s(m)
					case kFloat:
						t = numText(v.f(m))
					}
					s = strings.ReplaceAll(s, "{"+strconv.Itoa(j)+"}", t)
				}
				return s
			}}, nil
		}},

		"str.length": {params: []string{"string"}, pure: true, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if !given(a[0]) || a[0].k != kStr {
				return cx{}, errAt(x.t, "str.length 要一段文字")
			}
			return numX(func(m *Machine) float64 { return float64(len([]rune(a[0].s(m)))) }), nil
		}},
		"str.contains": {params: []string{"source", "str"}, pure: true, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if !given(a[0]) || !given(a[1]) || a[0].k != kStr || a[1].k != kStr {
				return cx{}, errAt(x.t, "str.contains 要两段文字")
			}
			return numX(func(m *Machine) float64 { return boolF(strings.Contains(a[0].s(m), a[1].s(m))) }), nil
		}},
		"str.upper": strFn(strings.ToUpper),
		"str.lower": strFn(strings.ToLower),

		"color.new": pure([]string{"color", "transp"}, func(a []cx) func(m *Machine) float64 {
			tr := or(a[1], 0)
			return func(m *Machine) float64 {
				r, g, b, _, ok := Unpack(a[0].f(m))
				t := tr.f(m)
				if !ok || math.IsNaN(t) {
					return nan
				}
				return Pack(r, g, b, alphaOf(t))
			}
		}),
		"color.rgb": pure([]string{"red", "green", "blue", "transp"}, func(a []cx) func(m *Machine) float64 {
			tr := or(a[3], 0)
			return func(m *Machine) float64 {
				r, g, b, t := a[0].f(m), a[1].f(m), a[2].f(m), tr.f(m)
				if math.IsNaN(r + g + b + t) {
					return nan
				}
				return Pack(channel(r), channel(g), channel(b), alphaOf(t))
			}
		}),
		"color.r": channelFn(0), "color.g": channelFn(1), "color.b": channelFn(2), "color.t": channelFn(3),
		"color.from_gradient": pure([]string{"value", "bottom_value", "top_value", "bottom_color", "top_color"}, func(a []cx) func(m *Machine) float64 {
			return func(m *Machine) float64 {
				v, lo, hi := a[0].f(m), a[1].f(m), a[2].f(m)
				r1, g1, b1, a1, ok1 := Unpack(a[3].f(m))
				r2, g2, b2, a2, ok2 := Unpack(a[4].f(m))
				if math.IsNaN(v) || !ok1 || !ok2 {
					return nan
				}
				t := 0.0
				if hi != lo {
					t = min(max((v-lo)/(hi-lo), 0), 1)
				}
				mix := func(p, q uint8) uint8 { return uint8(math.Round(float64(p) + (float64(q)-float64(p))*t)) }
				return Pack(mix(r1, r2), mix(g1, g2), mix(b1, b2), mix(a1, a2))
			}
		}),

		"timestamp": {params: []string{"year", "month", "day", "hour", "minute", "second"}, compile: compileTimestamp},
		"timeframe.change": {params: []string{"timeframe"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if !given(a[0]) || a[0].k != kStr || !a[0].konst {
				return cx{}, errAt(x.t, "timeframe.change 要写固定的周期，如 \"D\"")
			}
			tf := a[0].ks
			return numX(func(m *Machine) float64 {
				if m.i == 0 {
					return 1
				}
				return boolF(m.periodOf(tf, m.bars[m.i].T) != m.periodOf(tf, m.bars[m.i-1].T))
			}), nil
		}},
		"time": {params: []string{"timeframe", "session", "timezone"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if given(a[1]) {
				return cx{}, errAt(a[1].tok, "Candle 不支持 time() 的交易时段参数")
			}
			tf := ""
			if given(a[0]) {
				if a[0].k != kStr || !a[0].konst {
					return cx{}, errAt(x.t, "time() 的周期要写固定的，如 \"D\"")
				}
				tf = a[0].ks
			}
			return numX(func(m *Machine) float64 {
				if tf == "" || tf == m.env.Period {
					return float64(m.bars[m.i].T)
				}
				return float64(m.periodOf(tf, m.bars[m.i].T))
			}), nil
		}},
	}
	for _, f := range []string{"year", "month", "dayofmonth", "dayofweek", "hour", "minute", "second", "weekofyear"} {
		builtinFuncs[f] = &bfn{params: []string{"time", "timezone"}, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
			if err := need(x, a, []string{"time"}, "time"); err != nil {
				return cx{}, err
			}
			part := f
			return numX(func(m *Machine) float64 {
				t := a[0].f(m)
				if math.IsNaN(t) {
					return nan
				}
				return timePart(time.UnixMilli(int64(t)).In(m.env.Loc), part)
			}), nil
		}}
	}
	for name, kind := range map[string]string{"input.int": "int", "input.float": "float", "input.bool": "bool",
		"input.source": "source", "input.string": "string", "input.color": "color", "input": "", "input.price": "float",
		"input.timeframe": "fixed", "input.session": "fixed", "input.symbol": "fixed", "input.text_area": "fixed", "input.time": "fixedNum"} {
		builtinFuncs[name] = inputFn(kind)
	}
	builtinFuncs["indicator"] = &bfn{params: []string{"title", "shorttitle", "overlay", "format", "precision"}, global: true, compile: compileIndicator}
	builtinFuncs["plot"] = &bfn{params: []string{"series", "title", "color", "linewidth", "style", "trackprice", "histbase", "offset", "join", "editable", "show_last", "display", "format", "precision", "force_overlay"}, global: true, compile: compilePlot}
	builtinFuncs["hline"] = &bfn{params: []string{"price", "title", "color", "linestyle", "linewidth", "editable", "display"}, global: true, compile: compileHline}
	builtinFuncs["fill"] = &bfn{params: []string{"hline1", "hline2", "color", "title", "editable", "fillgaps", "display", "top_color", "bottom_color", "top_value", "bottom_value"}, global: true, compile: compileFill}
	builtinFuncs["bgcolor"] = &bfn{params: []string{"color", "offset", "editable", "show_last", "title", "display", "overlay", "force_overlay"}, global: true, compile: compileBg}
	builtinFuncs["plotshape"] = &bfn{params: []string{"series", "title", "style", "location", "color", "offset", "text", "textcolor", "editable", "size", "show_last", "display", "format", "precision", "force_overlay"}, global: true, compile: shapeFn(false)}
	builtinFuncs["plotchar"] = &bfn{params: []string{"series", "title", "char", "location", "color", "offset", "text", "textcolor", "editable", "size", "show_last", "display", "format", "precision", "force_overlay"}, global: true, compile: shapeFn(true)}
	builtinFuncs["plotarrow"] = &bfn{params: []string{"series", "title", "colorup", "colordown", "offset", "minheight", "maxheight", "editable", "show_last", "display", "format", "precision", "force_overlay"}, global: true, compile: compileArrow}
}

func roundHalfUp(v float64) float64 { return math.Floor(v + 0.5) }

func channel(v float64) uint8 { return uint8(min(max(math.Round(v), 0), 255)) }

// alphaOf is the alpha of transparency t, 0 opaque to 100 clear.
func alphaOf(t float64) uint8 { return uint8(math.Round(255 * (1 - min(max(t, 0), 100)/100))) }

func channelFn(k int) *bfn {
	return pure([]string{"color"}, func(a []cx) func(m *Machine) float64 {
		return func(m *Machine) float64 {
			r, g, b, al, ok := Unpack(a[0].f(m))
			if !ok {
				return nan
			}
			switch k {
			case 0:
				return float64(r)
			case 1:
				return float64(g)
			case 2:
				return float64(b)
			}
			return math.Round((1 - float64(al)/255) * 100)
		}
	})
}

func numText(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	return strconv.FormatFloat(math.Round(v*1e8)/1e8, 'f', -1, 64)
}

func extreme(w []float64, sign float64) float64 {
	r := nan
	for _, v := range w {
		if !math.IsNaN(v) && (math.IsNaN(r) || (v-r)*sign > 0) {
			r = v
		}
	}
	return r
}

// extremeAt is how many bars back from the newest the highest (sign 1) or
// lowest value of w is, negative: the newest of those alike.
func extremeAt(w []float64, sign float64) float64 {
	best, at := nan, nan
	for j := len(w) - 1; j >= 0; j-- {
		if v := w[j]; !math.IsNaN(v) && (math.IsNaN(best) || (v-best)*sign > 0) {
			best, at = v, float64(j-(len(w)-1))
		}
	}
	return at
}

func (c *compiler) deviation(a []cx, x *callX, root bool) (cx, error) {
	if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
		return cx{}, err
	}
	rec, b := c.record(a[0])
	biased := or(a[2], 1)
	return numX(func(m *Machine) float64 {
		rec(m)
		w := win(m, b, length(a[1].f(m)))
		if w == nil || anyNaN(w) {
			return nan
		}
		mu := mean(w)
		s := 0.0
		for _, v := range w {
			s += (v - mu) * (v - mu)
		}
		d := float64(len(w))
		if !truth(biased.f(m)) {
			d--
		}
		v := s / d
		if root {
			return math.Sqrt(v)
		}
		return v
	}), nil
}

func (c *compiler) change(a []cx, x *callX, f func(v, old float64) float64) (cx, error) {
	if !given(a[0]) || a[0].k != kFloat {
		return cx{}, errAt(x.t, "%s 少了参数 source", x.fn)
	}
	rec, b := c.record(a[0])
	ln := or(a[1], 1)
	return numX(func(m *Machine) float64 {
		v := rec(m)
		return f(v, past(m, b, length(ln.f(m))))
	}), nil
}

func (c *compiler) cum(src cx) cx {
	o := c.newBuf(false)
	return numX(func(m *Machine) float64 {
		v := src.f(m)
		prev := 0.0
		if m.i > 0 && !math.IsNaN(m.buf[o][m.i-1]) {
			prev = m.buf[o][m.i-1]
		}
		if !math.IsNaN(v) {
			prev += v
		}
		m.buf[o][m.i] = prev
		return prev
	})
}

func (c *compiler) allTime(src cx, f func(a, b float64) float64) cx {
	o := c.newBuf(false)
	return numX(func(m *Machine) float64 {
		v := src.f(m)
		if m.i > 0 && !math.IsNaN(m.buf[o][m.i-1]) {
			if math.IsNaN(v) {
				v = m.buf[o][m.i-1]
			} else {
				v = f(v, m.buf[o][m.i-1])
			}
		}
		m.buf[o][m.i] = v
		return v
	})
}

func (c *compiler) cross(a []cx, x *callX, f func(now, before float64) bool) (cx, error) {
	if err := need(x, a, []string{"source1", "source2"}, "source1", "source2"); err != nil {
		return cx{}, err
	}
	ra, ba := c.record(a[0])
	rb, bb := c.record(a[1])
	return numX(func(m *Machine) float64 {
		p := ra(m) - rb(m)
		q := past(m, ba, 1) - past(m, bb, 1)
		if math.IsNaN(p) || math.IsNaN(q) {
			return 0
		}
		return boolF(f(p, q))
	}), nil
}

func (c *compiler) trend(a []cx, x *callX, sign float64) (cx, error) {
	if err := need(x, a, []string{"source", "length"}, "source", "length"); err != nil {
		return cx{}, err
	}
	rec, b := c.record(a[0])
	return numX(func(m *Machine) float64 {
		v := rec(m)
		n := length(a[1].f(m))
		if m.i < n || math.IsNaN(v) {
			return 0
		}
		for k := 1; k <= n; k++ {
			if (v-past(m, b, k))*sign <= 0 {
				return 0
			}
		}
		return 1
	}), nil
}

// pivot is the value of the bar rightbars back when it is above (sign 1)
// or below all leftbars bars before it and rightbars after it, else na.
func (c *compiler) pivot(a []cx, x *callX, def string, sign float64) (cx, error) {
	src, left, right := a[0], a[1], a[2]
	if !given(right) {
		src, left, right = barX(def), a[0], a[1]
	}
	if !given(src) || !given(left) || !given(right) {
		return cx{}, errAt(x.t, "%s 少了参数", x.fn)
	}
	rec, b := c.record(src)
	return numX(func(m *Machine) float64 {
		rec(m)
		l, r := length(left.f(m)), length(right.f(m))
		w := win(m, b, l+r+1)
		if w == nil {
			return nan
		}
		p := w[l]
		if math.IsNaN(p) {
			return nan
		}
		for j, v := range w {
			if j != l && (math.IsNaN(v) || (p-v)*sign <= 0) {
				return nan
			}
		}
		return p
	}), nil
}

// vwap is the volume weighted average of src from the start of each
// trading day, or the bars anchor holds on, on; with a multiplier, that
// and the bands that many standard deviations above and below it.
func (c *compiler) vwap(src, anchor, mult cx) cx {
	pv, vv, p2 := c.newBuf(false), c.newBuf(false), c.newBuf(false)
	calc := func(m *Machine) (float64, float64) {
		p, v := src.f(m), m.bars[m.i].V
		start := m.i == 0
		if given(anchor) {
			start = start || truth(anchor.f(m))
		} else if !start {
			start = m.dayOf(m.bars[m.i].T) != m.dayOf(m.bars[m.i-1].T)
		}
		a, b, s := p*v, v, p*p*v
		if !start {
			a, b, s = a+m.buf[pv][m.i-1], b+m.buf[vv][m.i-1], s+m.buf[p2][m.i-1]
		}
		m.buf[pv][m.i], m.buf[vv][m.i], m.buf[p2][m.i] = a, b, s
		if b == 0 {
			return nan, nan
		}
		w := a / b
		return w, math.Sqrt(math.Max(s/b-w*w, 0))
	}
	if !given(mult) {
		return numX(func(m *Machine) float64 { w, _ := calc(m); return w })
	}
	return cx{k: kTuple, n: 3, tu: func(m *Machine) []float64 {
		w, sd := calc(m)
		k := mult.f(m)
		return []float64{w, w + sd*k, w - sd*k}
	}}
}

func (m *Machine) tick() float64 {
	if m.env.Tick > 0 {
		return m.env.Tick
	}
	return 0.01
}

func (m *Machine) dayOf(t int64) int64 {
	if m.env.DayOf != nil {
		return m.env.DayOf(t)
	}
	return time.UnixMilli(t).In(m.env.Loc).Truncate(24 * time.Hour).UnixMilli()
}

// periodOf is when the period tf ("D", "W", "M", or minutes) the bar
// opening at t is in starts.
func (m *Machine) periodOf(tf string, t int64) int64 {
	at := time.UnixMilli(t).In(m.env.Loc)
	switch strings.TrimLeft(tf, "1") {
	case "D":
		return m.dayOf(t)
	case "W":
		d := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, m.env.Loc)
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)).UnixMilli()
	case "M":
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, m.env.Loc).UnixMilli()
	}
	if mins, err := strconv.Atoi(tf); err == nil && mins > 0 {
		span := int64(mins) * 60_000
		return t / span * span
	}
	return t
}

func timePart(t time.Time, part string) float64 {
	switch part {
	case "year":
		return float64(t.Year())
	case "month":
		return float64(t.Month())
	case "dayofmonth":
		return float64(t.Day())
	case "dayofweek":
		return float64(t.Weekday()) + 1
	case "hour":
		return float64(t.Hour())
	case "minute":
		return float64(t.Minute())
	case "second":
		return float64(t.Second())
	}
	_, w := t.ISOWeek()
	return float64(w)
}

func compileTimestamp(c *compiler, a []cx, x *callX) (cx, error) {
	if given(a[0]) && a[0].k == kStr {
		return cx{}, errAt(x.t, "Candle 的 timestamp 不支持时区参数")
	}
	if err := need(x, a, []string{"year", "month", "day"}, "year", "month", "day"); err != nil {
		return cx{}, err
	}
	h, mi, s := or(a[3], 0), or(a[4], 0), or(a[5], 0)
	return numX(func(m *Machine) float64 {
		t := time.Date(int(a[0].f(m)), time.Month(int(a[1].f(m))), int(a[2].f(m)), int(h.f(m)), int(mi.f(m)), int(s.f(m)), 0, m.env.Loc)
		return float64(t.UnixMilli())
	}), nil
}

func strFn(f func(string) string) *bfn {
	return &bfn{params: []string{"source"}, pure: true, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
		if !given(a[0]) || a[0].k != kStr {
			return cx{}, errAt(x.t, "%s 要一段文字", x.fn)
		}
		return cx{k: kStr, s: func(m *Machine) string { return f(a[0].s(m)) }}, nil
	}}
}
