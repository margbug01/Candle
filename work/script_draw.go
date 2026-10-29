package main

import (
	"math"

	"github.com/egoist/mygo/ui"

	"candle/pine"
)

// Drawing what a Pine script works out: its backgrounds, fills, levels,
// plots in their styles and colors, which may change from bar to bar, and
// its shapes.

// canvas is where indicators are drawn in a frame, a pane or the candles',
// and how bars and values land on it.
type canvas struct {
	p        *ui.Painter
	r        ui.Rect
	x        func(int) float32
	y        func(float64) float32
	from, to int
	bw, px   float32
	sp       float32 // the room a bar takes
	mid      func(float32) float32
	k        palette
	main     bool // the candles': shapes go above and below them
}

func toColor(v float64) (ui.Color, bool) {
	c, ok := packedColor(v)
	return ui.Color{R: c[0], G: c[1], B: c[2], A: c[3]}, ok && c[3] > 0
}

// plotColor is plot j's color at bar i: as the settings have it, unless it
// changes from bar to bar, else as the script has it. ok is false for none.
func plotColor(ind *indicator, k *indKind, s *series, j, i int) (ui.Color, bool) {
	if !k.plots[j].dynamic && j < len(ind.Colors) && ind.Colors[j].set() {
		return ui.Color(ind.Colors[j]), true
	}
	if s == nil || s.vm == nil || j >= len(s.vm.PlotColors) || i < 0 || i >= len(s.vm.PlotColors[j]) {
		return ui.Color{}, false
	}
	return toColor(s.vm.PlotColors[j][i])
}

// plotShift is how many bars to the right plot j is drawn.
func plotShift(s *series, j int) int {
	if s == nil || s.vm == nil || j >= len(s.vm.PlotOffset) {
		return 0
	}
	return s.vm.PlotOffset[j]
}

// scriptWidth is plot j's line width, as set, else as the script has it.
func scriptWidth(ind *indicator, s *series, j int) float32 {
	if j < len(ind.Widths) && ind.Widths[j] >= 1 {
		return float32(ind.Widths[j])
	}
	if s != nil && s.vm != nil && j < len(s.vm.PlotWidth) && s.vm.PlotWidth[j] >= 1 {
		return float32(min(s.vm.PlotWidth[j], maxLineWidth))
	}
	return 1
}

// span is the bars of vals, drawn off to the right, that land from from to to,
// and one past each end.
func barSpan(n, from, to, off int) (int, int) {
	return max(from-1-off, 0), min(to+1-off, n-1)
}

