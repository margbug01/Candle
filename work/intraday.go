package main

import (
	"context"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// intraday is a day's minutes (分时图), as Chinese apps show them when a
// day's candle is double-clicked: the price minute by minute, its average,
// the close the day before and the volume.
type intraday struct {
	title    string
	day      market.Bar
	prev     float64 // the close the day before, 0 unknown
	minutes  int     // the minutes a whole day trades, 0 unknown
	decimals int
	bars     []market.Bar
	iv       string // the bars' size: "1m", or "5m" for days whose minutes are gone
	loading  bool
	err      string
	stop     context.CancelFunc
}

// openIntraday opens the minutes of the day whose candle is at x, y on a
// chart of days, reporting whether one is there: a double click beside the
// candles goes back to the newest bar, as it does in TradingView.
func (a *application) openIntraday(x, y float32) bool {
	n := len(a.bars)
	if a.interval != "1d" || y >= a.plotH || !a.mapReady() {
		return false
	}
	v := a.view
	v.clamp(a.plotW, n)
	i := int(math.Round(float64(v.index(x, a.plotW, n))))
	if i < 0 || i >= n {
		return false
	}
	b := a.bars[i]
	const slack = 4 // DIPs above and below the wick that still hit it
	if y < a.shown.y(b.H, a.axis)-slack || y > a.shown.y(b.L, a.axis)+slack {
		return false
	}
	a.closeIntraday()
	base, _ := a.names(a.symbol)
	at := time.UnixMilli(b.T).In(a.set.Chart.loc())
	d := &intraday{
		title: base + a.after(a.symbol) + " 分时 · " + at.Format("2006-01-02") + " 周" + weekdays[at.Weekday()],
		day:   b, decimals: a.chartDecimals(b.C), loading: true,
	}
	if i > 0 {
		d.prev = a.bars[i-1].C
	}
	from, open, to, minutes := dayWindow(a.symbol, b.T)
	d.minutes = minutes
	ctx, stop := context.WithCancel(context.Background())
	d.stop = stop
	a.intra, a.drag = d, dragNone
	feed, sym := a.barFeed(a.symbol), a.symbol
	a.spawn(func() {
		bars, iv, err := dayBars(ctx, feed, sym, from, open, to)
		a.post(func() {
			if a.intra != d {
				return
			}
			d.loading, d.bars, d.iv = false, bars, iv
			if err != nil {
				d.err = shorten(err.Error(), 160)
			}
		})
	})
	return true
}

// dayWindow is when the bars of sym's day opening at t open, from to to,
// when it opens and how many minutes it trades (0 unknown). A future's day
// begins the evening before; the auction before a market opens belongs to
// its day. Only Binance trades round the clock.
func dayWindow(sym string, t int64) (from, open, to int64, minutes int) {
	in, ok := market.InstrumentOf(sym)
	if !ok {
		return t, t, t + 86_400_000, 1440
	}
	open = in.SessionStart(t)
	return open - 10*60_000, open, open + 86_400_000, in.Minutes()
}

func (a *application) closeIntraday() {
	if a.intra != nil {
		a.intra.stop()
		a.intra = nil
	}
}

// dayBars are the bars opening in [from, to): of a minute where the feed
// still has all of them, else of 5 minutes (Eastmoney keeps a day of
// minutes, Yahoo a month); none where it has neither.
func dayBars(ctx context.Context, feed market.Feed, sym string, from, open, to int64) ([]market.Bar, string, error) {
	for _, iv := range []string{"1m", "5m"} {
		bars, whole, err := barsIn(ctx, feed, sym, iv, from, open, to)
		if err != nil {
			return nil, "", err
		}
		if whole && len(bars) > 0 {
			return bars, iv, nil
		}
	}
	return nil, "", nil
}

// barsIn pages back from to until it reaches open, a page of a thousand at
// a time (a day of Binance's minutes takes two), and keeps the bars from
// from to to. whole is whether the feed's bars go back to open, so that none
// of the day's are missing: Eastmoney's minutes begin at today's.
// shortcut: a market's first day reads as missing when it opens late; fine while nobody looks at listing days.
func barsIn(ctx context.Context, feed market.Feed, sym, iv string, from, open, to int64) (bars []market.Bar, whole bool, err error) {
	end := to - 1
	if to > time.Now().UnixMilli() {
		end = 0 // the day is still on
	}
	for range 4 {
		got, err := feed.Klines(ctx, sym, iv, end, 1000)
		if err != nil {
			return nil, false, err
		}
		bars = append(got, bars...)
		if len(got) > 0 && got[0].T <= open {
			whole = true
			break
		}
		if len(got) < 1000 {
			break
		}
		end = got[0].T - 1
	}
	out := bars[:0]
	for _, b := range bars {
		if b.T >= from && b.T < to {
			out = append(out, b)
		}
	}
	return out, whole, nil
}

func (a *application) intradayDialog(c *ui.Context, t *ui.Theme, k palette) {
	d := a.intra
	if d == nil {
		return
	}
	open := true
	ww, wh := c.Size()
	ui.DialogBase(c.Key("intraday"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(min(880, ww-60)).Height(min(580, wh-40)).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label(d.title)
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			dialogHeader(c, t, k, d.title, "关闭分时", func() { open = false })
			ui.RichText(c, d.summary(k)...).SingleLine().Padding(0, 24, 10, 24)
			box := ui.Box(c.Key("intraday-chart")).FillWidth().Grow(1).MinHeight(0).Margin(0, 16, 16, 16)
			hx, hy, over := box.PointerPosition()
			loc := a.set.Chart.loc()
			box.Draw(func(p *ui.Painter, r ui.Rect) { d.draw(p, r, k, loc, hx, hy, over) })
			msg, detail := "", d.err
			switch {
			case d.loading:
				msg = "正在加载分时…"
			case d.err != "":
				msg = "分时没加载上"
			case len(d.bars) == 0:
				msg, detail = "这一天的分时拿不到", "数据源只留最近一段时间的分钟线（东方财富约一个半月、Yahoo 两个月），更早的日子没有"
			}
			if msg != "" {
				box.Children(func() {
					ui.Column(c).FillWidth().FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
						ui.Text(c, msg).FontSize(14).FontWeight(600)
						if detail != "" {
							ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).MaxWidth(520).TextAlign(ui.Center)
						}
					})
				})
			}
		})
	})
	if !open { // the close button, Escape or a click beside it
		a.closeIntraday()
	}
}

