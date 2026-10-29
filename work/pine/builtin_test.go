package pine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"candle/market"
)

func testBars(n int) []market.Bar {
	bars := make([]market.Bar, n)
	p := 100.0
	for i := range bars {
		o := p
		c := o * (1 + math.Sin(float64(i)*0.7)*0.01 + math.Cos(float64(i)*0.31)*0.006)
		bars[i] = market.Bar{T: int64(i) * 3_600_000, O: o, C: c, H: math.Max(o, c) * 1.004, L: math.Min(o, c) * 0.995, V: 100 + float64(i%7)*30}
		p = c
	}
	return bars
}

// Every indicator Candle ships compiles and gives values.
func TestShippedIndicatorsRun(t *testing.T) {
	files, _ := filepath.Glob("../indicators/*.pine")
	if len(files) < 21 {
		t.Fatalf("found %d indicators", len(files))
	}
	bars := testBars(300)
	for _, f := range files {
		src, _ := os.ReadFile(f)
		s, err := Compile(string(src))
		if err != nil {
			e := err.(*Error)
			t.Errorf("%s:%d: %v", f, Line(string(src), e.Start), err)
			continue
		}
		m := s.Run(nil, bars, nil, 0, Env{IntervalMs: 3_600_000, Period: "60"})
		if m.Err != nil {
			t.Errorf("%s: %v", f, m.Err)
			continue
		}
		for j, p := range s.Plots {
			if p.Hidden {
				continue
			}
			if v := m.Plots[j][len(bars)-1]; math.IsNaN(v) && !(filepath.Base(f) == "vwap.pine" && j > 0) && filepath.Base(f) != "supertrend.pine" {
				t.Errorf("%s: %s has no value on the last bar", f, p.Title)
			}
		}
		if len(s.Warnings) > 0 {
			t.Errorf("%s: warnings %v", f, s.Warnings[0])
		}
	}
}

// Running the newest bar again as it changes, and each new one as it
// comes, gives what running every bar from the start does.
func TestIncrementalRunsMatchFullOnes(t *testing.T) {
	files, _ := filepath.Glob("../indicators/*.pine")
	all := testBars(260)
	env := Env{IntervalMs: 3_600_000, Period: "60", DayOf: func(t int64) int64 { return t / 86_400_000 }}
	for _, f := range files {
		src, _ := os.ReadFile(f)
		s, err := Compile(string(src))
		if err != nil {
			t.Fatal(err)
		}
		var m *Machine
		for n := 200; n <= len(all); n++ {
			// The newest bar first arrives half formed, then complete.
			partial := append([]market.Bar(nil), all[:n]...)
			last := &partial[n-1]
			last.C = (last.O + last.C) / 2
			last.H, last.L = math.Max(last.O, last.C)*1.001, math.Min(last.O, last.C)*0.999
			from := 0
			if m != nil {
				from = n - 2 // the bar before was half formed: it is run again
			}
			m = s.Run(m, partial, nil, from, env)
			m = s.Run(m, all[:n], nil, n-1, env)
		}
		full := s.Run(nil, all, nil, 0, env)
		for j := range s.Plots {
			for i := range all {
				a, b := m.Plots[j][i], full.Plots[j][i]
				if !(a == b || math.IsNaN(a) && math.IsNaN(b)) {
					t.Fatalf("%s: %s bar %d: %v run bar by bar, %v at once", f, s.Plots[j].Title, i, a, b)
				}
			}
		}
	}
}
