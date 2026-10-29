package main

import (
	"cmp"
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
	yahoo market.Yahoo     // CME futures and US stocks
	perp  market.Perp      // Binance's perpetuals
	east  market.Eastmoney // the search by Chinese names
	post  func(func())     // runs fn on the main thread
	spawn func(func())     // starts background work; the tests drop it
	file  string
	set   settings

	// The layout's charts, cells holding them all, those it hides too (as
	// TradingView keeps them, for a layout with more), and the one the
	// toolbar acts on. cell is the one being built, drawn or handled.
	*cell
	cells      []*cell
	active     int
	maxed      int                    // the chart maximized, -1 for none
	feeds      map[string]*sharedFeed // by feedKey
	cross      crossPoint             // where the crosshair is, this frame
	splitHeld  string                 // the line between charts being dragged
	layoutMenu bool                   // the layout menu is open
	clock      int64                  // Binance's clock minus this machine's, ms

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
	query      string          // what is typed in the search box
	qb         searchBox       // the box's caret, selection and composition
	stopWatch  context.CancelFunc
	icons      icons                          // the coins' icons
	usFound    map[string][]market.Instrument // Yahoo's answers for US stocks, by query, until usList is in
	usNames    map[string]string              // the stocks' names, from those answers
	usAsking   string                         // the query being asked
	zhFound    map[string][]market.Instrument // what Eastmoney's search found for queries in Chinese, by query
	zhNames    map[string]string              // the US stocks' Chinese names, from those answers
	zhAsking   string                         // the query being asked
	usList     []market.Instrument            // the US stocks and ETFs, from Nasdaq's list
	usByID     map[string]market.Instrument   // usList by ID
	usLoading  bool
	usFailed   time.Time // when the list last did not come
	usFetch    func(context.Context) ([]byte, error)
	searchGen  int // grows as what the search looks through changes
	watchList  ui.ListState
	found      foundCache
	priced     map[string]time.Time // when the results' prices were asked for

	// The drawings: the tool in hand, what is copied and on which market.
	tool    string
	hide    bool
	nextID  int64
	clip    *drawing // what Ctrl+C copied, and on which market
	clipSym string

	// The indicators: the list to add them from and the dialog setting one,
	// with it as it was, for 取消. later is what the frame being built
	// changes once it is built.
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
	a := &application{
		bin: bin, yahoo: market.Yahoo{HTTP: bin.HTTP}, perp: market.Perp{HTTP: bin.HTTP}, east: market.Eastmoney{HTTP: bin.HTTP}, post: post, file: file, set: set,
		usFound: map[string][]market.Instrument{}, usNames: map[string]string{}, priced: map[string]time.Time{},
		zhFound: map[string][]market.Instrument{}, zhNames: map[string]string{},
		usFetch: func(ctx context.Context) ([]byte, error) { return market.USList(ctx, bin.HTTP) },
		spawn:   func(f func()) { go f() },
		infos:   map[string]market.Symbol{},
		quotes:  map[string]market.Ticker{},
		picks:   map[string]*choice[string]{},
		colors:  map[string]*choice[ui.Color]{},
		feeds:   map[string]*sharedFeed{},
		nextID:  maxDrawingID(set.Drawings),
		icons:   newIcons("", bin.Icons, bin.Icon),
	}
	for _, cs := range set.Charts {
		a.cells = append(a.cells, newCell(cs, set.Chart))
	}
	a.active, a.maxed = set.Active, -1
	a.cell = a.activeCell()
	return a
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
	for _, c := range a.cells[:a.shownCells()] {
		a.on(c, func() { a.open(c.symbol, c.interval) })
	}
	a.watchQuotes()
	a.loadAll()
	a.loadPerps()
	a.loadUS()
	a.loadIcons(false)
	a.spawn(a.syncClock)
	a.watchUpdates()
}

// save writes the settings, the charts' as they are. While the settings
// dialog is open the chart is saved as it was: what the dialog shows is
// 确认's to keep.
func (a *application) save() {
	a.set.Charts = make([]chartSettings, len(a.cells))
	for i, c := range a.cells {
		a.set.Charts[i] = c.saved()
	}
	a.set.Active = a.active
	s := a.set
	if a.editing {
		s.Chart = a.before
	}
	saveSettings(a.file, s)
}