// summary is the day's candle read out, and the size of the bars below.
func (d *intraday) summary(k palette) []ui.Span {
	b, f := d.day, func(v float64) string { return formatPrice(v, d.decimals) }
	ref := d.prev
	if ref <= 0 {
		ref = b.O
	}
	col := k.move(b.C - ref)
	out := []ui.Span{
		span("开 ", 13, k.muted, 400), span(f(b.O)+"   ", 13, col, 400),
		span("高 ", 13, k.muted, 400), span(f(b.H)+"   ", 13, col, 400),
		span("低 ", 13, k.muted, 400), span(f(b.L)+"   ", 13, col, 400),
		span("收 ", 13, k.muted, 400), span(f(b.C)+"   ", 13, col, 400),
	}
	if d.prev > 0 {
		out = append(out, span(formatSigned(b.C-d.prev, d.decimals)+" ("+pctText((b.C/d.prev-1)*100, 2)+")   ", 13, col, 400))
	}
	out = append(out, span("量 ", 13, k.muted, 400), span(formatVolume(b.V), 13, k.text, 400))
	switch d.iv {
	case "1m":
		out = append(out, span("   · 1 分钟", 13, k.muted, 400))
	case "5m":
		out = append(out, span("   · 5 分钟（更细的已经没有了）", 13, k.muted, 400))
	}
	return out
}

