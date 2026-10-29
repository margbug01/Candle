package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func center(r ui.Rect) (float32, float32) { return r.X + r.W/2, r.Y + r.H/2 }

// hover rests the pointer on what label names.
func hover(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no %q", label)
	}
	tt.Move(center(r))
	for range 5 {
		tt.Frame()
	}
}

// A tool's star shows while the pointer is on it, and puts it on the bar, in
// the column's order; again, takes it off.
func TestStarATool(t *testing.T) {
	a := testApp()
	a.set.FavTools = []string{"trend", "fib"}
	tt := newTester(a, true)
	tool, _ := tt.Find("射线")
	corner := ui.Rect{X: tool.X + tool.W - 8, Y: tool.Y - 2, W: 12, H: 12} // where the star goes
	if box := inkBox(tt.Image(), corner); box[0] > 3 || box[1] > 3 {       // the button's own edge, at most
		t.Fatalf("a star shows before the pointer is on the tool: %v", box)
	}
	hover(t, tt, "射线")
	if box := inkBox(tt.Image(), corner); box[0] < 7 || box[1] < 7 {
		t.Fatalf("no star with the pointer on the tool: %v", box)
	}
	click(t, tt, "加入常用工具栏 射线")
	if want := []string{"trend", "ray", "fib"}; !slices.Equal(a.set.FavTools, want) {
		t.Fatalf("favorites %v, want %v", a.set.FavTools, want)
	}
	if _, ok := tt.Find("常用 射线"); !ok {
		t.Fatal("not on the bar")
	}
	hover(t, tt, "射线")
	click(t, tt, "从常用工具栏移除 射线")
	if want := []string{"trend", "fib"}; !slices.Equal(a.set.FavTools, want) {
		t.Fatalf("favorites %v, want %v", a.set.FavTools, want)
	}
}

// A tool on the bar is taken up as from the column, and draws.
func TestDrawFromTheBar(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "常用 水平线")
	if a.tool != "hline" {
		t.Fatalf("tool %q", a.tool)
	}
	clickPlot(tt, a, 300, 300)
	if ds := drawings(a); len(ds) != 1 || ds[0].Kind != "hline" {
		t.Fatalf("drew %+v", ds)
	}
	if _, ok := tt.Find("常用工具栏"); !ok {
		t.Fatal("the bar went")
	}
}

// The bar goes where its grip is dragged, no further than the chart's
// edges, and stays there; the switch in the column hides it.
func TestMoveTheBar(t *testing.T) {
	a := testApp()
	a.file = filepath.Join(t.TempDir(), "settings.json")
	tt := newTester(a, true)
	before := a.set.FavX
	g, ok := tt.Find("拖动常用工具栏")
	if !ok {
		t.Fatal("no grip")
	}
	x0, y0 := center(g)
	tt.Move(x0, y0)
	tt.Frame()
	tt.Press(x0, y0)
	tt.Frame()
	for s := float32(1); s <= 10; s++ {
		tt.Move(x0-30*s, y0+25*s)
		tt.Frame()
	}
	tt.Release(x0-300, y0+250)
	tt.Frame()
	tt.Frame()
	g2, _ := tt.Find("拖动常用工具栏")
	if dx, dy := g2.X-g.X, g2.Y-g.Y; abs32(dx+300) > 2 || abs32(dy-250) > 2 {
		t.Fatalf("the grip moved by %v, %v, want -300, 250", dx, dy)
	}
	if a.set.FavX >= before || a.set.FavY <= 0.01 {
		t.Fatalf("kept at %v, %v", a.set.FavX, a.set.FavY)
	}
	if s := loadSettings(a.file); s.FavX != a.set.FavX || s.FavY != a.set.FavY {
		t.Fatalf("saved at %v, %v; it is at %v, %v", s.FavX, s.FavY, a.set.FavX, a.set.FavY)
	}
	// Far past the chart's corner: it stops at it.
	x0, y0 = center(g2)
	tt.Press(x0, y0)
	tt.Frame()
	for s := float32(1); s <= 10; s++ {
		tt.Move(x0+300*s, y0+300*s)
		tt.Frame()
	}
	tt.Release(x0+3000, y0+3000)
	tt.Frame()
	if a.set.FavX != 1 || a.set.FavY != 1 {
		t.Fatalf("past the corner: %v, %v", a.set.FavX, a.set.FavY)
	}
	bar, _ := tt.Find("常用工具栏")
	if bar.X+bar.W > chartLeft(a)+a.plotW+1 || bar.Y+bar.H > 820-timeH+1 {
		t.Fatalf("the bar %+v is off the chart", bar)
	}
	click(t, tt, "常用工具栏")
	if a.set.FavBar {
		t.Fatal("the switch did not hide it")
	}
	tt.Frame()
	if _, ok := tt.Find("拖动常用工具栏"); ok {
		t.Fatal("hidden, it shows")
	}
}

// Settings from before the bar get the default favorites; an emptied list
// stays empty; tools Candle does not have are dropped.
func TestFavoritesInTheSettings(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	os.WriteFile(file, []byte(`{"Symbol":"ETHUSDT"}`), 0o644)
	if s := loadSettings(file); !s.FavBar || len(s.FavTools) != 6 || s.FavX != 1 {
		t.Fatalf("old settings: %v %v %v", s.FavBar, s.FavTools, s.FavX)
	}
	os.WriteFile(file, []byte(`{"FavTools":[],"FavBar":false}`), 0o644)
	if s := loadSettings(file); s.FavBar || len(s.FavTools) != 0 {
		t.Fatalf("emptied: %v %v", s.FavBar, s.FavTools)
	}
	os.WriteFile(file, []byte(`{"FavTools":["trend","nope","trend","fib"],"FavX":7,"FavY":-1}`), 0o644)
	if s := loadSettings(file); !slices.Equal(s.FavTools, []string{"trend", "fib"}) || s.FavX != 1 || s.FavY != 0 {
		t.Fatalf("damaged: %v %v %v", s.FavTools, s.FavX, s.FavY)
	}
}
