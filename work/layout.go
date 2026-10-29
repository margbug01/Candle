package main

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The layouts, TradingView's: up to eight charts in a window, split into
// rows and columns as its Select Layout menu has them, by its names
// (LayoutType in its Advanced Charts' documentation) and with the charts in
// its order. A layout with fewer charts hides the rest, which come back as
// they were with one with more.

const maxCells = 8

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

// charts builds the layout's charts. The pointer going down on one, on the
// chart, its legend or its buttons, makes it active: down outside every
// chart but one, it went down on that one.
func (a *application) charts(c *ui.Context, t *ui.Theme, k palette) {
	n := a.shownCells()
	inside, pressed := -1, 0
	for i := range n {
		out := a.cellBox(c, t, k, i, func() ui.Element {
			return ui.Box(c.Key("cell-" + strconv.Itoa(i))).FillWidth().FillHeight()
		})
		if out {
			pressed++
		} else {
			inside = i
		}
	}
	if n > 1 && pressed == n-1 {
		a.setActive(inside)
	}
}
