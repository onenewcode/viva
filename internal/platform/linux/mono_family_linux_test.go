package linux

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestAFixedPitchFaceNamesItsFallbacksToPangoInOrderBeforeTheGenericFamily(t *testing.T) {
	maple := ui.MonoFamily("Test Maple Mono NF")
	stack := ui.MonoStack(maple, "Test LXGW WenKai Mono Lite", "Test Symbols")
	for _, test := range []struct {
		name string
		face ui.MonoFace
		want string
	}{
		{"system", ui.MonoSystem, "monospace"},
		{"classic", ui.MonoClassic, "DejaVu Sans Mono"},
		{"family", maple, "Test Maple Mono NF, monospace"},
		{"stack", stack, "Test Maple Mono NF, Test LXGW WenKai Mono Lite, Test Symbols, monospace"},
		{"system stack", ui.MonoStack(ui.MonoSystem, "Test CJK"), "Test CJK, monospace"},
		{"classic stack", ui.MonoStack(ui.MonoClassic, "Test CJK"), "DejaVu Sans Mono, Test CJK, monospace"},
	} {
		if got := monoFamily(test.face); got != test.want {
			t.Errorf("%s: family list = %q, want %q", test.name, got, test.want)
		}
	}
}
