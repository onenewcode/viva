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

	// Viewport destination must equal total surface logical dimensions (848x648 at 1.5x with margin 24)
	if viewportSetWidth != 848 || viewportSetHeight != 648 {
		t.Fatalf("expected viewport destination (848, 648), got (%d, %d)", viewportSetWidth, viewportSetHeight)
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

func TestWaylandScaledWindowGeometryAndInputRegion(t *testing.T) {
	var (
		setGeomX, setGeomY, setGeomW, setGeomH int32
		addedRegionX, addedRegionY, addedRegionW, addedRegionH int32
	)

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if opcode == xdgSurfaceSetWindowGeometryOpcode && len(args) >= 4 {
				setGeomX = args[0].(int32)
				setGeomY = args[1].(int32)
				setGeomW = args[2].(int32)
				setGeomH = args[3].(int32)
			}
			if opcode == wlRegionAddOpcode && len(args) >= 4 {
				addedRegionX = args[0].(int32)
				addedRegionY = args[1].(int32)
				addedRegionW = args[2].(int32)
				addedRegionH = args[3].(int32)
			}
			if opcode == wlCompositorCreateRegionOpcode {
				return 999
			}
			return 0
		},
		proxyDestroy: func(proxy uintptr) {},
	}

	app := &waylandApplication{
		api: api,
		globals: waylandGlobals{
			compositor: 888,
		},
	}

	win := &waylandWindow{
		app:        app,
		surface:    10,
		xdgSurface: 20,
		width:      1272, // 1200 + 2*36
		height:     972,  // 900 + 2*36
		margin:     36,   // 24 * 1.5
	}
	win.storeScale(1.5)

	win.updateWindowGeometry()
	if setGeomX != 24 || setGeomY != 24 || setGeomW != 800 || setGeomH != 600 {
		t.Fatalf("expected logical geometry (24, 24, 800, 600), got (%d, %d, %d, %d)", setGeomX, setGeomY, setGeomW, setGeomH)
	}

	win.updateInputRegion()
	// grip at 1.5x = 8*1.5 = 12. reach = min(12, 36) = 12.
	// margin - reach = 36 - 12 = 24 physical -> 24 / 1.5 = 16 logical
	// cW + 2*reach = 1200 + 24 = 1224 physical -> 1224 / 1.5 = 816 logical
	if addedRegionX != 16 || addedRegionY != 16 || addedRegionW != 816 || addedRegionH != 616 {
		t.Fatalf("expected logical input region (16, 16, 816, 616), got (%d, %d, %d, %d)", addedRegionX, addedRegionY, addedRegionW, addedRegionH)
	}
}
