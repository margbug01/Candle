package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

const (
	titleBarHeight = 44
	watchW         = 330
	notch          = 99 // DIPs a wheel notch scrolls on Windows: 3 lines of 33
	zoomPerNotch   = 1.12
	axisDrag       = 150 // DIPs dragged down the price axis to widen the prices e times
)

func svg(body string) *ui.SVG {
	return ui.MustParseSVG([]byte("<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' fill='none' stroke='currentColor' stroke-width='1.4' stroke-linecap='round' stroke-linejoin='round'>" + body + "</svg>"))
}

var (
	candleIcon = svg("<path d='M5 1.5v13M11 3v10'/><rect x='3' y='4.5' width='4' height='6.5' rx='.6' fill='currentColor'/><rect x='9' y='6' width='4' height='4.5' rx='.6'/>")
	searchIcon = svg("<circle cx='7' cy='7' r='4.5'/><path d='m10.5 10.5 3.5 3.5'/>")
	clearIcon  = svg("<path d='m4 4 8 8M12 4l-8 8'/>")
	latestIcon = svg("<path d='m4 4 4 4-4 4M9 4l4 4-4 4'/>")
)

func (a *application) render(c *ui.Context) {
	theme := *c.Theme()
	k := paletteFor(theme.Dark)
	k.up, k.down = a.set.Chart.Up.or(k.up), a.set.Chart.Down.or(k.down)
	k = k.withInk(&a.set.Chart, a.set.Chart.Background.or(k.bg))
	k.apply(&theme)
	c.SetTheme(&theme)
	t := &theme
	a.drawingShortcuts(c) // window-wide: declared before anything is built in
	ui.Column(c).FillWidth().FillHeight().Background(k.bg).Children(func() {
		a.toolbar(c, t, k)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			a.toolsColumn(c, t, k)
			a.chartArea(c, t, k)
			a.watchlist(c, t, k)
		})
		a.settingsDialog(c, t, k)
		a.indicatorsDialog(c, t, k)
		a.indicatorDialog(c, t, k)
		a.editorDialog(c, t, k)
	})
	// What clicks in this frame do to the lists it was built from, now that
	// it is built; and a frame to show it.
	if len(a.later) > 0 {
		later := a.later
		a.later = nil
		for _, f := range later {
			f()
		}
		c.Invalidate()
	}
}

// toolbar sits in the title bar: the market, the bar sizes and the stream's state.
func (a *application) toolbar(c *ui.Context, t *ui.Theme, k palette) {
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(titleBarHeight).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.line).Children(func() {
		// No name or logo: the room goes to the market. On macOS the window's
		// buttons are left of it.
		ui.Box(c).Width(max(bar.Left-4, 0)).FillHeight().Shrink(0).DragWindow()
		a.headline(c, t, k)
		divider(c, k)
		ui.Row(c).Shrink(0).Gap(2).Padding(0, 10).AlignItems(ui.Center).Children(func() {
			for _, iv := range intervals {
				a.intervalButton(c, t, k, iv)
			}
		})
		divider(c, k)
		if in, _ := market.InstrumentOf(a.symbol); in.Future {
			a.sessionButtons(c, t, k)
			divider(c, k)
		}
		a.indicatorsButton(c, t, k)
		ui.Box(c).Grow(1).FillHeight().DragWindow()
		a.updateBadge(c, t, k)
		a.gearButton(c, t, k)
		a.status(c, t, k)
		ui.Box(c).Width(bar.Right).Shrink(0)
	})
}

func divider(c *ui.Context, k palette) { ui.Box(c).Width(1).Height(20).Shrink(0).Background(k.line) }

