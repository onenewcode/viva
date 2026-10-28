//go:build linux

package linux

import (
	"math"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/internal/platform"
)

func TestWaylandSurfaceProviderExtensionsAndCreate(t *testing.T) {
	provider := newWaylandSurfaceProvider(0x1234, 0x5678)
	exts := provider.RequiredExtensions()
	if len(exts) != 2 || waylandGoString(exts[0]) != "VK_KHR_surface" || waylandGoString(exts[1]) != "VK_KHR_wayland_surface" {
		t.Fatalf("unexpected RequiredExtensions: %v, %v", waylandGoString(exts[0]), waylandGoString(exts[1]))
	}

	if unsafe.Sizeof(vkWaylandSurfaceCreateInfo{}) != 40 {
		t.Fatalf("vkWaylandSurfaceCreateInfo size = %d, want 40", unsafe.Sizeof(vkWaylandSurfaceCreateInfo{}))
	}
}

func TestWaylandWindowModesGeometryAndInputRegion(t *testing.T) {
	scales := []float32{1.0, 1.5, 2.0}

	for _, scale := range scales {
		margin := frameMargin(scale)
		grip := frameGrip(scale)

		wantMargin := uint32(math.Round(float64(24) * float64(scale)))
		wantGrip := uint32(math.Round(float64(8) * float64(scale)))

		if margin != wantMargin {
			t.Errorf("scale %g: margin = %d, want %d", scale, margin, wantMargin)
		}
		if grip != wantGrip {
			t.Errorf("scale %g: grip = %d, want %d", scale, grip, wantGrip)
		}

		logicalW, logicalH := 400, 300
		contentW := deviceLength(logicalW, scale)
		contentH := deviceLength(logicalH, scale)
		totalW := contentW + 2*margin
		totalH := contentH + 2*margin

		// Content box for set_window_geometry
		contentBoxW := contentSize(totalW, margin)
		contentBoxH := contentSize(totalH, margin)
		if contentBoxW != contentW || contentBoxH != contentH {
			t.Errorf("scale %g: contentBox (%d, %d), want (%d, %d)", scale, contentBoxW, contentBoxH, contentW, contentH)
		}

		// Input region calculation
		reach := min(grip, margin)
		rx := int32(margin - reach)
		ry := int32(margin - reach)
		rw := int32(contentBoxW + 2*reach)
		rh := int32(contentBoxH + 2*reach)

		// The input box must strictly encompass content plus grip, leaving outer (margin - reach) for click passthrough
		if rx <= 0 {
			t.Errorf("scale %g: rx = %d, expected > 0 for shadow click passthrough", scale, rx)
		}
		if rw >= int32(totalW) {
			t.Errorf("scale %g: rw = %d, expected < totalW (%d) for shadow click passthrough", scale, rw, totalW)
		}
		if ry <= 0 {
			t.Errorf("scale %g: ry = %d, expected > 0 for shadow click passthrough", scale, ry)
		}
		if rh >= int32(totalH) {
			t.Errorf("scale %g: rh = %d, expected < totalH (%d) for shadow click passthrough", scale, rh, totalH)
		}
	}
}

