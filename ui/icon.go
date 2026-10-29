package ui

import "math"

// Icon is one compiled vector glyph: immutable Path commands plus stroke
// metadata, produced ahead of time by the `viva icons` compiler from SVG
// sources. Icons carry no parser at runtime; an application embeds the
// generated Go values and paints them at any size in any tint.
//
//	var chevron = ui.Icon{
//	    ViewBox: 24,
//	    Stroke:  2,
//	    Cap:     ui.CapRound,
//	    Join:    ui.JoinRound,
//	    Commands: []ui.PathCommand{
//	        ui.MoveTo(6, 9), ui.QuadTo(12, 15, 18, 9),
//	    },
//	}
//	canvas.DrawIcon(chevron, bounds, theme.Foreground)
type Icon struct {
	// ViewBox is the source coordinate width, and the height too unless ViewHeight
	// says otherwise (Lucide's square icons use 24).
	ViewBox float32
	// ViewHeight is the source coordinate height of an icon that is not square,
	// such as a horizontal logo. Zero means the icon is square, ViewBox high.
	ViewHeight float32
	// Stroke is the source stroke width in view-box units (Lucide uses 2).
	Stroke float32
	// Cap and Join describe the source stroke geometry.
	Cap  StrokeCap
	Join StrokeJoin
	// Commands is the compiled path vocabulary, stroked (or filled when Stroke
	// is zero) in the tint DrawIcon is given.
	Commands []PathCommand
	// Layers are filled shapes with colors of their own, drawn after Commands
	// in order. They are what a multi-color glyph (a file-type icon, a brand
	// mark) is made of; the tint contributes only its alpha to them.
	Layers []IconLayer
}

// IconLayer is one filled shape of a multi-color icon.
type IconLayer struct {
	// Fill is the solid color. With a Gradient it is only the color a tool that
	// ignores gradients would use.
	Fill     Color
	FillRule PathFillRule
	// Gradient, when set, replaces Fill with a linear gradient.
	Gradient *LinearGradient
	Commands []PathCommand
}

// LinearGradient is a two-color linear gradient in an icon's view-box units: From
// at Start, To at End, held flat beyond both (SVG's spreadMethod "pad"), mixed as
// straight RGBA like SVG. Renderers interpolate colors across a triangle, so the
// canvas cuts the layer's triangles at the two lines where the gradient starts and
// ends: the pieces beyond are flat, the ones between carry the gradient as the
// colors of their vertices, which is exact. The tessellation is cached with the
// icon, so a gradient costs a replay no more than a solid layer.
type LinearGradient struct {
	Start, End Point
	From, To   Color
}

// At returns the gradient color at t, 0 at Start and 1 at End.
func (gradient LinearGradient) At(t float32) Color {
	if t <= 0 {
		return gradient.From
	}
	if t >= 1 {
		return gradient.To
	}
	return Color{
		Red:   gradient.From.Red + (gradient.To.Red-gradient.From.Red)*t,
		Green: gradient.From.Green + (gradient.To.Green-gradient.From.Green)*t,
		Blue:  gradient.From.Blue + (gradient.To.Blue-gradient.From.Blue)*t,
		Alpha: gradient.From.Alpha + (gradient.To.Alpha-gradient.From.Alpha)*t,
	}
}

// height is the icon's source height.
func (icon Icon) height() float32 {
	if icon.ViewHeight > 0 {
		return icon.ViewHeight
	}
	return icon.ViewBox
}

// DrawIcon paints one icon tinted with a single color, scaled uniformly
// to fit bounds and centered. The icon strokes scale with the icon, but its size
// and origin are rounded to whole device pixels and a stroke up to half a device
// pixel (see SetPixelScale), never thinner than one: a 16 point Lucide icon draws
// a 1.33 point stroke, which whole-pixel rounding turned into a single hairline at
// 100% but 1.5 points at 200%, so the same icon looked a third fainter on a 1x
// display.
//
// A multi-color icon paints each of its layers in the layer's own color; the
// tint's alpha fades the whole glyph and its color is not used.
func (canvas *Canvas) DrawIcon(icon Icon, bounds Rect, tint Color) {
	if tint.Alpha <= 0 || icon.ViewBox <= 0 || len(icon.Commands) == 0 && len(icon.Layers) == 0 {
		return
	}
	target, ok := canvas.iconTarget(icon, bounds)
	if !ok {
		return
	}
	canvas.drawIconCached(icon, target.Width, Point{X: target.X, Y: target.Y}, tint)
}

// iconTarget is where and how large DrawIcon paints an icon in bounds, on whole device pixels.
func (canvas *Canvas) iconTarget(icon Icon, bounds Rect) (Rect, bool) {
	width, height := icon.fitted(bounds)
	if width <= 0 || height <= 0 {
		return Rect{}, false
	}
	// Stay in device pixels until placement is rounded. Converting a snapped
	// width back to float32 first can turn an exact half-pixel origin into a
	// value just below the tie (e.g. a 14px icon at 125%), shifting it up/left.
	scale := float64(canvas.PixelScale())
	pixelWidth, pixelHeight := float64(width)*scale, float64(height)*scale
	if snapped := math.Floor(pixelWidth + 0.5); snapped > 0 {
		pixelHeight *= snapped / pixelWidth
		pixelWidth = snapped
	}
	x := (float64(bounds.X)+float64(bounds.Width)*0.5)*scale - pixelWidth*0.5
	y := (float64(bounds.Y)+float64(bounds.Height)*0.5)*scale - pixelHeight*0.5
	return Rect{
		X: float32(math.Floor(x+0.5) / scale), Y: float32(math.Floor(y+0.5) / scale),
		Width: float32(pixelWidth / scale), Height: float32(pixelHeight / scale),
	}, true
}

