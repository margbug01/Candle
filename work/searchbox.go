package main

import (
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// The search box is Candle's own, not a TextInput: an input method's
// letters are a TextInput's only once chosen, so with Pinyin on, sndk found
// nothing until Enter, and with input methods turned off for it, 闪迪 could
// not be typed at all. This box sees the composition as it is typed
// (InputCompose) and searches it with the rest: sndk, ywd and shandi as they
// are typed, 闪迪 once chosen. It edits as a field does: the caret and a
// selection, by the keys, the pointer and the clipboard.

// searchBox is the box's editing of a.query: the caret and the selection,
// in runes, and an input method's composition at the caret.
type searchBox struct {
	caret, anchor int    // a selection between them
	compose       string // not the query's until chosen
	composeCaret  int    // in the composition's runes
	seen          string // the query as the box last had it, to tell when the app set it
	focused       bool
	dragging      bool
	scroll        float32   // how far left the text has moved, keeping the caret in sight
	xs            []float32 // where each rune of the text shown begins, and its end
	svc           ui.Services
	undo, redo    []searchEdit // the query before each edit, and after each undo
	lastEdit      string       // the kind of the last edit, typing running into one step
}

// A searchEdit is the query as it was, for Undo.
type searchEdit struct {
	q             string
	caret, anchor int
}

const searchFontSize = 13

var searchFont = ui.Font{Size: searchFontSize}

// The keys that move by a word and to an end of the line: Option and
// Command on macOS, Ctrl and Home or End elsewhere.
var wordMod, lineMod = func() (ui.Modifiers, ui.Modifiers) {
	if runtime.GOOS == "darwin" {
		return ui.Alt, ui.Super
	}
	return ui.Ctrl, 0
}()

// searchQuery is what the search looks for: the query with the composition
// at the caret, its syllables run together (shan'di is shandi).
func (a *application) searchQuery() string {
	b := &a.qb
	b.sync(a.query)
	if b.compose == "" {
		return strings.TrimSpace(a.query)
	}
	r := []rune(a.query)
	at := min(b.caret, len(r))
	comp := strings.Map(func(c rune) rune {
		if c == '\'' || c == '’' || unicode.IsSpace(c) {
			return -1
		}
		return c
	}, b.compose)
	return strings.TrimSpace(string(r[:at]) + comp + string(r[at:]))
}

// clearSearch empties the search, as picking a market or the box's ✕ does.
func (a *application) clearSearch() {
	a.query, a.searchKind = "", ""
	a.qb.sync("")
	a.qb.compose = ""
}

// sync puts the caret at the end of a query the app set, which Undo does
// not take back.
func (b *searchBox) sync(q string) {
	if q != b.seen {
		n := utf8.RuneCountInString(q)
		b.caret, b.anchor, b.seen = n, n, q
		b.undo, b.redo, b.lastEdit = nil, nil, ""
	}
	n := utf8.RuneCountInString(q)
	b.caret, b.anchor = min(b.caret, n), min(b.anchor, n)
}

func (b *searchBox) sel() (lo, hi int) { return min(b.caret, b.anchor), max(b.caret, b.anchor) }

// replace puts s in place of the selection, the caret after it. A line
// break or a tab pasted is a space; other control characters are dropped.
func (b *searchBox) replace(q *string, s string) {
	s = strings.Map(func(c rune) rune {
		switch {
		case c == '\n' || c == '\r' || c == '\t':
			return ' '
		case unicode.IsControl(c):
			return -1
		}
		return c
	}, s)
	r := []rune(*q)
	lo, hi := b.sel()
	*q = string(r[:lo]) + s + string(r[hi:])
	b.caret = lo + utf8.RuneCountInString(s)
	b.anchor, b.seen = b.caret, *q
}

// edit replaces the selection with s, as replace does, remembering the
// query for Undo: typing, or deleting, one key after another is one step.
func (b *searchBox) edit(q *string, s, kind string) {
	lo, hi := b.sel()
	b.editAt(q, lo, hi, s, kind)
}

// editAt is edit of runes lo to hi, as Backspace deletes the rune before
// the caret: Undo puts back the caret and the selection as they were.
func (b *searchBox) editAt(q *string, lo, hi int, s, kind string) {
	if s == "" && lo == hi {
		return
	}
	if kind != b.lastEdit || kind != "type" && kind != "delete" {
		b.undo = append(b.undo, searchEdit{*q, b.caret, b.anchor})
		if len(b.undo) > 100 {
			b.undo = b.undo[1:]
		}
	}
	b.redo, b.lastEdit = nil, kind
	b.anchor, b.caret = lo, hi
	b.replace(q, s)
}

// undoEdit takes the last edit back, or with redo the last undone again.
func (b *searchBox) undoEdit(q *string, redo bool) {
	from, to := &b.undo, &b.redo
	if redo {
		from, to = to, from
	}
	if len(*from) == 0 {
		return
	}
	e := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, searchEdit{*q, b.caret, b.anchor})
	*q, b.caret, b.anchor, b.seen, b.lastEdit = e.q, e.caret, e.anchor, e.q, ""
}

