package market

import (
	"strings"
	"testing"
)

func TestParseKlines(t *testing.T) {
	body := `[[1502928000000,"4261.48000000","4485.39000000","4200.74000000","4285.08000000","795.15037700",1503014399999,"3454770.05073206",3427,"616.24854100","2678216.40060401","0"]]`
	bars, err := parseKlines([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := Bar{T: 1502928000000, O: 4261.48, H: 4485.39, L: 4200.74, C: 4285.08, V: 795.150377}
	if len(bars) != 1 || bars[0] != want {
		t.Fatalf("got %+v, want %+v", bars, want)
	}
	if _, err := parseKlines([]byte(`[[1,"x","1","1","1","1"]]`)); err == nil {
		t.Fatal("a non-numeric price parsed")
	}
}

// The example from Binance's docs: t/T, l/L, v/V and e/E differ only in case.
func TestParseKlineEventKeepsCasePairsApart(t *testing.T) {
	data := `{"e":"kline","E":1672515782136,"s":"BNBBTC","k":{"t":1672515780000,"T":1672515839999,"s":"BNBBTC","i":"1m","f":100,"L":200,"o":"0.0010","c":"0.0020","h":"0.0025","l":"0.0015","v":"1000","n":100,"x":false,"q":"1.0000","V":"500","Q":"0.500","B":"123456"}}`
	bar, closed, at, err := parseKlineEvent([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := Bar{T: 1672515780000, O: 0.001, H: 0.0025, L: 0.0015, C: 0.002, V: 1000}
	if bar != want || closed || at != 1672515782136 {
		t.Fatalf("got %+v closed=%v at=%d, want %+v closed=false at=1672515782136", bar, closed, at, want)
	}
	if _, _, _, err := parseKlineEvent([]byte(`{"result":null,"id":1}`)); err == nil {
		t.Fatal("a non-kline message parsed")
	}
}

func TestParseChart(t *testing.T) {
	body := []byte(`{"chart":{"result":[{"meta":{"timezone":"EDT","exchangeTimezoneName":"America/New_York","dataGranularity":"1d"},"timestamp":[8640000,8726400,8812800],"events":{"splits":{"8726400":{"date":8726400,"numerator":4,"denominator":1}}},"indicators":{"quote":[{"open":[1,null,3],"high":[2,null,4],"low":[0.5,null,2.5],"close":[1.5,null,3.5],"volume":[10,null,30]}]}}],"error":null}}`)
	c, err := parseChart(body, "1d")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bars) != 2 || c.Skipped != 1 || c.Splits != 1 || c.Timezone != "America/New_York" {
		t.Fatalf("got %+v", c)
	}
	if want := (Bar{T: 8812800000, O: 3, H: 4, L: 2.5, C: 3.5, V: 30}); c.Bars[1] != want {
		t.Fatalf("got %+v, want %+v", c.Bars[1], want)
	}
	if _, err := parseChart(body, "1h"); err == nil || !strings.Contains(err.Error(), "got 1d") {
		t.Fatalf("coarser bars than asked for were accepted: %v", err)
	}
	// An intraday chart after the close: the last row is the closing quote,
	// opening at the session end (300) with no volume.
	intraday := []byte(`{"chart":{"result":[{"meta":{"exchangeTimezoneName":"America/New_York","dataGranularity":"1m","tradingPeriods":[[{"start":180,"end":300}]]},"timestamp":[180,240,300],"indicators":{"quote":[{"open":[1,2,3],"high":[1,2,3],"low":[1,2,3],"close":[1,2,3],"volume":[5,6,0]}]}}],"error":null}}`)
	c, err = parseChart(intraday, "1m")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bars) != 2 || c.Skipped != 1 || c.Bars[1].T != 240000 {
		t.Fatalf("closing quote kept as a bar: %+v", c)
	}
	// A future's Sunday evening, traded but outside the days listed: kept.
	sunday := []byte(`{"chart":{"result":[{"meta":{"dataGranularity":"1m","tradingPeriods":[[{"start":600,"end":900}]]},"timestamp":[480,540,600],"indicators":{"quote":[{"open":[1,2,3],"high":[1,2,3],"low":[1,2,3],"close":[1,2,3],"volume":[5,6,7]}]}}],"error":null}}`)
	if c, _ := parseChart(sunday, "1m"); len(c.Bars) != 3 {
		t.Fatalf("traded bars outside the sessions dropped: %+v", c)
	}
	if _, err := parseChart([]byte(`{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found, symbol may be delisted"}}}`), "1d"); err == nil {
		t.Fatal("a Yahoo error parsed")
	}
}
