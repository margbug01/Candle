package market

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"
)

// Eastmoney's search answers with its markets, the US stocks among them by
// their Chinese names; Candle takes the US stocks, as Yahoo names them.
func TestEastmoneySuggest(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		asked = q.Get("input") + " " + q.Get("type") + " " + q.Get("token") + " " + q.Get("count")
		w.Write([]byte(`{"QuotationCodeTable":{"Data":[
			{"Code":"000660","Name":"SK海力士","PinYin":"SKHLS","MktNum":"177","SecurityTypeName":"韩股"},
			{"Code":"SKHY","Name":"SK海力士","PinYin":"SKHLS","MktNum":"105","SecurityTypeName":"美股"},
			{"Code":"BRK_B","Name":"伯克希尔哈撒韦-B","PinYin":"BKXEHSWB","MktNum":"106","SecurityTypeName":"美股"},
			{"Code":"BITB","Name":"Bitwise Bitcoin ETF","PinYin":"BITWISEBITCOINETF","MktNum":"107","SecurityTypeName":"美股"},
			{"Code":"600519","Name":"贵州茅台","PinYin":"GZMT","MktNum":"1","SecurityTypeName":"沪A"},
			{"Code":"BKNVDA","Name":"英伟达概念","PinYin":"YWDGN","MktNum":"90","SecurityTypeName":"板块"},
			{"Code":"BAD$","Name":"坏代码","MktNum":"105"},
			{"Code":"X","Name":"没有市场","MktNum":""}
		],"Status":0,"Message":"成功","TotalCount":7}}`))
	}))
	defer srv.Close()
	e := Eastmoney{HTTP: srv.Client(), Base: srv.URL}
	got, err := e.Suggest(context.Background(), "海力士", 30)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "海力士 14 "+eastmoneyToken+" 30" {
		t.Fatalf("asked %q", asked)
	}
	if len(got) != 7 || got[0] != (Suggestion{Code: "000660", Name: "SK海力士", Initials: "SKHLS", Market: 177, Kind: "韩股"}) {
		t.Fatalf("suggestions: %+v", got)
	}
	us := USStocks(got)
	var ids, names []string
	for _, in := range us {
		ids = append(ids, in.ID)
		names = append(names, in.Alias+"|"+in.Name)
		if in.Code != in.Ticker || in.Tick != 0.01 || in.ID != "US:"+in.Code {
			t.Errorf("%+v", in)
		}
	}
	if want := []string{"US:SKHY", "US:BRK-B", "US:BITB"}; !slices.Equal(ids, want) {
		t.Fatalf("US stocks: %v, want %v", ids, want)
	}
	if want := []string{"SK海力士|", "伯克希尔哈撒韦-B|", "|Bitwise Bitcoin ETF"}; !slices.Equal(names, want) {
		t.Fatalf("names: %q", names)
	}
}

func TestEastmoneyFails(t *testing.T) {
	for _, body := range []string{`{"QuotationCodeTable":{"Data":null,"Status":-1,"Message":"token"}}`, `<html>`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		e := Eastmoney{HTTP: srv.Client(), Base: srv.URL}
		if got, err := e.Suggest(context.Background(), "闪迪", 30); err == nil {
			t.Errorf("%s: %v, no error", body, got)
		}
		srv.Close()
	}
}

func TestHasHan(t *testing.T) {
	for s, want := range map[string]bool{"闪迪": true, "SK海力士": true, "sndk": false, "": false, "ＡＢ": false, "，": false} {
		if HasHan(s) != want {
			t.Errorf("%q: %v", s, !want)
		}
	}
}

// Eastmoney itself, with CANDLE_LIVE=1: 闪迪 is SNDK, 海力士 finds SKHY.
func TestEastmoneyLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE=1 asks Eastmoney")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	e := Eastmoney{}
	for q, want := range map[string]string{"闪迪": "US:SNDK", "海力士": "US:SKHY", "英伟达": "US:NVDA", "伯克希尔": "US:BRK-B"} {
		got, err := e.Suggest(ctx, q, 30)
		if err != nil {
			t.Fatal(err)
		}
		us := USStocks(got)
		i := slices.IndexFunc(us, func(in Instrument) bool { return in.ID == want })
		if i < 0 {
			t.Errorf("%s: %+v, no %s", q, us, want)
			continue
		}
		t.Logf("%s: %s %s (%d of %d)", q, us[i].ID, us[i].Alias, i+1, len(us))
	}
}
