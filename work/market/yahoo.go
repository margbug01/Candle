package market

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Yahoo reads the chart, spark and search endpoints behind Yahoo Finance's
// own pages, for CME futures and US stocks. They need no key, are not a
// published API, and can change or throttle without notice. From mainland
// China they take a proxy (the system's, like binance.com's).
//
// CME futures come 10 minutes late: Yahoo has no licence to show them live,
// and no free source does. The prices are the exchange's own trades, on the
// contract's tick; mainland portals' live futures prices are smoothed
// estimates instead, off by points.
type Yahoo struct {
	HTTP  *http.Client
	Base  string        // the endpoints' host, "" for Yahoo's; for the tests
	Every time.Duration // how often Stream and Watch ask, 0 for 5 and 10 seconds
	// RTH keeps a future's intraday bars to the regular trading hours, 8:30
	// to 15:15 in Chicago (9:30 to 16:15 New York time), as TradingView's
	// RTH does for every CME, CBOT, NYMEX and COMEX contract; without it
	// they cover the whole electronic session (ETH). Days and weeks are the
	// same either way.
	RTH bool
}

const yahooBase = "https://query1.finance.yahoo.com"

func (y Yahoo) base() string {
	if y.Base != "" {
		return y.Base
	}
	return yahooBase
}

// An Instrument is a market Yahoo serves, as Candle names it: "CME:NQ" for a
// futures contract (the front month, rolled as Yahoo rolls it), "US:AAPL"
// for a stock or ETF listed in New York.
type Instrument struct {
	ID     string  // "CME:NQ", "US:AAPL"
	Code   string  // what it reads as: "NQ", "AAPL"
	Name   string  // "纳指 E-mini", or a company's name
	Ticker string  // Yahoo's: "NQ=F", "AAPL"
	Tick   float64 // the smallest price step; 0 unknown
	Future bool
	Delay  time.Duration // how late Yahoo shows it
	Alias  string        // other names it is found by: "英伟达"
	Pinyin string        // the pinyin of its Chinese names, a syllable a word: "ying wei da"
	Rank   int           // its place among the most looked up (Popular), 1 first, 0 none
}

// The CME contracts Candle offers. Yahoo has the micro contracts' own trades
// too: MNQ is not NQ's prices under another name.
var Futures = []Instrument{
	future("NQ", "纳指 E-mini", "na zhi", 0.25),
	future("MNQ", "微型纳指", "wei xing na zhi", 0.25),
	future("ES", "标普 E-mini", "biao pu", 0.25),
	future("MES", "微型标普", "wei xing biao pu", 0.25),
	future("YM", "道指 E-mini", "dao zhi", 1),
	future("MYM", "微型道指", "wei xing dao zhi", 1),
	future("RTY", "罗素 2000 E-mini", "luo su", 0.1),
	future("M2K", "微型罗素 2000", "wei xing luo su", 0.1),
	future("GC", "黄金", "huang jin", 0.1),
	future("CL", "原油 WTI", "yuan you", 0.01),
}

func future(code, name, pinyin string, tick float64) Instrument {
	return Instrument{ID: "CME:" + code, Code: code, Name: name, Pinyin: pinyin, Ticker: code + "=F", Tick: tick, Future: true, Delay: 10 * time.Minute}
}

// IsYahoo is whether Candle's name for a market is one of Yahoo's.
func IsYahoo(id string) bool { return strings.HasPrefix(id, "CME:") || strings.HasPrefix(id, "US:") }

// InstrumentOf is the market Candle names id.
func InstrumentOf(id string) (Instrument, bool) {
	if code, ok := strings.CutPrefix(id, "US:"); ok && code != "" {
		// A cent, until Symbols reads the stock's own (a hundredth of one under a dollar).
		return Instrument{ID: id, Code: code, Ticker: code, Tick: 0.01}, true
	}
	if i := slices.IndexFunc(Futures, func(f Instrument) bool { return f.ID == id }); i >= 0 {
		return Futures[i], true
	}
	return Instrument{}, false
}

// A yahooInterval is a bar size of Yahoo's: its name, how far back it goes
// and how much one request may span.
type yahooInterval struct {
	name string
	ms   int64         // of Yahoo's bars
	back time.Duration // 0: all the history there is
	span time.Duration // 0: no limit
}

const day = 24 * time.Hour

