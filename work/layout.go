package main

import (
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// The layouts, TradingView's: up to eight charts in a window, split into
// rows and columns as its Select Layout menu has them, by its names
// (LayoutType in its Advanced Charts' documentation) and with the charts in
// its order. A layout with fewer charts hides the rest, which come back as
// they were with one with more.

const (
	maxCells = 8
	splitGap = 4   // DIPs between charts, the line between them in the middle
	minCellW = 160 // what a chart keeps at least across, dragging the lines
	minCellH = 110 // and down
)

// syncSettings are what the layout's charts follow of the active one, as
// TradingView's Sync in layout: its market, its bars, where the pointer is
// (the crosshair) and the time in sight.
type syncSettings struct {
	Symbol, Interval, Crosshair, Time bool
}

// crossPoint is where the crosshair is: on which chart, at what bar's time
// and price, for the other charts to show it.
type crossPoint struct {
	from  *cell
	t     int64
	price float64
}

// A layoutKind is a layout: its name, and how it splits the chart area, as
// a tree of rows (r) and columns (c) of charts by their number:
// "c(0,r(1,2))" is the first chart on the left, the second and third
// stacked on the right.
type layoutKind struct {
	id, tree string
}

// layoutKinds are the layouts by how many charts they show, in the order
// TradingView's menu has them.
var layoutKinds = [][]layoutKind{
	{{"s", "0"}},
	{{"2h", "c(0,1)"}, {"2v", "r(0,1)"}},
	{{"3h", "c(0,1,2)"}, {"3s", "c(0,r(1,2))"}, {"3r", "c(r(0,1),2)"}, {"2-1", "r(c(0,2),1)"}, {"1-2", "r(0,c(1,2))"}, {"3v", "r(0,1,2)"}},
	{{"4", "r(c(0,2),c(1,3))"}, {"4h", "c(0,1,2,3)"}, {"1-3", "r(0,c(1,2,3))"}, {"3-1", "r(c(0,1,2),3)"},
		{"4s", "c(0,r(1,2,3))"}, {"4s-l", "c(r(1,2,3),0)"}, {"2-2-l", "c(0,1,r(2,3))"}, {"2-2-r", "c(r(0,1),2,3)"},
		{"2-2", "r(c(0,1),2,3)"}, {"4v", "r(0,1,2,3)"}},
	{{"1-4", "r(0,c(1,2,3,4))"}, {"5h", "c(0,1,2,3,4)"}, {"4-1", "r(c(0,1,2,3),4)"}, {"2-3", "r(c(0,1),c(2,3,4))"},
		{"3-2", "r(c(0,1,2),c(3,4))"}, {"5s", "c(0,r(1,2,3,4))"}, {"5s-l", "c(r(1,2,3,4),0)"}, {"2-3-l", "c(0,1,r(2,3,4))"},
		{"2-3-r", "c(r(0,1,2),3,4)"}, {"5v", "r(0,1,2,3,4)"}},
	{{"6", "r(c(0,2,4),c(1,3,5))"}, {"6h", "c(0,1,2,3,4,5)"}, {"6c", "r(c(0,1),c(2,3),c(4,5))"},
		{"2-4", "r(c(0,1),c(2,3,4,5))"}, {"4-2", "r(c(0,1,2,3),c(4,5))"}, {"6v", "r(0,1,2,3,4,5)"}},
	{{"4-3", "r(c(0,1,2,3),c(4,5,6))"}, {"7h", "c(0,1,2,3,4,5,6)"}, {"7s", "c(0,r(1,2,3,4,5,6))"}},
	{{"8", "r(c(0,2,4,6),c(1,3,5,7))"}, {"8c", "r(c(0,1),c(2,3),c(4,5),c(6,7))"}, {"8h", "c(0,1,2,3,4,5,6,7)"}, {"8v", "r(0,1,2,3,4,5,6,7)"}},
}

// A split is a node of a layout's tree: a chart, or charts side by side
// (across) or stacked. Its node is its place among the layout's splits
// that hold others, depth first, for the sizes the settings keep.
type split struct {
	chart  int
	across bool
	kids   []*split
	node   int
}

// layoutOf is the layout named id, and how many charts it shows; one chart
// for a name it does not know.
func layoutOf(id string) (*split, int) {
	for n, kinds := range layoutKinds {
		for _, k := range kinds {
			if k.id == id {
				nodes := 0
				root, _ := parseSplit(k.tree, &nodes)
				return root, n + 1
			}
		}
	}
	return &split{}, 1
}

// parseSplit reads a tree as layoutKinds writes it, numbering the splits
// that hold others from *nodes on; it returns what is left of s.
func parseSplit(s string, nodes *int) (*split, string) {
	if s[0] == 'c' || s[0] == 'r' {
		sp := &split{across: s[0] == 'c', node: *nodes}
		*nodes++
		s = s[2:] // past "c("
		for {
			var kid *split
			kid, s = parseSplit(s, nodes)
			sp.kids = append(sp.kids, kid)
			sep := s[0]
			s = s[1:]
			if sep == ')' {
				return sp, s
			}
		}
	}
	end := strings.IndexAny(s, ",)")
	if end < 0 {
		end = len(s)
	}
	n, _ := strconv.Atoi(s[:end])
	return &split{chart: n}, s[end:]
}

// shownCells is how many charts the layout shows.
func (a *application) shownCells() int {
	_, n := layoutOf(a.set.Layout)
	return min(n, len(a.cells))
}

// charts builds the layout's charts, or the one maximized. The pointer
// going down on one, on the chart, its legend or its buttons, makes it
// active: down outside every chart but one, it went down on that one.
func (a *application) charts(c *ui.Context, t *ui.Theme, k palette, area ui.Element) {
	root, n := layoutOf(a.set.Layout)
	n = min(n, len(a.cells))
	a.findCross(area, n)
	inside, pressed := -1, 0
	leaf := func(i int, box ui.Element) {
		if a.cellBox(c, t, k, i, box) {
			pressed++
		} else {
			inside = i
		}
	}
	if a.maxed >= 0 && a.maxed < n {
		leaf(a.maxed, splitBox(c, &split{chart: a.maxed}).FillWidth().FillHeight())
		return
	}
	a.maxed = -1
	a.splitView(c, k, root, nil, leaf)
	if n > 1 && pressed == n-1 {
		a.setActive(inside)
	}
}

// findCross finds the chart under the pointer, and the bar and the price
// there, as the last frame laid the charts out, before any is built: they
// all show it, built before it or after.
func (a *application) findCross(area ui.Element, n int) {
	a.cross = crossPoint{}
	px, py, over := area.PointerPosition()
	if !over {
		return
	}
	b := area.Bounds()
	x, y := b.X+px, b.Y+py
	for _, c := range a.cells[:n] {
		r := c.bounds
		if a.maxed >= 0 && c != a.cells[a.maxed] || x < r.X || y < r.Y || x >= r.X+r.W || y >= r.Y+r.H {
			continue
		}
		a.on(c, func() {
			hx, hy := x-r.X, y-r.Y
			if len(a.bars) == 0 || hx >= a.plotW || hy >= a.boxH-timeH {
				return
			}
			price := math.NaN()
			if hy < a.plotH {
				price = a.shown.at(hy, a.axis)
			}
			a.cross = crossPoint{from: c, t: a.bars[a.hoverBar(hx, hy, true)].T, price: price}
		})
	}
}

// syncedBar is the bar on this chart at the time of the crosshair on
// another, when the layout syncs the crosshair.
func (a *application) syncedBar() (int, bool) {
	if !a.set.Sync.Crosshair || a.cross.from == nil || a.cross.from == a.cell {
		return 0, false
	}
	return barAt(a.bars, a.cross.t, a.intervalMs())
}

// legendBar is the bar the legend reads out: the one under the crosshair,
// on this chart or, synced, on another, else the newest.
func (a *application) legendBar(hx, hy float32, over bool) int {
	if i, ok := a.syncedBar(); ok && !over {
		return i
	}
	return a.hoverBar(hx, hy, over)
}

// barAt is the bar holding time t: the last to open at t or before, not
// the newest once it has closed.
func barAt(bars []market.Bar, t, ivMs int64) (int, bool) {
	n := len(bars)
	i := sort.Search(n, func(j int) bool { return bars[j].T > t }) - 1
	if i < 0 || i == n-1 && t >= bars[i].T+ivMs {
		return 0, false
	}
	return i, true
}

// timeAt is the time at a bar, fractional: as far between two bars as it
// is, before the first or after the newest as many bars' lengths.
func timeAt(bars []market.Bar, i float32, ivMs int64) int64 {
	n := len(bars)
	switch {
	case i <= 0:
		return bars[0].T + int64(float64(i)*float64(ivMs))
	case i >= float32(n-1):
		return bars[n-1].T + int64(float64(i-float32(n-1))*float64(ivMs))
	}
	j := int(i)
	return bars[j].T + int64(float64(i-float32(j))*float64(bars[j+1].T-bars[j].T))
}

// indexAt is the bar at time t, fractional: timeAt's inverse.
func indexAt(bars []market.Bar, t, ivMs int64) float32 {
	n := len(bars)
	switch {
	case t <= bars[0].T:
		return float32(float64(t-bars[0].T) / float64(ivMs))
	case t >= bars[n-1].T:
		return float32(n-1) + float32(float64(t-bars[n-1].T)/float64(ivMs))
	}
	j := sort.Search(n, func(k int) bool { return bars[k].T > t }) - 1
	return float32(j) + float32(float64(t-bars[j].T)/float64(bars[j+1].T-bars[j].T))
}

// syncTime brings the layout's other charts to the time this one shows,
// as the layout syncs the time: showing its newest bar, they show theirs;
// looking back, they have the time at its right edge at theirs. Those in
// bars as long take its zoom too.
func (a *application) syncTime() {
	n := len(a.bars)
	if !a.set.Sync.Time || a.shownCells() < 2 || n == 0 || a.plotW <= 0 {
		return
	}
	src, v := a.cell, a.view
	v.clamp(a.plotW, n)
	_, to := v.span(a.plotW, n)
	newest := to >= n-1
	edge := timeAt(a.bars, v.right(n), a.intervalMs())
	for _, c := range a.cells[:a.shownCells()] {
		if c == src || len(c.bars) == 0 || c.plotW <= 0 {
			continue
		}
		a.on(c, func() {
			same := a.interval == src.interval
			if same {
				a.view.spacing = v.spacing
			}
			m := len(a.bars)
			switch {
			case same && (newest || a.bars[m-1].T == src.bars[n-1].T):
				a.view.offset = v.offset
			case newest:
				a.view.offset = a.home()
			default:
				a.view.offset = indexAt(a.bars, edge, a.intervalMs()) - float32(m-1)
			}
			a.view.clamp(a.plotW, m)
		})
	}
}

// splitView builds sp, a part of the layout, in box (nil for the whole
// area): its charts by leaf, and the lines between them, which drag.
func (a *application) splitView(c *ui.Context, k palette, sp *split, box *ui.Element, leaf func(int, ui.Element)) {
	if box == nil {
		el := splitBox(c, sp).FillWidth().FillHeight()
		box = &el
	}
	if len(sp.kids) == 0 {
		leaf(sp.chart, *box)
		return
	}
	el := *box
	size := el.Bounds()
	room := size.H
	if sp.across {
		room = size.W
	}
	room -= float32(len(sp.kids)-1) * splitGap
	el.Children(func() {
		w := a.splitSizes(sp)
		for i, kid := range sp.kids {
			if i > 0 {
				a.splitter(c, k, sp, i-1, room)
			}
			kb := splitBox(c, kid).Basis(0).Grow(float32(w[i])).MinWidth(0).MinHeight(0)
			if sp.across {
				kb.FillHeight()
			} else {
				kb.FillWidth()
			}
			a.splitView(c, k, kid, &kb, leaf)
		}
	})
}

// splitBox is the box sp is built in: a row of charts side by side, a
// column of them stacked, or a chart's.
func splitBox(c *ui.Context, sp *split) ui.Element {
	switch {
	case len(sp.kids) > 0 && sp.across:
		return ui.Row(c.Key("split-" + strconv.Itoa(sp.node)))
	case len(sp.kids) > 0:
		return ui.Column(c.Key("split-" + strconv.Itoa(sp.node)))
	}
	return ui.Box(c.Key("cell-" + strconv.Itoa(sp.chart)))
}

// splitSizes are the shares sp's parts take, as dragged, else alike.
func (a *application) splitSizes(sp *split) []float64 {
	if all := a.set.Splits[a.layoutID()]; sp.node < len(all) && len(all[sp.node]) == len(sp.kids) {
		return all[sp.node]
	}
	w := make([]float64, len(sp.kids))
	for i := range w {
		w[i] = 1
	}
	return w
}

// layoutID is the layout's name, "s" for one chart.
func (a *application) layoutID() string {
	if a.set.Layout == "" {
		return "s"
	}
	return a.set.Layout
}

// splitter is the line between parts i and i+1 of sp, room DIPs long
// together: dragged, it moves, the two parts giving and taking the room,
// each keeping minCellW (or minCellH) at least; double clicked, sp's parts
// share alike again.
func (a *application) splitter(c *ui.Context, k palette, sp *split, i int, room float32) {
	key := "splitter-" + strconv.Itoa(sp.node) + "-" + strconv.Itoa(i)
	b := ui.Box(c.Key(key)).Shrink(0)
	if sp.across {
		b.Width(splitGap).FillHeight().Cursor(ui.CursorResizeColumn)
	} else {
		b.Height(splitGap).FillWidth().Cursor(ui.CursorResizeRow)
	}
	hot := b.Hovered() || b.Pressed()
	across := sp.across
	b.Draw(func(p *ui.Painter, r ui.Rect) {
		if hot {
			p.Fill(r, k.accent.Alpha(0.6), 0)
			return
		}
		px := 1 / p.Scale()
		if across {
			x := r.X + (r.W-px)/2
			p.Fill(ui.Rect{X: x, Y: r.Y, W: px, H: r.H}, k.line, 0)
		} else {
			y := r.Y + (r.H-px)/2
			p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: px}, k.line, 0)
		}
	})
	if b.DoubleClicked() {
		a.setSplit(sp, nil)
		a.save()
		return
	}
	dx, dy, ok := b.Dragged()
	d := dy
	if across {
		d = dx
	}
	if ok && d != 0 && room > 0 {
		w := slices.Clone(a.splitSizes(sp))
		sum := 0.0
		for _, v := range w {
			sum += v
		}
		least := float64(minCellH)
		if across {
			least = minCellW
		}
		least = min(least/float64(room)*sum, (w[i]+w[i+1])/2) // a short window: halves at least
		move := float64(d) / float64(room) * sum
		move = min(max(move, least-w[i]), w[i+1]-least)
		w[i] += move
		w[i+1] -= move
		a.setSplit(sp, w)
	}
	switch {
	case b.Pressed():
		a.splitHeld = key
	case a.splitHeld == key: // let go: kept for the next time
		a.splitHeld = ""
		a.save()
	}
}

