package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The indicators on screen, after TradingView's: 指标 in the top bar lists
// the kinds to add; each indicator has a row in the legend of its pane,
// reading out the bar under the crosshair, with buttons, on hover, to set
// it up, hide it and delete it.

var (
	fxIcon   = svg("<path d='M3 13.5c1.6 0 2.1-.9 2.4-2.6l1.2-6.8C6.9 2.4 7.5 2 9 2'/><path d='M4 6.5h4.4M10 8.5l3.5 5M13.5 8.5l-3.5 5'/>")
	plusIcon = svg("<path d='M8 3v10M3 8h10'/>")
)

// fixedText is spans on one line at the width they take now: MyGo lays a
// frame out with the widths of the last one, and text grown since would wrap.
func fixedText(c *ui.Context, spans ...ui.Span) ui.Element {
	w, _ := c.MeasureText(0, spans...)
	return ui.RichText(c, spans...).SingleLine().Shrink(0).Width(float32(math.Ceil(float64(w))) + 1)
}

func (a *application) indicatorByID(id int64) *indicator {
	for i := range a.indicators {
		if a.indicators[i].ID == id {
			return &a.indicators[i]
		}
	}
	return nil
}

func (a *application) addIndicator(k *indKind) {
	var id int64
	for _, ind := range a.indicators {
		id = max(id, ind.ID)
	}
	a.indicators = append(slices.Clone(a.indicators), newIndicator(k, id+1, a.indicators))
	a.save()
}

func (a *application) deleteIndicator(id int64) {
	a.indicators = slices.DeleteFunc(slices.Clone(a.indicators), func(ind indicator) bool { return ind.ID == id })
	delete(a.series, id)
	delete(a.paneViews, id)
	if a.indEdit == id {
		a.indDialog, a.indEditing, a.indEdit = false, false, 0
	}
	a.save()
}

// indicatorsButton opens the list of indicators to add.
func (a *application) indicatorsButton(c *ui.Context, t *ui.Theme, k palette) {
	b := ui.Row(c.Key("indicators")).Height(28).Padding(0, 10).Margin(0, 6).Gap(6).AlignItems(ui.Center).Radius(4).
		Cursor(ui.CursorPointer).Label("指标").Tooltip("添加指标")
	switch {
	case a.indList:
		b.Background(k.chosen)
	case b.Hovered():
		b.Background(k.hover)
	}
	b.Children(func() {
		ui.Icon(c, fxIcon).Size(16, 16).TextColor(t.Text)
		fixedText(c, ui.Span{Text: "指标", Size: 13, Weight: 500, Color: t.Text})
	})
	if b.Clicked() {
		a.indList, a.indFocus, a.indQuery = true, true, ""
	}
}

// legends are, over the candles, the market, bar i read out and a row for
// each indicator drawn over them, and at the top of each indicator's pane
// its row.
func (a *application) legends(c *ui.Context, t *ui.Theme, k palette, i int) {
	title, values := a.legendSpans(k, i)
	ui.Column(c.Key("legend")).Absolute().Top(8).Left(10).Gap(2).PassThrough().Children(func() {
		if len(title) > 0 {
			ui.Row(c).Gap(6).Margin(0, 0, 2, 4).AlignItems(ui.Center).PassThrough().Children(func() {
				if a.set.Chart.Logo {
					a.symIcon(c, a.symbol, 18).PassThrough()
				}
				fixedText(c, title...).PassThrough()
			})
		}
		if len(values) > 0 {
			fixedText(c, values...).Margin(0, 0, 2, 4).PassThrough()
		}
		for j := range a.indicators {
			if kind, ok := kindOf(a.indicators[j].Kind); ok && !kind.pane {
				a.legendRow(c, t, k, &a.indicators[j], kind, i)
			}
		}
	})
	_, panes := a.layout(a.boxH - timeH)
	for _, pn := range panes {
		ind := &a.indicators[pn.idx]
		kind, _ := kindOf(ind.Kind)
		ui.Column(c.Key(fmt.Sprintf("legend-%d", ind.ID))).Absolute().Top(pn.y + 4).Left(10).PassThrough().Children(func() {
			a.legendRow(c, t, k, ind, kind, i)
		})
	}
}