// Yahoo keeps 1-minute bars for 30 days, 7 to a request, 5 to 30 minutes for
// 60 days and hours for 730; the limits are checked against its clock, so a
// day short of them.
var yahooIntervals = map[string]yahooInterval{
	"1m":  {"1m", 60_000, 29 * day, 7 * day},
	"5m":  {"5m", 300_000, 59 * day, 0},
	"15m": {"15m", 900_000, 59 * day, 0},
	"30m": {"30m", 1_800_000, 59 * day, 0},
	"1h":  {"1h", 3_600_000, 729 * day, 0},
	"1d":  {"1d", 86_400_000, 0, 0},
	"1wk": {"1wk", 604_800_000, 0, 0},
}

// A barPlan is how Candle's bars of a size are made from Yahoo's: from its
// bars of src, group of them to one (1: as they are) counting from start,
// when the session holding a time began, and of them only those keep
// takes (nil: all).
type barPlan struct {
	src   yahooInterval
	group int
	start func(int64) int64
	keep  func(int64) bool
}

// size is the plan's bars' length, in ms.
func (p barPlan) size() int64 { return p.src.ms * int64(p.group) }

// The regular trading hours of CME's contracts, New York time: 9:30 to 16:15.
const (
	rthOpen  = (9*60 + 30) * 60_000
	rthClose = (16*60 + 15) * 60_000
)

// sinceMidnight is the time of day in New York at t, Unix ms, in ms.
func sinceMidnight(t int64) int64 {
	n := time.UnixMilli(t).In(newYork)
	return int64(n.Hour()*3600+n.Minute()*60+n.Second())*1000 + int64(n.Nanosecond()/1e6)
}

// InRTH is whether a bar opening at t (Unix ms) is in a future's regular
// trading hours.
func InRTH(t int64) bool {
	d := sinceMidnight(t)
	return d >= rthOpen && d < rthClose
}

// rthStart is when the regular session holding t began: 9:30 New York time
// that day.
func rthStart(t int64) int64 {
	n := time.UnixMilli(t).In(newYork)
	return time.Date(n.Year(), n.Month(), n.Day(), 9, 30, 0, 0, newYork).UnixMilli()
}

// plan is how y makes in's bars of interval: as Yahoo has them, 4 hours of
// four from the session's start, and for a future's regular hours bars
// opening from 9:30 on, from Yahoo's that divide both (its hours open on
// the hour: an hour from 9:30 is two half hours, which go back 60 days).
func (y Yahoo) plan(in Instrument, interval string) (barPlan, bool) {
	p := barPlan{group: 1, start: in.SessionStart}
	switch interval {
	case "1m", "5m", "15m", "1h":
		p.src = yahooIntervals[interval]
	case "4h":
		p.src, p.group = yahooIntervals["1h"], 4
	case "1d":
		p.src = yahooIntervals["1d"]
	case "1w":
		p.src = yahooIntervals["1wk"]
	default:
		return barPlan{}, false
	}
	if !y.RTH || !in.Future || p.src.ms >= 86_400_000 {
		return p, true
	}
	size := p.size()
	for _, name := range []string{"1h", "30m", "15m", "5m", "1m"} {
		if s := yahooIntervals[name]; size%s.ms == 0 && rthOpen%s.ms == 0 {
			p.src, p.group = s, int(size/s.ms)
			break
		}
	}
	p.start, p.keep = rthStart, InRTH
	return p, true
}

// newYork tells the sessions apart: a stock's begins at 9:30 there, a CME
// contract's at 18:00 the day before.
var newYork = func() *time.Location {
	l, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return l
}()

// SessionStart is when the session holding t (Unix ms) began: a stock's at
// 9:30 New York time that day, a future's at 18:00 the evening before.
func (in Instrument) SessionStart(t int64) int64 {
	n := time.UnixMilli(t).In(newYork)
	if !in.Future {
		return time.Date(n.Year(), n.Month(), n.Day(), 9, 30, 0, 0, newYork).UnixMilli()
	}
	s := time.Date(n.Year(), n.Month(), n.Day(), 18, 0, 0, 0, newYork)
	if n.Before(s) {
		s = time.Date(n.Year(), n.Month(), n.Day()-1, 18, 0, 0, 0, newYork)
	}
	return s.UnixMilli()
}

