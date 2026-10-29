package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// palette is the window's colors, after TradingView's.
type palette struct {
	bg, line, grid, field, hover, chosen ui.Color
	text, muted, accent                  ui.Color
	up, down, amber                      ui.Color
	upInk, downInk                       ui.Color // up and down for text and lines, set by withInk
	cross, label, onLabel                ui.Color
}

func paletteFor(dark bool) palette {
	k := themePalette(dark)
	k.upInk, k.downInk = k.up, k.down
	return k
}

func themePalette(dark bool) palette {
	if dark {
		return palette{
			bg: ui.Hex("#131722"), line: ui.Hex("#2a2e39"), grid: ui.RGBA(255, 255, 255, 0.045),
			field: ui.Hex("#1e222d"), hover: ui.RGBA(255, 255, 255, 0.05), chosen: ui.RGBA(41, 98, 255, 0.2),
			text: ui.Hex("#d1d4dc"), muted: ui.Hex("#787b86"), accent: ui.Hex("#2962ff"),
			up: ui.Hex("#26a69a"), down: ui.Hex("#ef5350"), amber: ui.Hex("#f0b90b"),
			cross: ui.Hex("#9598a1"), label: ui.Hex("#363a45"), onLabel: ui.Hex("#ffffff"),
		}
	}
	return palette{
		bg: ui.Hex("#ffffff"), line: ui.Hex("#e0e3eb"), grid: ui.RGBA(0, 0, 0, 0.05),
		field: ui.Hex("#f0f3fa"), hover: ui.RGBA(0, 0, 0, 0.04), chosen: ui.RGBA(41, 98, 255, 0.1),
		text: ui.Hex("#131722"), muted: ui.Hex("#787b86"), accent: ui.Hex("#2962ff"),
		up: ui.Hex("#089981"), down: ui.Hex("#f23645"), amber: ui.Hex("#d39e00"),
		cross: ui.Hex("#9598a1"), label: ui.Hex("#131722"), onLabel: ui.Hex("#ffffff"),
	}
}

// apply hands the palette to MyGo's controls.
func (k palette) apply(t *ui.Theme) {
	t.Font = "MiSans, system-ui"
	t.Radius = 6
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = k.bg, k.field, k.hover, k.hover
	t.Border, t.Text, t.TextMuted = k.line, k.text, k.muted
	t.Accent, t.AccentText = k.accent, ui.Hex("#ffffff")
	t.AccentHover, t.AccentPressed = k.accent.Mix(ui.Hex("#ffffff"), 0.12), k.accent.Mix(ui.Hex("#000000"), 0.12)
	t.Selection = k.accent.Alpha(0.3)
}

// breaks is the day lines' color unless the settings say otherwise.
func (k palette) breaks() ui.Color { return k.accent.Alpha(0.55) }

// move is the color of text telling a move up or down.
func (k palette) move(v float64) ui.Color {
	if v >= 0 {
		return k.upInk
	}
	return k.downInk
}

// withInk sets the colors labels, text and lines take for up and down, as
// st.Ink says. Automatic, they are up and down, unless one is lost on the
// background bg: then its border's, then the text's.
func (k palette) withInk(st *style, bg ui.Color) palette {
	up, down := readable(k.up, st.BorderUp.or(k.up), bg, k.text), readable(k.down, st.BorderDown.or(k.down), bg, k.text)
	switch st.Ink {
	case "body":
		up, down = k.up, k.down
	case "custom":
		up, down = st.InkUp.or(up), st.InkDown.or(down)
	}
	k.upInk, k.downInk = up, down
	return k
}

func readable(c, border, bg, text ui.Color) ui.Color {
	near := func(c ui.Color) bool { d := luma(c) - luma(bg); return d < 0.12 && d > -0.12 }
	switch {
	case !near(c):
		return c
	case !near(border):
		return border
	}
	return text
}

type align int

const (
	alignStart align = iota
	alignCenter
)

func span(s string, size float32, c ui.Color, weight int) ui.Span {
	return ui.Span{Text: s, Size: size, Color: c, Weight: weight, Features: "tnum"}
}

// label draws sp with its middle at y and x at its start or center.
func label(p *ui.Painter, x, y float32, sp ui.Span, a align) {
	w, h := p.MeasureText(0, sp)
	if a == alignCenter {
		x -= w / 2
	}
	p.RichText(x, y-h/2, 0, sp)
}

