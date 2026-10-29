//go:build linux

package linux

import (
	"errors"
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

func TestWaylandInitialConfigureGeometryBeforeState(t *testing.T) {
	win := &waylandWindow{
		surface:       1,
		xdgSurface:    2,
		width:         400,
		height:        300,
		savedLogicalW: 400,
		savedLogicalH: 300,
	}
	win.storeScale(1)

	win.handleXdgToplevelConfigure(800, 600, nil)
	win.handleXdgSurfaceConfigure(99)

	if win.width != 800 || win.height != 600 {
		t.Fatalf("initial configure geometry = %dx%d, want 800x600", win.width, win.height)
	}
	if !win.geometryDirty {
		t.Fatal("initial configure did not mark the corrected geometry dirty")
	}
	if win.pendingSerial != 99 {
		t.Fatal("initial configure serial was consumed before the first buffer was ready")
	}
	if !win.initialResizePending {
		t.Fatal("initial configure did not request an application resize")
	}

	var reported platform.Size
	win.state = &waylandWindowState{
		win: win,
		config: platform.Config{
			OnResize: func(size platform.Size) (platform.Frame, error) {
				reported = size
				return platform.Frame{}, nil
			},
		},
	}
	if _, resized, err := win.takeInitialResizeFrame(); err != nil || !resized {
		t.Fatalf("initial resize frame: resized=%v err=%v", resized, err)
	}
	if reported.Width != 800 || reported.Height != 600 {
		t.Fatalf("application initial size = %gx%g, want 800x600", reported.Width, reported.Height)
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

func TestWaylandHiddenRedrawWaitsForShow(t *testing.T) {
	app := &waylandApplication{}
	win := &waylandWindow{
		app:         app,
		surface:     1,
		hidden:      true,
		needsRedraw: true,
	}
	redraws := 0
	presents := 0
	state := &waylandWindowState{
		app:      app,
		win:      win,
		renderer: &vulkanRenderer{},
		config: platform.Config{
			OnRedraw: func() (platform.Frame, error) {
				redraws++
				return platform.Frame{}, nil
			},
		},
	}
	state.router.present = func(frame platform.Frame) error {
		presents++
		return nil
	}
	win.state = state

	win.renderFrame()
	if redraws != 0 || presents != 0 {
		t.Fatalf("hidden redraw called OnRedraw %d times and present %d times", redraws, presents)
	}
	if !win.needsRedraw {
		t.Fatal("hidden redraw discarded the pending content update")
	}

	win.Show()
	if redraws != 1 || presents != 1 {
		t.Fatalf("Show called OnRedraw %d times and present %d times, want one each", redraws, presents)
	}
}

func TestWaylandRedrawErrorsReachApplication(t *testing.T) {
	callbackErr := errors.New("redraw callback failed")
	routerErr := errors.New("scene upload failed")
	tests := []struct {
		name     string
		renderer *vulkanRenderer
		onRedraw func() (platform.Frame, error)
		present  func(platform.Frame) error
		want     error
	}{
		{
			name:     "callback",
			renderer: &vulkanRenderer{},
			onRedraw: func() (platform.Frame, error) {
				return platform.Frame{}, callbackErr
			},
			want: callbackErr,
		},
		{
			name:     "router",
			renderer: &vulkanRenderer{},
			onRedraw: func() (platform.Frame, error) {
				return platform.Frame{}, nil
			},
			present: func(platform.Frame) error { return routerErr },
			want:    routerErr,
		},
		{
			name: "renderer",
			renderer: &vulkanRenderer{api: &vulkanAPI{
				waitForFences: func(uintptr, uint32, *uintptr, uint32, uint64) int32 {
					return vkErrorDeviceLost
				},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := &waylandApplication{}
			win := &waylandWindow{app: app, surface: 1}
			win.state = &waylandWindowState{
				app:      app,
				win:      win,
				renderer: test.renderer,
				config:   platform.Config{OnRedraw: test.onRedraw},
			}
			win.state.router.present = test.present

			win.renderFrame()
			if app.firstErr == nil {
				t.Fatal("redraw error did not reach the application")
			}
			if test.want != nil && !errors.Is(app.firstErr, test.want) {
				t.Fatalf("application error = %v, want %v", app.firstErr, test.want)
			}
		})
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

func TestWaylandProtocolSignatures(t *testing.T) {
	tests := []struct {
		name     string
		msg      wlMessage
		wantName string
		wantSig  string
	}{
		{"xdg_toplevel.set_parent", xdgToplevelRequests[1], "set_parent", "?o"},
		{"xdg_toplevel.show_window_menu", xdgToplevelRequests[4], "show_window_menu", "ouii"},
		{"xdg_toplevel.move", xdgToplevelRequests[5], "move", "ou"},
		{"xdg_toplevel.resize", xdgToplevelRequests[6], "resize", "ouu"},
		{"xdg_toplevel.set_fullscreen", xdgToplevelRequests[11], "set_fullscreen", "?o"},
		{"xdg_surface.get_popup", xdgSurfaceRequests[2], "get_popup", "n?oo"},
		{"wp_viewport.set_source", wpViewportRequests[1], "set_source", "ffff"},
		{"wl_pointer.set_cursor", wlPointerRequests[0], "set_cursor", "u?oii"},
		{"wl_data_device.start_drag", wlDataDeviceRequests[0], "start_drag", "?oo?ou"},
		{"wl_data_device.set_selection", wlDataDeviceRequests[1], "set_selection", "?ou"},
	}

	for _, tt := range tests {
		gotName := waylandGoString(tt.msg.Name)
		gotSig := waylandGoString(tt.msg.Signature)
		if gotName != tt.wantName {
			t.Errorf("%s: got name %q, want %q", tt.name, gotName, tt.wantName)
		}
		if gotSig != tt.wantSig {
			t.Errorf("%s: got signature %q, want %q", tt.name, gotSig, tt.wantSig)
		}
	}
}

func TestWaylandViewportDestinationSync(t *testing.T) {
	var destViewport uintptr
	var destW, destH int32

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if opcode == 2 && proxy == 0xbeef && len(args) >= 2 { // wp_viewport.set_destination
				destViewport = proxy
				if w, ok := args[0].(int32); ok {
					destW = w
				}
				if h, ok := args[1].(int32); ok {
					destH = h
				}
			}
			return 0
		},
	}
	app := &waylandApplication{api: api}

	// 1. Initial size at 1.25x scaling with margin (custom titlebar)
	// Logical content: 800x600. Scale: 1.25.
	// contentW = round(800 * 1.25) = 1000
	// contentH = round(600 * 1.25) = 750
	// margin = frameMargin(1.25) = round(24 * 1.25) = 30
	// totalW = 1000 + 60 = 1060, totalH = 750 + 60 = 810
	win := &waylandWindow{
		app:      app,
		viewport: 0xbeef,
		width:    1060,
		height:   810,
		margin:   30,
	}
	win.storeScale(1.25)

	win.updateViewportDestination()
	if destViewport != 0xbeef || destW != 848 || destH != 648 {
		t.Fatalf("expected viewport destination (848, 648) at 1.25x with margin, got (%d, %d)", destW, destH)
	}

	// 2. Maximized window at 1.25x (no margin)
	// totalW = 1250, totalH = 1000
	win.margin = 0
	win.width = 1250
	win.height = 1000
	win.updateViewportDestination()
	if destW != 1000 || destH != 800 {
		t.Fatalf("expected viewport destination (1000, 800) at 1.25x maximized, got (%d, %d)", destW, destH)
	}

	// 3. Configure event with new dimensions
	win.pendingWidth = 1200 // new logical width
	win.pendingHeight = 900 // new logical height
	win.handleXdgSurfaceConfigure(42)

	expectedLogicalW := int32(math.Round(float64(float32(win.width) / 1.25)))
	expectedLogicalH := int32(math.Round(float64(float32(win.height) / 1.25)))
	if destW != expectedLogicalW || destH != expectedLogicalH {
		t.Fatalf("expected configure to sync viewport destination to (%d, %d), got (%d, %d)", expectedLogicalW, expectedLogicalH, destW, destH)
	}
}

func TestWaylandAtomicConfigureResizeSequence(t *testing.T) {
	type protocolEvent struct {
		name string
		val  any
	}
	var events []protocolEvent

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			switch opcode {
			case xdgSurfaceAckConfigureOpcode:
				if len(args) > 0 {
					events = append(events, protocolEvent{name: "ack_configure", val: args[0]})
				}
			case xdgSurfaceSetWindowGeometryOpcode:
				if len(args) >= 4 {
					events = append(events, protocolEvent{name: "set_window_geometry", val: [4]int32{args[0].(int32), args[1].(int32), args[2].(int32), args[3].(int32)}})
				}
			case 2: // wp_viewport.set_destination
				if len(args) >= 2 {
					events = append(events, protocolEvent{name: "set_destination", val: [2]int32{args[0].(int32), args[1].(int32)}})
				}
			case wlSurfaceCommitOpcode:
				events = append(events, protocolEvent{name: "surface_commit", val: proxy})
			}
			return 0
		},
		proxyAddListener: func(proxy uintptr, implementation uintptr, data unsafe.Pointer) int32 {
			return 0
		},
		displayFlush: func(display uintptr) int32 {
			events = append(events, protocolEvent{name: "display_flush", val: nil})
			return 0
		},
	}

	app := &waylandApplication{api: api, display: 0x111}
	win := &waylandWindow{
		app:        app,
		surface:    0x222,
		xdgSurface: 0x333,
		viewport:   0x444,
		width:      800,
		height:     600,
		margin:     0,
	}
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnResize: func(size platform.Size) (platform.Frame, error) {
				events = append(events, protocolEvent{name: "on_resize", val: size})
				return platform.Frame{}, nil
			},
		},
	}
	win.state = state

	// Simulate compositor configuring a top-left resize from 800x600 down to 780x570
	win.pendingWidth = 780
	win.pendingHeight = 570
	win.handleXdgSurfaceConfigure(9999)

	// Verify order:
	// 1. ack_configure must occur before surface_commit
	// 2. set_window_geometry must reflect new size (780, 570) before surface_commit
	// 3. Exactly one commit per configure event!
	ackIdx := -1
	geomIdx := -1
	commitIdx := -1
	commitCount := 0

	for i, ev := range events {
		switch ev.name {
		case "ack_configure":
			if ackIdx == -1 {
				ackIdx = i
			}
			if ev.val != uint32(9999) {
				t.Errorf("expected ack serial 9999, got %v", ev.val)
			}
		case "set_window_geometry":
			if geomIdx == -1 {
				geomIdx = i
			}
			geom := ev.val.([4]int32)
			if geom[2] != 780 || geom[3] != 570 {
				t.Errorf("expected set_window_geometry width=780, height=570, got %+v", geom)
			}
		case "surface_commit":
			commitCount++
			if commitIdx == -1 {
				commitIdx = i
			}
		}
	}

	if ackIdx == -1 {
		t.Fatal("expected ack_configure event")
	}
	if geomIdx == -1 {
		t.Fatal("expected set_window_geometry event")
	}
	if commitIdx == -1 {
		t.Fatal("expected surface_commit event")
	}
	if ackIdx > commitIdx {
		t.Errorf("ack_configure (index %d) occurred after surface_commit (index %d)", ackIdx, commitIdx)
	}
	if geomIdx > commitIdx {
		t.Errorf("set_window_geometry (index %d) occurred after surface_commit (index %d)", geomIdx, commitIdx)
	}
	if commitCount != 1 {
		t.Errorf("expected exactly 1 commit for resize configure, got %d", commitCount)
	}
}

