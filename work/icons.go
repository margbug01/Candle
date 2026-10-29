package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"image"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// The coins' icons go beside their names: in the watchlist, the search, the
// top bar and the legend. Binance's website lists them (market.Binance.Icons),
// and the list and the icons are kept in the cache folder, so each is
// fetched once: ~/Library/Caches/Candle on macOS, %LOCALAPPDATA%\Candle on
// Windows. Until a coin's icon is in, or when it has none, or binance.com is
// out of reach (it takes a proxy from mainland China), its first letter on a
// disc stands in.

const (
	iconListAge  = 7 * 24 * time.Hour // the list is asked for again after a week
	iconRetry    = 10 * time.Minute   // what did not come is asked for again after this
	iconParallel = 4                  // downloads at once: a search shows 50 coins
)

// icons is where the icons are at. Like the rest of application it belongs
// to the main thread: the list and the icons come in with spawn and post.
type icons struct {
	dir   string // the cache folder, "" to keep nothing
	list  func(context.Context) (map[string]string, error)
	fetch func(ctx context.Context, url string) ([]byte, error)

	urls     map[string]string // the icons' addresses by coin, once the list is in
	listing  bool              // the list is on its way
	asked    bool              // Binance gave the list this run
	listFail time.Time         // when it last did not come
	bitmaps  map[string]*ui.Bitmap
	light    map[string]bool      // logos drawn in white, for a dark page
	busy     map[string]bool      // coming
	failed   map[string]time.Time // did not come, and when
	slots    chan struct{}        // bounds the downloads
}

func newIcons(dir string, list func(context.Context) (map[string]string, error), fetch func(context.Context, string) ([]byte, error)) icons {
	return icons{
		dir: dir, list: list, fetch: fetch,
		bitmaps: map[string]*ui.Bitmap{}, light: map[string]bool{}, busy: map[string]bool{}, failed: map[string]time.Time{},
		slots: make(chan struct{}, iconParallel),
	}
}

// iconDir is the cache folder for the icons, "" when there is none.
func iconDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Candle")
}

// loadIcons reads the list of icons from the cache folder or, a week old or
// missing, from Binance, keeping the old one if Binance does not answer.
// remote asks Binance whatever the folder holds: for a coin the list kept
// lacks, listed since.
func (a *application) loadIcons(remote bool) {
	ic := &a.icons
	if ic.listing || time.Since(ic.listFail) < iconRetry || ic.urls != nil && !remote {
		return
	}
	ic.listing = true
	dir, list := ic.dir, ic.list
	a.spawn(func() {
		var urls map[string]string
		fresh, asked := false, false
		if !remote {
			urls, fresh = readIconList(dir)
		}
		if !fresh {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			got, err := list(ctx)
			cancel()
			if err == nil {
				urls, asked = got, true
				writeIconList(dir, got)
			}
		}
		a.post(func() {
			ic.listing = false
			ic.asked = ic.asked || asked
			if urls == nil {
				ic.listFail = time.Now()
				return
			}
			ic.urls = urls
		})
	})
}

// icon is coin's icon, nil until it is in or when there is none. Asked for
// the first time, it is read from the cache folder or downloaded.
func (a *application) icon(coin string) *ui.Bitmap {
	ic := &a.icons
	if b := ic.bitmaps[coin]; b != nil {
		return b
	}
	var u string
	if code, ok := strings.CutPrefix(coin, "US:"); ok { // a stock's logo: no list to ask
		u = market.StockLogo(code)
	} else {
		if ic.urls == nil {
			a.loadIcons(false)
			return nil
		}
		u = ic.urls[coin]
		if u == "" && !ic.asked { // a coin listed since the list was kept
			a.loadIcons(true)
		}
	}
	if u == "" || ic.busy[coin] || time.Since(ic.failed[coin]) < iconRetry {
		return nil
	}
	ic.busy[coin] = true
	file := iconFile(ic.dir, coin, u)
	fetch, slots := ic.fetch, ic.slots
	a.spawn(func() {
		data, err := os.ReadFile(file)
		var b *ui.Bitmap
		if err == nil {
			b, err = ui.DecodeBitmap(data)
		}
		if err != nil { // not kept yet, or damaged
			slots <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			data, err = fetch(ctx, u)
			cancel()
			<-slots
			if err == nil {
				if b, err = ui.DecodeBitmap(data); err == nil {
					writeFile(file, data)
				}
			}
		}
		light := err == nil && strings.HasPrefix(coin, "US:") && whiteLogo(data)
		a.post(func() {
			delete(ic.busy, coin)
			ic.light[coin] = light
			if err != nil {
				ic.failed[coin] = time.Now()
				if errors.Is(err, market.ErrNoIcon) { // none there: not asked again this run
					ic.failed[coin] = time.Now().Add(100 * 365 * 24 * time.Hour)
				}
				return
			}
			delete(ic.failed, coin)
			ic.bitmaps[coin] = b
		})
	})
	return nil
}

// symIcon is sym's icon, size across: a coin's picture, a stock's logo, or
// for a future, and until a picture is in, the first letter of its code.
func (a *application) symIcon(c *ui.Context, sym string, size float32) ui.Element {
	base, _ := a.names(sym)
	switch {
	case strings.HasPrefix(sym, "US:"):
		if b := a.icon(sym); b != nil {
			return logoDisc(c, b, size, a.icons.light[sym])
		}
		return letterDisc(c, base, size)
	case market.IsYahoo(sym):
		return letterDisc(c, base, size)
	}
	return a.coinIcon(c, iconCoin(base), size)
}