// dash draws a horizontal or vertical line from x0, y0 to x1, y1, w wide, in a
// pattern: "" dashed, "dotted", "solid".
func dash(p *ui.Painter, x0, y0, x1, y1, w float32, c ui.Color, pattern string) {
	on, off := float32(4), float32(3)
	switch pattern {
	case "dotted":
		on, off = w, 2*w
	case "solid":
		p.Line(x0, y0, x1, y1, w, c)
		return
	}
	if y0 == y1 {
		for x := x0; x < x1; x += on + off {
			p.Line(x, y0, min(x+on, x1), y0, w, c)
		}
		return
	}
	for y := y0; y < y1; y += on + off {
		p.Line(x0, y, x0, min(y+on, y1), w, c)
	}
}

// outline draws r's edges one device pixel (px) wide, inside it.
func outline(p *ui.Painter, r ui.Rect, px float32, c ui.Color) {
	if r.W <= 2*px || r.H <= 2*px {
		p.Fill(r, c, 0)
		return
	}
	l, rt := r.X+px/2, r.X+r.W-px/2
	p.Line(l, r.Y, l, r.Y+r.H, px, c)
	p.Line(rt, r.Y, rt, r.Y+r.H, px, c)
	p.Line(r.X, r.Y+px/2, r.X+r.W, r.Y+px/2, px, c)
	p.Line(r.X, r.Y+r.H-px/2, r.X+r.W, r.Y+r.H-px/2, px, c)
}

func pctText(v float64, d int) string {
	if math.Abs(v) < 0.5*math.Pow(10, -float64(d)) {
		return formatPrice(0, d) + "%"
	}
	return formatSigned(v, d) + "%"
}

// rising is whether bar i counts as up: by its own move, or by the move from
// the last close.
func (a *application) rising(i int) bool {
	b := a.bars[i]
	if a.set.Chart.PrevClose && i > 0 {
		return b.C >= a.bars[i-1].C
	}
	return b.C >= b.O
}

// luma is a color's lightness, 0 black to 1 white.
func luma(c ui.Color) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// onColor is text that reads on a label filled with c.
func onColor(c ui.Color) ui.Color {
	if luma(c) > 0.62 {
		return ui.Hex("#131722")
	}
	return ui.Hex("#ffffff")
}

// inkColor is bar i's color for what is drawn as a line or as text, which
// must show on the background: a body as light as a white background (or as
// dark as a black one) gives way to its border's color, then to the text's.
func (a *application) inkColor(i int, k palette) ui.Color {
	k = k.withInk(&a.set.Chart, a.set.Chart.Background.or(k.bg))
	if a.rising(i) {
		return k.upInk
	}
	return k.downInk
}

func (a *application) barColor(i int, k palette) ui.Color {
	if a.rising(i) {
		return k.up
	}
	return k.down
}