// headline is the market's name, its last price and its move over 24 hours.
// The numbers keep a fixed width: MyGo lays a frame out with the widths of
// the one before, and a number growing would be squeezed for a frame.
func (a *application) headline(c *ui.Context, t *ui.Theme, k palette) {
	base, _ := a.names(a.symbol)
	ui.Row(c).Shrink(0).Gap(10).Padding(0, 16).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
			a.symIcon(c, a.symbol, 22)
			ui.RichText(c, ui.Span{Text: base, Size: 16, Weight: 600}, ui.Span{Text: a.after(a.symbol), Size: 13, Color: t.TextMuted}).SingleLine().Shrink(0)
		})
		q, hasQ := a.quotes[a.symbol]
		last, ok := q.Last, hasQ
		// The newest bar, closer to the moment than the quote, but in the
		// regular hours only, not once they are over.
		if n := len(a.bars); n > 0 && !(a.rth() && hasQ) {
			last, ok = a.bars[n-1].C, true
		}
		if !ok {
			return
		}
		col := t.Text
		if hasQ {
			col = k.move(q.Change())
		}
		ui.Text(c, a.priceText(a.symbol, last)).FontSize(15).FontWeight(600).FontFeatures("tnum").TextColor(col).Width(96).Shrink(0).SingleLine()
		if hasQ {
			ui.Text(c, fmt.Sprintf("%+.2f%%", q.Change()*100)).FontSize(12).FontFeatures("tnum").TextColor(col).Width(60).Shrink(0)
		}
	})
}

func (a *application) intervalButton(c *ui.Context, t *ui.Theme, k palette, iv interval) {
	chosen := iv.key == a.interval
	b := ui.ButtonBase(c.Key("iv-" + iv.key)).Height(28).PaddingX(10).Center().Radius(4).Cursor(ui.CursorPointer).Label(iv.label)
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
	// A width of its own: restyled, a label is laid out again in the width the
	// last frame left it, which can come out a hair short, and 5分 wrapped.
	w, _ := c.MeasureText(0, ui.Span{Text: iv.label, Size: 13, Weight: 500})
	b.Children(func() {
		ui.Text(c, iv.label).FontSize(13).FontWeight(500).TextColor(col).SingleLine().Width(float32(math.Ceil(float64(w))) + 1)
	})
	if b.Clicked() && !chosen {
		a.open(a.symbol, iv.key)
	}
}

// sessionButtons choose a future's trading hours, as TradingView's ETH and
// RTH do: the whole electronic session, or 9:30 to 16:15 New York time.
// Days and weeks are the same either way.
func (a *application) sessionButtons(c *ui.Context, t *ui.Theme, k palette) {
	daily := a.intervalMs() >= 86_400_000
	ny, _ := time.LoadLocation("America/New_York")
	open := time.Now().In(ny)
	open = time.Date(open.Year(), open.Month(), open.Day(), 9, 30, 0, 0, ny)
	local := open.In(time.Local).Format("15:04") + "–" + open.Add(6*time.Hour+45*time.Minute).In(time.Local).Format("15:04")
	ui.Row(c).Shrink(0).Gap(2).Padding(0, 10).AlignItems(ui.Center).Children(func() {
		for _, o := range []struct {
			label, tip string
			rth        bool
		}{
			{"ETH", "全时段：CME 电子盘整个交易时段，纽约时间 18:00 到第二天 17:00", false},
			{"RTH", "常规时段：纽约时间 9:30–16:15（你这里 " + local + "），和 TradingView 的 RTH 一样", true},
		} {
			chosen := a.set.FuturesRTH == o.rth
			b := ui.ButtonBase(c.Key("session-" + o.label)).Height(28).PaddingX(8).Center().Radius(4).Label(o.label)
			if daily {
				b.Disabled(true).Tooltip("日线和周线不分时段，两种都一样")
			} else {
				b.Cursor(ui.CursorPointer).Tooltip(o.tip)
			}
			col := t.TextMuted
			switch {
			case chosen && !daily:
				b.Background(k.chosen)
				col = k.accent
			case b.Hovered() && !daily:
				b.Background(k.hover)
			}
			if daily {
				col = col.Alpha(0.5)
			}
			b.Children(func() {
				ui.Text(c, o.label).FontSize(12).FontWeight(600).TextColor(col).SingleLine().Width(28).TextAlign(ui.Center)
			})
			if b.Clicked() && !chosen && !daily {
				a.set.FuturesRTH = o.rth
				a.save()
				a.open(a.symbol, a.interval)
			}
		}
	})
}

