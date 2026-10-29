package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// The watchlist goes by market: coins, futures and US stocks each under a
// heading of their own, in the order the headings were dragged to. A click
// on a heading folds its section away or opens it; rows drag up and down
// within their section.

type watchSection struct{ key, label string }

// sections are the markets the watchlist parts, in their first order.
var sections = []watchSection{{"crypto", "加密货币"}, {"perp", "永续合约"}, {"futures", "期货"}, {"us", "美股"}}

// sectionOf is the section a market goes in.
func sectionOf(sym string) string {
	switch {
	case strings.HasPrefix(sym, "CME:"):
		return "futures"
	case strings.HasPrefix(sym, "US:"):
		return "us"
	case market.IsPerp(sym):
		return "perp"
	}
	return "crypto"
}

// sectionOrder is the sections in the order the settings keep. One they do
// not name (one added since: the perpetuals) goes after the one it follows
// at first, the perpetuals after the coins.
func (a *application) sectionOrder() []watchSection {
	var out []watchSection
	for _, k := range a.set.Sections {
		i := slices.IndexFunc(sections, func(s watchSection) bool { return s.key == k })
		if i >= 0 && !slices.Contains(out, sections[i]) {
			out = append(out, sections[i])
		}
	}
	for i, s := range sections {
		if slices.Contains(out, s) {
			continue
		}
		at := 0
		if i > 0 {
			at = slices.Index(out, sections[i-1]) + 1
		}
		out = slices.Insert(out, at, s)
	}
	return out
}

// A watchRow is a row of the watchlist: a section's heading, with the number
// of its markets, or a market.
type watchRow struct {
	sec watchSection
	sym string // "" for the heading
	n   int
}

// watchRows are the watchlist's rows: each section with markets, its heading
// and, unless folded, its markets in the order of the watchlist.
func (a *application) watchRows() []watchRow {
	var rows []watchRow
	for _, s := range a.sectionOrder() {
		var syms []string
		for _, w := range a.set.Watch {
			if sectionOf(w) == s.key {
				syms = append(syms, w)
			}
		}
		if len(syms) == 0 {
			continue
		}
		rows = append(rows, watchRow{sec: s, n: len(syms)})
		if slices.Contains(a.set.Folded, s.key) {
			continue
		}
		for _, w := range syms {
			rows = append(rows, watchRow{sec: s, sym: w})
		}
	}
	return rows
}

// moveWatch moves the market of row from to before row to (len(rows) for
// the end), keeping it in its section: dropped above it, it goes to the top,
// below it to the bottom. The other sections' markets stay as they are.
func (a *application) moveWatch(rows []watchRow, from, to int) {
	sym, key := rows[from].sym, rows[from].sec.key
	at := 0 // the section's other markets above where it drops
	for _, r := range rows[:min(to, len(rows))] {
		if r.sec.key == key && r.sym != "" && r.sym != sym {
			at++
		}
	}
	var mine []string
	for _, w := range a.set.Watch {
		if sectionOf(w) == key && w != sym {
			mine = append(mine, w)
		}
	}
	mine = slices.Insert(mine, at, sym)
	watch, j := slices.Clone(a.set.Watch), 0
	for i, w := range watch {
		if sectionOf(w) == key {
			watch[i], j = mine[j], j+1
		}
	}
	a.set.Watch = watch
	a.save()
}

// moveSection puts the section key where the section to is: after it,
// dragged from above, before it from below.
func (a *application) moveSection(key, to string) {
	var order []string
	for _, s := range a.sectionOrder() {
		order = append(order, s.key)
	}
	from, at := slices.Index(order, key), slices.Index(order, to)
	if from < 0 || at < 0 || from == at {
		return
	}
	order = slices.Insert(slices.Delete(order, from, from+1), at, key)
	a.set.Sections = order
	a.save()
}

