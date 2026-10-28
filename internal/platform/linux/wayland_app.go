//go:build linux

package linux

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/internal/input"
	"github.com/vivaui/viva/internal/platform"
)

var activeWaylandApp atomic.Pointer[waylandApplication]

// waylandApplication coordinates the Wayland client display connection,
// event loop, and window lifecycle.
type waylandApplication struct {
	api     *waylandAPI
	libc    *libcAPI
	display uintptr
	loop    *waylandLoop
	globals waylandGlobals

	pointer            uintptr
	keyboard           uintptr
	pointerFocus       *waylandWindow
	pointerSerial      uint32
	keyboardFocus      *waylandWindow
	lastSerial         uint32
	scrollPending      bool
	scrollV120Axis0    bool
	scrollV120Axis1    bool
	pendingScroll      platform.ScrollEvent
	cursorMgr          *waylandCursorManager
	currentCursorShape uint8

	dataDevice     uintptr
	dataSource     uintptr
	clipboardText  string
	ownsClipboard  bool
	clipboardOffer *waylandDataOffer
	dragState      *waylandDragState
	dataOffers     map[uintptr]*waylandDataOffer

	xkb            *xkbAPI
	xkbContext     uintptr
	xkbKeymap      uintptr
	xkbState       uintptr
	modifiers      input.Modifiers
	repeatRate     int32
	repeatDelay    int32
	repeatTimer    *time.Timer
	repeatTicker   *time.Ticker
	repeatKey      platform.KeyEvent
	repeatScancode uint32

	mu      sync.Mutex
	windows []*waylandWindowState
}

// newWaylandApplication initializes Wayland display, event loop, and binds registry globals.
func newWaylandApplication() (*waylandApplication, error) {
	api, err := loadWaylandClient()
	if err != nil {
		return nil, err
	}
	libc, err := loadLibc()
	if err != nil {
		api.close()
		return nil, err
	}
	display := api.displayConnect(nil)
	if display == 0 {
		libc.teardown()
		api.close()
		return nil, errors.New("linux: failed to connect to Wayland display")
	}
	loop, err := newWaylandLoop(api, display, libc)
	if err != nil {
		api.displayDisconnect(display)
		libc.teardown()
		api.close()
		return nil, err
	}
	app := &waylandApplication{
		api:     api,
		libc:    libc,
		display: display,
		loop:    loop,
	}
	app.globals.onCapabilities = app.handleSeatCapabilities
	if err := app.globals.bindGlobals(api, display); err != nil {
		app.close()
		return nil, err
	}
	app.setupDataDevice()
	activeWaylandApp.Store(app)
	return app, nil
}

// post enqueues a command onto the main loop waker pipeline.
func (app *waylandApplication) post(cmd waylandCommand) {
	if app.loop != nil {
		app.loop.post(cmd)
	}
}

// Quit signals the event loop to stop and terminates the application cleanly.
func (app *waylandApplication) Quit() {
	app.post(func() {
		if app.loop != nil {
			app.loop.stop()
		}
	})
}

// OpenWindow creates and displays a new Wayland-backed window.
func (app *waylandApplication) OpenWindow(config platform.Config) (platform.Window, error) {
	return app.openWindow(config)
}

// AddStatusItem is currently a stub on Wayland until system tray integration is completed.
func (app *waylandApplication) AddStatusItem(config platform.StatusItemConfig) (platform.StatusItem, error) {
	return nil, errors.New("linux: StatusNotifierItem not supported on Wayland yet")
}

func (app *waylandApplication) removeWindow(state *waylandWindowState) {
	app.mu.Lock()
	defer app.mu.Unlock()
	for i, w := range app.windows {
		if w == state {
			app.windows = append(app.windows[:i], app.windows[i+1:]...)
			break
		}
	}
}

func (app *waylandApplication) findWindowBySurface(surface uintptr) *waylandWindow {
	if surface == 0 {
		return nil
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, state := range app.windows {
		if state.win != nil && state.win.surface == surface {
			return state.win
		}
	}
	return nil
}

func (app *waylandApplication) handleSeatCapabilities(caps uint32) {
	if caps&wlSeatCapabilityPointer != 0 {
		app.setupPointer()
	} else {
		app.teardownPointer()
	}
	if caps&wlSeatCapabilityKeyboard != 0 {
		app.setupKeyboard()
	} else {
		app.teardownKeyboard()
	}
}

func (app *waylandApplication) hasWindows() bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	return len(app.windows) > 0
}

// close releases all Wayland proxies, sockets, and dynamically linked libraries.
func (app *waylandApplication) close() {
	activeWaylandApp.CompareAndSwap(app, nil)
	app.teardownDataDevice()
	app.teardownPointer()
	app.teardownKeyboard()
	if app.cursorMgr != nil {
		app.cursorMgr.destroy(app.api)
		app.cursorMgr = nil
	}
	if app.globals.compositor != 0 {
		app.globals.destroy(app.api)
	}
	if app.loop != nil {
		app.loop.close()
		app.loop = nil
	}
	if app.display != 0 && app.api != nil {
		app.api.displayDisconnect(app.display)
		app.display = 0
	}
	if app.libc != nil {
		app.libc.teardown()
		app.libc = nil
	}
	if app.api != nil {
		app.api.close()
		app.api = nil
	}
}
