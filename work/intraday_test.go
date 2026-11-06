package main

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// pagedFeed answers Klines from bars by size, as Binance does: at most limit
// of them opening at or before end.
type pagedFeed struct {
	market.Feed
	bars  map[string][]market.Bar
	asked int
}

func (f *pagedFeed) Klines(_ context.Context, _, iv string, end int64, limit int) ([]market.Bar, error) {
	f.asked++
	all := f.bars[iv]
	if end > 0 {
		all = all[:sort.Search(len(all), func(i int) bool { return all[i].T > end })]
	}
	return all[max(len(all)-limit, 0):], nil
}

func run(from int64, n int, ivMs int64) []market.Bar {
	out := make([]market.Bar, n)
	for i := range out {
		out[i] = market.Bar{T: from + int64(i)*ivMs, O: 1, H: 1, L: 1, C: 1, V: 1}
	}
	return out
}

func TestDayBars(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	from, to := day, day+86_400_000
	ctx := context.Background()

	// Three days of minutes: the middle one takes two pages of a thousand.
	f := &pagedFeed{bars: map[string][]market.Bar{"1m": run(day-86_400_000, 3*1440, 60_000)}}
	got, iv, err := dayBars(ctx, f, "BTCUSDT", from, from, to)
	if err != nil || iv != "1m" || len(got) != 1440 || got[0].T != from || got[1439].T != to-60_000 || f.asked != 2 {
		t.Fatalf("a day of minutes: %d bars of %q, %d asked, %v", len(got), iv, f.asked, err)
	}

	// Minutes from the day's noon on only: the 5 minutes, which go back further.
	f = &pagedFeed{bars: map[string][]market.Bar{
		"1m": run(day+43_200_000, 2*1440, 60_000),
		"5m": run(day-86_400_000, 3*288, 300_000),
	}}
	if got, iv, _ := dayBars(ctx, f, "BTCUSDT", from, from, to); iv != "5m" || len(got) != 288 {
		t.Fatalf("minutes missing: %d bars of %q", len(got), iv)
	}

	// Eastmoney's minutes begin at today's open, and today's auction before
	// it: whole.
	open := day + 34_200_000
	f = &pagedFeed{bars: map[string][]market.Bar{"1m": append(run(open-60_000, 1, 60_000), run(open, 240, 60_000)...)}}
	if got, iv, _ := dayBars(ctx, f, "SSE:600519", open-600_000, open, open+86_400_000); iv != "1m" || len(got) != 241 {
		t.Fatalf("today's minutes: %d bars of %q", len(got), iv)
	}

	// Neither goes back that far.
	f = &pagedFeed{bars: map[string][]market.Bar{"1m": run(to, 100, 60_000), "5m": run(to, 100, 300_000)}}
	if got, iv, _ := dayBars(ctx, f, "BTCUSDT", from, from, to); got != nil || iv != "" {
		t.Fatalf("too old: %d bars of %q", len(got), iv)
	}
}

func TestIntradayDoubleClick(t *testing.T) {
	a := testApp()
	a.open("BTCUSDT", "1d")
	a.loading, a.bars, a.atStart = false, fakeBars(300, 86_400_000), true
	var spawned []func()
	a.spawn = func(f func()) { spawned = append(spawned, f) }
	tt := newTester(a, true)
	n := len(a.bars)
	i := n - 10
	b := a.bars[i]
	x := a.view.x(i, a.plotW, n)

	a.view.offset = -40 // a double click above the candle still goes back to the newest
	a.chartInput(ui.InputEvent{Kind: ui.InputPointerDown, X: x, Y: a.shown.y(b.H, a.axis) - 30, Clicks: 2})
	if a.intra != nil || a.view.offset != a.home() {
		t.Fatalf("beside the candle: intraday %v, offset %v", a.intra != nil, a.view.offset)
	}
	tt.Frame()
	x = a.view.x(i, a.plotW, n)
	before := len(spawned)
	a.chartInput(ui.InputEvent{Kind: ui.InputPointerDown, X: x, Y: a.shown.y((b.O+b.C)/2, a.axis), Clicks: 2})
	d := a.intra
	if d == nil || !d.loading || d.day != b || d.prev != a.bars[i-1].C || len(spawned) != before+1 {
		t.Fatalf("on the candle: %+v, %d spawned", d, len(spawned)-before)
	}
	tt.Frame()
	if !tt.HasText("正在加载分时…") {
		t.Fatal("no word of the minutes loading")
	}
	d.loading, d.iv, d.bars = false, "1m", fakeBars(900, 60_000)
	tt.Frame()
	tt.Move(700, 500)
	tt.Frame()
	shot(t, tt, "intraday")
	click(t, tt, "关闭分时")
	if a.intra != nil {
		t.Fatal("关闭分时 left it open")
	}

	a.open("BTCUSDT", "1h") // only on days
	a.loading, a.bars = false, fakeBars(300, 3_600_000)
	tt.Frame()
	n = len(a.bars)
	b = a.bars[n-10]
	a.chartInput(ui.InputEvent{Kind: ui.InputPointerDown, X: a.view.x(n-10, a.plotW, n), Y: a.shown.y((b.O+b.C)/2, a.axis), Clicks: 2})
	if a.intra != nil {
		t.Fatal("hours opened the minutes")
	}
}