func (a *application) status(c *ui.Context, t *ui.Theme, k palette) {
	text, col := "连接中", k.muted
	switch {
	case a.err != "":
		text, col = "加载失败", k.down
	case a.stream == live && a.delay() > 0:
		text, col = fmt.Sprintf("晚 %d 分", int(a.delay().Minutes())), k.amber
	case a.stream == live:
		text, col = "实时", paletteFor(t.Dark).up // green whichever way the bars are colored
	case a.stream == reconnecting:
		text, col = "重连中", k.amber
	}
	row := ui.Row(c).Width(84).Shrink(0).Gap(6).AlignItems(ui.Center).Justify(ui.End).Padding(0, 14, 0, 0).Children(func() {
		ui.Box(c).Size(7, 7).Radius(3.5).Background(col)
		ui.Text(c, text).FontSize(12).TextColor(t.TextMuted).SingleLine()
	})
	if d := a.delay(); d > 0 {
		row.Tooltip(fmt.Sprintf("%s的行情免费的都晚 %d 分钟：价格是交易所的真实成交，只是晚到", lateWhat(a.symbol), int(d.Minutes())))
	}
}

// chartArea is the chart, drawn by drawChart, with the messages and the
// button to the newest bar on top of it.
func (a *application) chartArea(c *ui.Context, t *ui.Theme, k palette) {
	if st := &a.set.Chart; st.Scale != a.scaleMode { // the prices refit on another scale
		a.scaleMode, a.scale = st.Scale, prices{}
	}
	if h := a.home(); h != a.homeShown { // more or less room on the right: resting there, follow it
		if a.view.offset == a.homeShown {
			a.view.offset = h
		}
		a.homeShown = h
	}
	ui.Box(c.Key("chart-area")).Grow(1).FillHeight().MinWidth(0).Children(func() { a.chart(c, t, k) })
	a.loadOlder()
}

// chart is the chart itself and, as its siblings on top so that they take
// their own clicks, the messages, the button to the newest bar and the
// switch for scaling the prices automatically.
func (a *application) chart(c *ui.Context, t *ui.Theme, k palette) {
	box := ui.Box(c.Key("chart")).FillWidth().FillHeight()
	hx, hy, over := box.PointerPosition()
	cursor := ui.CursorCrosshair
	switch {
	case a.drag == dragSep || a.drag == dragNone && over && len(a.bars) > 0 && a.sepAt(hy) >= 0:
		cursor = ui.CursorResizeRow
	case a.drag == dragChart || (a.drag == dragDrawing && a.handle < 0):
		cursor = ui.CursorGrabbing
	case a.drag == dragDrawing:
		cursor = ui.CursorMove
	case a.drag == dragAxis || a.drag == dragPaneAxis || (len(a.bars) > 0 && hx > a.plotW && hy < a.boxH-timeH):
		cursor = ui.CursorResizeNS
	case len(a.bars) == 0 || hx > a.plotW:
		cursor = ui.CursorDefault
	case a.tool == "" && a.creating == nil && over && hy < a.plotH && a.mapReady():
		if d, h := a.hitDrawing(a.mapper(), hx, hy); h >= 0 {
			cursor = ui.CursorMove
		} else if d != nil {
			cursor = ui.CursorPointer
		}
	}
	box.Cursor(cursor).HandleInput(a.chartInput).ContextMenu(a.chartMenu).Draw(func(p *ui.Painter, r ui.Rect) {
		a.drawChart(p, r, k, hx, hy, over)
	})
	switch {
	case len(a.bars) == 0 && a.err != "":
		overlay(c).Children(func() {
			ui.Text(c, "K 线没加载上").FontSize(15).FontWeight(600)
			ui.Text(c, a.err).FontSize(12).TextColor(t.TextMuted).MaxWidth(520).TextAlign(ui.Center)
			if ui.Button(c, "重试").Clicked() {
				a.open(a.symbol, a.interval)
			}
		})
	case len(a.bars) == 0:
		base, _ := a.names(a.symbol)
		overlay(c).Children(func() {
			ui.Text(c, "正在加载 "+base+a.after(a.symbol)+" …").FontSize(13).TextColor(t.TextMuted)
		})
	case a.view.offset < -1:
		a.latestButton(c, k)
	}
	if len(a.bars) > 0 {
		a.legends(c, t, k, a.hoverBar(hx, hy, over))
		a.scaleButtons(c, k, over && hx > a.plotW)
		a.drawingBar(c, t, k)
		a.favBar(c, t, k)
	}
	if a.older {
		ui.Text(c, "正在加载更早的 K 线…").Absolute().Left(14).Bottom(timeH + 10).FontSize(12).TextColor(t.TextMuted).PassThrough()
	}
	a.noticeBar(c, t, k)
}

