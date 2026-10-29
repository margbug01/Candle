package pine

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The declarations: indicator, the inputs, and what a script draws.

func konstStr(a cx, def string) (string, bool) {
	if !given(a) {
		return def, true
	}
	if a.k == kStr && a.konst {
		return a.ks, true
	}
	return "", false
}

func konstNum(a cx, def float64) (float64, bool) {
	if !given(a) {
		return def, true
	}
	if a.k == kFloat && a.konst {
		return a.kv, true
	}
	return 0, false
}

func compileIndicator(c *compiler, a []cx, x *callX) (cx, error) {
	s := c.s
	if s.declared {
		return cx{}, errAt(x.t, "indicator 只能写一次")
	}
	title, ok := konstStr(a[0], "")
	if !ok || title == "" {
		return cx{}, errAt(x.t, "indicator 的第一个参数要是指标名字，如 indicator(\"我的指标\")")
	}
	short, _ := konstStr(a[1], "")
	overlay, ok := konstNum(a[2], 0)
	if !ok {
		return cx{}, errAt(a[2].tok, "overlay 要写 true 或 false")
	}
	if f, ok := konstStr(a[3], "inherit"); ok {
		s.Format = f
	}
	if p, ok := konstNum(a[4], -1); ok && p >= 0 {
		s.Precision = min(int(p), 10)
	}
	s.Title, s.Short, s.Overlay, s.declared = title, short, truth(overlay), true
	return cx{k: kVoid}, nil
}

// inputFn is input.int, input.float and the others; kind "" is input(),
// which takes its kind from its default.
func inputFn(kind string) *bfn {
	params := []string{"defval", "title", "minval", "maxval", "step", "options", "tooltip", "type"}
	switch kind {
	case "bool", "color", "source", "fixed", "fixedNum":
		params = []string{"defval", "title", "tooltip"}
	case "string":
		params = []string{"defval", "title", "options", "tooltip"}
	}
	return &bfn{params: params, compile: func(c *compiler, a []cx, x *callX) (cx, error) {
		get := func(n string) cx {
			for i, p := range params {
				if p == n {
					return a[i]
				}
			}
			return cx{k: kMissing}
		}
		def := get("defval")
		if !given(def) {
			return cx{}, errAt(x.t, "%s 少了默认值", x.fn)
		}
		k := kind
		if t := get("type"); given(t) && t.k == kStr { // version 4's input(…, type = input.integer)
			k = map[string]string{"integer": "int", "float": "float", "bool": "bool", "source": "source", "string": "string", "color": "color"}[t.ks]
		}
		if k == "" {
			switch {
			case def.source != "":
				k = "source"
			case def.k == kStr:
				k = "string"
			case def.k == kFloat && def.konst && def.kv == math.Trunc(def.kv) && !strings.Contains(def.tok.text, "."):
				k = "int"
			default:
				k = "float"
			}
		}
		in := Input{Kind: k, Min: nan, Max: nan, Step: nan}
		title, ok := konstStr(get("title"), c.hint)
		if !ok {
			return cx{}, errAt(get("title").tok, "输入的标题要是固定的文字")
		}
		if title == "" {
			title = "参数 " + strconv.Itoa(len(c.s.Inputs)+1)
		}
		in.Title = title
		in.Tooltip, _ = konstStr(get("tooltip"), "")
		for _, f := range []struct {
			n string
			v *float64
		}{{"minval", &in.Min}, {"maxval", &in.Max}, {"step", &in.Step}} {
			if v := get(f.n); given(v) {
				if !v.konst || v.k != kFloat {
					return cx{}, errAt(v.tok, "%s 要是固定的数", f.n)
				}
				*f.v = v.kv
			}
		}
		switch k {
		case "fixed", "fixedNum":
			if def.konst {
				if def.k == kStr {
					return strX(def.ks), nil
				}
				return konstX(def.kv), nil
			}
			return def, nil
		case "source":
			src := def.source
			if src == "" {
				return cx{}, errAt(def.tok, "源的默认值要是 close、open、hl2 这类")
			}
			in.Def = float64(max(indexOf(Sources, src), 0))
			j := c.addInput(in)
			v := numX(func(m *Machine) float64 { return m.source(Sources[sourceIdx(m.inputs[j])]) })
			v.hist = func(m *Machine, k int) float64 {
				if k > m.i {
					return nan
				}
				return sourceOf(m.bars[m.i-k], Sources[sourceIdx(m.inputs[j])])
			}
			return v, nil
		case "string":
			if !def.konst || def.k != kStr {
				return cx{}, errAt(def.tok, "文字输入的默认值要是固定的文字")
			}
			if o := get("options"); given(o) {
				opts, err := c.options(x, o)
				if err != nil {
					return cx{}, err
				}
				in.Options = opts
				in.Def = float64(max(indexOf(opts, def.ks), 0))
				j := c.addInput(in)
				return cx{k: kStr, simple: true, s: func(m *Machine) string {
					return opts[min(max(int(m.inputs[j]), 0), len(opts)-1)]
				}}, nil
			}
			in.Fixed = def.ks
			c.addInput(in)
			return strX(def.ks), nil
		}
		if !def.konst || def.k != kFloat {
			return cx{}, errAt(def.tok, "输入的默认值要是固定的数")
		}
		in.Def = def.kv
		if o := get("options"); given(o) {
			nums, err := c.numOptions(x, o)
			if err != nil {
				return cx{}, err
			}
			in.NumOptions = nums
		}
		j := c.addInput(in)
		v := numX(func(m *Machine) float64 { return m.inputs[j] })
		v.simple = true
		return v, nil
	}}
}

