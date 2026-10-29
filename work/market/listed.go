package market

import (
	"slices"
	"strings"
	"time"
)

// The markets beyond the coins, the CME's and New York's: Shanghai's and
// Shenzhen's A shares and indices, and Hong Kong's, from Eastmoney, which
// has them as they trade (Yahoo has them 30 and 15 minutes late); Korea's,
// and Cboe's VIX, from Yahoo, 20 and 15 minutes late (Eastmoney has no VIX,
// and its Korean minutes carry no volume).
//
// Candle names them by exchange, as TradingView does: SSE:600795 (国电电力),
// SSE:000001 (上证指数), SZSE:000001 (平安银行), HKEX:00700, HKEX:HSI,
// KRX:000660, CBOE:VIX.

// The exchanges' clocks.
var (
	shanghai = zone("Asia/Shanghai", 8)
	hongKong = zone("Asia/Hong_Kong", 8)
	seoul    = zone("Asia/Seoul", 9)
)

func zone(name string, hours int) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone(name, hours*3600)
	}
	return l
}

func hm(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

// Indices are the indices Candle offers by name: found by their Chinese
// names and pinyin, as the futures are. Ticker is the source's code for them.
var Indices = []Instrument{
	{ID: "CBOE:VIX", Code: "VIX", Name: "恐慌指数", Pinyin: "kong huang zhi shu", Ticker: "^VIX", Tick: 0.01, Index: true,
		Loc: newYork, Opens: hm(3, 0), Delay: 15 * time.Minute, Venue: "Cboe"},
	cnIndex("SSE", "000001", "上证指数", "shang zheng zhi shu"),
	cnIndex("SZSE", "399001", "深证成指", "shen zheng cheng zhi"),
	cnIndex("SZSE", "399006", "创业板指", "chuang ye ban zhi"),
	cnIndex("SSE", "000300", "沪深300", "hu shen"),
	cnIndex("SSE", "000688", "科创50", "ke chuang"),
	hkIndex("HSI", "100.HSI", "恒生指数", "heng sheng zhi shu"),
	hkIndex("HSTECH", "124.HSTECH", "恒生科技指数", "heng sheng ke ji zhi shu"),
	hkIndex("HSCEI", "100.HSCEI", "国企指数", "guo qi zhi shu"),
}

func cnIndex(ex, code, name, pinyin string) Instrument {
	in := cnStock(ex, code)
	in.Name, in.Pinyin, in.Index = name, pinyin, true
	return in
}

func hkIndex(code, secid, name, pinyin string) Instrument {
	in := hkStock(code)
	in.Ticker, in.Name, in.Pinyin, in.Index, in.Tick = secid, name, pinyin, true, 0.01
	return in
}

// cnStock is an A share, or an index, of Shanghai (SSE) or Shenzhen (SZSE):
// Eastmoney's 1.600795 or 0.000001.
func cnStock(ex, code string) Instrument {
	in := Instrument{ID: ex + ":" + code, Code: code, Ticker: "1." + code, Tick: 0.01, Loc: shanghai, Opens: hm(9, 30), Venue: "上交所"}
	if ex == "SZSE" {
		in.Ticker, in.Venue = "0."+code, "深交所"
	}
	return in
}

// hkStock is a Hong Kong share, Eastmoney's 116.00700.
func hkStock(code string) Instrument {
	return Instrument{ID: "HKEX:" + code, Code: code, Ticker: "116." + code, Tick: 0.001, Loc: hongKong, Opens: hm(9, 30), Venue: "港交所"}
}

// krStock is a share of Korea's main board, Yahoo's 000660.KS. Its prices
// step by won.
func krStock(code string) Instrument {
	return Instrument{ID: "KRX:" + code, Code: code, Ticker: code + ".KS", Tick: 1, Loc: seoul, Opens: hm(9, 0),
		Delay: 20 * time.Minute, Venue: "韩国交易所"}
}

// listedOf is the market id names among these.
func listedOf(id string) (Instrument, bool) {
	if i := slices.IndexFunc(Indices, func(in Instrument) bool { return in.ID == id }); i >= 0 {
		return Indices[i], true
	}
	ex, code, _ := strings.Cut(id, ":")
	switch {
	case ex == "SSE" && digits(code, 6), ex == "SZSE" && digits(code, 6) && strings.ContainsAny(code[:1], "0123"):
		return cnStock(ex, code), true
	case ex == "HKEX" && digits(code, 5):
		return hkStock(code), true
	case ex == "KRX" && digits(code, 6):
		return krStock(code), true
	}
	return Instrument{}, false
}

func digits(s string, n int) bool {
	return len(s) == n && !strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
}

// IsEastmoney is whether Candle's name for a market is one Eastmoney has
// the bars of: an A share's, a Hong Kong share's, or one of their indices.
func IsEastmoney(id string) bool {
	return strings.HasPrefix(id, "SSE:") || strings.HasPrefix(id, "SZSE:") || strings.HasPrefix(id, "HKEX:")
}

// IsListed is whether a market is one of an exchange's, a future, a stock
// or an index, rather than Binance's.
func IsListed(id string) bool { return IsYahoo(id) || IsEastmoney(id) }

// session is when a market trades on a day, from its open: the morning's and
// the afternoon's, as times of day on its exchange's clock.
type session struct{ open, close time.Duration }

// sessions are the parts of in's trading day: A shares break for lunch from
// 11:30 to 13:00, Hong Kong's from 12:00.
func (in Instrument) sessions() []session {
	switch {
	case in.Loc == shanghai:
		return []session{{hm(9, 30), hm(11, 30)}, {hm(13, 0), hm(15, 0)}}
	case in.Loc == hongKong:
		return []session{{hm(9, 30), hm(12, 0)}, {hm(13, 0), hm(16, 0)}}
	}
	return nil
}

// hourStarts are the times of day in's hour bars begin: an hour from each
// part's open, the last of a part shorter (Hong Kong's 11:30, which ends at
// noon); every fourth of them begins a 4-hour bar.
func (in Instrument) hourStarts(n int) []time.Duration {
	var out []time.Duration
	for _, s := range in.sessions() {
		for t := s.open; t < s.close; t += time.Hour {
			out = append(out, t)
		}
	}
	var every []time.Duration
	for i := 0; i < len(out); i += n {
		every = append(every, out[i])
	}
	return every
}
