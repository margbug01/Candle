package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func chartBody(gran string, ts []int64, close []float64, vol []float64, extra string) []byte {
	b, _ := json.Marshal(map[string]any{"chart": map[string]any{"result": []any{map[string]any{
		"meta":       json.RawMessage(`{"exchangeTimezoneName":"America/New_York","dataGranularity":"` + gran + `"` + extra + `}`),
		"timestamp":  ts,
		"indicators": map[string]any{"quote": []any{map[string]any{"open": close, "high": close, "low": close, "close": close, "volume": vol}}},
	}}, "error": nil}})
	return b
}

// While a market trades, Yahoo ends the list with the latest quote at its own
// time: it goes into the bar it falls in, never stands as a bar of its own.
func TestParseChartFoldsTheQuote(t *testing.T) {
	// Minutes, the quote 58 seconds into the last one, higher.
	c, err := parseChart(chartBody("1m", []int64{600, 660, 718}, []float64{1, 2, 3}, []float64{4, 5, 0}, ""), "1m")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bars) != 2 || c.Bars[1] != (Bar{T: 660_000, O: 2, H: 3, L: 2, C: 3, V: 5}) {
		t.Fatalf("minutes: %+v", c.Bars)
	}
	// Ten minutes late, the quote can be the only row: no bar opens there.
	if c, _ := parseChart(chartBody("1m", []int64{718}, []float64{3}, []float64{0}, ""), "1m"); len(c.Bars) != 0 {
		t.Fatalf("a lone quote made a bar: %+v", c.Bars)
	}
	// Weeks begin on Monday at midnight New York time; Thursday's quote folds in,
	// its volume (the day's) left out.
	mon := time.Date(2026, 10, 26, 0, 0, 0, 0, newYork).Unix()
	thu := time.Date(2026, 10, 29, 1, 23, 19, 0, newYork).Unix()
	c, _ = parseChart(chartBody("1wk", []int64{mon - 7*86400, mon, thu}, []float64{10, 11, 9}, []float64{100, 200, 50}, ""), "1wk")
	if len(c.Bars) != 2 || c.Bars[1].C != 9 || c.Bars[1].L != 9 || c.Bars[1].V != 200 {
		t.Fatalf("weeks: %+v", c.Bars)
	}
	// The week the clocks go forward opens an hour less than a week after the
	// last: a new bar, not a quote.
	mar2 := time.Date(2026, 3, 2, 0, 0, 0, 0, newYork).Unix()
	mar9 := time.Date(2026, 3, 9, 0, 0, 0, 0, newYork).Unix()
	c, _ = parseChart(chartBody("1wk", []int64{mar2 - 7*86400, mar2, mar9}, []float64{1, 2, 3}, []float64{1, 1, 1}, ""), "1wk")
	if len(c.Bars) != 3 {
		t.Fatalf("weeks across the change: %+v", c.Bars)
	}
	// Hours asked as 1h come back as 60m.
	if _, err := parseChart(chartBody("60m", []int64{3600}, []float64{1}, []float64{1}, ""), "1h"); err != nil {
		t.Fatal(err)
	}
	// Nothing traded in the range: no bars, no error.
	if c, err := parseChart([]byte(`{"chart":{"result":[{"meta":{"dataGranularity":"1d"},"indicators":{"quote":[{}]}}],"error":null}}`), "1h"); err != nil || len(c.Bars) != 0 {
		t.Fatalf("empty: %+v, %v", c, err)
	}
}

