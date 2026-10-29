//go:build linux

package linux

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/internal/dbus"
	"github.com/vivaui/viva/internal/platform"
)

type fakeWaylandInputMethod struct {
	fd         int
	err        error
	pending    int
	closeCalls int
}

func (bus *fakeWaylandInputMethod) FD() int         { return bus.fd }
func (bus *fakeWaylandInputMethod) Err() error      { return bus.err }
func (bus *fakeWaylandInputMethod) Dispatch() error { return bus.err }
func (bus *fakeWaylandInputMethod) Close() {
	bus.closeCalls++
	bus.pending = 0
	bus.fd = -1
}

func TestWaylandAddStatusItemContract(t *testing.T) {
	app := &waylandApplication{}
	_, err := app.AddStatusItem(platform.StatusItemConfig{})
	if err == nil {
		t.Fatal("expected error from Wayland AddStatusItem, got nil")
	}
	if !errors.Is(err, platform.ErrStatusAreaUnavailable) {
		t.Fatalf("expected error wrapping platform.ErrStatusAreaUnavailable, got: %v", err)
	}
}

func TestWaylandQuitBypassesCloseRequestAndClosesAllWindows(t *testing.T) {
	app := &waylandApplication{}

	var closeRequestCalled atomic.Int32
	var win1Closed atomic.Int32
	var win2Closed atomic.Int32

	win1 := &waylandWindow{app: app}
	state1 := &waylandWindowState{
		app: app,
		win: win1,
		config: platform.Config{
			OnCloseRequest: func(closeWindow func()) {
				closeRequestCalled.Add(1)
				// deliberately refuse to close
			},
			OnClosed: func() {
				win1Closed.Add(1)
			},
		},
	}
	win1.state = state1

	win2 := &waylandWindow{app: app}
	state2 := &waylandWindowState{
		app: app,
		win: win2,
		config: platform.Config{
			OnCloseRequest: func(closeWindow func()) {
				closeRequestCalled.Add(1)
			},
			OnClosed: func() {
				win2Closed.Add(1)
			},
		},
	}
	win2.state = state2

	app.windows = []*waylandWindowState{state1, state2}

	// Quit must close all windows and trigger OnClosed without asking OnCloseRequest
	app.Quit()

	if req := closeRequestCalled.Load(); req != 0 {
		t.Errorf("expected 0 close requests called, got %d", req)
	}
	if c1 := win1Closed.Load(); c1 != 1 {
		t.Errorf("expected win1 OnClosed called 1 time, got %d", c1)
	}
	if c2 := win2Closed.Load(); c2 != 1 {
		t.Errorf("expected win2 OnClosed called 1 time, got %d", c2)
	}
	if len(app.windows) != 0 {
		t.Errorf("expected app.windows to be empty, got %d windows", len(app.windows))
	}
}

func TestWaylandStatusItemDrainRemovals(t *testing.T) {
	app := &waylandApplication{}
	notifier := &statusNotifier{
		items: map[string]*linuxStatusItem{
			"item1": {name: "item1"},
			"item2": {name: "item2"},
		},
		removals: []string{"item1"},
	}
	app.status = notifier

	app.drainStatusRemovals()

	if _, exists := notifier.items["item1"]; exists {
		t.Error("expected item1 to be removed from notifier")
	}
	if _, exists := notifier.items["item2"]; !exists {
		t.Error("expected item2 to remain in notifier")
	}
}

func TestWaylandWindowContentOrigin(t *testing.T) {
	win := &waylandWindow{
		margin: 48,
	}
	win.storeScale(2.0)

	originX, originY := win.contentOrigin()
	if originX != 24 || originY != 24 {
		t.Fatalf("expected content origin (24, 24), got (%d, %d)", originX, originY)
	}

	win.margin = 0
	win.storeScale(1.0)
	originX, originY = win.contentOrigin()
	if originX != 0 || originY != 0 {
		t.Fatalf("expected content origin (0, 0), got (%d, %d)", originX, originY)
	}
}

