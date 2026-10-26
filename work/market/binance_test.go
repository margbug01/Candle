package market

import "testing"

func TestParseSymbols(t *testing.T) {
	body := `{"timezone":"UTC","symbols":[{"symbol":"PEPEUSDT","status":"TRADING","baseAsset":"PEPE","baseAssetPrecision":2,"quoteAsset":"USDT","filters":[{"filterType":"PRICE_FILTER","minPrice":"0.00000001","maxPrice":"1.00000000","tickSize":"0.00000001"},{"filterType":"LOT_SIZE","minQty":"1.00","stepSize":"1.00"}]}]}`
	got, err := parseSymbols([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Symbol{Name: "PEPEUSDT", Base: "PEPE", Quote: "USDT", Tick: 1e-8}); len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTickersDropsDeadMarkets(t *testing.T) {
	body := `[
		{"symbol":"BTCUSDT","openPrice":"80000.00","highPrice":"81000","lowPrice":"79000","lastPrice":"80800.00","volume":"10","quoteVolume":"808000.5","openTime":1,"closeTime":2000,"firstId":1,"lastId":2,"count":2},
		{"symbol":"OLDUSDT","openPrice":"1","highPrice":"1","lowPrice":"1","lastPrice":"1","volume":"5","quoteVolume":"5","openTime":1,"closeTime":500,"firstId":1,"lastId":2,"count":2},
		{"symbol":"IDLEUSDT","openPrice":"0","highPrice":"0","lowPrice":"0","lastPrice":"0","volume":"0","quoteVolume":"0","openTime":1,"closeTime":2000,"firstId":-1,"lastId":-1,"count":0}
	]`
	got, err := parseTickers([]byte(body), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Ticker{Symbol: "BTCUSDT", Open: 80000, Last: 80800, QuoteVolume: 808000.5}) {
		t.Fatalf("got %+v", got)
	}
	if c := got[0].Change(); c < 0.00999 || c > 0.01001 {
		t.Fatalf("change %v, want 0.01", c)
	}
}

// From Binance's docs. e/E differ only in case; h, l and v are left out and
// must not land on another field.
func TestParseMiniTicker(t *testing.T) {
	data := `{"stream":"bnbbtc@miniTicker","data":{"e":"24hrMiniTicker","E":1672515782136,"s":"BNBBTC","c":"0.0025","o":"0.0010","h":"0.0025","l":"0.0010","v":"10000","q":"18"}}`
	got, err := parseMiniTicker([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Ticker{Symbol: "BNBBTC", Open: 0.001, Last: 0.0025, QuoteVolume: 18}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if _, err := parseMiniTicker([]byte(`{"result":null,"id":1}`)); err == nil {
		t.Fatal("a non-ticker message parsed")
	}
}