// drawChart paints the bars in r: the candles' pane, the indicators' panes
// below it, the price axis on the right of each and the time axis under
// them all, as the settings have them. hx, hy is the pointer, relative to r,
// when over.
func (a *application) drawChart(p *ui.Painter, r ui.Rect, k palette, hx, hy float32, over bool) {
	st := &a.set.Chart
	bottom := r.Y + r.H - timeH // the top of the time axis
	mainH, panes := a.layout(r.H - timeH)
	plot := ui.Rect{X: r.X, Y: r.Y, W: r.W - axisW, H: mainH}
	a.plotW, a.plotH, a.boxH = plot.W, plot.H, r.H
	s := p.Scale()
	px := 1 / s
	// mid is the middle of the device pixel at v, where a one-pixel line is sharp.
	mid := func(v float32) float32 { return (float32(math.Floor(float64(v*s))) + 0.5) / s }
	snap := func(v float32) float32 { return float32(math.Round(float64(v*s))) / s }
	p.Fill(r, st.Background.or(k.bg), 0)
	p.Line(mid(plot.X+plot.W), r.Y, mid(plot.X+plot.W), r.Y+r.H, px, k.line)
	p.Line(r.X, mid(bottom), r.X+r.W, mid(bottom), px, k.line)
	for i, pn := range panes {
		yy := mid(r.Y + pn.y)
		if a.drag == dragSep && a.sep == i || a.drag == dragNone && over && math.Abs(float64(hy-pn.y)) <= sepGrip {
			p.Fill(ui.Rect{X: r.X, Y: yy - 1.5, W: r.W, H: 3}, k.accent.Alpha(0.6), 0)
		} else {
			p.Line(r.X, yy, r.X+r.W, yy, px, k.line)
		}
	}
	n := len(a.bars)
	if n == 0 || plot.W <= 0 || plot.H <= 0 {
		return
	}
	v := a.view
	v.clamp(plot.W, n)
	from, to := v.span(plot.W, n)
	if to < from {
		return
	}
	bandTop, bandBottom := st.band()
	ax := axis{h: plot.H, top: bandTop, bottom: bandBottom, log: st.Scale == "log"}
	a.axis = ax
	sc := a.scale
	if !sc.manual {
		sc.lo, sc.hi = priceRange(a.bars[from : to+1])
		sc.lo, sc.hi = a.overlayRange(from, to, sc.lo, sc.hi, ax.log)
	}
	a.shown = sc
	dm := mapper{v: v, w: plot.W, h: plot.H, bars: a.bars, iv: a.intervalMs(), sc: sc, ax: ax}
	y := func(price float64) float32 { return plot.Y + sc.y(price, ax) }
	priceAt := func(py float32) float64 { return sc.at(py-plot.Y, ax) }
	last := a.bars[n-1]
	d := a.chartDecimals(last.C)
	ivMs := a.intervalMs()
	axisX := plot.X + plot.W + 8
	size, textCol := float32(st.TextSize), st.TextColor.or(k.muted)
	// On a percent scale prices read as the move from the first bar in sight.
	ref := a.bars[from].C
	pct := st.Scale == "percent" && ref > 0
	priceText := func(price float64) string {
		if pct {
			return pctText((price/ref-1)*100, 2)
		}
		return formatPrice(price, d)
	}

	// The labels on the price axis, which the round prices make way for: the
	// last price, taller with the time left until its bar closes by Binance's
	// clock, and the highest and lowest prices in sight.
	var taken [][2]float32
	showValue := st.LastPrice == "" || st.LastPrice == "value"
	showLine := st.LastPrice == "" || st.LastPrice == "line"
	ly := min(max(mid(y(last.C)), plot.Y+10), plot.Y+plot.H-24)
	now := time.Now().UnixMilli() + a.clock
	closeAt := last.T + ivMs
	running := showValue && st.Countdown && now >= last.T && now < closeAt
	lh := float32(20)
	if running {
		lh = 34
	}
	lastCol := a.inkColor(n-1, k)
	if st.Type == "line" || st.Type == "area" {
		lastCol = st.Line.or(k.accent)
	}
	if showValue {
		taken = append(taken, [2]float32{ly - 10, ly - 10 + lh})
	}
	hiI, loI := from, from
	for i := from; i <= to; i++ {
		if a.bars[i].H > a.bars[hiI].H {
			hiI = i
		}
		if a.bars[i].L < a.bars[loI].L {
			loI = i
		}
	}
	var extremes []float64
	if st.HighLow {
		for _, e := range []float64{a.bars[hiI].H, a.bars[loI].L} {
			if yy := mid(y(e)); yy >= plot.Y && yy <= plot.Y+plot.H {
				extremes = append(extremes, e)
				taken = append(taken, [2]float32{yy - 10, yy + 10})
			}
		}
	}
	tags := a.overlayTags(func(v float64) float32 { return mid(y(v)) }, priceText, plot.Y, plot.Y+plot.H)
	taken = append(taken, tagSpans(tags)...)

	type mark struct {
		y    float32
		text string
	}
	var marks []mark
	count := max(int(plot.H/56), 2)
	pTop, pBottom := priceAt(plot.Y), priceAt(plot.Y+plot.H)
	if pct {
		ql, qh := (pBottom/ref-1)*100, (pTop/ref-1)*100
		qd := stepDecimals(niceStep(qh-ql, count))
		for _, q := range priceTicks(ql, qh, count, false) {
			marks = append(marks, mark{mid(y(ref * (1 + q/100))), pctText(q, qd)})
		}
	} else {
		for _, pr := range priceTicks(pBottom, pTop, count, ax.log) {
			marks = append(marks, mark{mid(y(pr)), formatPrice(pr, min(max(d, stepDecimals(pr)), 10))})
		}
	}
	hgrid, labelled := st.HorzColor.or(k.grid), float32(-1e9)
	for _, m := range marks {
		if m.y < plot.Y+8 || m.y > plot.Y+plot.H-8 {
			continue
		}
		if st.HorzGrid {
			p.Line(plot.X, m.y, plot.X+plot.W, m.y, px, hgrid)
		}
		if float32(math.Abs(float64(m.y-labelled))) < size+4 {
			continue
		}
		if st.NoOverlap && covered(m.y, taken) {
			continue
		}
		label(p, axisX, m.y, span(m.text, size, textCol, 0), alignStart)
		labelled = m.y
	}
	vgrid := st.VertColor.or(k.grid)
	for _, t := range timeTicks(a.bars, from, to, ivMs, v.spacing, st.loc(), st.Clock12) {
		xx := mid(plot.X + v.x(t.i, plot.W, n))
		if xx < plot.X+24 || xx > plot.X+plot.W-24 {
			continue
		}
		if st.VertGrid {
			p.Line(xx, plot.Y, xx, bottom, px, vgrid)
		}
		weight := 0
		if t.level >= 2 {
			weight = 600
		}
		label(p, xx, bottom+timeH/2, span(t.text, size, textCol, weight), alignCenter)
	}
	// Where each day begins: under a day's bars only, where a bar is a day or more.
	if st.Breaks && ivMs < 86_400_000 {
		col, w, at := st.BreakColor.or(k.breaks()), float32(st.BreakWidth)*px, mid
		if st.BreakWidth%2 == 0 {
			at = snap // an even width is sharp on a pixel's edge
		}
		for _, b := range dayBreaks(a.bars, from, to, ivMs, st.breakLoc()) {
			xx := at(plot.X + v.x(b.i, plot.W, n) + (b.frac-0.5)*v.spacing)
			if xx >= plot.X && xx <= plot.X+plot.W {
				dash(p, xx, plot.Y, xx, bottom, w, col, st.BreakStyle)
			}
		}
	}
	if st.Watermark {
		base, quote := a.names(a.symbol)
		iv, _ := intervalOf(a.interval)
		wm := ui.Span{Text: base + suffix(quote) + " · " + iv.label, Size: 56, Weight: 700, Color: k.text.Alpha(0.07)}
		w, h := p.MeasureText(0, wm)
		p.RichText(plot.X+(plot.W-w)/2, plot.Y+(plot.H-h)/2, 0, wm)
	}

	maxVol := 0.0
	for _, b := range a.bars[from : to+1] {
		maxVol = max(maxVol, b.V)
	}
	bw := max(px, float32(math.Floor(float64(v.spacing*0.72*s)))/s)
	if w := int(math.Round(float64(bw * s))); w > 1 && w%2 == 0 {
		bw -= px // odd, so the wick runs down its middle
	}
	xAt := func(i int) float32 { return plot.X + v.x(i, plot.W, n) }
	// The volume takes the candles' colors, but one lost on the background
	// (white rising candles on white) gives way to the bars' own.
	bg := st.Background.or(k.bg)
	volUp, volDown := readable(k.up, ui.Hex(barUp), bg, ui.Hex(barUp)), readable(k.down, ui.Hex(barDown), bg, ui.Hex(barDown))
	p.Clip(plot, 0, func() {
		for i := from; i <= to && st.Volume && maxVol > 0; i++ {
			h := float32(a.bars[i].V/maxVol) * plot.H * 0.18
			xc := mid(plot.X + v.x(i, plot.W, n))
			c := volDown
			if a.rising(i) {
				c = volUp
			}
			p.Fill(ui.Rect{X: xc - bw/2, Y: plot.Y + plot.H - h, W: bw, H: h}, c.Alpha(0.3), 0)
		}
		a.drawBars(p, plot, k, v, from, to, y, bw, px, mid, snap)
		a.drawOverlays(p, xAt, y, from, to)
		if showLine {
			dash(p, plot.X, mid(y(last.C)), plot.X+plot.W, mid(y(last.C)), px, lastCol, "")
		}
		for _, e := range extremes {
			dash(p, plot.X, mid(y(e)), plot.X+plot.W, mid(y(e)), px, k.cross, "dotted")
		}
		a.drawDrawings(p, plot, dm, k)
	})

	drawTags(p, plot.X+plot.W, tags, size+0.5) // under the last price's label
	for _, e := range extremes {
		yy := mid(y(e))
		p.Fill(ui.Rect{X: plot.X + plot.W, Y: yy - 10, W: axisW, H: 20}, k.label, 2)
		label(p, axisX, yy, span(priceText(e), size+0.5, k.onLabel, 0), alignStart)
	}
	if showValue {
		p.Fill(ui.Rect{X: plot.X + plot.W, Y: ly - 10, W: axisW, H: lh}, lastCol, 2)
		label(p, axisX, ly, span(priceText(last.C), size+0.5, onColor(lastCol), 600), alignStart)
	}
	a.drawDrawingLabels(p, plot, dm, k, priceText, size)
	if running {
		left := closeAt - now
		label(p, axisX, ly+14, span(countdown(left), size, onColor(lastCol).Alpha(0.85), 0), alignStart)
		p.After(time.Duration(left%1000+10) * time.Millisecond)
	}

	scales := make([]paneScale, len(panes))
	for i, pn := range panes {
		scales[i] = a.drawPane(p, ui.Rect{X: plot.X, Y: r.Y + pn.y, W: plot.W, H: pn.h}, &a.set.Indicators[pn.idx], xAt, from, to, k, bw, px, mid)
	}

	// The crosshair runs down every pane; its line across, and the label on
	// the axis, are in the pane the pointer is in.
	if over && hx >= 0 && hx < plot.W && hy >= 0 && r.Y+hy < bottom {
		hover := a.hoverBar(hx, hy, over)
		cx, cy := mid(plot.X+v.x(hover, plot.W, n)), mid(r.Y+hy)
		cross := st.Cross.or(k.cross)
		dash(p, cx, plot.Y, cx, bottom, px, cross, st.CrossStyle)
		across := func(top, h float32, text string) {
			dash(p, plot.X, cy, plot.X+plot.W, cy, px, cross, st.CrossStyle)
			cly := min(max(cy, top+10), top+h-10) // the label stays on the axis
			p.Fill(ui.Rect{X: plot.X + plot.W, Y: cly - 10, W: axisW, H: 20}, k.label, 2)
			label(p, axisX, cly, span(text, size+0.5, k.onLabel, 0), alignStart)
		}
		if cy < plot.Y+plot.H {
			across(plot.Y, plot.H, priceText(priceAt(cy)))
		}
		for i, pn := range panes {
			if top := r.Y + pn.y; cy >= top && cy < top+pn.h {
				ps := scales[i]
				across(top, pn.h, a.valueText(ps.k, ps.sc.at(cy-top, ps.ax)))
			}
		}
		ts := span(st.crossTime(a.bars[hover].T, ivMs), size+0.5, k.onLabel, 0)
		w, _ := p.MeasureText(0, ts)
		bx := min(max(cx-w/2-8, plot.X), plot.X+plot.W-w-16)
		p.Fill(ui.Rect{X: bx, Y: bottom + 3, W: w + 16, H: timeH - 6}, k.label, 2)
		label(p, bx+8, bottom+timeH/2, ts, alignStart)
	}
}