// fold folds the section key away, or opens it.
func (a *application) fold(key string) {
	if i := slices.Index(a.set.Folded, key); i >= 0 {
		a.set.Folded = slices.Delete(slices.Clone(a.set.Folded), i, i+1)
	} else {
		a.set.Folded = append(slices.Clone(a.set.Folded), key)
	}
	a.save()
}

// sectionDrag is a section's heading being dragged to another place.
type sectionDrag struct{ key string }

var chevronIcon = svg("<path d='m6 3.5 4.5 4.5-4.5 4.5'/>")

// watchlistRows shows the watchlist in its sections.
func (a *application) watchlistRows(c *ui.Context, t *ui.Theme, k palette) {
	rows := a.watchRows()
	if len(rows) == 0 {
		ui.Text(c, "在上面搜索币、期货或美股，点一下就会加进来").FontSize(12).TextColor(t.TextMuted).Padding(12)
		return
	}
	l := &a.watchList
	l.Header = func(i int) bool { return i < len(rows) && rows[i].sym == "" }
	l.Key = func(i int) any {
		if i < len(rows) {
			return rows[i].sec.key + "|" + rows[i].sym
		}
		return i
	}
	l.Reorder = func(from []int, to int) {
		if len(from) == 1 && from[0] < len(rows) && rows[from[0]].sym != "" {
			a.moveWatch(rows, from[0], to)
		}
	}
	var drop, open, folding string
	var moved [2]string
	ui.List(c.Key("watch"), l, len(rows), func(i int) {
		r := rows[i]
		if r.sym == "" {
			if f, m := a.sectionHeading(c, t, k, r); f {
				folding = r.sec.key
			} else if m != "" {
				moved = [2]string{m, r.sec.key}
			}
			return
		}
		q, ok := a.quotes[r.sym]
		o, rm := a.quoteRow(c, t, k, r.sym, q, ok, true)
		switch {
		case rm:
			drop = r.sym
		case o && r.sym != a.symbol:
			open = r.sym
		}
	}).Grow(1).FillWidth().MinHeight(0)
	switch {
	case drop != "":
		a.unwatch(drop)
	case open != "":
		a.open(open, a.interval)
	case folding != "":
		a.fold(folding)
	case moved[0] != "":
		a.moveSection(moved[0], moved[1])
	}
}

// sectionHeading is a section's heading: its name and number of markets,
// folding it at a click and taking another heading dropped on it. It
// reports a click, and the section dropped on it.
func (a *application) sectionHeading(c *ui.Context, t *ui.Theme, k palette, r watchRow) (clicked bool, dropped string) {
	folded := slices.Contains(a.set.Folded, r.sec.key)
	h := ui.Row(c.Key("sec-"+r.sec.key)).FillWidth().Height(30).Shrink(0).Padding(0, 12, 0, 8).Gap(6).AlignItems(ui.Center).
		Background(k.bg).Cursor(ui.CursorPointer).Label(r.sec.label).Drag(sectionDrag{r.sec.key})
	if over, ok := ui.DragOver[sectionDrag](h); ok && over.key != r.sec.key {
		h.Background(k.chosen)
	} else if h.Hovered() {
		h.Background(k.hover)
	}
	if got, ok := ui.Drop[sectionDrag](h); ok && got.key != r.sec.key {
		dropped = got.key
	}
	tip := "点一下收起，拖动标题可以调换分区的顺序"
	if folded {
		tip = "点一下展开，拖动标题可以调换分区的顺序"
	}
	h.Tooltip(tip).Children(func() {
		angle := float32(90)
		if folded {
			angle = 0
		}
		ui.Icon(c, chevronIcon).Size(10, 10).Shrink(0).TextColor(t.TextMuted).Rotate(angle)
		ui.Text(c, r.sec.label).FontSize(12).FontWeight(600).TextColor(t.TextMuted).SingleLine()
		ui.Text(c, strconv.Itoa(r.n)).FontSize(11).FontFeatures("tnum").TextColor(k.muted).SingleLine()
	})
	return h.Clicked(), dropped
}
