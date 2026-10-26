package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// The panes: the candles' on top, below it one for each indicator drawn in
// a pane of its own, and the time axis under them all. Every pane has its
// price axis on the right, which scales its prices as the candles' does
// theirs; the lines between panes move.

const (
	minMainH = 120 // what the candles' pane keeps at least
	minPaneH = 40
	sepGrip  = 4 // DIPs either side of a line between panes that take the pointer
)

// paneBox is an indicator's pane as a frame lays it out: the indicator's
// place in the list, and the pane's top and height in the chart.
type paneBox struct {
	idx  int
	y, h float32
}

// paneView is an indicator's pane's prices: as set, automatic unless scaled
// by hand, and as the last frame showed and laid them out.
type paneView struct {
	scale, shown prices
	ax           axis
}

// layout splits h, the chart's height above the time axis, into the
// candles' pane and the indicators' panes below it, each one its share of
// h; shares adding up to more than leaves the candles minMainH (or half of
// a short window) shrink alike.
func (a *application) layout(h float32) (mainH float32, panes []paneBox) {
	var want float32
	for i := range a.set.Indicators {
		if k, ok := kindOf(a.set.Indicators[i].Kind); ok && k.pane {
			want += float32(a.set.Indicators[i].Height) * h
			panes = append(panes, paneBox{idx: i})
		}
	}
	if len(panes) == 0 {
		return h, nil
	}
	f := float32(1)
	if room := max(h-minMainH, h/2); want > room {
		f = room / want
	}
	y := h
	for i := len(panes) - 1; i >= 0; i-- {
		ph := float32(a.set.Indicators[panes[i].idx].Height) * h * f
		y -= ph
		panes[i].y, panes[i].h = y, ph
	}
	return y, panes
}

// paneAt is the indicator's pane at y in the chart.
func (a *application) paneAt(y float32) (paneBox, bool) {
	_, panes := a.layout(a.boxH - timeH)
	for _, pn := range panes {
		if y >= pn.y && y < pn.y+pn.h {
			return pn, true
		}
	}
	return paneBox{}, false
}

func (a *application) paneView(id int64) *paneView {
	pv := a.paneViews[id]
	if pv == nil {
		pv = &paneView{}
		a.paneViews[id] = pv
	}
	return pv
}

// sepAt is which line between panes is at y in the chart, by the pane under
// it; -1 for none.
func (a *application) sepAt(y float32) int {
	_, panes := a.layout(a.boxH - timeH)
	for i, pn := range panes {
		if math.Abs(float64(y-pn.y)) <= sepGrip {
			return i
		}
	}
	return -1
}

// moveSep moves the top of pane i to y, the pane above it giving or taking
// the room.
func (a *application) moveSep(i int, y float32) {
	h := a.boxH - timeH
	_, panes := a.layout(h)
	if i < 0 || i >= len(panes) || h <= 0 {
		return
	}
	for _, pn := range panes { // as laid out, should they have shrunk alike
		a.set.Indicators[pn.idx].Height = float64(pn.h / h)
	}
	top, least := float32(0), float32(minMainH)
	if i > 0 {
		top, least = panes[i-1].y, minPaneH
	}
	bottom := panes[i].y + panes[i].h
	y = min(max(y, top+least), bottom-minPaneH)
	if i > 0 {
		a.set.Indicators[panes[i-1].idx].Height = float64((y - top) / h)
	}
	a.set.Indicators[panes[i].idx].Height = float64((bottom - y) / h)
}

// paneInput drags the lines between panes, and scales a pane's prices on
// its price axis as the candles' axis does theirs: the wheel, and a drag up
// or down, until a double click there. It reports whether it took the event.
func (a *application) paneInput(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown, ui.InputScroll:
		if ev.Kind == ui.InputPointerDown && ev.Button != 0 {
			return false
		}
		if i := a.sepAt(ev.Y); i >= 0 && ev.Kind == ui.InputPointerDown {
			_, panes := a.layout(a.boxH - timeH)
			a.drag, a.sep, a.dragY = dragSep, i, ev.Y-panes[i].y // the line follows the pointer from where it took it
			return true
		}
		pn, ok := a.paneAt(ev.Y)
		if !ok || ev.X <= a.plotW {
			return false
		}
		id := a.set.Indicators[pn.idx].ID
		pv := a.paneView(id)
		switch {
		case ev.Kind == ui.InputScroll:
			if holdPrices(&pv.scale, pv.shown) {
				pv.scale.zoom(math.Pow(zoomPerNotch, float64(ev.DY/notch)), pv.scale.at(ev.Y-pn.y, pv.ax), pv.ax)
			}
		case ev.Clicks >= 2:
			pv.scale = prices{}
		case holdPrices(&pv.scale, pv.shown):
			a.drag, a.dragY, a.dragPane = dragPaneAxis, ev.Y, id
		}
		return true
	case ui.InputPointerMove:
		if a.drag != dragSep && a.drag != dragPaneAxis {
			return false
		}
		if ev.Button != 0 { // let go somewhere we did not hear of
			a.drag = dragNone
			a.save()
			return true
		}
		if a.drag == dragSep {
			a.moveSep(a.sep, ev.Y-a.dragY)
			return true
		}
		pv := a.paneView(a.dragPane)
		dy := ev.Y - a.dragY
		a.dragY = ev.Y
		m := pv.ax
		pv.scale.zoom(math.Exp(float64(dy)/axisDrag), pv.scale.at(m.h*(m.top+m.bottom)/2, m), m)
		return true
	case ui.InputPointerUp:
		if a.drag != dragSep && a.drag != dragPaneAxis {
			return false
		}
		if a.drag == dragSep {
			a.save()
		}
		a.drag = dragNone
		return true
	}
	return false
}

