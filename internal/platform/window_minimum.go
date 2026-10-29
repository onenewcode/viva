package platform

import "math"

// MinimumContentSize converts the requested minimum to device pixels. Available
// dimensions exclude native decorations; a nonpositive available axis means its
// screen limit is unknown. Zero minima stay zero, preserving native defaults.
func (config Config) MinimumContentSize(scale float32, availableWidth, availableHeight int) (int, int) {
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		scale = 1
	}
	axis := func(requested, available int) int {
		if requested <= 0 {
			return 0
		}
		pixels := int(math.Ceil(float64(requested) * float64(scale)))
		if available > 0 {
			pixels = min(pixels, available)
		}
		return pixels
	}
	return axis(config.MinWidth, availableWidth), axis(config.MinHeight, availableHeight)
}