// noticeBar shows, above the time axis, what came into the indicators
// folder, for a few seconds each.
func (a *application) noticeBar(c *ui.Context, t *ui.Theme, k palette) {
	const stay = 8 * time.Second
	a.notices = slices.DeleteFunc(a.notices, func(n notice) bool { return time.Since(n.at) > stay })
	if len(a.notices) == 0 {
		return
	}
	ui.Column(c.Key("notices")).Absolute().Left(0).Right(axisW).Bottom(timeH + 52).AlignItems(ui.Center).Gap(6).PassThrough().Children(func() {
		for i, n := range a.notices {
			ui.Row(c.Key(fmt.Sprintf("notice-%d", i))).Height(34).Padding(0, 6, 0, 14).Gap(8).AlignItems(ui.Center).MaxWidth(640).
				Radius(6).Background(k.field).Border(1, k.line).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.25)).Children(func() {
				ui.Text(c, n.text).FontSize(12).SingleLine().MinWidth(0).Shrink(1)
				if n.edit != "" && ui.Button(c, "去改").Clicked() {
					key := n.edit
					a.later = append(a.later, func() { a.openEditor(key, "") })
				}
				if legendButton(c, t, k, fmt.Sprintf("notice-close-%d", i), "关闭提示", clearIcon).Clicked() {
					a.later = append(a.later, func() { a.notices = slices.DeleteFunc(a.notices, func(m notice) bool { return m.at == n.at }) })
				}
			})
		}
	})
	c.After(stay - time.Since(a.notices[0].at) + 10*time.Millisecond)
}

func overlay(c *ui.Context) ui.Element {
	return ui.Column(c).Absolute().Top(0).Left(0).Right(axisW).Bottom(timeH).AlignItems(ui.Center).Justify(ui.Center).Gap(10)
}

func (a *application) latestButton(c *ui.Context, k palette) {
	b := ui.Row(c.Key("latest")).Absolute().Right(axisW + 14).Bottom(timeH + 14).Height(28).PaddingX(12).Gap(6).
		AlignItems(ui.Center).Radius(14).Cursor(ui.CursorPointer).Label("回到最新")
	bg := k.label
	if b.Hovered() {
		bg = k.accent
	}
	b.Background(bg).Children(func() {
		ui.Text(c, "回到最新").FontSize(12).TextColor(k.onLabel)
		ui.Icon(c, latestIcon).Size(12, 12).TextColor(k.onLabel)
	})
	if b.Clicked() {
		a.view.offset = a.home()
	}
}

