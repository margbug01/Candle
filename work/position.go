package main

import (
	"fmt"
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// The long and short position tools, after TradingView's: a box from the
// entry to the take-profit and to the stop-loss, which reads out how far
// each is and the risk/reward ratio. Its points are the entry, the
// take-profit and the stop-loss, all three at the box's left edge, and its
// right edge at the entry's price. One click places it.

const (
	positionBars   = 20   // how wide a new box is
	defaultAccount = 1000 // in what the market is priced in
	defaultRisk    = 1    // percent of the account
)

func account(d *drawing) float64 {
	if d.Account > 0 {
		return d.Account
	}
	return defaultAccount
}

func riskPct(d *drawing) float64 {
	if d.Risk > 0 {
		return d.Risk
	}
	return defaultRisk
}

// positionSize is how much to buy or sell for the stop-loss to lose the
// risk, that part of the account; false without a stop-loss away from the
// entry.
func positionSize(d *drawing) (float64, bool) {
	per := math.Abs(d.Pts[0].P - d.Pts[2].P)
	if per == 0 {
		return 0, false
	}
	return account(d) * riskPct(d) / 100 / per, true
}

// qtyText is a quantity to about four digits.
func qtyText(q float64) string {
	d := 2
	if q > 0 {
		d = min(max(3-int(math.Floor(math.Log10(q))), 0), 8)
	}
	return strconv.FormatFloat(q, 'f', d, 64)
}

func isPosition(kind string) bool { return kind == "long" || kind == "short" }

// position is a new box of kind entered at pt: the stop-loss a twentieth of
// the prices on screen away, the take-profit twice that on the other side.
func position(kind string, id int64, pt point, m mapper) drawing {
	risk := (m.sc.hi - m.sc.lo) * 0.05
	if kind == "short" {
		risk = -risk
	}
	end := point{T: m.timeAt(m.idx(pt.T) + positionBars), P: pt.P}
	return drawing{ID: id, Kind: kind, Width: 1, Pts: []point{pt, {T: pt.T, P: pt.P + 2*risk}, {T: pt.T, P: pt.P - risk}, end}}
}

// fixPosition keeps a box square after one of its points moved: the
// take-profit and the stop-loss at the entry's time, the right edge at its
// price, and each on its side of it.
func fixPosition(d *drawing) {
	e := d.Pts[0]
	d.Pts[1].T, d.Pts[2].T, d.Pts[3].P = e.T, e.T, e.P
	if d.Kind == "long" {
		d.Pts[1].P, d.Pts[2].P = max(d.Pts[1].P, e.P), min(d.Pts[2].P, e.P)
	} else {
		d.Pts[1].P, d.Pts[2].P = min(d.Pts[1].P, e.P), max(d.Pts[2].P, e.P)
	}
}

// riskReward is the reward over the risk; false without risk.
func riskReward(d *drawing) (float64, bool) {
	risk := math.Abs(d.Pts[0].P - d.Pts[2].P)
	if risk == 0 {
		return 0, false
	}
	return math.Abs(d.Pts[1].P-d.Pts[0].P) / risk, true
}

func ratioText(d *drawing) string {
	if r, ok := riskReward(d); ok {
		return fmt.Sprintf("盈亏比 %.2f", r)
	}
	return "盈亏比 —"
}

// drawPosition draws the box: the profit in green, the loss in red, the
// entry across them, and their labels beyond them.
func (a *application) drawPosition(p *ui.Painter, plot ui.Rect, m mapper, k palette, d *drawing) {
	at := func(pt point) (float32, float32) {
		x, y := m.xy(pt)
		return plot.X + x, plot.Y + y
	}
	x0, ye := at(d.Pts[0])
	_, yt := at(d.Pts[1])
	_, ys := at(d.Pts[2])
	x1, _ := at(d.Pts[3])
	l, r := min(x0, x1), max(x0, x1)
	box := func(y0, y1 float32) ui.Rect {
		return ui.Rect{X: l, Y: min(y0, y1), W: r - l, H: float32(math.Abs(float64(y1 - y0)))}
	}
	profit, loss := ui.Hex(barUp), ui.Hex(barDown)
	p.Fill(box(ye, yt), profit.Alpha(0.2), 0)
	p.Fill(box(ye, ys), loss.Alpha(0.2), 0)
	p.Line(l, ye, r, ye, 1, k.muted)

	entry := d.Pts[0].P
	dec := a.chartDecimals(entry)
	qty, sized := positionSize(d)
	// move is a price, how far it is from the entry, and what the position
	// makes or loses there.
	move := func(v float64) string {
		s := formatPrice(v, dec)
		if entry != 0 {
			s += fmt.Sprintf(" (%+.2f%%)", (v/entry-1)*100)
		}
		if sized {
			gain := (v - entry) * qty
			if d.Kind == "short" {
				gain = -gain
			}
			s += fmt.Sprintf("  %+.2f", gain)
		}
		return s
	}
	// tag puts text in a label of color c, centered at cx, its near edge at y
	// and going up (above) or down.
	cx := (l + r) / 2
	tag := func(text string, c ui.Color, y float32, above bool) {
		sp := span(text, 11.5, onColor(c), 500)
		w, h := p.MeasureText(0, sp)
		bw, bh := w+12, h+6
		by := y + 4
		if above {
			by = y - 4 - bh
		}
		p.Fill(ui.Rect{X: cx - bw/2, Y: by, W: bw, H: bh}, c, 3)
		p.RichText(cx-w/2, by+3, 0, sp)
	}
	tag("止盈 "+move(d.Pts[1].P), profit, yt, yt < ye)
	tag("止损 "+move(d.Pts[2].P), loss, ys, ys < ye)
	text := ratioText(d)
	if sized {
		text += "  数量 " + qtyText(qty)
	}
	sp := span(text, 11.5, k.onLabel, 600)
	w, h := p.MeasureText(0, sp)
	p.Fill(ui.Rect{X: cx - w/2 - 6, Y: ye - h/2 - 3, W: w + 12, H: h + 6}, k.label, 3)
	p.RichText(cx-w/2, ye-h/2, 0, sp)
}
