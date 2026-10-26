package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// point is where a drawing is anchored: a bar's open time and a price. Times
// keep a drawing on the same moment whichever bar size shows it.
type point struct {
	T int64
	P float64
}

// drawing is a line or shape drawn on a market's chart.
type drawing struct {
	ID     int64
	Kind   string // a drawTool's kind
	Pts    []point
	Color  hexColor // unset, the accent
	Width  float64
	Text   string  // a text's
	Size   float64 // a text's, 0 for 14
	Locked bool    // kept from being moved
}

func textSize(d *drawing) float64 {
	if d.Size > 0 {
		return d.Size
	}
	return 14
}

// drawTool is a tool on the left of the chart: what it draws and how many
// points that takes.
type drawTool struct {
	kind, label string
	points      int
	icon        *ui.SVG
}

var drawTools = []drawTool{
	{"trend", "趋势线", 2, svg("<path d='M4.4 11.6 11.6 4.4'/><circle cx='3.4' cy='12.6' r='1.4'/><circle cx='12.6' cy='3.4' r='1.4'/>")},
	{"ray", "射线", 2, svg("<circle cx='3.4' cy='12.6' r='1.4'/><path d='M4.4 11.6 14 2M10 2h4v4'/>")},
	{"hline", "水平线", 1, svg("<path d='M1.5 8h4.6M9.9 8h4.6'/><circle cx='8' cy='8' r='1.6'/>")},
	{"hray", "水平射线", 1, svg("<circle cx='3.5' cy='8' r='1.6'/><path d='M5.3 8h9.2'/>")},
	{"vline", "垂直线", 1, svg("<path d='M8 1.5v4.6M8 9.9v4.6'/><circle cx='8' cy='8' r='1.6'/>")},
	{"channel", "平行通道", 3, svg("<path d='M2 9.5 9.5 2M6.5 14 14 6.5'/><path d='M4.5 11.5l7-7' stroke-dasharray='1.4 1.8'/>")},
	{"rect", "矩形", 2, svg("<rect x='2.5' y='4' width='11' height='8' rx='1'/>")},
	{"fib", "斐波那契回撤", 2, svg("<path d='M2 3h12M2 6h12M2 8.5h12M2 13h12'/>")},
	{"range", "价格区间", 2, svg("<path d='M8 3.5v9M5.8 5.5 8 3.2l2.2 2.3M5.8 10.5 8 12.8l2.2-2.3M3 2.5h10M3 13.5h10'/>")},
	{"text", "文字", 1, svg("<path d='M3 3.5h10M8 3.5v10M6 13.5h4'/>")},
}

func toolOf(kind string) (drawTool, bool) {
	for _, t := range drawTools {
		if t.kind == kind {
			return t, true
		}
	}
	return drawTool{}, false
}

// The retracement levels, from the second point (0) back to the first (1).
var fibLevels = []float64{0, 0.236, 0.382, 0.5, 0.618, 0.786, 1}

// mapper turns anchors into places on a plot and back, as a frame lays the
// chart out. x and y are relative to the plot's top left.
type mapper struct {
	v    view
	w, h float32
	bars []market.Bar
	iv   int64 // the bar size, ms
	sc   prices
	ax   axis
}

// idx is the bar at time t, fractional: between bars it is where t falls
// between their opening times, and past either end it counts bar sizes.
func (m mapper) idx(t int64) float64 {
	n := len(m.bars)
	i := sort.Search(n, func(i int) bool { return m.bars[i].T >= t })
	switch {
	case i < n && m.bars[i].T == t:
		return float64(i)
	case i == n:
		return float64(n-1) + float64(t-m.bars[n-1].T)/float64(m.iv)
	case i == 0:
		return float64(t-m.bars[0].T) / float64(m.iv)
	}
	a, b := m.bars[i-1].T, m.bars[i].T
	return float64(i-1) + float64(t-a)/float64(b-a)
}

// timeAt is the inverse of idx.
func (m mapper) timeAt(f float64) int64 {
	n := len(m.bars)
	switch {
	case f >= float64(n-1):
		return m.bars[n-1].T + int64(math.Round((f-float64(n-1))*float64(m.iv)))
	case f <= 0:
		return m.bars[0].T + int64(math.Round(f*float64(m.iv)))
	}
	i := int(math.Floor(f))
	a, b := m.bars[i].T, m.bars[i+1].T
	return a + int64(math.Round((f-float64(i))*float64(b-a)))
}

