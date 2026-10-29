package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"candle/pine"
)

// The editor of the user's scripts: Pine highlighted, worked out as it is
// typed, its errors underlined and told of with their line.

type scriptEditor struct {
	key     string // the script edited; "" for a new one
	st      *editor.State
	ver     uint64
	err     string // why it does not run, with the line
	script  *pine.Script
	saved   string    // what the file holds, to tell unsaved changes
	armed   time.Time // when 取消 or 删除 asked to be clicked again
	arming  string    // which
	message string    // what the last save did
}

// openEditor opens the script keyed key, or a new one holding src.
func (a *application) openEditor(key, src string) {
	if k := customs[key]; k != nil && k.custom != "" {
		data, err := os.ReadFile(k.custom)
		if err != nil {
			a.notify("读不了 "+filepath.Base(k.custom)+"："+err.Error(), "")
			return
		}
		src = string(data)
	} else {
		key = ""
	}
	buf := editor.NewBuffer(src)
	buf.SetLanguage(pineLanguage)
	ed := &scriptEditor{key: key, saved: src, ver: ^uint64(0)}
	if key == "" {
		ed.saved = ""
	}
	ed.st = &editor.State{Buffer: buf, Label: "Pine 代码", TabSize: 4, Indent: editor.IndentSpaces, SoftWrap: true, NoScrollPastEnd: true}
	ed.st.OnCompletion = func(r editor.CompletionRequest) { ed.st.ShowCompletions(r.Offset-len(r.Prefix), completions(r.Prefix)) }
	a.ed, a.indList = ed, false
}

func (a *application) closeEditor(force bool) {
	ed := a.ed
	if ed == nil {
		return
	}
	if !force && ed.st.Buffer.String() != ed.saved && !(ed.arming == "cancel" && time.Since(ed.armed) < 4*time.Second) {
		ed.arming, ed.armed = "cancel", time.Now()
		return
	}
	a.ed = nil
}

// check compiles the text when it changed.
func (ed *scriptEditor) check() {
	v := ed.st.Buffer.Version()
	if v == ed.ver {
		return
	}
	ed.ver = v
	src := ed.st.Buffer.String()
	s, err := pine.Compile(src)
	ed.script, ed.err = s, ""
	var diags []editor.Diagnostic
	if err != nil {
		ed.err = errorText(src, err)
		if e, ok := err.(*pine.Error); ok {
			diags = append(diags, editor.Diagnostic{Start: e.Start, End: max(e.End, e.Start+1), Severity: editor.SeverityError, Message: e.Msg})
		}
	} else {
		for _, w := range s.Warnings {
			diags = append(diags, editor.Diagnostic{Start: w.Start, End: max(w.End, w.Start+1), Severity: editor.SeverityWarning, Message: w.Msg})
		}
	}
	ed.st.SetDiagnostics(v, diags)
}

func (a *application) editorDialog(c *ui.Context, t *ui.Theme, k palette) {
	ed := a.ed
	if ed == nil {
		return
	}
	ed.check()
	open := true
	ww, wh := c.Size()
	title := "新建指标"
	if kind := customs[ed.key]; kind != nil {
		title = "编辑指标 · " + filepath.Base(kind.custom)
	}
	ui.DialogBase(c.Key("script-editor"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(min(980, ww-60)).Height(min(760, wh-40)).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label(title)
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			dialogHeader(c, t, k, title, "关闭编辑器", func() { a.closeEditor(false) })
			ui.Box(c).FillWidth().Grow(1).MinHeight(0).Margin(0, 16).Radius(6).Border(1, k.line).Clip().Children(func() {
				editor.View(c, ed.st).FillWidth().FillHeight()
			})
			a.editorStatus(c, t, k, ed)
			a.editorButtons(c, t, k, ed)
		})
	})
	if !open {
		a.closeEditor(false)
	}
	if ed.arming != "" {
		if left := 4*time.Second - time.Since(ed.armed); left > 0 {
			c.After(left)
		} else {
			ed.arming = ""
		}
	}
}

