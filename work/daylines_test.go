package main

import (
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

func barsFrom(start time.Time, n int, iv time.Duration) []market.Bar {
	out := make([]market.Bar, n)
	for i := range out {
		out[i] = market.Bar{T: start.Add(time.Duration(i) * iv).UnixMilli(), O: 1, H: 1, L: 1, C: 1}
	}
	return out
}

func TestDayBreaks(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	hour := barsFrom(time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC), 30, time.Hour) // 20:00 UTC to 01:00 two days on
	for _, c := range []struct {
		name     string
		bars     []market.Bar
		from, to int
		iv       time.Duration
		loc      *time.Location
		want     []dayBreak
	}{
		{"hourly, UTC", hour, 0, 29, time.Hour, time.UTC, []dayBreak{{4, 0}, {28, 0}}},
		// Midnight in Shanghai is 16:00 UTC, which these bars reach on the second day.
		{"hourly, Shanghai", hour, 0, 29, time.Hour, sh, []dayBreak{{20, 0}}},
		// 12-hour bars open at 08:00 and 20:00 in Shanghai: midnight is 4 hours into the 20:00 one.
		{"12 hours, Shanghai", barsFrom(time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), 5, 12*time.Hour), 0, 4, 12 * time.Hour, sh,
			[]dayBreak{{1, 1. / 3}, {3, 1. / 3}}},
		// Only the bars in sight, and the one right of them: a day beginning in the last bar shows.
		{"in sight", hour, 5, 27, time.Hour, time.UTC, []dayBreak{{28, 0}}},
		{"the last bar in sight", hour, 0, 3, time.Hour, time.UTC, []dayBreak{{4, 0}}},
		// A gap in the data over midnight: the line goes at the bar after it.
		{"gap", append(barsFrom(time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC), 2, time.Hour),
			barsFrom(time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC), 2, time.Hour)...), 0, 3, time.Hour, time.UTC, []dayBreak{{2, 0}}},
	} {
		got := dayBreaks(c.bars, c.from, c.to, int64(c.iv/time.Millisecond), c.loc)
		if !slices.EqualFunc(got, c.want, func(a, b dayBreak) bool { return a.i == b.i && math.Abs(float64(a.frac-b.frac)) < 1e-4 }) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// lineColumns are the x of the columns of the plot at least half of whose
// height is c.
func lineColumns(img *image.RGBA, a *application, c ui.Color) (xs []int) {
	left, top := int(chartLeft(a)), titleBarHeight
	for x := left; x < left+int(a.plotW); x++ {
		n := 0
		for y := top; y < top+int(a.plotH); y++ {
			if p := img.RGBAAt(x, y); p.R == c.R && p.G == c.G && p.B == c.B && p.A == 255 {
				n++
			}
		}
		if n > int(a.plotH)/2 {
			xs = append(xs, x)
		}
	}
	return xs
}

// The day lines go on the left edge of the first bar of each day, in the
// chart's zone or UTC's, and only on bars under a day, when turned on.
func TestDayLines(t *testing.T) {
	a := testApp()
	st := &a.set.Chart
	col := ui.Hex("#ff00ff")
	st.BreakStyle, st.BreakColor, st.Zone = "solid", hexColor(col), "Asia/Shanghai"
	tt := newTester(a, true)
	if xs := lineColumns(tt.Image(), a, col); len(xs) != 0 {
		t.Fatalf("lines at %v while turned off", xs)
	}
	st.Breaks = true
	// where is where the days begin, at offset hours past midnight UTC.
	where := func(offset int64) (xs []int) {
		v := a.view
		n := len(a.bars)
		v.clamp(a.plotW, n)
		from, to := v.span(a.plotW, n)
		for i := from; i <= to; i++ {
			if (a.bars[i].T/3_600_000+offset)%24 == 0 {
				x := chartLeft(a) + v.x(i, a.plotW, n) - v.spacing/2
				if x >= chartLeft(a) && x <= chartLeft(a)+a.plotW-1 {
					xs = append(xs, int(math.Floor(float64(x))))
				}
			}
		}
		return xs
	}
	for _, c := range []struct {
		zone   string
		offset int64
	}{{"", 8}, {"utc", 0}} {
		st.BreakZone = c.zone
		tt.Frame()
		got, want := lineColumns(tt.Image(), a, col), where(c.offset)
		if len(want) < 2 || !slices.Equal(got, want) {
			t.Fatalf("day beginning %q: lines at %v, want %v", c.zone, got, want)
		}
	}
	shot(t, tt, "day-lines")
	a.open("BTCUSDT", "1d")
	a.loading, a.bars, a.atStart, a.stream = false, fakeBars(300, 86_400_000), true, live
	tt.Frame()
	tt.Frame()
	if xs := lineColumns(tt.Image(), a, col); len(xs) != 0 {
		t.Fatalf("lines at %v on daily bars", xs)
	}
}

// The settings: the lines on with their color, style and width, and when
// a day begins, as the dialog sets them.
func TestDayLineSettings(t *testing.T) {
	a := testApp()
	a.set.Chart.Zone = "Asia/Shanghai"
	tt := newTester(a, true)
	openSettings(t, tt, "版面")
	if tt.HasText("一天的起点") {
		t.Fatal("when a day begins shows with the lines off")
	}
	click(t, tt, "交易日分隔线")
	click(t, tt, "1 像素")
	click(t, tt, "3 像素")
	click(t, tt, "图表时区 0 点")
	click(t, tt, "UTC 0 点（图表时间 08:00，币安日线开盘）")
	click(t, tt, "确认")
	if st := a.set.Chart; !st.Breaks || st.BreakWidth != 3 || st.BreakZone != "utc" {
		t.Fatalf("after 确认: on %v, width %d, day from %q", st.Breaks, st.BreakWidth, st.BreakZone)
	}
}
