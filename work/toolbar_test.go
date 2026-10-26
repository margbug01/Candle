package main

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Every bar size shows its label on one line, whole, chosen or not: not
// wrapped onto two lines (27 pixels tall), not cut to an ellipsis (2).
func TestIntervalLabelsStayWhole(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	check := func(when string) {
		for _, iv := range intervals {
			r, ok := tt.Find(iv.label)
			if !ok {
				t.Fatalf("no %s button", iv.label)
			}
			if box := inkBox(tt.Image(), r); box[1] < 8 || box[1] > 16 {
				t.Errorf("%s: %s takes %dx%d pixels", when, iv.label, box[0], box[1])
			}
		}
	}
	check("at the start")
	for _, iv := range intervals {
		click(t, tt, iv.label)
		check(iv.label + " chosen")
	}
}

// inkBox is the width and height of what r holds besides its background.
func inkBox(img *image.RGBA, r ui.Rect) [2]int {
	x0, y0, x1, y1 := int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H)
	bg := img.RGBAAt(x0+3, (y0+y1)/2)
	diff := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	left, right, top, bottom := x1, x0-1, y1, y0-1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if c := img.RGBAAt(x, y); diff(c.R, bg.R)+diff(c.G, bg.G)+diff(c.B, bg.B) > 90 {
				left, right, top, bottom = min(left, x), max(right, x), min(top, y), max(bottom, y)
			}
		}
	}
	return [2]int{max(right-left+1, 0), max(bottom-top+1, 0)}
}
