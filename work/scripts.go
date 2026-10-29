package main

import (
	"cmp"
	"embed"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"candle/market"
	"candle/pine"
)

// The indicators written in Pine Script that Candle ships, after
// TradingView's own scripts: run by the same interpreter as the user's.

//go:embed indicators/*.pine
var shipped embed.FS

// shippedScripts are the scripts' files, the sections of the list they go
// in, and more words the search finds them by, in the order they list.
var shippedScripts = []struct{ file, category, alias string }{
	{"wma", "均线", "均线 加权 Weighted"},
	{"hma", "均线", "均线 赫尔 Hull"},
	{"vwma", "均线", "均线 成交量加权"},
	{"vwap", "成交量", "均价 Volume Weighted Average Price"},
	{"kc", "通道", "肯特纳 Keltner Channels"},
	{"donchian", "通道", "唐奇安 Donchian Channels 海龟"},
	{"ichimoku", "趋势", "一目 云图 Ichimoku Cloud"},
	{"sar", "趋势", "抛物线 停损 Parabolic SAR"},
	{"supertrend", "趋势", "超级趋势 SuperTrend"},
	{"dmi", "趋势", "ADX 趋向 Directional Movement Index"},
	{"aroon", "趋势", "阿隆 Aroon"},
	{"stochrsi", "震荡", "随机 RSI Stoch RSI"},
	{"cci", "震荡", "顺势 Commodity Channel Index"},
	{"wr", "震荡", "威廉 Williams %R WR"},
	{"roc", "震荡", "变动率 Rate of Change"},
	{"trix", "震荡", "三重 TRIX"},
	{"bias", "震荡", "乖离 BIAS"},
	{"atr", "波动", "真实波幅 Average True Range"},
	{"obv", "成交量", "能量潮 On Balance Volume"},
	{"mfi", "成交量", "资金流量 Money Flow Index"},
	{"cmf", "成交量", "资金流 Chaikin Money Flow"},
}

func init() {
	for _, sh := range shippedScripts {
		src, err := shipped.ReadFile(path.Join("indicators", sh.file+".pine"))
		if err != nil {
			panic(err)
		}
		s, err := pine.Compile(string(src))
		if err != nil {
			panic(fmt.Sprintf("%s.pine: %v", sh.file, err))
		}
		indKinds = append(indKinds, scriptKind(sh.file, s, sh.category, sh.alias))
	}
}

// sourceNames are pine.Sources as the settings name them.
var sourceNames = map[string]string{"close": "收盘价", "open": "开盘价", "high": "最高价", "low": "最低价",
	"hl2": "(高+低)/2", "hlc3": "(高+低+收)/3", "ohlc4": "(开+高+低+收)/4", "hlcc4": "(高+低+收+收)/4", "volume": "成交量"}

func pineSources() []option {
	out := make([]option, len(pine.Sources))
	for i, s := range pine.Sources {
		out[i] = option{strconv.Itoa(i), sourceNames[s]}
	}
	return out
}

// scriptKind is the kind of indicator s works out.
func scriptKind(key string, s *pine.Script, category, alias string) indKind {
	k := indKind{key: key, short: cmp.Or(s.Short, s.Title), name: s.Title, alias: alias, pane: !s.Overlay,
		category: category, script: s, decimals: -1}
	switch {
	case s.Format == "volume":
		k.decimals = -2
	case s.Precision >= 0:
		k.decimals = s.Precision
	case !s.Overlay && s.Format != "price":
		k.decimals = -3 // as many as the values need
	}
	for _, in := range s.Inputs {
		pd := paramDef{name: in.Title, def: in.Def, lo: -1e9, hi: 1e9, step: 1, tip: in.Tooltip}
		if !math.IsNaN(in.Min) {
			pd.lo = in.Min
		}
		if !math.IsNaN(in.Max) {
			pd.hi = in.Max
		}
		switch in.Kind {
		case "float":
			pd.step = 0.1
		case "bool":
			pd.kind = paramBool
		case "source":
			pd.kind, pd.opts = paramChoice, pineSources()
		case "string":
			pd.kind = paramFixed
			if len(in.Options) > 0 {
				pd.kind = paramChoice
				for i, o := range in.Options {
					pd.opts = append(pd.opts, option{strconv.Itoa(i), o})
				}
			}
		case "color":
			pd.kind = paramColor
		}
		if !math.IsNaN(in.Step) && in.Step > 0 {
			pd.step = in.Step
		}
		if len(in.NumOptions) > 0 {
			pd.kind = paramNumChoice
			for _, o := range in.NumOptions {
				t := strconv.FormatFloat(o, 'f', -1, 64)
				pd.opts = append(pd.opts, option{t, t})
			}
		}
		k.params = append(k.params, pd)
	}
	for _, p := range s.Plots {
		pd := plotDef{name: p.Title, style: plotLine, width: max(p.Width, 1), hidden: p.Hidden, dynamic: p.Dynamic}
		if !p.Dynamic {
			pd.color = packedHex(p.Color)
		}
		k.plots = append(k.plots, pd)
	}
	return k
}

// packedHex is a packed color as #rrggbbaa, "" for none.
func packedHex(c uint32) string {
	if c == 0 {
		return ""
	}
	return fmt.Sprintf("#%08x", c)
}

func packedColor(v float64) (ui8 [4]uint8, ok bool) {
	r, g, b, a, ok := pine.Unpack(v)
	return [4]uint8{r, g, b, a}, ok
}

var chicago, newYork = loadLoc("America/Chicago"), loadLoc("America/New_York")

func loadLoc(name string) *time.Location {
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	return time.UTC
}

// pineEnv is what the scripts know of the chart, and a key that changes
// with it: the exchange's clock and its trading days, as on TradingView,
// which starts a pair's day at midnight UTC.
func (a *application) pineEnv() (pine.Env, string) {
	iv, _ := intervalOf(a.interval)
	env := pine.Env{Tick: a.tick(a.symbol), Ticker: a.symbol, IntervalMs: iv.ms, Period: tvPeriod(iv.ms), Loc: time.UTC,
		DayOf: midnights(time.UTC)}
	if in, ok := market.InstrumentOf(a.symbol); ok {
		env.DayOf, env.Loc = in.SessionStart, newYork
		switch {
		case in.Loc != nil:
			env.Loc = in.Loc
		case in.Future:
			env.Loc = chicago
		}
	}
	if n := len(a.bars); n > 0 && iv.ms > 0 {
		env.LastClosed = time.Now().UnixMilli()+a.clock >= a.bars[n-1].T+iv.ms
	}
	return env, fmt.Sprint(a.symbol, iv.ms, env.Tick, env.LastClosed)
}

// tvPeriod is a bar size as TradingView's timeframe.period has it.
func tvPeriod(ms int64) string {
	switch {
	case ms >= 30*86_400_000:
		return "M"
	case ms >= 7*86_400_000:
		return "W"
	case ms >= 86_400_000:
		return "D"
	case ms < 60_000:
		return strconv.FormatInt(ms/1000, 10) + "S"
	}
	return strconv.FormatInt(ms/60_000, 10)
}

// isScriptKey reports whether key is a script of the user's.
func isScriptKey(key string) bool { return strings.HasPrefix(key, "pine:") }