func TestWaylandImmediateAtomicResizeAndConfigureAck(t *testing.T) {
	type protocolEvent struct {
		name string
		val  any
	}
	var events []protocolEvent
	var nextCallbackId uintptr = 0x555

	const (
		mockSurface    = 0x222
		mockXdgSurface = 0x333
	)

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if proxy == mockXdgSurface {
				switch opcode {
				case xdgSurfaceAckConfigureOpcode:
					if len(args) > 0 {
						events = append(events, protocolEvent{name: "ack_configure", val: args[0]})
					}
				case xdgSurfaceSetWindowGeometryOpcode:
					if len(args) >= 4 {
						events = append(events, protocolEvent{name: "set_window_geometry", val: [4]int32{args[0].(int32), args[1].(int32), args[2].(int32), args[3].(int32)}})
					}
				}
			} else if proxy == mockSurface {
				switch opcode {
				case wlSurfaceFrameOpcode:
					id := nextCallbackId
					nextCallbackId++
					events = append(events, protocolEvent{name: "surface_frame", val: id})
					return id
				case wlSurfaceCommitOpcode:
					events = append(events, protocolEvent{name: "surface_commit", val: proxy})
				}
			}
			return 0
		},
		proxyAddListener: func(proxy uintptr, implementation uintptr, data unsafe.Pointer) int32 {
			return 0
		},
		proxyDestroy: func(proxy uintptr) {
			events = append(events, protocolEvent{name: "proxy_destroy", val: proxy})
		},
		displayFlush: func(display uintptr) int32 {
			events = append(events, protocolEvent{name: "display_flush", val: nil})
			return 0
		},
	}

	app := &waylandApplication{api: api, display: 0x111}
	win := &waylandWindow{
		app:        app,
		surface:    mockSurface,
		xdgSurface: mockXdgSurface,
		viewport:   0x444,
		width:      800,
		height:     600,
		margin:     0,
	}
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnResize: func(size platform.Size) (platform.Frame, error) {
				events = append(events, protocolEvent{name: "on_resize", val: size})
				return platform.Frame{}, nil
			},
		},
	}
	win.state = state

	// 1. Initial resize configure (500x400, serial 101)
	win.pendingWidth = 500
	win.pendingHeight = 400
	win.handleXdgSurfaceConfigure(101)

	// Since framePending was false, this first configure triggers applyPendingResize immediately
	if !win.framePending {
		t.Fatal("expected framePending to be true after first configure commit")
	}
	if win.width != 500 || win.height != 400 {
		t.Fatalf("expected width=500, height=400, got (%d, %d)", win.width, win.height)
	}

	// Count commits so far
	commitsAfterFirst := 0
	for _, ev := range events {
		if ev.name == "surface_commit" {
			commitsAfterFirst++
		}
	}
	if commitsAfterFirst != 1 {
		t.Fatalf("expected 1 commit after first configure, got %d", commitsAfterFirst)
	}

	// 2. Interactive configure event 2 arrives (520x410, serial 102) -> immediately acknowledged and applied
	win.pendingWidth = 520
	win.pendingHeight = 410
	win.handleXdgSurfaceConfigure(102)

	if win.width != 520 || win.height != 410 {
		t.Fatalf("expected width=520, height=410, got (%d, %d)", win.width, win.height)
	}

	// 3. Interactive configure event 3 arrives (540x420, serial 103) -> immediately acknowledged and applied
	win.pendingWidth = 540
	win.pendingHeight = 420
	win.handleXdgSurfaceConfigure(103)

	if win.width != 540 || win.height != 420 {
		t.Fatalf("expected width=540, height=420, got (%d, %d)", win.width, win.height)
	}

	// Verify all 3 configure events were acknowledged in sequence and committed with matching geometry
	var acks []uint32
	var geoms [][4]int32
	totalCommits := 0
	for _, ev := range events {
		switch ev.name {
		case "surface_commit":
			totalCommits++
		case "ack_configure":
			acks = append(acks, ev.val.(uint32))
		case "set_window_geometry":
			geoms = append(geoms, ev.val.([4]int32))
		}
	}

	if totalCommits != 3 {
		t.Fatalf("expected exactly 3 commits for 3 interactive configure events, got %d", totalCommits)
	}
	if len(acks) != 3 || acks[0] != 101 || acks[1] != 102 || acks[2] != 103 {
		t.Fatalf("expected acks [101, 102, 103], got %v", acks)
	}
	if len(geoms) != 3 || geoms[2][2] != 540 || geoms[2][3] != 420 {
		t.Fatalf("expected latest geometry (540, 420), got %+v", geoms)
	}
}

