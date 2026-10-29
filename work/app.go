package main

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// The stream's state, for the toolbar.
const (
	connecting = iota
	live
	reconnecting
)

// What the pointer drags.
const (
	dragNone = iota
	dragChart
	dragAxis     // the price axis
	dragCreate   // a drawing being drawn
	dragDrawing  // a drawing, or one of its points
	dragSep      // the line above an indicator's pane
	dragPaneAxis // an indicator's pane's price axis
)

// application is the window's state. Everything here belongs to the main
// thread: background work hands its results over with post.
type application struct {
	bin   market.Binance
	post  func(func()) // runs fn on the main thread
	spawn func(func()) // starts background work; the tests drop it
	file  string
	set   settings

	// The chart. gen counts the symbol and interval switches: an answer for an
	// earlier one arrives with an older gen and is dropped.
	symbol, interval string
	bars             []market.Bar
	view             view
	plotW, plotH     float32 // the candles' pane
	boxH             float32 // the whole chart, the time axis too
	gen              int
	ctx              context.Context
	stop             context.CancelFunc
	loading          bool // the first page is on its way
	older            bool // a page further back is on its way
	atStart          bool // the market's first bar is loaded
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
	clock            int64 // Binance's clock minus this machine's, ms

	// The watchlist and the search.
	infos     map[string]market.Symbol
	quotes    map[string]market.Ticker
	all       []market.Ticker
	allState  int // 0 not asked, 1 loading, 2 loaded, 3 failed
	query     string
	stopWatch context.CancelFunc
	icons     icons // the coins' icons

	// The drawings: the tool in hand, the drawing being drawn and the point
	// it is at, the one selected and the point of it being dragged.
	tool           string
	creating       *drawing
	stage          int
	sel            int64
	handle         int
	pressX, pressY float32
	dragged        bool
	orig           []point
	hide           bool
	editText       bool
	textPick       choice[string]
	textBox        map[int64]ui.Rect // texts as the last frame drew them
	trashArmed     time.Time
	nextID         int64
	barFor         int64 // the drawing the bar showed last frame
	pricePick      choice[float64]
	accountPick    choice[float64]
	riskPick       choice[float64]
	undo, redo     [][]drawing // the market's drawings before each change, and after each undo
	lastEdit       string      // the key of the last remembered change
	dragBefore     []drawing   // the drawings as a drag of one began
	clip           *drawing    // what Ctrl+C copied, and on which market
	clipSym        string

	// The indicators: their values, the list to add them from and the
	// dialog setting one, with it as it was, for 取消. later is what the
	// frame being built changes once it is built.
	series     map[int64]*series
	paneViews  map[int64]*paneView // the indicators' panes' prices
	indList    bool
	indQuery   string
	indFocus   bool
	indDialog  bool
	indEditing bool
	indEdit    int64
	indBefore  indicator
	later      []func()

	// The settings dialog: open, and the style as it was, for 取消.
	dialog, editing bool
	before          style
	page            int
	picks           map[string]*choice[string]
	colors          map[string]*choice[ui.Color]

	upd updates
}

func newApplication(set settings, file string, bin market.Binance, post func(func())) *application {
	return &application{
		bin: bin, post: post, file: file, set: set,
		spawn:     func(f func()) { go f() },
		view:      defaultView(),
		infos:     map[string]market.Symbol{},
		quotes:    map[string]market.Ticker{},
		picks:     map[string]*choice[string]{},
		colors:    map[string]*choice[ui.Color]{},
		series:    map[int64]*series{},
		paneViews: map[int64]*paneView{},
		scaleMode: set.Chart.Scale, homeShown: float32(set.Chart.RightBars),
		textBox: map[int64]ui.Rect{},
		nextID:  maxDrawingID(set.Drawings),
		icons:   newIcons("", bin.Icons, bin.Icon),
	}
}

func maxDrawingID(all map[string][]drawing) (id int64) {
	for _, ds := range all {
		for _, d := range ds {
			id = max(id, d.ID)
		}
	}
	return id
}

func (a *application) start() {
	a.open(a.set.Symbol, a.set.Interval)
	a.watchQuotes()
	a.loadAll()
	a.loadIcons(false)
	a.spawn(a.syncClock)
	a.watchUpdates()
}

// save writes the settings. While the settings dialog is open the chart is
// saved as it was: what the dialog shows is 确认's to keep.
func (a *application) save() {
	s := a.set
	if a.editing {
		s.Chart = a.before
	}
	saveSettings(a.file, s)
}