func covered(y float32, taken [][2]float32) bool {
	for _, t := range taken {
		if y+8 > t[0] && y-8 < t[1] {
			return true
		}
	}
	return false
}

// drawBars draws bars from..to as the chart type has them, bw wide.
func (a *application) drawBars(p *ui.Painter, plot ui.Rect, k palette, v view, from, to int, y func(float64) float32, bw, px float32, mid, snap func(float32) float32) {
	st := &a.set.Chart
	n := len(a.bars)
	switch st.Type {
	case "line", "area":
		col := st.Line.or(k.accent)
		// One bar past each edge, so the line runs off the plot.
		i0, i1 := max(from-1, 0), min(to+1, n-1)
		var line, area ui.Path
		for i := i0; i <= i1; i++ {
			x, yy := plot.X+v.x(i, plot.W, n), y(a.bars[i].C)
			if i == i0 {
				line.MoveTo(x, yy)
				area.MoveTo(x, yy)
			} else {
				line.LineTo(x, yy)
				area.LineTo(x, yy)
			}
		}
		if st.Type == "area" {
			area.LineTo(plot.X+v.x(i1, plot.W, n), plot.Y+plot.H).LineTo(plot.X+v.x(i0, plot.W, n), plot.Y+plot.H).Close()
			p.FillPathGradient(&area, ui.LinearGradient{From: col.Alpha(0.28), To: col.Alpha(0.02), Angle: 180})
		}
		p.StrokePath(&line, 2, col)
	case "bars":
		tick := max(px, float32(math.Floor(float64(v.spacing*0.36/px)))*px)
		for i := from; i <= to; i++ {
			b, c := a.bars[i], a.inkColor(i, k)
			xc := mid(plot.X + v.x(i, plot.W, n))
			p.Line(xc, snap(y(b.H)), xc, snap(y(b.L)), px, c)
			p.Line(xc-tick, mid(y(b.O)), xc, mid(y(b.O)), px, c)
			p.Line(xc, mid(y(b.C)), xc+tick, mid(y(b.C)), px, c)
		}
	default:
		for i := from; i <= to; i++ {
			b := a.bars[i]
			up, filled, edged := a.rising(i), st.Body, st.Borders
			if st.Type == "hollow" { // colored by the last close, hollow when it closed higher than it opened
				up = i == 0 || b.C >= a.bars[i-1].C
				filled, edged = b.C < b.O, true
			}
			body, edge, wick := k.down, st.BorderDown.or(k.down), st.WickDown.or(k.down)
			if up {
				body, edge, wick = k.up, st.BorderUp.or(k.up), st.WickUp.or(k.up)
			}
			xc := mid(plot.X + v.x(i, plot.W, n))
			t, bt := snap(y(max(b.O, b.C))), snap(y(min(b.O, b.C)))
			box := ui.Rect{X: xc - bw/2, Y: t, W: bw, H: max(bt-t, px)}
			if st.Wick {
				p.Line(xc, snap(y(b.H)), xc, box.Y, px, wick)
				p.Line(xc, box.Y+box.H, xc, snap(y(b.L)), px, wick)
			}
			if filled {
				p.Fill(box, body, 0)
			}
			if edged {
				outline(p, box, px, edge)
			}
		}
	}
}

