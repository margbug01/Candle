package market

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Eastmoney's quotes, as its site has them: push2his the bars, push2 the
// prices, both from China without a proxy. A shares come 3 seconds late, Hong
// Kong's at once. It keeps a month and a half of minutes (a day of 1-minute
// bars) and the days and weeks since a market listed. Neither streams the
// way Binance does: Candle asks again every few seconds, as its site does.
const (
	eastHisBase  = "https://push2his.eastmoney.com"
	eastPushBase = "https://push2.eastmoney.com"
)

var _ Feed = Eastmoney{}

func (e Eastmoney) his() string  { return cmp.Or(e.His, eastHisBase) }
func (e Eastmoney) push() string { return cmp.Or(e.Push, eastPushBase) }

func (e Eastmoney) every() time.Duration {
	if e.Every > 0 {
		return e.Every
	}
	return 5 * time.Second
}

// eastInterval is Eastmoney's bar size (klt) for an interval: minutes, 101
// for days, 102 for weeks; hours and 4 hours, made of 5-minute bars, group
// them by 1 or 4 hours.
func eastInterval(interval string) (klt, group int, ok bool) {
	switch interval {
	case "1m":
		return 1, 0, true
	case "5m":
		return 5, 0, true
	case "15m":
		return 15, 0, true
	case "1h":
		return 5, 1, true
	case "4h":
		return 5, 4, true
	case "1d":
		return 101, 0, true
	case "1w":
		return 102, 0, true
	}
	return 0, 0, false
}

func (e Eastmoney) market(symbol string) (Instrument, error) {
	in, ok := InstrumentOf(symbol)
	if !ok || !IsEastmoney(symbol) {
		return Instrument{}, fmt.Errorf("eastmoney: no market %q", symbol)
	}
	return in, nil
}

// Klines returns up to limit bars opening at or before end (Unix ms, 0 for
// now), oldest first; fewer than limit means Eastmoney's history begins there.
func (e Eastmoney) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	in, err := e.market(symbol)
	if err != nil {
		return nil, err
	}
	klt, group, ok := eastInterval(interval)
	if !ok {
		return nil, fmt.Errorf("eastmoney: no %s bars", interval)
	}
	var bars []Bar
	if klt < 101 {
		// All the minutes there are, in one request: a day of 1 minute (240
		// in Shanghai, 330 in Hong Kong), a month and a half of 5 and 15
		// (about 1,600 and 550 in Hong Kong). Asking for more than there
		// could be looks like scraping to Eastmoney, which shuts out who does.
		bars, err = e.klines(ctx, in, klt, "20500101", map[int]int{1: 400, 5: 2500, 15: 900}[klt])
	} else {
		// A page of days or weeks dated up to end's day, the one opening
		// after end (that day's own) left out below.
		day := "20500101"
		if end > 0 {
			day = time.UnixMilli(end).In(in.Loc).Format("20060102")
		}
		bars, err = e.klines(ctx, in, klt, day, limit+1)
	}
	if err != nil {
		return nil, err
	}
	if group > 0 {
		bars = in.eastGroup(bars, group)
	}
	if end > 0 {
		bars = slices.DeleteFunc(bars, func(b Bar) bool { return b.T > end })
	}
	return bars[max(len(bars)-limit, 0):], nil
}