func TestWaylandAtspiReconnectFailureKeepsWatcherPolling(t *testing.T) {
	app := &waylandApplication{loop: &waylandLoop{}}
	host := &atspiHost{watcher: &dbus.Conn{}}
	app.atspi = host
	address := "unix:path=" + t.TempDir() + "/missing-atspi-bus"

	for range 2 {
		app.connectAtspi(host, address)
		if host.watcher == nil {
			t.Fatal("failed reconnect discarded the AT-SPI watcher")
		}
		if len(app.loop.extraFDs) != 1 || app.loop.extraFDs[0] != int32(host.watcher.FD()) {
			t.Fatalf("watcher poll FDs = %v, want one restored descriptor", app.loop.extraFDs)
		}
		if len(app.loop.extraCBs) != 1 || app.loop.extraCBs[0] == nil {
			t.Fatal("watcher dispatch callback was not restored")
		}
	}
}

func TestWaylandInputMethodFDRemovalUsesRegistration(t *testing.T) {
	loop := &waylandLoop{}
	app := &waylandApplication{loop: loop, ibusFD: 44, ibusFDSet: true}
	loop.addExtraFD(44, func() {})

	app.removeInputMethodFD()
	if len(loop.extraFDs) != 0 || app.ibusFDSet {
		t.Fatalf("registered IBus FD survived removal: FDs=%v set=%v", loop.extraFDs, app.ibusFDSet)
	}
	app.removeInputMethodFD()
	if len(loop.extraFDs) != 0 {
		t.Fatal("repeated IBus removal changed the poll set")
	}
}

func TestWaylandCloseReleasesInputMethod(t *testing.T) {
	loop := &waylandLoop{}
	bus := &fakeWaylandInputMethod{fd: 44, pending: 2}
	app := &waylandApplication{loop: loop, ibus: bus, ibusFD: 44, ibusFDSet: true}
	loop.addExtraFD(44, func() {})

	app.close()
	if bus.closeCalls != 1 || bus.pending != 0 {
		t.Fatalf("IBus close calls=%d pending=%d", bus.closeCalls, bus.pending)
	}
	if len(loop.extraFDs) != 0 || app.ibus != nil || app.ibusFDSet {
		t.Fatalf("IBus teardown left state: FDs=%v bus=%v FDSet=%v", loop.extraFDs, app.ibus != nil, app.ibusFDSet)
	}
}

func TestWaylandAtspiServeAfterSelfClose(t *testing.T) {
	loop := &waylandLoop{}
	conn := &fakeAtspiTransport{fd: 42}
	bridge := newAtspiBridge(conn)
	host := &atspiHost{bridge: bridge}
	app := &waylandApplication{loop: loop, atspi: host}
	app.addAtspiFD(host)
	bridge.close()

	app.serveAtspi()
	if host.bridge != nil || len(loop.extraFDs) != 0 || host.waylandFDSet {
		t.Fatalf("self-closed AT-SPI state bridge=%v FDs=%v FDSet=%v", host.bridge != nil, loop.extraFDs, host.waylandFDSet)
	}
	// A second readiness notification is a no-op, not another failure dispatch.
	app.serveAtspi()
}