func (m mapper) x(t int64) float32   { return m.v.xf(float32(m.idx(t)), m.w, len(m.bars)) }
func (m mapper) y(p float64) float32 { return m.sc.y(p, m.ax) }
func (m mapper) xy(pt point) (float32, float32) {
	return m.x(pt.T), m.y(pt.P)
}

// magnetRange is how near, in DIPs, a price must be to an open, high, low or
// close for the magnet to take it there.
const magnetRange = 20

// snap is the anchor at x, y: on the nearest bar, and with the magnet on, on
// that bar's open, high, low or close when one is near.
func (m mapper) snap(x, y float32, magnet bool) point {
	n := len(m.bars)
	i := int(math.Round(float64(m.v.index(x, m.w, n))))
	pt := point{T: m.timeAt(float64(i)), P: m.sc.at(y, m.ax)}
	if magnet && i >= 0 && i < n {
		b, best := m.bars[i], float32(magnetRange)
		for _, p := range []float64{b.O, b.H, b.L, b.C} {
			if d := float32(math.Abs(float64(m.y(p) - y))); d < best {
				pt.P, best = p, d
			}
		}
	}
	return pt
}

// move is d's anchors, as at the start of a drag, moved by whole bars and dy DIPs.
func (m mapper) move(orig []point, bars int, dy float32) []point {
	out := make([]point, len(orig))
	for i, pt := range orig {
		out[i] = point{T: m.timeAt(m.idx(pt.T) + float64(bars)), P: m.sc.at(m.y(pt.P)+dy, m.ax)}
	}
	return out
}

// distSeg is how far x, y is from the segment x0, y0 – x1, y1.
func distSeg(x, y, x0, y0, x1, y1 float32) float32 {
	dx, dy := x1-x0, y1-y0
	t := float32(0)
	if l := dx*dx + dy*dy; l > 0 {
		t = min(max(((x-x0)*dx+(y-y0)*dy)/l, 0), 1)
	}
	return float32(math.Hypot(float64(x-x0-t*dx), float64(y-y0-t*dy)))
}

// far is where the line from x0, y0 through x1, y1 leaves any plot: 4000
// DIPs on. Much farther and the rasterizer drops the line.
func far(x0, y0, x1, y1 float32) (float32, float32) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return x1, y1
	}
	k := 4000 / l
	return x0 + dx*k, y0 + dy*k
}

// channelOffset is how far down the second line of a channel runs from its first.
func channelOffset(m mapper, d *drawing) float32 {
	x0, y0 := m.xy(d.Pts[0])
	x1, y1 := m.xy(d.Pts[1])
	x2, y2 := m.xy(d.Pts[2])
	if x1 == x0 {
		return y2 - y0
	}
	return y2 - (y0 + (y1-y0)*(x2-x0)/(x1-x0))
}

// hitTolerance is how near, in DIPs, the pointer must be to a line to take it.
const hitTolerance = 6

// hits reports whether x, y is on d.
func (a *application) hits(m mapper, d *drawing, x, y float32) bool {
	x0, y0 := m.xy(d.Pts[0])
	near := func(x0, y0, x1, y1 float32) bool { return distSeg(x, y, x0, y0, x1, y1) <= hitTolerance }
	inside := func(x0, y0, x1, y1 float32) bool {
		return x >= min(x0, x1)-hitTolerance && x <= max(x0, x1)+hitTolerance && y >= min(y0, y1)-hitTolerance && y <= max(y0, y1)+hitTolerance
	}
	switch d.Kind {
	case "hline":
		return math.Abs(float64(y-y0)) <= hitTolerance
	case "hray":
		return x >= x0-hitTolerance && math.Abs(float64(y-y0)) <= hitTolerance
	case "vline":
		return math.Abs(float64(x-x0)) <= hitTolerance
	case "text":
		r, ok := a.textBox[d.ID]
		return ok && x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H
	}
	x1, y1 := m.xy(d.Pts[1])
	switch d.Kind {
	case "trend":
		return near(x0, y0, x1, y1)
	case "ray":
		fx, fy := far(x0, y0, x1, y1)
		return near(x0, y0, fx, fy)
	case "channel":
		off := channelOffset(m, d)
		return near(x0, y0, x1, y1) || near(x0, y0+off, x1, y1+off)
	case "rect", "range":
		return inside(x0, y0, x1, y1)
	case "fib":
		if x < min(x0, x1)-hitTolerance || x > max(x0, x1)+hitTolerance {
			return false
		}
		for _, l := range fibLevels {
			ly := m.y(d.Pts[1].P + (d.Pts[0].P-d.Pts[1].P)*l)
			if math.Abs(float64(y-ly)) <= hitTolerance {
				return true
			}
		}
	}
	return false
}

