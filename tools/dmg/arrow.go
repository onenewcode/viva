package dmg

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
)

// The picture behind the icons when a project supplies none: a plain light
// ground with an arrow from the application to the Applications shortcut. The
// Finder draws icon labels in black over a picture whichever appearance it is
// in, so the ground is light; it is also drawn at 2x, tagged 144 dpi, so it is
// sharp on a Retina screen and still the window's size in points.
const (
	defaultScale = 2

	// arrowGap is the space kept between an icon and the arrow's end, and
	// arrowMinimum the shortest arrow worth drawing, both in points. A window
	// whose icons are closer than the two together gets no arrow.
	arrowGap, arrowMinimum = 18, 40
	// shaft and head are the arrow's stroke width and half the height of its
	// head, in points.
	shaft, head = 8.0, 20.0
)

var (
	groundTop    = [3]float64{247, 247, 248}
	groundBottom = [3]float64{232, 233, 236}
	arrowColor   = [3]float64{140, 146, 156}
)

// defaultBackground draws the picture for the layout as a PNG.
func (l Layout) defaultBackground() ([]byte, error) {
	width, height := l.Width*defaultScale, l.Height*defaultScale
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))

	y := float64(l.App.Y) * defaultScale
	half := float64(l.IconSize) / 2
	left := (float64(l.App.X) + half + arrowGap) * defaultScale
	right := (float64(l.Applications.X) - half - arrowGap) * defaultScale
	drawn := right-left >= arrowMinimum*defaultScale
	stroke := shaft * defaultScale / 2
	reach := head * defaultScale
	tip := right - stroke*0.5

	for row := 0; row < height; row++ {
		t := float64(row) / float64(height-1)
		ground := [3]float64{}
		for channel := range ground {
			ground[channel] = groundTop[channel] + (groundBottom[channel]-groundTop[channel])*t
		}
		for column := 0; column < width; column++ {
			coverage := 0.0
			if drawn {
				x, py := float64(column)+0.5, float64(row)+0.5
				distance := math.Min(segment(x, py, left, y, tip, y),
					math.Min(segment(x, py, tip-reach, y-reach, tip, y), segment(x, py, tip-reach, y+reach, tip, y)))
				// One pixel of falloff around the stroke's edge.
				coverage = math.Max(0, math.Min(1, stroke-distance+0.5))
			}
			picture.SetNRGBA(column, row, color.NRGBA{
				R: mix(ground[0], arrowColor[0], coverage),
				G: mix(ground[1], arrowColor[1], coverage),
				B: mix(ground[2], arrowColor[2], coverage),
				A: 255,
			})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		return nil, err
	}
	return tagResolution(encoded.Bytes(), 72*defaultScale), nil
}

func mix(from, to, amount float64) uint8 {
	return uint8(math.Round(from + (to-from)*amount))
}

// segment is the distance from (px, py) to the line from (ax, ay) to (bx, by).
func segment(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// tagResolution inserts the pHYs chunk that tells the Finder how many pixels
// make a point. The encoder writes none, and chunks may follow IHDR directly.
func tagResolution(data []byte, dpi int) []byte {
	const afterHeader = 8 + 25
	perMetre := uint32(math.Round(float64(dpi) / 0.0254))
	chunk := binary.BigEndian.AppendUint32(nil, 9)
	chunk = append(chunk, "pHYs"...)
	chunk = binary.BigEndian.AppendUint32(chunk, perMetre)
	chunk = binary.BigEndian.AppendUint32(chunk, perMetre)
	chunk = append(chunk, 1)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	out := append([]byte(nil), data[:afterHeader]...)
	out = append(out, chunk...)
	return append(out, data[afterHeader:]...)
}