// klines asks for lmt of in's bars of klt dated up to day (YYYYMMDD),
// adjusted for dividends and splits from the newest back (前复权), as Chinese
// apps show them.
func (e Eastmoney) klines(ctx context.Context, in Instrument, klt int, day string, lmt int) ([]Bar, error) {
	q := url.Values{
		"secid": {in.Ticker}, "fields1": {"f1,f2,f3"}, "fields2": {"f51,f52,f53,f54,f55,f56"},
		"klt": {strconv.Itoa(klt)}, "fqt": {"1"}, "end": {day}, "lmt": {strconv.Itoa(lmt)},
	}
	body, err := get(ctx, e.HTTP, e.his()+"/api/qt/stock/kline/get?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("eastmoney %s: %w", in.Code, err)
	}
	var r struct {
		Data *struct {
			Klines []string `json:"klines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("eastmoney %s: %w", in.Code, err)
	}
	if r.Data == nil {
		return nil, fmt.Errorf("eastmoney: no market %s (%s)", in.Code, in.Ticker)
	}
	bars, err := in.eastBars(r.Data.Klines, klt)
	if err != nil {
		return nil, fmt.Errorf("eastmoney %s: %w", in.Code, err)
	}
	return bars, nil
}

// eastBars reads Eastmoney's bars, "2026-10-29 09:35,open,close,high,low,
// volume": a minute bar stamped in Beijing time at its end, a day by its
// date, a week by its last trading day's. Candle's open at their beginnings:
// 9:30, a day at its market's opening, a week at its Monday's. Shanghai's and
// Shenzhen's volumes come in lots of 100 shares.
func (in Instrument) eastBars(lines []string, klt int) ([]Bar, error) {
	lot := 1.0
	if in.Loc == shanghai {
		lot = 100
	}
	out := make([]Bar, 0, len(lines))
	for _, l := range lines {
		f := strings.Split(l, ",")
		if len(f) < 6 {
			return nil, fmt.Errorf("bar %q", l)
		}
		var v [5]float64
		for i := range v {
			x, err := strconv.ParseFloat(f[i+1], 64)
			if err != nil {
				return nil, fmt.Errorf("bar %q", l)
			}
			v[i] = x
		}
		var t time.Time
		if klt >= 101 {
			d, err := time.ParseInLocation("2006-01-02", f[0], in.Loc)
			if err != nil {
				return nil, fmt.Errorf("bar %q", l)
			}
			if klt == 102 {
				d = d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
			}
			t = d.Add(in.Opens)
		} else {
			e, err := time.ParseInLocation("2006-01-02 15:04", f[0], shanghai)
			if err != nil {
				return nil, fmt.Errorf("bar %q", l)
			}
			t = e.Add(-time.Duration(klt) * time.Minute)
		}
		out = append(out, Bar{T: t.UnixMilli(), O: v[0], C: v[1], H: v[2], L: v[3], V: v[4] * lot})
	}
	return out, nil
}

// eastGroup merges 5-minute bars into hour bars (n 1) or 4-hour ones (n 4)
// as TradingView makes them: from each part of the day's open, the last of
// a part shorter (Hong Kong's 11:30 to noon), 4 hours of four of those. A
// group whose first bars are not among bars is left out.
func (in Instrument) eastGroup(bars []Bar, n int) []Bar {
	starts := in.hourStarts(n)
	if len(starts) == 0 {
		return nil
	}
	var out []Bar
	partial := int64(-1)
	for i, b := range bars {
		key := in.bucket(b.T, starts)
		if i == 0 && key < b.T {
			partial = key
		}
		if key == partial {
			continue
		}
		if k := len(out) - 1; k >= 0 && out[k].T == key {
			o := &out[k]
			o.H, o.L, o.C, o.V = max(o.H, b.H), min(o.L, b.L), b.C, o.V+b.V
			continue
		}
		b.T = key
		out = append(out, b)
	}
	return out
}

// bucket is when the bar of starts holding t began.
func (in Instrument) bucket(t int64, starts []time.Duration) int64 {
	n := time.UnixMilli(t).In(in.Loc)
	day := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, in.Loc)
	s := starts[0]
	for _, x := range starts {
		if day.Add(x).UnixMilli() <= t {
			s = x
		}
	}
	return day.Add(s).UnixMilli()
}

// Stream calls fn with the newest two bars every few seconds, until ctx ends
// or a request fails.
func (e Eastmoney) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	in, err := e.market(symbol)
	if err != nil {
		return err
	}
	klt, group, ok := eastInterval(interval)
	if !ok {
		return fmt.Errorf("eastmoney: no %s bars", interval)
	}
	lmt := 2
	if group > 0 {
		lmt = 200 // two days of 5 minutes and more: the newest whole groups
	}
	for {
		now := time.Now()
		bars, err := e.klines(ctx, in, klt, "20500101", lmt)
		if err != nil {
			return fmt.Errorf("eastmoney stream: %w", err)
		}
		if group > 0 {
			bars = in.eastGroup(bars, group)
		}
		for _, b := range bars[max(len(bars)-2, 0):] {
			fn(b, false, now.UnixMilli())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.every()):
		}
	}
}

// Quotes is each market's price and its last session's close, fifty to a
// request.
func (e Eastmoney) Quotes(ctx context.Context, ids []string) ([]Ticker, error) {
	bySecid := map[string]string{}
	var secids []string
	for _, id := range ids {
		if in, ok := InstrumentOf(id); ok && IsEastmoney(id) {
			bySecid[in.Ticker] = id
			secids = append(secids, in.Ticker)
		}
	}
	var out []Ticker
	for i := 0; i < len(secids); i += 50 {
		q := url.Values{"fltt": {"2"}, "secids": {strings.Join(secids[i:min(i+50, len(secids))], ",")}, "fields": {"f2,f12,f13,f18"}}
		body, err := get(ctx, e.HTTP, e.push()+"/api/qt/ulist.np/get?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("eastmoney quotes: %w", err)
		}
		var r struct {
			Data *struct {
				Diff []struct {
					Last   json.RawMessage `json:"f2"`
					Code   string          `json:"f12"`
					Market int             `json:"f13"`
					Before json.RawMessage `json:"f18"`
				} `json:"diff"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("eastmoney quotes: %w", err)
		}
		if r.Data == nil {
			continue
		}
		for _, d := range r.Data.Diff {
			id, ok := bySecid[strconv.Itoa(d.Market)+"."+d.Code]
			last := number(d.Last)
			if !ok || last <= 0 { // "-": suspended, or not yet traded
				continue
			}
			t := Ticker{Symbol: id, Last: last, Open: last}
			if b := number(d.Before); b > 0 {
				t.Open = b
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// number is a price Eastmoney gives, or 0 for its "-".
func number(raw json.RawMessage) float64 {
	v, err := strconv.ParseFloat(strings.Trim(string(raw), `"`), 64)
	if err != nil || math.IsNaN(v) {
		return 0
	}
	return v
}

// Watch calls fn with each market's quote every few seconds, until ctx ends
// or a request fails.
func (e Eastmoney) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	for {
		got, err := e.Quotes(ctx, symbols)
		if err != nil {
			return err
		}
		for _, t := range got {
			fn(t)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.every()):
		}
	}
}

// Symbols looks markets up: their Chinese names and how many decimals their
// prices take.
func (e Eastmoney) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	var out []Symbol
	for _, n := range names {
		in, err := e.market(n)
		if err != nil {
			return nil, err
		}
		q := url.Values{"secid": {in.Ticker}, "fltt": {"2"}, "fields": {"f57,f58,f59"}}
		body, err := get(ctx, e.HTTP, e.push()+"/api/qt/stock/get?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("eastmoney %s: %w", in.Code, err)
		}
		var r struct {
			Data *struct {
				Name     string `json:"f58"`
				Decimals int    `json:"f59"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("eastmoney %s: %w", in.Code, err)
		}
		if r.Data == nil || r.Data.Name == "" {
			return nil, fmt.Errorf("eastmoney: no market %s (%s)", in.Code, in.Ticker)
		}
		out = append(out, Symbol{Name: in.ID, Base: in.Code, Tick: math.Pow(10, -float64(r.Data.Decimals)), Desc: cmp.Or(in.Name, r.Data.Name)})
	}
	return out, nil
}