// Klines returns up to limit bars opening at or before end, oldest first, as
// Binance.Klines does: end is Unix ms, 0 for now, and fewer than limit means
// the history Yahoo keeps begins there. It asks back in widening steps until
// it has limit bars or reaches that beginning.
func (y Yahoo) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	in, ok := InstrumentOf(symbol)
	if !ok {
		return nil, fmt.Errorf("yahoo: no market %q", symbol)
	}
	p, ok := y.plan(in, interval)
	if !ok {
		return nil, fmt.Errorf("yahoo: no %s bars", interval)
	}
	yi := p.src
	now := time.Now()
	to := now.Add(time.Minute)
	if end > 0 {
		to = time.UnixMilli(end + 1)
	}
	floor := time.Unix(0, 0) // nothing older than this to ask for
	if yi.back > 0 {
		floor = now.Add(-yi.back)
	}
	if !to.After(floor) {
		return nil, nil
	}
	want := limit * p.group
	span := time.Duration(want) * time.Duration(yi.ms) * time.Millisecond * 2
	var base []Bar // Yahoo's bars from from onwards, oldest first
	var out []Bar
	for {
		if yi.span > 0 {
			span = min(span, yi.span)
		}
		from := to.Add(-span)
		atFloor := !from.After(floor)
		if atFloor {
			from = floor
		}
		got, err := y.chart(ctx, in, yi, from.Unix(), to.Unix())
		if err != nil {
			return nil, err
		}
		// Yahoo stretches the range to whole days for daily bars: keep to it,
		// or pages overlap.
		got = slices.DeleteFunc(got, func(b Bar) bool {
			return b.T < from.UnixMilli() || b.T >= to.UnixMilli() || p.keep != nil && !p.keep(b.T)
		})
		base = append(got, base...)
		out = base
		if p.group > 1 {
			out = groupBars(base, p.group, yi.ms, p.start, from.UnixMilli())
		}
		if len(out) >= limit || atFloor {
			break
		}
		// Short of limit: what is left goes back to where Yahoo's history
		// begins (a week at a time for minutes), in one request rather than
		// widening steps: a stock listed last year has its days in two.
		to, span = from, from.Sub(floor)
	}
	return out[max(len(out)-limit, 0):], nil
}