func TestWaylandStartResizeFlushesDisplay(t *testing.T) {
	flushed := false
	resized := false
	moved := false

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			switch opcode {
			case xdgToplevelResizeOpcode:
				resized = true
			case xdgToplevelMoveOpcode:
				moved = true
			}
			return 0
		},
		displayFlush: func(display uintptr) int32 {
			flushed = true
			return 0
		},
	}
	app := &waylandApplication{
		api:     api,
		display: 0x111,
		globals: waylandGlobals{seat: 0x222},
	}
	win := &waylandWindow{
		app:         app,
		xdgToplevel: 0x333,
	}

	flushed = false
	win.startResize(42, platform.EdgeBottomRight)
	if !resized || !flushed {
		t.Fatalf("startResize: resized=%v, flushed=%v; want true, true", resized, flushed)
	}

	flushed = false
	win.startMove(43)
	if !moved || !flushed {
		t.Fatalf("startMove: moved=%v, flushed=%v; want true, true", moved, flushed)
	}
}

func TestWaylandRestoreToSavedFloatingSize(t *testing.T) {
	var (
		resizes []platform.Size
		commits int
		acks    []uint32
	)

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if proxy == 0x222 { // xdgSurface
				if opcode == xdgSurfaceAckConfigureOpcode && len(args) > 0 {
					acks = append(acks, args[0].(uint32))
				}
			} else if proxy == 0x111 { // surface
				if opcode == wlSurfaceCommitOpcode {
					commits++
				}
			}
			return 0
		},
		proxyAddListener: func(proxy uintptr, implementation uintptr, data unsafe.Pointer) int32 { return 0 },
		proxyDestroy:     func(proxy uintptr) {},
		displayFlush:     func(display uintptr) int32 { return 0 },
	}

	app := &waylandApplication{api: api, display: 0x99}
	win := &waylandWindow{
		app:           app,
		surface:       0x111,
		xdgSurface:    0x222,
		xdgToplevel:   0x333,
		viewport:      0x444,
		width:         548,
		height:        448,
		margin:        24,
		savedLogicalW: 500,
		savedLogicalH: 400,
		frameWanted:   true,
	}
	win.storeScale(1.0)
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnResize: func(size platform.Size) (platform.Frame, error) {
				resizes = append(resizes, size)
				return platform.Frame{}, nil
			},
		},
	}
	win.state = state

	// 1. Toplevel configure: Maximized state at 1920x1080
	statesData := []uint32{xdgToplevelStateMaximized}
	statesArray := wlArray{
		Size:  uintptr(len(statesData) * 4),
		Alloc: uintptr(len(statesData) * 4),
		Data:  unsafe.Pointer(&statesData[0]),
	}
	win.handleXdgToplevelConfigure(1920, 1080, &statesArray)
	win.handleXdgSurfaceConfigure(501)

	if !win.maximized {
		t.Fatal("expected window to be maximized")
	}
	if win.margin != 0 {
		t.Fatalf("expected margin = 0 when maximized, got %d", win.margin)
	}
	if win.width != 1920 || win.height != 1080 {
		t.Fatalf("expected maximized dimensions (1920, 1080), got (%d, %d)", win.width, win.height)
	}
	if win.savedLogicalW != 500 || win.savedLogicalH != 400 {
		t.Fatalf("expected saved floating size (500, 400), got (%g, %g)", win.savedLogicalW, win.savedLogicalH)
	}

	// 2. Unmaximize: compositor sends (0, 0)
	emptyStates := wlArray{}
	win.handleXdgToplevelConfigure(0, 0, &emptyStates)

	if win.maximized {
		t.Fatal("expected window to be unmaximized")
	}
	if win.margin != 24 {
		t.Fatalf("expected margin restored to 24, got %d", win.margin)
	}

	resizesBefore := len(resizes)
	win.handleXdgSurfaceConfigure(502)

	if win.width != 548 || win.height != 448 {
		t.Fatalf("expected restored dimensions (548, 448), got (%d, %d)", win.width, win.height)
	}
	if len(resizes) != resizesBefore+1 {
		t.Fatalf("expected OnResize called on restore, got %d calls", len(resizes)-resizesBefore)
	}
	lastResize := resizes[len(resizes)-1]
	if lastResize.Width != 500 || lastResize.Height != 400 {
		t.Fatalf("expected restored content size (500, 400), got %+v", lastResize)
	}
	if logical := win.logicalSize(); logical.Width != 500 || logical.Height != 400 {
		t.Fatalf("expected win.logicalSize() = (500, 400), got %+v", logical)
	}
}