// iconCoin is the coin whose icon a market's goes by: a perpetual counted in
// thousands, 1000PEPE, has PEPE's.
func iconCoin(base string) string {
	for _, p := range []string{"1000000", "10000", "1000", "1M"} {
		if c, ok := strings.CutPrefix(base, p); ok && c != "" && c[0] >= 'A' && c[0] <= 'Z' {
			return c
		}
	}
	return base
}

// logoDisc is a company's logo on a white disc, a little inside it: the
// logos are drawn for a white page, and a disc of their own size would cut
// their corners off and lose a black one on the dark theme. Those drawn in
// white, for a dark page (Invesco's QQQ), go on a dark one.
func logoDisc(c *ui.Context, b *ui.Bitmap, size float32, light bool) ui.Element {
	d := float32(math.Round(float64(size) * 0.72))
	bg := ui.Hex("#ffffff")
	if light {
		bg = ui.Hex("#1e222d")
	}
	return ui.Box(c).Size(size, size).Radius(size/2).Shrink(0).Background(bg).Border(1, ui.RGBA(0, 0, 0, 0.08)).Center().
		Children(func() { ui.Image(c, b).Size(d, d) })
}

// whiteLogo is whether a logo is drawn in white on nothing: nearly all that
// it covers near white.
func whiteLogo(data []byte) bool {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	b := img.Bounds()
	var covered, white int
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			covered++
			// Unpremultiplied, near white.
			if r*0xffff/a > 0xe000 && g*0xffff/a > 0xe000 && bl*0xffff/a > 0xe000 {
				white++
			}
		}
	}
	return covered > 0 && white*10 >= covered*9
}

// coinIcon is coin's icon, size across, as a disc: the picture, or its first
// letter until there is one.
func (a *application) coinIcon(c *ui.Context, coin string, size float32) ui.Element {
	if b := a.icon(coin); b != nil {
		return ui.Image(c, b).Size(size, size).Radius(size / 2).Shrink(0)
	}
	return letterDisc(c, coin, size)
}

// letterDisc is name's first letter on a disc of its color.
func letterDisc(c *ui.Context, coin string, size float32) ui.Element {
	bg := discColor(coin)
	return ui.Box(c).Size(size, size).Radius(size / 2).Shrink(0).Center().Background(bg).Children(func() {
		// The letter lets the pointer through to the disc: in the legend the
		// disc lets it through to the chart.
		ui.Text(c, initial(coin)).FontSize(float32(int(size * 0.5))).FontWeight(600).TextColor(onColor(bg)).SingleLine().PassThrough()
	})
}

// discColors are the stand-ins' colors, one for each coin by its name.
var discColors = []ui.Color{
	ui.Hex("#f7931a"), ui.Hex("#627eea"), ui.Hex("#26a17b"), ui.Hex("#e84142"), ui.Hex("#8247e5"),
	ui.Hex("#2775ca"), ui.Hex("#f0b90b"), ui.Hex("#14b8a6"), ui.Hex("#ec4899"), ui.Hex("#64748b"),
}

func discColor(coin string) ui.Color {
	h := fnv.New32a()
	h.Write([]byte(coin))
	return discColors[h.Sum32()%uint32(len(discColors))]
}

// initial is the first letter of coin, as 1000SATS's S; its first character
// when it has no letter.
func initial(coin string) string {
	for _, r := range coin {
		if unicode.IsLetter(r) {
			return string(unicode.ToUpper(r))
		}
	}
	for _, r := range coin {
		return string(r)
	}
	return "?"
}

// iconFile is where coin's icon from u is kept: its name carries the
// address, so an icon Binance changes is fetched again.
func iconFile(dir, coin, u string) string {
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(u))
	name := coin
	for _, r := range coin {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			name = "x" + hex.EncodeToString([]byte(coin)) // nothing a file name could choke on
			break
		}
	}
	return filepath.Join(dir, "icons", name+"-"+hex.EncodeToString(sum[:4]))
}

func iconListFile(dir string) string { return filepath.Join(dir, "icons.json") }

// readIconList is the list kept in dir, if any, and whether it is under a
// week old.
func readIconList(dir string) (map[string]string, bool) {
	if dir == "" {
		return nil, false
	}
	f := iconListFile(dir)
	data, err := os.ReadFile(f)
	if err != nil {
		return nil, false
	}
	var urls map[string]string
	if json.Unmarshal(data, &urls) != nil || len(urls) == 0 {
		return nil, false
	}
	fi, err := os.Stat(f)
	return urls, err == nil && time.Since(fi.ModTime()) < iconListAge
}

func writeIconList(dir string, urls map[string]string) {
	if dir == "" {
		return
	}
	if data, err := json.Marshal(urls); err == nil {
		writeFile(iconListFile(dir), data)
	}
}

// writeFile writes beside name and renames over it, so a reader never sees
// half of it. An empty name keeps nothing.
func writeFile(name string, data []byte) {
	if name == "" || os.MkdirAll(filepath.Dir(name), 0o755) != nil {
		return
	}
	tmp := name + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		os.Remove(tmp)
		return
	}
	if os.Rename(tmp, name) != nil {
		os.Remove(tmp)
	}
}
