package main

import (
	"bytes"
	"candle/market"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

var magenta = color.RGBA{255, 0, 255, 255}

// iconPNG is a square of c, 32 pixels across, as a PNG.
func iconPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeBinance counts what is asked of it; a nil list or icon fails.
type fakeBinance struct {
	urls           map[string]string
	icon           []byte
	lists, fetches int
	got            []string        // the addresses fetched
	none           map[string]bool // addresses with no picture
}

func (f *fakeBinance) list(context.Context) (map[string]string, error) {
	f.lists++
	if f.urls == nil {
		return nil, errors.New("no list")
	}
	return f.urls, nil
}

func (f *fakeBinance) fetch(_ context.Context, u string) ([]byte, error) {
	f.fetches++
	f.got = append(f.got, u)
	if f.none[u] {
		return nil, fmt.Errorf("icon %s: %w", u, market.ErrNoIcon)
	}
	if f.icon == nil {
		return nil, errors.New("no icon")
	}
	return f.icon, nil
}

// iconApp is testApp with its background work done at once, its icons from
// f, kept in dir.
func iconApp(dir string, f *fakeBinance) *application {
	a := testApp()
	a.spawn = func(fn func()) { fn() }
	a.icons = newIcons(dir, f.list, f.fetch)
	return a
}

// iconOf asks for coin's icon a few times, as frames do: reading the list,
// asking Binance for it again for a coin it lacks, fetching the icon.
func iconOf(a *application, coin string) *ui.Bitmap {
	for range 5 {
		if b := a.icon(coin); b != nil {
			return b
		}
	}
	return nil
}

func TestIconsAreFetchedOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	f := &fakeBinance{urls: map[string]string{"BTC": "https://x/btc.png", "币安人生": "https://x/life.png"}, icon: iconPNG(t, magenta)}
	a := iconApp(dir, f)
	if a.icon("BTC") != nil {
		t.Fatal("an icon before the list")
	}
	for _, coin := range []string{"BTC", "币安人生"} {
		if b := iconOf(a, coin); b == nil {
			t.Fatalf("no icon for %s", coin)
		} else if w, h := b.Size(); w != 32 || h != 32 {
			t.Fatalf("%s's icon is %dx%d", coin, w, h)
		}
	}
	if iconOf(a, "BTC"); f.lists != 1 || f.fetches != 2 {
		t.Fatalf("asked for the list %d times and icons %d times, want 1 and 2", f.lists, f.fetches)
	}
	if _, err := os.Stat(filepath.Join(dir, "icons.json")); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(dir, "icons"))
	var names []string
	for _, e := range files {
		names = append(names, e.Name())
	}
	if len(names) != 2 || !strings.HasPrefix(names[0], "BTC-") || !strings.HasPrefix(names[1], "x") {
		t.Fatalf("kept %v", names) // the Chinese name kept in hex, nothing half written
	}

	// Another run, Binance out of reach: all from the folder.
	off := &fakeBinance{}
	b := iconApp(dir, off)
	if iconOf(b, "BTC") == nil || iconOf(b, "币安人生") == nil || off.lists != 0 || off.fetches != 0 {
		t.Fatalf("from the folder: asked for the list %d times and icons %d times", off.lists, off.fetches)
	}
}

// A week old, the list is asked for again; Binance not answering, the old
// one stands. A coin it lacks has the list asked for once more in a run.
func TestIconListRefreshes(t *testing.T) {
	dir := t.TempDir()
	writeIconList(dir, map[string]string{"BTC": "https://x/btc.png"})
	old := time.Now().Add(-iconListAge - time.Hour)
	os.Chtimes(iconListFile(dir), old, old)

	off := &fakeBinance{icon: iconPNG(t, magenta)}
	a := iconApp(dir, off)
	if iconOf(a, "BTC") == nil || off.lists != 1 {
		t.Fatalf("a stale list and no answer: icon missing, or the list asked for %d times", off.lists)
	}

	f := &fakeBinance{urls: map[string]string{"BTC": "https://x/btc.png", "NEW": "https://x/new.png"}, icon: iconPNG(t, magenta)}
	b := iconApp(dir, f)
	if iconOf(b, "BTC") == nil || f.lists != 1 {
		t.Fatalf("a stale list: the list asked for %d times", f.lists)
	}
	if fi, err := os.Stat(iconListFile(dir)); err != nil || time.Since(fi.ModTime()) > time.Minute {
		t.Fatal("the new list was not kept")
	}

	// The list kept is fresh now, but lacks a coin listed since.
	writeIconList(dir, map[string]string{"BTC": "https://x/btc.png"})
	c := iconApp(dir, f)
	f.lists = 0
	if iconOf(c, "NEW") == nil || f.lists != 1 {
		t.Fatalf("a coin the list lacks: asked for it %d times", f.lists)
	}
	if iconOf(c, "NONE") != nil || f.lists != 1 {
		t.Fatalf("a coin Binance has no icon for: the list asked for %d times", f.lists)
	}
}

