//go:build ignore

// Macicon draws the app icon of the macOS build from its SVG, on a Mac:
//
//	go run tools/macicon.go docs/icon-macos.svg work/assets/icon-macos.png
//
// Quick Look draws SVG with WebKit but onto white. This draws it once over
// black and once over white and works the transparency back out of the two:
// a pixel's alpha is 1 − (over white − over black), its color over black
// divided by its alpha.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const size = 1024

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run tools/macicon.go <icon.svg> <icon.png>")
		os.Exit(2)
	}
	svg, err := os.ReadFile(os.Args[1])
	check(err)
	tmp, err := os.MkdirTemp("", "macicon")
	check(err)
	defer os.RemoveAll(tmp)
	black := render(tmp, string(svg), "black")
	white := render(tmp, string(svg), "white")
	out := image.NewNRGBA(black.Bounds())
	for y := range size {
		for x := range size {
			br, bg, bb, _ := black.At(x, y).RGBA()
			wr, wg, wb, _ := white.At(x, y).RGBA()
			a := 65535 - (int(wr-br)+int(wg-bg)+int(wb-bb))/3
			if a <= 0 {
				continue
			}
			a = min(a, 65535)
			c := func(v uint32) uint8 { return uint8(min(int(v)*255/a, 255)) }
			out.SetNRGBA(x, y, color.NRGBA{c(br), c(bg), c(bb), uint8(a >> 8)})
		}
	}
	f, err := os.Create(os.Args[2])
	check(err)
	check((&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, out))
	check(f.Close())
}

// render draws the SVG over a background of the color with Quick Look.
func render(dir, svg, bg string) image.Image {
	dir = filepath.Join(dir, bg)
	check(os.Mkdir(dir, 0o755))
	under := fmt.Sprintf("</defs>\n  <rect width=%q height=%q fill=%q/>", fmt.Sprint(size), fmt.Sprint(size), bg)
	file := filepath.Join(dir, "icon.svg")
	check(os.WriteFile(file, []byte(strings.Replace(svg, "</defs>", under, 1)), 0o644))
	cmd := exec.Command("qlmanage", "-t", "-s", fmt.Sprint(size), "-o", dir, file)
	if out, err := cmd.CombinedOutput(); err != nil {
		check(fmt.Errorf("qlmanage: %v\n%s", err, out))
	}
	f, err := os.Open(file + ".png")
	check(err)
	defer f.Close()
	img, err := png.Decode(f)
	check(err)
	if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
		check(fmt.Errorf("qlmanage drew %v, not %d×%d", b.Size(), size, size))
	}
	return img
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