// drawIconDirect tessellates and paints an icon from scratch. DrawIcon replays a
// cached tessellation instead; this is what fills the cache and what the tests
// compare it against.
func (canvas *Canvas) drawIconDirect(icon Icon, bounds Rect, tint Color) {
	if tint.Alpha <= 0 || icon.ViewBox <= 0 || len(icon.Commands) == 0 && len(icon.Layers) == 0 {
		return
	}
	target, ok := canvas.iconTarget(icon, bounds)
	if !ok {
		return
	}
	canvas.paintIconShapes(icon, target, tint, nil)
}

// fitted is the size the icon takes inside bounds: as large as fits with its
// aspect ratio kept.
func (icon Icon) fitted(bounds Rect) (width, height float32) {
	scale := min(bounds.Width/icon.ViewBox, bounds.Height/icon.height())
	return icon.ViewBox * scale, icon.height() * scale
}

// paintIconShapes paints the icon's strokes or fills and then its layers into
// target. After each shape it calls shape, if given, with -1 for the icon's own and
// the layer's index otherwise.
func (canvas *Canvas) paintIconShapes(icon Icon, target Rect, tint Color, shape func(layer int)) {
	if len(icon.Commands) > 0 {
		scale := target.Width / icon.ViewBox
		strokeWidth := icon.Stroke * scale
		if strokeWidth > 0 {
			// A stroke never renders thinner than it was drawn: rounding to whole
			// pixels turned a 1.33 point stroke into 1 point at 100% but 1.5 at 200%.
			// Half a device pixel is the finest grid that keeps the weight steady.
			strokeWidth = max(canvas.snapToHalfDeviceUp(strokeWidth), canvas.snapToDevice(1))
		}
		style := PathStyle{
			Stroke:      tint,
			StrokeWidth: strokeWidth,
			Cap:         icon.Cap,
			Join:        icon.Join,
		}
		if icon.Stroke <= 0 {
			style.Stroke = Color{}
			style.Fill = tint
		}
		canvas.DrawPath(Path{width: icon.ViewBox, height: icon.height(), commands: icon.Commands}, target, style)
		if shape != nil {
			shape(-1)
		}
	}
	for index := range icon.Layers {
		layer := &icon.Layers[index]
		fill := layer.Fill
		if layer.Gradient != nil {
			fill = layer.Gradient.From
		}
		fill.Alpha *= tint.Alpha
		start := len(canvas.triangles)
		canvas.DrawPath(Path{width: icon.ViewBox, height: icon.height(), commands: layer.Commands}, target,
			PathStyle{Fill: fill, FillRule: layer.FillRule})
		if shape != nil {
			shape(index)
		} else if layer.Gradient != nil {
			canvas.shadeGradient(start, newGradientAxis(*layer.Gradient, target, target.Width/icon.ViewBox, target.Width/icon.ViewBox), layer.Gradient, tint.Alpha)
		}
	}
}

// referenceScale is the device scale a canvas paints for until SetPixelScale says
// otherwise: a Retina display, and the scale the headless renderer defaults to.
const referenceScale = 2

// SetPixelScale tells the canvas how many device pixels one logical pixel is on the
// display it paints for (1.25 at 125%), so that what must land on whole pixels, such as the
// edges of an icon, does at that scale, not only at 2x; an icon's stroke snaps up to half
// a device pixel at that scale. Zero is unspecified and restores the reference scale. It
// survives Reset.
func (canvas *Canvas) SetPixelScale(scale float32) {
	if scale > 0 {
		canvas.pixelScale = scale
	} else {
		canvas.pixelScale = 0
	}
}

// PixelScale is the device pixels per logical pixel the canvas paints for.
func (canvas *Canvas) PixelScale() float32 {
	if canvas.pixelScale > 0 {
		return canvas.pixelScale
	}
	return referenceScale
}

// snapToDevice rounds a logical-pixel length or coordinate to the nearest whole device pixel.
func (canvas *Canvas) snapToDevice(value float32) float32 {
	scale := canvas.PixelScale()
	return float32(math.Floor(float64(value)*float64(scale)+0.5)) / scale
}

// snapToHalfDeviceUp rounds a logical-pixel length up to the nearest half device pixel:
// fine enough for a stroke to keep the weight it was drawn with, and never below it, so a
// 1.33 point icon stroke is 1.5 pixels at 100% and 3 at 200% instead of 1 and 3.
func (canvas *Canvas) snapToHalfDeviceUp(value float32) float32 {
	scale := canvas.PixelScale()
	// A float32 stroke puts an exact half a hair above the boundary; without the
	// tolerance a 1.33 point stroke at 150% would gain a whole half pixel.
	halves := math.Ceil(float64(value)*float64(scale)*2 - 1e-4)
	return float32(halves) / (2 * scale)
}