// seriesFor is ind's values on the bars, brought up to date.
func (a *application) seriesFor(ind *indicator) *series {
	k, _ := kindOf(ind.Kind)
	s := a.series[ind.ID]
	if s == nil {
		s = &series{}
		a.series[ind.ID] = s
	}
	s.update(k, ind.Params, ind.Source, a.bars, a.gen)
	return s
}

// color is plot j's; a bar plot's above zero, or rising.
func (ind *indicator) color(k *indKind, j int) ui.Color {
	return pick(ind.Colors, j, k.plots[j].color)
}

// downColor is a bar plot's second color: below zero, or falling.
func (ind *indicator) downColor(k *indKind, j int) ui.Color {
	return pick(ind.Downs, j, k.plots[j].down)
}

// pick is cs[j], or def while unset.
func pick(cs []hexColor, j int, def string) ui.Color {
	var d ui.Color
	if def != "" {
		d = ui.Hex(def)
	}
	if j < len(cs) {
		return cs[j].or(d)
	}
	return d
}

func (ind *indicator) width(j int) float32 {
	if j < len(ind.Widths) && ind.Widths[j] >= 1 {
		return float32(ind.Widths[j])
	}
	return 1
}

// plotBarColor is the color of plot j's bar i: a histogram's by its side of
// zero, paler while it shrinks, as TradingView's MACD has it; volume's by
// whether the candle rose.
func (a *application) plotBarColor(ind *indicator, k *indKind, j int, vals []float64, i int) ui.Color {
	if k.plots[j].style == plotVolume {
		if a.rising(i) {
			return ind.color(k, j)
		}
		return ind.downColor(k, j)
	}
	v := vals[i]
	c := ind.color(k, j)
	if v < 0 {
		c = ind.downColor(k, j)
	}
	if i > 0 && !math.IsNaN(vals[i-1]) && math.Abs(v) < math.Abs(vals[i-1]) {
		return c.Alpha(0.45)
	}
	return c
}

// valueText is an indicator's value as the legend and the axis read it.
func (a *application) valueText(k *indKind, v float64) string {
	switch {
	case math.IsNaN(v):
		return "—"
	case k.decimals == -2:
		return formatVolume(v)
	case k.decimals == -1:
		return formatPrice(v, a.chartDecimals(v))
	}
	return formatPrice(v, k.decimals)
}

// overlays calls fn with every indicator drawn over the candles and shown.
func (a *application) overlays(fn func(ind *indicator, k *indKind, s *series)) {
	for i := range a.set.Indicators {
		ind := &a.set.Indicators[i]
		if k, ok := kindOf(ind.Kind); ok && !k.pane && !ind.Hidden {
			fn(ind, k, a.seriesFor(ind))
		}
	}
}

// overlayRange widens lo and hi to the values of the overlays drawn on bar
// from to bar to, but those a log scale cannot show.
func (a *application) overlayRange(from, to int, lo, hi float64, log bool) (float64, float64) {
	a.overlays(func(ind *indicator, k *indKind, s *series) {
		off := ind.shift()
		for j := range k.plots {
			vals := s.out[j]
			for i := max(from-off, 0); i <= min(to-off, len(vals)-1); i++ {
				if v := vals[i]; !math.IsNaN(v) && (!log || v > 0) {
					lo, hi = min(lo, v), max(hi, v)
				}
			}
		}
	})
	return lo, hi
}

