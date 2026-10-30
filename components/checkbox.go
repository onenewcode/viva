package components

import "github.com/vivaui/viva/ui"

// Checkbox paints one official shadcn checkable control. State remains owned
// by the caller and is exposed through a stable typed hit.
type Checkbox struct {
	canvas    *ui.Canvas
	theme     ui.Theme
	id        ui.ID
	bounds    ui.Rect
	hitBounds ui.Rect
	label     string
	checked   bool
	// indeterminate is Base UI's mixed state: the box shows a dash and the
	// accessibility value is "mixed", whatever checked says.
	indeterminate bool
	focused       bool
	invalid       bool
	disabled      bool
}

// BeginCheckbox starts a checkbox declaration. Bounds provide only the visual
// origin; the official visual remains 16px at default density. ID zero is
// preview-only.
func BeginCheckbox(canvas *ui.Canvas, theme ui.Theme, id ui.ID, bounds ui.Rect, checked bool) Checkbox {
	focused := false
	if canvas != nil && !id.IsZero() {
		focused = canvas.FocusVisible() == id
	}
	return Checkbox{
		canvas: canvas, theme: theme, id: id, bounds: bounds,
		label: "checkbox", checked: checked, focused: focused,
	}
}

// Label sets the accessible name without changing visual geometry.
func (checkbox Checkbox) Label(label string) Checkbox {
	checkbox.label = label
	return checkbox
}

// HitBounds makes an enclosing label or selection card activate this checkbox.
// The visual square keeps its normal position and size; zero bounds use the
// default enlarged target. The entire target exposes one checkbox semantic hit,
// so it replaces the square's target rather than adding to it: the rect must
// contain the square, and it must not cover other interactive hits painted
// after this checkbox, because the later hit wins. Canvas.HitLabel is the other
// way to make an enclosing region activate a control; it keeps the control's
// own bounds and exposes the region as that control's label, so prefer it when
// the semantic target should stay the square.
func (checkbox Checkbox) HitBounds(bounds ui.Rect) Checkbox {
	checkbox.hitBounds = bounds
	return checkbox
}

// Indeterminate paints Base UI's mixed state: a primary box with a dash,
// exposed as aria-checked="mixed". It takes precedence over Checked.
func (checkbox Checkbox) Indeterminate() Checkbox {
	checkbox.indeterminate = true
	return checkbox
}

// Invalid applies the official destructive validation border and ring.
func (checkbox Checkbox) Invalid() Checkbox {
	checkbox.invalid = true
	return checkbox
}

// Disabled paints the official 50% opacity state and retains disabled
// accessibility semantics for nonzero IDs.
func (checkbox Checkbox) Disabled() Checkbox {
	checkbox.disabled = true
	return checkbox
}

// CheckboxResult is what painting reports back; Clicked is true in the event pass of a
// click on the control (see ui.Canvas.Clicked), so the code that declares it
// handles it.
type CheckboxResult struct {
	canvas *ui.Canvas
	id     ui.ID
}

// Clicked reports whether this paint is delivering a click on the control.
func (result CheckboxResult) Clicked() bool {
	return result.canvas != nil && result.canvas.Clicked(result.id)
}

// CheckboxExtent is the side of a checkbox's square at the theme's density.
func CheckboxExtent(theme ui.Theme) float32 { return 16 * theme.Metrics.ControlHeight / 32 }

// CheckboxHitExtent is the width and height of the interaction region Paint
// declares around a checkbox at the theme's density. A layout that gives a
// checkbox this much room can center the square inside it and keep adjacent
// checkboxes' hit regions from overlapping.
func CheckboxHitExtent(theme ui.Theme) (width, height float32) {
	side := CheckboxExtent(theme)
	scale := theme.Metrics.ControlHeight / 32
	return side + 24*scale, side + 16*scale
}