// scaleButtons, in the corner under the price axis, are TradingView's A and
// L: 自动 fits the prices to the bars in sight or holds them where they are,
// 对数 switches the log scale. Unless the settings say otherwise they show
// while the pointer is on the price axis (near) or on them.
func (a *application) scaleButtons(c *ui.Context, k palette, near bool) {
	st := &a.set.Chart
	if st.Buttons == "never" {
		return
	}
	var auto, log ui.Element
	buttons := ui.Row(c.Key("scale-buttons")).Absolute().Right(3).Bottom((timeH - 20) / 2).Width(axisW - 6).Height(20).Gap(2)
	buttons.Children(func() {
		auto = cornerButton(c, k, "自动", "价格轴跟着 K 线自动缩放", !a.scale.manual)
		log = cornerButton(c, k, "对数", "对数坐标", st.Scale == "log")
	})
	if st.Buttons == "" && !near && !auto.Hovered() && !log.Hovered() {
		buttons.Opacity(0)
	}
	if auto.Clicked() {
		if a.scale.manual {
			a.scale = prices{}
		} else {
			a.manualScale()
		}
	}
	if log.Clicked() {
		st.Scale = map[bool]string{true: "", false: "log"}[st.Scale == "log"]
		a.save()
	}
}

func cornerButton(c *ui.Context, k palette, text, tip string, on bool) ui.Element {
	b := ui.ButtonBase(c.Key("corner-" + text)).Grow(1).FillHeight().Center().Radius(4).Cursor(ui.CursorPointer).Label(text).Tooltip(tip)
	col := k.muted
	switch {
	case on:
		col = k.accent
		b.Background(k.chosen)
	case b.Hovered():
		b.Background(k.hover)
	}
	return b.Children(func() { ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(col).SingleLine() })
}

// chartMenu is the chart's right-click menu.
func (a *application) chartMenu(m *ui.Menu) {
	st := &a.set.Chart
	if m.Item("重置图表").Chosen() {
		a.resetView()
	}
	m.Separator()
	if m.Item("自动缩放价格").Checked(!a.scale.manual).Chosen() {
		if a.scale.manual {
			a.scale = prices{}
		} else {
			a.manualScale()
		}
	}
	for _, o := range scaleModes {
		if m.Item(o.label + "坐标").Checked(st.Scale == o.key).Chosen() {
			st.Scale = o.key
			a.save()
		}
	}
	m.Separator()
	if m.Item("撤销画线").Shortcut(ui.Cmd, ui.KeyZ).Disabled(len(a.undo) == 0).Chosen() {
		a.undoDrawings()
	}
	if m.Item("重做画线").Shortcut(redoMods, redoKey).Disabled(len(a.redo) == 0).Chosen() {
		a.redoDrawings()
	}
	if m.Item("隐藏画线").Checked(a.hide).Chosen() {
		a.hide, a.sel, a.creating = !a.hide, 0, nil
	}
	if n := len(a.set.Drawings[a.symbol]); n > 0 && m.Item(fmt.Sprintf("删除全部画线（%d）", n)).Chosen() {
		a.clearDrawings()
	}
	m.Separator()
	if m.Item("设置…").Chosen() {
		a.openSettings()
	}
}

// resetView goes back to the newest bar at the default zoom, the prices
// fitted, the panes' too.
func (a *application) resetView() {
	a.view, a.scale = view{spacing: defaultView().spacing, offset: a.home()}, prices{}
	for _, pv := range a.paneViews {
		pv.scale = prices{}
	}
}