// legendSpans are the market's name and bar i read out, as the legend over
// the candles shows them: the bar under the crosshair, else the newest; the
// status line settings pick the parts.
func (a *application) legendSpans(k palette, i int) (title, spans []ui.Span) {
	st := &a.set.Chart
	b := a.bars[i]
	d := a.chartDecimals(a.bars[len(a.bars)-1].C)
	base, quote := a.names(a.symbol)
	if st.Title {
		iv, _ := intervalOf(a.interval)
		title = []ui.Span{
			{Text: base + suffix(quote), Size: 14, Weight: 600, Color: k.text},
			{Text: "  ·  " + iv.label + "  ·  Binance", Size: 12, Color: k.muted}}
	}
	if st.Values {
		col := a.inkColor(i, k)
		for _, f := range []struct {
			name string
			v    float64
		}{{"开", b.O}, {"高", b.H}, {"低", b.L}, {"收", b.C}} {
			spans = append(spans, ui.Span{Text: f.name + " ", Size: 12, Color: k.muted}, span(formatPrice(f.v, d)+"    ", 12, col, 0))
		}
	}
	if st.Change {
		prev := b.O
		if i > 0 {
			prev = a.bars[i-1].C
		}
		move, pct := b.C-prev, 0.0
		if prev != 0 {
			pct = move / prev * 100
		}
		spans = append(spans, span(formatSigned(move, d)+fmt.Sprintf(" (%+.2f%%)    ", pct), 12, k.move(move), 0))
	}
	if st.VolumeValue {
		spans = append(spans, ui.Span{Text: "量 ", Size: 12, Color: k.muted}, span(formatVolume(b.V)+" "+base, 12, k.text, 0))
	}
	return title, spans
}

func suffix(quote string) string {
	if quote == "" {
		return ""
	}
	return "/" + quote
}
