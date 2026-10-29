package main

import (
	"context"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
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
	yahoo market.Yahoo // CME futures and US stocks
	perp  market.Perp  // Binance's perpetuals
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
	infos      map[string]market.Symbol
	quotes     map[string]market.Ticker
	all        []market.Ticker
	allState   int             // 0 not asked, 1 loading, 2 loaded, 3 failed
	perps      []market.Ticker // every perpetual, for the search
	perpState  int             // as allState
	funding    market.Funding  // the perpetual on the chart's, once in
	fundingOf  string          // whose funding is in funding
	searchKind string          // the search's section shown whole, "" for the first few of each
	query      string
	stopWatch  context.CancelFunc
	icons      icons                          // the coins' icons
	usFound    map[string][]market.Instrument // Yahoo's answers for US stocks, by query, until usList is in
	usNames    map[string]string              // the stocks' names, from those answers
	usAsking   string                         // the query being asked
	usList     []market.Instrument            // the US stocks and ETFs, from Nasdaq's list
	usByID     map[string]market.Instrument   // usList by ID
	usLoading  bool
	usFailed   time.Time // when the list last did not come
	usFetch    func(context.Context) ([]byte, error)
	searchGen  int // grows as what the search looks through changes
	watchList  ui.ListState
	found      foundCache
	priced     map[string]time.Time // when the results' prices were asked for

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
		bin: bin, yahoo: market.Yahoo{HTTP: bin.HTTP}, perp: market.Perp{HTTP: bin.HTTP}, post: post, file: file, set: set,
		usFound: map[string][]market.Instrument{}, usNames: map[string]string{}, priced: map[string]time.Time{},
		usFetch:   func(ctx context.Context) ([]byte, error) { return market.USList(ctx, bin.HTTP) },
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
	a.loadPerps()
	a.loadUS()
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
	ctx, gen, feed := a.ctx, a.gen, a.barFeed(sym)
	_, known := a.infos[sym]
	a.spawn(func() { a.feed(ctx, gen, feed, sym, iv, !known) })
	if market.IsPerp(sym) {
		perp := a.perp
		a.spawn(func() { a.followFunding(ctx, gen, perp, sym) })
	}
}

