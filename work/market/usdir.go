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
		list[i].Alias, list[i].Pinyin, list[i].Rank = p.Zh, p.Py, r+1
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
// least, with the Chinese names they go by and those names' pinyin, a
// syllable a word: the search puts them first among as good matches, and
// finds them by those names, typed or spelled (ywd, yingweida: 英伟达).
var Popular = []struct{ Code, Zh, Py string }{
	{"NVDA", "英伟达", "ying wei da"}, {"TSLA", "特斯拉", "te si la"}, {"AAPL", "苹果", "ping guo"}, {"SPY", "标普500ETF", "biao pu"}, {"QQQ", "纳指100ETF", "na zhi"},
	{"MSFT", "微软", "wei ruan"}, {"AMZN", "亚马逊", "ya ma xun"}, {"META", "脸书 Meta", "lian shu"}, {"GOOGL", "谷歌", "gu ge"}, {"GOOG", "谷歌C", "gu ge"},
	{"AMD", "超威半导体", "chao wei ban dao ti"}, {"AVGO", "博通", "bo tong"}, {"TSM", "台积电", "tai ji dian"}, {"PLTR", "帕兰提尔", "pa lan ti er"}, {"MSTR", "微策略", "wei ce lve"},
	{"COIN", "Coinbase", ""}, {"NFLX", "奈飞", "nai fei"}, {"BABA", "阿里巴巴", "a li ba ba"}, {"PDD", "拼多多", "pin duo duo"}, {"JD", "京东", "jing dong"},
	{"BIDU", "百度", "bai du"}, {"NIO", "蔚来", "wei lai"}, {"LI", "理想汽车", "li xiang qi che"}, {"XPEV", "小鹏汽车", "xiao peng qi che"}, {"BILI", "哔哩哔哩 B站", "bi li bi li"},
	{"NTES", "网易", "wang yi"}, {"TME", "腾讯音乐", "teng xun yin yue"}, {"FUTU", "富途", "fu tu"}, {"TCOM", "携程", "xie cheng"}, {"BEKE", "贝壳", "bei ke"},
	{"ZTO", "中通快递", "zhong tong kuai di"}, {"YUMC", "百胜中国", "bai sheng zhong guo"}, {"BRK-B", "伯克希尔", "bo ke xi er"}, {"JPM", "摩根大通", "mo gen da tong"}, {"V", "维萨 Visa", "wei sa"},
	{"MA", "万事达", "wan shi da"}, {"LLY", "礼来", "li lai"}, {"UNH", "联合健康", "lian he jian kang"}, {"XOM", "埃克森美孚", "ai ke sen mei fu"}, {"WMT", "沃尔玛", "wo er ma"},
	{"COST", "好市多 Costco", "hao shi duo"}, {"INTC", "英特尔", "ying te er"}, {"ORCL", "甲骨文", "jia gu wen"}, {"QCOM", "高通", "gao tong"}, {"MU", "美光", "mei guang"},
	{"ARM", "安谋", "an mou"}, {"SMCI", "超微电脑", "chao wei dian nao"}, {"ASML", "阿斯麦", "a si mai"}, {"CRM", "赛富时", "sai fu shi"}, {"ADBE", "奥多比", "ao duo bi"},
	{"HOOD", "罗宾汉", "luo bin han"}, {"UBER", "优步", "you bu"}, {"ABNB", "爱彼迎", "ai bi ying"}, {"DIS", "迪士尼", "di shi ni"}, {"NKE", "耐克", "nai ke"},
	{"KO", "可口可乐", "ke kou ke le"}, {"PEP", "百事", "bai shi"}, {"MCD", "麦当劳", "mai dang lao"}, {"SBUX", "星巴克", "xing ba ke"}, {"BA", "波音", "bo yin"},
	{"GS", "高盛", "gao sheng"}, {"MS", "摩根士丹利", "mo gen shi dan li"}, {"BAC", "美国银行", "mei guo yin hang"}, {"C", "花旗", "hua qi"}, {"PYPL", "贝宝 PayPal", "bei bao"},
	{"F", "福特", "fu te"}, {"GM", "通用汽车", "tong yong qi che"}, {"RIVN", "Rivian", ""}, {"SOFI", "SoFi", ""}, {"CRWD", "CrowdStrike", ""},
	{"VOO", "先锋标普500ETF", "xian feng biao pu"}, {"IWM", "罗素2000ETF", "luo su"}, {"DIA", "道指ETF", "dao zhi"}, {"TQQQ", "三倍做多纳指", "san bei zuo duo na zhi"}, {"SQQQ", "三倍做空纳指", "san bei zuo kong na zhi"},
	{"SOXL", "三倍做多半导体", "san bei zuo duo ban dao ti"}, {"SOXS", "三倍做空半导体", "san bei zuo kong ban dao ti"}, {"SMH", "半导体ETF", "ban dao ti"}, {"ARKK", "方舟创新ETF", "fang zhou chuang xin"}, {"GLD", "黄金ETF", "huang jin"},
	{"SLV", "白银ETF", "bai yin"}, {"TLT", "20年美债ETF", "nian mei zhai"}, {"UVXY", "恐慌指数ETF", "kong huang zhi shu"}, {"KWEB", "中概互联网ETF", "zhong gai hu lian wang"}, {"FXI", "中国大盘股ETF", "zhong guo da pan gu"},
	{"YINN", "三倍做多中国", "san bei zuo duo zhong guo"}, {"IBIT", "比特币ETF", "bi te bi"}, {"ETHA", "以太坊ETF", "yi tai fang"}, {"XLK", "科技ETF", "ke ji"}, {"XLF", "金融ETF", "jin rong"},
	{"XLE", "能源ETF", "neng yuan"}, {"SNDK", "闪迪", "shan di"}, {"SKHY", "SK海力士 海力士", "hai li shi"}, {"WDC", "西部数据", "xi bu shu ju"}, {"STX", "希捷科技 希捷", "xi jie ke ji"},
}

// PopularOnly is the popular markets alone, for the search until the list
// is in.
func PopularOnly() []Instrument { return WithPopular(nil) }
