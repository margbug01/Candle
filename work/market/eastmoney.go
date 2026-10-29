package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Eastmoney's search is what the box of its site suggests as one types: the
// stocks of every market it quotes, by code, by Chinese name (闪迪 is SNDK,
// SK海力士 SKHY), or by the initials of a name's pinyin (ywd). Its US stocks
// carry the Chinese names people in China know them by, which Nasdaq's list
// lacks; its A shares and Hong Kong's are there for when Candle shows them.
// The token is the site's own, the same for everyone. It answers from China
// without a proxy, in a tenth of a second.
type Eastmoney struct {
	HTTP *http.Client
	Base string // "" Eastmoney's
}

const (
	eastmoneyBase  = "https://searchapi.eastmoney.com"
	eastmoneyToken = "D43BF722C8E33BDC906FB84D85E326E8"
)

// A Suggestion is a market Eastmoney's search found.
type Suggestion struct {
	Code     string // as Eastmoney writes it: "SNDK", "BRK_B", "600519", "00700"
	Name     string // "闪迪"; some foreign funds go by their English names
	Initials string // of the name's pinyin: "SD"
	Market   int    // Eastmoney's: 105 Nasdaq, 106 NYSE, 107 NYSE American and Arca, 1 Shanghai, 0 Shenzhen, 116 Hong Kong
	Kind     string // "美股", "沪A", "港股"…
}

// Suggest asks Eastmoney's search for q, the best first.
func (e *Eastmoney) Suggest(ctx context.Context, q string, count int) ([]Suggestion, error) {
	base := e.Base
	if base == "" {
		base = eastmoneyBase
	}
	v := url.Values{"input": {q}, "type": {"14"}, "token": {eastmoneyToken}, "count": {strconv.Itoa(count)}}
	body, err := get(ctx, e.HTTP, base+"/api/suggest/get?"+v.Encode())
	if err != nil {
		return nil, fmt.Errorf("eastmoney search: %w", err)
	}
	var r struct {
		QuotationCodeTable struct {
			Data []struct {
				Code, Name, PinYin, MktNum, SecurityTypeName string
			}
			Status  int
			Message string
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("eastmoney search: %w", err)
	}
	if t := r.QuotationCodeTable; t.Status != 0 {
		return nil, fmt.Errorf("eastmoney search: %d %s", t.Status, t.Message)
	}
	var out []Suggestion
	for _, d := range r.QuotationCodeTable.Data {
		m, err := strconv.Atoi(d.MktNum)
		if err != nil || d.Code == "" {
			continue
		}
		out = append(out, Suggestion{Code: d.Code, Name: strings.TrimSpace(d.Name), Initials: d.PinYin, Market: m, Kind: d.SecurityTypeName})
	}
	return out, nil
}

// USStocks are the US stocks among s as Candle names them ("US:BRK-B": Yahoo
// writes the class after a dash, Eastmoney after an underscore), in s's
// order, their Chinese names as Alias.
func USStocks(s []Suggestion) []Instrument {
	var out []Instrument
	for _, x := range s {
		if x.Market != 105 && x.Market != 106 && x.Market != 107 {
			continue
		}
		code := strings.NewReplacer("_", "-", ".", "-").Replace(x.Code)
		if !usCode(strings.ReplaceAll(code, "-", ".")) {
			continue
		}
		in := Instrument{ID: "US:" + code, Code: code, Ticker: code, Tick: 0.01}
		if HasHan(x.Name) {
			in.Alias = x.Name
		} else {
			in.Name = x.Name
		}
		out = append(out, in)
	}
	return out
}

// HasHan is whether s holds a Chinese character, as a name or a query does
// that is not a code.
func HasHan(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}