func sourceIdx(v float64) int { return min(max(int(v), 0), len(Sources)-1) }

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func (c *compiler) addInput(in Input) int {
	c.s.Inputs = append(c.s.Inputs, in)
	return len(c.s.Inputs) - 1
}

// options reads options = ["a", "b"], which the parser left as a tuple.
func (c *compiler) options(x *callX, o cx) ([]string, error) {
	for _, a := range x.args {
		if a.name == "options" {
			tu, ok := a.x.(*tupleX)
			if !ok {
				break
			}
			var out []string
			for _, e := range tu.xs {
				s, ok := e.(*strLit)
				if !ok {
					return nil, errAt(e.at(), "options 里要是固定的文字")
				}
				out = append(out, s.t.text)
			}
			return out, nil
		}
	}
	return nil, errAt(x.t, "options 要写成 options = [\"A\", \"B\"]")
}

func (c *compiler) numOptions(x *callX, o cx) ([]float64, error) {
	for _, a := range x.args {
		if a.name == "options" {
			if tu, ok := a.x.(*tupleX); ok {
				var out []float64
				for _, e := range tu.xs {
					v, err := c.expr(e, c.global)
					if err != nil || !v.konst || v.k != kFloat {
						return nil, errAt(e.at(), "options 里要是固定的数")
					}
					out = append(out, v.kv)
				}
				return out, nil
			}
		}
	}
	return nil, errAt(x.t, "options 要写成 options = [1, 2, 3]")
}

const defaultPlotColor = 0x2962ffff

// colorArg is a call's color argument: its value now and whether that
// changes from bar to bar.
func colorArg(a cx, def uint32) (cx, uint32, bool) {
	if !given(a) {
		return konstX(float64(def)), def, false
	}
	if a.konst {
		if math.IsNaN(a.kv) {
			return a, 0, false
		}
		return a, uint32(a.kv), false
	}
	return a, 0, true
}

