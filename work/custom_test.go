package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const crossScript = `//@version=6
indicator("双均线交叉", overlay = true)
fastLen = input.int(9, "快线", minval = 1)
slowLen = input.int(21, "慢线", minval = 1)
plot(ta.ema(close, fastLen), "快线", color = color.orange)
plot(ta.ema(close, slowLen), "慢线", color = color.blue)
plotshape(ta.crossover(ta.ema(close, fastLen), ta.ema(close, slowLen)), "金叉", shape.triangleup, location.belowbar, color.green)
`

// customApp is a test app with a folder of scripts of its own, and no
// scripts known from another test.
func customApp(t *testing.T) (*application, string) {
	t.Helper()
	before := customs
	customs = map[string]*indKind{}
	t.Cleanup(func() { customs = before })
	a := testApp()
	a.indDir = t.TempDir()
	files, _ := readScripts(a.indDir, nil)
	a.takeScripts(files, true)
	return a, a.indDir
}

func write(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findInd(a *application, kind string) *indicator {
	for i := range a.indicators {
		if a.indicators[i].Kind == kind {
			return &a.indicators[i]
		}
	}
	return nil
}

// A script put in the folder while Candle runs, as an agent puts it there,
// goes onto the chart; one with an error says where; one changed is worked
// out again; one taken away leaves its indicator, settings and all, till it
// comes back.
func TestScriptsFromTheFolder(t *testing.T) {
	a, dir := customApp(t)
	files, _ := readScripts(dir, nil)
	write(t, filepath.Join(dir, "双均线.pine"), crossScript)
	files, changed := readScripts(dir, files)
	if !changed {
		t.Fatal("a new file is no change")
	}
	a.takeScripts(files, false)
	ind := findInd(a, "pine:双均线")
	if ind == nil {
		t.Fatalf("not on the chart: %+v", a.indicators)
	}
	if len(a.notices) != 1 || !strings.Contains(a.notices[0].text, "双均线交叉") {
		t.Fatalf("notices %+v", a.notices)
	}
	k, _ := kindOf(ind.Kind)
	s := a.seriesFor(ind)
	if k.pane || len(k.params) != 2 || s.vm == nil || s.vm.Err != nil {
		t.Fatalf("kind %+v, machine %+v", k, s.vm)
	}
	if v := s.at(0, len(a.bars)-1); v != v {
		t.Fatal("no value on the newest bar")
	}

	// The status for an agent.
	var st struct {
		Files map[string]struct {
			OK      bool     `json:"ok"`
			Error   string   `json:"error"`
			OnChart bool     `json:"on_chart"`
			Inputs  []string `json:"inputs"`
		} `json:"files"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "_status.json"))
	if err != nil || json.Unmarshal(data, &st) != nil {
		t.Fatal(err)
	}
	if f := st.Files["双均线.pine"]; !f.OK || !f.OnChart || strings.Join(f.Inputs, ",") != "快线,慢线" {
		t.Fatalf("status %s", data)
	}

	// An error, told of with its line.
	write(t, filepath.Join(dir, "坏的.pine"), "//@version=6\nindicator(\"坏\")\nplot(ta.nosuch(close))\n")
	files, _ = readScripts(dir, files)
	a.takeScripts(files, false)
	if findInd(a, "pine:坏的") != nil {
		t.Fatal("a script with an error went onto the chart")
	}
	last := a.notices[len(a.notices)-1]
	if !strings.Contains(last.text, "第 3 行") || last.edit != "pine:坏的" {
		t.Fatalf("notice %+v", last)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "_status.json"))
	json.Unmarshal(data, &st)
	if f := st.Files["坏的.pine"]; f.OK || !strings.Contains(f.Error, "ta.nosuch") {
		t.Fatalf("status %s", data)
	}

	// Set up, then changed on disk: the settings stay, the values follow.
	ind.Params[0] = 5
	write(t, filepath.Join(dir, "双均线.pine"), strings.Replace(crossScript, "ta.ema(close, fastLen), \"快线\"", "ta.sma(close, fastLen), \"快线\"", 1)+"\n")
	files, _ = readScripts(dir, files)
	a.takeScripts(files, false)
	if ind = findInd(a, "pine:双均线"); ind == nil || ind.Params[0] != 5 {
		t.Fatalf("after the change %+v", ind)
	}
	if got, want := a.seriesFor(ind).at(0, len(a.bars)-1), smaOf(a, 5); abs64(got-want) > 1e-6 {
		t.Fatalf("worked out %v after the change, want the SMA %v", got, want)
	}
	if len(a.indicators) != 1 {
		t.Fatalf("a change added it again: %+v", a.indicators)
	}

	// Gone, then back.
	os.Rename(filepath.Join(dir, "双均线.pine"), filepath.Join(dir, "双均线.txt"))
	files, _ = readScripts(dir, files)
	a.takeScripts(files, false)
	ind = findInd(a, "pine:双均线")
	if k, _ := kindOf("pine:双均线"); ind == nil || k.broken == "" || ind.Params[0] != 5 {
		t.Fatalf("gone: %+v, %+v", ind, k)
	}
	tt := newTester(a, true)
	if _, ok := tt.Find("双均线 5 21"); !ok {
		t.Fatal("no legend row for the script that is gone")
	}
	os.Rename(filepath.Join(dir, "双均线.txt"), filepath.Join(dir, "双均线.pine"))
	files, _ = readScripts(dir, files)
	a.takeScripts(files, false)
	if ind = findInd(a, "pine:双均线"); ind == nil || ind.Params[0] != 5 || len(a.indicators) != 1 {
		t.Fatalf("back: %+v", a.indicators)
	}
}

func smaOf(a *application, n int) float64 {
	s := 0.0
	for _, b := range a.bars[len(a.bars)-n:] {
		s += b.C
	}
	return s / float64(n)
}

func abs64(v float64) float64 { return max(v, -v) }

// Many files at once are not all put on the chart.
func TestManyScriptsAreOnlyListed(t *testing.T) {
	a, dir := customApp(t)
	files, _ := readScripts(dir, nil)
	for _, n := range []string{"a", "b", "c", "d"} {
		write(t, filepath.Join(dir, n+".pine"), strings.Replace(crossScript, "双均线交叉", n, 1))
	}
	files, _ = readScripts(dir, files)
	a.takeScripts(files, false)
	if len(a.indicators) != 0 || len(customList()) != 4 || !strings.Contains(a.notices[0].text, "4 个指标") {
		t.Fatalf("indicators %+v, notices %+v", a.indicators, a.notices)
	}
}

// The scripts there when Candle opens stay off the chart; the README is
// written for agents.
func TestFolderAtStart(t *testing.T) {
	before := customs
	customs = map[string]*indKind{}
	t.Cleanup(func() { customs = before })
	a := testApp()
	a.indDir = t.TempDir()
	write(t, filepath.Join(a.indDir, "双均线.pine"), crossScript)
	writeReadme(a.indDir)
	files, _ := readScripts(a.indDir, nil)
	a.takeScripts(files, true)
	if len(a.indicators) != 0 || len(a.notices) != 0 || customs["pine:双均线"] == nil {
		t.Fatalf("at the start: %+v %+v", a.indicators, a.notices)
	}
	data, err := os.ReadFile(filepath.Join(a.indDir, "README.md"))
	if err != nil || !strings.Contains(string(data), "_status.json") || !strings.Contains(string(data), readmeVersion) {
		t.Fatalf("README: %v", err)
	}
	// An older README is replaced, one of this version kept.
	write(t, filepath.Join(a.indDir, "README.md"), "old")
	writeReadme(a.indDir)
	if data, _ := os.ReadFile(filepath.Join(a.indDir, "README.md")); string(data) == "old" {
		t.Fatal("kept an old README")
	}
}

// The clipboard's script is saved, named after its title, and goes onto the
// chart; one that does not run opens in the editor to be put right.
func TestImportFromTheClipboard(t *testing.T) {
	a, dir := customApp(t)
	clip := crossScript
	before := readClipboard
	readClipboard = func() string { return clip }
	t.Cleanup(func() { readClipboard = before })
	a.importClipboard()
	if _, err := os.Stat(filepath.Join(dir, "双均线交叉.pine")); err != nil {
		t.Fatal(err)
	}
	if findInd(a, "pine:双均线交叉") == nil {
		t.Fatalf("indicators %+v", a.indicators)
	}
	a.importClipboard() // again: a file of its own
	if _, err := os.Stat(filepath.Join(dir, "双均线交叉 2.pine")); err != nil {
		t.Fatal(err)
	}
	clip = "indicator(\"x\")\nplot(request.security(\"AAPL\", \"D\", close))"
	a.importClipboard()
	if a.ed == nil || a.ed.key != "" || !strings.Contains(a.ed.st.Buffer.String(), "request.security") {
		t.Fatal("the editor did not open on the script that does not run")
	}
	a.ed.check()
	if !strings.Contains(a.ed.err, "第 2 行") || !strings.Contains(a.ed.err, "别的品种") {
		t.Fatalf("editor's error %q", a.ed.err)
	}
}

// 新建 opens the editor; what is typed is checked as it is typed; saving
// puts the file in the folder and the indicator on the chart.
func TestEditorMakesAScript(t *testing.T) {
	a, dir := customApp(t)
	tt := newTester(a, true)
	click(t, tt, "指标")
	click(t, tt, "新建指标")
	if a.ed == nil {
		t.Fatal("no editor")
	}
	a.ed.st.Buffer.Set("//@version=6\nindicator(\"我的 RSI\")\nplot(ta.rsi(close, 14)\n")
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("第 3 行：少了 )"); !ok {
		t.Fatalf("the error is not shown: %q", a.ed.err)
	}
	if _, ok := tt.Find("保存并加到图上"); ok {
		t.Fatal("offered to put a script with an error on the chart")
	}
	a.ed.st.Buffer.Set("//@version=6\nindicator(\"我的 RSI\")\nplot(ta.rsi(close, 14), \"RSI\")\nhline(70)\n")
	tt.Frame()
	tt.Frame()
	click(t, tt, "保存并加到图上")
	if a.ed != nil {
		t.Fatal("the editor stayed open")
	}
	if _, err := os.Stat(filepath.Join(dir, "我的 RSI.pine")); err != nil {
		t.Fatal(err)
	}
	ind := findInd(a, "pine:我的 RSI")
	if k, _ := kindOf("pine:我的 RSI"); ind == nil || !k.pane {
		t.Fatalf("on the chart: %+v", a.indicators)
	}
	tt.Frame()
	if _, ok := tt.Find("我的 RSI"); !ok {
		t.Fatal("no legend row")
	}
}

// Closing the editor with changes not saved asks again; 删除 asks again,
// then moves the file to the trash and takes it off the chart.
func TestEditorAsksBeforeLosingWork(t *testing.T) {
	a, dir := customApp(t)
	write(t, filepath.Join(dir, "双均线.pine"), crossScript)
	files, _ := readScripts(dir, nil)
	a.takeScripts(files, true)
	k, _ := kindOf("pine:双均线")
	a.addIndicator(k)
	var trashed string
	before := trashFile
	trashFile = func(p string) error { trashed = p; return os.Remove(p) }
	t.Cleanup(func() { trashFile = before })
	tt := newTester(a, true)
	a.openEditor("pine:双均线", "")
	tt.Frame()
	a.ed.st.Buffer.Set(crossScript + "// 改了\n")
	tt.Frame()
	click(t, tt, "取消")
	if a.ed == nil {
		t.Fatal("closed with the change unsaved")
	}
	click(t, tt, "放弃修改")
	if a.ed != nil {
		t.Fatal("did not close the second time")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "双均线.pine")); string(data) != crossScript {
		t.Fatal("the file changed")
	}
	a.openEditor("pine:双均线", "")
	tt.Frame()
	click(t, tt, "删除指标")
	if trashed != "" {
		t.Fatal("deleted at the first click")
	}
	click(t, tt, "删除指标")
	if trashed != filepath.Join(dir, "双均线.pine") || a.ed != nil || len(a.indicators) != 0 || customs["pine:双均线"] != nil {
		t.Fatalf("trashed %q, editor %v, indicators %+v", trashed, a.ed != nil, a.indicators)
	}
}
