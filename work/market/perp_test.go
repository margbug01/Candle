package market

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakePerp is Binance's perpetuals API: REST answers by path, and on the
// streams under /market, a message for each stream asked for.
func fakePerp(t *testing.T, asked *[]string) (Perp, func()) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*asked = append(*asked, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.URL.Path == "/fapi/v1/klines":
			fmt.Fprint(w, `[[1000,"1.5","2","1","1.75","10",1999,"x"]]`)
		case r.URL.Path == "/fapi/v1/exchangeInfo":
			fmt.Fprint(w, `{"symbols":[
				{"symbol":"BTCUSDT","contractType":"PERPETUAL","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT","underlyingType":"COIN","filters":[{"filterType":"PRICE_FILTER","tickSize":"0.10"}]},
				{"symbol":"NVDAUSDT","contractType":"TRADIFI_PERPETUAL","status":"TRADING","baseAsset":"NVDA","quoteAsset":"USDT","underlyingType":"EQUITY","filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01"}]},
				{"symbol":"XAUUSDT","contractType":"TRADIFI_PERPETUAL","status":"TRADING","baseAsset":"XAU","quoteAsset":"USDT","underlyingType":"COMMODITY","filters":[]},
				{"symbol":"BTCUSDT_261225","contractType":"CURRENT_QUARTER","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT","filters":[]},
				{"symbol":"OLDUSDT","contractType":"PERPETUAL","status":"SETTLING","baseAsset":"OLD","quoteAsset":"USDT","filters":[]}]}`)
		case r.URL.Path == "/fapi/v1/ticker/24hr":
			now := time.Now().UnixMilli()
			fmt.Fprintf(w, `[{"symbol":"BTCUSDT","openPrice":"79000","lastPrice":"80000","quoteVolume":"9e9","closeTime":%d},
				{"symbol":"BTCUSDT_261225","openPrice":"79000","lastPrice":"81000","quoteVolume":"1e6","closeTime":%d},
				{"symbol":"OLDUSDT","openPrice":"1","lastPrice":"1","quoteVolume":"5","closeTime":1}]`, now, now)
		case r.URL.Path == "/fapi/v1/premiumIndex":
			fmt.Fprint(w, `{"symbol":"BTCUSDT","markPrice":"79953.4","lastFundingRate":"0.00004967","nextFundingTime":1793260800000}`)
		case strings.HasPrefix(r.URL.Path, "/market/"):
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			if r.URL.Path == "/market/stream" {
				for _, s := range strings.Split(r.URL.Query().Get("streams"), "/") {
					sym := strings.ToUpper(strings.TrimSuffix(s, "@miniTicker"))
					c.Write(r.Context(), websocket.MessageText, []byte(`{"stream":"`+s+`","data":{"e":"24hrMiniTicker","E":1,"s":"`+sym+`","c":"2","o":"1","q":"5"}}`))
				}
			} else {
				c.Write(r.Context(), websocket.MessageText, []byte(`{"e":"kline","E":5,"s":"BTCUSDT","k":{"t":1000,"T":1999,"s":"BTCUSDT","i":"1m","o":"1","h":"3","l":"0.5","c":"2","v":"7","x":false}}`))
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	return Perp{HTTP: srv.Client(), Base: srv.URL, WS: ws}, srv.Close
}

// Candle's names for perpetuals end in .P; Binance's do not.
func TestPerp(t *testing.T) {
	var asked []string
	p, done := fakePerp(t, &asked)
	defer done()
	ctx := context.Background()
	bars, err := p.Klines(ctx, "BTCUSDT.P", "1h", 5000, 1000)
	if err != nil || len(bars) != 1 || bars[0] != (Bar{T: 1000, O: 1.5, H: 2, L: 1, C: 1.75, V: 10}) {
		t.Fatalf("klines: %+v, %v", bars, err)
	}
	if !strings.HasSuffix(asked[0], "symbol=BTCUSDT") || !strings.Contains(asked[0], "endTime=5000") {
		t.Fatalf("asked %s", asked[0])
	}
	syms, err := p.Symbols(ctx, "NVDAUSDT.P", "BTCUSDT.P")
	if err != nil || len(syms) != 2 || syms[0] != (Symbol{Name: "NVDAUSDT.P", Base: "NVDA", Quote: "USDT", Tick: 0.01, Desc: "股票永续"}) || syms[1].Tick != 0.1 {
		t.Fatalf("symbols: %+v, %v", syms, err)
	}
	for _, n := range []string{"BTCUSDT_261225.P", "OLDUSDT.P", "ETHUSDT.P"} {
		if _, err := p.Symbols(ctx, n); err == nil {
			t.Errorf("%s looked up", n)
		}
	}
	tk, err := p.Tickers(ctx)
	if err != nil || len(tk) != 1 || tk[0] != (Ticker{Symbol: "BTCUSDT.P", Open: 79000, Last: 80000, QuoteVolume: 9e9}) {
		t.Fatalf("tickers: %+v, %v", tk, err)
	}
	f, err := p.Funding(ctx, "BTCUSDT.P")
	if err != nil || f != (Funding{Mark: 79953.4, Rate: 0.00004967, Next: 1793260800000}) || !strings.Contains(asked[len(asked)-1], "symbol=BTCUSDT") {
		t.Fatalf("funding: %+v, %v", f, err)
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	var got []Bar
	p.Stream(sctx, "BTCUSDT.P", "1m", func(b Bar, _ bool, _ int64) { got = append(got, b); cancel() })
	cancel()
	if len(got) != 1 || got[0].C != 2 || !slices.Contains(asked, "/market/ws/btcusdt@kline_1m?") {
		t.Fatalf("stream: %+v, asked %v", got, asked)
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	seen := map[string]bool{}
	p.Watch(wctx, []string{"BTCUSDT.P", "NVDAUSDT.P"}, func(x Ticker) {
		seen[x.Symbol] = true
		if len(seen) == 2 {
			cancel()
		}
	})
	cancel()
	if !seen["BTCUSDT.P"] || !seen["NVDAUSDT.P"] {
		t.Fatalf("watch: %v", seen)
	}
	for id, want := range map[string]bool{"BTCUSDT.P": true, "BTCUSDT": false, ".P": false, "CME:NQ": false} {
		if IsPerp(id) != want {
			t.Errorf("IsPerp(%q) = %v", id, !want)
		}
	}
}
