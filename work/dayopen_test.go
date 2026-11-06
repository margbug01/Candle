package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"candle/market"
)

// klineTransport answers Binance's klines with one bar opening at the
// endTime asked for, opening at open, and notes what was asked.
type klineTransport struct {
	open  float64
	asked []string
}

func (k *klineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	q := r.URL.Query()
	k.asked = append(k.asked, q.Get("symbol")+" "+q.Get("interval")+" "+q.Get("endTime"))
	body := fmt.Sprintf(`[[%s,"%g","1","1","1","1",0,"0",0,"0","0","0"]]`, q.Get("endTime"), k.open)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func TestDayOpens(t *testing.T) {
	kt := &klineTransport{open: 80000}
	a := newApplication(defaultSettings(), "", market.Binance{HTTP: &http.Client{Transport: kt}}, func(f func()) { f() })
	var spawned []func()
	a.spawn = func(f func()) { spawned = append(spawned, f) }
	a.set.Chart.Zone = "Asia/Shanghai"
	q := market.Ticker{Symbol: "BTCUSDT", Open: 78000, Last: 80800}
	if _, ok := a.change("BTCUSDT", q); ok {
		t.Fatal("a move before the day's open is in")
	}
	a.askDayOpens([]string{"BTCUSDT", "US:AAPL"})
	a.askDayOpens([]string{"BTCUSDT"}) // asked already
	if len(spawned) != 1 {
		t.Fatalf("%d asked", len(spawned))
	}
	spawned[0]()
	now := time.Now().In(a.set.Chart.loc())
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	if want := fmt.Sprint("BTCUSDT 1m ", midnight); len(kt.asked) != 1 || kt.asked[0] != want {
		t.Fatalf("asked %q, want %q", kt.asked, want)
	}
	// From the day's open, not the 24 hours' (+3.59%).
	if got, ok := a.change("BTCUSDT", q); !ok || math.Abs(got-0.01) > 1e-9 {
		t.Fatalf("move %v %v, want +1%%", got, ok)
	}
	// A stock's is Yahoo's, since the last close.
	if got, ok := a.change("US:AAPL", market.Ticker{Open: 100, Last: 102}); !ok || math.Abs(got-0.02) > 1e-9 {
		t.Fatalf("a stock's move %v %v", got, ok)
	}

	// Another beginning of the day is another open, asked for again.
	a.set.Chart.BreakZone = "utc"
	if _, ok := a.change("BTCUSDT", q); ok {
		t.Fatal("the day's open kept for another beginning")
	}
	a.askDayOpens([]string{"BTCUSDT"})
	spawned[1]()
	if utc := time.Now().UTC().Truncate(24 * time.Hour).UnixMilli(); kt.asked[1] != fmt.Sprint("BTCUSDT 1m ", utc) {
		t.Fatalf("asked %q from UTC's midnight", kt.asked[1])
	}
}