func compilePlot(c *compiler, a []cx, x *callX) (cx, error) {
	if err := need(x, a, []string{"series"}, "series"); err != nil {
		return cx{}, err
	}
	title, ok := konstStr(a[1], "")
	if !ok {
		return cx{}, errAt(a[1].tok, "plot 的 title 要是固定的文字")
	}
	if title == "" {
		title = "Plot"
		if c.hint != "" {
			title = c.hint
		}
	}
	col, first, dynamic := colorArg(a[2], defaultPlotColor)
	style, ok := konstNum(a[4], StyleLine)
	if !ok {
		return cx{}, errAt(a[4].tok, "plot 的 style 要是固定的，如 plot.style_line")
	}
	width, ok := konstNum(a[3], 1)
	if !ok && !a[3].simple {
		return cx{}, errAt(a[3].tok, "linewidth 要是固定的数")
	}
	hb, ok := konstNum(a[6], 0)
	if !ok {
		return cx{}, errAt(a[6].tok, "histbase 要是固定的数")
	}
	hidden := false
	if d, ok := konstNum(a[11], displayAll); ok {
		hidden = int(d)&displayPane == 0
	}
	p := Plot{Title: title, Color: first, Dynamic: dynamic, Style: int(style), Width: width, Histbase: hb, Hidden: hidden}
	c.s.Plots = append(c.s.Plots, p)
	j := len(c.s.Plots) - 1
	series, lw, off := a[0], a[3], or(a[7], 0)
	if !off.simple {
		return cx{}, errAt(a[7].tok, "offset 要是固定的数")
	}
	return cx{k: kPlot, id: j, f: func(m *Machine) float64 {
		m.Plots[j][m.i] = series.f(m)
		m.PlotColors[j][m.i] = col.f(m)
		if m.i == 0 {
			m.PlotOffset[j] = int(math.Round(off.f(m)))
			if given(lw) {
				m.PlotWidth[j] = lw.f(m)
			}
		}
		return nan
	}}, nil
}

const defaultHlineColor = 0x787b86ff

func compileHline(c *compiler, a []cx, x *callX) (cx, error) {
	if err := need(x, a, []string{"price"}, "price"); err != nil {
		return cx{}, err
	}
	price := a[0]
	if !price.simple {
		return cx{}, errAt(price.tok, "hline 的价格要是固定的数（可以是输入）")
	}
	title, _ := konstStr(a[1], "Hline")
	_, col, dynamic := colorArg(a[2], defaultHlineColor)
	if dynamic {
		col = defaultHlineColor
		if a[2].simple {
			c.warn(a[2].tok, "hline 的颜色只认固定的，这里用默认的灰色")
		}
	}
	st, ok := konstNum(a[3], LineDashed)
	if !ok {
		return cx{}, errAt(a[3].tok, "linestyle 要是固定的，如 hline.style_dashed")
	}
	w, _ := konstNum(a[4], 1)
	c.s.Hlines = append(c.s.Hlines, Hline{Title: title, Color: col, Style: int(st), Width: w})
	j := len(c.s.Hlines) - 1
	return cx{k: kHline, id: j, f: func(m *Machine) float64 {
		if m.i == 0 {
			m.HlinePrice[j] = price.f(m)
		}
		return nan
	}}, nil
}

func compileFill(c *compiler, a []cx, x *callX) (cx, error) {
	p1, p2 := a[0], a[1]
	if !given(p1) || !given(p2) {
		return cx{}, errAt(x.t, "fill 要两个 plot 或两个 hline")
	}
	if p1.k != p2.k || p1.k != kPlot && p1.k != kHline {
		return cx{}, errAt(x.t, "fill 的前两个参数要都是 plot() 的结果，或都是 hline() 的结果")
	}
	colA := a[2]
	if given(a[7]) { // the gradient: Candle takes its top color
		colA = a[7]
	} else if given(colA) && colA.k == kFloat && len(x.args) >= 6 && x.args[2].name == "" {
		return cx{}, errAt(x.t, "Candle 只支持纯色的 fill，渐变请写 top_color = …")
	}
	col, _, _ := colorArg(colA, 0x2962ff1a)
	title, _ := konstStr(a[3], "Fill")
	c.s.Fills = append(c.s.Fills, Fill{Title: title, A: p1.id, B: p2.id, Levels: p1.k == kHline})
	j := len(c.s.Fills) - 1
	return cx{k: kVoid, f: func(m *Machine) float64 {
		m.FillColors[j][m.i] = col.f(m)
		return nan
	}}, nil
}