// A stock's sessions begin at 9:30 New York time, a future's at 18:00 the
// evening before, summer and winter.
func TestSessionStart(t *testing.T) {
	nq, _ := InstrumentOf("CME:NQ")
	aapl, _ := InstrumentOf("US:AAPL")
	ny := func(y int, m time.Month, d, h, min int) int64 {
		return time.Date(y, m, d, h, min, 0, 0, newYork).UnixMilli()
	}
	for _, c := range []struct {
		in       Instrument
		at, want int64
	}{
		{nq, ny(2026, 10, 29, 1, 30), ny(2026, 10, 28, 18, 0)},
		{nq, ny(2026, 10, 28, 18, 0), ny(2026, 10, 28, 18, 0)},
		{nq, ny(2026, 10, 28, 16, 59), ny(2026, 10, 27, 18, 0)},
		{nq, ny(2026, 1, 15, 3, 0), ny(2026, 1, 14, 18, 0)},
		{aapl, ny(2026, 10, 28, 15, 55), ny(2026, 10, 28, 9, 30)},
		{aapl, ny(2026, 1, 15, 10, 30), ny(2026, 1, 15, 9, 30)},
	} {
		if got := c.in.SessionStart(c.at); got != c.want {
			t.Errorf("%s at %s: session from %s, want %s", c.in.Code, time.UnixMilli(c.at).In(newYork), time.UnixMilli(got).In(newYork), time.UnixMilli(c.want).In(newYork))
		}
	}
}

// Four hours are four hourly bars from the session's beginning: 18:00, 22:00,
// 2:00… for a future, 9:30 and 13:30 for a stock. A group missing its first
// bars is left out.
func TestGroupBars(t *testing.T) {
	nq, _ := InstrumentOf("CME:NQ")
	aapl, _ := InstrumentOf("US:AAPL")
	hourly := func(from time.Time, n int, skip func(time.Time) bool) (out []Bar) {
		for i := range n {
			at := from.Add(time.Duration(i) * time.Hour)
			if !skip(at) {
				out = append(out, Bar{T: at.UnixMilli(), O: float64(i), H: float64(i) + 0.5, L: float64(i) - 0.5, C: float64(i), V: 1})
			}
		}
		return out
	}
	// A future from 20:00 on Tuesday (in its first group) to 17:00 on Wednesday, no 17:00 hour.
	bars := hourly(time.Date(2026, 10, 27, 20, 0, 0, 0, newYork), 21, func(t time.Time) bool { return t.Hour() == 17 })
	got := groupBars(bars, 4, 3_600_000, nq.SessionStart, bars[0].T)
	var opens []string
	for _, b := range got {
		opens = append(opens, time.UnixMilli(b.T).In(newYork).Format("15:04"))
	}
	if want := []string{"22:00", "02:00", "06:00", "10:00", "14:00"}; !slices.Equal(opens, want) {
		t.Fatalf("future: groups open at %v, want %v", opens, want)
	}
	if g := got[1]; g.O != 6 || g.C != 9 || g.H != 9.5 || g.L != 5.5 || g.V != 4 {
		t.Fatalf("02:00 group: %+v", g)
	}
	if last := got[len(got)-1]; last.V != 3 { // 14:00, 15:00, 16:00
		t.Fatalf("14:00 group has %v bars", last.V)
	}
	// A stock's hourly bars open at :30; the day's last is half an hour.
	day := hourly(time.Date(2026, 10, 28, 9, 30, 0, 0, newYork), 7, func(time.Time) bool { return false })
	got = groupBars(day, 4, 3_600_000, aapl.SessionStart, day[0].T)
	if len(got) != 2 || time.UnixMilli(got[1].T).In(newYork).Format("15:04") != "13:30" || got[0].V != 4 || got[1].V != 3 {
		t.Fatalf("stock: %+v", got)
	}
}

// fakeYahoo serves hourly, minute and daily bars of every market, as Yahoo
// does: from first on, not in the hour before 18:00 New York time, and only
// as far back as Yahoo keeps them. It records what it was asked.
type fakeYahoo struct {
	first time.Time
	mu    sync.Mutex
	asked []string
}

