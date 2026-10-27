package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func press(tt *ui.Tester, mods ui.Modifiers, k ui.Key) {
	tt.Key(mods, k)
	tt.Frame()
	tt.Frame()
}

func dragPlot(tt *ui.Tester, a *application, x, y, dx, dy float32) {
	wx, wy := win(a, x, y)
	tt.Press(wx, wy)
	tt.Move(wx+dx/2, wy+dy/2)
	tt.Move(wx+dx, wy+dy)
	tt.Release(wx+dx, wy+dy)
	tt.Frame()
}

// Ctrl+Z steps back through drawing, moving and deleting, Ctrl+Y and
// Ctrl+Shift+Z forward again; in the search box Ctrl+Z undoes typing, and
// another market starts with nothing to undo.
func TestUndoRedo(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	drawn := drawings(a)[0].Pts[0].P
	dragPlot(tt, a, 600, 201, 0, 60)
	moved := drawings(a)[0].Pts[0].P
	if moved == drawn {
		t.Fatal("the drag did not move the line")
	}
	press(tt, 0, ui.KeyDelete)
	if len(drawings(a)) != 0 {
		t.Fatal("Delete left the line")
	}
	for i, s := range []struct {
		mods  ui.Modifiers
		key   ui.Key
		n     int
		price float64
	}{
		{ui.Cmd, ui.KeyZ, 1, moved},
		{ui.Cmd, ui.KeyZ, 1, drawn},
		{ui.Cmd, ui.KeyZ, 0, 0},
		{ui.Cmd, ui.KeyY, 1, drawn},
		{ui.Cmd | ui.Shift, ui.KeyZ, 1, moved},
		{ui.Cmd, ui.KeyY, 0, 0},
	} {
		press(tt, s.mods, s.key)
		if ds := drawings(a); len(ds) != s.n || s.n == 1 && ds[0].Pts[0].P != s.price {
			t.Fatalf("step %d: %+v, want %d drawings at %v", i, ds, s.n, s.price)
		}
	}
	tt.ClickAt(1320-watchW/2, titleBarHeight+28)
	tt.Type("b")
	tt.Frame()
	tt.Frame()
	press(tt, ui.Cmd, ui.KeyZ)
	if len(a.undo) != 3 || len(drawings(a)) != 0 || a.query != "" {
		t.Fatalf("Ctrl+Z in the search box undid a drawing: %d steps left, %d drawings", len(a.undo), len(drawings(a)))
	}
	a.open("ETHUSDT", "1h")
	if len(a.undo)+len(a.redo) != 0 {
		t.Fatal("ETH can undo BTC's drawings")
	}
}

func TestCopyAndPaste(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "趋势线")
	clickPlot(tt, a, 300, 200)
	clickPlot(tt, a, 500, 120)
	orig := drawings(a)[0]
	want := a.mapper().move(orig.Pts, 3, 16)
	click(t, tt, "复制画线")
	ds := drawings(a)
	if len(ds) != 2 || a.sel != ds[1].ID || ds[1].ID == orig.ID || ds[1].Pts[0] != want[0] || ds[1].Pts[1] != want[1] {
		t.Fatalf("copied %+v from %+v, want points %+v", ds, orig, want)
	}
	press(tt, ui.Cmd, ui.KeyC)
	press(tt, ui.Cmd, ui.KeyV)
	press(tt, ui.Cmd, ui.KeyV)
	ds = drawings(a)
	if len(ds) != 4 || ds[3].Pts[0] == ds[2].Pts[0] || ds[2].Pts[0] == ds[1].Pts[0] {
		t.Fatalf("pasted twice: %+v", ds)
	}
	dragPlot(tt, a, 0, 0, 0, 0) // nothing there: unselects
	if ds[0].Pts[0] != orig.Pts[0] {
		t.Fatal("the copies moved the original")
	}
	press(tt, ui.Cmd, ui.KeyZ)
	if len(drawings(a)) != 3 {
		t.Fatalf("Ctrl+Z after pasting: %d drawings", len(drawings(a)))
	}
}

// A locked drawing can be selected but not moved: dragging it pans the chart.
func TestLockedDrawingStaysPut(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	click(t, tt, "锁定画线")
	d := drawings(a)[0]
	if !d.Locked {
		t.Fatal("锁定画线 left it unlocked")
	}
	offset := a.view.offset
	dragPlot(tt, a, 600, 201, -80, 60)
	if got := drawings(a)[0].Pts[0]; got != d.Pts[0] || a.sel != d.ID {
		t.Fatalf("dragging the locked line moved it to %+v (selected %d)", got, a.sel)
	}
	if a.view.offset == offset {
		t.Fatal("dragging the locked line did not pan the chart")
	}
	if _, h := a.hitDrawing(a.mapper(), 300, a.mapper().y(d.Pts[0].P)); h >= 0 {
		t.Fatal("a locked line offers a point to drag")
	}
	click(t, tt, "解锁画线")
	if drawings(a)[0].Locked {
		t.Fatal("解锁画线 left it locked")
	}
	press(tt, ui.Cmd, ui.KeyZ)
	if !drawings(a)[0].Locked {
		t.Fatal("Ctrl+Z did not lock it again")
	}
}

