package pine

import (
	"fmt"
	"math"
	"time"

	"candle/market"
)

// Script is a compiled indicator: what it declares, and its code.
type Script struct {
	Title, Short string
	Overlay      bool
	Precision    int    // decimals of its values, -1 as the chart's
	Format       string // "inherit", "price", "volume" or "percent"
	Version      int
	Inputs       []Input
	Plots        []Plot
	Hlines       []Hline
	Fills        []Fill
	Bgs          []Bg
	Shapes       []Shape
	Warnings     []*Error // what Candle skips

	src      string
	declared bool
	run      []func(*Machine)
	carry    []bool // by buffer: a var's, which a bar starts with as the last one ended
	nstr     int
}

// Input is a setting the script asks for. Its value is a number: a bool's
// 1 or 0, a source's or a string's place in Sources or Options, a color
// packed as Pack does.
type Input struct {
	Kind           string // "int", "float", "bool", "source", "string", "color"
	Title          string
	Def            float64
	Min, Max, Step float64 // NaN when not given
	Options        []string
	NumOptions     []float64
	Fixed          string // a string input without options: not to be set
	Tooltip        string
}

// Sources are what input.source picks from.
var Sources = []string{"close", "open", "high", "low", "hl2", "hlc3", "ohlc4", "hlcc4", "volume"}

// Plot styles.
const (
	StyleLine = iota
	StyleLineBr
	StyleStep
	StyleHistogram
	StyleColumns
	StyleCircles
	StyleCross
	StyleArea
	StyleAreaBr
	StyleStepBr
)

// Plot is a series the script draws.
type Plot struct {
	Title    string
	Color    uint32 // the color it starts with, when it has one color
	Dynamic  bool   // its color changes from bar to bar
	Style    int
	Width    float64
	Histbase float64
	Hidden   bool // display.none: worked out, not drawn
}

// Line styles.
const (
	LineSolid = iota
	LineDashed
	LineDotted
)

// Hline is a level across the pane.
type Hline struct {
	Title string
	Color uint32
	Style int
	Width float64
}

// Fill shades the space between two plots, or two levels.
type Fill struct {
	Title  string
	A, B   int
	Levels bool // A and B are hlines
}

// Bg colors the bars' background.
type Bg struct{ Title string }

// Shape kinds, locations and sizes.
const (
	ShapeTriangleUp = iota
	ShapeTriangleDown
	ShapeCircle
	ShapeCross
	ShapeXCross
	ShapeArrowUp
	ShapeArrowDown
	ShapeLabelUp
	ShapeLabelDown
	ShapeSquare
	ShapeDiamond
	ShapeFlag
	ShapeChar
)

const (
	LocAboveBar = iota
	LocBelowBar
	LocTop
	LocBottom
	LocAbsolute
)

// Shape marks bars where its series holds, as plotshape, plotchar and
// plotarrow do.
type Shape struct {
	Title     string
	Style     int
	Char      string
	Location  int
	Size      int // 0 auto, 1 tiny … 5 huge
	Text      string
	TextColor uint32
	Arrow     bool // plotarrow's: up below the bar when positive, down above it when negative
}

// Env is what a script knows of the chart it runs on.
type Env struct {
	Tick       float64 // syminfo.mintick
	Ticker     string
	IntervalMs int64
	Period     string // timeframe.period: "1", "60", "240", "D", "W"
	Loc        *time.Location
	DayOf      func(t int64) int64 // which trading day the bar opening at t is in
	LastClosed bool                // whether the newest bar is complete
}

// Machine runs a script on bars and holds what it worked out.
type Machine struct {
	Plots      [][]float64 // by plot then bar, NaN for none
	PlotColors [][]float64 // packed colors, NaN for none: not drawn
	PlotOffset []int       // bars to the right each plot is drawn
	PlotWidth  []float64
	HlinePrice []float64
	FillColors [][]float64
	BgColors   [][]float64
	ShapeVals  [][]float64
	ShapeCols  [][]float64
	ShapeOff   []int
	Err        error // what stopped it

	s      *Script
	bars   []market.Bar
	env    Env
	inputs []float64
	i, n   int
	buf    [][]float64
	str    []string
	jump   int
}

// Source is the script's text.
func (s *Script) Source() string { return s.src }

// Bars is how many bars the machine has run on.
func (m *Machine) Bars() int { return m.n }

// RunError is a problem met running a script, on a bar.
type RunError struct {
	Bar int
	Msg string
}

func (e *RunError) Error() string { return e.Msg }

const loopLimit = 200_000

func (m *Machine) loop(n int) {
	if n >= loopLimit {
		panic(&RunError{Bar: m.i, Msg: fmt.Sprintf("循环跑了 %d 次还没停，Candle 把它停下了", loopLimit)})
	}
}