func (f *fakeYahoo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p1, _ := strconv.ParseInt(q.Get("period1"), 10, 64)
	p2, _ := strconv.ParseInt(q.Get("period2"), 10, 64)
	iv := q.Get("interval")
	f.mu.Lock()
	f.asked = append(f.asked, fmt.Sprintf("%s %d %d", iv, p1, p2))
	f.mu.Unlock()
	size := granularity(iv)
	limits := map[string]time.Duration{"1m": 30 * day, "5m": 60 * day, "15m": 60 * day, "30m": 60 * day, "1h": 730 * day}
	if lim, ok := limits[iv]; ok && time.Unix(p1, 0).Before(time.Now().Add(-lim)) {
		w.WriteHeader(422)
		fmt.Fprintf(w, `{"chart":{"result":null,"error":{"code":"Unprocessable Entity","description":"%s data not available"}}}`, iv)
		return
	}
	if iv == "1m" && p2-p1 > int64(8*day/time.Second) {
		w.WriteHeader(422)
		fmt.Fprint(w, `{"chart":{"result":null,"error":{"code":"Unprocessable Entity","description":"Only 8 days worth of 1m granularity data are allowed to be fetched per request."}}}`)
		return
	}
	if p2 <= f.first.Unix() {
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"chart":{"result":null,"error":{"code":"Bad Request","description":"Data doesn't exist for startDate = %d, endDate = %d"}}}`, p1, p2)
		return
	}
	var ts []int64
	var cl, vol []float64
	if size >= 86400 { // Yahoo stretches daily ranges to whole days
		p1, p2 = p1/86400*86400, (p2/86400+1)*86400
	}
	start := max(p1, f.first.Unix())
	stock := !strings.HasSuffix(r.URL.Path, "%3DF") && !strings.HasSuffix(r.URL.Path, "=F")
	for t := start / size * size; t < p2 && t <= time.Now().Unix(); t += size {
		ny := time.Unix(t, 0).In(newYork)
		if size < 86400 && ny.Hour() == 17 {
			continue
		}
		// A stock trades 9:30 to 16:00 on weekdays.
		if stock && size < 86400 && (ny.Weekday() == time.Saturday || ny.Weekday() == time.Sunday || ny.Hour()*60+ny.Minute() < 570 || ny.Hour() >= 16) {
			continue
		}
		ts = append(ts, t)
		cl = append(cl, float64(t/size%1000)/4)
		vol = append(vol, 1)
		if t <= p1 && p1 < t+size { // Yahoo gives the bar holding period1 no volume
			vol[len(vol)-1] = 0
		}
	}
	w.Write(chartBody(map[string]string{"1h": "1h"}[iv]+map[bool]string{true: iv}[iv != "1h"], ts, cl, vol, ""))
}

// Klines pages back as Binance's do: limit bars ending at end, fewer only
// where the history begins, never asking Yahoo for more than it keeps.
func TestYahooKlinesPages(t *testing.T) {
	f := &fakeYahoo{first: time.Now().Add(-400 * day)}
	srv := httptest.NewServer(f)
	defer srv.Close()
	y := Yahoo{HTTP: srv.Client(), Base: srv.URL}
	ctx := context.Background()
	for _, iv := range []string{"1h", "4h", "1m"} {
		first, err := y.Klines(ctx, "CME:MNQ", iv, 0, 1000)
		if err != nil {
			t.Fatal(iv, err)
		}
		if len(first) != 1000 {
			t.Fatalf("%s: %d bars on the first page", iv, len(first))
		}
		older, err := y.Klines(ctx, "CME:MNQ", iv, first[0].T-1, 1000)
		if err != nil {
			t.Fatal(iv, err)
		}
		if len(older) != 1000 || older[len(older)-1].T >= first[0].T {
			t.Fatalf("%s: older page of %d ends at %d, the first begins at %d", iv, len(older), older[len(older)-1].T, first[0].T)
		}
		all := append(older, first...)
		for i := 1; i < len(all); i++ {
			if all[i].T <= all[i-1].T {
				t.Fatalf("%s: bar %d opens at %d after %d", iv, i, all[i].T, all[i-1].T)
			}
		}
		for _, b := range all {
			if b.V == 0 {
				t.Fatalf("%s: the bar at %s lost its volume", iv, time.UnixMilli(b.T))
			}
		}
		if iv == "4h" {
			nq, _ := InstrumentOf("CME:MNQ")
			for _, b := range all {
				// Four hours are counted from 18:00, so a session the clocks
				// change in has its groups an hour off after the change.
				at, from := time.UnixMilli(b.T).In(newYork), time.UnixMilli(nq.SessionStart(b.T)).In(newYork)
				_, o1 := at.Zone()
				_, o2 := from.Zone()
				if h := at.Hour(); (h+6)%4 != 0 && o1 == o2 {
					t.Fatalf("a 4-hour bar opens at %s New York time", at)
				}
			}
			// The group just before the first page's is whole: four hours.
			if g := older[len(older)-1]; g.V != 4 && time.UnixMilli(g.T).In(newYork).Hour() != 14 {
				t.Fatalf("the older page's last group has %v hours", g.V)
			}
		}
	}
	// Daily bars: 400 of them in all, so the second page is short and says so.
	days, err := y.Klines(ctx, "CME:MNQ", "1d", 0, 300)
	if err != nil || len(days) != 300 {
		t.Fatalf("days: %d, %v", len(days), err)
	}
	f.asked = nil
	rest, err := y.Klines(ctx, "CME:MNQ", "1d", days[0].T-1, 300)
	// A page short of 300 asks for the rest at once, not in widening steps.
	if len(f.asked) != 2 {
		t.Fatalf("the rest of the days in %d requests: %v", len(f.asked), f.asked)
	}
	if err != nil || len(rest) == 0 || len(rest) >= 300 || rest[len(rest)-1].T >= days[0].T {
		t.Fatalf("the rest of the days: %d, %v", len(rest), err)
	}
	// Minutes of a stock, sparse: a week to a request at most, never past
	// the 30 days kept.
	f.asked = nil
	var mins []Bar
	end := int64(0)
	for {
		page, err := y.Klines(ctx, "US:AAPL", "1m", end, 1000)
		if err != nil {
			t.Fatal(err)
		}
		mins = append(page, mins...)
		if len(page) < 1000 {
			break
		}
		end = page[0].T - 1
	}
	if span := time.Since(time.UnixMilli(mins[0].T)); span < 26*day || span > 30*day {
		t.Fatalf("minutes reach back %v", span)
	}
	// Three thousand minutes of a stock take more than a week of days.
	if many, err := y.Klines(ctx, "US:AAPL", "1m", 0, 3000); err != nil || len(many) != 3000 {
		t.Fatalf("3000 minutes: %d, %v", len(many), err)
	}
	for _, a := range f.asked {
		var iv string
		var p1, p2 int64
		fmt.Sscan(a, &iv, &p1, &p2)
		if p2-p1 > int64(7*day/time.Second)+60 {
			t.Fatalf("asked for %s", a)
		}
	}
	if _, err := y.Klines(ctx, "CME:XX", "1h", 0, 10); err == nil {
		t.Fatal("an unknown market's bars")
	}
}

// Stream asks for a few days first, then from the bar before the newest, and
// hands on the last two.
func TestYahooStream(t *testing.T) {
	f := &fakeYahoo{first: time.Now().Add(-10 * day)}
	srv := httptest.NewServer(f)
	defer srv.Close()
	y := Yahoo{HTTP: srv.Client(), Base: srv.URL, Every: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	var got []Bar
	y.Stream(ctx, "US:AAPL", "1h", func(b Bar, _ bool, _ int64) {
		got = append(got, b)
		if len(got) == 4 {
			cancel()
		}
	})
	if len(got) != 4 || got[1].T != got[3].T || got[0].T != got[2].T || got[1].T-got[0].T < 3_600_000 || got[2].V == 0 {
		t.Fatalf("streamed %+v", got)
	}
	var p1 [2]int64
	for i, a := range f.asked[:2] {
		var iv string
		var p2 int64
		fmt.Sscan(a, &iv, &p1[i], &p2)
	}
	if time.Since(time.Unix(p1[0], 0)) < 3*day || p1[1]*1000 != got[1].T-3_600_000-1000 {
		t.Fatalf("asked from %v, then from %v", time.Unix(p1[0], 0), time.Unix(p1[1], 0))
	}
}

func TestParseSpark(t *testing.T) {
	body := []byte(`{"NQ=F":{"timestamp":[1],"symbol":"NQ=F","close":[29739.5,null],"chartPreviousClose":29979.5},"AAPL":{"close":[343.83],"chartPreviousClose":347.1},"ZZZ":{"close":[null]}}`)
	got, err := parseSpark(body, map[string]string{"NQ=F": "CME:NQ", "AAPL": "US:AAPL", "ZZZ": "US:ZZZ"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Ticker{{Symbol: "CME:NQ", Open: 29979.5, Last: 29739.5}, {Symbol: "US:AAPL", Open: 347.1, Last: 343.83}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

// The search keeps stocks and ETFs listed in New York, not the same company
// elsewhere, its futures or tokens.
func TestParseSearch(t *testing.T) {
	body := []byte(`{"quotes":[
		{"symbol":"AAPL","quoteType":"EQUITY","exchange":"NMS","shortname":"Apple Inc.","longname":"Apple Inc."},
		{"symbol":"SAAPL=F","quoteType":"FUTURE","exchange":"CME","shortname":"Apple Inc Stock Futures"},
		{"symbol":"AAPL.TO","quoteType":"EQUITY","exchange":"TOR","shortname":"APPLE CDR"},
		{"symbol":"BRK-B","quoteType":"EQUITY","exchange":"NYQ","shortname":"Berkshire Hathaway Inc. New"},
		{"symbol":"QQQ","quoteType":"ETF","exchange":"NGM","shortname":"Invesco QQQ Trust, Series 1"},
		{"symbol":"APC","quoteType":"EQUITY","exchange":"GER","shortname":"Apple Inc. R"},
		{"symbol":"BRKB-USD","quoteType":"CRYPTOCURRENCY","exchange":"CCC"}]}`)
	got, err := parseSearch(body)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, in := range got {
		ids = append(ids, in.ID)
	}
	if want := []string{"US:AAPL", "US:BRK-B", "US:QQQ"}; !slices.Equal(ids, want) || got[0].Name != "Apple Inc." || got[1].Ticker != "BRK-B" {
		t.Fatalf("got %+v", got)
	}
}

func TestInstrumentOf(t *testing.T) {
	for id, want := range map[string]string{"CME:NQ": "NQ=F", "CME:MNQ": "MNQ=F", "US:AAPL": "AAPL", "US:BRK-B": "BRK-B"} {
		if in, ok := InstrumentOf(id); !ok || in.Ticker != want || !IsYahoo(id) {
			t.Errorf("%s: %+v %v", id, in, ok)
		}
	}
	for _, id := range []string{"BTCUSDT", "CME:XX", "US:", "NASDAQ:AAPL"} {
		if _, ok := InstrumentOf(id); ok {
			t.Errorf("%s is a market", id)
		}
	}
	for _, f := range Futures {
		if f.Tick <= 0 || f.Delay != 10*time.Minute || !strings.HasSuffix(f.Ticker, "=F") {
			t.Errorf("%+v", f)
		}
		if math.Mod(1, f.Tick) > 1e-9 && math.Abs(math.Mod(1, f.Tick)-f.Tick) > 1e-9 {
			t.Errorf("%s ticks of %v don't make a point", f.Code, f.Tick)
		}
	}
}

// A future's regular hours are made of Yahoo's bars that open from 9:30 on:
// minutes as they are, an hour of two half hours, 4 hours of eight; days
// are the same as without.
func TestPlan(t *testing.T) {
	nq, _ := InstrumentOf("CME:NQ")
	aapl, _ := InstrumentOf("US:AAPL")
	for _, c := range []struct {
		in       Instrument
		rth      bool
		iv, src  string
		group    int
		filtered bool
	}{
		{nq, false, "1h", "1h", 1, false},
		{nq, false, "4h", "1h", 4, false},
		{nq, true, "1m", "1m", 1, true},
		{nq, true, "5m", "5m", 1, true},
		{nq, true, "15m", "15m", 1, true},
		{nq, true, "1h", "30m", 2, true},
		{nq, true, "4h", "30m", 8, true},
		{nq, true, "1d", "1d", 1, false},
		{nq, true, "1w", "1wk", 1, false},
		{aapl, true, "4h", "1h", 4, false}, // a stock's hours are its own
	} {
		p, ok := Yahoo{RTH: c.rth}.plan(c.in, c.iv)
		if !ok || p.src.name != c.src || p.group != c.group || (p.keep != nil) != c.filtered {
			t.Errorf("%s %s rth %v: from %s × %d, filtered %v", c.in.Code, c.iv, c.rth, p.src.name, p.group, p.keep != nil)
		}
	}
	if _, ok := (Yahoo{}).plan(nq, "3m"); ok {
		t.Error("3-minute bars planned")
	}
	ny := func(h, m int) int64 { return time.Date(2026, 10, 28, h, m, 0, 0, newYork).UnixMilli() }
	for at, want := range map[int64]bool{ny(9, 29): false, ny(9, 30): true, ny(16, 14): true, ny(16, 15): false, ny(18, 0): false, ny(3, 0): false} {
		if InRTH(at) != want {
			t.Errorf("%s in the regular hours: %v", time.UnixMilli(at).In(newYork).Format("15:04"), !want)
		}
	}
}

// In the regular hours an hour opens at 9:30, 10:30 … 15:30, the last one
// three quarters long, made of whole half hours; minutes keep to 9:30–16:15;
// the stream too.
func TestYahooRTH(t *testing.T) {
	f := &fakeYahoo{first: time.Now().Add(-400 * day)}
	srv := httptest.NewServer(f)
	defer srv.Close()
	y := Yahoo{HTTP: srv.Client(), Base: srv.URL, RTH: true}
	ctx := context.Background()
	hours, err := y.Klines(ctx, "CME:NQ", "1h", 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	// Half hours go back 60 days: about 40 days of 7 hours.
	if len(hours) < 200 || len(hours) >= 1000 || time.Since(time.UnixMilli(hours[0].T)) > 60*day {
		t.Fatalf("%d hours from %s", len(hours), time.UnixMilli(hours[0].T))
	}
	for _, b := range hours {
		at := time.UnixMilli(b.T).In(newYork)
		m := at.Hour()*60 + at.Minute()
		// Two half hours each, 15:30 too (15:30 and 16:00); the newest may be under way.
		if m < 9*60+30 || m > 15*60+30 || m%60 != 30 || b.V != 2 && b.T != hours[len(hours)-1].T {
			t.Fatalf("an hour at %s of %v half hours", at.Format("Mon 15:04"), b.V)
		}
	}
	quarters, err := y.Klines(ctx, "CME:NQ", "15m", 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range quarters {
		if !InRTH(b.T) {
			t.Fatalf("a quarter at %s", time.UnixMilli(b.T).In(newYork).Format("Mon 15:04"))
		}
	}
	// A thousand of the sparser regular quarters reach further back.
	if eth, _ := (Yahoo{HTTP: srv.Client(), Base: srv.URL}).Klines(ctx, "CME:NQ", "15m", 0, 1000); len(quarters) != 1000 || eth[0].T <= quarters[0].T {
		t.Fatalf("%d regular quarters from %s, the whole session's from %s", len(quarters), time.UnixMilli(quarters[0].T), time.UnixMilli(eth[0].T))
	}
	y.Every = time.Millisecond
	sctx, cancel := context.WithCancel(ctx)
	n := 0
	y.Stream(sctx, "CME:NQ", "1h", func(b Bar, _ bool, _ int64) {
		if !InRTH(b.T) || sinceMidnight(b.T)%3_600_000 != 1_800_000 {
			t.Errorf("streamed an hour at %s", time.UnixMilli(b.T).In(newYork).Format("Mon 15:04"))
		}
		if n++; n == 4 {
			cancel()
		}
	})
}