// An icon that did not come is asked for again after a while, not every
// frame; one kept damaged is fetched again.
func TestIconFailureWaits(t *testing.T) {
	dir := t.TempDir()
	f := &fakeBinance{urls: map[string]string{"BTC": "https://x/btc.png"}}
	a := iconApp(dir, f)
	if iconOf(a, "BTC") != nil || iconOf(a, "BTC") != nil || f.fetches != 1 {
		t.Fatalf("failing: fetched %d times", f.fetches)
	}
	f.icon = iconPNG(t, magenta)
	a.icons.failed["BTC"] = time.Now().Add(-iconRetry)
	if iconOf(a, "BTC") == nil || f.fetches != 2 {
		t.Fatalf("after the wait: fetched %d times", f.fetches)
	}

	file := iconFile(dir, "BTC", "https://x/btc.png")
	os.WriteFile(file, []byte("<html>"), 0o644)
	b := iconApp(dir, f)
	if iconOf(b, "BTC") == nil || f.fetches != 3 {
		t.Fatalf("a damaged file: fetched %d times", f.fetches)
	}
	if data, _ := os.ReadFile(file); !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatal("the damaged file stays")
	}
}

func TestInitial(t *testing.T) {
	for coin, want := range map[string]string{"btc": "B", "1000SATS": "S", "币安人生": "币", "1000": "1", "": "?"} {
		if got := initial(coin); got != want {
			t.Errorf("initial(%q) = %q, want %q", coin, got, want)
		}
	}
}

// count is how many pixels in r are c.
func count(img *image.RGBA, r image.Rectangle, c color.RGBA) (n int) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) == c {
				n++
			}
		}
	}
	return n
}

// The icons show in the top bar, the legend unless the settings say not, the
// watchlist and the search; a coin without one has its letter.
func TestIconsShow(t *testing.T) {
	a := testApp()
	icon, err := ui.DecodeBitmap(iconPNG(t, magenta))
	if err != nil {
		t.Fatal(err)
	}
	a.icons.urls = map[string]string{}
	for _, coin := range []string{"BTC", "ETH", "SOL", "BNB", "XRP", "DOGE", "SUI", "PEPE"} { // not ADA
		a.icons.bitmaps[coin] = icon
	}
	tt := newTester(a, true)
	disc := 200 // of a 20 DIP disc's 314 pixels, at least
	top := image.Rect(0, 0, 500, titleBarHeight)
	left := int(chartLeft(a))
	legend := image.Rect(left, titleBarHeight, left+300, titleBarHeight+40)
	watch := image.Rect(1320-watchW, titleBarHeight, 1320, 820)
	img := tt.Image()
	if n := count(img, top, magenta); n < disc {
		t.Fatalf("%d icon pixels in the top bar", n)
	}
	if n := count(img, legend, magenta); n < disc*3/4 {
		t.Fatalf("%d icon pixels in the legend", n)
	}
	if n, want := count(img, watch, magenta), disc*(len(a.set.Watch)-1); n < want {
		t.Fatalf("%d icon pixels in the watchlist, want %d", n, want)
	}
	if !tt.HasText("A") {
		t.Fatal("ADA has no letter")
	}
	shot(t, tt, "icons")

	a.set.Chart.Logo = false
	tt.Frame()
	if n := count(tt.Image(), legend, magenta); n != 0 {
		t.Fatalf("the legend's icon turned off: %d of its pixels", n)
	}
	a.query = "pe"
	tt.Frame()
	tt.Frame()
	if n := count(tt.Image(), watch, magenta); n < disc*2 { // PEPE/USDT and PEPE/FDUSD
		t.Fatalf("%d icon pixels in the search", n)
	}
}

// A stock's logo comes from FMP, without Binance's list, kept like a coin's;
// a stock with none shows its letter and is not asked for again.
func TestStockLogos(t *testing.T) {
	dir := t.TempDir()
	f := &fakeBinance{icon: iconPNG(t, magenta), none: map[string]bool{market.StockLogo("ZZZZ"): true}}
	a := iconApp(dir, f)
	if iconOf(a, "US:NVDA") == nil || f.lists != 0 || len(f.got) != 1 || f.got[0] != "https://financialmodelingprep.com/image-stock/NVDA.png" {
		t.Fatalf("NVDA: lists %d, fetched %v", f.lists, f.got)
	}
	if b := iconApp(dir, f); iconOf(b, "US:NVDA") == nil || f.fetches != 1 {
		t.Fatalf("NVDA's logo not kept: %d fetches", f.fetches)
	}
	for range 3 {
		a.icon("US:ZZZZ")
	}
	if f.fetches != 2 || a.icon("US:ZZZZ") != nil {
		t.Fatalf("a stock without a logo: %d fetches", f.fetches)
	}
	if a.icon("CME:NQ") != nil || f.fetches != 2 {
		t.Fatal("a future's icon asked for")
	}
}
