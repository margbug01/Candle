package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// The bar of favorite drawing tools, after TradingView's: the tools starred
// in the column on the left, on a bar that floats over the chart where it
// is dragged by its grip, so that they are at hand where the drawing is.

var (
	starIcon     = svg("<path d='M8 1.8l1.9 3.9 4.2.6-3 3 .7 4.2L8 11.5l-3.8 2 .7-4.2-3-3 4.2-.6z'/>")
	starFillIcon = svg("<path d='M8 1.8l1.9 3.9 4.2.6-3 3 .7 4.2L8 11.5l-3.8 2 .7-4.2-3-3 4.2-.6z' fill='currentColor'/>")
	gripIcon     = svg("<circle cx='6' cy='4' r='.9' fill='currentColor'/><circle cx='10' cy='4' r='.9' fill='currentColor'/><circle cx='6' cy='8' r='.9' fill='currentColor'/><circle cx='10' cy='8' r='.9' fill='currentColor'/><circle cx='6' cy='12' r='.9' fill='currentColor'/><circle cx='10' cy='12' r='.9' fill='currentColor'/>")
)

const (
	favButton = 30 // a tool's button on the bar
	favGrip   = 16
)

func (a *application) isFav(kind string) bool { return slices.Contains(a.set.FavTools, kind) }

// toggleFav stars a tool, or takes its star away. The bar keeps the order
// of the column.
func (a *application) toggleFav(kind string) {
	if a.isFav(kind) {
		a.set.FavTools = slices.DeleteFunc(slices.Clone(a.set.FavTools), func(k string) bool { return k == kind })
	} else {
		var out []string
		for _, tl := range drawTools {
			if tl.kind == kind || a.isFav(tl.kind) {
				out = append(out, tl.kind)
			}
		}
		a.set.FavTools = out
		a.set.FavBar = true
	}
	a.save()
}

// favSize is the bar's width and height.
func (a *application) favSize() (float32, float32) {
	return 8 + favGrip + 2 + float32(len(a.set.FavTools))*(favButton+2) + 4, favButton + 8
}

// favRoom is how far across and down the chart the bar's top left can go.
func (a *application) favRoom() (float32, float32) {
	w, h := a.favSize()
	return max(a.plotW-w, 0), max(a.boxH-timeH-h, 0)
}

// favAt is the bar's top left in the chart.
func (a *application) favAt() (float32, float32) {
	rw, rh := a.favRoom()
	return float32(a.set.FavX) * rw, float32(a.set.FavY) * rh
}

// moveFav moves the bar by dx, dy, keeping it on the chart.
func (a *application) moveFav(dx, dy float32) {
	rw, rh := a.favRoom()
	x, y := a.favAt()
	x, y = min(max(x+dx, 0), rw), min(max(y+dy, 0), rh)
	if rw > 0 {
		a.set.FavX = float64(x / rw)
	}
	if rh > 0 {
		a.set.FavY = float64(y / rh)
	}
}

// favBar floats over the chart while it is shown and has tools.
func (a *application) favBar(c *ui.Context, t *ui.Theme, k palette) {
	if !a.set.FavBar || len(a.set.FavTools) == 0 || a.plotW <= 0 || a.hide {
		return
	}
	x, y := a.favAt()
	bar := ui.Row(c.Key("fav-bar")).Absolute().Left(x).Top(y).Height(favButton+8).Padding(0, 4).Gap(2).AlignItems(ui.Center).
		Radius(7).Background(k.field).Border(1, k.line).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.22)).Label("常用工具栏")
	bar.Children(func() {
		grip := ui.Box(c.Key("fav-grip")).Width(favGrip).Height(favButton).Center().Radius(4).Cursor(ui.CursorGrab).
			Label("拖动常用工具栏").Tooltip("按住拖动")
		if grip.Hovered() || grip.Dragging() {
			grip.Background(k.hover)
		}
		grip.Children(func() { ui.Icon(c, gripIcon).Size(14, 14).TextColor(t.TextMuted) })
		if dx, dy, ok := grip.Dragged(); ok && (dx != 0 || dy != 0) {
			a.moveFav(dx, dy)
			c.Invalidate()
		}
		if _, _, ended := grip.DragEnded(); ended {
			a.save()
		}
		for _, kind := range a.set.FavTools {
			tl, ok := toolOf(kind)
			if !ok {
				continue
			}
			b := ui.ButtonBase(c.Key("fav-"+kind)).Size(favButton, favButton).Shrink(0).Center().Radius(5).Cursor(ui.CursorPointer).
				Label("常用 " + tl.label).Tooltip(tl.label)
			col := t.Text
			switch {
			case a.tool == kind:
				b.Background(k.chosen)
				col = k.accent
			case b.Hovered():
				b.Background(k.hover)
			}
			b.Children(func() { ui.Icon(c, tl.icon).Size(17, 17).TextColor(col) })
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("从常用工具栏移除").Chosen() {
					a.later = append(a.later, func() { a.toggleFav(kind) })
				}
			})
			if b.Clicked() {
				a.pickTool(kind)
			}
		}
	})
}

// toolWithStar is a tool's button in the column, with, while the pointer
// is on it, a star that puts it on the bar of favorites or takes it off.
func (a *application) toolWithStar(c *ui.Context, t *ui.Theme, k palette, tl drawTool) {
	ui.Box(c.Key("tool-wrap-"+tl.kind)).Size(32, 32).Shrink(0).Children(func() {
		b := toolButton(c, t, k, "tool-"+tl.kind, tl.label, tl.icon, a.tool == tl.kind)
		fav := a.isFav(tl.kind)
		label, icon, col := "加入常用工具栏", starIcon, t.TextMuted
		if fav {
			label, icon, col = "从常用工具栏移除", starFillIcon, ui.Hex("#f0b90b")
		}
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item(label).Chosen() {
				a.later = append(a.later, func() { a.toggleFav(tl.kind) })
			}
		})
		star := ui.ButtonBase(c.Key("star-"+tl.kind)).Absolute().Top(-3).Right(-6).Size(16, 16).Center().Radius(8).
			Cursor(ui.CursorPointer).Label(label + " " + tl.label).Tooltip(label)
		star.Children(func() { ui.Icon(c, icon).Size(11, 11).TextColor(col) })
		if !b.Hovered() && !star.Hovered() && !star.Focused() {
			star.Invisible()
		}
		if star.Clicked() {
			a.later = append(a.later, func() { a.toggleFav(tl.kind) })
		}
		if b.Clicked() {
			a.pickTool(tl.kind)
		}
	})
}