// legendRow is ind's row in a legend: its name, parameters and values at
// bar i in its lines' colors and, on hover, its buttons. Hidden, it is
// grey and keeps the button to show it.
func (a *application) legendRow(c *ui.Context, t *ui.Theme, k palette, ind *indicator, kind *indKind, i int) {
	id := ind.ID
	row := ui.Row(c.Key(fmt.Sprintf("ind-%d", id))).Height(20).Padding(0, 4).Gap(2).AlignItems(ui.Center).Radius(4).
		Label(kind.short + " " + ind.paramText())
	hovered := row.Hovered()
	if hovered {
		row.Background(a.set.Chart.Background.or(k.bg)).Border(1, k.line)
	}
	row.Children(func() {
		name := t.Text
		if ind.Hidden {
			name = t.TextMuted.Alpha(0.6)
		}
		// The buttons go between the name and the values, as TradingView's:
		// the values change as the pointer moves to a button, the name does not.
		fixedText(c, ui.Span{Text: kind.short, Size: 12, Weight: 500, Color: name}, span(" "+ind.paramText(), 12, t.TextMuted, 0)).Margin(0, 4, 0, 0)
		if hovered || ind.Hidden {
			label, icon := "隐藏指标", eyeIcon
			if ind.Hidden {
				label, icon = "显示指标", eyeOffIcon
			}
			if legendButton(c, t, k, fmt.Sprintf("ind-eye-%d", id), label, icon).Clicked() {
				ind.Hidden = !ind.Hidden
				a.save()
			}
		}
		if hovered {
			if legendButton(c, t, k, fmt.Sprintf("ind-gear-%d", id), "指标设置", gearIcon).Clicked() {
				a.openIndicator(id)
			}
			if legendButton(c, t, k, fmt.Sprintf("ind-del-%d", id), "删除指标", clearIcon).Clicked() {
				a.afterBuild(func() { a.deleteIndicator(id) })
			}
		}
		if ind.Hidden {
			return
		}
		var spans []ui.Span
		s := a.seriesFor(ind)
		for j, pl := range kind.plots {
			v := s.at(j, i-ind.shift())
			text, col := a.valueText(kind, v), ind.color(kind, j)
			if pl.style != plotLine && !math.IsNaN(v) {
				col = a.plotBarColor(ind, kind, j, s.out[j], i).Alpha(1)
			}
			if len(kind.plots) > 1 {
				text = pl.name + " " + text
			}
			if j > 0 {
				text = "  " + text
			}
			spans = append(spans, span(text, 12, col, 0))
		}
		fixedText(c, spans...).Margin(0, 4)
	})
}

func legendButton(c *ui.Context, t *ui.Theme, k palette, key, label string, icon *ui.SVG) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(20, 20).Shrink(0).Center().Radius(4).Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(k.hover)
	}
	return b.Children(func() { ui.Icon(c, icon).Size(13, 13).TextColor(t.TextMuted) })
}

// dialogHeader is a dialog's title and its close button, named closeLabel.
func dialogHeader(c *ui.Context, t *ui.Theme, k palette, title, closeLabel string, close func()) {
	ui.Row(c).FillWidth().Height(56).Shrink(0).Padding(0, 14, 0, 24).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(18).FontWeight(600).Grow(1)
		x := ui.ButtonBase(c.Key(closeLabel)).Size(30, 30).Center().Radius(6).Cursor(ui.CursorPointer).Label(closeLabel)
		if x.Hovered() {
			x.Background(k.hover)
		}
		if x.Children(func() { ui.Icon(c, clearIcon).Size(14, 14).TextColor(t.TextMuted) }).Clicked() {
			close()
		}
	})
}

// searchKinds are the kinds whose names hold query, all for none.
func searchKinds(query string) []*indKind {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []*indKind
	for i := range indKinds {
		k := &indKinds[i]
		if q == "" || strings.Contains(strings.ToLower(k.key+" "+k.short+" "+k.name+" "+k.alias), q) {
			out = append(out, k)
		}
	}
	return out
}

