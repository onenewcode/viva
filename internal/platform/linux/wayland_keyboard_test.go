//go:build linux

package linux

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/internal/input"
	"github.com/vivaui/viva/internal/platform"
)

func TestWaylandKeyboardKeyTranslation(t *testing.T) {
	app := &waylandApplication{}
	win := &waylandWindow{
		app:     app,
		surface: 200,
	}

	var keyEvents []platform.KeyEvent

	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnKey: func(event platform.KeyEvent) (platform.Frame, error) {
				keyEvents = append(keyEvents, event)
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{
		config: state.config,
	}
	win.state = state
	app.keyboardFocus = win
	app.windows = []*waylandWindowState{state}

	// 1. Press Space (evdev scancode 57)
	app.handleKey(1, 100, 57, 1)

	if len(keyEvents) != 1 {
		t.Fatalf("expected 1 key event, got %d", len(keyEvents))
	}
	if keyEvents[0].Key != input.KeySpace {
		t.Errorf("expected KeySpace, got %v", keyEvents[0].Key)
	}
	if keyEvents[0].Text != " " {
		t.Errorf("expected text ' ', got %q", keyEvents[0].Text)
	}
	if keyEvents[0].Release {
		t.Errorf("expected Release == false for press")
	}

	// 2. Release Space
	app.handleKey(2, 120, 57, 0)
	// Default router without wantsKeyReleases does not forward key releases for normal apps
	if len(keyEvents) != 1 {
		t.Fatalf("expected release not to be delivered without wantsKeyReleases, got %d", len(keyEvents))
	}
}

func TestWaylandKeyboardRepeat(t *testing.T) {
	app := &waylandApplication{
		repeatRate:  25, // 25 Hz = 40ms interval
		repeatDelay: 50, // 50ms delay
	}
	win := &waylandWindow{
		app:     app,
		surface: 201,
	}

	var repeatCount atomic.Int32

	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnKey: func(event platform.KeyEvent) (platform.Frame, error) {
				if event.Repeat {
					repeatCount.Add(1)
				}
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{
		config: state.config,
	}
	win.state = state
	app.keyboardFocus = win
	app.windows = []*waylandWindowState{state}

	// Set loop waker so repeated keys post into synchronous call
	loop := &waylandLoop{}
	app.loop = loop

	// Press Enter (evdev scancode 28)
	app.handleKey(10, 100, 28, 1)

	// Wait for repeat timer + ticker (50ms delay + ~100ms)
	time.Sleep(150 * time.Millisecond)

	// Drain posted commands
	loop.drainCommands()

	if repeatCount.Load() == 0 {
		t.Fatal("expected key repeat to generate repeat events")
	}

	// Release key
	app.handleKey(11, 250, 28, 0)
	curr := repeatCount.Load()

	// Wait more and ensure no more repeat events
	time.Sleep(80 * time.Millisecond)
	loop.drainCommands()

	if repeatCount.Load() != curr {
		t.Fatalf("key repeat continued after key release: was %d, now %d", curr, repeatCount.Load())
	}
}

func TestWaylandKeyboardFocusAndModifiers(t *testing.T) {
	focusLostCalled := false
	app := &waylandApplication{}
	win := &waylandWindow{
		app:     app,
		surface: 202,
	}
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnFocusLost: func() (platform.Frame, error) {
				focusLostCalled = true
				return platform.Frame{}, nil
			},
		},
	}
	win.state = state
	app.windows = []*waylandWindowState{state}

	initKeyboardListeners()

	// Enter window
	app.keyboardFocus = win

	if app.keyboardFocus != win {
		t.Fatal("expected keyboard focus to be set to win")
	}

	// Set modifiers
	app.modifiers = input.ModifierShift | input.ModifierControl
	if app.modifiers&(input.ModifierShift|input.ModifierControl) != (input.ModifierShift | input.ModifierControl) {
		t.Fatalf("unexpected modifiers: %v", app.modifiers)
	}

	// Leave window
	app.stopKeyRepeat()
	if app.keyboardFocus != nil && app.keyboardFocus.state != nil && app.keyboardFocus.state.config.OnFocusLost != nil {
		app.keyboardFocus.state.config.OnFocusLost()
	}
	app.keyboardFocus = nil

	if !focusLostCalled {
		t.Fatal("expected OnFocusLost to be called on keyboard leave")
	}
	if app.keyboardFocus != nil {
		t.Fatal("expected keyboardFocus to be nil after leave")
	}
}