// Run works the script out on bars with the inputs given, from bar from on:
// the bars before it must be those m ran on last. A nil machine, or one of
// another script, starts again from the first bar.
func (s *Script) Run(m *Machine, bars []market.Bar, inputs []float64, from int, env Env) (out *Machine) {
	if m == nil || m.s != s || m.Err != nil || from > m.n {
		m, from = &Machine{s: s}, 0
	}
	out = m
	n := len(bars)
	m.bars, m.env, m.inputs, m.n = bars, env, s.fitInputs(inputs), n
	if env.Loc == nil {
		m.env.Loc = time.UTC
	}
	if from == 0 {
		m.str = make([]string, s.nstr)
	}
	grow := func(b []float64) []float64 {
		if cap(b) < n {
			g := make([]float64, n, n+n/4+64)
			copy(g, b[:min(from, len(b))])
			return g
		}
		return b[:n]
	}
	growAll := func(bs [][]float64, k int) [][]float64 {
		if len(bs) != k {
			bs = make([][]float64, k)
		}
		for j := range bs {
			bs[j] = grow(bs[j])
		}
		return bs
	}
	m.buf = growAll(m.buf, len(s.carry))
	m.Plots = growAll(m.Plots, len(s.Plots))
	m.PlotColors = growAll(m.PlotColors, len(s.Plots))
	m.FillColors = growAll(m.FillColors, len(s.Fills))
	m.BgColors = growAll(m.BgColors, len(s.Bgs))
	m.ShapeVals = growAll(m.ShapeVals, len(s.Shapes))
	m.ShapeCols = growAll(m.ShapeCols, len(s.Shapes))
	if len(m.PlotOffset) != len(s.Plots) {
		m.PlotOffset, m.PlotWidth = make([]int, len(s.Plots)), make([]float64, len(s.Plots))
		for j, p := range s.Plots {
			m.PlotWidth[j] = p.Width
		}
	}
	if len(m.HlinePrice) != len(s.Hlines) {
		m.HlinePrice = make([]float64, len(s.Hlines))
	}
	if len(m.ShapeOff) != len(s.Shapes) {
		m.ShapeOff = make([]int, len(s.Shapes))
	}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*RunError); ok {
				m.Err = e
				return
			}
			m.Err = &RunError{Bar: m.i, Msg: fmt.Sprint("脚本出错：", r)}
		}
	}()
	for i := from; i < n; i++ {
		m.i = i
		for b, carry := range s.carry {
			if carry && i > 0 {
				m.buf[b][i] = m.buf[b][i-1]
			} else {
				m.buf[b][i] = math.NaN()
			}
		}
		for _, out := range [][][]float64{m.Plots, m.PlotColors, m.FillColors, m.BgColors, m.ShapeVals, m.ShapeCols} {
			for _, o := range out {
				o[i] = math.NaN()
			}
		}
		m.jump = 0
		runBlock(m, s.run)
	}
	return m
}

// fitInputs is inputs as many as the script has, the defaults filling in.
func (s *Script) fitInputs(in []float64) []float64 {
	out := make([]float64, len(s.Inputs))
	for j, d := range s.Inputs {
		out[j] = d.Def
		if j < len(in) && !math.IsNaN(in[j]) {
			out[j] = in[j]
		}
	}
	return out
}

// Source is the series bar values key names.
func (m *Machine) source(key string) float64 {
	b := m.bars[m.i]
	return sourceOf(b, key)
}

func sourceOf(b market.Bar, key string) float64 {
	switch key {
	case "open":
		return b.O
	case "high":
		return b.H
	case "low":
		return b.L
	case "hl2":
		return (b.H + b.L) / 2
	case "hlc3":
		return (b.H + b.L + b.C) / 3
	case "ohlc4":
		return (b.O + b.H + b.L + b.C) / 4
	case "hlcc4":
		return (b.H + b.L + 2*b.C) / 4
	case "volume":
		return b.V
	}
	return b.C
}

// Pack is a color as scripts keep it.
func Pack(r, g, b, a uint8) float64 {
	return float64(uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a))
}

// Unpack is a packed color's channels; ok is false for na.
func Unpack(v float64) (r, g, b, a uint8, ok bool) {
	if math.IsNaN(v) || v < 0 || v > math.MaxUint32 {
		return 0, 0, 0, 0, false
	}
	u := uint32(v)
	return uint8(u >> 24), uint8(u >> 16), uint8(u >> 8), uint8(u), true
}

// Truth is whether a value counts as true: neither 0 nor na.
func Truth(v float64) bool { return truth(v) }

// Bar is bar i the machine ran on.
func (m *Machine) Bar(i int) market.Bar { return m.bars[i] }
