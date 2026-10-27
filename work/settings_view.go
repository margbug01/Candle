package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// The settings dialog, after TradingView's: its pages on the left, a page's
// rows on the right. What changes shows on the chart at once; 确认 keeps it,
// 取消, Escape or a click beside the dialog puts it back as it was.

var (
	gearIcon   = svg("<path d='M13.5 5H8M8 11H2.5'/><circle cx='5' cy='5' r='2.2'/><circle cx='11' cy='11' r='2.2'/>")
	statusIcon = svg("<path d='M2.5 4h11M2.5 8h7M2.5 12h11'/>")
	scaleIcon  = svg("<path d='M2.5 2v11.5H14'/><path d='m5 10 3-3 2 2 3.5-4.5'/>")
	canvasIcon = svg("<path d='m10.5 2.5 3 3-8 8h-3v-3z'/><path d='m9 4 3 3'/>")
	aboutIcon  = svg("<circle cx='8' cy='8' r='6'/><path d='M8 7.3v3.9'/><circle cx='8' cy='5.1' r='.5' fill='currentColor'/>")
)

var settingsPages = []struct {
	label string
	icon  *ui.SVG
	build func(a *application, c *ui.Context, t *ui.Theme, k palette)
}{
	{"商品", candleIcon, (*application).symbolPage},
	{"状态行", statusIcon, (*application).statusPage},
	{"坐标", scaleIcon, (*application).scalesPage},
	{"版面", canvasIcon, (*application).canvasPage},
	{"关于", aboutIcon, (*application).aboutPage},
}

// choice is a control's own copy of a value. MyGo writes a drop-down's or a
// color well's input back after the frame is built, so the control binds
// something that outlives the frame; shown is what the last frame showed,
// and v differing from it is the user's change.
type choice[T comparable] struct{ v, shown T }

// bind is called once a frame with what should show; it returns what to
// bind the control to and, when changed, what the user just picked.
func (ch *choice[T]) bind(current T) (p *T, picked T, changed bool) {
	if ch.v != ch.shown {
		ch.shown = ch.v
		return &ch.v, ch.v, true
	}
	ch.v, ch.shown = current, current
	return &ch.v, current, false
}

func (a *application) openSettings() {
	a.before, a.editing, a.dialog = a.set.Chart, true, true
}

// closeSettings keeps the settings, or puts back what they were.
func (a *application) closeSettings(keep bool) {
	a.editing, a.dialog = false, false
	if keep {
		a.save()
	} else {
		if a.set.Chart.Theme != a.before.Theme {
			applyTheme(a.before.Theme)
		}
		a.set.Chart = a.before
	}
}

// gearButton opens the settings.
func (a *application) gearButton(c *ui.Context, t *ui.Theme, k palette) {
	b := ui.ButtonBase(c.Key("gear")).Size(30, 30).Shrink(0).Center().Radius(6).Cursor(ui.CursorPointer).Label("图表设置").Tooltip("图表设置")
	if b.Hovered() {
		b.Background(k.hover)
	}
	if b.Children(func() { ui.Icon(c, gearIcon).Size(16, 16).TextColor(t.TextMuted) }).Clicked() {
		a.openSettings()
	}
}

func (a *application) settingsDialog(c *ui.Context, t *ui.Theme, k palette) {
	if a.editing && !a.dialog { // closed by Escape or a click beside it
		a.closeSettings(false)
	}
	if !a.dialog {
		return
	}
	_, wh := c.Size()
	ui.DialogBase(c.Key("settings"), &a.dialog, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(720).Height(min(680, wh-48)).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label("图表设置")
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			dialogHeader(c, t, k, "设置", "关闭设置", func() { a.closeSettings(false) })
			ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Width(168).Shrink(0).FillHeight().Padding(4, 10, 0, 12).Gap(2).Children(func() {
					for i, pg := range settingsPages {
						a.pageButton(c, t, k, i, pg.label, pg.icon)
					}
				})
				pg := settingsPages[a.page]
				ui.Scroll(c.Key("settings-" + pg.label)).Grow(1).FillHeight().MinWidth(0).Children(func() {
					ui.Column(c).FillWidth().Padding(0, 28, 24, 16).Children(func() { pg.build(a, c, t, k) })
				})
			})
			ui.Row(c).FillWidth().Height(64).Shrink(0).Padding(0, 20).Gap(10).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(k.line).Children(func() {
				if settingsPages[a.page].label != "关于" && ui.Button(c, "恢复默认").Clicked() {
					theme := a.set.Chart.Theme
					a.set.Chart = defaultStyle()
					a.set.Chart.Theme = theme
				}
				ui.Box(c).Grow(1)
				if ui.Button(c, "取消").Clicked() {
					a.closeSettings(false)
				}
				if ui.PrimaryButton(c, "确认").Clicked() {
					a.closeSettings(true)
				}
			})
		})
	})
}