// draw draws the minutes in r: the prices spread evenly above and below the
// close the day before, the volume under them, the hours the market is shut
// (a lunch break, a night) left out. A day still on fills only its part.
func (d *intraday) draw(p *ui.Painter, r ui.Rect, k palette, loc *time.Location, hx, hy float32, over bool) {
	n := len(d.bars)
	if n == 0 {
		return
	}
	s := p.Scale()
	px := 1 / s
	mid := func(v float32) float32 { return (float32(math.Floor(float64(v*s))) + 0.5) / s }
	const sideW, bottomH, gap = 66, 22, 8
	plot := ui.Rect{X: r.X + sideW, Y: r.Y, W: r.W - 2*sideW, H: r.H - bottomH}
	if plot.W <= 0 || plot.H <= 0 {
		return
	}
	priceH := plot.H * 0.72
	volTop, volH := plot.Y+priceH+gap, plot.H-priceH-gap
	ivMs := int64(60_000)
	if d.iv == "5m" {
		ivMs = 300_000
	}
	slots := max(n, int(int64(d.minutes)*60_000/ivMs))
	slotW := plot.W / float32(slots)
	x := func(i int) float32 { return plot.X + (float32(i)+0.5)*slotW }

	ref := d.prev
	if ref <= 0 {
		ref = d.bars[0].O
	}
	avg := make([]float64, n) // the volume-weighted average since the open (均价)
	var pv, vol, vmax, far float64
	for i, b := range d.bars {
		pv, vol, vmax = pv+(b.H+b.L+b.C)/3*b.V, vol+b.V, max(vmax, b.V)
		avg[i] = b.C
		if vol > 0 {
			avg[i] = pv / vol
		}
		far = max(far, math.Abs(b.C-ref), math.Abs(avg[i]-ref))
	}
	if far == 0 {
		far = ref * 0.01
	}
	far *= 1.08
	y := func(v float64) float32 { return plot.Y + float32((ref+far-v)/(2*far))*priceH }

	outline(p, ui.Rect{X: plot.X, Y: plot.Y, W: plot.W, H: priceH}, px, k.line)
	outline(p, ui.Rect{X: plot.X, Y: volTop, W: plot.W, H: volH}, px, k.line)
	for _, f := range []float64{1, 0.5, 0, -0.5, -1} {
		v := ref + f*far
		yy := mid(y(v))
		if f == 0 {
			dash(p, plot.X, yy, plot.X+plot.W, yy, px, k.muted, "")
		} else {
			p.Line(plot.X, yy, plot.X+plot.W, yy, px, k.grid)
		}
		ly := min(max(yy, plot.Y+8), plot.Y+priceH-8)
		col := k.muted
		if f != 0 {
			col = k.move(f)
		}
		ps := span(formatPrice(v, d.decimals), 11, col, 400)
		w, _ := p.MeasureText(0, ps)
		label(p, plot.X-6-w, ly, ps, alignStart)
		label(p, plot.X+plot.W+6, ly, span(pctText((v/ref-1)*100, 2), 11, col, 400), alignStart)
	}

	bw := max(px, slotW*0.6)
	for i, b := range d.bars {
		before := ref
		if i > 0 {
			before = d.bars[i-1].C
		}
		col := k.up
		if b.C < before {
			col = k.down
		}
		if h := float32(b.V/max(vmax, 1e-12)) * volH; h > 0 {
			p.Fill(ui.Rect{X: x(i) - bw/2, Y: volTop + volH - h, W: bw, H: h}, col.Alpha(0.7), 0)
		}
	}
	var line, mean ui.Path
	for i, b := range d.bars {
		if i == 0 {
			line.MoveTo(x(i), y(b.C))
			mean.MoveTo(x(i), y(avg[i]))
		} else {
			line.LineTo(x(i), y(b.C))
			mean.LineTo(x(i), y(avg[i]))
		}
	}
	p.StrokePath(&mean, 1.2, k.amber)
	p.StrokePath(&line, 1.5, k.accent)

	// The open, the middle of the day and the close, when it is in.
	clock := func(t int64) string { return time.UnixMilli(t).In(loc).Format("15:04") }
	ty := plot.Y + plot.H + bottomH/2
	label(p, plot.X, ty, span(clock(d.bars[0].T), 11, k.muted, 400), alignStart)
	if m := slots / 2; m < n {
		label(p, x(m), ty, span(clock(d.bars[m].T), 11, k.muted, 400), alignCenter)
	}
	if n == slots {
		es := span(clock(d.bars[n-1].T+ivMs), 11, k.muted, 400)
		w, _ := p.MeasureText(0, es)
		label(p, plot.X+plot.W-w, ty, es, alignStart)
	}

	// The bar under the pointer, read out over the prices.
	if !over || hx < plot.X-r.X || hx >= plot.X-r.X+plot.W || hy >= plot.H {
		return
	}
	i := min(int((hx-(plot.X-r.X))/slotW), n-1)
	b := d.bars[i]
	xx := mid(x(i))
	dash(p, xx, plot.Y, xx, plot.Y+plot.H, px, k.cross, "")
	p.Fill(ui.Rect{X: xx - 2, Y: y(b.C) - 2, W: 4, H: 4}, k.accent, 2)
	label(p, plot.X+8, plot.Y+12, span(clock(b.T)+"   价 "+formatPrice(b.C, d.decimals)+"   均 "+formatPrice(avg[i], d.decimals)+"   "+
		pctText((b.C/ref-1)*100, 2)+"   量 "+formatVolume(b.V), 12, k.text, 400), alignStart)
}