// chartInput pans on a drag and zooms on the wheel, around the pointer; a
// sideways swipe on a touchpad pans. Over the price axis the wheel, and a drag
// up or down, scale the prices, which then stay put, the chart dragging them
// up and down too, until a double click: on the axis that only fits them to
// the bars again, on the chart it goes back to the newest bar as well. An
// indicator's pane is the same (paneInput), its prices scaled on their own;
// the time is the same for all panes.
func (a *application) chartInput(ev ui.InputEvent) bool {
	n := len(a.bars)
	if n == 0 || a.plotW <= 0 {
		return false
	}
	inMain := ev.Y < a.plotH
	onAxis := ev.X > a.plotW
	corner := onAxis && ev.Y >= a.boxH-timeH // under the price axes, the auto switch's
	paneAxis := onAxis && !inMain && !corner
	if a.paneInput(ev) || a.drawInput(ev, !onAxis && inMain) {
		return true
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || corner || paneAxis {
			return false
		}
		if ev.Clicks >= 2 {
			if onAxis {
				a.scale = prices{}
			} else {
				a.resetView()
			}
			return true
		}
		if onAxis && !a.manualScale() {
			return false
		}
		a.drag, a.dragX, a.dragY, a.dragPane = dragChart, ev.X, ev.Y, 0
		if pn, ok := a.paneAt(ev.Y); ok {
			a.dragPane = a.set.Indicators[pn.idx].ID
		}
		if onAxis {
			a.drag = dragAxis
		}
	case ui.InputPointerMove:
		if a.drag == dragNone {
			return false
		}
		if ev.Button != 0 { // let go somewhere we did not hear of
			a.drag = dragNone
			return true
		}
		dx, dy := ev.X-a.dragX, ev.Y-a.dragY
		a.dragX, a.dragY = ev.X, ev.Y
		if a.drag == dragAxis {
			m := a.axis
			a.scale.zoom(math.Exp(float64(dy)/axisDrag), a.scale.at(m.h*(m.top+m.bottom)/2, m), m)
			return true
		}
		a.view.pan(dx)
		switch pv := a.paneViews[a.dragPane]; {
		case a.dragPane == 0 && a.scale.manual:
			a.scale.pan(dy, a.axis)
		case pv != nil && pv.scale.manual:
			pv.scale.pan(dy, pv.ax)
		}
	case ui.InputPointerUp:
		if a.drag == dragNone {
			return false
		}
		a.drag = dragNone
	case ui.InputScroll:
		if corner || paneAxis {
			return false
		}
		if onAxis {
			if !a.manualScale() {
				return false
			}
			a.scale.zoom(math.Pow(zoomPerNotch, float64(ev.DY/notch)), a.scale.at(ev.Y, a.axis), a.axis)
			return true
		}
		if math.Abs(float64(ev.DX)) > math.Abs(float64(ev.DY)) {
			a.view.pan(-ev.DX)
		} else {
			a.view.zoom(float32(math.Pow(zoomPerNotch, float64(-ev.DY/notch))), ev.X, a.plotW, n)
		}
	default:
		return false
	}
	a.view.clamp(a.plotW, n)
	return true
}

// watchlist is the column on the right: the search, and the watched markets
// or what the search finds.
func (a *application) watchlist(c *ui.Context, t *ui.Theme, k palette) {
	ui.Column(c).Width(watchW).FillHeight().Shrink(0).BorderWidth(0, 0, 0, 1).BorderColor(k.line).Children(func() {
		ui.Box(c).FillWidth().Shrink(0).Padding(12).Children(func() { a.searchBox(c, t, k) })
		if a.searchQuery() != "" {
			a.results(c, t, k)
			return
		}
		header(c, t, "自选")
		a.watchlistRows(c, t, k)
	})
}

func header(c *ui.Context, t *ui.Theme, title string) {
	// Its right padding lines the columns up with the rows: 6 of padding, 20
	// for the remove button and 6 of gap.
	ui.Row(c).FillWidth().Shrink(0).Padding(2, 32, 6, 12).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(12).TextColor(t.TextMuted).Grow(1)
		ui.Text(c, "最新价").FontSize(12).TextColor(t.TextMuted).Width(92).TextAlign(ui.End)
		ui.Text(c, "24h").FontSize(12).TextColor(t.TextMuted).Width(58).TextAlign(ui.End)
	})
}

func (a *application) searchBox(c *ui.Context, t *ui.Theme, k palette) {
	ui.Row(c.Key("search")).FillWidth().Height(32).Padding(0, 4, 0, 10).Gap(6).AlignItems(ui.Center).Radius(6).Background(k.field).Children(func() {
		ui.Icon(c, searchIcon).Size(14, 14).TextColor(t.TextMuted).Shrink(0)
		a.searchField(c, t, k)
		if a.query == "" && a.qb.compose == "" {
			return
		}
		x := ui.ButtonBase(c.Key("clear")).Size(24, 24).Shrink(0).Center().Radius(4).Label("清空搜索").Cursor(ui.CursorPointer)
		if x.Hovered() {
			x.Background(k.hover)
		}
		if x.Children(func() { ui.Icon(c, clearIcon).Size(12, 12).TextColor(t.TextMuted) }).Clicked() {
			a.clearSearch()
		}
	})
}