// move puts the caret at i, the selection stretching from the anchor if
// extend.
func (b *searchBox) move(q string, i int, extend bool) {
	b.lastEdit = "" // typing after the caret moved is another step
	b.caret = max(0, min(i, utf8.RuneCountInString(q)))
	if !extend {
		b.anchor = b.caret
	}
}

// wordBefore and wordAfter are where the word before or after rune i
// begins or ends, past the spaces next to i.
func wordBefore(r []rune, i int) int {
	for i > 0 && unicode.IsSpace(r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(r[i-1]) {
		i--
	}
	return i
}

func wordAfter(r []rune, i int) int {
	for i < len(r) && unicode.IsSpace(r[i]) {
		i++
	}
	for i < len(r) && !unicode.IsSpace(r[i]) {
		i++
	}
	return i
}

// searchInput takes the box's input as it comes.
func (a *application) searchInput(ev ui.InputEvent) bool {
	b := &a.qb
	b.sync(a.query)
	switch ev.Kind {
	case ui.InputText:
		b.compose, b.composeCaret = "", 0
		// Enter, Tab and Backspace come as characters too on Windows,
		// after their keys did what they do.
		if strings.TrimFunc(ev.Text, unicode.IsControl) != "" {
			b.edit(&a.query, ev.Text, "type")
		}
		return true
	case ui.InputCompose:
		if ev.Text != "" && b.compose == "" { // what is composed takes the selection's place
			b.edit(&a.query, "", "type")
		}
		b.compose, b.composeCaret = ev.Text, ev.Caret
		return true
	case ui.InputCommand:
		return a.searchCommand(ev.Text)
	case ui.InputKeyDown:
		return a.searchKey(ev)
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if b.compose == "" {
			if ev.Clicks >= 2 {
				b.anchor, b.caret = 0, utf8.RuneCountInString(a.query)
			} else {
				b.move(a.query, b.indexAt(ev.X), ev.Mods&ui.Shift != 0)
			}
		}
		b.dragging = true
		return true
	case ui.InputPointerMove:
		if b.dragging && ev.Button == 0 && b.compose == "" {
			b.move(a.query, b.indexAt(ev.X), true)
		}
		return b.dragging
	case ui.InputPointerUp:
		was := b.dragging
		b.dragging = false
		return was
	}
	return false
}

// searchCommand does an edit command, from a menu or its keys.
func (a *application) searchCommand(cmd string) bool {
	b := &a.qb
	lo, hi := b.sel()
	switch cmd {
	case "selectAll":
		b.anchor, b.caret = 0, utf8.RuneCountInString(a.query)
	case "copy", "cut":
		if lo == hi {
			return true
		}
		b.svc.WriteClipboard(string([]rune(a.query)[lo:hi]))
		if cmd == "cut" {
			b.edit(&a.query, "", "cut")
		}
	case "paste", "pasteAndMatchStyle":
		if s := b.svc.ReadClipboard(); s != "" {
			b.edit(&a.query, s, "paste")
		}
	case "delete":
		b.edit(&a.query, "", "cut")
	case "undo", "redo":
		b.undoEdit(&a.query, cmd == "redo")
	default:
		return false
	}
	return true
}

// searchKey takes the keys that edit and move: those typing text come as
// text after them, and while an input method composes, the keys are its own.
func (a *application) searchKey(ev ui.InputEvent) bool {
	b := &a.qb
	if b.compose != "" { // the platforms keep them from us; should one come, it is still not ours
		return true
	}
	r := []rune(a.query)
	lo, hi := b.sel()
	shift := ev.Mods&ui.Shift != 0
	word := wordMod != 0 && ev.Mods&wordMod != 0
	line := lineMod != 0 && ev.Mods&lineMod != 0
	cmd := ev.Mods&ui.Cmd != 0
	switch ev.Key {
	case ui.KeyEnter:
		if sym := a.firstFinding(); sym != "" {
			a.pick(sym)
		}
	case ui.KeyEscape:
		if a.query == "" {
			return false
		}
		a.clearSearch()
	case ui.KeyBackspace, ui.KeyDelete:
		if lo == hi {
			back, at := ev.Key == ui.KeyBackspace, b.caret
			switch {
			case back && line:
				lo = 0
			case back && word:
				lo = wordBefore(r, at)
			case back:
				lo = max(0, at-1)
			case line:
				hi = len(r)
			case word:
				hi = wordAfter(r, at)
			default:
				hi = min(len(r), at+1)
			}
		}
		b.editAt(&a.query, lo, hi, "", "delete")
	case ui.KeyLeft:
		switch {
		case line:
			b.move(a.query, 0, shift)
		case word:
			b.move(a.query, wordBefore(r, b.caret), shift)
		case lo < hi && !shift:
			b.move(a.query, lo, false)
		default:
			b.move(a.query, b.caret-1, shift)
		}
	case ui.KeyRight:
		switch {
		case line:
			b.move(a.query, len(r), shift)
		case word:
			b.move(a.query, wordAfter(r, b.caret), shift)
		case lo < hi && !shift:
			b.move(a.query, hi, false)
		default:
			b.move(a.query, b.caret+1, shift)
		}
	case ui.KeyHome:
		b.move(a.query, 0, shift)
	case ui.KeyEnd:
		b.move(a.query, len(r), shift)
	case ui.KeyA, ui.KeyC, ui.KeyX, ui.KeyV, ui.KeyZ, ui.KeyY: // with no Edit menu taking them
		if !cmd || ev.Mods&ui.Alt != 0 {
			return false
		}
		name := map[ui.Key]string{ui.KeyA: "selectAll", ui.KeyC: "copy", ui.KeyX: "cut", ui.KeyV: "paste", ui.KeyZ: "undo", ui.KeyY: "redo"}[ev.Key]
		if ev.Key == ui.KeyZ && shift {
			name = "redo"
		}
		return a.searchCommand(name)
	default:
		return false
	}
	return true
}

// shown is the text the box shows, the composition at the caret, and where
// in it the caret and the selection are.
func (b *searchBox) shown(q string) (text string, caret, lo, hi int) {
	r := []rune(q)
	at := min(b.caret, len(r))
	lo, hi = b.sel()
	if b.compose == "" {
		return q, at, lo, hi
	}
	n := utf8.RuneCountInString(b.compose)
	return string(r[:at]) + b.compose + string(r[at:]), at + min(b.composeCaret, n), at, at
}

// lay finds where each rune of text begins, and its end.
func (b *searchBox) lay(text string) []ui.Glyph {
	g := ui.Shape(text, searchFont)
	n := utf8.RuneCountInString(text)
	xs := make([]float32, n+1)
	set := make([]bool, n+1)
	end := float32(0)
	for _, gl := range g {
		if c := gl.Cluster; c >= 0 && c <= n && !set[c] {
			xs[c], set[c] = gl.X, true
			// The runes of a cluster, as a ligature's, share its width.
			for k := 1; k < gl.Runes && c+k < n; k++ {
				xs[c+k], set[c+k] = gl.X+gl.Advance*float32(k)/float32(gl.Runes), true
			}
		}
		end = max(end, gl.X+gl.Advance)
	}
	xs[n], set[n] = end, true
	for i := n - 1; i >= 0; i-- { // a rune drawn with the one before: where the next begins
		if !set[i] {
			xs[i] = xs[i+1]
		}
	}
	b.xs = xs
	return g
}

// indexAt is the rune of the query nearest x, from the box's left.
func (b *searchBox) indexAt(x float32) int {
	x += b.scroll
	best := 0
	for i, at := range b.xs {
		if d, bd := abs32(at-x), abs32(b.xs[best]-x); d < bd {
			best = i
		}
	}
	return best
}

// searchField builds the box: the text, the composition underlined, the
// selection, the caret, and the placeholder while empty.
func (a *application) searchField(c *ui.Context, t *ui.Theme, k palette) ui.Element {
	b := &a.qb
	b.sync(a.query)
	b.svc = c.Services()
	el := ui.Box(c.Key("query")).Grow(1).MinWidth(0).FillHeight().Focusable().FocusRing(false).
		Role(ui.RoleTextField).Label("搜索交易对、期货、美股").Value(a.query).Cursor(ui.CursorText).
		HandleInput(a.searchInput)
	b.focused = el.Focused()
	if !b.focused { // input methods drop their composition with the focus
		b.compose, b.dragging = "", false
	}
	text, caret, lo, hi := b.shown(a.query)
	glyphs := b.lay(text)
	box := el.Bounds()
	w, h := box.W, box.H
	if h <= 0 {
		h = 32
	}
	m := searchFont.Metrics()
	lineH := m.Ascent + m.Descent
	top := (h - lineH) / 2
	// Scroll so the caret shows, and no further than the text needs.
	cx := b.xs[caret]
	if w > 0 {
		end := b.xs[len(b.xs)-1]
		b.scroll = max(0, min(b.scroll, end-w+2))
		if cx-b.scroll > w-2 {
			b.scroll = cx - w + 2
		}
		if cx < b.scroll {
			b.scroll = cx
		}
	}
	el.TextCaret(ui.Rect{X: cx - b.scroll, Y: top, W: 1, H: lineH})
	focused, scroll, composed := b.focused, b.scroll, utf8.RuneCountInString(b.compose)
	xs := b.xs
	place := ui.Shape("代码、拼音或中文：PEPE、NQ、英伟达", searchFont)
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		base := r.Y + top + m.Ascent
		p.Clip(r, 0, func() {
			x0 := r.X - scroll
			if text == "" {
				p.Glyphs(place, r.X, base, t.TextMuted)
			}
			if focused && lo < hi {
				p.Fill(ui.Rect{X: x0 + xs[lo], Y: r.Y + top, W: xs[hi] - xs[lo], H: lineH}, k.accent.Alpha(0.3), 0)
			}
			p.Glyphs(glyphs, x0, base, t.Text)
			if composed > 0 {
				p.Fill(ui.Rect{X: x0 + xs[lo], Y: base + 2, W: xs[lo+composed] - xs[lo], H: 1}, t.Text, 0)
			}
			if focused {
				p.Fill(ui.Rect{X: x0 + xs[caret], Y: r.Y + top, W: 1, H: lineH}, t.Text, 0)
			}
		})
	})
	return el
}