func compileBg(c *compiler, a []cx, x *callX) (cx, error) {
	if !given(a[0]) {
		return cx{}, errAt(x.t, "bgcolor 少了颜色")
	}
	title, _ := konstStr(a[4], "Background")
	c.s.Bgs = append(c.s.Bgs, Bg{Title: title})
	j := len(c.s.Bgs) - 1
	col := a[0]
	return cx{k: kVoid, f: func(m *Machine) float64 {
		m.BgColors[j][m.i] = col.f(m)
		return nan
	}}, nil
}

func shapeFn(char bool) func(c *compiler, a []cx, x *callX) (cx, error) {
	return func(c *compiler, a []cx, x *callX) (cx, error) {
		if err := need(x, a, []string{"series"}, "series"); err != nil {
			return cx{}, err
		}
		title, _ := konstStr(a[1], "Shape")
		sh := Shape{Title: title, Style: ShapeXCross, Location: LocAboveBar, TextColor: defaultPlotColor}
		if char {
			sh.Style, sh.Char = ShapeChar, "★"
			if ch, ok := konstStr(a[2], "★"); ok && ch != "" {
				sh.Char = ch
			}
		} else if st, ok := konstNum(a[2], ShapeXCross); ok {
			sh.Style = int(st)
		} else {
			return cx{}, errAt(a[2].tok, "style 要是固定的，如 shape.triangleup")
		}
		if loc, ok := konstNum(a[3], LocAboveBar); ok {
			sh.Location = int(loc)
		} else {
			return cx{}, errAt(a[3].tok, "location 要是固定的，如 location.abovebar")
		}
		sh.Text, _ = konstStr(a[6], "")
		if tc, ok := konstNum(a[7], defaultPlotColor); ok && !math.IsNaN(tc) {
			sh.TextColor = uint32(tc)
		}
		if sz, ok := konstNum(a[9], 0); ok {
			sh.Size = int(sz)
		}
		col, _, _ := colorArg(a[4], defaultPlotColor)
		return c.addShape(sh, a[0], col, or(a[5], 0)), nil
	}
}

func compileArrow(c *compiler, a []cx, x *callX) (cx, error) {
	if err := need(x, a, []string{"series"}, "series"); err != nil {
		return cx{}, err
	}
	title, _ := konstStr(a[1], "Arrow")
	up, _, _ := colorArg(a[2], 0x4caf50ff)
	down, _, _ := colorArg(a[3], 0xff5252ff)
	series := a[0]
	col := numX(func(m *Machine) float64 {
		if series.f(m) >= 0 {
			return up.f(m)
		}
		return down.f(m)
	})
	return c.addShape(Shape{Title: title, Arrow: true, Style: ShapeArrowUp}, series, col, or(a[4], 0)), nil
}

func (c *compiler) addShape(sh Shape, series, col, off cx) cx {
	c.s.Shapes = append(c.s.Shapes, sh)
	j := len(c.s.Shapes) - 1
	return cx{k: kVoid, f: func(m *Machine) float64 {
		m.ShapeVals[j][m.i] = series.f(m)
		m.ShapeCols[j][m.i] = col.f(m)
		if m.i == 0 {
			m.ShapeOff[j] = int(math.Round(off.f(m)))
		}
		return nan
	}}
}

// The built-in variables and constants.

const (
	displayAll  = 31
	displayPane = 4
)

var colors = map[string]uint32{
	"aqua": 0x00bcd4ff, "black": 0x363a45ff, "blue": 0x2196f3ff, "fuchsia": 0xe040fbff, "gray": 0x787b86ff,
	"green": 0x4caf50ff, "lime": 0x00e676ff, "maroon": 0x880e4fff, "navy": 0x311b92ff, "olive": 0x808000ff,
	"orange": 0xff9800ff, "purple": 0x9c27b0ff, "red": 0xff5252ff, "silver": 0xb2b5beff, "teal": 0x00897bff,
	"white": 0xffffffff, "yellow": 0xffeb3bff,
}