// handleRadius is the radius, in DIPs, of the circles that move a selected
// drawing's points.
const handleRadius = 5

// hitDrawing is the drawing at x, y, the topmost, and which of its points is
// there when it is the selected one (-1 for none of them).
func (a *application) hitDrawing(m mapper, x, y float32) (*drawing, int) {
	if a.hide {
		return nil, -1
	}
	ds := a.set.Drawings[a.symbol]
	if d := a.selected(); d != nil && !d.Locked {
		for i, pt := range d.Pts {
			px, py := m.xy(pt)
			if math.Hypot(float64(x-px), float64(y-py)) <= handleRadius+3 {
				return d, i
			}
		}
	}
	for i := len(ds) - 1; i >= 0; i-- {
		if a.hits(m, &ds[i], x, y) {
			return &ds[i], -1
		}
	}
	return nil, -1
}

// drawDrawings paints the market's drawings, and the one being drawn, on
// the plot; the caller clips to it.
func (a *application) drawDrawings(p *ui.Painter, plot ui.Rect, m mapper, k palette) {
	clear(a.textBox)
	if a.hide {
		return
	}
	ds := a.set.Drawings[a.symbol]
	for i := range ds {
		a.drawOne(p, plot, m, k, &ds[i])
	}
	if a.creating != nil {
		a.drawOne(p, plot, m, k, a.creating)
	}
	sel := a.selected()
	if a.creating != nil {
		sel = a.creating
	}
	if sel == nil || sel.Locked { // a locked drawing shows no points to drag
		return
	}
	for _, pt := range sel.Pts {
		x, y := m.xy(pt)
		c := ui.Rect{X: plot.X + x - handleRadius, Y: plot.Y + y - handleRadius, W: 2 * handleRadius, H: 2 * handleRadius}
		p.Fill(c, k.bg, handleRadius)
		p.Stroke(c, sel.Color.or(k.accent), handleRadius, 1.5)
	}
}

func (a *application) drawOne(p *ui.Painter, plot ui.Rect, m mapper, k palette, d *drawing) {
	col, w := d.Color.or(k.accent), float32(max(d.Width, 1))
	at := func(pt point) (float32, float32) {
		x, y := m.xy(pt)
		return plot.X + x, plot.Y + y
	}
	line := func(x0, y0, x1, y1, w float32, c ui.Color) {
		var path ui.Path
		path.MoveTo(x0, y0).LineTo(x1, y1)
		p.StrokePath(&path, w, c)
	}
	x0, y0 := at(d.Pts[0])
	switch d.Kind {
	case "hline":
		p.Line(plot.X, y0, plot.X+plot.W, y0, w, col)
		return
	case "hray":
		p.Line(x0, y0, plot.X+plot.W, y0, w, col)
		return
	case "vline":
		p.Line(x0, plot.Y, x0, plot.Y+plot.H, w, col)
		return
	case "text":
		sp := ui.Span{Text: d.Text, Size: float32(textSize(d)), Weight: 500, Color: col}
		tw, th := p.MeasureText(0, sp)
		p.RichText(x0, y0-th/2, 0, sp)
		a.textBox[d.ID] = ui.Rect{X: x0 - plot.X - 2, Y: y0 - plot.Y - th/2 - 2, W: tw + 4, H: th + 4}
		return
	}
	x1, y1 := at(d.Pts[1])
	switch d.Kind {
	case "trend":
		line(x0, y0, x1, y1, w, col)
	case "ray":
		fx, fy := far(x0, y0, x1, y1)
		line(x0, y0, fx, fy, w, col)
	case "channel":
		off := channelOffset(m, d)
		var band ui.Path
		band.MoveTo(x0, y0).LineTo(x1, y1).LineTo(x1, y1+off).LineTo(x0, y0+off).Close()
		p.FillPath(&band, col.Alpha(0.1))
		line(x0, y0, x1, y1, w, col)
		line(x0, y0+off, x1, y1+off, w, col)
		line(x0, y0+off/2, x1, y1+off/2, 1, col.Alpha(0.5))
	case "rect":
		r := ui.Rect{X: min(x0, x1), Y: min(y0, y1), W: float32(math.Abs(float64(x1 - x0))), H: float32(math.Abs(float64(y1 - y0)))}
		p.Fill(r, col.Alpha(0.15), 0)
		p.Stroke(r, col, 0, w)
	case "fib":
		l, r := min(x0, x1), max(x0, x1)
		top, bottom := min(y0, y1), max(y0, y1)
		p.Fill(ui.Rect{X: l, Y: top, W: r - l, H: bottom - top}, col.Alpha(0.06), 0)
		line(x0, y0, x1, y1, 1, col.Alpha(0.5))
		dec := a.chartDecimals(d.Pts[0].P)
		for _, lv := range fibLevels {
			pr := d.Pts[1].P + (d.Pts[0].P-d.Pts[1].P)*lv
			ly := plot.Y + m.y(pr)
			p.Line(l, ly, r, ly, max(w-1, 1), col)
			label(p, l+4, ly-8, span(fmt.Sprintf("%g (%s)", lv, formatPrice(pr, dec)), 11, col, 0), alignStart)
		}
	case "range":
		a.drawRange(p, plot, m, k, d, x0, y0, x1, y1, line)
	}
}