// open shows sym in bars of iv, keeping the zoom; the bars come from the
// feed another cell has for them, if one does.
func (a *application) open(sym, iv string) {
	a.park()
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
	a.save()
	key := a.feedKey(sym, iv)
	if a.joinFeed(key) {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	f := &sharedFeed{key: key, stop: stop, cells: []*cell{a.cell}}
	a.feeds[key], a.share = f, f
	feed := a.barFeed(sym)
	_, known := a.infos[sym]
	a.spawn(func() { a.feed(ctx, f, feed, sym, iv, !known) })
	if market.IsPerp(sym) {
		perp := a.perp
		a.spawn(func() { a.followFunding(ctx, f, perp, sym) })
	}
}

// park stops the cell's requests: what is on its way, and its feed unless
// another cell takes it. The cell keeps its market, for showing it again.
func (a *application) park() {
	if a.stop != nil {
		a.stop()
		a.stop = nil
	}
	a.leaveFeed()
}

// followFunding asks for the perpetual's mark price and funding every half
// minute while it is on a chart.
func (a *application) followFunding(ctx context.Context, f *sharedFeed, perp market.Perp, sym string) {
	for {
		got, err := perp.Funding(ctx, sym)
		if err == nil {
			a.post(func() {
				a.each(f, func() { a.funding, a.fundingOf = got, sym })
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
// growing pause and filling in what it missed meanwhile, for the cells f
// feeds. A first page that does not come ends it, and the next cell to
// show the market starts another.
func (a *application) feed(ctx context.Context, f *sharedFeed, feed market.Feed, sym, iv string, needInfo bool) {
	if needInfo {
		go a.lookup(ctx, []string{sym})
	}
	bars, err := feed.Klines(ctx, sym, iv, 0, 1000)
	if ctx.Err() != nil {
		return
	}
	a.post(func() {
		if err != nil && a.feeds[f.key] == f {
			delete(a.feeds, f.key)
		}
		a.each(f, func() {
			a.loading = false
			if err != nil {
				a.err = shorten(err.Error(), 160)
				return
			}
			a.bars, a.atStart = slices.Clone(bars), len(bars) < 1000
		})
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
				a.each(f, func() {
					a.stream = live
					a.addBars([]market.Bar{bar})
				})
			})
		})
		if ctx.Err() != nil {
			return
		}
		if heard {
			pause = time.Second
		}
		a.post(func() {
			a.each(f, func() { a.stream = reconnecting })
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		pause = min(pause*2, 30*time.Second)
		if got, err := feed.Klines(ctx, sym, iv, 0, 1000); err == nil && len(got) > 0 {
			a.post(func() {
				a.each(f, func() {
					// Away longer than a page: start over rather than leave a hole.
					if n := len(a.bars); n == 0 || got[0].T > a.bars[n-1].T {
						a.bars, a.atStart, a.view.offset = slices.Clone(got), len(got) < 1000, a.home()
						return
					}
					a.addBars(got)
				})
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
	c, ctx, gen, sym, iv, end, feed := a.cell, a.ctx, a.gen, a.symbol, a.interval, a.bars[0].T-1, a.barFeed(a.symbol)
	a.spawn(func() {
		got, err := feed.Klines(ctx, sym, iv, end, 1000)
		a.post(func() {
			if gen != c.gen {
				return
			}
			a.on(c, func() { a.olderCame(got, err) })
		})
	})
}

// olderCame puts the page before the first bar in front of it.
func (a *application) olderCame(got []market.Bar, err error) {
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
		hits := searchAll(q, pairs, localMarkets, a.stocksFor(q), a.zhFound[q], math.MaxInt)
		*f = foundCache{done: true, q: q, gen: a.searchGen, hits: hits, groups: groupHits(hits)}
	}
	return f
}

// firstFinding is what Enter in the search opens: the best of the section
// shown whole, or of all.
func (a *application) firstFinding() string {
	for _, g := range a.foundFor(a.searchQuery()).groups {
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
			if a.searchQuery() != q { // typed on: that query asks
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

// searchZh asks Eastmoney's search for the stocks a query in Chinese names
// (闪迪, 海力士, 国电电力) or digits (600795, 00700) finds, US stocks, A
// shares, Hong Kong's and Korea's, a moment after the typing stops. Answers
// are kept by query; one that did not come is asked again when the query
// comes back.
func (a *application) searchZh(query string) {
	q := strings.TrimSpace(query)
	if !market.HasHan(q) && !codeQuery(q) || a.zhFound[q] != nil || a.zhAsking == q {
		return
	}
	a.zhAsking = q
	a.spawn(func() {
		time.Sleep(150 * time.Millisecond)
		a.post(func() {
			if a.searchQuery() != q { // typed on: that query asks
				if a.zhAsking == q {
					a.zhAsking = ""
				}
				return
			}
			a.spawn(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				got, err := a.east.Suggest(ctx, q, 30)
				a.post(func() {
					if a.zhAsking == q {
						a.zhAsking = ""
					}
					if err != nil {
						return
					}
					if len(a.zhFound) > 200 {
						clear(a.zhFound)
					}
					found := append([]market.Instrument{}, market.Found(got)...)
					a.zhFound[q] = found
					for _, in := range found {
						if in.Alias != "" {
							a.zhNames[in.ID] = in.Alias
						}
					}
					a.searchGen++
				})
			})
		})
	})
}

// codeQuery is whether q is a stock's code in digits, three or more: an A
// share's 600795, Hong Kong's 00700.
func codeQuery(q string) bool {
	return len(q) >= 3 && !strings.ContainsFunc(q, func(r rune) bool { return r < '0' || r > '9' })
}

// priceHits asks Yahoo and Eastmoney for the prices of the futures, stocks
// and indices among the first results that have none, a moment after the
// typing stops: the results show at once, the prices a moment after.
func (a *application) priceHits(q string, hits []market.Ticker) {
	var ids []string
	for _, h := range hits[:min(len(hits), 15)] {
		if _, ok := a.quotes[h.Symbol]; !ok && market.IsListed(h.Symbol) && time.Since(a.priced[h.Symbol]) > 30*time.Second {
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
			if a.searchQuery() != q { // typed on: ask for these again when they show
				for _, id := range ids {
					delete(a.priced, id)
				}
				return
			}
			a.spawn(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				got, _ := a.yahoo.Quotes(ctx, ids) // each takes its own
				more, _ := a.east.Quotes(ctx, ids)
				got = append(got, more...)
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

// localMarkets are the markets the search finds without asking anyone,
// beside the pairs and the stocks: the futures and the indices, by code and
// by Chinese name.
var localMarkets = append(slices.Clip(market.Futures), market.Indices...)

// feedOf is where sym's bars come from: Yahoo for futures, US and Korean
// stocks and the VIX, Eastmoney for A shares and Hong Kong's, Binance for
// the rest.
func (a *application) feedOf(sym string) market.Feed {
	switch {
	case market.IsYahoo(sym):
		return a.yahoo
	case market.IsEastmoney(sym):
		return a.east
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

// byFeed splits names by where they come from, keeping their order.
func byFeed(names []string) (groups [][]string) {
	var bin, perp, yahoo, east []string
	for _, n := range names {
		switch {
		case market.IsYahoo(n):
			yahoo = append(yahoo, n)
		case market.IsEastmoney(n):
			east = append(east, n)
		case market.IsPerp(n):
			perp = append(perp, n)
		default:
			bin = append(bin, n)
		}
	}
	for _, g := range [][]string{bin, perp, yahoo, east} {
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
	a.clearSearch()
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
		a.openSynced(sym, a.interval)
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
	return splitPair(strings.TrimSuffix(sym, ".P")) // a perpetual: as its pair
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
	if !market.IsListed(sym) {
		return suffix(quote)
	}
	in, ok := a.usByID[sym]
	if !ok {
		in = popularByID[sym]
	}
	if zh, _, _ := strings.Cut(in.Alias, " "); zh != "" { // 英伟达, as the futures read in Chinese
		return " " + zh
	}
	if zh := a.zhNames[sym]; zh != "" { // 闪迪, as Eastmoney calls it
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
	return !market.IsListed(a.symbol) || ivMs < 86_400_000
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

// venue is where sym trades, and how late its prices are, for the legend:
// 上交所, 港交所, Cboe · 晚 15 分钟.
func venue(sym string) string {
	in, ok := market.InstrumentOf(sym)
	where := cmp.Or(in.Venue, "美股")
	switch {
	case !ok && market.IsPerp(sym):
		return "币安永续"
	case !ok:
		return "Binance"
	case in.Future:
		where = "CME"
	}
	if in.Delay > 0 {
		return fmt.Sprintf("%s · 晚 %d 分钟", where, int(in.Delay.Minutes()))
	}
	return where
}

// lateWhat is what comes late, for the top bar's word on it.
func lateWhat(sym string) string {
	in, _ := market.InstrumentOf(sym)
	switch {
	case in.Future:
		return "CME 期货"
	case in.Index:
		return in.Venue + " 指数"
	}
	return in.Venue + "股票"
}

// volumeUnit is what a bar's volume counts: coins, contracts or shares.
func (a *application) volumeUnit(sym string) string {
	base, _ := a.names(sym)
	if in, ok := market.InstrumentOf(sym); ok {
		switch {
		case in.Future:
			return "手"
		case in.Index:
			return ""
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
