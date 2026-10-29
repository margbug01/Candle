package market

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func localAt(loc *time.Location, s string) int64 {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

// Candle names the markets beyond the coins by exchange, as TradingView
// does, and knows each one's source, clock and day.
func TestListedInstruments(t *testing.T) {
	for _, c := range []struct {
		id, ticker, venue string
		index, yahoo      bool
		tick              float64
		delay             time.Duration
	}{
		{"SSE:600795", "1.600795", "上交所", false, false, 0.01, 0},
		{"SSE:000001", "1.000001", "上交所", true, false, 0.01, 0},   // 上证指数
		{"SZSE:000001", "0.000001", "深交所", false, false, 0.01, 0}, // 平安银行
		{"SZSE:399006", "0.399006", "深交所", true, false, 0.01, 0},
		{"HKEX:00700", "116.00700", "港交所", false, false, 0.001, 0},
		{"HKEX:HSI", "100.HSI", "港交所", true, false, 0.01, 0},
		{"HKEX:HSTECH", "124.HSTECH", "港交所", true, false, 0.01, 0},
		{"KRX:000660", "000660.KS", "韩国交易所", false, true, 1, 20 * time.Minute},
		{"CBOE:VIX", "^VIX", "Cboe", true, true, 0.01, 15 * time.Minute},
	} {
		in, ok := InstrumentOf(c.id)
		if !ok || in.ID != c.id || in.Ticker != c.ticker || in.Venue != c.venue || in.Index != c.index || in.Tick != c.tick || in.Delay != c.delay {
			t.Errorf("%s: %+v %v", c.id, in, ok)
		}
		if IsYahoo(c.id) != c.yahoo || IsEastmoney(c.id) == c.yahoo || !IsListed(c.id) {
			t.Errorf("%s: yahoo %v, eastmoney %v", c.id, IsYahoo(c.id), IsEastmoney(c.id))
		}
	}
	for _, id := range []string{"SZSE:830799", "SSE:60079", "HKEX:700", "HKEX:XYZ", "KRX:A00660", "CBOE:SPX", "BTCUSDT", "SSE:"} {
		if in, ok := InstrumentOf(id); ok {
			t.Errorf("%s: %+v", id, in)
		}
	}
	if IsListed("BTCUSDT") || IsListed("BTCUSDT.P") || !IsListed("US:AAPL") || !IsListed("CME:NQ") {
		t.Error("IsListed")
	}
}

// A market's day begins at its opening on its own clock: Shanghai's at 9:30,
// Seoul's at 9:00, the VIX's at 3:00 in New York (the day the clocks go
// forward too); New York's stocks and the CME's futures as before.
func TestListedSessions(t *testing.T) {
	for _, c := range []struct {
		id         string
		loc        *time.Location
		t, session string
	}{
		{"SSE:600795", shanghai, "2026-10-29 14:30", "2026-10-29 09:30"},
		{"HKEX:00700", hongKong, "2026-10-29 09:30", "2026-10-29 09:30"},
		{"KRX:000660", seoul, "2026-10-29 15:00", "2026-10-29 09:00"},
		{"CBOE:VIX", newYork, "2026-10-29 10:00", "2026-10-29 03:00"},
		{"CBOE:VIX", newYork, "2026-03-08 12:00", "2026-03-08 03:00"},
		{"US:AAPL", newYork, "2026-10-29 15:00", "2026-10-29 09:30"},
		{"CME:NQ", newYork, "2026-10-29 10:00", "2026-10-28 18:00"},
	} {
		in, _ := InstrumentOf(c.id)
		if got := in.SessionStart(localAt(c.loc, c.t)); got != localAt(c.loc, c.session) {
			t.Errorf("%s at %s: %s, want %s", c.id, c.t, time.UnixMilli(got).In(c.loc).Format("01-02 15:04"), c.session)
		}
	}
}

// fakeEast answers as Eastmoney does, with bars from klines by klt, and
// notes what was asked.
type fakeEast struct {
	mu     sync.Mutex
	asked  []string
	klines map[string][]string // by secid and klt: "1.600795/5"
}

func (f *fakeEast) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.Header.Get("Referer") != "https://quote.eastmoney.com/" {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	f.mu.Lock()
	f.asked = append(f.asked, r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()
	switch r.URL.Path {
	case "/api/qt/stock/kline/get":
		k, ok := f.klines[q.Get("secid")+"/"+q.Get("klt")]
		if !ok {
			fmt.Fprint(w, `{"rc":0,"data":null}`)
			return
		}
		var n int
		fmt.Sscan(q.Get("lmt"), &n)
		k = k[max(len(k)-n, 0):]
		fmt.Fprintf(w, `{"rc":0,"data":{"code":"x","klines":["%s"]}}`, strings.Join(k, `","`))
	case "/api/qt/ulist.np/get":
		fmt.Fprint(w, `{"rc":0,"data":{"total":3,"diff":[{"f2":5.38,"f12":"600795","f13":1,"f14":"国电电力","f18":5.26},{"f2":"-","f12":"00005","f13":116,"f18":"-"},{"f2":22958.65,"f12":"HSI","f13":100,"f18":23094.83}]}}`)
	case "/api/qt/stock/get":
		switch q.Get("secid") {
		case "116.00700":
			fmt.Fprint(w, `{"rc":0,"data":{"f57":"00700","f58":"腾讯控股","f59":3}}`)
		case "100.HSI":
			fmt.Fprint(w, `{"rc":0,"data":{"f57":"HSI","f58":"HSI Index","f59":2}}`)
		default:
			fmt.Fprint(w, `{"rc":0,"data":null}`)
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeEast) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[len(f.asked)-1]
}

// fiveMinutes are 5-minute bars from from to to, the i0th on: the ith
// opening at 10+i/100 with volume i+1.
func fiveMinutes(day string, from, to string, i0 int) []string {
	var out []string
	t, _ := time.Parse("2006-01-02 15:04", day+" "+from)
	end, _ := time.Parse("2006-01-02 15:04", day+" "+to)
	for i := i0; !t.After(end); i++ {
		t = t.Add(5 * time.Minute)
		if t.After(end) {
			break
		}
		p := 10 + float64(i)/100
		out = append(out, fmt.Sprintf("%s,%.2f,%.2f,%.2f,%.2f,%d,0", t.Format("2006-01-02 15:04"), p, p+0.01, p+0.02, p-0.01, 1+i))
	}
	return out
}

func newFakeEast(t *testing.T) (*fakeEast, Eastmoney) {
	t.Helper()
	day := "2026-10-29"
	// Shanghai's morning and afternoon, Hong Kong's longer ones.
	cn := append(fiveMinutes(day, "09:30", "11:30", 0), fiveMinutes(day, "13:00", "15:00", 24)...)
	hk := append(fiveMinutes(day, "09:30", "12:00", 0), fiveMinutes(day, "13:00", "16:00", 30)...)
	f := &fakeEast{klines: map[string][]string{
		"1.600795/1": {"2026-10-29 09:31,5.30,5.31,5.32,5.29,1200,0", "2026-10-29 13:01,5.31,5.33,5.34,5.30,800,0"},
		"1.600795/5": cn, "116.00700/5": hk,
		"1.600795/101": {"2026-10-27,5.20,5.22,5.25,5.18,100,0", "2026-10-28,5.22,5.26,5.27,5.21,200,0", "2026-10-29,5.26,5.38,5.40,5.25,300,0"},
		"1.600795/102": {"2026-10-23,5.00,5.20,5.30,4.90,1000,0", "2026-10-29,5.20,5.38,5.40,5.15,600,0"},
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, Eastmoney{HTTP: srv.Client(), Base: srv.URL, His: srv.URL, Push: srv.URL, Every: 10 * time.Millisecond}
}

func times(bars []Bar, loc *time.Location) (s []string) {
	for _, b := range bars {
		s = append(s, time.UnixMilli(b.T).In(loc).Format("01-02 15:04"))
	}
	return s
}

// Eastmoney stamps a bar at its end, in Beijing time, a day by its date, a
// week by its last day's: Candle's open at their beginnings. Hours and 4
// hours are made of 5 minutes, as TradingView makes them: Hong Kong's hour
// from 11:30 is half of one. Shanghai's volumes are in lots of 100.
func TestEastmoneyKlines(t *testing.T) {
	f, e := newFakeEast(t)
	ctx := context.Background()
	get := func(sym, iv string, end int64, limit int) []Bar {
		t.Helper()
		bars, err := e.Klines(ctx, sym, iv, end, limit)
		if err != nil {
			t.Fatalf("%s %s: %v", sym, iv, err)
		}
		return bars
	}
	one := get("SSE:600795", "1m", 0, 10)
	if got := times(one, shanghai); !slices.Equal(got, []string{"10-29 09:30", "10-29 13:00"}) || one[0].V != 120000 || one[0].O != 5.30 || one[0].C != 5.31 || one[0].H != 5.32 || one[0].L != 5.29 {
		t.Fatalf("1m: %v %+v", got, one)
	}
	if a := f.last(); !strings.Contains(a, "secid=1.600795") || !strings.Contains(a, "klt=1&lmt=400") || !strings.Contains(a, "fqt=1") {
		t.Fatalf("asked %s", a)
	}
	for _, c := range []struct {
		sym, iv string
		loc     *time.Location
		want    []string
		first   int // how many 5-minute bars the first holds
	}{
		{"SSE:600795", "1h", shanghai, []string{"10-29 09:30", "10-29 10:30", "10-29 13:00", "10-29 14:00"}, 12},
		{"SSE:600795", "4h", shanghai, []string{"10-29 09:30"}, 48},
		{"HKEX:00700", "1h", hongKong, []string{"10-29 09:30", "10-29 10:30", "10-29 11:30", "10-29 13:00", "10-29 14:00", "10-29 15:00"}, 12},
		{"HKEX:00700", "4h", hongKong, []string{"10-29 09:30", "10-29 14:00"}, 42},
	} {
		bars := get(c.sym, c.iv, 0, 100)
		if !strings.Contains(f.last(), "klt=5&lmt=2500") {
			t.Errorf("%s %s: asked %s", c.sym, c.iv, f.last())
		}
		if got := times(bars, c.loc); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: %v, want %v", c.sym, c.iv, got, c.want)
			continue
		}
		lot := map[bool]float64{true: 100, false: 1}[c.loc == shanghai]
		// The volumes 1, 2, 3…: the first bar's sum, its first open, its last close.
		if b := bars[0]; b.V != float64(c.first*(c.first+1)/2)*lot || b.O != 10 || b.C != 10+float64(c.first-1)/100+0.01 {
			t.Errorf("%s %s first bar: %+v", c.sym, c.iv, b)
		}
	}
	// Hong Kong's 11:30 hour is the half to noon.
	if hk := get("HKEX:00700", "1h", 0, 100); hk[2].V != float64(25+26+27+28+29+30) || hk[3].O != 10.30 {
		t.Errorf("Hong Kong's noon: %+v %+v", hk[2], hk[3])
	}
	day := get("SSE:600795", "1d", 0, 2)
	if got := times(day, shanghai); !slices.Equal(got, []string{"10-28 09:30", "10-29 09:30"}) || day[1].V != 30000 {
		t.Fatalf("1d: %v %+v", got, day)
	}
	if a := f.last(); !strings.Contains(a, "klt=101") || !strings.Contains(a, "end=20500101") || !strings.Contains(a, "lmt=3") {
		t.Fatalf("asked %s", a)
	}
	// The page before a day: the days up to it, without it.
	older := get("SSE:600795", "1d", day[1].T-1, 2)
	if got := times(older, shanghai); !slices.Equal(got, []string{"10-27 09:30", "10-28 09:30"}) || !strings.Contains(f.last(), "end=20261029") {
		t.Fatalf("older days: %v, asked %s", got, f.last())
	}
	week := get("SSE:600795", "1w", 0, 5)
	if got := times(week, shanghai); !slices.Equal(got, []string{"10-19 09:30", "10-26 09:30"}) {
		t.Fatalf("1w: %v", got)
	}
	// Minutes before end, all there are being asked at once.
	if bars := get("SSE:600795", "1h", localAt(shanghai, "2026-10-29 12:00"), 100); !slices.Equal(times(bars, shanghai), []string{"10-29 09:30", "10-29 10:30"}) {
		t.Fatalf("hours before noon: %v", times(bars, shanghai))
	}
	if _, err := e.Klines(ctx, "SZSE:000002", "1d", 0, 10); err == nil {
		t.Fatal("a market Eastmoney lacks: no error")
	}
	if _, err := e.Klines(ctx, "US:AAPL", "1d", 0, 10); err == nil {
		t.Fatal("a US stock from Eastmoney: no error")
	}
}

// A group whose first bars are missing, the oldest asked for beginning in
// the middle of it, is left out.
func TestEastmoneyGroupsOnlyWholeHours(t *testing.T) {
	in, _ := InstrumentOf("SSE:600795")
	var bars []Bar
	for _, s := range []string{"10:05", "10:10", "10:30", "10:35", "13:00"} {
		bars = append(bars, Bar{T: localAt(shanghai, "2026-10-29 "+s), O: 1, H: 1, L: 1, C: 1, V: 1})
	}
	if got := times(in.eastGroup(bars, 1), shanghai); !slices.Equal(got, []string{"10-29 10:30", "10-29 13:00"}) {
		t.Fatalf("groups: %v", got)
	}
}

// Prices fifty to a request, from the last session's close; one suspended
// ("-") is left out. Names and decimals, an index keeping its Chinese name.
func TestEastmoneyQuotes(t *testing.T) {
	f, e := newFakeEast(t)
	ctx := context.Background()
	q, err := e.Quotes(ctx, []string{"SSE:600795", "HKEX:00005", "HKEX:HSI", "US:AAPL", "BTCUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Ticker{{Symbol: "SSE:600795", Open: 5.26, Last: 5.38}, {Symbol: "HKEX:HSI", Open: 23094.83, Last: 22958.65}}
	if !slices.Equal(q, want) {
		t.Fatalf("quotes %+v", q)
	}
	if a := f.last(); !strings.Contains(a, "secids=1.600795%2C116.00005%2C100.HSI") {
		t.Fatalf("asked %s", a)
	}
	s, err := e.Symbols(ctx, "HKEX:00700", "HKEX:HSI")
	if err != nil || len(s) != 2 || s[0] != (Symbol{Name: "HKEX:00700", Base: "00700", Tick: 0.001, Desc: "腾讯控股"}) || s[1].Desc != "恒生指数" || s[1].Tick != 0.01 {
		t.Fatalf("symbols %+v %v", s, err)
	}
	if _, err := e.Symbols(ctx, "SSE:600000"); err == nil {
		t.Fatal("a market Eastmoney does not know: no error")
	}
	var got []Ticker
	wctx, cancel := context.WithCancel(ctx)
	e.Watch(wctx, []string{"SSE:600795"}, func(tk Ticker) {
		if got = append(got, tk); len(got) == 2 {
			cancel()
		}
	})
	if len(got) != 2 || got[1].Last != 5.38 {
		t.Fatalf("watched %+v", got)
	}
}

// The stream asks again and again for the newest bars, and passes on the
// newest two; hours from enough 5 minutes to leave a whole one before the
// newest.
func TestEastmoneyStream(t *testing.T) {
	f, e := newFakeEast(t)
	for _, c := range []struct {
		iv   string
		want []string
		ask  string
	}{
		{"1m", []string{"10-29 09:30", "10-29 13:00"}, "klt=1&lmt=2"},
		{"1h", []string{"10-29 13:00", "10-29 14:00"}, "klt=5&lmt=200"},
		{"1d", []string{"10-28 09:30", "10-29 09:30"}, "klt=101&lmt=2"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // fails rather than hangs
		var got []Bar
		rounds := 0
		e.Stream(ctx, "SSE:600795", c.iv, func(b Bar, closed bool, at int64) {
			if got = append(got, b); len(got)%2 == 0 {
				if rounds++; rounds == 2 {
					cancel()
				}
			}
		})
		if len(got) != 4 || !slices.Equal(times(got[2:], shanghai), c.want) || !strings.Contains(f.last(), c.ask) {
			t.Errorf("%s: %v, asked %s", c.iv, times(got, shanghai), f.last())
		}
	}
}

// Of what Eastmoney's search finds, the markets Candle has, as it names
// them, with their Chinese names: A shares, indices, Hong Kong's and Korea's
// shares, US stocks; not Beijing's, nor an index Candle has no clock for.
func TestFound(t *testing.T) {
	got := Found([]Suggestion{
		{Code: "600795", Name: "国电电力", Market: 1}, {Code: "000001", Name: "上证指数", Market: 1},
		{Code: "000001", Name: "平安银行", Market: 0}, {Code: "830799", Name: "艾融软件", Market: 0},
		{Code: "00700", Name: "腾讯控股", Market: 116}, {Code: "HSI", Name: "恒生指数", Market: 100},
		{Code: "N225", Name: "日经225", Market: 100}, {Code: "000660", Name: "SK海力士", Market: 177},
		{Code: "SKHY", Name: "SK海力士", Market: 105}, {Code: "07709", Name: "南方东英SK海力士", Market: 116},
	})
	var ids, zh []string
	for _, in := range got {
		ids, zh = append(ids, in.ID), append(zh, in.Alias)
	}
	if want := []string{"SSE:600795", "SSE:000001", "SZSE:000001", "HKEX:00700", "HKEX:HSI", "KRX:000660", "US:SKHY", "HKEX:07709"}; !slices.Equal(ids, want) {
		t.Fatalf("found %v, want %v", ids, want)
	}
	if want := []string{"国电电力", "上证指数", "平安银行", "腾讯控股", "恒生指数", "SK海力士", "SK海力士", "南方东英SK海力士"}; !slices.Equal(zh, want) {
		t.Fatalf("names %v", zh)
	}
	if !got[1].Index || got[2].Index || got[4].Ticker != "100.HSI" {
		t.Fatalf("%+v", got)
	}
}

// Yahoo looks up Korea's shares and the VIX as its stocks: a Korean price
// steps by won, the VIX keeps its Chinese name. It takes none of Eastmoney's.
func TestYahooListed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := map[string]string{"/v8/finance/chart/000660.KS": "SK hynix Inc.", "/v8/finance/chart/^VIX": "CBOE Volatility Index"}[r.URL.Path]
		fmt.Fprintf(w, `{"chart":{"result":[{"meta":{"longName":%q,"priceHint":2}}]}}`, name)
	}))
	defer srv.Close()
	y := Yahoo{HTTP: srv.Client(), Base: srv.URL}
	ctx := context.Background()
	s, err := y.Symbols(ctx, "KRX:000660", "CBOE:VIX")
	if err != nil || len(s) != 2 || s[0].Tick != 1 || s[0].Desc != "SK hynix Inc." || s[1].Tick != 0.01 || s[1].Desc != "恐慌指数" {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := y.Symbols(ctx, "SSE:600795"); err == nil {
		t.Fatal("Yahoo looked up an A share")
	}
	if _, err := y.Klines(ctx, "HKEX:00700", "1h", 0, 10); err == nil {
		t.Fatal("Yahoo's bars of a Hong Kong share")
	}
	if q, err := y.Quotes(ctx, []string{"SSE:600795"}); err != nil || len(q) != 0 {
		t.Fatalf("Yahoo's quote of an A share: %v %v", q, err)
	}
}

// A market trades on weekdays, from 5 minutes before each part of its day
// to 15 after: Shanghai's lunch and evenings are quiet, Hong Kong's closing
// auction is not. Stream and Watch ask every few seconds then, every minute
// otherwise.
func TestEastmoneyPace(t *testing.T) {
	cn, _ := InstrumentOf("SSE:600795")
	hk, _ := InstrumentOf("HKEX:HSI")
	for _, c := range []struct {
		in   Instrument
		at   string
		want bool
	}{
		{cn, "2026-10-29 09:24", false}, {cn, "2026-10-29 09:26", true}, {cn, "2026-10-29 11:40", true},
		{cn, "2026-10-29 12:00", false}, {cn, "2026-10-29 15:10", true}, {cn, "2026-10-29 15:20", false},
		{cn, "2026-10-31 10:00", false}, // a Saturday
		{hk, "2026-10-29 12:10", true}, {hk, "2026-10-29 12:30", false}, {hk, "2026-10-29 16:10", true}, {hk, "2026-10-29 16:20", false},
	} {
		if got := c.in.trading(time.UnixMilli(localAt(shanghai, c.at))); got != c.want {
			t.Errorf("%s at %s: trading %v", c.in.ID, c.at, got)
		}
	}
	e := Eastmoney{}
	noon, night := time.UnixMilli(localAt(shanghai, "2026-10-29 12:10")), time.UnixMilli(localAt(shanghai, "2026-10-29 20:00"))
	if e.pace([]Instrument{cn}, noon) != time.Minute || e.pace([]Instrument{cn, hk}, noon) != 5*time.Second || e.pace([]Instrument{cn, hk}, night) != time.Minute {
		t.Fatal("pace")
	}
}

// Once Eastmoney hangs up, Candle asks it nothing for a while, longer each
// time, and again once that is over; a request that comes through ends the
// rest. A market Eastmoney does not know is no failure.
func TestEastmoneyRests(t *testing.T) {
	defer func(r, m time.Duration) { eastRest, eastRestMax = r, m }(eastRest, eastRestMax)
	eastRest, eastRestMax = 150*time.Millisecond, 250*time.Millisecond
	var mu sync.Mutex
	asked, hangUp := 0, true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		h := hangUp
		mu.Unlock()
		if h { // as Eastmoney does: no answer at all
			c, _, _ := w.(http.Hijacker).Hijack()
			c.Close()
			return
		}
		// A new connection each time: Go asks again on its own when a kept
		// one is hung up on, which would count twice.
		w.Header().Set("Connection", "close")
		if r.URL.Query().Get("secid") == "1.600000" {
			fmt.Fprint(w, `{"rc":0,"data":null}`)
			return
		}
		fmt.Fprint(w, `{"rc":0,"data":{"f57":"600795","f58":"国电电力","f59":2}}`)
	}))
	defer srv.Close()
	e := Eastmoney{HTTP: srv.Client(), His: srv.URL, Push: srv.URL}
	ctx := context.Background()
	count := func() int { mu.Lock(); defer mu.Unlock(); return asked }
	set := func(h bool) { mu.Lock(); hangUp = h; mu.Unlock() }
	if _, err := e.Symbols(ctx, "SSE:600795"); err == nil || errors.Is(err, ErrEastmoneyResting) || count() != 1 {
		t.Fatalf("hung up: %v, %d asked", err, count())
	}
	if _, err := e.Klines(ctx, "SSE:600795", "1d", 0, 10); !errors.Is(err, ErrEastmoneyResting) || count() != 1 {
		t.Fatalf("resting: %v, %d asked", err, count())
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := e.Symbols(ctx, "SSE:600795"); err == nil || count() != 2 { // asks again, hung up again: a longer rest
		t.Fatalf("after the rest: %v, %d asked", err, count())
	}
	time.Sleep(200 * time.Millisecond) // past the first rest's length, not the second's
	if _, err := e.Symbols(ctx, "SSE:600795"); !errors.Is(err, ErrEastmoneyResting) || count() != 2 {
		t.Fatalf("the longer rest: %v, %d asked", err, count())
	}
	set(false)
	time.Sleep(100 * time.Millisecond)
	if s, err := e.Symbols(ctx, "SSE:600795"); err != nil || s[0].Desc != "国电电力" || count() != 3 {
		t.Fatalf("answered again: %+v %v", s, err)
	}
	// The rest after that is a short one again.
	set(true)
	e.Symbols(ctx, "SSE:600795")
	time.Sleep(200 * time.Millisecond)
	set(false)
	if _, err := e.Symbols(ctx, "SSE:600795"); err != nil || count() != 5 {
		t.Fatalf("after a rest begun anew: %v, %d asked", err, count())
	}
	if _, err := e.Symbols(ctx, "SSE:600000"); err == nil || errors.Is(err, ErrEastmoneyResting) {
		t.Fatalf("an unknown market: %v", err)
	}
	if _, err := e.Symbols(ctx, "SSE:600795"); err != nil || count() != 7 {
		t.Fatalf("an unknown market made Candle rest: %v, %d asked", err, count())
	}
	// Its own server for each base: another Eastmoney is not resting.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.Symbols(cctx, "SSE:600795"); err == nil {
		t.Fatal("a cancelled request came through")
	}
	if _, err := e.Symbols(ctx, "SSE:600795"); err != nil {
		t.Fatalf("a cancelled request made Candle rest: %v", err)
	}
}

// Watch and Stream ask every few seconds while the market trades, and a
// twelfth as often at night.
func TestEastmoneyAsksLessAtNight(t *testing.T) {
	defer func(f func() time.Time) { eastNow = f }(eastNow)
	_, e := newFakeEast(t)
	gap := func(at string, run func(context.Context, func())) time.Duration {
		eastNow = func() time.Time { return time.UnixMilli(localAt(shanghai, at)) }
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var times []time.Time
		run(ctx, func() {
			if times = append(times, time.Now()); len(times) == 2 {
				cancel()
			}
		})
		if len(times) < 2 {
			t.Fatalf("at %s: asked %d times", at, len(times))
		}
		return times[1].Sub(times[0])
	}
	watch := func(ctx context.Context, f func()) {
		e.Watch(ctx, []string{"SSE:600795"}, func(tk Ticker) {
			if tk.Symbol == "SSE:600795" {
				f()
			}
		})
	}
	stream := func(ctx context.Context, f func()) {
		n := 0
		e.Stream(ctx, "SSE:600795", "1d", func(Bar, bool, int64) {
			if n++; n%2 == 1 {
				f()
			}
		})
	}
	for name, run := range map[string]func(context.Context, func()){"watch": watch, "stream": stream} {
		day, night := gap("2026-10-29 10:00", run), gap("2026-10-29 20:00", run)
		if day > 60*time.Millisecond || night < 100*time.Millisecond {
			t.Errorf("%s: %v apart by day, %v at night", name, day, night)
		}
	}
}

// Most tests ask a fake Eastmoney many times over: no gap for them.
func init() { eastGap = 0 }

// Requests to Eastmoney go out eastGap apart, however many ask at once;
// one whose ctx ends while it waits goes nowhere.
func TestEastmoneyGap(t *testing.T) {
	defer func(g time.Duration) { eastGap = g }(eastGap)
	eastGap = 80 * time.Millisecond
	var mu sync.Mutex
	var at []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
		fmt.Fprint(w, `{"rc":0,"data":{"f57":"600795","f58":"国电电力","f59":2}}`)
	}))
	defer srv.Close()
	e := Eastmoney{HTTP: srv.Client(), Push: srv.URL}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { e.Symbols(context.Background(), "SSE:600795") })
	}
	time.Sleep(10 * time.Millisecond) // their turns taken: this one's is after them
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := e.Symbols(ctx, "SSE:600795"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a request whose ctx ended while waiting: %v", err)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(at) != 4 {
		t.Fatalf("%d requests came, want 4", len(at))
	}
	slices.SortFunc(at, func(x, y time.Time) int { return x.Compare(y) })
	for i := 1; i < len(at); i++ {
		if d := at[i].Sub(at[i-1]); d < eastGap-10*time.Millisecond {
			t.Errorf("request %d came %v after the one before, want %v at least", i, d, eastGap)
		}
	}
}