// setSplit keeps the shares of sp's parts, nil for alike.
func (a *application) setSplit(sp *split, w []float64) {
	id := a.layoutID()
	if a.set.Splits == nil {
		a.set.Splits = map[string][][]float64{}
	}
	all := slices.Clone(a.set.Splits[id])
	for len(all) <= sp.node {
		all = append(all, nil)
	}
	all[sp.node] = w
	a.set.Splits = maps.Clone(a.set.Splits)
	a.set.Splits[id] = all
}

// setLayout shows the charts as layout id has them. Charts it shows that
// were not there show the active chart's market, bars and indicators, as
// TradingView's do; those it hides stop asking for their bars.
func (a *application) setLayout(id string) {
	if id == "s" {
		id = ""
	}
	before := a.shownCells()
	_, n := layoutOf(id)
	for len(a.cells) < n {
		cs := a.activeCell().saved()
		cs.Indicators = slices.Clone(cs.Indicators)
		for j := range cs.Indicators {
			cs.Indicators[j] = cloneIndicator(cs.Indicators[j])
		}
		a.cells = append(a.cells, newCell(cs, a.set.Chart))
	}
	a.set.Layout, a.maxed = id, -1
	for i := n; i < before; i++ {
		a.on(a.cells[i], func() {
			a.park()
			a.bars = nil // loaded again when it shows
		})
	}
	for i := before; i < n; i++ {
		c := a.cells[i]
		a.on(c, func() { a.open(c.symbol, c.interval) })
	}
	if a.active >= n {
		a.setActive(0)
	}
	a.save()
}

