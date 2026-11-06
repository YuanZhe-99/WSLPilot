// Command icon renders resources/icon.svg into resources/icon.png, the
// 1024×1024 app icon mygo build packages, and into the 64×64 icons of the
// sidebar (internal/gui/appicon.png) and the tray (cmd/wslpilot/tray.png). Run from the repository root:
//
//	go run ./tools/icon
//
// It draws the SVG with MyGo's own renderer in a headless Tester, once on
// black and once on white, and recovers each pixel's alpha from the
// difference, since the Tester's frames are opaque.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/egoist/mygo/ui"
)

const src = "resources/icon.svg"

// outputs are the files written and their sizes in pixels.
var outputs = []struct {
	path string
	size int
}{
	{"resources/icon.png", 1024},
	{"internal/gui/appicon.png", 64},
	{"cmd/wslpilot/tray.png", 64},
}

func main() {
	data, err := os.ReadFile(src)
	if err != nil {
		fail(err)
	}
	icon, err := ui.ParseSVG(data)
	if err != nil {
		fail(err)
	}
	for _, o := range outputs {
		write(icon, o.path, o.size)
	}
}

// write renders icon at size pixels into path.
func write(icon *ui.SVG, dst string, size int) {
	black := render(icon, ui.Hex("#000000"), size)
	white := render(icon, ui.Hex("#FFFFFF"), size)
	out := matte(black, white)
	f, err := os.Create(dst)
	if err != nil {
		fail(err)
	}
	if err := png.Encode(f, out); err != nil {
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
	fmt.Println("wrote", dst)
}

// render draws icon at size×size device pixels over a solid background.
func render(icon *ui.SVG, bg ui.Color, size int) *image.RGBA {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(bg).Children(func() {
			ui.Image(c, icon).Width(float32(size)).Height(float32(size))
		})
	}, size, size)
	tt.SetScale(1)
	tt.Frame()
	return tt.Image()
}

// matte recovers a straight-alpha image from renders over black and white:
// alpha = 1 - (white - black), color = black / alpha.
func matte(black, white *image.RGBA) *image.NRGBA {
	b := black.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			k := black.RGBAAt(x, y)
			w := white.RGBAAt(x, y)
			diff := (int(w.R) - int(k.R) + int(w.G) - int(k.G) + int(w.B) - int(k.B)) / 3
			a := 255 - diff
			if a <= 0 {
				continue
			}
			if a > 255 {
				a = 255
			}
			un := func(v uint8) uint8 { return uint8(min(255, int(v)*255/a)) }
			out.SetNRGBA(x, y, color.NRGBA{un(k.R), un(k.G), un(k.B), uint8(a)})
		}
	}
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "icon:", err)
	os.Exit(1)
}