// indicatorsDialog lists the kinds, those over the candles first; a click
// adds one, the dialog staying open to add more.
func (a *application) indicatorsDialog(c *ui.Context, t *ui.Theme, k palette) {
	if !a.indList {
		return
	}
	_, wh := c.Size()
	ui.DialogBase(c.Key("indicators-dialog"), &a.indList, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(520).Height(min(560, wh-48)).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label("添加指标")
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			dialogHeader(c, t, k, "指标", "关闭指标列表", func() { a.indList = false })
			hits := searchKinds(a.indQuery)
			ui.Row(c).FillWidth().Shrink(0).Padding(0, 24, 10, 24).Children(func() {
				ui.Row(c).FillWidth().Height(34).Padding(0, 10).Gap(6).AlignItems(ui.Center).Radius(6).Background(k.field).Children(func() {
					ui.Icon(c, searchIcon).Size(14, 14).TextColor(t.TextMuted).Shrink(0)
					in := ui.TextInput(c.Key("ind-query"), &a.indQuery).Grow(1).MinWidth(0).Background(ui.Transparent).Border(0, ui.Transparent).
						Padding(0).FontSize(13).Placeholder("搜索指标，如 MA、MACD、布林").Label("搜索指标")
					if a.indFocus {
						in.Focus()
						a.indFocus = false
					}
					if in.Submitted() && len(hits) > 0 {
						first := hits[0]
						a.afterBuild(func() { a.addIndicator(first) })
					}
				})
			})
			ui.Scroll(c.Key("ind-kinds")).Grow(1).FillWidth().MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Padding(0, 16, 16, 16).Children(func() {
					if len(hits) == 0 {
						ui.Text(c, "没有找到「"+strings.TrimSpace(a.indQuery)+"」").FontSize(13).TextColor(t.TextMuted).Padding(12, 8)
						return
					}
					for _, pane := range []bool{false, true} {
						shown := false
						for _, kind := range hits {
							if kind.pane != pane {
								continue
							}
							if !shown {
								section(c, t, map[bool]string{false: "主图（叠加在 K 线上）", true: "副图（K 线下方单独一栏）"}[pane])
								shown = true
							}
							a.kindRow(c, t, k, kind)
						}
					}
				})
			})
		})
	})
}

func (a *application) kindRow(c *ui.Context, t *ui.Theme, k palette, kind *indKind) {
	count := 0
	for _, ind := range a.indicators {
		if ind.Kind == kind.key {
			count++
		}
	}
	row := ui.Row(c.Key("kind-"+kind.key)).FillWidth().Height(44).Shrink(0).Padding(0, 12).Gap(12).AlignItems(ui.Center).
		Radius(6).Cursor(ui.CursorPointer).Label("添加 " + kind.short)
	if row.Hovered() {
		row.Background(k.hover)
	}
	row.Children(func() {
		ui.Text(c, kind.short).FontSize(13).FontWeight(600).Width(56).Shrink(0).SingleLine()
		ui.Text(c, kind.name).FontSize(13).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
		if count > 0 {
			fixedText(c, span(fmt.Sprintf("已添加 %d", count), 12, k.accent, 0))
		}
		ui.Icon(c, plusIcon).Size(14, 14).TextColor(t.TextMuted).Shrink(0)
	})
	if row.Clicked() {
		a.afterBuild(func() { a.addIndicator(kind) })
	}
}

func (a *application) openIndicator(id int64) {
	if ind := a.indicatorByID(id); ind != nil {
		a.indEdit, a.indBefore, a.indDialog, a.indEditing = id, cloneIndicator(*ind), true, true
	}
}

// closeIndicator keeps the indicator as set, or puts it back as it was.
func (a *application) closeIndicator(keep bool) {
	if ind := a.indicatorByID(a.indEdit); ind != nil && !keep {
		*ind = cloneIndicator(a.indBefore)
	}
	a.indDialog, a.indEditing, a.indEdit = false, false, 0
	a.save()
}

