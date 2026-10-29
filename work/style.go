package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// style is how the chart looks, as the settings dialog sets it. The empty
// string is the default of each choice, and an unset color follows the theme.
type style struct {
	// 商品
	Type                                   string // "" candles, "hollow", "bars", "line", "area"
	Up, Down                               hexColor
	Body, Borders, Wick                    bool
	BorderUp, BorderDown, WickUp, WickDown hexColor // unset, the body's
	Ink                                    string   // up and down for labels, text and lines: "" the body's unless lost on the background, then the border's; "body"; "custom"
	InkUp, InkDown                         hexColor // Ink "custom"'s; unset, as for ""
	Line                                   hexColor // of line and area charts
	PrevClose                              bool     // a bar's color by its move from the last close
	Volume                                 bool
	Precision                              int    // decimals, -1 the market's
	Zone                                   string // IANA, "" this machine's

	// 状态行
	Title, Values, Change, VolumeValue bool
	Logo                               bool // the coin's icon before its name

	// 坐标
	Scale      string // "", "percent", "log"
	Buttons    string // the 自动 and 对数 switches: "" while the pointer is on the axis, "always", "never"
	NoOverlap  bool
	LastPrice  string // "" label and line, "value", "line", "none"
	IndValues  bool   // the indicators' newest values on the axes
	Countdown  bool
	HighLow    bool
	Weekday    bool
	DateFormat string // a Go layout
	Clock12    bool

	// 版面
	Theme                   string // "", "light", "dark"
	Background              hexColor
	VertGrid, HorzGrid      bool
	VertColor, HorzColor    hexColor
	Breaks                  bool     // a line where each day begins, on bars under a day
	BreakColor              hexColor // unset, the theme's
	BreakStyle              string   // "" dashed, "dotted", "solid"
	BreakWidth              int      // in pixels, 1–4
	BreakZone               string   // when a day begins: "" midnight in the chart's zone, "utc" midnight UTC, as Binance's daily bars
	Cross                   hexColor
	CrossStyle              string // "" dashed, "dotted", "solid"
	Watermark               bool
	TextSize                float64
	TextColor               hexColor
	TopMargin, BottomMargin float64 // percent of the plot left above and below the candles
	RightBars               float64 // room right of the newest bar, in bars
}

func defaultStyle() style {
	return style{
		Body: true, Borders: true, Wick: true, Volume: true, Precision: -1,
		Title: true, Values: true, Change: true, VolumeValue: true, Logo: true,
		NoOverlap: true, IndValues: true, Countdown: true, Weekday: true, DateFormat: "2006-01-02",
		VertGrid: true, HorzGrid: true, BreakWidth: 1, TextSize: 11, TopMargin: 8, BottomMargin: 22, RightBars: 6,
	}
}

// fix puts back what a damaged file leaves out of range.
func (st *style) fix() {
	d := defaultStyle()
	if st.TextSize < 9 || st.TextSize > 16 {
		st.TextSize = d.TextSize
	}
	if st.TopMargin < 0 || st.TopMargin > 40 {
		st.TopMargin = d.TopMargin
	}
	if st.BottomMargin < 0 || st.BottomMargin > 60 {
		st.BottomMargin = d.BottomMargin
	}
	if st.RightBars < 0 || st.RightBars > 200 {
		st.RightBars = d.RightBars
	}
	if st.Precision < -1 || st.Precision > 10 {
		st.Precision = -1
	}
	if st.DateFormat == "" {
		st.DateFormat = d.DateFormat
	}
	if st.BreakWidth < 1 || st.BreakWidth > 4 {
		st.BreakWidth = d.BreakWidth
	}
}

// band is the stretch of a plot's height, as fractions from its top, that
// the candles take.
func (st style) band() (top, bottom float32) {
	top = float32(st.TopMargin / 100)
	return top, max(1-float32(st.BottomMargin/100), top+0.1)
}

// hexColor is a color as settings.json keeps it, "#rrggbb" or "#rrggbbaa";
// unset, it is "".
type hexColor ui.Color

func (h hexColor) set() bool { return h.R != 0 || h.G != 0 || h.B != 0 || h.A != 0 }

// or is the color, or def while it is unset.
func (h hexColor) or(def ui.Color) ui.Color {
	if !h.set() {
		return def
	}
	return ui.Color(h)
}

func (h hexColor) MarshalText() ([]byte, error) {
	switch {
	case !h.set():
		return nil, nil
	case h.A == 255:
		return fmt.Appendf(nil, "#%02x%02x%02x", h.R, h.G, h.B), nil
	}
	return fmt.Appendf(nil, "#%02x%02x%02x%02x", h.R, h.G, h.B, h.A), nil
}

