package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"candle/pine"
)

// The user's own indicators: Pine scripts in the indicators folder beside
// the settings. Candle looks at the folder every second and a half; what
// is new there while it runs goes onto the chart, so that an agent writing
// a script there puts it before the user; what changes is worked out
// again. The folder's README tells an agent what Candle runs, and
// _status.json, which Candle keeps, whether each script does.

// customs are the scripts in the folder, by key ("pine:" and the file's
// name), as last read.
var customs = map[string]*indKind{}

// customKind is the user's script keyed key; one that is gone draws
// nothing, its indicators keeping their settings till it comes back.
func customKind(key string) *indKind {
	if !isScriptKey(key) {
		return nil
	}
	if k := customs[key]; k != nil {
		return k
	}
	name := strings.TrimPrefix(key, "pine:")
	return &indKind{key: key, short: name, name: name, category: "自定义", broken: "找不到脚本文件 " + name + ".pine"}
}

// customList is the user's scripts in the order of their names.
func customList() []*indKind {
	keys := make([]string, 0, len(customs))
	for k := range customs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*indKind, len(keys))
	for i, k := range keys {
		out[i] = customs[k]
	}
	return out
}

func scriptKey(file string) string {
	return "pine:" + strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
}

// scriptFile is a script as the folder holds it.
type scriptFile struct {
	path, src string
	mod       time.Time
	size      int64
}

// readScripts reads the scripts in dir whose files are not as in known.
func readScripts(dir string, known map[string]scriptFile) (map[string]scriptFile, bool) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, len(known) > 0
	}
	out := map[string]scriptFile{}
	changed := false
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pine") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		f := scriptFile{path: p, mod: info.ModTime(), size: info.Size()}
		if old, ok := known[p]; ok && old.mod.Equal(f.mod) && old.size == f.size {
			out[p] = old
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f.src = string(data)
		out[p], changed = f, true
	}
	return out, changed || len(out) != len(known)
}

// compileFile is the kind a script file makes: broken, with why, when it
// does not compile.
func compileFile(f scriptFile, before *indKind) *indKind {
	key := scriptKey(f.path)
	name := strings.TrimPrefix(key, "pine:")
	s, err := pine.Compile(strings.TrimPrefix(f.src, "\ufeff"))
	if err != nil {
		k := &indKind{key: key, short: name, name: name, category: "自定义", custom: f.path, broken: errorText(f.src, err)}
		if before != nil { // its pane stays as it was
			k.pane = before.pane
		}
		return k
	}
	k := scriptKind(key, s, "自定义", name)
	k.custom = f.path
	return &k
}

// errorText is a script's error with the line it is on.
func errorText(src string, err error) string {
	if e, ok := err.(*pine.Error); ok {
		return fmt.Sprintf("第 %d 行：%s", pine.Line(src, e.Start), e.Msg)
	}
	return err.Error()
}

func (a *application) indicatorsDir() string {
	if a.indDir != "" || a.file == "" {
		return a.indDir
	}
	return filepath.Join(filepath.Dir(a.file), "indicators")
}

// watchScripts reads the folder now, and again whenever it changes.
func (a *application) watchScripts() {
	dir := a.indicatorsDir()
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0o755)
	writeReadme(dir)
	files, _ := readScripts(dir, nil)
	a.takeScripts(files, true)
	go func() {
		for range time.Tick(1500 * time.Millisecond) {
			next, changed := readScripts(dir, files)
			if !changed {
				continue
			}
			files = next
			a.post(func() { a.takeScripts(next, false) })
		}
	}()
}

// takeScripts makes the files read the scripts Candle knows. New ones, read
// after the first time, go onto the chart, a few at a time.
func (a *application) takeScripts(files map[string]scriptFile, first bool) {
	next := map[string]*indKind{}
	var added []*indKind
	for _, f := range files {
		key := scriptKey(f.path)
		old := customs[key]
		if old != nil && old.custom == f.path && old.rev == f.mod.UnixNano()+f.size {
			next[key] = old
			continue
		}
		k := compileFile(f, old)
		k.rev = f.mod.UnixNano() + f.size
		next[key] = k
		if old == nil && !first && !a.ownSave[key] && !slices.ContainsFunc(a.set.Indicators, func(ind indicator) bool { return ind.Kind == key }) {
			added = append(added, k)
		}
	}
	customs = next
	a.ownSave = nil
	for i := range a.set.Indicators { // their inputs may have changed
		if isScriptKey(a.set.Indicators[i].Kind) {
			a.set.Indicators[i].fix()
		}
	}
	slices.SortFunc(added, func(x, y *indKind) int { return cmp.Compare(x.key, y.key) })
	switch {
	case len(added) > 3:
		a.notify(fmt.Sprintf("文件夹里多了 %d 个指标，在「指标 › 自定义」里添加", len(added)), "")
	case len(added) > 0:
		for _, k := range added {
			if k.broken != "" {
				a.notify("「"+k.name+"」有错，"+k.broken, k.key)
				continue
			}
			a.addIndicator(k)
			a.notify("已把「"+k.name+"」加到图上", "")
		}
	}
	a.writeStatus()
	a.searchGen++
}

// notice is a word on the chart for a moment: what came into the folder.
type notice struct {
	text string
	edit string // the script it is about, to open in the editor
	at   time.Time
}

func (a *application) notify(text, edit string) {
	a.notices = append(a.notices, notice{text: text, edit: edit, at: time.Now()})
	if len(a.notices) > 3 {
		a.notices = a.notices[len(a.notices)-3:]
	}
}

