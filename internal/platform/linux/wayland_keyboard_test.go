//go:build linux

package linux

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/internal/ibus"
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
	now := time.Unix(100, 0)
	app := &waylandApplication{
		repeatRate:  25, // 25 Hz = 40ms interval
		repeatDelay: 50,
		now:         func() time.Time { return now },
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
	state.router = eventRouter{config: state.config}
	win.state = state
	app.keyboardFocus = win
	app.windows = []*waylandWindowState{state}

	app.handleKey(10, 100, 28, 1)
	if timeout := app.pollTimeout(now); timeout != 50 {
		t.Fatalf("initial repeat timeout = %dms, want 50ms", timeout)
	}
	app.runKeyRepeat(now.Add(49 * time.Millisecond))
	if repeatCount.Load() != 0 {
		t.Fatal("repeat fired before its initial delay")
	}

	now = now.Add(50 * time.Millisecond)
	app.runKeyRepeat(now)
	if repeatCount.Load() != 1 {
		t.Fatalf("first repeat count = %d, want 1", repeatCount.Load())
	}
	if want := now.Add(40 * time.Millisecond); !app.repeatDeadline.Equal(want) {
		t.Fatalf("next repeat deadline = %v, want %v", app.repeatDeadline, want)
	}

	now = now.Add(500 * time.Millisecond)
	app.runKeyRepeat(now)
	app.runKeyRepeat(now)
	if repeatCount.Load() != 2 {
		t.Fatalf("late repeat catch-up count = %d, want exactly one new event", repeatCount.Load())
	}
	if !app.repeatDeadline.After(now) {
		t.Fatalf("late repeat deadline %v did not move beyond %v", app.repeatDeadline, now)
	}

	app.handleKey(11, 250, 28, 0)
	current := repeatCount.Load()
	app.runKeyRepeat(now.Add(time.Second))
	if repeatCount.Load() != current {
		t.Fatalf("key repeat continued after release: was %d, now %d", current, repeatCount.Load())
	}
	if !app.repeatDeadline.IsZero() {
		t.Fatal("release retained a repeat deadline")
	}
}

