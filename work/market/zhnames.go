package market

// ZhNames are the Chinese names, and their pinyin a syllable a word, of
// coins and of what Binance's TradFi perpetuals trade other than US stocks
// (those go by Popular's names and Eastmoney's): Korean and Hong Kong
// shares, metals, oil, the companies not yet listed. The search finds the
// pairs and perpetuals of a code by these names: 比特币 BTCUSDT, 海力士
// SKHYNIXUSDT.P. The first name is the one shown.
var ZhNames = map[string]struct{ Zh, Py string }{
	// Coins.
	"BTC":  {"比特币", "bi te bi"},
	"ETH":  {"以太坊", "yi tai fang"},
	"BNB":  {"币安币", "bi an bi"},
	"SOL":  {"索拉纳", "suo la na"},
	"XRP":  {"瑞波币", "rui bo bi"},
	"DOGE": {"狗狗币", "gou gou bi"},
	"ADA":  {"艾达币", "ai da bi"},
	"TRX":  {"波场", "bo chang"},
	"LTC":  {"莱特币", "lai te bi"},
	"DOT":  {"波卡", "bo ka"},
	"SHIB": {"柴犬币", "chai quan bi"},
	"BCH":  {"比特币现金", "bi te bi xian jin"},
	"ETC":  {"以太经典", "yi tai jing dian"},
	"AVAX": {"雪崩", "xue beng"},
	"FIL":  {"文件币", "wen jian bi"},

	// Metals and energy.
	"XAU":    {"黄金", "huang jin"},
	"XAG":    {"白银", "bai yin"},
	"XPT":    {"铂金", "bo jin"},
	"XPD":    {"钯金", "ba jin"},
	"COPPER": {"铜", "tong"},
	"CL":     {"WTI原油 原油", "yuan you"},
	"BZ":     {"布伦特原油 原油", "bu lun te yuan you"},
	"NATGAS": {"天然气", "tian ran qi"},

	// Korean shares.
	"SKHYNIX":       {"SK海力士 海力士", "hai li shi"},
	"SAMSUNG":       {"三星电子 三星", "san xing dian zi"},
	"SAMSUNGEM":     {"三星电机", "san xing dian ji"},
	"HYUNDAI":       {"现代汽车", "xian dai qi che"},
	"HANMI":         {"韩美半导体", "han mei ban dao ti"},
	"LGELECTRONICS": {"LG电子", "dian zi"},
	"KODEX200":      {"韩国200ETF", "han guo"},
	"CSOPSKHYNIX2L": {"二倍做多海力士", "er bei zuo duo hai li shi"},
	"CSOPSAMSUNG2L": {"二倍做多三星", "er bei zuo duo san xing"},

	// Hong Kong's, and China's.
	"HK0700":   {"腾讯控股 腾讯", "teng xun kong gu"},
	"TENCENT":  {"腾讯", "teng xun"},
	"HK1810":   {"小米集团 小米", "xiao mi ji tuan"},
	"HK0992":   {"联想集团 联想", "lian xiang ji tuan"},
	"HK0625":   {"希音", "xi yin"},
	"BYD":      {"比亚迪", "bi ya di"},
	"MEITUAN":  {"美团", "mei tuan"},
	"KUAISHOU": {"快手", "kuai shou"},
	"POPMART":  {"泡泡玛特", "pao pao ma te"},
	"GIGADEV":  {"兆易创新", "zhao yi chuang xin"},
	"ZHONGJI":  {"中际旭创", "zhong ji xu chuang"},
	"MINIMAX":  {"稀宇科技 MiniMax", "xi yu ke ji"},
	"ZHIPU":    {"智谱", "zhi pu"},
	"CXMT":     {"长鑫存储 长鑫", "chang xin cun chu"},
	"UNITREE":  {"宇树科技 宇树", "yu shu ke ji"},
	"MOONSHOT": {"月之暗面", "yue zhi an mian"},
}