// drawRange is the price range: the box between its points, an arrow from
// the first price to the second, and how far that is in price, percent,
// bars and time.
func (a *application) drawRange(p *ui.Painter, plot ui.Rect, m mapper, k palette, d *drawing, x0, y0, x1, y1 float32, line func(x0, y0, x1, y1, w float32, c ui.Color)) {
	from, to := d.Pts[0], d.Pts[1]
	col := k.up
	if to.P < from.P {
		col = k.down
	}
	l, r, top, bottom := min(x0, x1), max(x0, x1), min(y0, y1), max(y0, y1)
	p.Fill(ui.Rect{X: l, Y: top, W: r - l, H: bottom - top}, col.Alpha(0.15), 0)
	cx := (l + r) / 2
	line(cx, y0, cx, y1, 1.5, col)
	head := float32(5)
	if y1 > y0 {
		head = -5
	}
	line(cx-5, y1+head, cx, y1, 1.5, col)
	line(cx+5, y1+head, cx, y1, 1.5, col)
	dec := a.chartDecimals(from.P)
	move, pct := to.P-from.P, 0.0
	if from.P != 0 {
		pct = move / from.P * 100
	}
	bars := int(math.Round(m.idx(to.T) - m.idx(from.T)))
	lines := []ui.Span{
		span(fmt.Sprintf("%s (%+.2f%%)", formatSigned(move, dec), pct), 12, k.onLabel, 600),
		span(fmt.Sprintf("%d 根K线 · %s", bars, spanText(to.T-from.T)), 11.5, k.onLabel, 0),
	}
	var bw, bh float32
	for _, sp := range lines {
		w, h := p.MeasureText(0, sp)
		bw, bh = max(bw, w), bh+h
	}
	bw, bh = bw+16, bh+10
	// Past the end the arrow points to, unless the plot has no room there.
	above, below := top-6-bh, bottom+6
	by := below
	if to.P >= from.P && above >= plot.Y || below+bh > plot.Y+plot.H {
		by = above
	}
	box := ui.Rect{X: cx - bw/2, Y: by, W: bw, H: bh}
	p.Fill(box, col, 4)
	ty := box.Y + 5
	for _, sp := range lines {
		w, h := p.MeasureText(0, sp)
		p.RichText(cx-w/2, ty, 0, sp)
		ty += h
	}
}

// spanText is a stretch of time, as the price range reads it.
func spanText(ms int64) string {
	s := ms / 1000
	if s < 0 {
		s = -s
	}
	d, h, m := s/86400, s/3600%24, s/60%60
	switch {
	case d > 0 && h > 0:
		return fmt.Sprintf("%d天%d小时", d, h)
	case d > 0:
		return fmt.Sprintf("%d天", d)
	case h > 0 && m > 0:
		return fmt.Sprintf("%d小时%d分", h, m)
	case h > 0:
		return fmt.Sprintf("%d小时", h)
	}
	return fmt.Sprintf("%d分", m)
}

// drawDrawingLabels puts the prices of horizontal lines on the price axis.
func (a *application) drawDrawingLabels(p *ui.Painter, plot ui.Rect, m mapper, k palette, priceText func(float64) string, size float32) {
	if a.hide {
		return
	}
	for _, d := range a.set.Drawings[a.symbol] {
		if d.Kind != "hline" && d.Kind != "hray" {
			continue
		}
		y := m.y(d.Pts[0].P)
		if y < 0 || y > plot.H {
			continue
		}
		y += plot.Y
		p.Fill(ui.Rect{X: plot.X + plot.W, Y: y - 10, W: axisW, H: 20}, d.Color.or(k.accent), 2)
		label(p, plot.X+plot.W+8, y, span(priceText(d.Pts[0].P), size+0.5, k.onLabel, 0), alignStart)
	}
}
