package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// The panes: the candles' on top, below it one for each indicator drawn in
// a pane of its own, and the time axis under them all. Every pane has its
// price axis on the right; the lines between panes move.

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

// paneInput drags the lines between panes. It reports whether it took the event.
func (a *application) paneInput(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if i := a.sepAt(ev.Y); i >= 0 {
			_, panes := a.layout(a.boxH - timeH)
			a.drag, a.sep, a.dragY = dragSep, i, ev.Y-panes[i].y // the line follows the pointer from where it took it
			return true
		}
	case ui.InputPointerMove:
		if a.drag != dragSep {
			return false
		}
		if ev.Button != 0 { // let go somewhere we did not hear of
			a.drag = dragNone
			a.save()
			return true
		}
		a.moveSep(a.sep, ev.Y-a.dragY)
		return true
	case ui.InputPointerUp:
		if a.drag != dragSep {
			return false
		}
		a.drag = dragNone
		a.save()
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
	s.update(k, ind.Params, a.bars, a.gen)
	return s
}

// color is plot j's; bars, which take the candles' colors, have none.
func (ind *indicator) color(k *indKind, j int) ui.Color {
	var def ui.Color
	if c := k.plots[j].color; c != "" {
		def = ui.Hex(c)
	}
	if j < len(ind.Colors) {
		return ind.Colors[j].or(def)
	}
	return def
}

func (ind *indicator) width(j int) float32 {
	if j < len(ind.Widths) && ind.Widths[j] >= 1 {
		return float32(ind.Widths[j])
	}
	return 1
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

// overlayRange widens lo and hi to the values of the overlays from bar
// from to bar to, but those a log scale cannot show.
func (a *application) overlayRange(from, to int, lo, hi float64, log bool) (float64, float64) {
	a.overlays(func(ind *indicator, k *indKind, s *series) {
		for j := range k.plots {
			for _, v := range s.out[j][from : to+1] {
				if !math.IsNaN(v) && (!log || v > 0) {
					lo, hi = min(lo, v), max(hi, v)
				}
			}
		}
	})
	return lo, hi
}

// strokeSeries draws vals from bar from to bar to as a line, one bar past
// each end so that it runs off the plot; a NaN breaks it.
func strokeSeries(p *ui.Painter, x func(int) float32, y func(float64) float32, vals []float64, from, to int, w float32, c ui.Color) {
	var path ui.Path
	drawn, open := false, false
	for i := max(from-1, 0); i <= min(to+1, len(vals)-1); i++ {
		v := vals[i]
		switch {
		case math.IsNaN(v):
			open = false
		case open:
			path.LineTo(x(i), y(v))
			drawn = true
		default:
			path.MoveTo(x(i), y(v))
			open = true
		}
	}
	if drawn {
		p.StrokePath(&path, w, c)
	}
}

// fillBetween shades the space between two series from bar from to bar to
// where both have values.
func fillBetween(p *ui.Painter, x func(int) float32, y func(float64) float32, top, bottom []float64, from, to int, c ui.Color) {
	i0, i1 := max(from-1, 0), min(to+1, len(top)-1)
	for i0 <= i1 && (math.IsNaN(top[i0]) || math.IsNaN(bottom[i0])) {
		i0++
	}
	if i0 >= i1 {
		return
	}
	var path ui.Path
	path.MoveTo(x(i0), y(top[i0]))
	for i := i0 + 1; i <= i1; i++ {
		path.LineTo(x(i), y(top[i]))
	}
	for i := i1; i >= i0; i-- {
		path.LineTo(x(i), y(bottom[i]))
	}
	p.FillPath(path.Close(), c)
}

// drawOverlays draws the indicators over the candles.
func (a *application) drawOverlays(p *ui.Painter, x func(int) float32, y func(float64) float32, from, to int) {
	a.overlays(func(ind *indicator, k *indKind, s *series) {
		if len(k.fill) == 2 {
			fillBetween(p, x, y, s.out[k.fill[0]], s.out[k.fill[1]], from, to, ind.color(k, k.fill[0]).Alpha(0.07))
		}
		for j := range k.plots {
			strokeSeries(p, x, y, s.out[j], from, to, ind.width(j), ind.color(k, j))
		}
	})
}

// paneScale is how a pane laid its values out, for the crosshair's label.
type paneScale struct {
	k  *indKind
	sc prices
	ax axis
}

// drawPane draws ind in pane r: its plots and levels, and on the price axis
// beside it a scale fitted to the values in sight and the levels.
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
	sc := prices{lo: lo, hi: hi}
	y := func(v float64) float32 { return r.Y + sc.y(v, ax) }

	size, textCol := float32(st.TextSize), st.TextColor.or(k.muted)
	count := max(int(r.H/36), 2)
	vTop, vBottom := sc.at(0, ax), sc.at(r.H, ax)
	step := niceStep(vTop-vBottom, count)
	for _, v := range priceTicks(vBottom, vTop, count, false) {
		yy := mid(y(v))
		if yy < r.Y+8 || yy > r.Y+r.H-8 {
			continue
		}
		if st.HorzGrid {
			p.Line(r.X, yy, r.X+r.W, yy, px, st.HorzColor.or(k.grid))
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
		for j, pl := range kind.plots {
			vals := s.out[j]
			switch pl.style {
			case plotLine:
				strokeSeries(p, x, y, vals, from, to, ind.width(j), ind.color(kind, j))
			case plotVolume:
				for i := from; i <= to; i++ {
					top := y(vals[i])
					xc := mid(x(i))
					p.Fill(ui.Rect{X: xc - bw/2, Y: top, W: bw, H: max(r.Y+r.H-top, px)}, a.barColor(i, k).Alpha(0.5), 0)
				}
			case plotHist:
				zero := y(0)
				for i := from; i <= to; i++ {
					v := vals[i]
					if math.IsNaN(v) {
						continue
					}
					c := histColor(v, vals, i, k)
					top, h := min(zero, y(v)), max(float32(math.Abs(float64(y(v)-zero))), px)
					xc := mid(x(i))
					p.Fill(ui.Rect{X: xc - bw/2, Y: top, W: bw, H: h}, c, 0)
				}
			}
		}
	})
	return paneScale{kind, sc, ax}
}

// histColor is a histogram bar's: the up color above zero and the down
// color below it, paler while it shrinks, as TradingView's MACD has it.
func histColor(v float64, vals []float64, i int, k palette) ui.Color {
	c := k.up
	if v < 0 {
		c = k.down
	}
	if i > 0 && !math.IsNaN(vals[i-1]) && math.Abs(v) < math.Abs(vals[i-1]) {
		return c.Alpha(0.45)
	}
	return c
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
