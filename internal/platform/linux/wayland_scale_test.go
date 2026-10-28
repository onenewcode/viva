//go:build linux

package linux

import (
	"testing"

	"github.com/vivaui/viva/internal/platform"
)

func TestWaylandFractionalScaleCalculations(t *testing.T) {
	cases := []struct {
		numerator uint32
		wantScale float32
	}{
		{120, 1.0},
		{150, 1.25},
		{180, 1.5},
		{210, 1.75},
		{240, 2.0},
		{0, 1.0},
	}

	for _, c := range cases {
		scale := float32(c.numerator) / 120.0
		if scale <= 0 {
			scale = 1.0
		}
		if scale != c.wantScale {
			t.Errorf("numerator %d -> scale %g, want %g", c.numerator, scale, c.wantScale)
		}
	}
}

func TestWaylandApplyScaleAndViewport(t *testing.T) {
	var viewportSetWidth, viewportSetHeight int32

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if opcode == 2 && len(args) >= 2 { // wp_viewport.set_destination
				if w, ok := args[0].(int32); ok {
					viewportSetWidth = w
				}
				if h, ok := args[1].(int32); ok {
					viewportSetHeight = h
				}
			}
			return 0
		},
	}
	app := &waylandApplication{api: api}

	// 800x600 logical at 1.0x with margin 24
	win := &waylandWindow{
		app:         app,
		surface:     10,
		xdgSurface:  20,
		viewport:    30,
		width:       848, // 800 + 2*24
		height:      648, // 600 + 2*24
		margin:      24,
		frameWanted: true,
	}
	win.storeScale(1.0)

	var reportedSize platform.Size
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnResize: func(size platform.Size) (platform.Frame, error) {
				reportedSize = size
				return platform.Frame{}, nil
			},
		},
	}
	win.state = state

	// Apply 1.5x scale
	win.applyScale(1.5)

	if win.displayScale() != 1.5 {
		t.Fatalf("expected displayScale 1.5, got %g", win.displayScale())
	}
	if win.margin != 36 {
		t.Fatalf("expected margin 36 at 1.5x, got %d", win.margin)
	}

	// 800 * 1.5 = 1200 + 2*36 = 1272
	if win.width != 1272 || win.height != 972 {
		t.Fatalf("expected total size 1272x972, got %dx%d", win.width, win.height)
	}

	// Viewport destination must equal logical content dimensions (800x600)
	if viewportSetWidth != 800 || viewportSetHeight != 600 {
		t.Fatalf("expected viewport destination (800, 600), got (%d, %d)", viewportSetWidth, viewportSetHeight)
	}

	// Logical size reported to application must remain 800x600 with scale 1.5
	if reportedSize.Width != 800 || reportedSize.Height != 600 || reportedSize.Scale != 1.5 {
		t.Fatalf("expected logical size (800x600 @ 1.5x), got %v", reportedSize)
	}
}

func TestWaylandSurfaceEnterLeaveOutput(t *testing.T) {
	win := &waylandWindow{}

	// Enter output 1
	win.handleSurfaceEnter(1)
	if len(win.activeOutputs) != 1 || win.activeOutputs[0] != 1 {
		t.Fatalf("expected activeOutputs [1], got %v", win.activeOutputs)
	}

	// Enter output 2 (dual monitor spanning)
	win.handleSurfaceEnter(2)
	if len(win.activeOutputs) != 2 || win.activeOutputs[1] != 2 {
		t.Fatalf("expected activeOutputs [1, 2], got %v", win.activeOutputs)
	}

	// Leave output 1
	win.handleSurfaceLeave(1)
	if len(win.activeOutputs) != 1 || win.activeOutputs[0] != 2 {
		t.Fatalf("expected activeOutputs [2], got %v", win.activeOutputs)
	}

	// Leave output 2
	win.handleSurfaceLeave(2)
	if len(win.activeOutputs) != 0 {
		t.Fatalf("expected activeOutputs empty, got %v", win.activeOutputs)
	}
}