// open shows sym in bars of iv, keeping the zoom.
func (a *application) open(sym, iv string) {
	if a.stop != nil {
		a.stop()
	}
	a.gen++
	a.ctx, a.stop = context.WithCancel(context.Background())
	if sym != a.symbol { // undo stays with the market it was done on
		a.undo, a.redo, a.lastEdit = nil, nil, ""
	}
	a.symbol, a.interval = sym, iv
	a.bars, a.loading, a.older, a.atStart, a.err, a.stream, a.drag = nil, true, false, false, "", connecting, dragNone
	a.scale = prices{}
	clear(a.paneViews)
	a.sel, a.creating = 0, nil
	a.olderFailed = time.Time{}
	a.view.offset = a.home()
	a.set.Symbol, a.set.Interval = sym, iv
	a.save()
	ctx, gen := a.ctx, a.gen
	_, known := a.infos[sym]
	a.spawn(func() { a.feed(ctx, gen, sym, iv, !known) })
}

// feed loads the newest page, then follows the live bar, reconnecting with a
// growing pause and filling in what it missed meanwhile.
func (a *application) feed(ctx context.Context, gen int, sym, iv string, needInfo bool) {
	if needInfo {
		go a.lookup(ctx, []string{sym})
	}
	bars, err := a.bin.Klines(ctx, sym, iv, 0, 1000)
	if ctx.Err() != nil {
		return
	}
	a.post(func() {
		if gen != a.gen {
			return
		}
		a.loading = false
		if err != nil {
			a.err = shorten(err.Error(), 160)
			return
		}
		a.bars, a.atStart = bars, len(bars) < 1000
	})
	if err != nil {
		return
	}
	pause := time.Second
	for {
		heard := false
		a.bin.Stream(ctx, sym, iv, func(bar market.Bar, _ bool, _ int64) {
			heard = true
			a.post(func() {
				if gen == a.gen {
					a.stream = live
					a.addBars([]market.Bar{bar})
				}
			})
		})
		if ctx.Err() != nil {
			return
		}
		if heard {
			pause = time.Second
		}
		a.post(func() {
			if gen == a.gen {
				a.stream = reconnecting
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		pause = min(pause*2, 30*time.Second)
		if got, err := a.bin.Klines(ctx, sym, iv, 0, 1000); err == nil && len(got) > 0 {
			a.post(func() {
				if gen != a.gen {
					return
				}
				// Away longer than a page: start over rather than leave a hole.
				if n := len(a.bars); n == 0 || got[0].T > a.bars[n-1].T {
					a.bars, a.atStart, a.view.offset = got, len(got) < 1000, a.home()
					return
				}
				a.addBars(got)
			})
		}
	}
}

// addBars merges bars from the stream or a refetch. Looking at the past, the
// view stays put; looking at the newest bar, it follows new ones.
func (a *application) addBars(got []market.Bar) {
	var added int
	a.bars, added = merge(a.bars, got)
	if a.view.offset < 0 {
		a.view.offset -= float32(added)
	}
}

// manualScale fixes the price axis at the prices on screen, for scaling or
// moving them by hand; false before a frame has shown any.
func (a *application) manualScale() bool { return holdPrices(&a.scale, a.shown) }

// holdPrices fixes sc, unless set by hand already, at shown, the prices on
// screen; false before a frame has shown any.
func holdPrices(sc *prices, shown prices) bool {
	if !sc.manual {
		if shown.hi <= shown.lo {
			return false
		}
		*sc = prices{manual: true, lo: shown.lo, hi: shown.hi}
	}
	return true
}

// loadOlder fetches the page before the first bar once the view nears it.
func (a *application) loadOlder() {
	n := len(a.bars)
	if n == 0 || a.loading || a.older || a.atStart || a.plotW <= 0 || time.Since(a.olderFailed) < 5*time.Second {
		return
	}
	if from, _ := a.view.span(a.plotW, n); from > 200 {
		return
	}
	a.older = true
	ctx, gen, sym, iv, end := a.ctx, a.gen, a.symbol, a.interval, a.bars[0].T-1
	a.spawn(func() {
		got, err := a.bin.Klines(ctx, sym, iv, end, 1000)
		a.post(func() {
			if gen != a.gen {
				return
			}
			a.older = false
			if err != nil {
				a.olderFailed = time.Now()
				return
			}
			a.atStart = len(got) < 1000
			if len(a.bars) > 0 {
				got = got[:sort.Search(len(got), func(i int) bool { return got[i].T >= a.bars[0].T })]
			}
			a.bars = append(got, a.bars...)
		})
	})
}

// watchQuotes follows the watchlist's prices, restarting when it changes.
// Updates are passed on at most twice a second: each one rebuilds the window.
func (a *application) watchQuotes() {
	if a.stopWatch != nil {
		a.stopWatch()
	}
	list := slices.Clone(a.set.Watch)
	if len(list) == 0 {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	a.stopWatch = stop
	var missing []string
	for _, s := range list {
		if _, ok := a.infos[s]; !ok {
			missing = append(missing, s)
		}
	}
	a.spawn(func() {
		if len(missing) > 0 {
			a.lookup(ctx, missing)
		}
		pause := time.Second
		for {
			heard := false
			pending, sent := map[string]market.Ticker{}, time.Time{}
			a.bin.Watch(ctx, list, func(t market.Ticker) {
				heard = true
				pending[t.Symbol] = t
				if time.Since(sent) >= 500*time.Millisecond {
					batch := pending
					pending, sent = map[string]market.Ticker{}, time.Now()
					a.post(func() { maps.Copy(a.quotes, batch) })
				}
			})
			if heard {
				pause = time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}
			pause = min(pause*2, 30*time.Second)
		}
	})
}

// lookup fetches names and price steps. One delisted name fails a batch, so
// then it asks one by one.
func (a *application) lookup(ctx context.Context, names []string) {
	syms, err := a.bin.Symbols(ctx, names...)
	if err != nil && len(names) > 1 {
		for _, n := range names {
			if s, err := a.bin.Symbols(ctx, n); err == nil {
				syms = append(syms, s...)
			}
		}
	}
	a.post(func() {
		for _, s := range syms {
			a.infos[s.Name] = s
		}
	})
}

// loadAll fetches every market once, for the search.
func (a *application) loadAll() {
	if a.allState == 1 || a.allState == 2 {
		return
	}
	a.allState = 1
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		all, err := a.bin.Tickers(ctx)
		a.post(func() {
			if err != nil {
				a.allState = 3
				return
			}
			a.all, a.allState = all, 2
		})
	})
}

// syncClock measures Binance's clock against ours, for the bar countdown.
func (a *application) syncClock() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t0 := time.Now()
	server, err := a.bin.ServerTime(ctx)
	if err != nil {
		return
	}
	off := server - t0.Add(time.Since(t0)/2).UnixMilli()
	a.post(func() { a.clock = off })
}

// pick shows sym and keeps it on the watchlist, at the top when it is new.
func (a *application) pick(sym string) {
	a.query = ""
	if !slices.Contains(a.set.Watch, sym) {
		a.set.Watch = slices.Insert(slices.Clone(a.set.Watch), 0, sym)
		a.save()
		a.watchQuotes()
	}
	if sym != a.symbol {
		a.open(sym, a.interval)
	}
}

func (a *application) unwatch(sym string) {
	a.set.Watch = slices.DeleteFunc(slices.Clone(a.set.Watch), func(s string) bool { return s == sym })
	a.save()
	a.watchQuotes()
}

// names splits a market's name into what it trades and what it is priced in,
// guessing from common quote assets until Binance says.
func (a *application) names(sym string) (base, quote string) {
	if s, ok := a.infos[sym]; ok && s.Base != "" {
		return s.Base, s.Quote
	}
	for _, q := range []string{"USDT", "USDC", "FDUSD", "BTC", "ETH", "BNB"} {
		if b, ok := strings.CutSuffix(sym, q); ok && b != "" {
			return b, q
		}
	}
	return sym, ""
}

// priceDecimals is how many decimals sym's prices show; sample stands in until
// its step is known.
func (a *application) priceDecimals(sym string, sample float64) int {
	if s, ok := a.infos[sym]; ok && s.Tick > 0 {
		return decimals(s.Tick)
	}
	return autoDecimals(sample)
}

// home is where the newest bar rests: as many bars from the right edge as
// the settings leave room for.
func (a *application) home() float32 { return float32(a.set.Chart.RightBars) }

// chartDecimals is how many decimals the chart's prices show: as the settings
// say, else the market's step.
func (a *application) chartDecimals(sample float64) int {
	if d := a.set.Chart.Precision; d >= 0 {
		return d
	}
	return a.priceDecimals(a.symbol, sample)
}

func (a *application) intervalMs() int64 {
	iv, _ := intervalOf(a.interval)
	return iv.ms
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