func (a *application) pageButton(c *ui.Context, t *ui.Theme, k palette, i int, label string, icon *ui.SVG) {
	b := ui.Row(c.Key("page-"+label)).FillWidth().Height(36).Padding(0, 10).Gap(10).AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(label)
	col, weight := t.Text, 400
	switch {
	case i == a.page:
		b.Background(k.field)
		weight = 600
	case b.Hovered():
		b.Background(k.hover)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(16, 16).TextColor(col)
		ui.Text(c, label).FontSize(13).FontWeight(weight).TextColor(col).SingleLine()
	})
	if b.Clicked() {
		a.page = i
	}
}

func section(c *ui.Context, t *ui.Theme, title string) {
	ui.Text(c, title).FontSize(12).TextColor(t.TextMuted).Padding(20, 0, 6, 0)
}

// row is a setting: its name, and the controls beside it.
func row(c *ui.Context, name string, controls func()) {
	ui.Row(c).FillWidth().MinHeight(40).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(13).Width(150).Shrink(0)
		ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Children(controls)
	})
}

// checkRow is a setting switched by a check box, with what else it takes.
func checkRow(c *ui.Context, on *bool, name string, controls func()) {
	ui.Row(c).FillWidth().MinHeight(40).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c.Key("check-"+name), on, name).Width(150).Shrink(0)
		if controls != nil {
			ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Children(controls)
		}
	})
}

// dropdown is a drop-down of opts showing the one keyed cur; set takes what the
// user picks.
func (a *application) dropdown(c *ui.Context, key string, opts []option, cur string, set func(string)) ui.Element {
	ch := a.picks[key]
	if ch == nil {
		ch = &choice[string]{}
		a.picks[key] = ch
	}
	p, picked, changed := ch.bind(labelOf(opts, cur))
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.label
		if changed && o.label == picked {
			set(o.key)
		}
	}
	return ui.Select(c.Key(key), p, labels).Width(240).Label(key)
}

// well is a color well showing cur; set takes the color the user picks.
func (a *application) well(c *ui.Context, key string, cur ui.Color, set func(hexColor)) ui.Element {
	ch := a.colors[key]
	if ch == nil {
		ch = &choice[ui.Color]{}
		a.colors[key] = ch
	}
	p, picked, changed := ch.bind(cur)
	if changed {
		set(hexColor(picked))
	}
	return ui.ColorWell(c.Key(key), p).Label(key)
}

// upDown is a pair of color wells, for rising and for falling bars.
func (a *application) upDown(c *ui.Context, key string, up, down *hexColor, defUp, defDown ui.Color) {
	a.well(c, key+"涨", up.or(defUp), func(h hexColor) { *up = h }).Tooltip("上涨")
	a.well(c, key+"跌", down.or(defDown), func(h hexColor) { *down = h }).Tooltip("下跌")
}

func (a *application) symbolPage(c *ui.Context, t *ui.Theme, k palette) {
	st := &a.set.Chart
	section(c, t, "K线")
	row(c, "图表类型", func() { a.dropdown(c, "图表类型", chartTypes, st.Type, func(v string) { st.Type = v }) })
	row(c, "快速配色", func() {
		def := paletteFor(t.Dark)
		if ui.Button(c, "绿涨红跌").Clicked() {
			st.Up, st.Down = hexColor{}, hexColor{}
			st.BorderUp, st.BorderDown, st.WickUp, st.WickDown = hexColor{}, hexColor{}, hexColor{}, hexColor{}
		}
		if ui.Button(c, "红涨绿跌").Clicked() {
			st.Up, st.Down = hexColor(def.down), hexColor(def.up)
			st.BorderUp, st.BorderDown, st.WickUp, st.WickDown = hexColor{}, hexColor{}, hexColor{}, hexColor{}
		}
	})
	checkRow(c, &st.Body, "实体", func() { a.upDown(c, "实体", &st.Up, &st.Down, paletteFor(t.Dark).up, paletteFor(t.Dark).down) })
	checkRow(c, &st.Borders, "边框", func() { a.upDown(c, "边框", &st.BorderUp, &st.BorderDown, k.up, k.down) })
	checkRow(c, &st.Wick, "影线", func() { a.upDown(c, "影线", &st.WickUp, &st.WickDown, k.up, k.down) })
	// What reads on the chart in the up and down colors: the last price's
	// label and line, the legend, the top bar's price, the watchlist's moves.
	row(c, "标签和文字", func() {
		a.dropdown(c, "标签和文字", inkModes, st.Ink, func(v string) { st.Ink = v }).Width(200).
			Tooltip("价格标签、图例和涨跌数字用的颜色；实体颜色和背景太接近时，自动换成边框色")
		if st.Ink == "custom" {
			auto := *st
			auto.Ink = ""
			def := k.withInk(&auto, st.Background.or(k.bg))
			a.upDown(c, "文字", &st.InkUp, &st.InkDown, def.upInk, def.downInk)
		}
	})
	row(c, "折线和面积", func() { a.well(c, "折线", st.Line.or(k.accent), func(h hexColor) { st.Line = h }) })
	checkRow(c, &st.PrevClose, "按前收盘价着色", nil)
	checkRow(c, &st.Volume, "成交量", nil)
	section(c, t, "数据")
	row(c, "价格精度", func() {
		a.dropdown(c, "价格精度", precisions, strconv.Itoa(st.Precision), func(v string) { st.Precision, _ = strconv.Atoi(v) })
	})
	row(c, "时区", func() { a.dropdown(c, "时区", timeZones, st.Zone, func(v string) { st.Zone = v }) })
}