// indicatorDialog sets an indicator up: its parameters, and its lines'
// colors and widths. As the chart settings, what changes shows at once.
func (a *application) indicatorDialog(c *ui.Context, t *ui.Theme, k palette) {
	if a.indEditing && !a.indDialog { // closed by Escape or a click beside it
		a.closeIndicator(false)
	}
	if !a.indDialog {
		return
	}
	ind := a.indicatorByID(a.indEdit)
	if ind == nil {
		a.indDialog, a.indEditing = false, false
		return
	}
	kind, _ := kindOf(ind.Kind)
	id := strconv.FormatInt(ind.ID, 10)
	_, wh := c.Size()
	ui.DialogBase(c.Key("indicator-dialog"), &a.indDialog, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(480).MaxHeight(wh-48).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label(kind.short + " 设置")
		ui.Column(c).FillWidth().Children(func() {
			dialogHeader(c, t, k, kind.short+" · "+kind.name, "关闭指标设置", func() { a.closeIndicator(false) })
			ui.Column(c).FillWidth().Padding(0, 28, 16, 24).Children(func() {
				section(c, t, "参数")
				for j, pd := range kind.params {
					row(c, pd.name, func() {
						ui.NumberInput(c.Key("ind-param-"+id+"-"+strconv.Itoa(j)), &ind.Params[j], pd.lo, pd.hi, pd.step).
							Width(170).Shrink(0).Label(kind.short + " " + pd.name)
					})
				}
				if kind.source {
					row(c, "源", func() {
						a.dropdown(c, kind.short+" 源", sources, ind.Source, func(v string) { ind.Source = v }).Width(170).MinWidth(170)
					})
				}
				if kind.offset {
					row(c, "偏移", func() {
						ui.NumberInput(c.Key("ind-offset-"+id), &ind.Offset, -maxOffset, maxOffset, 1).Width(170).Shrink(0).Label(kind.short + " 偏移")
						ui.Text(c, "根K线").FontSize(13).TextColor(t.TextMuted)
					})
				}
				section(c, t, "样式")
				for j, pl := range kind.plots {
					key := "ind-" + id + "-" + strconv.Itoa(j)
					if pl.style != plotLine {
						up, down := "上涨", "下跌"
						if pl.style == plotHist {
							up, down = "零轴上方", "零轴下方"
						}
						row(c, pl.name, func() {
							a.well(c, key+"-up", ind.color(kind, j), func(h hexColor) { ind.Colors[j] = h }).Label(pl.name + up + "颜色").Tooltip(up)
							a.well(c, key+"-down", ind.downColor(kind, j), func(h hexColor) { ind.Downs[j] = h }).Label(pl.name + down + "颜色").Tooltip(down)
						})
						continue
					}
					row(c, pl.name, func() {
						a.well(c, key+"-color", ind.color(kind, j), func(h hexColor) { ind.Colors[j] = h }).Label(pl.name + "线色").Tooltip("线色")
						widthButtons(c, t, k, key+"-width", ind.Widths[j], func(w float64) { ind.Widths[j] = w })
					})
				}
			})
			ui.Row(c).FillWidth().Height(64).Shrink(0).Padding(0, 20).Gap(10).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(k.line).Children(func() {
				if ui.Button(c, "恢复默认").Clicked() {
					fresh := indicator{ID: ind.ID, Kind: ind.Kind, Hidden: ind.Hidden, Height: ind.Height}
					fresh.fix()
					*ind = fresh
				}
				ui.Box(c).Grow(1)
				if ui.Button(c, "取消").Clicked() {
					a.closeIndicator(false)
				}
				if ui.PrimaryButton(c, "确认").Clicked() {
					a.closeIndicator(true)
				}
			})
		})
	})
}

// widthButtons are buttons for line widths 1 to 4, each drawing its width;
// set takes the one clicked.
func widthButtons(c *ui.Context, t *ui.Theme, k palette, key string, cur float64, set func(float64)) {
	for _, w := range []float64{1, 2, 3, 4} {
		b := ui.ButtonBase(c.Key(fmt.Sprintf("%s-%g", key, w))).Size(28, 28).Center().Radius(4).Cursor(ui.CursorPointer).
			Label(fmt.Sprintf("线宽 %g", w)).Tooltip(fmt.Sprintf("线宽 %g", w))
		switch {
		case cur == w:
			b.Background(k.chosen)
		case b.Hovered():
			b.Background(k.hover)
		}
		b.Children(func() { ui.Box(c).Width(14).Height(float32(w)).Background(t.Text) })
		if b.Clicked() && cur != w {
			set(w)
		}
	}
}