func TestWaylandKeyRepeatQueuedAfterDestroyIsIgnored(t *testing.T) {
	app := &waylandApplication{loop: &waylandLoop{}}
	win := &waylandWindow{app: app, surface: 204}
	delivered := false
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnKey: func(event platform.KeyEvent) (platform.Frame, error) {
				delivered = true
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{config: state.config}
	win.state = state
	app.windows = []*waylandWindowState{state}
	app.keyboardFocus = win
	app.repeatScancode = 28
	app.repeatGeneration = 1
	app.repeatWindow = win

	win.Close()
	app.post(func() {
		app.dispatchKeyRepeat(win, platform.KeyEvent{Repeat: true}, 28, 1)
	})
	app.loop.drainCommands()

	if delivered {
		t.Fatal("queued key repeat was delivered after window destruction")
	}
	if win.state != nil {
		t.Fatal("expected window state to be destroyed")
	}
}

func TestWaylandIBusModifiersUseXMask(t *testing.T) {
	app := &waylandApplication{
		modifiers: input.ModifierShift | input.ModifierControl | input.ModifierAlt | input.ModifierMeta,
	}
	context := &fakeIMEContext{}
	ime := &linuxIME{app: app, active: true, context: context}
	win := &waylandWindow{app: app}
	state := &waylandWindowState{app: app, win: win}
	state.router.ime = ime
	ime.router = state.router
	win.state = state
	app.keyboardFocus = win

	app.handleKey(1, 100, 57, 1)
	if len(context.keys) != 1 {
		t.Fatalf("IBus received %d keys, want 1", len(context.keys))
	}
	want := uint32(xcbModShift | xcbModControl | xcbMod1 | xcbMod4)
	if got := context.keys[0].state; got != want {
		t.Fatalf("IBus modifier mask = %#x, want X mask %#x", got, want)
	}
}

func TestWaylandIBusKeyReleaseUsesReleaseMask(t *testing.T) {
	app := &waylandApplication{modifiers: input.ModifierShift}
	context := &fakeIMEContext{}
	ime := &linuxIME{app: app, active: true, context: context}
	win := &waylandWindow{app: app}
	state := &waylandWindowState{app: app, win: win}
	state.router.ime = ime
	ime.router = state.router
	win.state = state
	app.keyboardFocus = win

	app.handleKey(1, 100, 57, 1)
	app.handleKey(2, 120, 57, 0)
	if len(context.keys) != 2 {
		t.Fatalf("IBus received %d keys, want press and release", len(context.keys))
	}
	release := context.keys[1]
	want := uint32(xcbModShift) | ibus.ReleaseMask
	if release.state != want {
		t.Fatalf("IBus release mask = %#x, want %#x", release.state, want)
	}
	if release.done != nil {
		t.Fatal("IBus release unexpectedly has an application delivery callback")
	}
	if release.keysym != context.keys[0].keysym || release.keycode != context.keys[0].keycode {
		t.Fatalf("press and release identities differ: press=%+v release=%+v", context.keys[0], release)
	}
}

func TestWaylandKeyboardErrorsReachApplication(t *testing.T) {
	expectedErr := errors.New("key failed")
	app := &waylandApplication{}
	win := &waylandWindow{app: app}
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnKey: func(platform.KeyEvent) (platform.Frame, error) {
				return platform.Frame{}, expectedErr
			},
		},
	}
	state.router = eventRouter{config: state.config}
	win.state = state
	app.keyboardFocus = win

	app.handleKey(1, 100, 57, 1)
	if !errors.Is(app.firstErr, expectedErr) {
		t.Fatalf("application error = %v, want %v", app.firstErr, expectedErr)
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

func TestWaylandKeyRepeatCleanupAndIndependentRelease(t *testing.T) {
	app := &waylandApplication{
		repeatRate:  50,
		repeatDelay: 50,
	}
	win := &waylandWindow{
		app:     app,
		surface: 203,
	}
	repeatEvents := 0
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnKey: func(event platform.KeyEvent) (platform.Frame, error) {
				if event.Repeat {
					repeatEvents++
				}
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{config: state.config, scale: win.displayScale}
	win.state = state
	app.windows = []*waylandWindowState{state}
	app.keyboardFocus = win

	app.handleKey(1, 100, 30, 1)
	app.mu.Lock()
	generation := app.repeatGeneration
	if app.repeatScancode != 30 || app.repeatWindow != win || app.repeatDeadline.IsZero() {
		app.mu.Unlock()
		t.Fatal("key press did not initialize main-loop repeat state")
	}
	app.mu.Unlock()

	// Releasing a different key must not stop the active repeat generation.
	app.handleKey(2, 120, 42, 0)
	app.mu.Lock()
	if app.repeatScancode != 30 || app.repeatGeneration != generation {
		app.mu.Unlock()
		t.Fatal("release of a different key changed the repeat generation")
	}
	app.mu.Unlock()

	app.handleKey(3, 140, 30, 0)
	app.mu.Lock()
	if app.repeatScancode != 0 || app.repeatWindow != nil || !app.repeatDeadline.IsZero() || app.repeatGeneration == generation {
		app.mu.Unlock()
		t.Fatal("releasing the repeating key did not invalidate its state")
	}
	app.mu.Unlock()

	app.dispatchKeyRepeat(win, platform.KeyEvent{Repeat: true}, 30, generation)
	if repeatEvents != 0 {
		t.Fatal("released repeat generation delivered an event")
	}
}

func TestWaylandKeyRepeatRejectsPreviousPressGeneration(t *testing.T) {
	app := &waylandApplication{repeatRate: 50, repeatDelay: 50}
	win := &waylandWindow{app: app, surface: 205}
	repeatEvents := 0
	state := &waylandWindowState{
		app: app,
		win: win,
		config: platform.Config{
			OnKey: func(event platform.KeyEvent) (platform.Frame, error) {
				if event.Repeat {
					repeatEvents++
				}
				return platform.Frame{}, nil
			},
		},
	}
	state.router = eventRouter{config: state.config}
	win.state = state
	app.keyboardFocus = win
	app.windows = []*waylandWindowState{state}

	app.handleKey(1, 100, 30, 1)
	oldGeneration := app.repeatGeneration
	app.handleKey(2, 120, 30, 0)
	app.handleKey(3, 140, 30, 1)
	if app.repeatGeneration == oldGeneration {
		t.Fatal("same-key press reused the previous generation")
	}

	app.dispatchKeyRepeat(win, platform.KeyEvent{Repeat: true}, 30, oldGeneration)
	if repeatEvents != 0 {
		t.Fatal("previous press generation delivered after same-key re-press")
	}
	app.dispatchKeyRepeat(win, platform.KeyEvent{Repeat: true}, 30, app.repeatGeneration)
	if repeatEvents != 1 {
		t.Fatalf("current press generation delivered %d events, want 1", repeatEvents)
	}
}