func TestWaylandConfigureStateTransitions(t *testing.T) {
	win := &waylandWindow{
		width:       500,
		height:      400,
		margin:      24,
		frameWanted: true,
	}
	win.storeScale(1.0)

	var maximizeReports []bool
	win.state = &waylandWindowState{
		win: win,
		config: platform.Config{
			OnMaximize: func(maximized bool) {
				maximizeReports = append(maximizeReports, maximized)
			},
		},
	}

	// 1. Toplevel configure: Maximized state set
	statesData := []uint32{xdgToplevelStateMaximized, xdgToplevelStateActivated}
	statesArray := wlArray{
		Size:  uintptr(len(statesData) * 4),
		Alloc: uintptr(len(statesData) * 4),
		Data:  unsafe.Pointer(&statesData[0]),
	}

	win.handleXdgToplevelConfigure(800, 600, &statesArray)

	if !win.maximized {
		t.Fatal("expected window to be marked maximized")
	}
	if win.margin != 0 {
		t.Fatalf("expected margin to be stripped to 0 on maximize, got %d", win.margin)
	}
	if len(maximizeReports) != 1 || !maximizeReports[0] {
		t.Fatalf("expected OnMaximize(true), got %v", maximizeReports)
	}

	// 2. Surface configure with serial
	win.handleXdgSurfaceConfigure(1001)
	if win.pendingSerial != 1001 {
		t.Fatalf("expected pendingSerial 1001, got %d", win.pendingSerial)
	}
	if !win.configured {
		t.Fatal("expected configured = true")
	}

	// 3. Toplevel configure: Unmaximized (Normal state)
	emptyStates := wlArray{}
	win.handleXdgToplevelConfigure(400, 300, &emptyStates)

	if win.maximized {
		t.Fatal("expected window to be unmaximized")
	}
	if win.margin != 24 {
		t.Fatalf("expected margin to be restored to 24 on unmaximize, got %d", win.margin)
	}
	if len(maximizeReports) != 2 || maximizeReports[1] != false {
		t.Fatalf("expected OnMaximize(false), got %v", maximizeReports)
	}
}

func TestWaylandFrameClockBackpressure(t *testing.T) {
	redrawCount := 0
	win := &waylandWindow{
		surface:    1,
		xdgSurface: 2,
		configured: true,
	}
	win.state = &waylandWindowState{
		win: win,
		config: platform.Config{
			OnRedraw: func() (platform.Frame, error) {
				redrawCount++
				return platform.Frame{}, nil
			},
		},
	}

	// When framePending is true, renderFrame must not render immediately, but set needsRedraw
	win.framePending = true
	win.renderFrame()

	if !win.needsRedraw {
		t.Fatal("expected needsRedraw to be set to true when framePending is active")
	}
	if redrawCount != 0 {
		t.Fatalf("expected redraw to be delayed by backpressure, got %d", redrawCount)
	}

	// When onFrameDone is called, framePending is cleared and deferred redraw runs
	win.onFrameDone(0, 0)
	if win.framePending {
		// Note: renderFrame requests a new frame callback when app/api are present;
		// here app is nil so framePending stays false
	}
	if win.needsRedraw {
		t.Fatal("expected deferred redraw to clear needsRedraw")
	}
}

func TestWaylandMultiWindowIndependence(t *testing.T) {
	app := &waylandApplication{}

	win1 := &waylandWindow{
		app:         app,
		surface:     10,
		xdgSurface:  20,
		xdgToplevel: 30,
		width:       400,
		height:      300,
	}
	state1 := &waylandWindowState{app: app, win: win1}
	win1.state = state1

	win2 := &waylandWindow{
		app:         app,
		surface:     11,
		xdgSurface:  21,
		xdgToplevel: 31,
		width:       600,
		height:      500,
	}
	state2 := &waylandWindowState{app: app, win: win2}
	win2.state = state2

	app.windows = []*waylandWindowState{state1, state2}

	// Close win1
	closed1 := false
	state1.config.OnClosed = func() {
		closed1 = true
	}
	win1.destroy()

	if !closed1 {
		t.Fatal("expected win1 OnClosed to be called")
	}
	if len(app.windows) != 1 || app.windows[0] != state2 {
		t.Fatalf("expected only state2 to remain in app.windows, got %d", len(app.windows))
	}
	if win2.closed.Load() {
		t.Fatal("win2 should remain open and unaffected")
	}
	if win2.width != 600 || win2.height != 500 {
		t.Fatalf("win2 dimensions corrupted: %dx%d", win2.width, win2.height)
	}

	// Close win2
	win2.destroy()
	if len(app.windows) != 0 {
		t.Fatalf("expected all windows closed, got %d", len(app.windows))
	}
}