// saveScript writes src to the script keyed key, or a new file named after
// its title when key is "", and reads it at once. It returns the key.
func (a *application) saveScript(key, src string) (string, error) {
	dir := a.indicatorsDir()
	if dir == "" {
		return "", fmt.Errorf("没有放指标的文件夹")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var file string
	if k := customs[key]; k != nil && k.custom != "" {
		file = k.custom
	} else {
		name := "我的指标"
		if s, err := pine.Compile(src); err == nil {
			name = s.Title
		}
		file = freeName(dir, fileName(name))
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return "", err
	}
	key = scriptKey(file)
	info, _ := os.Stat(file)
	f := scriptFile{path: file, src: src}
	if info != nil {
		f.mod, f.size = info.ModTime(), info.Size()
	}
	k := compileFile(f, customs[key])
	k.rev = f.mod.UnixNano() + f.size
	next := map[string]*indKind{}
	for kk, v := range customs {
		next[kk] = v
	}
	next[key] = k
	customs = next
	if a.ownSave == nil {
		a.ownSave = map[string]bool{}
	}
	a.ownSave[key] = true // the watcher will find it: it is not new to the user
	for i := range a.set.Indicators {
		if a.set.Indicators[i].Kind == key {
			a.set.Indicators[i].fix()
		}
	}
	a.writeStatus()
	a.searchGen++
	return key, nil
}

// fileName is a title made fit to name a file.
func fileName(title string) string {
	title = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	title = strings.Trim(title, ". ")
	if title == "" {
		title = "我的指标"
	}
	return shorten(title, 60)
}

// freeName is dir/name.pine, or name 2, name 3… while that is taken.
func freeName(dir, name string) string {
	p := filepath.Join(dir, name+".pine")
	for n := 2; ; n++ {
		if _, err := os.Stat(p); err != nil && customs[scriptKey(p)] == nil {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s %d.pine", name, n))
	}
}

// deleteScript moves the script to the trash and takes its indicators off
// the chart.
func (a *application) deleteScript(key string) error {
	k := customs[key]
	if k == nil || k.custom == "" {
		return nil
	}
	if err := trashFile(k.custom); err != nil {
		return err
	}
	next := map[string]*indKind{}
	for kk, v := range customs {
		if kk != key {
			next[kk] = v
		}
	}
	customs = next
	for _, ind := range slices.Clone(a.set.Indicators) {
		if ind.Kind == key {
			a.deleteIndicator(ind.ID)
		}
	}
	a.writeStatus()
	a.searchGen++
	return nil
}

var trashFile = func(path string) error { return mygo.Shell.TrashItem(path) }

// importClipboard adds the script on the clipboard: to the folder and the
// chart when it runs, else to the editor to be put right.
func (a *application) importClipboard() {
	src := strings.TrimSpace(readClipboard())
	if src == "" {
		a.notify("剪贴板里没有文字，先在 TradingView 或编辑器里复制指标的代码", "")
		return
	}
	if _, err := pine.Compile(src); err != nil {
		a.openEditor("", src)
		return
	}
	key, err := a.saveScript("", src+"\n")
	if err != nil {
		a.notify("没存上："+err.Error(), "")
		return
	}
	k, _ := kindOf(key)
	a.addIndicator(k)
	a.notify("已从剪贴板导入「"+k.name+"」，加到了图上", "")
}

var readClipboard = func() string { return mygo.Clipboard.ReadText() }

// writeStatus writes _status.json: whether each script runs, and if not
// why, for an agent writing them.
func (a *application) writeStatus() {
	dir := a.indicatorsDir()
	if dir == "" {
		return
	}
	type entry struct {
		Key      string   `json:"key"`
		Title    string   `json:"title"`
		OK       bool     `json:"ok"`
		Error    string   `json:"error,omitempty"`
		Overlay  bool     `json:"overlay"`
		Inputs   []string `json:"inputs,omitempty"`
		Plots    []string `json:"plots,omitempty"`
		Warnings []string `json:"warnings,omitempty"`
		OnChart  bool     `json:"on_chart"`
	}
	st := struct {
		Updated string           `json:"updated"`
		Files   map[string]entry `json:"files"`
	}{Updated: time.Now().Format(time.RFC3339), Files: map[string]entry{}}
	for _, k := range customList() {
		e := entry{Key: k.key, Title: k.name, OK: k.broken == "", Error: k.broken, Overlay: !k.pane}
		for _, p := range k.params {
			e.Inputs = append(e.Inputs, p.name)
		}
		for _, p := range k.plots {
			e.Plots = append(e.Plots, p.name)
		}
		if k.script != nil {
			for _, w := range k.script.Warnings {
				e.Warnings = append(e.Warnings, errorText(k.script.Source(), w))
			}
		}
		for _, ind := range a.set.Indicators {
			e.OnChart = e.OnChart || ind.Kind == k.key
		}
		st.Files[filepath.Base(k.custom)] = e
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	tmp := filepath.Join(dir, "_status.json.tmp")
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, filepath.Join(dir, "_status.json"))
	}
}

const readmeVersion = "candle-indicators-readme 1"

// writeReadme puts the folder's README there, or a newer one over an older.
func writeReadme(dir string) {
	p := filepath.Join(dir, "README.md")
	if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), readmeVersion) {
		return
	}
	os.WriteFile(p, []byte(readme), 0o644)
}
