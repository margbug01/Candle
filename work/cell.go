package main

import (
	"context"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// A cell is one chart of the layout: a market in bars of an interval, how
// it is scrolled and scaled, the drawing being drawn on it and its
// indicators. The application embeds the cell being built, drawn or
// handled, so that the chart's code reads a.bars and a.view whichever cell
// it is; between frames that is the active one, which the toolbar, the
// watchlist and the keys act on.
type cell struct {
	// The chart. gen counts the symbol and interval switches: an answer for
	// an earlier one arrives with an older gen and is dropped.
	symbol, interval string
	bars             []market.Bar
	view             view
	plotW, plotH     float32 // the candles' pane
	boxH             float32 // the whole chart, the time axis too
	gen              int
	ctx              context.Context
	stop             context.CancelFunc
	share            *sharedFeed // the feed bringing its bars
	loading          bool        // the first page is on its way
	older            bool        // a page further back is on its way
	atStart          bool        // the market's first bar is loaded
	olderFailed      time.Time
	err              string
	stream           int
	drag             int // dragNone, dragChart…
	dragX, dragY     float32
	dragPane         int64  // the indicator whose pane, or its price axis, is dragged
	sep              int    // the pane whose top line is dragged
	scale            prices // the price axis as set: automatic unless scaled by hand
	shown            prices // the prices the last frame showed
	axis             axis   // how the last frame laid them out
	scaleMode        string // the scale the prices were set by hand on
	homeShown        float32
	funding          market.Funding // the perpetual on the chart's, once in
	fundingOf        string         // whose funding is in funding

	// The drawing being drawn and the point it is at, the one selected and
	// the point of it being dragged.
	creating       *drawing
	stage          int
	sel            int64
	handle         int
	pressX, pressY float32
	dragged        bool
	orig           []point
	editText       bool
	textPick       choice[string]
	textBox        map[int64]ui.Rect // texts as the last frame drew them
	trashArmed     time.Time
	barFor         int64 // the drawing the bar showed last frame
	pricePick      choice[float64]
	accountPick    choice[float64]
	riskPick       choice[float64]
	undo, redo     [][]drawing // the market's drawings before each change, and after each undo
	lastEdit       string      // the key of the last remembered change
	dragBefore     []drawing   // the drawings as a drag of one began

	// The indicators, each chart its own, and their values and panes.
	indicators []indicator
	series     map[int64]*series
	paneViews  map[int64]*paneView // the indicators' panes' prices
}

func newCell(cs chartSettings, st style) *cell {
	return &cell{
		symbol: cs.Symbol, interval: cs.Interval, indicators: cs.Indicators,
		view: defaultView(), scaleMode: st.Scale, homeShown: float32(st.RightBars),
		series: map[int64]*series{}, paneViews: map[int64]*paneView{}, textBox: map[int64]ui.Rect{},
	}
}

// saved is what the settings keep of the cell.
func (c *cell) saved() chartSettings {
	return chartSettings{Symbol: c.symbol, Interval: c.interval, Indicators: c.indicators}
}

// on runs fn with c as the cell, as for building, drawing or handling it.
// After it the cell is the one before, or the active one if that was: fn
// may have made another active.
func (a *application) on(c *cell, fn func()) {
	was, active := a.cell, a.activeCell()
	a.cell = c
	defer func() {
		if was == active {
			was = a.activeCell()
		}
		a.cell = was
	}()
	fn()
}

// setActive makes cell i the one the toolbar, the watchlist and the keys
// act on. What it was doing on the one before, a drawing being drawn, ends.
func (a *application) setActive(i int) {
	if i == a.active || i < 0 || i >= len(a.cells) {
		return
	}
	a.on(a.activeCell(), func() { a.creating, a.drag = nil, dragNone })
	if a.cell == a.activeCell() {
		a.cell = a.cells[i]
	}
	a.active = i
	a.save()
}

// onCell is on for fn reporting back, as an input handler does.
func (a *application) onCell(c *cell, fn func() bool) (ok bool) {
	a.on(c, func() { ok = fn() })
	return ok
}

// afterBuild runs fn on the cell being built once the frame is built.
func (a *application) afterBuild(fn func()) {
	c := a.cell
	a.later = append(a.later, func() { a.on(c, fn) })
}

// activeCell is the chart the toolbar, the watchlist and the keys act on.
func (a *application) activeCell() *cell { return a.cells[a.active] }

// A sharedFeed loads a market's bars in an interval and follows them, for
// every cell showing them: two cells on BTC's hours ask Binance once.
type sharedFeed struct {
	key   string
	stop  context.CancelFunc
	cells []*cell
}

// feedKey tells feeds apart: the market, the interval and, for a future,
// whether in the regular hours only.
func (a *application) feedKey(sym, iv string) string {
	key := sym + " " + iv
	if in, _ := market.InstrumentOf(sym); in.Future && a.set.FuturesRTH {
		key += " rth"
	}
	return key
}

// each runs fn on each cell f feeds, as answers for it come in.
func (a *application) each(f *sharedFeed, fn func()) {
	for _, c := range slices.Clone(f.cells) {
		a.on(c, fn)
	}
}

// leaveFeed stops the cell's feed feeding it, and the feed itself when no
// other cell takes it.
func (a *application) leaveFeed() {
	f := a.share
	if f == nil {
		return
	}
	a.share = nil
	f.cells = slices.DeleteFunc(f.cells, func(c *cell) bool { return c == a.cell })
	if len(f.cells) == 0 {
		f.stop()
		if a.feeds[f.key] == f {
			delete(a.feeds, f.key)
		}
	}
}

// joinFeed has the cell take the bars another cell already has from a feed
// for its market and interval, and what comes after. It reports false when
// no cell has that feed.
func (a *application) joinFeed(key string) bool {
	f := a.feeds[key]
	if f == nil {
		return false
	}
	f.cells = append(f.cells, a.cell)
	a.share = f
	for _, peer := range f.cells {
		if peer == a.cell || peer.loading {
			continue
		}
		a.bars, a.atStart, a.err, a.stream = slices.Clone(peer.bars), peer.atStart, peer.err, peer.stream
		a.funding, a.fundingOf = peer.funding, peer.fundingOf
		a.loading = false
		break
	}
	return true
}