var builtinVars = map[string]func(c *compiler) cx{}

func init() {
	for _, k := range []string{"open", "high", "low", "close", "volume", "hl2", "hlc3", "ohlc4", "hlcc4"} {
		builtinVars[k] = func(*compiler) cx { return barX(k) }
	}
	consts := map[string]float64{
		"math.pi": math.Pi, "math.e": math.E, "math.phi": 1.618033988749895, "math.rphi": 0.6180339887498948,
		"plot.style_line": StyleLine, "plot.style_linebr": StyleLineBr, "plot.style_stepline": StyleStep,
		"plot.style_stepline_diamond": StyleStep, "plot.style_steplinebr": StyleStepBr, "plot.style_histogram": StyleHistogram,
		"plot.style_columns": StyleColumns, "plot.style_circles": StyleCircles, "plot.style_cross": StyleCross,
		"plot.style_area": StyleArea, "plot.style_areabr": StyleAreaBr,
		"hline.style_solid": LineSolid, "hline.style_dashed": LineDashed, "hline.style_dotted": LineDotted,
		"shape.triangleup": ShapeTriangleUp, "shape.triangledown": ShapeTriangleDown, "shape.circle": ShapeCircle,
		"shape.cross": ShapeCross, "shape.xcross": ShapeXCross, "shape.arrowup": ShapeArrowUp, "shape.arrowdown": ShapeArrowDown,
		"shape.labelup": ShapeLabelUp, "shape.labeldown": ShapeLabelDown, "shape.square": ShapeSquare,
		"shape.diamond": ShapeDiamond, "shape.flag": ShapeFlag,
		"location.abovebar": LocAboveBar, "location.belowbar": LocBelowBar, "location.top": LocTop,
		"location.bottom": LocBottom, "location.absolute": LocAbsolute,
		"size.auto": 0, "size.tiny": 1, "size.small": 2, "size.normal": 3, "size.large": 4, "size.huge": 5,
		"display.none": 0, "display.all": displayAll, "display.data_window": 2, "display.pane": displayPane,
		"display.price_scale": 8, "display.status_line": 16,
		"dayofweek.sunday": 1, "dayofweek.monday": 2, "dayofweek.tuesday": 3, "dayofweek.wednesday": 4,
		"dayofweek.thursday": 5, "dayofweek.friday": 6, "dayofweek.saturday": 7,
		"syminfo.pointvalue": 1,
	}
	for k, v := range consts {
		builtinVars[k] = func(*compiler) cx { return konstX(v) }
	}
	for name, col := range colors {
		builtinVars["color."+name] = func(*compiler) cx { return konstX(float64(col)) }
	}
	for k, v := range map[string]string{"format.inherit": "inherit", "format.price": "price", "format.volume": "volume",
		"format.percent": "percent", "format.mintick": "price",
		"input.integer": "integer", "input.float": "float", "input.bool": "bool", "input.source": "source",
		"input.string": "string", "input.color": "color", "input.resolution": "string", "input.session": "string"} {
		builtinVars[k] = func(*compiler) cx { return strX(v) }
	}
	series := map[string]func(m *Machine) float64{
		"bar_index":        func(m *Machine) float64 { return float64(m.i) },
		"last_bar_index":   func(m *Machine) float64 { return float64(m.n - 1) },
		"time":             func(m *Machine) float64 { return float64(m.bars[m.i].T) },
		"time_close":       func(m *Machine) float64 { return float64(m.bars[m.i].T + m.env.IntervalMs) },
		"last_bar_time":    func(m *Machine) float64 { return float64(m.bars[m.n-1].T) },
		"timenow":          func(m *Machine) float64 { return float64(time.Now().UnixMilli()) },
		"syminfo.mintick":  func(m *Machine) float64 { return m.tick() },
		"barstate.isfirst": func(m *Machine) float64 { return boolF(m.i == 0) },
		"barstate.islast":  func(m *Machine) float64 { return boolF(m.i == m.n-1) },
		"barstate.isnew":   func(m *Machine) float64 { return 1 },
		"barstate.isconfirmed": func(m *Machine) float64 {
			return boolF(m.i < m.n-1 || m.env.LastClosed)
		},
		"barstate.isrealtime": func(m *Machine) float64 { return boolF(m.i == m.n-1 && !m.env.LastClosed) },
		"barstate.ishistory":  func(m *Machine) float64 { return boolF(m.i < m.n-1 || m.env.LastClosed) },
		"barstate.islastconfirmedhistory": func(m *Machine) float64 {
			if m.env.LastClosed {
				return boolF(m.i == m.n-1)
			}
			return boolF(m.i == m.n-2)
		},
		"session.isfirstbar": func(m *Machine) float64 {
			return boolF(m.i == 0 || m.dayOf(m.bars[m.i].T) != m.dayOf(m.bars[m.i-1].T))
		},
		"session.isfirstbar_regular": func(m *Machine) float64 {
			return boolF(m.i == 0 || m.dayOf(m.bars[m.i].T) != m.dayOf(m.bars[m.i-1].T))
		},
		"session.islastbar": func(m *Machine) float64 {
			return boolF(m.i < m.n-1 && m.dayOf(m.bars[m.i].T) != m.dayOf(m.bars[m.i+1].T))
		},
		"session.ismarket":     func(m *Machine) float64 { return 1 },
		"timeframe.multiplier": func(m *Machine) float64 { return float64(tfMultiplier(m.env)) },
		"timeframe.isintraday": func(m *Machine) float64 { return boolF(m.env.IntervalMs < 86_400_000) },
		"timeframe.isdaily":    func(m *Machine) float64 { return boolF(m.env.IntervalMs == 86_400_000) },
		"timeframe.isweekly":   func(m *Machine) float64 { return boolF(m.env.IntervalMs == 7*86_400_000) },
		"timeframe.ismonthly":  func(m *Machine) float64 { return boolF(m.env.IntervalMs > 7*86_400_000) },
		"timeframe.isdwm":      func(m *Machine) float64 { return boolF(m.env.IntervalMs >= 86_400_000) },
		"timeframe.isminutes":  func(m *Machine) float64 { return boolF(m.env.IntervalMs < 86_400_000 && m.env.IntervalMs >= 60_000) },
		"timeframe.isseconds":  func(m *Machine) float64 { return boolF(m.env.IntervalMs < 60_000) },
		"timeframe.isticks":    func(m *Machine) float64 { return 0 },
		"ta.tr":                func(m *Machine) float64 { return trueRange(m, false) },
	}
	for k, f := range series {
		builtinVars[k] = func(*compiler) cx { return numX(f) }
	}
	for _, part := range []string{"year", "month", "dayofmonth", "dayofweek", "hour", "minute", "second", "weekofyear"} {
		builtinVars[part] = func(*compiler) cx {
			return numX(func(m *Machine) float64 { return timePart(time.UnixMilli(m.bars[m.i].T).In(m.env.Loc), part) })
		}
	}
	strs := map[string]func(m *Machine) string{
		"syminfo.ticker": func(m *Machine) string { return m.env.Ticker }, "syminfo.tickerid": func(m *Machine) string { return m.env.Ticker },
		"syminfo.description": func(m *Machine) string { return m.env.Ticker }, "syminfo.timezone": func(m *Machine) string { return m.env.Loc.String() },
		"syminfo.currency": func(m *Machine) string { return "" }, "syminfo.basecurrency": func(m *Machine) string { return "" },
		"syminfo.type": func(m *Machine) string { return "" }, "syminfo.session": func(m *Machine) string { return "" },
		"timeframe.period": func(m *Machine) string { return m.env.Period }, "timeframe.main_period": func(m *Machine) string { return m.env.Period },
	}
	for k, f := range strs {
		builtinVars[k] = func(*compiler) cx { return cx{k: kStr, s: f} }
	}
	builtinVars["ta.obv"] = func(c *compiler) cx {
		rec, b := c.record(barX("close"))
		return c.cum(numX(func(m *Machine) float64 {
			ch := rec(m) - past(m, b, 1)
			s := 0.0
			if ch > 0 {
				s = 1
			} else if ch < 0 {
				s = -1
			} else if math.IsNaN(ch) {
				return nan
			}
			return s * m.bars[m.i].V
		}))
	}
	builtinVars["ta.accdist"] = func(c *compiler) cx {
		return c.cum(numX(func(m *Machine) float64 {
			b := m.bars[m.i]
			if b.C == b.H && b.C == b.L || b.H == b.L {
				return 0
			}
			return (2*b.C - b.L - b.H) / (b.H - b.L) * b.V
		}))
	}
	builtinVars["ta.vwap"] = func(c *compiler) cx { return c.vwap(barX("hlc3"), cx{k: kMissing}, cx{k: kMissing}) }
	builtinVars["ta.nvi"] = func(c *compiler) cx { return c.volumeIndex(-1) }
	builtinVars["ta.pvi"] = func(c *compiler) cx { return c.volumeIndex(1) }
	for _, ns := range []string{"position", "xloc", "yloc", "extend", "text", "font", "label", "line", "box", "table", "shape", "order", "alert", "barmerge", "currency", "scale", "adjustment", "backadjustment", "settlement_as_close", "splits", "dividends", "earnings", "session", "strategy"} {
		skipped[ns] = true
	}
}