func TestWaylandAtspiReplyFailuresRemoveFDAndStopBatch(t *testing.T) {
	for _, test := range []struct {
		name          string
		path          string
		replyErr      error
		replyErrorErr error
	}{
		{name: "normal reply", path: atspiRootPath, replyErr: errors.New("reply write failed")},
		{name: "error reply", path: "/missing", replyErrorErr: errors.New("error reply write failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			loop := &waylandLoop{}
			call := &dbus.Message{Type: dbus.TypeCall, Path: test.path, Interface: atspiIntrospect, Member: "Introspect"}
			conn := &fakeAtspiTransport{
				messages: []*dbus.Message{call, call}, fd: 42,
				replyErr: test.replyErr, replyErrorErr: test.replyErrorErr,
			}
			host := &atspiHost{bridge: newAtspiBridge(conn)}
			app := &waylandApplication{loop: loop, atspi: host}
			app.addAtspiFD(host)

			app.serveAtspi()
			if conn.replyCalls+conn.replyErrorCalls != 1 {
				t.Fatalf("batch dispatched %d replies, want 1", conn.replyCalls+conn.replyErrorCalls)
			}
			if host.bridge != nil || len(loop.extraFDs) != 0 || host.waylandFDSet {
				t.Fatalf("failed reply left AT-SPI registered: bridge=%v FDs=%v FDSet=%v", host.bridge != nil, loop.extraFDs, host.waylandFDSet)
			}
		})
	}
}

func TestWaylandAtspiFDRemovalAfterConnectionClose(t *testing.T) {
	loop := &waylandLoop{}
	app := &waylandApplication{loop: loop}
	host := &atspiHost{watcher: &dbus.Conn{}}
	app.addAtspiFD(host)
	if !host.waylandFDSet || len(loop.extraFDs) != 1 {
		t.Fatalf("AT-SPI registration FDs=%v set=%v", loop.extraFDs, host.waylandFDSet)
	}

	// A failed service may clear its connection before the application cleans up.
	host.watcher = nil
	app.removeAtspiFD(host)
	if len(loop.extraFDs) != 0 || host.waylandFDSet {
		t.Fatalf("closed AT-SPI FD survived removal: FDs=%v set=%v", loop.extraFDs, host.waylandFDSet)
	}
}

func TestWaylandAtspiLifecycle(t *testing.T) {
	app := &waylandApplication{}
	win := &waylandWindow{app: app}
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			Title: "Wayland Atspi Window",
			Accessibility: []platform.AccessibilityNode{
				{Role: platform.A11yRoleButton, Label: "Click me"},
			},
		},
	}
	win.state = state

	app.atspiOpened(state)
	if app.atspi != nil {
		app.atspiFrame(state, []platform.AccessibilityNode{
			{Role: platform.A11yRoleButton, Label: "Clicked"},
		})
		app.atspiActive(state, true)
		app.atspiActive(state, false)
		app.atspiClosed(state)
	}
	app.atspiClose()
	if app.atspi != nil {
		t.Error("expected app.atspi to be nil after atspiClose")
	}
}

func TestWaylandRunReturnsFirstApplicationError(t *testing.T) {
	expectedErr := errors.New("first presentation failure")
	app := &waylandApplication{}
	app.fail(expectedErr)
	app.fail(errors.New("later failure"))

	err := app.Run(func(platform.Application) error { return nil })
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run error = %v, want first error %v", err, expectedErr)
	}
}

func TestWaylandRunReturnsLoopError(t *testing.T) {
	expectedErr := errors.New("wayland transport failed")
	app := &waylandApplication{loop: &waylandLoop{err: expectedErr}}

	err := app.Run(func(platform.Application) error { return nil })
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run error = %v, want %v", err, expectedErr)
	}
}

func TestWaylandRunClosesWindowsOnStartError(t *testing.T) {
	app := &waylandApplication{}

	var winClosed atomic.Int32

	expectedErr := errors.New("custom start failure")
	err := app.Run(func(a platform.Application) error {
		win := &waylandWindow{app: app}
		state := &waylandWindowState{
			app: app,
			win: win,
			config: platform.Config{
				OnClosed: func() {
					winClosed.Add(1)
				},
			},
		}
		win.state = state
		app.windows = append(app.windows, state)
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
	if winClosed.Load() != 1 {
		t.Errorf("expected OnClosed to be called on start error, got %d", winClosed.Load())
	}
	if len(app.windows) != 0 {
		t.Errorf("expected app.windows to be empty, got %d", len(app.windows))
	}
}