// openSynced shows sym in bars of iv on the active chart, and on the
// others too as the layout syncs their markets and their bars.
func (a *application) openSynced(sym, iv string) {
	act := a.activeCell()
	newSym, newIv := sym != act.symbol, iv != act.interval
	a.on(act, func() { a.open(sym, iv) })
	a.syncCharts(newSym, newIv)
}

// syncCharts has the other charts take the active one's market (sym), its
// bars (iv) or both, as the layout syncs them.
func (a *application) syncCharts(sym, iv bool) {
	act := a.activeCell()
	for _, c := range a.cells[:a.shownCells()] {
		s, v := c.symbol, c.interval
		if sym && a.set.Sync.Symbol {
			s = act.symbol
		}
		if iv && a.set.Sync.Interval {
			v = act.interval
		}
		if c != act && (s != c.symbol || v != c.interval) {
			a.on(c, func() { a.open(s, v) })
		}
	}
}

// reopenFutures loads the futures on the charts again, for the trading
// hours as they are set now.
func (a *application) reopenFutures() {
	for _, c := range a.cells[:a.shownCells()] {
		if in, _ := market.InstrumentOf(c.symbol); in.Future {
			a.on(c, func() { a.open(c.symbol, c.interval) })
		}
	}
}

var (
	maximizeIcon = svg("<path d='M2.5 6V2.5H6M10 2.5h3.5V6M13.5 10v3.5H10M6 13.5H2.5V10'/>")
	restoreIcon  = svg("<path d='M6 2.5V6H2.5M13.5 6H10V2.5M10 13.5V10h3.5M2.5 10H6v3.5'/>")
)