func TestWaylandResizeEdgeMapping(t *testing.T) {
	cases := []struct {
		edge platform.WindowEdge
		want uint32
	}{
		{platform.EdgeTop, 1},
		{platform.EdgeBottom, 2},
		{platform.EdgeLeft, 4},
		{platform.EdgeTopLeft, 5},
		{platform.EdgeBottomLeft, 6},
		{platform.EdgeRight, 8},
		{platform.EdgeTopRight, 9},
		{platform.EdgeBottomRight, 10},
		{platform.EdgeNone, 0},
	}

	for _, c := range cases {
		got := xdgResizeEdge(c.edge)
		if got != c.want {
			t.Errorf("xdgResizeEdge(%d) = %d, want %d", c.edge, got, c.want)
		}
	}
}

func TestWaylandConfigureAckOnStateChange(t *testing.T) {
	var ackedSerials []uint32
	var committedSurfaces []uintptr

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if opcode == xdgSurfaceAckConfigureOpcode && len(args) > 0 {
				if serial, ok := args[0].(uint32); ok {
					ackedSerials = append(ackedSerials, serial)
				}
			} else if opcode == wlSurfaceCommitOpcode {
				committedSurfaces = append(committedSurfaces, proxy)
			}
			return 0
		},
		proxyAddListener: func(proxy uintptr, implementation uintptr, data unsafe.Pointer) int32 {
			return 0
		},
	}
	app := &waylandApplication{api: api}
	win := &waylandWindow{
		app:         app,
		surface:     10,
		xdgSurface:  20,
		xdgToplevel: 30,
		width:       400,
		height:      300,
	}
	state := &waylandWindowState{app: app, win: win}
	win.state = state

	// Compositor sends state change (e.g. activation) with 0, 0 (no size change)
	statesData := []uint32{xdgToplevelStateActivated}
	statesArray := wlArray{
		Size:  4,
		Alloc: 4,
		Data:  unsafe.Pointer(&statesData[0]),
	}
	win.handleXdgToplevelConfigure(0, 0, &statesArray)

	// Followed by surface configure with serial
	win.handleXdgSurfaceConfigure(5555)

	if len(ackedSerials) != 1 || ackedSerials[0] != 5555 {
		t.Fatalf("expected serial 5555 to be acknowledged on state change without resize, got %v", ackedSerials)
	}
	if len(committedSurfaces) != 1 || committedSurfaces[0] != 10 {
		t.Fatalf("expected surface commit on state change configure, got %v", committedSurfaces)
	}
	if win.pendingSerial != 0 {
		t.Fatalf("expected pendingSerial to be cleared after ack, got %d", win.pendingSerial)
	}
}

func TestWaylandDestructorOrdering(t *testing.T) {
	var order []string

	app := &waylandApplication{}
	win := &waylandWindow{
		app:         app,
		surface:     10,
		xdgSurface:  20,
		xdgToplevel: 30,
		decoration:  40,
	}
	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			return 0
		},
		proxyDestroy: func(proxy uintptr) {
			switch proxy {
			case 40:
				order = append(order, "decoration")
			case 30:
				order = append(order, "xdgToplevel")
			case 20:
				order = append(order, "xdgSurface")
			case 10:
				order = append(order, "surface")
			}
		},
	}
	app.api = api

	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnClosed: func() {
				order = append(order, "OnClosed")
			},
		},
	}
	win.state = state

	win.destroy()

	// Order must be: decoration -> xdgToplevel -> xdgSurface -> OnClosed (renderer teardown) -> surface
	expectedOrder := []string{"decoration", "xdgToplevel", "xdgSurface", "OnClosed", "surface"}
	if len(order) != len(expectedOrder) {
		t.Fatalf("unexpected destructor order: got %v, want %v", order, expectedOrder)
	}
	for i := range order {
		if order[i] != expectedOrder[i] {
			t.Fatalf("at index %d: got %s, want %s", i, order[i], expectedOrder[i])
		}
	}
}