// strokeSeries draws vals as a line where they land on bars from to to, each
// off bars to the right of its own, one bar past each end so that it runs
// off the plot; a NaN breaks it.
func strokeSeries(p *ui.Painter, x func(int) float32, y func(float64) float32, vals []float64, from, to, off int, w float32, c ui.Color) {
	var path ui.Path
	drawn, open := false, false
	for i := max(from-1-off, 0); i <= min(to+1-off, len(vals)-1); i++ {
		v := vals[i]
		switch {
		case math.IsNaN(v):
			open = false
		case open:
			path.LineTo(x(i+off), y(v))
			drawn = true
		default:
			path.MoveTo(x(i+off), y(v))
			open = true
		}
	}
	if drawn {
		p.StrokePath(&path, w, c)
	}
}

// fillBetween shades the space between two series where they land on bars
// from to to, off bars to the right, and both have values.
func fillBetween(p *ui.Painter, x func(int) float32, y func(float64) float32, top, bottom []float64, from, to, off int, c ui.Color) {
	i0, i1 := max(from-1-off, 0), min(to+1-off, len(top)-1)
	for i0 <= i1 && (math.IsNaN(top[i0]) || math.IsNaN(bottom[i0])) {
		i0++
	}
	if i0 >= i1 {
		return
	}
	var path ui.Path
	path.MoveTo(x(i0+off), y(top[i0]))
	for i := i0 + 1; i <= i1; i++ {
		path.LineTo(x(i+off), y(top[i]))
	}
	for i := i1; i >= i0; i-- {
		path.LineTo(x(i+off), y(bottom[i]))
	}
	p.FillPath(path.Close(), c)
}

// drawOverlays draws the indicators over the candles.
func (a *application) drawOverlays(p *ui.Painter, x func(int) float32, y func(float64) float32, from, to int) {
	a.overlays(func(ind *indicator, k *indKind, s *series) {
		off := ind.shift()
		if len(k.fill) == 2 {
			fillBetween(p, x, y, s.out[k.fill[0]], s.out[k.fill[1]], from, to, off, ind.color(k, k.fill[0]).Alpha(0.07))
		}
		for j := range k.plots {
			strokeSeries(p, x, y, s.out[j], from, to, off, ind.width(j), ind.color(k, j))
		}
	})
}

// axisTag is a value's label on a price axis, in its line's color.
type axisTag struct {
	y    float32
	text string
	c    ui.Color
}

// overlayTags are the newest values of the overlays, for the candles' axis.
func (a *application) overlayTags(y func(float64) float32, text func(float64) string, top, bottom float32) []axisTag {
	var tags []axisTag
	if !a.set.Chart.IndValues {
		return nil
	}
	a.overlays(func(ind *indicator, k *indKind, s *series) {
		for j := range k.plots {
			if v := s.at(j, len(a.bars)-1); !math.IsNaN(v) {
				if yy := y(v); yy >= top && yy <= bottom {
					tags = append(tags, axisTag{yy, text(v), ind.color(k, j)})
				}
			}
		}
	})
	return tags
}

func drawTags(p *ui.Painter, x float32, tags []axisTag, size float32) {
	for _, t := range tags {
		p.Fill(ui.Rect{X: x, Y: t.y - 10, W: axisW, H: 20}, t.c, 2)
		label(p, x+8, t.y, span(t.text, size, onColor(t.c), 0), alignStart)
	}
}

func tagSpans(tags []axisTag) (out [][2]float32) {
	for _, t := range tags {
		out = append(out, [2]float32{t.y - 10, t.y + 10})
	}
	return out
}

// paneScale is how a pane laid its values out, for the crosshair's label.
type paneScale struct {
	k  *indKind
	sc prices
	ax axis
}

