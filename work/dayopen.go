package main

import (
	"context"
	"time"

	"candle/market"
)

// A dayOpen is a Binance market's price as the day began: its first
// minute's open, 0 until it is in. Binance's tickers count over the last 24
// hours; the watchlist and the headline count from 一天的起点, as Binance's
// app does when set to a day.
type dayOpen struct {
	day   int64 // when the day began, Unix ms
	price float64
	asked time.Time
}

// dayStart is when the day holding now began, as 一天的起点 says.
func (a *application) dayStart(now time.Time) int64 {
	n := now.In(a.set.Chart.breakLoc())
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location()).UnixMilli()
}

// change is sym's move as a fraction: a Binance market's since the day
// began, false until that price is in; another's since the last close, as
// Yahoo and Eastmoney tell it.
func (a *application) change(sym string, q market.Ticker) (float64, bool) {
	if market.IsListed(sym) {
		return q.Change(), true
	}
	o := a.dayOpens[sym]
	if o.price <= 0 || o.day != a.dayStart(time.Now()) {
		return 0, false
	}
	return q.Last/o.price - 1, true
}

// askDayOpens asks for the day's opening prices of the Binance markets
// among syms that lack them, one at a time, and again a minute after one
// did not come.
func (a *application) askDayOpens(syms []string) {
	day := a.dayStart(time.Now())
	var want []string
	var feeds []market.Feed
	for _, s := range syms {
		o, ok := a.dayOpens[s]
		if market.IsListed(s) || ok && o.day == day && (o.price > 0 || time.Since(o.asked) < time.Minute) {
			continue
		}
		a.dayOpens[s] = dayOpen{day: day, asked: time.Now()}
		want, feeds = append(want, s), append(feeds, a.feedOf(s))
	}
	if len(want) == 0 {
		return
	}
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i, s := range want {
			bars, err := feeds[i].Klines(ctx, s, "1m", day, 1)
			if err != nil || len(bars) == 0 {
				continue
			}
			b := bars[0]
			price := b.O
			if b.T < day { // nothing traded in the day's first minute: the price it began at
				price = b.C
			}
			a.post(func() {
				if o := a.dayOpens[s]; o.day == day {
					o.price = price
					a.dayOpens[s] = o
				}
			})
		}
	})
}
