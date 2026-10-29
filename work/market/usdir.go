package market

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// The search finds US stocks and ETFs on the computer, without asking anyone
// as one types: Nasdaq lists every security traded on its market, NYSE's,
// Arca's and Cboe's listings too, about 11,000 once warrants, units, rights
// and preferred shares are left out. The list is a megabyte, published every
// day; Candle keeps it a week.
const usListURL = "https://www.nasdaqtrader.com/dynamic/SymDir/nasdaqtraded.txt"

// USList downloads Nasdaq's list, for ParseUSList.
func USList(ctx context.Context, c *http.Client) ([]byte, error) {
	body, err := get(ctx, c, usListURL)
	if err != nil {
		return nil, fmt.Errorf("nasdaq list: %w", err)
	}
	if _, err := ParseUSList(body); err != nil {
		return nil, fmt.Errorf("nasdaq list: %w", err)
	}
	return body, nil
}

// ParseUSList reads Nasdaq's list into markets as Candle names them
// ("US:BRK-B": Yahoo writes the class after a dash), with the popular ones
// marked (Popular), and adds the popular ones it lacks.
func ParseUSList(data []byte) ([]Instrument, error) {
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) < 2 || !bytes.HasPrefix(lines[0], []byte("Nasdaq Traded|Symbol|Security Name|")) {
		return nil, errors.New("not Nasdaq's list of symbols")
	}
	seen := map[string]bool{}
	var out []Instrument
	for _, l := range lines[1:] {
		f := strings.Split(strings.TrimRight(string(l), "\r"), "|")
		if len(f) < 8 || f[7] == "Y" { // a test issue
			continue
		}
		code, name := f[1], f[2]
		if !usCode(code) || other(code, name) {
			continue
		}
		code = strings.ReplaceAll(code, ".", "-")
		if seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, Instrument{ID: "US:" + code, Code: code, Ticker: code, Tick: 0.01, Name: shortName(name)})
	}
	if len(out) < 1000 {
		return nil, fmt.Errorf("only %d symbols", len(out))
	}
	return WithPopular(out), nil
}

// WithPopular marks the popular markets in list and adds those it lacks.
func WithPopular(list []Instrument) []Instrument {
	at := map[string]int{}
	for i, in := range list {
		at[in.Code] = i
	}
	for r, p := range Popular {
		i, ok := at[p.Code]
		if !ok {
			list = append(list, Instrument{ID: "US:" + p.Code, Code: p.Code, Ticker: p.Code, Tick: 0.01})
			i = len(list) - 1
		}
		list[i].Alias, list[i].Rank = p.Zh, r+1
	}
	return list
}

// usCode is whether code is a stock's or an ETF's, BRK.B's class and all,
// not a preferred share's (ABR$D) or a when-issued one's.
func usCode(code string) bool {
	if code == "" || strings.Count(code, ".") > 1 {
		return false
	}
	return !strings.ContainsFunc(code, func(r rune) bool { return (r < 'A' || r > 'Z') && r != '.' })
}

// other is whether a security is a warrant, a unit or a right, by its code
// (ACME.W, ACME.U, ACME.RT) or its name: things the search would bury the
// stocks under.
func other(code, name string) bool {
	if _, class, ok := strings.Cut(code, "."); ok && strings.ContainsAny(class[:1], "WUR") {
		return true
	}
	w := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == ' ' || r == ',' || r == '-' })
	if len(w) == 0 {
		return true
	}
	for _, x := range w {
		if x == "warrant" || x == "warrants" || x == "unit" || x == "units" {
			return true
		}
	}
	return w[len(w)-1] == "right" || w[len(w)-1] == "rights"
}

// shortName drops what Nasdaq's names say after the company: "Meta
// Platforms, Inc. - Class A Common Stock" is "Meta Platforms, Inc.".
func shortName(name string) string {
	if i := strings.Index(name, " - "); i > 0 {
		name = name[:i]
	}
	for _, s := range []string{" New Common", " Common Stock", " Common stock", " Common Shares", " Ordinary Shares", " Ordinary Share", " American Depositary", " American depositary", " Capital Stock"} {
		if i := strings.Index(name, s); i > 0 {
			name = name[:i]
		}
	}
	return strings.TrimRight(strings.Join(strings.Fields(name), " "), " ,")
}

