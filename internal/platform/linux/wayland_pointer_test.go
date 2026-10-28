//go:build linux

package linux

import (
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/internal/input"
	"github.com/vivaui/viva/internal/platform"
	"github.com/vivaui/viva/ui"
)

func TestWaylandPointerCoordinates(t *testing.T) {
	// Scale 1.0, margin 24
	win1 := &waylandWindow{margin: 24}
	win1.storeScale(1.0)

	// Origin of content box: devX = 24, devY = 24 -> (0, 0)
	x, y := win1.pointerCoords(float64ToWlFixed(24), float64ToWlFixed(24))
	if x != 0 || y != 0 {
		t.Errorf("expected (0, 0), got (%g, %g)", x, y)
	}

	// Inside content box: devX = 124, devY = 74 -> (100, 50)
	x, y = win1.pointerCoords(float64ToWlFixed(124), float64ToWlFixed(74))
	if x != 100 || y != 50 {
		t.Errorf("expected (100, 50), got (%g, %g)", x, y)
	}

	// Scale 2.0, margin 48
	win2 := &waylandWindow{margin: 48}
	win2.storeScale(2.0)

	// Origin of content box: devX = 48, devY = 48 -> (0, 0)
	x, y = win2.pointerCoords(float64ToWlFixed(48), float64ToWlFixed(48))
	if x != 0 || y != 0 {
		t.Errorf("expected (0, 0) at 2x, got (%g, %g)", x, y)
	}

	// Inside content box: devX = 248, devY = 148 -> (100, 50)
	x, y = win2.pointerCoords(float64ToWlFixed(248), float64ToWlFixed(148))
	if x != 100 || y != 50 {
		t.Errorf("expected (100, 50) at 2x, got (%g, %g)", x, y)
	}
}

func TestWaylandPointerButtonMapping(t *testing.T) {
	cases := []struct {
		evdev uint32
		want  ui.PointerButton
	}{
		{btnLeft, ui.PointerButtonPrimary},
		{btnRight, ui.PointerButtonSecondary},
		{btnMiddle, ui.PointerButtonMiddle},
		{btnSide, ui.PointerButtonBack},
		{btnExtra, ui.PointerButtonForward},
		{0x999, ui.PointerButtonNone},
	}

	for _, c := range cases {
		got := waylandPointerButton(c.evdev)
		if got != c.want {
			t.Errorf("waylandPointerButton(%x) = %v, want %v", c.evdev, got, c.want)
		}
	}
}

func TestWaylandPointerTitlebarInteraction(t *testing.T) {
	var movedSerial uint32
	var resizedSerial uint32
	var resizedEdge platform.WindowEdge
	var maximizedToggleCount int

	api := &waylandAPI{
		proxyMarshalFlags: func(proxy uintptr, opcode uint32, interfacePtr uintptr, version uint32, flags uint32, args ...any) uintptr {
			if opcode == xdgToplevelMoveOpcode && len(args) >= 2 {
				if s, ok := args[1].(uint32); ok {
					movedSerial = s
				}
			} else if opcode == xdgToplevelResizeOpcode && len(args) >= 3 {
				if s, ok := args[1].(uint32); ok {
					resizedSerial = s
				}
				if e, ok := args[2].(uint32); ok {
					resizedEdge = platform.WindowEdge(e)
				}
			}
			return 0
		},
	}
	app := &waylandApplication{
		api: api,
		globals: waylandGlobals{
			seat: 99,
		},
	}

	win := &waylandWindow{
		app:         app,
		xdgToplevel: 10,
		width:       848, // 800 + 2*24
		height:      648, // 600 + 2*24
		margin:      24,
	}
	win.storeScale(1.0)

	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			WindowMode:     platform.WindowCustomTitlebar,
			TitlebarHeight: 40,
			PressAt: func(x, y float32) platform.PressKind {
				if y < 40 && x > 700 {
					return platform.PressControl // Close/min/max buttons
				}
				if y < 40 {
					return platform.PressMove
				}
				return platform.PressClient
			},
		},
	}
	win.state = state

	// 1. Press on titlebar move area -> should trigger startMove
	handled := win.beginTitlebarInteraction(1001, 100, 20, 1000)
	if !handled || movedSerial != 1001 {
		t.Fatalf("expected titlebar drag to trigger startMove(1001), handled=%v, movedSerial=%d", handled, movedSerial)
	}

	// 2. Press on top edge resize (x=400, y=2) -> should trigger startResize
	handled = win.beginTitlebarInteraction(1002, 400, 2, 2000)
	if !handled || resizedSerial != 1002 || resizedEdge != platform.EdgeTop {
		t.Fatalf("expected edge resize to trigger startResize(1002, EdgeTop), handled=%v, resizedSerial=%d, edge=%v", handled, resizedSerial, resizedEdge)
	}

	// 3. Press on controls area (x=750, y=20) -> should NOT be intercepted by titlebar
	handled = win.beginTitlebarInteraction(1003, 750, 20, 3000)
	if handled {
		t.Fatal("press on control button should not be intercepted as move/resize")
	}

	// 4. Double-click on titlebar (time diff <= 400ms) -> should trigger toggleMaximize
	win.lastClickTime = 4000
	handled = win.beginTitlebarInteraction(1004, 200, 20, 4200)
	if !handled {
		t.Fatal("expected double-click to be handled")
	}
	_ = maximizedToggleCount
}

