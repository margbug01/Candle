package main

import (
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

// The drawing tools, after TradingView's: a column of tools on the left of
// the chart, and, over the chart, a bar for the selected drawing's color,
// width, text, price, a copy, the lock, and to delete it. A tool draws one
// drawing, by clicks or by a drag, then the cursor comes back. Drawings
// belong to a market and keep to their moments on every bar size. What
// changes them can be undone (Ctrl+Z) and redone (Ctrl+Y, Ctrl+Shift+Z).

var (
	cursorIcon = svg("<path d='M4 2.5v10l2.8-2.6 1.8 4 1.7-.8-1.8-3.9H12z'/>")
	magnetIcon = svg("<path d='M3.5 2.5h3v5a1.5 1.5 0 0 0 3 0v-5h3v5a4.5 4.5 0 0 1-9 0z'/><path d='M3.5 5h3M9.5 5h3'/>")
	eyeIcon    = svg("<path d='M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8 12.1 12.5 8 12.5 1.5 8 1.5 8z'/><circle cx='8' cy='8' r='2'/>")
	eyeOffIcon = svg("<path d='M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8 12.1 12.5 8 12.5 1.5 8 1.5 8z'/><circle cx='8' cy='8' r='2'/><path d='M2.5 2.5l11 11'/>")
	trashIcon  = svg("<path d='M2.5 4.5h11M6.5 4.5V3h3v1.5M4 4.5l.7 9h6.6l.7-9'/>")
	copyIcon   = svg("<rect x='5.5' y='5.5' width='8' height='8' rx='1.2'/><path d='M10.5 3.5v-.3a.7.7 0 0 0-.7-.7H3.2a.7.7 0 0 0-.7.7v6.6a.7.7 0 0 0 .7.7h.3'/>")
	lockIcon   = svg("<rect x='3' y='7' width='10' height='7' rx='1.2'/><path d='M5.5 7V5a2.5 2.5 0 0 1 5 0v2'/>")
	unlockIcon = svg("<rect x='3' y='7' width='10' height='7' rx='1.2'/><path d='M5.5 7V5a2.5 2.5 0 0 1 4.9-.7'/>")
)

const (
	toolsW    = 44
	undoLimit = 100 // steps back the drawings remember
)

// drawTextSizes are the sizes a text can take.
var drawTextSizes = []option{{"12", "12"}, {"14", "14"}, {"16", "16"}, {"20", "20"}, {"24", "24"}, {"32", "32"}}

// mapper lays anchors out as the last frame did.
func (a *application) mapper() mapper {
	v := a.view
	v.clamp(a.plotW, len(a.bars))
	return mapper{v: v, w: a.plotW, h: a.plotH, bars: a.bars, iv: a.intervalMs(), sc: a.shown, ax: a.axis}
}

// mapReady reports whether a frame has laid the chart out to draw on.
func (a *application) mapReady() bool {
	return len(a.bars) > 0 && a.plotW > 0 && a.axis.h > 0 && a.shown.hi > a.shown.lo
}

func (a *application) selected() *drawing {
	if a.sel == 0 {
		return nil
	}
	ds := a.set.Drawings[a.symbol]
	for i := range ds {
		if ds[i].ID == a.sel {
			return &ds[i]
		}
	}
	return nil
}

func (a *application) newID() int64 {
	a.nextID++
	return a.nextID
}

// pickTool takes up a tool, or puts it down when it is the one in hand.
func (a *application) pickTool(kind string) {
	a.creating, a.sel = nil, 0
	if a.tool == kind {
		kind = ""
	}
	a.tool = kind
}

func cloneDrawings(ds []drawing) []drawing {
	out := make([]drawing, len(ds))
	for i, d := range ds {
		out[i] = d
		out[i].Pts = slices.Clone(d.Pts)
	}
	return out
}

func (a *application) setDrawings(ds []drawing) {
	if len(ds) == 0 {
		delete(a.set.Drawings, a.symbol)
		return
	}
	if a.set.Drawings == nil {
		a.set.Drawings = map[string][]drawing{}
	}
	a.set.Drawings[a.symbol] = ds
}

// remember keeps the market's drawings as they are, for 撤销, before they
// change. Changes under one key in a row, as typing a text or dragging
// through colors, make one step.
func (a *application) remember(key string) {
	if key != "" && key == a.lastEdit {
		return
	}
	a.lastEdit = key
	a.pushUndo(cloneDrawings(a.set.Drawings[a.symbol]))
}

func (a *application) pushUndo(ds []drawing) {
	if len(a.undo) == undoLimit {
		a.undo = a.undo[1:]
	}
	a.undo, a.redo = append(a.undo, ds), nil
}

func (a *application) undoDrawings() { a.step(&a.undo, &a.redo) }
func (a *application) redoDrawings() { a.step(&a.redo, &a.undo) }

// step puts back the drawings from's last entry kept, keeping the present
// ones on to.
func (a *application) step(from, to *[][]drawing) {
	n := len(*from)
	if n == 0 {
		return
	}
	*to = append(*to, cloneDrawings(a.set.Drawings[a.symbol]))
	a.setDrawings((*from)[n-1])
	*from = (*from)[:n-1]
	a.lastEdit, a.creating = "", nil
	if a.drag == dragCreate || a.drag == dragDrawing {
		a.drag = dragNone
	}
	if a.selected() == nil {
		a.sel = 0
	}
	a.save()
}

// add keeps a new drawing and selects it.
func (a *application) add(d drawing) {
	a.remember("")
	a.setDrawings(append(a.set.Drawings[a.symbol], d))
	a.sel = d.ID
	a.save()
}

// finish keeps a drawing just drawn, selects it and goes back to the cursor.
func (a *application) finish(d drawing) {
	a.add(d)
	a.creating, a.tool, a.drag = nil, "", dragNone
	a.editText = d.Kind == "text"
}

func (a *application) deleteSelected() {
	if a.selected() == nil {
		return
	}
	a.remember("")
	a.setDrawings(slices.DeleteFunc(a.set.Drawings[a.symbol], func(d drawing) bool { return d.ID == a.sel }))
	a.sel = 0
	a.save()
}

func (a *application) clearDrawings() {
	if len(a.set.Drawings[a.symbol]) > 0 {
		a.remember("")
	}
	delete(a.set.Drawings, a.symbol)
	a.sel, a.creating = 0, nil
	a.save()
}

// duplicate adds a copy of src, unlocked, three bars right of it and 16
// DIPs lower, and selects it.
func (a *application) duplicate(src drawing) (drawing, bool) {
	if !a.mapReady() {
		return drawing{}, false
	}
	d := src
	d.ID, d.Locked = a.newID(), false
	d.Pts = a.mapper().move(src.Pts, 3, 16)
	a.add(d)
	return d, true
}

// copySelected keeps the selected drawing for Ctrl+V.
func (a *application) copySelected() {
	if d := a.selected(); d != nil {
		cp := *d
		cp.Pts = slices.Clone(d.Pts)
		a.clip, a.clipSym = &cp, a.symbol
	}
}

// pasteClip adds a copy of the copied drawing; pasting again steps on from
// the last copy. A copy stays on its market: another market's prices would
// put it out of sight.
func (a *application) pasteClip() {
	if a.clip == nil || a.clipSym != a.symbol {
		return
	}
	if d, ok := a.duplicate(*a.clip); ok {
		d.Pts = slices.Clone(d.Pts)
		a.clip = &d
	}
}

// drawInput takes the pointer for the drawings, before the chart pans or
// zooms: with a tool in hand a press places the next point (a drag places
// two), and with the cursor a press on a drawing selects it and drags it, or
// one of its points; a locked one stays put and the press pans the chart.
// It reports whether it took the event.
func (a *application) drawInput(ev ui.InputEvent, inPlot bool) bool {
	if !a.mapReady() {
		return false
	}
	m := a.mapper()
	snap := func() point { return m.snap(ev.X, ev.Y, a.set.Magnet) }
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || !inPlot {
			return false
		}
		if a.creating != nil {
			a.place(snap())
			a.drag, a.pressX, a.pressY, a.dragged = dragCreate, ev.X, ev.Y, false
			return true
		}
		if isPosition(a.tool) { // one click places the whole box
			a.finish(position(a.tool, a.newID(), snap(), m))
			return true
		}
		if a.tool != "" {
			tool, _ := toolOf(a.tool)
			d := drawing{ID: a.newID(), Kind: a.tool, Pts: make([]point, tool.points), Width: 2}
			if d.Kind == "fib" {
				d.Width = 1
			}
			if d.Kind == "text" {
				d.Text = "文字"
			}
			for i := range d.Pts {
				d.Pts[i] = snap()
			}
			if tool.points == 1 {
				a.finish(d)
				return true
			}
			a.creating, a.stage = &d, 1
			a.drag, a.pressX, a.pressY, a.dragged = dragCreate, ev.X, ev.Y, false
			return true
		}
		d, h := a.hitDrawing(m, ev.X, ev.Y)
		if d == nil {
			a.sel = 0
			return false
		}
		a.sel = d.ID
		if ev.Clicks >= 2 {
			a.editText = d.Kind == "text"
			return true
		}
		if d.Locked {
			return false
		}
		a.drag, a.handle, a.pressX, a.pressY = dragDrawing, h, ev.X, ev.Y
		a.orig, a.dragBefore = slices.Clone(d.Pts), cloneDrawings(a.set.Drawings[a.symbol])
	case ui.InputPointerMove:
		switch {
		case a.creating != nil:
			if a.drag == dragCreate && ev.Button == 0 && math.Hypot(float64(ev.X-a.pressX), float64(ev.Y-a.pressY)) > 4 {
				a.dragged = true
			}
			pt := snap()
			for i := a.stage; i < len(a.creating.Pts); i++ {
				a.creating.Pts[i] = pt
			}
		case a.drag == dragDrawing:
			if ev.Button != 0 { // let go somewhere we did not hear of
				a.endDrag()
				return true
			}
			a.dragTo(m, ev.X, ev.Y)
		default:
			return false
		}
	case ui.InputPointerUp:
		switch {
		case a.creating != nil && a.drag == dragCreate:
			a.drag = dragNone
			if a.dragged { // drawn by dragging: letting go places the point
				a.place(snap())
			}
		case a.drag == dragDrawing:
			a.endDrag()
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// place puts the drawing's next point at pt, and keeps it once it has all.
func (a *application) place(pt point) {
	d := a.creating
	for i := a.stage; i < len(d.Pts); i++ {
		d.Pts[i] = pt
	}
	if a.stage++; a.stage == len(d.Pts) {
		a.finish(*d)
	}
}

// dragTo moves the selected drawing's point being dragged to x, y, or the
// whole drawing by whole bars along and as far up or down as the pointer went.
func (a *application) dragTo(m mapper, x, y float32) {
	d := a.selected()
	if d == nil || d.Locked {
		a.drag = dragNone
		return
	}
	if a.handle >= 0 {
		d.Pts[a.handle] = m.snap(x, y, a.set.Magnet)
		if isPosition(d.Kind) {
			fixPosition(d)
		}
		return
	}
	bars := int(math.Round(float64((x - a.pressX) / m.v.spacing)))
	copy(d.Pts, m.move(a.orig, bars, y-a.pressY))
}

// endDrag ends a drag of a drawing: one step to undo, when it moved.
func (a *application) endDrag() {
	a.drag = dragNone
	if d := a.selected(); d != nil && !slices.Equal(d.Pts, a.orig) {
		a.pushUndo(a.dragBefore)
		a.lastEdit = ""
	}
	a.dragBefore = nil
	a.save()
}

// toolsColumn is the column of drawing tools on the left of the chart.
func (a *application) toolsColumn(c *ui.Context, t *ui.Theme, k palette) {
	ui.Scroll(c.Key("tools")).Width(toolsW).FillHeight().Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(k.line).Children(func() {
		ui.Column(c).FillWidth().Padding(6, 0).Gap(2).AlignItems(ui.Center).Children(func() {
			if toolButton(c, t, k, "tool-cursor", "光标", cursorIcon, a.tool == "" && a.creating == nil).Clicked() {
				a.pickTool("")
			}
			for _, group := range [][]int{{0, 1, 2, 3, 4, 5}, {6}, {7, 8, 10, 11}, {9}} {
				toolSeparator(c, k)
				for _, i := range group {
					tl := drawTools[i]
					if toolButton(c, t, k, "tool-"+tl.kind, tl.label, tl.icon, a.tool == tl.kind).Clicked() {
						a.pickTool(tl.kind)
					}
				}
			}
			toolSeparator(c, k)
			magnetTip := "磁吸：关"
			if a.set.Magnet {
				magnetTip = "磁吸：开（靠近时吸到开高低收）"
			}
			if toolButton(c, t, k, "tool-magnet", "磁吸", magnetIcon, a.set.Magnet).Tooltip(magnetTip).Clicked() {
				a.set.Magnet = !a.set.Magnet
				a.save()
			}
			eye := eyeIcon
			if a.hide {
				eye = eyeOffIcon
			}
			if toolButton(c, t, k, "tool-hide", "隐藏画线", eye, a.hide).Clicked() {
				a.hide = !a.hide
				a.sel, a.creating = 0, nil
			}
			armed := time.Since(a.trashArmed) < 3*time.Second
			trash := toolButton(c, t, k, "tool-trash", "删除全部画线", trashIcon, false)
			if armed {
				trash.Background(k.down.Alpha(0.25)).Tooltip("再点一次，删除这个交易对的全部画线")
				c.After(3*time.Second - time.Since(a.trashArmed))
			}
			if trash.Clicked() {
				if armed {
					a.clearDrawings()
					a.trashArmed = time.Time{}
				} else if len(a.set.Drawings[a.symbol]) > 0 {
					a.trashArmed = time.Now()
				}
			}
		})
	})
}

func toolSeparator(c *ui.Context, k palette) {
	ui.Box(c).Width(24).Height(1).Shrink(0).Margin(4, 0).Background(k.line)
}

func toolButton(c *ui.Context, t *ui.Theme, k palette, key, tip string, icon *ui.SVG, on bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(32, 32).Shrink(0).Center().Radius(6).Cursor(ui.CursorPointer).Label(tip).Tooltip(tip)
	col := t.Text
	switch {
	case on:
		b.Background(k.chosen)
		col = k.accent
	case b.Hovered():
		b.Background(k.hover)
	}
	return b.Children(func() { ui.Icon(c, icon).Size(18, 18).TextColor(col) })
}

// barButton is an icon button of the drawing bar, named label, explained by tip.
func barButton(c *ui.Context, t *ui.Theme, k palette, key, label, tip string, icon *ui.SVG, on bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Shrink(0).Center().Radius(4).Cursor(ui.CursorPointer).Label(label).Tooltip(tip)
	col := t.TextMuted
	switch {
	case on:
		b.Background(k.chosen)
		col = k.accent
	case b.Hovered():
		b.Background(k.hover)
	}
	return b.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(col) })
}

// drawingBar floats over the chart while a drawing is selected.
func (a *application) drawingBar(c *ui.Context, t *ui.Theme, k palette) {
	d := a.selected()
	if d == nil || a.hide || a.creating != nil {
		a.barFor = 0
		return
	}
	if a.barFor != d.ID { // another drawing, or the same one again: its edits make steps of their own
		a.barFor, a.lastEdit = d.ID, ""
	}
	tool, _ := toolOf(d.Kind)
	// Centered over the plot by a row across it, which lets the pointer through to the chart.
	ui.Row(c.Key("drawing-bar-lane")).Absolute().Top(58).Left(0).Right(axisW).Justify(ui.Center).PassThrough().Children(func() {
		a.drawingBarItems(c, t, k, d, tool)
	})
}

func (a *application) drawingBarItems(c *ui.Context, t *ui.Theme, k palette, d *drawing, tool drawTool) {
	id := strconv.FormatInt(d.ID, 10)
	ui.Row(c.Key("drawing-bar")).Height(38).Padding(0, 6).Gap(4).AlignItems(ui.Center).
		Radius(6).Background(k.field).Border(1, k.line).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.25)).Children(func() {
		ui.Text(c, tool.label).FontSize(12).TextColor(t.TextMuted).Padding(0, 6, 0, 4).SingleLine()
		if d.Kind != "range" && !isPosition(d.Kind) { // those take the up and down colors
			a.well(c, "画线颜色", d.Color.or(k.accent), func(h hexColor) {
				a.remember("color:" + id)
				d.Color = h
				a.save()
			}).Tooltip("颜色")
		}
		switch d.Kind {
		case "text":
			p, picked, changed := a.textPick.bind(d.Text)
			if changed {
				a.remember("text:" + id)
				d.Text = picked
				a.save()
			}
			in := ui.TextInput(c.Key("drawing-text"), p).Width(150).FontSize(13).Label("文字内容")
			if a.editText {
				in.Focus()
				a.editText = false
			}
			a.dropdown(c, "文字大小", drawTextSizes, strconv.Itoa(int(textSize(d))), func(v string) {
				a.remember("")
				d.Size, _ = strconv.ParseFloat(v, 64)
				a.save()
			}).Width(76).MinWidth(76).Tooltip("文字大小") // Select keeps 140 at least otherwise
		case "range":
		case "long", "short":
			fixedText(c, span(ratioText(d), 12, t.Text, 600)).Margin(0, 6)
			// The account and the risk size the position the box reads out.
			for _, f := range []struct {
				key, name, tip, unit string
				pick                 *choice[float64]
				cur, lo, hi, step    float64
				set                  func(float64)
			}{
				{"account", "账户金额", "账户里有多少钱（按报价币计）", "账户", &a.accountPick, account(d), 1, 1e12, 100, func(v float64) { d.Account = v }},
				{"risk", "风险比例", "打到止损时亏掉账户的百分之几", "风险 %", &a.riskPick, riskPct(d), 0.01, 100, 0.5, func(v float64) { d.Risk = v }},
			} {
				p, picked, changed := f.pick.bind(f.cur)
				if changed && !d.Locked {
					a.remember(f.key + ":" + id)
					f.set(picked)
					a.save()
				}
				ui.Text(c, f.unit).FontSize(12).TextColor(t.TextMuted).Margin(0, 0, 0, 6).SingleLine()
				ui.NumberInput(c.Key("position-"+f.key), p, f.lo, f.hi, f.step).Width(130).ReadOnly(d.Locked).Label(f.name).Tooltip(f.tip)
			}
		default:
			widthButtons(c, t, k, "width", d.Width, func(w float64) {
				a.remember("")
				d.Width = w
				a.save()
			})
		}
		if d.Kind == "hline" || d.Kind == "hray" {
			p, picked, changed := a.pricePick.bind(d.Pts[0].P)
			if changed && !d.Locked {
				a.remember("price:" + id)
				d.Pts[0].P = picked
				a.save()
			}
			step := math.Pow(10, -float64(a.chartDecimals(d.Pts[0].P)))
			ui.NumberInput(c.Key("drawing-price"), p, -1e15, 1e15, step).Width(170).ReadOnly(d.Locked).Label("价格").Tooltip("价格")
		}
		if barButton(c, t, k, "drawing-copy", "复制画线", "复制一份（Ctrl+C、Ctrl+V）", copyIcon, false).Clicked() {
			a.duplicate(*d)
			return // d now points into the list before the copy was added
		}
		lockLabel, lockTip, icon := "锁定画线", "锁定：不能再拖动", unlockIcon
		if d.Locked {
			lockLabel, lockTip, icon = "解锁画线", "已锁定，点一下解锁", lockIcon
		}
		if barButton(c, t, k, "drawing-lock", lockLabel, lockTip, icon, d.Locked).Clicked() {
			a.remember("")
			d.Locked = !d.Locked
			a.save()
		}
		if barButton(c, t, k, "drawing-delete", "删除画线", "删除（Delete）", trashIcon, false).Clicked() {
			a.deleteSelected()
		}
	})
}

