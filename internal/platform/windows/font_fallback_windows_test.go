package windows

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/vivaui/viva/internal/platform"
	"github.com/vivaui/viva/ui"
)

// TestFallbackFamiliesDrawTheCharactersThePrimaryLacks: Consolas has no CJK glyphs, so
// a line drawn with two different CJK fallback families must differ, the same face must
// draw the same pixels twice, and the characters Consolas has must not move. The test
// needs the two CJK families; a machine without them skips it.
func TestFallbackFamiliesDrawTheCharactersThePrimaryLacks(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rasterizer, err := newDirectRasterizer()
	if err != nil {
		t.Skipf("no DirectWrite: %v", err)
	}
	defer rasterizer.close()
	consolas := ui.MonoFamily("Consolas")
	white := ui.Color{Red: 1, Green: 1, Blue: 1, Alpha: 1}
	draw := func(face ui.MonoFace, value string, runs []ui.TextRun) []byte {
		label := platform.TextLabel{Value: value, Width: 160, Height: 32, FontSize: 20, CellWidth: 12, Mono: true, MonoFace: face, Runs: runs}
		pixels, _, err := rasterizer.rasterize(label, 1, 160, 32, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), pixels...)
	}
	runs := []ui.TextRun{{Bytes: 3, Cells: 2, Color: white}, {Column: 2, Bytes: 3, Cells: 2, Color: white}}
	first := ui.MonoStack(consolas, "Microsoft YaHei UI")
	second := ui.MonoStack(consolas, "SimSun")
	a, again, b, plain := draw(first, "你好", runs), draw(first, "你好", runs), draw(second, "你好", runs), draw(consolas, "你好", runs)
	if !bytes.Equal(a, again) {
		t.Fatal("the same face drew two different images")
	}
	if bytes.Equal(a, b) {
		t.Fatal("two different fallback families drew the same pixels: the fallback list was not used")
	}
	if bytes.Equal(a, plain) {
		t.Fatal("a stack with a fallback drew like its bare primary")
	}
	ascii := []ui.TextRun{{Bytes: 5, Cells: 5, Color: white}}
	if !bytes.Equal(draw(first, "abcde", ascii), draw(consolas, "abcde", ascii)) {
		t.Fatal("a fallback changed how the primary draws characters it has")
	}
}

func TestFallbackFamiliesNeverChangeTheCell(t *testing.T) {
	plainWidth, plainHeight, err := MeasureMono(13, ui.MonoFamily("Consolas"))
	if err != nil {
		t.Skipf("no DirectWrite: %v", err)
	}
	width, height, err := MeasureMono(13, ui.MonoStack(ui.MonoFamily("Consolas"), "No Such Family Installed", "SimSun"))
	if err != nil || width != plainWidth || height != plainHeight {
		t.Fatalf("Consolas with fallbacks measures %v x %v (%v), want its own %v x %v", width, height, err, plainWidth, plainHeight)
	}
}