// Popular are the US stocks and ETFs looked up most, by people in China not
// least, with the Chinese names they go by: the search puts them first among
// as good matches, and finds them by those names.
var Popular = []struct{ Code, Zh string }{
	{"NVDA", "英伟达"}, {"TSLA", "特斯拉"}, {"AAPL", "苹果"}, {"SPY", "标普500ETF"}, {"QQQ", "纳指100ETF"},
	{"MSFT", "微软"}, {"AMZN", "亚马逊"}, {"META", "脸书 Meta"}, {"GOOGL", "谷歌"}, {"GOOG", "谷歌C"},
	{"AMD", "超威半导体"}, {"AVGO", "博通"}, {"TSM", "台积电"}, {"PLTR", "帕兰提尔"}, {"MSTR", "微策略"},
	{"COIN", "Coinbase"}, {"NFLX", "奈飞"}, {"BABA", "阿里巴巴"}, {"PDD", "拼多多"}, {"JD", "京东"},
	{"BIDU", "百度"}, {"NIO", "蔚来"}, {"LI", "理想汽车"}, {"XPEV", "小鹏汽车"}, {"BILI", "哔哩哔哩 B站"},
	{"NTES", "网易"}, {"TME", "腾讯音乐"}, {"FUTU", "富途"}, {"TCOM", "携程"}, {"BEKE", "贝壳"},
	{"ZTO", "中通快递"}, {"YUMC", "百胜中国"}, {"BRK-B", "伯克希尔"}, {"JPM", "摩根大通"}, {"V", "维萨 Visa"},
	{"MA", "万事达"}, {"LLY", "礼来"}, {"UNH", "联合健康"}, {"XOM", "埃克森美孚"}, {"WMT", "沃尔玛"},
	{"COST", "好市多 Costco"}, {"INTC", "英特尔"}, {"ORCL", "甲骨文"}, {"QCOM", "高通"}, {"MU", "美光"},
	{"ARM", "安谋"}, {"SMCI", "超微电脑"}, {"ASML", "阿斯麦"}, {"CRM", "赛富时"}, {"ADBE", "奥多比"},
	{"HOOD", "罗宾汉"}, {"UBER", "优步"}, {"ABNB", "爱彼迎"}, {"DIS", "迪士尼"}, {"NKE", "耐克"},
	{"KO", "可口可乐"}, {"PEP", "百事"}, {"MCD", "麦当劳"}, {"SBUX", "星巴克"}, {"BA", "波音"},
	{"GS", "高盛"}, {"MS", "摩根士丹利"}, {"BAC", "美国银行"}, {"C", "花旗"}, {"PYPL", "贝宝 PayPal"},
	{"F", "福特"}, {"GM", "通用汽车"}, {"RIVN", "Rivian"}, {"SOFI", "SoFi"}, {"CRWD", "CrowdStrike"},
	{"VOO", "先锋标普500ETF"}, {"IWM", "罗素2000ETF"}, {"DIA", "道指ETF"}, {"TQQQ", "三倍做多纳指"}, {"SQQQ", "三倍做空纳指"},
	{"SOXL", "三倍做多半导体"}, {"SOXS", "三倍做空半导体"}, {"SMH", "半导体ETF"}, {"ARKK", "方舟创新ETF"}, {"GLD", "黄金ETF"},
	{"SLV", "白银ETF"}, {"TLT", "20年美债ETF"}, {"UVXY", "恐慌指数ETF"}, {"KWEB", "中概互联网ETF"}, {"FXI", "中国大盘股ETF"},
	{"YINN", "三倍做多中国"}, {"IBIT", "比特币ETF"}, {"ETHA", "以太坊ETF"}, {"XLK", "科技ETF"}, {"XLF", "金融ETF"},
	{"XLE", "能源ETF"},
}

// PopularOnly is the popular markets alone, for the search until the list
// is in.
func PopularOnly() []Instrument { return WithPopular(nil) }
