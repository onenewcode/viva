//go:build vivahead && darwin

package headless

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// A label has the same size in logical pixels at every display scale. At a
// fractional one its bitmap used to be drawn at scale/ceil(scale) of its size.
func TestLabelKeepsItsLogicalSizeAtFractionalScales(t *testing.T) {
	width := func(scale float64) float64 {
		session, err := Open(app.Config{Title: "label", Width: 300, Height: 60, Paint: func(canvas *ui.Canvas, size ui.Size) {
			canvas.Label("Hello web-01 你好", ui.Rect{X: 10, Y: 10, Width: 280, Height: 40},
				ui.TextStyle{Size: 13, Weight: ui.WeightRegular, Color: ui.Color{Red: 1, Green: 1, Blue: 1, Alpha: 1}})
		}}, nil, Options{Scale: scale})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		path := filepath.Join(t.TempDir(), "label.png")
		if err := session.Screenshot(path); err != nil {
			t.Fatal(err)
		}
		picture := readPNG(t, path)
		left, right := picture.Bounds().Max.X, 0
		for y := 0; y < picture.Bounds().Max.Y; y++ {
			for x := 0; x < picture.Bounds().Max.X; x++ {
				if r, _, _, _ := picture.At(x, y).RGBA(); r > 0x8000 {
					left, right = min(left, x), max(right, x)
				}
			}
		}
		if right < left {
			t.Fatalf("scale %g drew no text", scale)
		}
		return float64(right-left+1) / scale
	}
	want := width(1)
	for _, scale := range []float64{1.25, 1.5, 1.75, 2} {
		if got := width(scale); got < want-3 || got > want+3 {
			t.Errorf("text is %.1f logical pixels wide at scale %g, %.1f at scale 1", got, scale, want)
		}
	}
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	picture, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	return picture
}