// skipped are the namespaces whose constants only what Candle skips takes.
var skipped = map[string]bool{}

func (c *compiler) volumeIndex(sign float64) cx {
	o := c.newBuf(false)
	return numX(func(m *Machine) float64 {
		v := 1.0
		if m.i > 0 {
			v = m.buf[o][m.i-1]
			b, p := m.bars[m.i], m.bars[m.i-1]
			if (b.V-p.V)*sign > 0 && p.C != 0 {
				v += (b.C - p.C) / p.C * v
			}
		}
		m.buf[o][m.i] = v
		return v
	})
}

func tfMultiplier(e Env) int64 {
	switch ms := e.IntervalMs; {
	case ms <= 0:
		return 1
	case ms < 86_400_000:
		return max(ms/60_000, 1)
	case ms < 7*86_400_000:
		return ms / 86_400_000
	case ms < 28*86_400_000:
		return ms / (7 * 86_400_000)
	}
	return 1
}

// The functions written in Pine, as TradingView's reference has them.
const prelude = `
ta.bb(series, length, mult) =>
    basis = ta.sma(series, length)
    dev = mult * ta.stdev(series, length)
    [basis, basis + dev, basis - dev]
ta.bbw(series, length, mult) =>
    basis = ta.sma(series, length)
    dev = mult * ta.stdev(series, length)
    ((basis + dev) - (basis - dev)) / basis
ta.kc(series, length, mult, useTrueRange = true) =>
    basis = ta.ema(series, length)
    span = useTrueRange ? ta.tr(true) : high - low
    rangeEma = ta.ema(span, length)
    [basis, basis + rangeEma * mult, basis - rangeEma * mult]
ta.kcw(series, length, mult, useTrueRange = true) =>
    basis = ta.ema(series, length)
    span = useTrueRange ? ta.tr(true) : high - low
    rangeEma = ta.ema(span, length)
    (rangeEma * mult * 2) / basis
ta.mfi(series, length) =>
    ch = ta.change(series)
    upper = math.sum(volume * (ch <= 0.0 ? 0.0 : series), length)
    lower = math.sum(volume * (ch >= 0.0 ? 0.0 : series), length)
    100.0 - (100.0 / (1.0 + upper / lower))
ta.wpr(length) =>
    hi = ta.highest(length)
    lo = ta.lowest(length)
    100 * (close - hi) / (hi - lo)
ta.cmo(series, length) =>
    mom = ta.change(series)
    sm1 = math.sum(mom >= 0 ? mom : 0.0, length)
    sm2 = math.sum(mom >= 0 ? 0.0 : -mom, length)
    100 * (sm1 - sm2) / (sm1 + sm2)
ta.tsi(source, short_length, long_length) =>
    pc = ta.change(source)
    ds = ta.ema(ta.ema(pc, long_length), short_length)
    dsa = ta.ema(ta.ema(math.abs(pc), long_length), short_length)
    ds / dsa
ta.dmi(diLength, adxSmoothing) =>
    up = ta.change(high)
    down = -ta.change(low)
    plusDM = na(up) ? na : (up > down and up > 0 ? up : 0)
    minusDM = na(down) ? na : (down > up and down > 0 ? down : 0)
    trur = ta.rma(ta.tr, diLength)
    plus = fixnan(100 * ta.rma(plusDM, diLength) / trur)
    minus = fixnan(100 * ta.rma(minusDM, diLength) / trur)
    sum = plus + minus
    adx = 100 * ta.rma(math.abs(plus - minus) / (sum == 0 ? 1 : sum), adxSmoothing)
    [plus, minus, adx]
ta.supertrend(factor, atrPeriod) =>
    src = hl2
    atr = ta.atr(atrPeriod)
    upperBand = src + factor * atr
    lowerBand = src - factor * atr
    prevLowerBand = nz(lowerBand[1])
    prevUpperBand = nz(upperBand[1])
    lowerBand := lowerBand > prevLowerBand or close[1] < prevLowerBand ? lowerBand : prevLowerBand
    upperBand := upperBand < prevUpperBand or close[1] > prevUpperBand ? upperBand : prevUpperBand
    int direction = na
    float superTrend = na
    prevSuperTrend = superTrend[1]
    if na(atr[1])
        direction := 1
    else if prevSuperTrend == prevUpperBand
        direction := close > upperBand ? -1 : 1
    else
        direction := close < lowerBand ? 1 : -1
    superTrend := direction == -1 ? lowerBand : upperBand
    [superTrend, direction]
ta.sar(start, inc, maximum) =>
    var float result = na
    var float maxMin = na
    var float acceleration = na
    var bool isBelow = false
    bool isFirstTrendBar = false
    if bar_index == 1
        if close > close[1]
            isBelow := true
            maxMin := high
            result := low[1]
        else
            isBelow := false
            maxMin := low
            result := high[1]
        isFirstTrendBar := true
        acceleration := start
    result := result + acceleration * (maxMin - result)
    if isBelow
        if result > low
            isFirstTrendBar := true
            isBelow := false
            result := math.max(high, maxMin)
            maxMin := low
            acceleration := start
    else
        if result < high
            isFirstTrendBar := true
            isBelow := true
            result := math.min(low, maxMin)
            maxMin := high
            acceleration := start
    if not isFirstTrendBar
        if isBelow
            if high > maxMin
                maxMin := high
                acceleration := math.min(acceleration + inc, maximum)
        else
            if low < maxMin
                maxMin := low
                acceleration := math.min(acceleration + inc, maximum)
    if isBelow
        result := math.min(result, low[1])
        if bar_index > 1
            result := math.min(result, low[2])
    else
        result := math.max(result, high[1])
        if bar_index > 1
            result := math.max(result, high[2])
    result
`

var preludeOnce = sync.OnceValue(func() map[string]*funcS {
	body, err := parse(prelude)
	if err != nil {
		panic("pine prelude: " + err.Error())
	}
	out := map[string]*funcS{}
	for _, st := range body {
		f := st.(*funcS)
		out[f.name] = f
	}
	return out
})

func preludeFuncs() map[string]*funcS { return preludeOnce() }

// Names are the functions and variables scripts can use, for completions:
// a function's name ends with "(".
func Names() []string {
	var out []string
	for n := range builtinFuncs {
		out = append(out, n+"(")
	}
	for n := range preludeFuncs() {
		out = append(out, n+"(")
	}
	for n := range builtinVars {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