// scriptRange widens lo and hi to the values in sight: the plots drawn, the
// levels, and the bases bars rise from.
func scriptRange(k *indKind, s *series, from, to int, lo, hi float64) (float64, float64) {
	if s == nil || s.vm == nil {
		return lo, hi
	}
	sc := k.script
	for j, pl := range sc.Plots {
		if pl.Hidden || j >= len(s.out) {
			continue
		}
		off := plotShift(s, j)
		vals := s.out[j]
		i0, i1 := max(from-off, 0), min(to-off, len(vals)-1)
		seen := false
		for i := i0; i <= i1; i++ {
			if v := vals[i]; !math.IsNaN(v) && !math.IsInf(v, 0) {
				lo, hi, seen = min(lo, v), max(hi, v), true
			}
		}
		if seen && (pl.Style == pine.StyleHistogram || pl.Style == pine.StyleColumns || pl.Style == pine.StyleArea || pl.Style == pine.StyleAreaBr) {
			lo, hi = min(lo, pl.Histbase), max(hi, pl.Histbase)
		}
	}
	for j := range sc.Hlines {
		if v := s.vm.HlinePrice[j]; !math.IsNaN(v) && k.pane {
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	for j, sh := range sc.Shapes {
		if sh.Location != pine.LocAbsolute {
			continue
		}
		off := s.vm.ShapeOff[j]
		vals := s.vm.ShapeVals[j]
		for i := max(from-off, 0); i <= min(to-off, len(vals)-1); i++ {
			if v := vals[i]; !math.IsNaN(v) {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	return lo, hi
}

// drawScript draws what ind's script worked out on cv.
func (a *application) drawScript(cv canvas, ind *indicator, k *indKind, s *series) {
	if s == nil || s.vm == nil || k.script == nil {
		return
	}
	vm, sc, p := s.vm, k.script, cv.p
	// Backgrounds, behind everything.
	for j := range sc.Bgs {
		cols := vm.BgColors[j]
		for i := max(cv.from, 0); i <= min(cv.to, len(cols)-1); i++ {
			c, ok := toColor(cols[i])
			if !ok {
				continue
			}
			x0 := cv.x(i) - cv.sp/2
			p.Fill(ui.Rect{X: x0, Y: cv.r.Y, W: cv.sp, H: cv.r.H}, c, 0)
		}
	}
	for j, f := range sc.Fills {
		a.drawFill(cv, s, sc, j, f)
	}
	for j, h := range sc.Hlines {
		v := vm.HlinePrice[j]
		if math.IsNaN(v) {
			continue
		}
		c, _ := toColor(float64(h.Color))
		pattern := map[int]string{pine.LineSolid: "solid", pine.LineDashed: "", pine.LineDotted: "dotted"}[h.Style]
		yy := cv.mid(cv.y(v))
		dash(p, cv.r.X, yy, cv.r.X+cv.r.W, yy, float32(max(h.Width, 1))*cv.px, c, pattern)
	}
	// The bars and areas first, behind the lines, which they would cut.
	for pass := 0; pass < 2; pass++ {
		for j, pl := range sc.Plots {
			if pl.Hidden || j >= len(s.out) {
				continue
			}
			bars := pl.Style == pine.StyleColumns || pl.Style == pine.StyleHistogram || pl.Style == pine.StyleArea || pl.Style == pine.StyleAreaBr
			if bars != (pass == 0) {
				continue
			}
			a.drawPlot(cv, ind, k, s, j, pl)
		}
	}
	for j, sh := range sc.Shapes {
		drawShapes(cv, s, j, sh)
	}
}

func (a *application) drawPlot(cv canvas, ind *indicator, k *indKind, s *series, j int, pl pine.Plot) {
	p, vals, off := cv.p, s.out[j], plotShift(s, j)
	i0, i1 := barSpan(len(vals), cv.from, cv.to, off)
	w := scriptWidth(ind, s, j)
	base := cv.y(pl.Histbase)
	switch pl.Style {
	case pine.StyleColumns, pine.StyleHistogram:
		for i := i0; i <= i1; i++ {
			v := vals[i]
			c, ok := plotColor(ind, k, s, j, i)
			if math.IsNaN(v) || !ok {
				continue
			}
			xc, yy := cv.mid(cv.x(i+off)), cv.y(v)
			top, h := min(base, yy), max(float32(math.Abs(float64(yy-base))), cv.px)
			bw := cv.bw
			if pl.Style == pine.StyleHistogram {
				bw = max(w*cv.px, cv.px)
			}
			p.Fill(ui.Rect{X: xc - bw/2, Y: top, W: bw, H: h}, c, 0)
		}
	case pine.StyleCircles, pine.StyleCross:
		r := 1.5 + w
		for i := i0; i <= i1; i++ {
			v := vals[i]
			c, ok := plotColor(ind, k, s, j, i)
			if math.IsNaN(v) || !ok {
				continue
			}
			xc, yy := cv.x(i+off), cv.y(v)
			if pl.Style == pine.StyleCircles {
				var path ui.Path
				p.FillPath(path.Circle(xc, yy, r), c)
			} else {
				p.Line(xc-r-1, yy, xc+r+1, yy, cv.px*max(w/2, 1), c)
				p.Line(xc, yy-r-1, xc, yy+r+1, cv.px*max(w/2, 1), c)
			}
		}
	default:
		if pl.Style == pine.StyleArea || pl.Style == pine.StyleAreaBr {
			a.fillArea(cv, ind, k, s, j, pl, i0, i1, base)
		}
		strokePlot(cv, ind, k, s, j, pl.Style, i0, i1, w)
	}
}

// strokePlot draws plot j as a line, in runs of one color: over bars
// without a value unless the style breaks there, stepping for the step
// styles. A bar without a color draws nothing into it.
func strokePlot(cv canvas, ind *indicator, k *indKind, s *series, j, style, i0, i1 int, w float32) {
	vals, off := s.out[j], plotShift(s, j)
	breaks := style == pine.StyleLineBr || style == pine.StyleStepBr || style == pine.StyleAreaBr
	step := style == pine.StyleStep || style == pine.StyleStepBr
	var path ui.Path
	var cur ui.Color
	open := false
	flush := func() {
		if open {
			cv.p.StrokePath(&path, w, cur)
			path.Reset()
			open = false
		}
	}
	prev, px, py := -1, float32(0), float32(0)
	// Start from the last value before the span, so the line comes in.
	for i := i0 - 1; i >= 0 && !breaks; i-- {
		if !math.IsNaN(vals[i]) {
			prev, px, py = i, cv.x(i+off), cv.y(vals[i])
			break
		}
	}
	for i := i0; i <= i1; i++ {
		v := vals[i]
		if math.IsNaN(v) {
			if breaks {
				prev = -1
				flush()
			}
			continue
		}
		x, y := cv.x(i+off), cv.y(v)
		c, ok := plotColor(ind, k, s, j, i)
		if prev >= 0 && ok && (!breaks || prev == i-1) {
			if !open || c != cur {
				flush()
				path.MoveTo(px, py)
				cur, open = c, true
			}
			if step {
				path.LineTo(x, py)
			}
			path.LineTo(x, y)
		} else {
			flush()
		}
		if !ok {
			flush()
		}
		prev, px, py = i, x, y
	}
	flush()
	if i1 >= 0 && i0 == i1 && !math.IsNaN(vals[i0]) { // a lone value: a dot
		if c, ok := plotColor(ind, k, s, j, i0); ok {
			var dot ui.Path
			cv.p.FillPath(dot.Circle(cv.x(i0+off), cv.y(vals[i0]), w), c)
		}
	}
}

func (a *application) fillArea(cv canvas, ind *indicator, k *indKind, s *series, j int, pl pine.Plot, i0, i1 int, base float32) {
	vals, off := s.out[j], plotShift(s, j)
	for i := max(i0, 1); i <= i1; i++ {
		v, u := vals[i], vals[i-1]
		c, ok := plotColor(ind, k, s, j, i)
		if math.IsNaN(v) || math.IsNaN(u) || !ok {
			continue
		}
		var path ui.Path
		path.MoveTo(cv.x(i-1+off), cv.y(u)).LineTo(cv.x(i+off), cv.y(v)).LineTo(cv.x(i+off), base).LineTo(cv.x(i-1+off), base)
		cv.p.FillPath(path.Close(), c.Alpha(float32(c.A)/255*0.25))
	}
}

// drawFill shades between two plots, or two levels, in the colors the
// script gives each bar.
func (a *application) drawFill(cv canvas, s *series, sc *pine.Script, j int, f pine.Fill) {
	vm := s.vm
	cols := vm.FillColors[j]
	if f.Levels {
		ya, yb := vm.HlinePrice[f.A], vm.HlinePrice[f.B]
		if math.IsNaN(ya) || math.IsNaN(yb) {
			return
		}
		for i := len(cols) - 1; i >= 0; i-- { // its color now
			if c, ok := toColor(cols[i]); ok {
				y0, y1 := cv.y(ya), cv.y(yb)
				cv.p.Fill(ui.Rect{X: cv.r.X, Y: min(y0, y1), W: cv.r.W, H: float32(math.Abs(float64(y1 - y0)))}, c, 0)
				return
			}
			if !math.IsNaN(cols[i]) {
				return
			}
		}
		return
	}
	if f.A >= len(vm.Plots) || f.B >= len(vm.Plots) {
		return
	}
	top, bottom := vm.Plots[f.A], vm.Plots[f.B]
	off := vm.PlotOffset[f.A]
	i0, i1 := barSpan(len(top), cv.from, cv.to, off)
	// Runs of bars with both values and one color, each a shape.
	start := -1
	var cur ui.Color
	flush := func(end int) {
		if start < 0 || end <= start {
			start = -1
			return
		}
		var path ui.Path
		path.MoveTo(cv.x(start+off), cv.y(top[start]))
		for i := start + 1; i <= end; i++ {
			path.LineTo(cv.x(i+off), cv.y(top[i]))
		}
		for i := end; i >= start; i-- {
			path.LineTo(cv.x(i+off), cv.y(bottom[i]))
		}
		cv.p.FillPath(path.Close(), cur)
		start = -1
	}
	for i := i0; i <= i1; i++ {
		c, ok := toColor(cols[i])
		has := !math.IsNaN(top[i]) && !math.IsNaN(bottom[i])
		switch {
		case !has:
			flush(i - 1)
		case !ok:
			flush(i)
		case start >= 0 && c != cur:
			flush(i)
			start, cur = i-1, c
			if math.IsNaN(top[i-1]) || math.IsNaN(bottom[i-1]) {
				start = i
			}
		case start < 0:
			start, cur = i, c
		}
	}
	flush(i1)
}

var shapeSizes = []float32{9, 6, 9, 13, 18, 26}

// drawShapes marks the bars where shape j's series holds.
func drawShapes(cv canvas, s *series, j int, sh pine.Shape) {
	vm, p := s.vm, cv.p
	vals, cols, off := vm.ShapeVals[j], vm.ShapeCols[j], vm.ShapeOff[j]
	sz := shapeSizes[min(max(sh.Size, 0), len(shapeSizes)-1)]
	textCol, _ := toColor(float64(sh.TextColor))
	i0, i1 := barSpan(len(vals), cv.from, cv.to, off)
	for i := i0; i <= i1; i++ {
		v := vals[i]
		show := pine.Truth(v)
		if sh.Arrow {
			show = !math.IsNaN(v) && v != 0
		}
		if !show {
			continue
		}
		c, ok := toColor(cols[i])
		if !ok {
			continue
		}
		xc := cv.x(i + off)
		loc, style := sh.Location, sh.Style
		if sh.Arrow {
			loc, style = pine.LocBelowBar, pine.ShapeArrowUp
			if v < 0 {
				loc, style = pine.LocAboveBar, pine.ShapeArrowDown
			}
		}
		if !cv.main && (loc == pine.LocAboveBar || loc == pine.LocBelowBar) {
			loc = map[int]int{pine.LocAboveBar: pine.LocTop, pine.LocBelowBar: pine.LocBottom}[loc]
		}
		var cy float32
		bar := i + off
		switch loc {
		case pine.LocAboveBar:
			cy = cv.y(barHigh(s, bar)) - 6 - sz/2
		case pine.LocBelowBar:
			cy = cv.y(barLow(s, bar)) + 6 + sz/2
		case pine.LocTop:
			cy = cv.r.Y + 6 + sz/2
		case pine.LocBottom:
			cy = cv.r.Y + cv.r.H - 6 - sz/2
		default:
			cy = cv.y(v)
		}
		if math.IsNaN(float64(cy)) {
			continue
		}
		drawShape(p, style, sh.Char, xc, cy, sz, c, textCol, sh.Text)
		if sh.Text != "" && style != pine.ShapeLabelUp && style != pine.ShapeLabelDown {
			ty := cy - sz/2 - 8
			if loc == pine.LocBelowBar || loc == pine.LocBottom {
				ty = cy + sz/2 + 8
			}
			label(p, xc, ty, span(sh.Text, 11, textCol, 0), alignCenter)
		}
	}
}

// barHigh and barLow are bar i's high and low, past the newest the newest's.
func barHigh(s *series, i int) float64 { return s.barAt(i).H }
func barLow(s *series, i int) float64  { return s.barAt(i).L }

func drawShape(p *ui.Painter, style int, char string, x, y, sz float32, c, textCol ui.Color, text string) {
	h := sz / 2
	var path ui.Path
	switch style {
	case pine.ShapeTriangleUp, pine.ShapeFlag:
		p.FillPath(path.MoveTo(x, y-h).LineTo(x+h, y+h).LineTo(x-h, y+h).Close(), c)
	case pine.ShapeTriangleDown:
		p.FillPath(path.MoveTo(x-h, y-h).LineTo(x+h, y-h).LineTo(x, y+h).Close(), c)
	case pine.ShapeArrowUp:
		p.FillPath(path.MoveTo(x, y-h).LineTo(x+h*0.8, y).LineTo(x-h*0.8, y).Close(), c)
		p.Line(x, y, x, y+h, max(h*0.45, 1.5), c)
	case pine.ShapeArrowDown:
		p.FillPath(path.MoveTo(x, y+h).LineTo(x+h*0.8, y).LineTo(x-h*0.8, y).Close(), c)
		p.Line(x, y-h, x, y, max(h*0.45, 1.5), c)
	case pine.ShapeCircle:
		p.FillPath(path.Circle(x, y, h), c)
	case pine.ShapeSquare:
		p.Fill(ui.Rect{X: x - h, Y: y - h, W: sz, H: sz}, c, 1)
	case pine.ShapeDiamond:
		p.FillPath(path.MoveTo(x, y-h).LineTo(x+h, y).LineTo(x, y+h).LineTo(x-h, y).Close(), c)
	case pine.ShapeCross:
		p.Line(x-h, y, x+h, y, max(sz/6, 1.5), c)
		p.Line(x, y-h, x, y+h, max(sz/6, 1.5), c)
	case pine.ShapeLabelUp, pine.ShapeLabelDown:
		sp := span(text, 11, textCol, 600)
		w, th := p.MeasureText(0, sp)
		bw, bh := max(w+10, sz), th+6
		top := y - bh/2
		p.Fill(ui.Rect{X: x - bw/2, Y: top, W: bw, H: bh}, c, 3)
		if style == pine.ShapeLabelUp {
			p.FillPath(path.MoveTo(x, top-4).LineTo(x+4, top).LineTo(x-4, top).Close(), c)
		} else {
			p.FillPath(path.MoveTo(x, top+bh+4).LineTo(x+4, top+bh).LineTo(x-4, top+bh).Close(), c)
		}
		if text != "" {
			label(p, x, y, sp, alignCenter)
		}
	case pine.ShapeChar:
		label(p, x, y, span(char, sz+3, c, 0), alignCenter)
	default: // xcross
		p.Line(x-h, y-h, x+h, y+h, max(sz/6, 1.5), c)
		p.Line(x-h, y+h, x+h, y-h, max(sz/6, 1.5), c)
	}
}