func TestWaylandFractionalScaleTransitionsAcrossOutputs(t *testing.T) {
	var viewportDests [][2]int32
	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if proxy == 0x444 && opcode == 2 { // wp_viewport.set_destination
				if len(args) >= 2 {
					viewportDests = append(viewportDests, [2]int32{args[0].(int32), args[1].(int32)})
				}
			}
			return 0
		},
		proxyAddListener: func(proxy uintptr, implementation uintptr, data unsafe.Pointer) int32 { return 0 },
		proxyDestroy:     func(proxy uintptr) {},
		displayFlush:     func(display uintptr) int32 { return 0 },
	}

	app := &waylandApplication{api: api, display: 0x99}
	win := &waylandWindow{
		app:           app,
		surface:       0x111,
		xdgSurface:    0x222,
		viewport:      0x444,
		width:         448, // 400 + 2*24
		height:        348, // 300 + 2*24
		margin:        24,
		savedLogicalW: 400,
		savedLogicalH: 300,
		frameWanted:   true,
	}
	win.storeScale(1.0)
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnResize: func(size platform.Size) (platform.Frame, error) {
				return platform.Frame{}, nil
			},
		},
	}
	win.state = state

	transitions := []float32{1.25, 1.75, 2.0, 1.0}
	for _, targetScale := range transitions {
		win.applyScale(targetScale)

		if got := win.displayScale(); got != targetScale {
			t.Fatalf("displayScale = %g, want %g", got, targetScale)
		}

		wantMargin := frameMargin(targetScale)
		if win.margin != wantMargin {
			t.Fatalf("scale %g: margin = %d, want %d", targetScale, win.margin, wantMargin)
		}

		expectedContentW := deviceLength(400, targetScale)
		expectedContentH := deviceLength(300, targetScale)
		actualContentW := contentSize(win.width, win.margin)
		actualContentH := contentSize(win.height, win.margin)
		if actualContentW != expectedContentW || actualContentH != expectedContentH {
			t.Fatalf("scale %g: content size = (%d, %d), want (%d, %d)", targetScale, actualContentW, actualContentH, expectedContentW, expectedContentH)
		}

		logical := win.logicalSize()
		if math.Abs(float64(logical.Width-400)) > 1.0 || math.Abs(float64(logical.Height-300)) > 1.0 {
			t.Fatalf("scale %g: logical size = %+v, want ~(400, 300)", targetScale, logical)
		}

		if len(viewportDests) == 0 {
			t.Fatalf("scale %g: expected viewport destination update", targetScale)
		}
		latestDest := viewportDests[len(viewportDests)-1]
		wantTotalLogicalW := int32(math.Round(float64(float32(win.width) / targetScale)))
		wantTotalLogicalH := int32(math.Round(float64(float32(win.height) / targetScale)))
		if latestDest[0] != wantTotalLogicalW || latestDest[1] != wantTotalLogicalH {
			t.Fatalf("scale %g: viewport destination = (%d, %d), want (%d, %d)", targetScale, latestDest[0], latestDest[1], wantTotalLogicalW, wantTotalLogicalH)
		}
	}
}