func TestWaylandPointerEventsDispatch(t *testing.T) {
	app := &waylandApplication{}
	win := &waylandWindow{
		app:     app,
		surface: 100,
		width:   800,
		height:  600,
	}
	win.storeScale(1.0)

	var pointerEvents []platform.PointerEvent
	var scrollEvents []platform.ScrollEvent

	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnPointer: func(event platform.PointerEvent) (platform.Frame, error) {
				pointerEvents = append(pointerEvents, event)
				return platform.Frame{}, nil
			},
			OnScroll: func(event platform.ScrollEvent) (platform.Frame, error) {
				scrollEvents = append(scrollEvents, event)
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{
		config: state.config,
		scale:  win.displayScale,
		clicks: &input.ClickCounter{Interval: 500 * time.Millisecond, Distance: 4},
	}
	win.state = state
	app.windows = []*waylandWindowState{state}

	initPointerListeners()

	// 1. Enter
	// Simulate enter via direct pointerCoords & dispatch
	x, y := win.pointerCoords(float64ToWlFixed(50), float64ToWlFixed(60))
	win.pointerX, win.pointerY = x, y
	_ = state.router.dispatchPointer(platform.PointerEvent{Kind: platform.PointerMove, X: x, Y: y})

	if len(pointerEvents) != 1 || pointerEvents[0].X != 50 || pointerEvents[0].Y != 60 {
		t.Fatalf("expected pointer event at (50, 60), got %v", pointerEvents)
	}

	// 2. Button Down
	_ = state.router.dispatchPointer(platform.PointerEvent{Kind: platform.PointerDown, Button: ui.PointerButtonPrimary, X: x, Y: y, ClickCount: 1})
	if len(pointerEvents) != 2 || pointerEvents[1].Kind != platform.PointerDown || pointerEvents[1].Button != ui.PointerButtonPrimary {
		t.Fatalf("expected PointerDown event, got %v", pointerEvents)
	}

	// 3. Scroll
	_ = state.router.dispatchScroll(platform.ScrollEvent{DeltaY: 48})
	if len(scrollEvents) != 1 || scrollEvents[0].DeltaY != 48 {
		t.Fatalf("expected scroll event with DeltaY=48, got %v", scrollEvents)
	}
}

func TestWaylandPointerScrollFrameAndV120Deduplication(t *testing.T) {
	app := &waylandApplication{
		modifiers: input.ModifierControl,
	}
	win := &waylandWindow{
		app:      app,
		surface:  101,
		pointerX: 120,
		pointerY: 240,
	}
	win.storeScale(1.0)

	var scrollEvents []platform.ScrollEvent
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnScroll: func(event platform.ScrollEvent) (platform.Frame, error) {
				scrollEvents = append(scrollEvents, event)
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{config: state.config, scale: win.displayScale}
	win.state = state
	app.windows = []*waylandWindowState{state}
	app.pointerFocus = win

	initPointerListeners()

	// axis_value120 (120 units = 1 notch = 48.0 delta)
	puregoInvokeCallback4(rawPointerListener[9], unsafe.Pointer(app), 0, 0, 120)

	// Fallback axis event in the same frame: must be ignored because axis_v120 was already processed!
	puregoInvokeCallback5(rawPointerListener[4], unsafe.Pointer(app), 0, 100, 0, float64ToWlFixed(10))

	// Before frame(): no scroll should be dispatched yet
	if len(scrollEvents) != 0 {
		t.Fatalf("expected 0 scroll events before frame(), got %d", len(scrollEvents))
	}

	// frame()
	puregoInvokeCallback2(rawPointerListener[5], unsafe.Pointer(app), 0)

	if len(scrollEvents) != 1 {
		t.Fatalf("expected exactly 1 scroll event after frame(), got %d", len(scrollEvents))
	}
	ev := scrollEvents[0]
	if ev.DeltaY != 48.0 || ev.X != 120 || ev.Y != 240 || ev.Modifiers != input.ModifierControl {
		t.Fatalf("unexpected scroll event: %+v", ev)
	}
}

func TestWaylandPointerCoordsWithScaleAndMargin(t *testing.T) {
	win := &waylandWindow{
		margin: 48, // physical margin at 2.0x scale -> 24 logical margin
	}
	win.storeScale(2.0)

	// Surface coords (24, 24) should map to content logical coords (0, 0)
	x, y := win.pointerCoords(float64ToWlFixed(24), float64ToWlFixed(24))
	if x != 0 || y != 0 {
		t.Fatalf("expected (0, 0), got (%f, %f)", x, y)
	}

	// Surface coords (124, 224) should map to content logical coords (100, 200)
	x, y = win.pointerCoords(float64ToWlFixed(124), float64ToWlFixed(224))
	if x != 100 || y != 200 {
		t.Fatalf("expected (100, 200), got (%f, %f)", x, y)
	}

	// When margin is 0 (maximized or frameless)
	win.margin = 0
	x, y = win.pointerCoords(float64ToWlFixed(50), float64ToWlFixed(60))
	if x != 50 || y != 60 {
		t.Fatalf("expected (50, 60), got (%f, %f)", x, y)
	}
}