// drawingShortcuts: Escape puts the tool down, drops what is half drawn and
// unselects; Delete and Backspace delete the selected drawing; Ctrl+Z,
// Ctrl+Y, Ctrl+C and Ctrl+V undo, redo, copy and paste drawings. A text
// input with the focus takes all of those keys first, so typing edits text.
func (a *application) drawingShortcuts(c *ui.Context) {
	if a.dialog || a.indList || a.indDialog {
		return
	}
	if a.creating != nil || a.tool != "" || a.sel != 0 {
		c.OnShortcut(0, ui.KeyEscape, func() {
			a.creating, a.tool, a.sel = nil, "", 0
			if a.drag == dragCreate || a.drag == dragDrawing {
				a.drag = dragNone
			}
		})
	}
	if a.sel != 0 {
		c.OnShortcut(0, ui.KeyDelete, a.deleteSelected)
		c.OnShortcut(0, ui.KeyBackspace, a.deleteSelected)
	}
	if len(a.undo) > 0 {
		c.OnShortcut(ui.Ctrl, ui.KeyZ, a.undoDrawings)
	}
	if len(a.redo) > 0 {
		c.OnShortcut(ui.Ctrl, ui.KeyY, a.redoDrawings)
		c.OnShortcut(ui.Ctrl|ui.Shift, ui.KeyZ, a.redoDrawings)
	}
	if a.sel != 0 {
		c.OnShortcut(ui.Ctrl, ui.KeyC, a.copySelected)
	}
	if a.clip != nil && a.clipSym == a.symbol {
		c.OnShortcut(ui.Ctrl, ui.KeyV, a.pasteClip)
	}
}