// cellFrame is, over chart i of a layout with more than one, the line
// around the active one and, while the pointer is on the chart, the button
// maximizing it, or with it maximized, bringing back the layout.
func (a *application) cellFrame(c *ui.Context, k palette, i int, hovered bool) {
	if a.shownCells() < 2 {
		return
	}
	if i == a.active && a.maxed < 0 {
		ui.Box(c.Key("active-frame")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Border(1, k.accent).PassThrough()
	}
	if !hovered && a.maxed < 0 {
		return
	}
	label, icon := "最大化这个图表", maximizeIcon
	if a.maxed >= 0 {
		label, icon = "还原布局", restoreIcon
	}
	b := ui.ButtonBase(c.Key("maximize")).Absolute().Top(6).Right(axisW+6).Size(24, 24).Center().Radius(4).
		Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(k.hover)
	}
	if b.Children(func() { ui.Icon(c, icon).Size(14, 14).TextColor(k.muted) }).Clicked() {
		a.maximize(i)
	}
}

// maximize shows chart i alone, or with it maximized, the layout again.
func (a *application) maximize(i int) {
	if a.maxed >= 0 {
		a.maxed = -1
		return
	}
	a.maxed = i
	a.setActive(i)
}

// layoutButton, in the top bar, shows the layout and opens the menu of
// them, with what the charts sync.
func (a *application) layoutButton(c *ui.Context, t *ui.Theme, k palette) {
	root, n := layoutOf(a.set.Layout)
	b := ui.ButtonBase(c.Key("layouts")).Height(28).Padding(0, 8).Margin(0, 6, 0, 0).Center().Radius(4).
		Cursor(ui.CursorPointer).Label("选择布局").Tooltip("选择布局：一个窗口看多个图表")
	switch {
	case a.layoutMenu:
		b.Background(k.chosen)
	case b.Hovered():
		b.Background(k.hover)
	}
	b.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			layoutIcon(c, root, t.Text)
			if n > 1 {
				ui.Text(c, strconv.Itoa(n)).FontSize(12).FontWeight(500).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
			}
		})
	})
	if b.Clicked() {
		a.layoutMenu = !a.layoutMenu
	}
	ui.Popover(c, b, &a.layoutMenu, func() { a.layoutChoices(c, t, k) })
}