func (a *application) editorStatus(c *ui.Context, t *ui.Theme, k palette, ed *scriptEditor) {
	ui.Row(c).FillWidth().MinHeight(34).Padding(8, 20, 0, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		switch {
		case ed.err != "":
			ui.Text(c, ed.err).FontSize(12).TextColor(k.down).Grow(1).MinWidth(0)
		case ed.script != nil:
			s := ed.script
			where := "画在 K 线上"
			if !s.Overlay {
				where = "画在 K 线下方单独一栏"
			}
			text := "能运行 · " + where
			if n := len(s.Inputs); n > 0 {
				text += " · " + itoa(n) + " 个参数"
			}
			if len(s.Warnings) > 0 {
				text += " · " + s.Warnings[0].Msg
			}
			ui.Text(c, text).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0)
		}
		if ed.message != "" {
			ui.Text(c, ed.message).FontSize(12).TextColor(k.accent).Shrink(0)
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func (a *application) editorButtons(c *ui.Context, t *ui.Theme, k palette, ed *scriptEditor) {
	kind := customs[ed.key]
	onChart := false
	for _, ind := range a.set.Indicators {
		onChart = onChart || ind.Kind == ed.key && ed.key != ""
	}
	armed := func(what string) bool { return ed.arming == what && time.Since(ed.armed) < 4*time.Second }
	ui.Row(c).FillWidth().Height(64).Shrink(0).Padding(0, 20).Gap(10).AlignItems(ui.Center).Children(func() {
		if kind != nil {
			del := "删除"
			if armed("delete") {
				del = "再点一次删除"
			}
			b := ui.Button(c, del).Label("删除指标")
			if armed("delete") {
				b.TextColor(k.down)
			}
			if b.Clicked() {
				if armed("delete") {
					key := ed.key
					a.later = append(a.later, func() {
						if err := a.deleteScript(key); err != nil {
							ed.message = "没删掉：" + err.Error()
							return
						}
						a.ed = nil
					})
				} else {
					ed.arming, ed.armed = "delete", time.Now()
				}
			}
			if ui.Button(c, "在文件夹中显示").Clicked() {
				showInFolder(kind.custom)
			}
		}
		ui.Box(c).Grow(1)
		cancel := "取消"
		if armed("cancel") {
			cancel = "放弃修改"
		}
		if ui.Button(c, cancel).Clicked() {
			a.closeEditor(armed("cancel"))
		}
		save := func(add bool) {
			src := ed.st.Buffer.String()
			key, err := a.saveScript(ed.key, src)
			if err != nil {
				ed.message = "没存上：" + err.Error()
				return
			}
			ed.key, ed.saved = key, src
			k, _ := kindOf(key)
			ed.message = "已保存到 " + filepath.Base(k.custom)
			if add {
				if !onChart && k.broken == "" {
					a.addIndicator(k)
				}
				a.ed = nil
			}
		}
		if ui.Button(c, "保存").Label("保存指标").Clicked() {
			a.later = append(a.later, func() { save(false) })
		}
		if ed.err == "" {
			label := "保存并加到图上"
			if onChart {
				label = "保存并关闭"
			}
			if ui.PrimaryButton(c, label).Clicked() {
				a.later = append(a.later, func() { save(true) })
			}
		}
	})
}

var showInFolder = func(path string) { mygo.Shell.ShowItemInFolder(path) }

// completions are the built-in names that start as prefix does.
func completions(prefix string) []editor.Completion {
	if prefix == "" {
		return nil
	}
	var out []editor.Completion
	for _, n := range pineNames() {
		if !strings.HasPrefix(n, prefix) {
			continue
		}
		c := editor.Completion{Label: n, Kind: editor.CompletionVariable}
		if strings.HasSuffix(n, "(") {
			c.Label, c.Kind, c.Insert = strings.TrimSuffix(n, "("), editor.CompletionFunction, strings.TrimSuffix(n, "(")
		}
		out = append(out, c)
		if len(out) == 60 {
			break
		}
	}
	return out
}

var pineNames = func() func() []string {
	var names []string
	return func() []string {
		if names == nil {
			names = append(pine.Names(), "indicator(", "plot(", "hline(", "fill(", "bgcolor(", "plotshape(", "plotchar(")
		}
		return names
	}
}()

// The language: Pine Script's words, highlighted.

var (
	pineKeywords = map[string]bool{"if": true, "else": true, "for": true, "to": true, "by": true, "while": true, "switch": true,
		"var": true, "varip": true, "and": true, "or": true, "not": true, "break": true, "continue": true, "import": true,
		"export": true, "method": true, "type": true, "in": true}
	pineTypes = map[string]bool{"int": true, "float": true, "bool": true, "color": true, "string": true, "series": true,
		"simple": true, "const": true, "line": true, "label": true, "box": true, "table": true}
	pineConsts = map[string]bool{"true": true, "false": true, "na": true}
)

var pineLanguage = &editor.Language{
	Name: "Pine Script", Extensions: []string{".pine"}, LineComment: "//", TabSize: 4, WordChars: ".",
	Brackets:       []editor.BracketPair{{Open: "(", Close: ")"}, {Open: "[", Close: "]"}},
	AutoClose:      []editor.BracketPair{{Open: "(", Close: ")"}, {Open: "[", Close: "]"}, {Open: `"`, Close: `"`, NotIn: []editor.TokenKind{editor.TokenString}}},
	IncreaseIndent: regexp.MustCompile(`(=>\s*(//.*)?$)|(^\s*(if|else|for|while|switch)\b)`),
	Tokenizer:      editor.TokenizerFunc(tokenizePine),
}

func tokenizePine(line string, state uint64, toks []editor.Token) ([]editor.Token, uint64) {
	add := func(end int, kind editor.TokenKind) { toks = append(toks, editor.Token{End: end, Kind: kind}) }
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case strings.HasPrefix(line[i:], "//"):
			add(len(line), editor.TokenComment)
			return toks, 0
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(line) && line[j] != c {
				if line[j] == '\\' {
					j++
				}
				j++
			}
			i = min(j+1, len(line))
			add(i, editor.TokenString)
		case c == '#':
			j := i + 1
			for j < len(line) && isHexDigit(line[j]) {
				j++
			}
			i = j
			add(i, editor.TokenConstant)
		case c >= '0' && c <= '9':
			j := i
			for j < len(line) && (line[j] >= '0' && line[j] <= '9' || line[j] == '.' || line[j] == 'e' || line[j] == 'E') {
				j++
			}
			i = j
			add(i, editor.TokenNumber)
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(line) && (line[j] == '_' || line[j] == '.' || line[j] >= 'a' && line[j] <= 'z' || line[j] >= 'A' && line[j] <= 'Z' || line[j] >= '0' && line[j] <= '9') {
				j++
			}
			w := line[i:j]
			k := 0
			for k = j; k < len(line) && line[k] == ' '; k++ {
			}
			kind := editor.TokenVariable
			switch {
			case pineKeywords[w]:
				kind = editor.TokenKeyword
			case pineTypes[w] && (k >= len(line) || line[k] != '('):
				kind = editor.TokenType
			case pineConsts[w]:
				kind = editor.TokenConstant
			case k < len(line) && line[k] == '(':
				kind = editor.TokenFunction
			case strings.Contains(w, "."):
				kind = editor.TokenProperty
			}
			i = j
			add(i, kind)
		case strings.ContainsRune("+-*/%=<>!?:", rune(c)):
			i++
			add(i, editor.TokenOperator)
		case c == ' ' || c == '\t':
			j := i
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			i = j
			add(i, editor.TokenText)
		default:
			i++
			add(i, editor.TokenPunctuation)
		}
	}
	return toks, 0
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