// drawPane draws ind in pane r: its bars, levels and lines, and on the price
// axis beside it a scale fitted to the values in sight and the levels, or
// as scaled by hand, and the newest values.
func (a *application) drawPane(p *ui.Painter, r ui.Rect, ind *indicator, x func(int) float32, from, to int, k palette, bw, px float32, mid func(float32) float32) paneScale {
	st := &a.set.Chart
	kind, _ := kindOf(ind.Kind)
	ax := axis{h: r.H, top: 0.1, bottom: 0.92}
	var s *series
	lo, hi := math.Inf(1), math.Inf(-1)
	if !ind.Hidden {
		s = a.seriesFor(ind)
		for j, pl := range kind.plots {
			for _, v := range s.out[j][from : to+1] {
				if !math.IsNaN(v) {
					lo, hi = min(lo, v), max(hi, v)
				}
			}
			switch pl.style {
			case plotHist:
				lo, hi = min(lo, 0), max(hi, 0)
			case plotVolume: // bars rise from the bottom of the pane
				lo, ax.bottom = 0, 1
			}
		}
		for _, l := range kind.levels {
			lo, hi = min(lo, l), max(hi, l)
		}
	}
	switch {
	case lo > hi: // nothing in sight
		lo, hi = 0, 1
	case lo == hi:
		d := max(math.Abs(hi)*0.01, 1e-8)
		lo, hi = lo-d, hi+d
	}
	pv := a.paneView(ind.ID)
	sc := prices{lo: lo, hi: hi}
	if pv.scale.manual {
		sc = pv.scale
	}
	pv.shown, pv.ax = sc, ax
	y := func(v float64) float32 { return r.Y + sc.y(v, ax) }

	var tags []axisTag
	if s != nil && st.IndValues {
		n := len(a.bars)
		for j, pl := range kind.plots {
			v := s.at(j, n-1)
			if math.IsNaN(v) {
				continue
			}
			c := ind.color(kind, j)
			if pl.style != plotLine {
				c = a.plotBarColor(ind, kind, j, s.out[j], n-1).Alpha(1)
			}
			if yy := mid(y(v)); yy >= r.Y && yy <= r.Y+r.H {
				tags = append(tags, axisTag{min(max(yy, r.Y+10), r.Y+r.H-10), a.valueText(kind, v), c})
			}
		}
	}
	size, textCol := float32(st.TextSize), st.TextColor.or(k.muted)
	count := max(int(r.H/36), 2)
	vTop, vBottom := sc.at(0, ax), sc.at(r.H, ax)
	step := niceStep(vTop-vBottom, count)
	taken := tagSpans(tags)
	for _, v := range priceTicks(vBottom, vTop, count, false) {
		yy := mid(y(v))
		if yy < r.Y+8 || yy > r.Y+r.H-8 {
			continue
		}
		if st.HorzGrid {
			p.Line(r.X, yy, r.X+r.W, yy, px, st.HorzColor.or(k.grid))
		}
		if st.NoOverlap && covered(yy, taken) {
			continue
		}
		text := formatPrice(v, min(stepDecimals(step), 10))
		if kind.decimals == -2 {
			text = formatVolume(v)
		}
		label(p, r.X+r.W+8, yy, span(text, size, textCol, 0), alignStart)
	}
	if s == nil {
		return paneScale{kind, sc, ax}
	}
	p.Clip(r, 0, func() {
		if kind.band && len(kind.levels) == 2 {
			y0, y1 := y(kind.levels[0]), y(kind.levels[1])
			p.Fill(ui.Rect{X: r.X, Y: min(y0, y1), W: r.W, H: float32(math.Abs(float64(y1 - y0)))}, ind.color(kind, 0).Alpha(0.08), 0)
		}
		for _, l := range kind.levels {
			dash(p, r.X, mid(y(l)), r.X+r.W, mid(y(l)), px, k.muted.Alpha(0.6), "")
		}
		// The bars first, behind the lines, which they would cut.
		for j, pl := range kind.plots {
			vals := s.out[j]
			switch pl.style {
			case plotVolume:
				for i := from; i <= to; i++ {
					top := y(vals[i])
					xc := mid(x(i))
					p.Fill(ui.Rect{X: xc - bw/2, Y: top, W: bw, H: max(r.Y+r.H-top, px)}, a.plotBarColor(ind, kind, j, vals, i).Alpha(0.5), 0)
				}
			case plotHist:
				zero := y(0)
				for i := from; i <= to; i++ {
					v := vals[i]
					if math.IsNaN(v) {
						continue
					}
					top, h := min(zero, y(v)), max(float32(math.Abs(float64(y(v)-zero))), px)
					xc := mid(x(i))
					p.Fill(ui.Rect{X: xc - bw/2, Y: top, W: bw, H: h}, a.plotBarColor(ind, kind, j, vals, i), 0)
				}
			}
		}
		for j, pl := range kind.plots {
			if pl.style == plotLine {
				strokeSeries(p, x, y, s.out[j], from, to, 0, ind.width(j), ind.color(kind, j))
			}
		}
	})
	drawTags(p, r.X+r.W, tags, size+0.5)
	return paneScale{kind, sc, ax}
}

// hoverBar is the bar under the pointer at x, y in the chart, in any pane;
// else the newest one.
func (a *application) hoverBar(x, y float32, over bool) int {
	n := len(a.bars)
	if !over || n == 0 || x < 0 || x >= a.plotW || y < 0 || y >= a.boxH-timeH {
		return n - 1
	}
	v := a.view
	v.clamp(a.plotW, n)
	return min(max(int(math.Round(float64(v.index(x, a.plotW, n)))), 0), n-1)
}