func (a *application) statusPage(c *ui.Context, t *ui.Theme, k palette) {
	st := &a.set.Chart
	section(c, t, "图表左上角的图例")
	checkRow(c, &st.Title, "商品名称和周期", nil)
	checkRow(c, &st.Values, "开高低收", nil)
	checkRow(c, &st.Change, "K线涨跌", nil)
	checkRow(c, &st.VolumeValue, "成交量", nil)
}

func (a *application) scalesPage(c *ui.Context, t *ui.Theme, k palette) {
	st := &a.set.Chart
	section(c, t, "价格坐标")
	row(c, "坐标模式", func() { a.dropdown(c, "坐标模式", scaleModes, st.Scale, func(v string) { st.Scale = v }) })
	row(c, "自动和对数按钮", func() {
		a.dropdown(c, "自动和对数按钮", buttonModes, st.Buttons, func(v string) { st.Buttons = v })
	})
	checkRow(c, &st.NoOverlap, "无重叠标签", nil)
	section(c, t, "价格标签和价格线")
	row(c, "最新价", func() { a.dropdown(c, "最新价", lastPrices, st.LastPrice, func(v string) { st.LastPrice = v }) })
	checkRow(c, &st.Countdown, "当前K线结束倒计时", nil)
	checkRow(c, &st.IndValues, "指标最新值", nil)
	checkRow(c, &st.HighLow, "可见范围的高低点", nil)
	section(c, t, "时间坐标")
	checkRow(c, &st.Weekday, "标签上的星期几", nil)
	row(c, "日期格式", func() {
		a.dropdown(c, "日期格式", dateFormats, st.DateFormat, func(v string) { st.DateFormat = v })
	})
	clock := ""
	if st.Clock12 {
		clock = "12"
	}
	row(c, "时间格式", func() { a.dropdown(c, "时间格式", clocks, clock, func(v string) { st.Clock12 = v == "12" }) })
}

func (a *application) canvasPage(c *ui.Context, t *ui.Theme, k palette) {
	st := &a.set.Chart
	section(c, t, "图表")
	row(c, "主题", func() {
		a.dropdown(c, "主题", themes, st.Theme, func(v string) {
			st.Theme = v
			applyTheme(v)
		})
	})
	row(c, "背景", func() { a.well(c, "背景", st.Background.or(k.bg), func(h hexColor) { st.Background = h }) })
	checkRow(c, &st.VertGrid, "垂直网格线", func() {
		a.well(c, "垂直网格线颜色", st.VertColor.or(k.grid), func(h hexColor) { st.VertColor = h })
	})
	checkRow(c, &st.HorzGrid, "水平网格线", func() {
		a.well(c, "水平网格线颜色", st.HorzColor.or(k.grid), func(h hexColor) { st.HorzColor = h })
	})
	row(c, "十字光标", func() {
		a.well(c, "十字光标颜色", st.Cross.or(k.cross), func(h hexColor) { st.Cross = h })
		a.dropdown(c, "十字光标线型", crossStyles, st.CrossStyle, func(v string) { st.CrossStyle = v })
	})
	checkRow(c, &st.Watermark, "水印", nil)
	section(c, t, "坐标")
	row(c, "文字", func() {
		a.well(c, "坐标文字颜色", st.TextColor.or(k.muted), func(h hexColor) { st.TextColor = h })
		a.dropdown(c, "坐标文字大小", textSizes, strconv.Itoa(int(st.TextSize)), func(v string) { st.TextSize, _ = strconv.ParseFloat(v, 64) })
	})
	section(c, t, "边距")
	margin := func(name string, v *float64, hi float64, unit string) {
		row(c, name, func() {
			ui.NumberInput(c.Key("margin-"+name), v, 0, hi, 1).Width(170).Shrink(0).Label(name)
			ui.Text(c, unit).FontSize(13).TextColor(t.TextMuted)
		})
	}
	margin("顶部", &st.TopMargin, 40, "%")
	margin("底部", &st.BottomMargin, 60, "%")
	margin("右侧", &st.RightBars, 200, "根K线")
}