// layoutChoices are the layout menu: TradingView's layouts by how many
// charts they show, and what the charts sync.
func (a *application) layoutChoices(c *ui.Context, t *ui.Theme, k palette) {
	id := a.layoutID()
	ui.Column(c).Padding(6, 4).Gap(2).Children(func() {
		for n, kinds := range layoutKinds {
			ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, strconv.Itoa(n+1)).FontSize(12).FontFeatures("tnum").TextColor(t.TextMuted).Width(18).TextAlign(ui.Center)
				for _, lk := range kinds {
					root, _ := layoutOf(lk.id)
					chosen := lk.id == id
					b := ui.ButtonBase(c.Key("layout-"+lk.id)).Size(34, 28).Center().Radius(4).Cursor(ui.CursorPointer).
						Label("布局 " + lk.id)
					switch {
					case chosen:
						b.Background(k.chosen)
					case b.Hovered():
						b.Background(k.hover)
					}
					col := t.TextMuted
					if chosen {
						col = k.accent
					}
					b.Children(func() { layoutIcon(c, root, col) })
					if b.Clicked() {
						a.layoutMenu = false
						if !chosen {
							a.afterBuild(func() { a.setLayout(lk.id) })
						}
					}
				}
			})
		}
		ui.Box(c).FillWidth().Height(1).Margin(6, 0).Background(k.line)
		ui.Text(c, "同步（其他图表跟着当前图表）").FontSize(12).TextColor(t.TextMuted).Padding(0, 6, 4, 6)
		ui.Grid(c).Columns(2).Gap(6).Padding(0, 6, 4, 6).Children(func() {
			sync := &a.set.Sync
			for _, o := range []struct {
				on        *bool
				name, tip string
				then      func()
			}{
				{&sync.Symbol, "品种", "换品种时所有图表一起换", func() { a.syncCharts(true, false) }},
				{&sync.Crosshair, "十字线", "鼠标在一个图表上，其他图表在同一时间画竖线", nil},
				{&sync.Interval, "周期", "换周期时所有图表一起换", func() { a.syncCharts(false, true) }},
				{&sync.Time, "时间", "拖动或缩放一个图表，其他图表跟着到同一时间", func() { a.on(a.activeCell(), a.syncTime) }},
			} {
				cb := ui.Checkbox(c.Key("sync-"+o.name), o.on, o.name).Tooltip(o.tip)
				if cb.Changed() {
					a.save()
					if *o.on && o.then != nil {
						a.afterBuild(o.then)
					}
				}
			}
		})
	})
}

