package platform

import "testing"

func TestMinimumContentSizeAcrossDisplays(t *testing.T) {
	config := Config{MinWidth: 960, MinHeight: 600}
	for _, test := range []struct {
		name                            string
		scale                           float32
		availableWidth, availableHeight int
		width, height                   int
	}{
		{"100%", 1, 1920, 1040, 960, 600},
		{"125%", 1.25, 1920, 1040, 1200, 750},
		{"150%", 1.5, 2560, 1400, 1440, 900},
		{"175%", 1.75, 3840, 2100, 1680, 1050},
		{"200%", 2, 3840, 2100, 1920, 1200},
		{"small high-DPI laptop", 1.5, 1366, 690, 1366, 690},
		{"work area unknown", 1.25, 0, 0, 1200, 750},
		{"scale unknown", 0, 1920, 1040, 960, 600},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, height := config.MinimumContentSize(test.scale, test.availableWidth, test.availableHeight)
			if width != test.width || height != test.height {
				t.Fatalf("minimum = %dx%d, want %dx%d", width, height, test.width, test.height)
			}
		})
	}
	if w, h := (Config{MinWidth: 961}).MinimumContentSize(1.25, 0, 0); w != 1202 || h != 0 {
		t.Fatalf("rounding/unrestricted height = %dx%d", w, h)
	}
	if w, h := (Config{}).MinimumContentSize(2, 1920, 1040); w != 0 || h != 0 {
		t.Fatalf("zero config changed native defaults: %dx%d", w, h)
	}
}
