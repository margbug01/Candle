package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

func TestHexColorJSON(t *testing.T) {
	st := defaultStyle()
	st.Up, st.Cross = hexColor(ui.Hex("#f23645")), hexColor(ui.RGBA(17, 34, 51, 0.5))
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Up":"#f23645"`, `"Cross":"#11223380"`, `"Down":""`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s lacks %s", data, want)
		}
	}
	var back style
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Up != st.Up || back.Cross.A != 128 || back.Down.set() {
		t.Fatalf("read back %+v %+v %+v", back.Up, back.Cross, back.Down)
	}
}

// A settings file from before the chart settings, or holding a few of them,
// keeps the defaults of the rest.
func TestLoadSettingsKeepsNewDefaults(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	os.WriteFile(old, []byte(`{"Symbol":"ETHUSDT","Interval":"4h","Watch":["ETHUSDT"]}`), 0o644)
	if s := loadSettings(old); s.Chart != defaultStyle() || s.Symbol != "ETHUSDT" {
		t.Fatalf("old file: %+v", s)
	}
	some := filepath.Join(dir, "some.json")
	os.WriteFile(some, []byte(`{"Chart":{"Type":"line","Up":"#ff0000","TextSize":99,"Volume":false}}`), 0o644)
	s := loadSettings(some)
	if s.Chart.Type != "line" || s.Chart.Up.or(ui.Color{}) != ui.Hex("#ff0000") || s.Chart.Volume || !s.Chart.Body || s.Chart.TextSize != 11 {
		t.Fatalf("partial file: %+v", s.Chart)
	}
}

func openSettings(t *testing.T, tt *ui.Tester, page string) {
	t.Helper()
	click(t, tt, "图表设置")
	click(t, tt, page)
}

// What the dialog changes shows at once; 取消 and Escape put it back, 确认 keeps it.
func TestSettingsCancelAndConfirm(t *testing.T) {
	var themes []string
	applyTheme = func(key string) { themes = append(themes, key) }
	t.Cleanup(func() { applyTheme = func(string) {} })
	a := testApp()
	a.set.Chart.Buttons = "never" // its 对数 would shadow the drop-down's
	tt := newTester(a, true)

	openSettings(t, tt, "版面")
	click(t, tt, "水印")
	click(t, tt, "跟随系统")
	click(t, tt, "深色")
	if !a.set.Chart.Watermark || a.set.Chart.Theme != "dark" || len(themes) != 1 || themes[0] != "dark" {
		t.Fatalf("while editing: watermark %v, theme %q, applied %v", a.set.Chart.Watermark, a.set.Chart.Theme, themes)
	}
	click(t, tt, "取消")
	if a.set.Chart.Watermark || a.set.Chart.Theme != "" || a.dialog || len(themes) != 2 || themes[1] != "" {
		t.Fatalf("after 取消: watermark %v, theme %q, open %v, applied %v", a.set.Chart.Watermark, a.set.Chart.Theme, a.dialog, themes)
	}

	openSettings(t, tt, "状态行")
	click(t, tt, "开高低收")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	if !a.set.Chart.Values || a.dialog || a.editing {
		t.Fatalf("after Escape: values %v, open %v, editing %v", a.set.Chart.Values, a.dialog, a.editing)
	}

	openSettings(t, tt, "坐标")
	a.scale = prices{manual: true, lo: 1, hi: 2}
	click(t, tt, "常规")
	click(t, tt, "对数")
	click(t, tt, "确认")
	if a.set.Chart.Scale != "log" || a.scale.manual || a.dialog {
		t.Fatalf("after 确认: scale %q, prices by hand %v, open %v", a.set.Chart.Scale, a.scale.manual, a.dialog)
	}
}

// 恢复默认 puts every setting back but the theme.
func TestSettingsReset(t *testing.T) {
	a := testApp()
	a.set.Chart.Type, a.set.Chart.Theme, a.set.Chart.Up = "bars", "light", hexColor(ui.Hex("#123456"))
	tt := newTester(a, true)
	openSettings(t, tt, "商品")
	click(t, tt, "恢复默认")
	want := defaultStyle()
	want.Theme = "light"
	if a.set.Chart != want {
		t.Fatalf("after 恢复默认: %+v", a.set.Chart)
	}
}

// The corner switches, by default, show while the pointer is on the price
// axis or on them.
func TestScaleButtonsShowNearTheAxis(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	corner := ui.Rect{X: 1320 - watchW - axisW + 3, Y: 820 - timeH + 4, W: axisW - 6, H: 20}
	shows := func(x, y float32) bool {
		tt.Move(x, y)
		tt.Frame()
		tt.Frame()
		return inkBox(tt.Image(), corner)[1] > 4
	}
	if shows(400, 300) {
		t.Fatal("the switches show with the pointer on the chart")
	}
	if !shows(chartLeft(a)+a.plotW+30, 300) {
		t.Fatal("the switches hide with the pointer on the price axis")
	}
	if !shows(corner.X+corner.W/4, corner.Y+10) {
		t.Fatal("the switches hide with the pointer on them")
	}
	click(t, tt, "对数")
	if a.set.Chart.Scale != "log" {
		t.Fatalf("对数 left the scale %q", a.set.Chart.Scale)
	}
	a.set.Chart.Buttons = "always"
	if !shows(400, 300) {
		t.Fatal("set to show always, the switches hide")
	}
}