// results lists what the search finds; picking one shows it and watches it.
// The search shows the first few of each section, the section with the best
// match first; a section's 全部 shows all of it, up to searchWhole.
const (
	searchPeek  = 4
	searchWhole = 100
)

func (a *application) results(c *ui.Context, t *ui.Theme, k palette) {
	note := func(s string) { ui.Text(c, s).FontSize(12).TextColor(t.TextMuted).Padding(4, 12) }
	q := a.searchQuery()
	a.loadUS() // asked again a while after it did not come
	a.searchUS(q)
	a.searchZh(q)
	if a.allState == 3 {
		ui.Row(c).Padding(4, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "币安的交易对列表没加载上").FontSize(12).TextColor(t.TextMuted)
			if ui.Button(c, "重试").Clicked() {
				a.loadAll()
			}
		})
	}
	groups := a.foundFor(q).groups
	if len(groups) == 0 {
		switch {
		case a.allState < 2 || a.perpState < 2 && a.perpState != 0 || a.usAsking == q || a.zhAsking == q:
			note("正在搜索「" + q + "」…")
		default:
			note("没有找到「" + q + "」")
		}
		a.perpNote(c, t)
		return
	}
	whole := slices.IndexFunc(groups, func(g hitGroup) bool { return g.sec.key == a.searchKind })
	if whole < 0 {
		a.searchKind = "" // the section has nothing for this query
	}
	var shown []market.Ticker
	for i, g := range groups {
		switch {
		case whole < 0:
			shown = append(shown, g.hits[:min(searchPeek, len(g.hits))]...)
		case i == whole:
			shown = append(shown, g.hits[:min(searchWhole, len(g.hits))]...)
		}
	}
	a.priceHits(q, shown)
	header(c, t, "搜索结果")
	var picked, expand string
	back := false
	ui.Scroll(c.Key("results")).Grow(1).FillWidth().MinHeight(0).Children(func() {
		for i, g := range groups {
			if whole >= 0 && i != whole {
				continue
			}
			n := searchPeek
			if whole >= 0 {
				n = searchWhole
			}
			if a.hitGroupHeading(c, t, k, g, whole >= 0, len(g.hits) > n || whole >= 0 && len(g.hits) > searchPeek) {
				if whole >= 0 {
					back = true
				} else {
					expand = g.sec.key
				}
			}
			for _, h := range g.hits[:min(n, len(g.hits))] {
				qt, hasQ := a.quotes[h.Symbol]
				if !market.IsListed(h.Symbol) {
					qt, hasQ = h, true // Binance's list has the day's move
				}
				if open, _ := a.quoteRow(c, t, k, h.Symbol, qt, hasQ, false); open {
					picked = h.Symbol
				}
			}
			if whole >= 0 && len(g.hits) > n {
				note(fmt.Sprintf("还有 %d 个，打得再具体些", len(g.hits)-n))
			}
		}
		a.perpNote(c, t)
		if a.usAsking == q {
			note("正在问 Yahoo 美股…")
		}
		if a.zhAsking == q {
			note("正在问东方财富…")
		}
	})
	switch {
	case picked != "":
		a.pick(picked)
	case expand != "":
		a.searchKind = expand
	case back:
		a.searchKind = ""
	}
}

// perpNote says the perpetuals did not come, which is a proxy missing from
// mainland China, with a button to ask again.
func (a *application) perpNote(c *ui.Context, t *ui.Theme) {
	if a.perpState != 3 {
		return
	}
	ui.Row(c).Padding(4, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "币安永续没连上（国内要开代理）").FontSize(12).TextColor(t.TextMuted)
		if ui.Button(c, "重试").Clicked() {
			a.loadPerps()
		}
	})
}