// Paint draws the checkbox and declares its enlarged desktop hit region.
func (checkbox Checkbox) Paint() CheckboxResult {
	result := CheckboxResult{canvas: checkbox.canvas, id: checkbox.id}
	if checkbox.canvas == nil {
		return result
	}
	scale := checkbox.theme.Metrics.ControlHeight / 32
	checkbox.bounds.Width = CheckboxExtent(checkbox.theme)
	checkbox.bounds.Height = checkbox.bounds.Width

	background := checkbox.theme.Background
	background.Alpha = 0
	border := checkbox.theme.Input
	foreground := checkbox.theme.PrimaryForeground
	if isDark(checkbox.theme) {
		background = scaledAlpha(checkbox.theme.Input, 0.30)
	}
	if checkbox.checked || checkbox.indeterminate {
		background = checkbox.theme.Primary
		border = checkbox.theme.Primary
	}
	if checkbox.focused && !checkbox.checked {
		border = checkbox.theme.Ring
	}
	if checkbox.invalid && !checkbox.checked {
		border = invalidBorder(checkbox.theme)
	}
	checkbox.canvas.Elevate(checkbox.bounds, 4*scale, ui.ShadowXS) // shadow-xs
	if checkbox.focused || checkbox.invalid {
		ring := scaledAlpha(checkbox.theme.Ring, 0.50)
		if checkbox.invalid {
			ring = invalidRing(checkbox.theme)
		}
		checkbox.canvas.Rounded(expand(checkbox.bounds, 3*scale), 7*scale, ring)
	}
	if checkbox.disabled {
		background.Alpha *= 0.50
		border.Alpha *= 0.50
		foreground.Alpha *= 0.50
	}
	paintBordered(checkbox.canvas, checkbox.theme, checkbox.bounds, 4*scale, border, background)
	if checkbox.indeterminate {
		dash := checkbox.bounds.Width * 0.5
		thickness := max(checkbox.bounds.Height/8, 1)
		checkbox.canvas.Fill(ui.Rect{
			X:     checkbox.bounds.X + (checkbox.bounds.Width-dash)/2,
			Y:     checkbox.bounds.Y + (checkbox.bounds.Height-thickness)/2,
			Width: dash, Height: thickness,
		}, foreground)
	} else if checkbox.checked {
		paintCheck(checkbox.canvas, checkbox.bounds, checkbox.bounds.Height*0.875, foreground)
	}
	if !checkbox.id.IsZero() {
		hitBounds := ui.Rect{
			X: checkbox.bounds.X - 12*scale, Y: checkbox.bounds.Y - 8*scale,
			Width: checkbox.bounds.Width + 24*scale, Height: checkbox.bounds.Height + 16*scale,
		}
		if checkbox.hitBounds.Width > 0 && checkbox.hitBounds.Height > 0 {
			hitBounds = checkbox.hitBounds
		}
		checkbox.canvas.HitWithOptions(checkbox.id, hitBounds, checkbox.label, ui.HitOptions{
			Role: ui.RoleCheckbox, Disabled: checkbox.disabled,
			Checked: checkbox.checked && !checkbox.indeterminate, Mixed: checkbox.indeterminate,
		})
	}
	return result
}

// The check is a vector, not a character. A ✓ set in a font is whatever the system's
// fonts hold for it: a different shape, weight and place in the line box on each
// platform (Segoe UI Symbol's is a tall calligraphic stroke, SF's a short one), and none
// is the Lucide mark the other icons are drawn with. A path is the same on all of them.
var checkIcon = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	// Center the visible path on y=12 too: the original y=6..17 glyph sits
	// half a view-box unit above the center of its 24-unit canvas.
	ui.MoveTo(20, 6.5), ui.LineTo(9, 17.5), ui.LineTo(4, 12.5),
}}

// paintCheck draws a check mark size pixels square, centered in bounds.
func paintCheck(canvas *ui.Canvas, bounds ui.Rect, size float32, color ui.Color) {
	canvas.DrawIcon(checkIcon, ui.Rect{X: bounds.X + (bounds.Width-size)/2, Y: bounds.Y + (bounds.Height-size)/2, Width: size, Height: size}, color)
}