func (h *hexColor) UnmarshalText(b []byte) error {
	s := strings.TrimPrefix(string(b), "#")
	if s == "" {
		*h = hexColor{}
		return nil
	}
	if len(s) == 6 {
		s += "ff"
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 8 {
		return fmt.Errorf("color %q", b)
	}
	*h = hexColor(ui.RGBA(uint8(v>>24), uint8(v>>16), uint8(v>>8), float32(uint8(v))/255))
	return nil
}

var zones = map[string]*time.Location{}

// loc is the zone the chart tells time in.
func (st style) loc() *time.Location {
	if st.Zone == "" {
		return time.Local
	}
	if l, ok := zones[st.Zone]; ok {
		return l
	}
	l, err := time.LoadLocation(st.Zone)
	if err != nil {
		l = time.Local
	}
	zones[st.Zone] = l
	return l
}

// breakLoc is the zone whose midnight begins a day for the day lines.
func (st style) breakLoc() *time.Location {
	if st.BreakZone == "utc" {
		return time.UTC
	}
	return st.loc()
}

var weekdays = [...]string{"日", "一", "二", "三", "四", "五", "六"}

// crossTime is the full time of a bar ivMs long opening at ms, as the
// crosshair's label shows it.
func (st style) crossTime(ms, ivMs int64) string {
	t := time.UnixMilli(ms).In(st.loc())
	s := t.Format(st.DateFormat)
	if st.Weekday {
		s = "周" + weekdays[t.Weekday()] + " " + s
	}
	if ivMs < 86_400_000 {
		s += " " + clockText(t, st.Clock12)
	}
	return s
}

func clockText(t time.Time, twelve bool) string {
	if !twelve {
		return t.Format("15:04")
	}
	if t.Hour() < 12 {
		return "上午" + t.Format("3:04")
	}
	return "下午" + t.Format("3:04")
}

// applyTheme follows the theme setting. The tests swap it out: SetSource
// waits for a main loop the Tester does not have.
var applyTheme = func(key string) {
	src := mygo.ThemeSystem
	switch key {
	case "light":
		src = mygo.ThemeLight
	case "dark":
		src = mygo.ThemeDark
	}
	mygo.Theme.SetSource(src)
}

// option is one choice of a drop-down: what settings.json keeps, and what it reads.
type option struct{ key, label string }

var (
	inkModes    = []option{{"", "自动（太浅时换边框色）"}, {"body", "跟实体颜色"}, {"custom", "自定义"}}
	chartTypes  = []option{{"", "K线"}, {"hollow", "空心K线"}, {"bars", "美国线"}, {"line", "折线"}, {"area", "面积"}}
	scaleModes  = []option{{"", "常规"}, {"percent", "百分比"}, {"log", "对数"}}
	buttonModes = []option{{"", "鼠标移到价格坐标上时显示"}, {"always", "总是显示"}, {"never", "总是隐藏"}}
	lastPrices  = []option{{"", "价格标签和价格线"}, {"value", "只显示价格标签"}, {"line", "只显示价格线"}, {"none", "隐藏"}}
	clocks      = []option{{"", "24 小时"}, {"12", "12 小时"}}
	themes      = []option{{"", "跟随系统"}, {"light", "浅色"}, {"dark", "深色"}}
	crossStyles = []option{{"", "虚线"}, {"dotted", "点线"}, {"solid", "实线"}}
	lineWidths  = []option{{"1", "1 像素"}, {"2", "2 像素"}, {"3", "3 像素"}, {"4", "4 像素"}}
	textSizes   = []option{{"10", "10"}, {"11", "11"}, {"12", "12"}, {"13", "13"}, {"14", "14"}}
	timeZones   = []option{
		{"", "本机时区"}, {"UTC", "UTC"}, {"Asia/Shanghai", "上海"}, {"Asia/Tokyo", "东京"}, {"Asia/Singapore", "新加坡"},
		{"Europe/London", "伦敦"}, {"America/New_York", "纽约"}, {"America/Los_Angeles", "洛杉矶"},
	}
	precisions = func() []option {
		out := []option{{"-1", "默认（跟交易对）"}, {"0", "1"}}
		for d := 1; d <= 8; d++ {
			out = append(out, option{strconv.Itoa(d), "0." + strings.Repeat("0", d-1) + "1"})
		}
		return out
	}()
	dateFormats = func() []option {
		sample := time.Date(1997, 9, 29, 0, 0, 0, 0, time.UTC)
		var out []option
		for _, f := range []string{"2006-01-02", "2006/01/02", "2006年1月2日", "02/01/2006", "01/02/2006", "02.01.2006"} {
			out = append(out, option{f, sample.Format(f)})
		}
		return out
	}()
)

func labelOf(opts []option, key string) string {
	for _, o := range opts {
		if o.key == key {
			return o.label
		}
	}
	return opts[0].label
}
