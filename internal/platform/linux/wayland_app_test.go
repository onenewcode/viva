//go:build linux

package linux

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/internal/platform"
)

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