// hitGroupHeading heads a section of the search's: its name and how many
// it found, with 全部 to show it whole when it has more than shows, and when
// it shows whole, 全部结果 to go back. It reports a click on either.
func (a *application) hitGroupHeading(c *ui.Context, t *ui.Theme, k palette, g hitGroup, whole, more bool) (clicked bool) {
	ui.Row(c.Key("hits-"+g.sec.key)).FillWidth().Height(30).Shrink(0).Padding(0, 10, 0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		link := func(key, label, text string) {
			b := ui.ButtonBase(c.Key(key)).Height(24).PaddingX(6).Center().Radius(4).Cursor(ui.CursorPointer).Label(label)
			if b.Hovered() {
				b.Background(k.hover)
			}
			b.Children(func() { ui.Text(c, text).FontSize(12).TextColor(k.accent).SingleLine() })
			clicked = b.Clicked()
		}
		if whole {
			link("hits-back", "返回全部结果", "‹ 全部结果")
			ui.Box(c).Grow(1)
		}
		ui.Text(c, g.sec.label).FontSize(12).FontWeight(600).TextColor(t.TextMuted).SingleLine()
		ui.Text(c, strconv.Itoa(len(g.hits))).FontSize(11).FontFeatures("tnum").TextColor(k.muted).SingleLine()
		if !whole {
			ui.Box(c).Grow(1)
			if more {
				link("hits-all-"+g.sec.key, "查看全部"+g.sec.label, "全部 ›")
			}
		}
	})
	return clicked
}

// quoteRow is one market: name, last price and move over 24 hours, and, on
// hover, a button taking it off the watchlist.
func (a *application) quoteRow(c *ui.Context, t *ui.Theme, k palette, sym string, q market.Ticker, hasQ, removable bool) (open, remove bool) {
	base, _ := a.names(sym)
	row := ui.Row(c.Key("row-"+sym)).FillWidth().Height(40).Shrink(0).Padding(0, 6, 0, 12).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(base + a.after(sym))
	hovered := row.Hovered()
	switch {
	case sym == a.symbol:
		row.Background(k.chosen)
	case hovered:
		row.Background(k.hover)
	}
	row.Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			a.symIcon(c, sym, 20)
			ui.RichText(c, ui.Span{Text: base, Size: 13, Weight: 600}, ui.Span{Text: a.after(sym), Size: 11, Color: t.TextMuted}).
				Grow(1).MinWidth(0).SingleLine()
		})
		price, change, col := "—", "", t.TextMuted
		if hasQ {
			price = a.priceText(sym, q.Last)
			change, col = fmt.Sprintf("%+.2f%%", q.Change()*100), k.move(q.Change())
		}
		ui.Text(c, price).FontSize(13).FontFeatures("tnum").Width(92).Shrink(0).TextAlign(ui.End).SingleLine()
		ui.Text(c, change).FontSize(12).FontFeatures("tnum").TextColor(col).Width(58).Shrink(0).TextAlign(ui.End)
		if !removable || !hovered {
			ui.Box(c).Size(20, 20).Shrink(0)
			return
		}
		x := ui.ButtonBase(c.Key("unwatch-"+sym)).Size(20, 20).Shrink(0).Center().Radius(4).Cursor(ui.CursorPointer).Label("移出自选")
		if x.Hovered() {
			x.Background(k.hover)
		}
		remove = x.Children(func() { ui.Icon(c, clearIcon).Size(10, 10).TextColor(t.TextMuted) }).Clicked()
	})
	return row.Clicked() && !remove, remove
}

// priceText is a price in a list: at the market's step once known, else as
// short as it goes.
func (a *application) priceText(sym string, p float64) string {
	if tick := a.tick(sym); tick > 0 {
		return formatPrice(p, decimals(tick))
	}
	return strconv.FormatFloat(p, 'f', -1, 64)
}