// layoutIcon is a layout drawn small, each chart a block.
func layoutIcon(c *ui.Context, root *split, col ui.Color) {
	ui.Box(c).Size(24, 18).Shrink(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		drawSplit(p, r, root, col)
	})
}

func drawSplit(p *ui.Painter, r ui.Rect, sp *split, col ui.Color) {
	if len(sp.kids) == 0 {
		p.Stroke(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, col, 1.5, 1)
		return
	}
	const gap = 1.5
	n := float32(len(sp.kids))
	for i, kid := range sp.kids {
		f := float32(i)
		if sp.across {
			w := (r.W - gap*(n-1)) / n
			drawSplit(p, ui.Rect{X: r.X + f*(w+gap), Y: r.Y, W: w, H: r.H}, kid, col)
		} else {
			h := (r.H - gap*(n-1)) / n
			drawSplit(p, ui.Rect{X: r.X, Y: r.Y + f*(h+gap), W: r.W, H: h}, kid, col)
		}
	}
}

// fitLegend keeps a legend on a chart of a layout of more off the price
// axis: what does not fit is cut off at it.
func (a *application) fitLegend(el ui.Element) ui.Element {
	if a.shownCells() > 1 && a.plotW > 20 {
		el.MaxWidth(a.plotW - 14).ClipX()
	}
	return el
}
