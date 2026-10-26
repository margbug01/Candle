// Command probe checks the candle sources from this machine: how far history
// goes, how fast pages come back, and whether live bars arrive and agree with REST.
//
//	go run ./cmd/probe binance [-proxy] [-symbol BTCUSDT] [-interval 1m] [-pages 5] [-live 70s]
//	go run ./cmd/probe yahoo [-direct] [-symbol AAPL] [-interval 1d] [-days 0]
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"
	_ "time/tzdata" // exchange time zones on machines without a Go install

	"candle/market"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe binance|yahoo [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "binance":
		err = probeBinance(os.Args[2:])
	case "yahoo":
		err = probeYahoo(os.Args[2:])
	default:
		err = fmt.Errorf("unknown source %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		os.Exit(1)
	}
}

// client connects directly, or through the Windows system proxy.
func client(proxy bool) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	if proxy {
		t.Proxy = market.SystemProxy
	}
	return &http.Client{Transport: t}
}

func probeBinance(args []string) error {
	fs := flag.NewFlagSet("binance", flag.ExitOnError)
	proxy := fs.Bool("proxy", false, "go through the system proxy instead of connecting directly")
	symbol := fs.String("symbol", "BTCUSDT", "spot symbol")
	interval := fs.String("interval", "1m", "kline interval")
	pages := fs.Int("pages", 5, "pages of 1000 bars to walk back, 0 for all of them")
	live := fs.Duration("live", 70*time.Second, "how long to watch the stream, 0 to skip it")
	fs.Parse(args)

	ctx := context.Background()
	b := market.Binance{HTTP: client(*proxy)}

	var bars []market.Bar
	start := time.Now()
	for page, end := 1, int64(0); *pages == 0 || page <= *pages; page++ {
		t0 := time.Now()
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		got, err := b.Klines(c, *symbol, *interval, end, 1000)
		cancel()
		if err != nil {
			return err
		}
		if len(got) > 0 {
			fmt.Printf("page %d: %4d bars  %s .. %s  %v\n", page, len(got), stamp(got[0].T), stamp(got[len(got)-1].T), since(t0))
			bars = append(got, bars...)
			end = got[0].T - 1
		}
		if len(got) < 1000 {
			fmt.Println("reached the first bar")
			break
		}
	}
	if len(bars) == 0 {
		return fmt.Errorf("no %s bars", *symbol)
	}
	fmt.Printf("history: %d bars in %v\n", len(bars), since(start))
	if ms := stepMs(*interval); ms > 0 {
		gs := gaps(bars, ms)
		fmt.Printf("gaps: %d\n", len(gs))
		for _, g := range gs[:min(len(gs), 5)] {
			fmt.Printf("  %s -> %s\n", stamp(g[0]), stamp(g[1]))
		}
	}
	if *live <= 0 {
		return nil
	}

	t0 := time.Now()
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	server, err := b.ServerTime(c)
	cancel()
	if err != nil {
		return err
	}
	rtt := time.Since(t0)
	offset := server - t0.Add(rtt/2).UnixMilli()
	fmt.Printf("clock: Binance is %+dms from this machine (round trip %v)\n", offset, rtt.Round(time.Millisecond))

	sc, stop := context.WithTimeout(ctx, *live)
	defer stop()
	newest := bars[len(bars)-1].T
	var lags []int64
	var closed []market.Bar
	t1 := time.Now()
	err = b.Stream(sc, *symbol, *interval, func(bar market.Bar, final bool, at int64) {
		if len(lags) == 0 {
			fmt.Printf("stream: first update after %v, bar %s, REST newest %s\n", since(t1), stamp(bar.T), stamp(newest))
		}
		lags = append(lags, time.Now().UnixMilli()+offset-at)
		if final {
			closed = append(closed, bar)
			fmt.Printf("closed %s  O %g H %g L %g C %g V %g\n", stamp(bar.T), bar.O, bar.H, bar.L, bar.C, bar.V)
		}
	})
	if sc.Err() == nil {
		return err // the stream ended before the window did
	}
	if len(lags) == 0 {
		return fmt.Errorf("no stream updates in %v", *live)
	}
	slices.Sort(lags)
	fmt.Printf("stream: %d updates, %d closed bars, lag median %dms max %dms\n", len(lags), len(closed), lags[len(lags)/2], lags[len(lags)-1])

	for _, want := range closed {
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		got, err := b.Klines(c, *symbol, *interval, want.T, 1)
		cancel()
		if err != nil {
			return err
		}
		if len(got) == 1 && got[0] == want {
			fmt.Printf("REST matches the stream on %s\n", stamp(want.T))
		} else {
			fmt.Printf("MISMATCH on %s: stream %+v, REST %+v\n", stamp(want.T), want, got)
		}
	}
	return nil
}

func probeYahoo(args []string) error {
	fs := flag.NewFlagSet("yahoo", flag.ExitOnError)
	direct := fs.Bool("direct", false, "connect directly instead of through the system proxy")
	symbol := fs.String("symbol", "AAPL", "Yahoo symbol")
	interval := fs.String("interval", "1d", "1m 5m 15m 30m 1h 1d 1wk 1mo")
	days := fs.Int("days", 0, "how far back, 0 for as far as Yahoo serves at that interval")
	fs.Parse(args)

	to := time.Now()
	from := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC) // Yahoo takes negative period1; ^GSPC starts in 1927
	if *days == 0 {
		*days = map[string]int{"1m": 7, "2m": 59, "5m": 59, "15m": 59, "30m": 59, "90m": 59, "60m": 729, "1h": 729}[*interval]
	}
	if *days > 0 {
		from = to.AddDate(0, 0, -*days)
	}

	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t0 := time.Now()
	chart, err := market.Yahoo{HTTP: client(!*direct)}.Chart(c, *symbol, *interval, from.Unix(), to.Unix())
	if err != nil {
		return err
	}
	fmt.Printf("%s %s: %d bars in %v, %d empty rows skipped, %d splits, zone %s\n",
		*symbol, *interval, len(chart.Bars), since(t0), chart.Skipped, chart.Splits, chart.Timezone)
	if len(chart.Bars) == 0 {
		return nil
	}
	loc, err := time.LoadLocation(chart.Timezone)
	if err != nil {
		return err
	}
	for _, b := range []market.Bar{chart.Bars[0], chart.Bars[len(chart.Bars)-1]} {
		fmt.Printf("  %s  O %g H %g L %g C %g V %g\n", time.UnixMilli(b.T).In(loc).Format("2006-01-02 15:04 MST"), b.O, b.H, b.L, b.C, b.V)
	}
	fmt.Printf("newest bar opened %v ago\n", time.Since(time.UnixMilli(chart.Bars[len(chart.Bars)-1].T)).Round(time.Minute))
	return nil
}

// stepMs is a Binance interval's length in ms, 0 for 1M whose months differ.
func stepMs(interval string) int64 {
	if len(interval) < 2 {
		return 0
	}
	n, err := strconv.Atoi(interval[:len(interval)-1])
	if err != nil {
		return 0
	}
	unit := map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[interval[len(interval)-1]]
	return int64(n) * unit.Milliseconds()
}

func gaps(bars []market.Bar, ms int64) (gs [][2]int64) {
	for i := 1; i < len(bars); i++ {
		if bars[i].T-bars[i-1].T != ms {
			gs = append(gs, [2]int64{bars[i-1].T, bars[i].T})
		}
	}
	return gs
}

func stamp(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04Z") }

func since(t time.Time) time.Duration { return time.Since(t).Round(time.Millisecond) }