func TestChartMenu(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	a.view.offset = -100
	choose := func(item string) {
		t.Helper()
		tt.RightClickAt(400, 300)
		tt.Frame()
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
	}
	choose("百分比坐标")
	if a.set.Chart.Scale != "percent" {
		t.Fatalf("scale %q", a.set.Chart.Scale)
	}
	choose("重置图表")
	if a.view.offset != a.home() {
		t.Fatalf("offset %v after 重置图表", a.view.offset)
	}
	choose("设置…")
	if !a.dialog {
		t.Fatal("设置… opened nothing")
	}
}

// More room on the right moves the newest bar along while it rests there.
func TestRightBarsFollowsTheNewestBar(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	a.set.Chart.RightBars = 20
	tt.Frame()
	if a.view.offset != 20 {
		t.Fatalf("offset %v", a.view.offset)
	}
	a.view.offset = -50
	a.set.Chart.RightBars = 2
	tt.Frame()
	if a.view.offset != -50 {
		t.Fatalf("looking back, the view moved to %v", a.view.offset)
	}
}

// growth is n daily bars rising a hundredfold, for the log scale's pictures.
func growth(n int) []market.Bar {
	bars := fakeBars(n, 86_400_000)
	for i := range bars {
		f := math.Pow(100, float64(i)/float64(n-1)) / 80
		b := &bars[i]
		b.O, b.H, b.L, b.C = b.O*f, b.H*f, b.L*f, b.C*f
	}
	return bars
}

func TestStyleShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	for _, typ := range chartTypes {
		a := testApp()
		a.set.Chart.Type = typ.key
		a.view.spacing = 12
		tt := newTester(a, true)
		shot(t, tt, "type-"+typ.label)
	}
	for _, mode := range []string{"", "log", "percent"} {
		a := testApp()
		a.interval, a.bars = "1d", growth(600)
		a.set.Chart.Scale, a.set.Chart.HighLow, a.scaleMode = mode, true, mode
		a.view.spacing = 1.6
		tt := newTester(a, true)
		tt.Move(500, 400)
		tt.Frame()
		tt.Frame()
		shot(t, tt, "scale-"+map[string]string{"": "linear", "log": "log", "percent": "percent"}[mode])
	}
	a := testApp()
	st := &a.set.Chart
	p := paletteFor(false)
	st.Up, st.Down, st.Watermark, st.CrossStyle, st.Clock12 = hexColor(p.down), hexColor(p.up), true, "dotted", true
	st.Values, st.Zone, st.TextSize = false, "America/New_York", 12
	tt := newTester(a, false)
	tt.Move(500, 400)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "light-custom")
	tt.SetDark(true)
	click(t, tt, "图表设置")
	for _, pg := range settingsPages {
		click(t, tt, pg.label)
		shot(t, tt, "dialog-"+pg.label)
	}
}

// White rising bodies on a white background, with black borders: what has
// to show takes the border's black, and the last price's label reads.
func TestInkShowsOnTheBackground(t *testing.T) {
	a := testApp()
	st := &a.set.Chart
	st.Up, st.Down, st.BorderUp, st.Background = hexColor(ui.Hex("#ffffff")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#000000")), hexColor(ui.Hex("#ffffff"))
	k := paletteFor(false)
	k.up, k.down = st.Up.or(k.up), st.Down.or(k.down)
	up, down := -1, -1
	for i := range a.bars {
		if a.rising(i) && up < 0 {
			up = i
		}
		if !a.rising(i) && down < 0 {
			down = i
		}
	}
	if c := a.inkColor(up, k); luma(c) > 0.2 {
		t.Fatalf("a white bar's ink is %+v on white", c)
	}
	if c := a.inkColor(down, k); c != ui.Hex("#000000") {
		t.Fatalf("a black bar's ink is %+v", c)
	}
	st.BorderUp = hexColor{}
	if c := a.inkColor(up, k); c != k.text {
		t.Fatalf("without a border, a white bar's ink is %+v, want the text's", c)
	}
	st.BorderUp = hexColor(ui.Hex("#000000"))
	if c := k.withInk(st, ui.Hex("#ffffff")).move(1); luma(c) > 0.2 {
		t.Fatalf("a move up reads in %+v on white", c)
	}
	if onColor(ui.Hex("#ffffff")) == ui.Hex("#ffffff") {
		t.Fatal("white text on a white label")
	}
	if os.Getenv("CANDLE_SHOTS") != "" {
		st.Volume = false
		tt := newTester(a, false)
		shot(t, tt, "mono-light")
	}
}