// followFunding asks for the perpetual's mark price and funding every half
// minute while it is on the chart.
func (a *application) followFunding(ctx context.Context, gen int, perp market.Perp, sym string) {
	for {
		f, err := perp.Funding(ctx, sym)
		if err == nil {
			a.post(func() {
				if gen == a.gen {
					a.funding, a.fundingOf = f, sym
				}
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// feed loads the newest page, then follows the live bar, reconnecting with a
// growing pause and filling in what it missed meanwhile.
func (a *application) feed(ctx context.Context, gen int, feed market.Feed, sym, iv string, needInfo bool) {
	if needInfo {
		go a.lookup(ctx, []string{sym})
	}
	bars, err := feed.Klines(ctx, sym, iv, 0, 1000)
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
		feed.Stream(ctx, sym, iv, func(bar market.Bar, _ bool, _ int64) {
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
		if got, err := feed.Klines(ctx, sym, iv, 0, 1000); err == nil && len(got) > 0 {
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
	ctx, gen, sym, iv, end, feed := a.ctx, a.gen, a.symbol, a.interval, a.bars[0].T-1, a.barFeed(a.symbol)
	a.spawn(func() {
		got, err := feed.Klines(ctx, sym, iv, end, 1000)
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

// watchQuotes follows the watchlist's prices, restarting when it changes:
// Binance's pairs on its stream, futures and stocks from Yahoo.
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
	if len(missing) > 0 {
		a.spawn(func() { a.lookup(ctx, missing) })
	}
	for _, group := range byFeed(list) {
		feed := a.feedOf(group[0])
		a.spawn(func() { a.follow(ctx, feed, group) })
	}
}

// follow passes on the quotes of list at most twice a second, each time
// all that came in since (each pass rebuilds the window), reconnecting with
// a growing pause until ctx ends.
func (a *application) follow(ctx context.Context, feed market.Feed, list []string) {
	var mu sync.Mutex
	pending := map[string]market.Ticker{}
	var timer *time.Timer
	send := func() {
		mu.Lock()
		batch := pending
		pending, timer = map[string]market.Ticker{}, nil
		mu.Unlock()
		if len(batch) > 0 {
			a.post(func() { maps.Copy(a.quotes, batch) })
		}
	}
	pause := time.Second
	for {
		heard := false
		feed.Watch(ctx, list, func(t market.Ticker) {
			heard = true
			mu.Lock()
			pending[t.Symbol] = t
			if timer == nil {
				timer = time.AfterFunc(500*time.Millisecond, send)
			}
			mu.Unlock()
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
}

// findings are what the search shows for query: Binance's pairs, the
// futures and the US stocks. Asked every frame, it searches once a query.
func (a *application) findings(query string, limit int) []market.Ticker {
	hits := a.foundFor(query).hits
	return hits[:min(len(hits), limit)]
}

// foundFor is what the search finds for query, all of it best first and by
// section, searched once a query.
func (a *application) foundFor(query string) *foundCache {
	q := strings.TrimSpace(query)
	f := &a.found
	if !f.done || f.q != q || f.gen != a.searchGen {
		pairs := append(slices.Clip(a.all), a.perps...)
		hits := searchAll(q, pairs, market.Futures, a.stocksFor(q), math.MaxInt)
		*f = foundCache{done: true, q: q, gen: a.searchGen, hits: hits, groups: groupHits(hits)}
	}
	return f
}

// firstFinding is what Enter in the search opens: the best of the section
// shown whole, or of all.
func (a *application) firstFinding() string {
	for _, g := range a.foundFor(a.query).groups {
		if a.searchKind == "" || g.sec.key == a.searchKind {
			return g.hits[0].Symbol
		}
	}
	return ""
}

// stocksFor is the US stocks the search looks through: Nasdaq's list, or
// until it is in, the popular ones and what Yahoo found for q.
func (a *application) stocksFor(q string) []market.Instrument {
	if a.usList != nil {
		return a.usList
	}
	return market.WithPopular(slices.Clone(a.usFound[q]))
}

// loadUS reads the list of US stocks from the cache folder or, a week old or
// missing, from Nasdaq, keeping the old one if Nasdaq does not answer.
func (a *application) loadUS() {
	if a.usList != nil || a.usLoading || time.Since(a.usFailed) < iconRetry {
		return
	}
	a.usLoading = true
	file, fetch := "", a.usFetch
	if a.icons.dir != "" {
		file = filepath.Join(a.icons.dir, "us-symbols.txt")
	}
	a.spawn(func() {
		var list []market.Instrument
		fresh := false
		if fi, err := os.Stat(file); file != "" && err == nil {
			if data, err := os.ReadFile(file); err == nil {
				list, _ = market.ParseUSList(data)
				fresh = list != nil && time.Since(fi.ModTime()) < iconListAge
			}
		}
		if !fresh {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			data, err := fetch(ctx)
			cancel()
			if err == nil {
				if got, err := market.ParseUSList(data); err == nil {
					list = got
					if file != "" {
						writeFile(file, data)
					}
				}
			}
		}
		byID := map[string]market.Instrument{}
		for _, in := range list {
			byID[in.ID] = in
		}
		a.post(func() {
			a.usLoading = false
			if list == nil {
				a.usFailed = time.Now()
				return
			}
			a.usList, a.usByID = list, byID
			a.searchGen++
		})
	})
}

// searchUS asks Yahoo for the US stocks query names, a moment after the
// typing stops, while Nasdaq's list is not in. Answers are kept by query.
func (a *application) searchUS(query string) {
	q := strings.TrimSpace(query)
	if q == "" || a.usList != nil || a.usFound[q] != nil || a.usAsking == q {
		return
	}
	a.usAsking = q
	a.spawn(func() {
		time.Sleep(200 * time.Millisecond)
		a.post(func() {
			if strings.TrimSpace(a.query) != q { // typed on: that query asks
				if a.usAsking == q {
					a.usAsking = ""
				}
				return
			}
			a.spawn(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				found, err := a.yahoo.Search(ctx, q)
				a.post(func() {
					if a.usAsking == q {
						a.usAsking = ""
					}
					if err != nil {
						return // asked again when the query changes back
					}
					if len(a.usFound) > 200 {
						clear(a.usFound)
					}
					a.usFound[q] = append([]market.Instrument{}, found...)
					for _, in := range found {
						a.usNames[in.ID] = in.Name
					}
					a.searchGen++
				})
			})
		})
	})
}

// priceHits asks Yahoo for the prices of the futures and stocks among the
// first results that have none, a moment after the typing stops: the results
// show at once, the prices a moment after.
func (a *application) priceHits(q string, hits []market.Ticker) {
	var ids []string
	for _, h := range hits[:min(len(hits), 15)] {
		if _, ok := a.quotes[h.Symbol]; !ok && market.IsYahoo(h.Symbol) && time.Since(a.priced[h.Symbol]) > 30*time.Second {
			ids = append(ids, h.Symbol)
		}
	}
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		a.priced[id] = time.Now()
	}
	a.spawn(func() {
		time.Sleep(150 * time.Millisecond)
		a.post(func() {
			if strings.TrimSpace(a.query) != q { // typed on: ask for these again when they show
				for _, id := range ids {
					delete(a.priced, id)
				}
				return
			}
			a.spawn(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				got, _ := a.yahoo.Quotes(ctx, ids)
				a.post(func() {
					for _, t := range got {
						if _, ok := a.quotes[t.Symbol]; !ok { // the watchlist's own are fresher
							a.quotes[t.Symbol] = t
						}
					}
				})
			})
		})
	})
}

// foundCache is the search's last results, for the query, the generation of
// what it looked through and how many it wanted.
type foundCache struct {
	done   bool
	q      string
	gen    int
	hits   []market.Ticker
	groups []hitGroup
}

// A hitGroup is the search's findings in a section of the watchlist's.
type hitGroup struct {
	sec  watchSection
	hits []market.Ticker // best first
}

// groupHits parts hits, best first, by section, the section of the best
// first: typing btc puts the coins first, nvda the stocks.
func groupHits(hits []market.Ticker) []hitGroup {
	var out []hitGroup
	at := map[string]int{}
	for _, h := range hits {
		key := sectionOf(h.Symbol)
		i, ok := at[key]
		if !ok {
			i, at[key] = len(out), len(out)
			out = append(out, hitGroup{sec: sections[slices.IndexFunc(sections, func(s watchSection) bool { return s.key == key })]})
		}
		out[i].hits = append(out[i].hits, h)
	}
	return out
}

// popularByID are the popular US markets by ID, for their names before
// Nasdaq's list is in.
var popularByID = func() map[string]market.Instrument {
	m := map[string]market.Instrument{}
	for _, in := range market.PopularOnly() {
		m[in.ID] = in
	}
	return m
}()

// feedOf is where sym's bars come from: Yahoo for "CME:" and "US:" names,
// Binance for the rest.
func (a *application) feedOf(sym string) market.Feed {
	switch {
	case market.IsYahoo(sym):
		return a.yahoo
	case market.IsPerp(sym):
		return a.perp
	}
	return a.bin
}

// barFeed is where sym's bars come from, as the chart shows them: a
// future's in the regular trading hours only when the settings say. It reads
// the settings, so it is called on the main thread and the feed handed on.
func (a *application) barFeed(sym string) market.Feed {
	if market.IsYahoo(sym) {
		y := a.yahoo
		y.RTH = a.set.FuturesRTH
		return y
	}
	return a.feedOf(sym)
}

// rth is whether the chart shows a future's regular trading hours only: the
// setting is on, and the bars are shorter than a day.
func (a *application) rth() bool {
	in, _ := market.InstrumentOf(a.symbol)
	return in.Future && a.set.FuturesRTH && a.intervalMs() < 86_400_000
}

// byFeed splits names into Binance's and Yahoo's, keeping their order.
func byFeed(names []string) (groups [][]string) {
	var bin, perp, yahoo []string
	for _, n := range names {
		switch {
		case market.IsYahoo(n):
			yahoo = append(yahoo, n)
		case market.IsPerp(n):
			perp = append(perp, n)
		default:
			bin = append(bin, n)
		}
	}
	for _, g := range [][]string{bin, perp, yahoo} {
		if len(g) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// lookup fetches names and price steps. One delisted name fails a batch, so
// then it asks one by one.
func (a *application) lookup(ctx context.Context, names []string) {
	var syms []market.Symbol
	for _, group := range byFeed(names) {
		feed := a.feedOf(group[0])
		got, err := feed.Symbols(ctx, group...)
		if err != nil && len(group) > 1 {
			for _, n := range group {
				if s, err := feed.Symbols(ctx, n); err == nil {
					got = append(got, s...)
				}
			}
		}
		syms = append(syms, got...)
	}
	a.post(func() {
		for _, s := range syms {
			a.infos[s.Name] = s
		}
	})
}

// loadPerps fetches every perpetual once, for the search. It takes a proxy
// from mainland China; without one the search has no perpetuals.
func (a *application) loadPerps() {
	if a.perpState == 1 || a.perpState == 2 {
		return
	}
	a.perpState = 1
	perp := a.perp
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		all, err := perp.Tickers(ctx)
		a.post(func() {
			if err != nil {
				a.perpState = 3
				return
			}
			a.perps, a.perpState = all, 2
			a.searchGen++
		})
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
			a.searchGen++
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
	a.query, a.searchKind = "", ""
	if i := slices.Index(a.set.Folded, sectionOf(sym)); i >= 0 { // to show where it went
		a.set.Folded = slices.Delete(slices.Clone(a.set.Folded), i, i+1)
		a.save()
	}
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
// guessing from common quote assets until Binance says. A future or a stock
// is its code, priced in dollars: "NQ", "AAPL".
func (a *application) names(sym string) (base, quote string) {
	if s, ok := a.infos[sym]; ok && s.Base != "" {
		return s.Base, s.Quote
	}
	if in, ok := market.InstrumentOf(sym); ok {
		return in.Code, ""
	}
	sym = strings.TrimSuffix(sym, ".P") // a perpetual: as its pair
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
	if tick := a.tick(sym); tick > 0 {
		return decimals(tick)
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

// tick is sym's price step, 0 until known. A future's is known at once.
func (a *application) tick(sym string) float64 {
	if s, ok := a.infos[sym]; ok && s.Tick > 0 {
		return s.Tick
	}
	in, _ := market.InstrumentOf(sym)
	return in.Tick
}

// after is what reads, muted, after a market's name: "/USDT" for a pair,
// what it is for a future or a stock: " 纳指 E-mini", " Apple Inc.".
func (a *application) after(sym string) string {
	_, quote := a.names(sym)
	if market.IsPerp(sym) {
		return suffix(quote) + " " + a.perpKind(sym)
	}
	if !market.IsYahoo(sym) {
		return suffix(quote)
	}
	in, ok := a.usByID[sym]
	if !ok {
		in = popularByID[sym]
	}
	if zh, _, _ := strings.Cut(in.Alias, " "); zh != "" { // 英伟达, as the futures read in Chinese
		return " " + zh
	}
	if s, ok := a.infos[sym]; ok && s.Desc != "" {
		return " " + s.Desc
	}
	if in.Name != "" {
		return " " + in.Name
	}
	if n := a.usNames[sym]; n != "" {
		return " " + n
	}
	if in, _ := market.InstrumentOf(sym); in.Name != "" {
		return " " + in.Name
	}
	return ""
}

// countsDown is whether the newest bar's close is a bar's length after it
// opened, for the countdown: not for a stock's or a future's day, which
// closes with its session.
func (a *application) countsDown(ivMs int64) bool {
	return !market.IsYahoo(a.symbol) || ivMs < 86_400_000
}

// delay is how late the market on the chart comes.
func (a *application) delay() time.Duration {
	in, _ := market.InstrumentOf(a.symbol)
	return in.Delay
}

// dayOf is when the day holding a time began, for the day lines: a stock's
// or a future's session (9:30 or 18:00 the evening before, New York time),
// a pair's midnight as the settings say.
func (a *application) dayOf() func(int64) int64 {
	if in, ok := market.InstrumentOf(a.symbol); ok {
		return in.SessionStart
	}
	return midnights(a.set.Chart.breakLoc())
}

// perpKind is what a perpetual is: 永续, or for one on a stock or gold,
// 股票永续 or 商品永续.
func (a *application) perpKind(sym string) string {
	if s := a.infos[sym]; s.Desc != "" {
		return s.Desc
	}
	return "永续"
}

// venue is where sym trades, and how late its prices are, for the legend.
func venue(sym string) string {
	in, ok := market.InstrumentOf(sym)
	switch {
	case !ok && market.IsPerp(sym):
		return "币安永续"
	case !ok:
		return "Binance"
	case in.Delay > 0:
		return fmt.Sprintf("CME · 晚 %d 分钟", int(in.Delay.Minutes()))
	}
	return "美股"
}

// volumeUnit is what a bar's volume counts: coins, contracts or shares.
func (a *application) volumeUnit(sym string) string {
	base, _ := a.names(sym)
	if in, ok := market.InstrumentOf(sym); ok {
		if in.Future {
			return "手"
		}
		return "股"
	}
	return base
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