// groupBars merges bars of ms each into bars of n of them, starting from the
// session's first: four hours from 18:00 for a future, from 9:30 for a stock
// or a future's regular hours.
// Groups beginning before from are missing bars and left out.
func groupBars(bars []Bar, n int, ms int64, sessionStart func(int64) int64, from int64) []Bar {
	size := ms * int64(n)
	var out []Bar
	for _, b := range bars {
		s := sessionStart(b.T)
		key := s + (b.T-s)/size*size
		if key < from {
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

// chart asks for in's bars of yi opening in [from, to), Unix seconds.
//
// Yahoo gives the bar holding period1 no volume, whatever it traded, so it
// is asked from a second earlier: that bar opens before from and is left out.
func (y Yahoo) chart(ctx context.Context, in Instrument, yi yahooInterval, from, to int64) ([]Bar, error) {
	q := url.Values{"interval": {yi.name}, "period1": {strconv.FormatInt(from-1, 10)}, "period2": {strconv.FormatInt(to, 10)}}
	u := y.base() + "/v8/finance/chart/" + url.PathEscape(in.Ticker) + "?" + q.Encode()
	body, err := get(ctx, y.HTTP, u)
	var ue *url.Error
	if errors.As(err, &ue) && ctx.Err() == nil { // the connection dropped, as through a proxy it now and then does
		body, err = get(ctx, y.HTTP, u)
	}
	if err != nil {
		// Asked for days before the market's first, Yahoo answers 400.
		if strings.Contains(err.Error(), "Data doesn't exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("yahoo %s: %w", in.Code, err)
	}
	c, err := parseChart(body, yi.name)
	if err != nil {
		return nil, fmt.Errorf("yahoo %s: %w", in.Code, err)
	}
	return slices.DeleteFunc(c.Bars, func(b Bar) bool { return b.T < from*1000 }), nil
}

// Stream calls fn with the newest bars as Yahoo has them, every few seconds,
// until ctx ends or a request fails. Yahoo has no stream to follow; this
// asks for the last few bars again and again, from a few days back the
// first time, so that a closed market has its last bar too.
func (y Yahoo) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	in, ok := InstrumentOf(symbol)
	p, ok2 := y.plan(in, interval)
	if !ok || !ok2 {
		return fmt.Errorf("yahoo: no %s bars of %q", interval, symbol)
	}
	yi := p.src
	every := y.Every
	if every == 0 {
		every = 5 * time.Second
	}
	size := p.size()
	var last int64 // the newest bar's open, Unix ms
	for {
		now := time.Now()
		from := now.Add(-4 * day).UnixMilli()
		if yi.ms >= 86_400_000 {
			from = now.Add(-30 * day).UnixMilli()
		}
		if last > 0 {
			from = last - size // the bar before too, in case the newest is the first of a new one
		}
		got, err := y.chart(ctx, in, yi, from/1000, now.Add(time.Minute).Unix())
		if err != nil {
			return fmt.Errorf("yahoo stream: %w", err)
		}
		if p.keep != nil {
			got = slices.DeleteFunc(got, func(b Bar) bool { return !p.keep(b.T) })
		}
		if p.group > 1 {
			got = groupBars(got, p.group, yi.ms, p.start, from)
		}
		if n := len(got); n > 0 {
			got = got[max(n-2, 0):]
			last = got[len(got)-1].T
			for _, b := range got {
				fn(b, false, now.UnixMilli())
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

// Symbols looks markets up: a future from the table, a stock from its chart's
// heading, for its name and how many decimals its prices take.
func (y Yahoo) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	var out []Symbol
	for _, n := range names {
		in, ok := InstrumentOf(n)
		if !ok {
			return nil, fmt.Errorf("yahoo: no market %q", n)
		}
		s := Symbol{Name: in.ID, Base: in.Code, Tick: in.Tick, Desc: in.Name}
		if !in.Future {
			body, err := get(ctx, y.HTTP, y.base()+"/v8/finance/chart/"+url.PathEscape(in.Ticker)+"?interval=1d&range=1d")
			if err != nil {
				return nil, fmt.Errorf("yahoo %s: %w", in.Code, err)
			}
			m, err := parseMeta(body)
			if err != nil {
				return nil, fmt.Errorf("yahoo %s: %w", in.Code, err)
			}
			s.Desc = cmp.Or(m.Long, m.Short)
			s.Tick = math.Pow(10, -float64(cmp.Or(m.PriceHint, 2)))
		}
		out = append(out, s)
	}
	return out, nil
}

type yahooMeta struct {
	Short     string `json:"shortName"`
	Long      string `json:"longName"`
	PriceHint int    `json:"priceHint"`
}

func parseMeta(body []byte) (yahooMeta, error) {
	var r struct {
		Chart struct {
			Result []struct {
				Meta yahooMeta `json:"meta"`
			} `json:"result"`
			Error *struct{ Code, Description string } `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return yahooMeta{}, err
	}
	if e := r.Chart.Error; e != nil {
		return yahooMeta{}, fmt.Errorf("%s: %s", e.Code, e.Description)
	}
	if len(r.Chart.Result) != 1 {
		return yahooMeta{}, fmt.Errorf("unexpected chart shape: %.200s", body)
	}
	return r.Chart.Result[0].Meta, nil
}

// Quotes is the last price and the last session's close of each market, in
// one request a score of them.
func (y Yahoo) Quotes(ctx context.Context, ids []string) ([]Ticker, error) {
	var out []Ticker
	for i := 0; i < len(ids); i += 20 {
		chunk := ids[i:min(i+20, len(ids))]
		var tickers []string
		byTicker := map[string]string{}
		for _, id := range chunk {
			if in, ok := InstrumentOf(id); ok {
				tickers = append(tickers, in.Ticker)
				byTicker[in.Ticker] = id
			}
		}
		if len(tickers) == 0 {
			continue
		}
		q := url.Values{"symbols": {strings.Join(tickers, ",")}, "range": {"1d"}, "interval": {"1d"}}
		body, err := get(ctx, y.HTTP, y.base()+"/v8/finance/spark?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("yahoo quotes: %w", err)
		}
		got, err := parseSpark(body, byTicker)
		if err != nil {
			return nil, fmt.Errorf("yahoo quotes: %w", err)
		}
		out = append(out, got...)
	}
	return out, nil
}

// parseSpark reads {"NQ=F": {"close": [...], "chartPreviousClose": p}, ...}:
// the last close is the price now, the move is from p.
func parseSpark(body []byte, byTicker map[string]string) ([]Ticker, error) {
	var r map[string]struct {
		Close     []*float64 `json:"close"`
		PrevClose *float64   `json:"chartPreviousClose"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	var out []Ticker
	for tk, s := range r {
		id, ok := byTicker[tk]
		if !ok {
			continue
		}
		var last float64
		for _, c := range s.Close {
			if c != nil {
				last = *c
			}
		}
		if last == 0 {
			continue
		}
		t := Ticker{Symbol: id, Last: last, Open: last}
		if s.PrevClose != nil && *s.PrevClose > 0 {
			t.Open = *s.PrevClose
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Ticker) int { return cmp.Compare(a.Symbol, b.Symbol) })
	return out, nil
}

// Watch calls fn with each market's quote every ten seconds or so, until ctx
// ends or a request fails.
func (y Yahoo) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	every := y.Every * 2
	if every == 0 {
		every = 10 * time.Second
	}
	for {
		got, err := y.Quotes(ctx, symbols)
		if err != nil {
			return err
		}
		for _, t := range got {
			fn(t)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

// usExchanges are Yahoo's codes for the exchanges in New York: Nasdaq's
// markets, the NYSE, NYSE American and Arca, and Cboe's BZX.
var usExchanges = map[string]bool{"NMS": true, "NGM": true, "NCM": true, "NYQ": true, "ASE": true, "PCX": true, "BTS": true}

// Search finds US stocks and ETFs by symbol or name.
func (y Yahoo) Search(ctx context.Context, query string) ([]Instrument, error) {
	q := url.Values{"q": {query}, "quotesCount": {"12"}, "newsCount": {"0"}, "listsCount": {"0"}}
	base := y.base()
	if base == yahooBase {
		base = "https://query2.finance.yahoo.com"
	}
	body, err := get(ctx, y.HTTP, base+"/v1/finance/search?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("yahoo search: %w", err)
	}
	return parseSearch(body)
}

func parseSearch(body []byte) ([]Instrument, error) {
	var r struct {
		Quotes []struct {
			Symbol   string `json:"symbol"`
			Type     string `json:"quoteType"`
			Exchange string `json:"exchange"`
			Short    string `json:"shortname"`
			Long     string `json:"longname"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	var out []Instrument
	for _, q := range r.Quotes {
		if (q.Type != "EQUITY" && q.Type != "ETF") || !usExchanges[q.Exchange] || q.Symbol == "" || strings.ContainsAny(q.Symbol, ".=^") {
			continue
		}
		out = append(out, Instrument{ID: "US:" + q.Symbol, Code: q.Symbol, Ticker: q.Symbol, Tick: 0.01, Name: strings.TrimSpace(cmp.Or(q.Long, q.Short))})
	}
	return out, nil
}

// Chart returns a ticker's bars between from and to (Unix seconds) as Yahoo
// names them ("^GSPC", "60m"), for cmd/probe's look at what it answers.
func (y Yahoo) Chart(ctx context.Context, ticker, interval string, from, to int64) (Chart, error) {
	q := url.Values{"interval": {interval}, "period1": {strconv.FormatInt(from, 10)}, "period2": {strconv.FormatInt(to, 10)}, "events": {"split"}}
	body, err := get(ctx, y.HTTP, y.base()+"/v8/finance/chart/"+url.PathEscape(ticker)+"?"+q.Encode())
	if err != nil {
		return Chart{}, fmt.Errorf("yahoo %s: %w", ticker, err)
	}
	c, err := parseChart(body, interval)
	if err != nil {
		return Chart{}, fmt.Errorf("yahoo %s: %w", ticker, err)
	}
	return c, nil
}

// Chart is one symbol's bars and what is needed to label them.
type Chart struct {
	Bars     []Bar
	Timezone string // the exchange's IANA zone, e.g. America/New_York
	Skipped  int    // rows without prices or outside the regular session
	Splits   int    // splits inside the range; prices are already adjusted for them
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Timezone    string `json:"exchangeTimezoneName"`
				Granularity string `json:"dataGranularity"`
				// Intraday only: one list of {start, end} per day.
				Sessions json.RawMessage `json:"tradingPeriods"`
			} `json:"meta"`
			Timestamp []int64 `json:"timestamp"`
			Events    struct {
				Splits map[string]json.RawMessage `json:"splits"`
			} `json:"events"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// sameSize says 60m and 1h are the same bars.
func sameSize(a, b string) bool {
	norm := func(s string) string {
		if s == "60m" {
			return "1h"
		}
		return s
	}
	return norm(a) == norm(b)
}

// granularity is a bar size of Yahoo's in seconds.
func granularity(name string) int64 {
	switch name {
	case "1m":
		return 60
	case "5m":
		return 300
	case "15m":
		return 900
	case "30m":
		return 1800
	case "1h", "60m":
		return 3600
	case "1d":
		return 86400
	case "1wk":
		return 7 * 86400
	}
	return 0
}

func parseChart(body []byte, interval string) (Chart, error) {
	var r chartResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return Chart{}, err
	}
	if e := r.Chart.Error; e != nil {
		return Chart{}, fmt.Errorf("%s: %s", e.Code, e.Description)
	}
	if len(r.Chart.Result) != 1 || len(r.Chart.Result[0].Indicators.Quote) > 1 {
		return Chart{}, fmt.Errorf("unexpected chart shape: %.200s", body)
	}
	res := r.Chart.Result[0]
	if len(res.Timestamp) == 0 { // nothing traded in the range
		return Chart{Timezone: res.Meta.Timezone}, nil
	}
	// Yahoo quietly answers with coarser bars than asked for (range=max turns 1d
	// into 3mo); drawing those as the asked size would be wrong.
	if !sameSize(res.Meta.Granularity, interval) {
		return Chart{}, fmt.Errorf("asked for %s bars, got %s", interval, res.Meta.Granularity)
	}
	if len(res.Indicators.Quote) != 1 {
		return Chart{}, fmt.Errorf("unexpected chart shape: %.200s", body)
	}
	q := res.Indicators.Quote[0]
	sessions := regularSessions(res.Meta.Sessions)
	size := granularity(interval)
	c := Chart{Timezone: res.Meta.Timezone, Splits: len(res.Events.Splits), Bars: make([]Bar, 0, len(res.Timestamp))}
	j := 0
	for i, ts := range res.Timestamp {
		o, h, l, cl := at(q.Open, i), at(q.High, i), at(q.Low, i), at(q.Close, i)
		if o == nil || h == nil || l == nil || cl == nil {
			c.Skipped++
			continue
		}
		b := Bar{T: ts * 1000, O: *o, H: *h, L: *l, C: *cl}
		if v := at(q.Volume, i); v != nil {
			b.V = *v
		}
		// After the close Yahoo appends the closing quote as one more intraday
		// bar opening right at the session's end, with no volume. (A bar with
		// trades outside the sessions it lists is kept: a future's Sunday
		// evening may not be listed.)
		if len(sessions) > 0 {
			for j < len(sessions) && sessions[j][1] <= ts {
				j++
			}
			if (j == len(sessions) || ts < sessions[j][0]) && b.V == 0 {
				c.Skipped++
				continue
			}
		}
		c.Bars = append(c.Bars, b)
	}
	// While it trades, the last row is the latest quote, at its own time
	// (12:05:23 among 1-minute bars, or Thursday among weeks beginning on
	// Monday): it belongs to the bar it falls in. Its volume is not that
	// bar's: none on a minute, the day's on a week.
	if n := len(c.Bars); n >= 2 && size > 0 {
		last, prev := c.Bars[n-1], &c.Bars[n-2]
		// A day's or a week's bars open at one time of day (a day after
		// the clocks go forward, an hour less than a day after the last).
		quote := size < 86400 || clock(last.T) != clock(prev.T)
		if last.T-prev.T < size*1000 && quote {
			prev.H, prev.L, prev.C = max(prev.H, last.H), min(prev.L, last.L), last.C
			c.Bars, c.Skipped = c.Bars[:n-1], c.Skipped+1
		}
	}
	// One left that is no bar's opening, with nothing before it to fold into.
	if n := len(c.Bars); n > 0 && size < 86400 && c.Bars[n-1].T%60_000 != 0 {
		c.Bars, c.Skipped = c.Bars[:n-1], c.Skipped+1
	}
	return c, nil
}

// clock is the time of day in New York at t, Unix ms, in seconds.
func clock(t int64) int {
	n := time.UnixMilli(t).In(newYork)
	return n.Hour()*3600 + n.Minute()*60 + n.Second()
}

// regularSessions flattens tradingPeriods into [start, end) pairs, oldest first,
// or returns nil when the chart has none (daily and longer).
func regularSessions(raw json.RawMessage) [][2]int64 {
	var days [][]struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}
	if json.Unmarshal(raw, &days) != nil {
		return nil
	}
	var s [][2]int64
	for _, d := range days {
		for _, p := range d {
			s = append(s, [2]int64{p.Start, p.End})
		}
	}
	slices.SortFunc(s, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	return s
}

func at(s []*float64, i int) *float64 {
	if i < len(s) {
		return s[i]
	}
	return nil
}