// From 14 to 24 the text grows by at least half of those 10 pixels: line
// heights differ between the system fonts, and macOS's is tighter at 20 and up.
func TestTextSize(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "文字")
	clickPlot(tt, a, 300, 200)
	id := drawings(a)[0].ID
	h := a.textBox[id].H
	click(t, tt, "文字大小")
	click(t, tt, "24")
	if d := drawings(a)[0]; d.Size != 24 || a.textBox[id].H < h+5 {
		t.Fatalf("size %v, the text %v tall (was %v)", d.Size, a.textBox[id].H, h)
	}
}

// A horizontal line takes a price typed into the bar, as one step to undo.
func TestHorizontalLinePrice(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 200)
	drawn := drawings(a)[0].Pts[0].P
	r, ok := tt.Find("价格")
	if !ok {
		t.Fatal("no price in the bar")
	}
	tt.ClickAt(r.X+30, r.Y+r.H/2)
	tt.Frame()
	press(tt, ui.Cmd, ui.KeyA)
	for _, ch := range "80000.5" { // a key at a time, as typed
		tt.Type(string(ch))
		tt.Frame()
	}
	tt.Frame()
	if p := drawings(a)[0].Pts[0].P; p != 80000.5 {
		t.Fatalf("typed 80000.5, the line is at %v", p)
	}
	clickPlot(tt, a, 300, 420)
	press(tt, ui.Cmd, ui.KeyZ)
	if p := drawings(a)[0].Pts[0].P; p != drawn {
		t.Fatalf("one Ctrl+Z after typing a price: %v, want %v", p, drawn)
	}
}

func TestInkSetting(t *testing.T) {
	a := testApp()
	st := &a.set.Chart
	white := ui.Hex("#ffffff")
	st.Up, st.Background = hexColor(white), hexColor(white)
	k := paletteFor(false)
	k.up = white
	if c := k.withInk(st, white).upInk; c == white {
		t.Fatal("automatic: white text on white")
	}
	st.Ink = "body"
	if c := k.withInk(st, white).upInk; c != white {
		t.Fatalf("跟实体颜色: %+v", c)
	}
	st.Ink, st.InkUp = "custom", hexColor(ui.Hex("#ff9800"))
	if c := k.withInk(st, white); c.upInk != ui.Hex("#ff9800") || c.downInk != k.down {
		t.Fatalf("自定义: %+v %+v", c.upInk, c.downInk)
	}
	st.Ink, st.InkUp = "", hexColor{}
	tt := newTester(a, false)
	openSettings(t, tt, "商品")
	if _, ok := tt.Find("文字涨"); ok {
		t.Fatal("colors to pick while automatic")
	}
	click(t, tt, labelOf(inkModes, ""))
	click(t, tt, "自定义")
	if _, ok := tt.Find("文字涨"); st.Ink != "custom" || !ok {
		t.Fatalf("after 自定义: ink %q, colors to pick %v", st.Ink, ok)
	}
	click(t, tt, "取消")
	if st.Ink != "" {
		t.Fatalf("取消 kept ink %q", st.Ink)
	}
}

// TestBarShots saves the drawing bar for a text, a horizontal line and a
// locked trend line.
func TestBarShots(t *testing.T) {
	dir := os.Getenv("CANDLE_SHOTS")
	if dir == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	for _, c := range []struct {
		name, tool string
		clicks     [][2]float32
		then       string
	}{
		{"bar-text", "文字", [][2]float32{{300, 200}}, ""},
		{"bar-hline", "水平线", [][2]float32{{300, 200}}, ""},
		{"bar-locked", "趋势线", [][2]float32{{300, 200}, {500, 120}}, "锁定画线"},
	} {
		a, tt := drawApp(t)
		click(t, tt, c.tool)
		for _, p := range c.clicks {
			clickPlot(tt, a, p[0], p[1])
		}
		if c.then != "" {
			click(t, tt, c.then)
		}
		img := tt.Image().SubImage(image.Rect(int(chartLeft(a)), titleBarHeight, int(chartLeft(a)+a.plotW+axisW), titleBarHeight+260))
		f, err := os.Create(filepath.Join(dir, c.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, img)
		f.Close()
	}
}
