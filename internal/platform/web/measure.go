//go:build js && wasm

package web

import (
	"fmt"
	"strings"
	"syscall/js"

	"github.com/vivaui/viva/ui"
)

// platformBaselines returns the browser font's ascent and descent at a size, so label
// baselines match the native CoreText formula. Widths, carets and hit-testing come from
// the text engine (textengine_js.go).
func platformBaselines() func(fontSize float32) baselineMetrics {
	document := js.Global().Get("document")
	offscreen := document.Call("createElement", "canvas")
	context := offscreen.Call("getContext", "2d")

	return func(fontSize float32) baselineMetrics {
		context.Set("font", cssFont(fontSize, 0))
		metrics := context.Call("measureText", "Ag")
		ascent := float32(metrics.Get("fontBoundingBoxAscent").Float())
		descent := float32(metrics.Get("fontBoundingBoxDescent").Float())
		if ascent <= 0 {
			ascent = float32(metrics.Get("actualBoundingBoxAscent").Float())
			descent = float32(metrics.Get("actualBoundingBoxDescent").Float())
		}
		return baselineMetrics{Ascent: ascent, Descent: descent}
	}
}

func cssFont(fontSize, weight float32) string {
	cssWeight := 400
	switch {
	case weight >= 0.34:
		cssWeight = 700
	case weight >= 0.20:
		cssWeight = 600
	case weight >= 0.10:
		cssWeight = 500
	}
	return fmt.Sprintf("%d %gpx system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif", cssWeight, fontSize)
}

// baselineMetrics mirrors the CoreText typographic values the native label
// rasterizer uses to vertically center a line inside its bounds.
type baselineMetrics struct {
	Ascent  float32
	Descent float32
}

// cssMonoFont is the browser's fixed-pitch font.
func cssMonoFont(fontSize, weight float32, italic bool, face ui.MonoFace) string {
	cssWeight := 400
	switch {
	case weight >= 0.34:
		cssWeight = 700
	case weight >= 0.20:
		cssWeight = 600
	case weight >= 0.10:
		cssWeight = 500
	}
	slant := ""
	if italic {
		slant = "italic "
	}
	return fmt.Sprintf("%s%d %gpx %s", slant, cssWeight, fontSize, cssMonoFamilies(face))
}

// cssMonoFamilies is the font-family list of a face: its primary font, then the
// families that draw what the primary lacks, then the generic ones.
func cssMonoFamilies(face ui.MonoFace) string {
	var list strings.Builder
	switch name := ui.MonoFamilyName(face); {
	case name != "":
		list.WriteString(cssFamily(name))
		list.WriteString(", ")
	case ui.MonoIsClassic(face):
		list.WriteString("'Courier New', Courier, ")
	}
	for _, fallback := range ui.MonoFallbacks(face) {
		list.WriteString(cssFamily(fallback))
		list.WriteString(", ")
	}
	switch {
	case ui.MonoFamilyName(face) != "":
		list.WriteString("monospace")
	case ui.MonoIsClassic(face):
		list.WriteString("monospace")
	default:
		list.WriteString("ui-monospace, SFMono-Regular, Menlo, Consolas, monospace")
	}
	return list.String()
}

// monoBaseline reports the fixed-pitch font's ascent and descent at a size.
func monoBaseline(context js.Value, fontSize float32, face ui.MonoFace) baselineMetrics {
	context.Set("font", cssMonoFont(fontSize, 0, false, face))
	metrics := context.Call("measureText", "Ag")
	ascent := float32(metrics.Get("fontBoundingBoxAscent").Float())
	descent := float32(metrics.Get("fontBoundingBoxDescent").Float())
	if ascent <= 0 {
		ascent = float32(metrics.Get("actualBoundingBoxAscent").Float())
		descent = float32(metrics.Get("actualBoundingBoxDescent").Float())
	}
	return baselineMetrics{Ascent: ascent, Descent: descent}
}

// cssFamily quotes a font family name for a CSS font shorthand. Quotes and
// backslashes are dropped: a family name has none, and one that did could close
// the quotes.
func cssFamily(name string) string {
	return "'" + strings.NewReplacer("'", "", `"`, "", `\`, "").Replace(name) + "'"
}
