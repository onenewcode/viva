//go:build linux

package linux

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/internal/dbus"
	"github.com/vivaui/viva/internal/dbusmenu"
	"github.com/vivaui/viva/internal/ibus"
	"github.com/vivaui/viva/internal/input"
	"github.com/vivaui/viva/internal/platform"
)

var activeWaylandApp atomic.Pointer[waylandApplication]

type waylandInputMethod interface {
	FD() int
	Err() error
	Dispatch() error
	Close()
}

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
	repeatStop     chan struct{}
	repeatKey      platform.KeyEvent
	repeatScancode uint32

	mu       sync.Mutex
	windows  []*waylandWindowState
	firstErr error

	status        *statusNotifier
	desktop       *desktopWatch
	desktopFailed bool
	ibus          waylandInputMethod
	ibusFD        int32
	ibusFDSet     bool
	ibusTried     bool
	atspi         *atspiHost
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
	app.globals.onOutputScale = app.handleOutputScale
	app.globals.onOutputRemove = app.handleOutputRemove
	if err := app.globals.bindGlobals(api, display); err != nil {
		app.close()
		return nil, err
	}
	app.setupDataDevice()
	platform.SetWatchHook(func() {
		app.post(func() {
			app.syncDesktopWatch()
		})
	})
	app.syncDesktopWatch()
	activeWaylandApp.Store(app)
	return app, nil
}

// post enqueues a command onto the main loop waker pipeline.
func (app *waylandApplication) post(cmd waylandCommand) {
	if app.loop != nil {
		app.loop.post(cmd)
	} else if cmd != nil {
		cmd()
	}
}

// Quit closes every window, bypassing close requests, and ends the loop.
// It is thread-safe.
func (app *waylandApplication) Quit() {
	app.post(func() {
		app.closeAll()
		if app.loop != nil {
			app.loop.stop()
		}
	})
}

// closeAll closes and destroys all currently open windows, bypassing OnCloseRequest.
func (app *waylandApplication) closeAll() {
	app.closeStatus()
	app.closeDesktop()
	app.mu.Lock()
	states := append([]*waylandWindowState(nil), app.windows...)
	app.mu.Unlock()
	for _, state := range states {
		if state != nil && state.win != nil {
			state.win.destroy()
		}
	}
}

// OpenWindow creates and displays a new Wayland-backed window.
func (app *waylandApplication) OpenWindow(config platform.Config) (platform.Window, error) {
	return app.openWindow(config)
}

// Run executes the application start callback and drives the Wayland main loop.
func (app *waylandApplication) Run(start platform.ApplicationStart) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer app.close()

	if err := start(app); err != nil {
		app.closeAll()
		return err
	}

	for app.hasWindows() && app.loop != nil && app.loop.step(-1) {
	}

	if app.firstErr != nil {
		return app.firstErr
	}
	if app.loop != nil {
		return app.loop.err
	}
	return nil
}

// AddStatusItem registers an icon with the desktop's StatusNotifier host via D-Bus.
func (app *waylandApplication) AddStatusItem(config platform.StatusItemConfig) (platform.StatusItem, error) {
	if !config.Icon.Valid() {
		return nil, fmt.Errorf("%w: status icon pixels do not match its size", platform.ErrStatusAreaUnavailable)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.status == nil {
		conn, err := dbus.DialSession()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", platform.ErrStatusAreaUnavailable, err)
		}
		var match dbus.Encoder
		match.String("type='signal',sender='" + busName + "',interface='" + busName + "',member='NameOwnerChanged'")
		if _, err := conn.Call(busName, "/org/freedesktop/DBus", busName, "AddMatch", "s", match.Bytes()); err != nil {
			conn.Close()
			return nil, err
		}
		app.status = &statusNotifier{
			conn:  conn,
			items: make(map[string]*linuxStatusItem),
			onRemoval: func() {
				app.post(func() {
					app.drainStatusRemovals()
				})
			},
		}
		if app.loop != nil {
			app.loop.addExtraFD(int32(conn.FD()), app.serveStatus)
		}
	}
	notifier := app.status
	notifier.nextID++
	item := &linuxStatusItem{
		notifier: notifier, tooltip: config.Tooltip, icon: config.Icon, onClick: config.OnClick,
		name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-%d", os.Getpid(), notifier.nextID),
	}
	if config.Menu != nil {
		item.menu = dbusmenu.New(config.Menu)
	}
	var request dbus.Encoder
	request.String(item.name)
	request.Uint32(requestDoNotQueue)
	reply, err := notifier.conn.Call(busName, "/org/freedesktop/DBus", busName, "RequestName", "su", request.Bytes())
	if err != nil {
		app.dropStatusIfEmpty()
		return nil, err
	}
	if reply.Args().Uint32() != 1 {
		app.dropStatusIfEmpty()
		return nil, fmt.Errorf("linux: the bus name %s is taken", item.name)
	}
	notifier.items[item.name] = item
	if err := notifier.register(item); err != nil {
		notifier.release(item)
		delete(notifier.items, item.name)
		app.dropStatusIfEmpty()
		return nil, fmt.Errorf("%w: %v", platform.ErrStatusAreaUnavailable, err)
	}
	return item, nil
}

func (app *waylandApplication) dropStatusIfEmpty() {
	if status := app.status; status != nil && len(status.items) == 0 {
		app.closeStatus()
	}
}

func (app *waylandApplication) closeStatus() {
	if app.status != nil {
		if app.loop != nil {
			app.loop.removeExtraFD(int32(app.status.conn.FD()))
		}
		app.status.conn.Close()
		app.status = nil
	}
}

func (app *waylandApplication) drainStatusRemovals() {
	app.mu.Lock()
	notifier := app.status
	if notifier == nil {
		app.mu.Unlock()
		return
	}
	notifier.mu.Lock()
	names := notifier.removals
	notifier.removals = nil
	notifier.mu.Unlock()
	for _, name := range names {
		if item := notifier.items[name]; item != nil {
			notifier.release(item)
			delete(notifier.items, name)
		}
	}
	app.dropStatusIfEmpty()
	app.mu.Unlock()
}

func (app *waylandApplication) serveStatus() {
	app.mu.Lock()
	notifier := app.status
	app.mu.Unlock()
	if notifier == nil {
		return
	}
	messages, err := notifier.conn.Receive()
	if err != nil {
		app.mu.Lock()
		app.closeStatus()
		app.mu.Unlock()
		return
	}
	for _, message := range messages {
		switch message.Type {
		case dbus.TypeCall:
			notifier.handle(message)
		case dbus.TypeSignal:
			if message.Member == "NameOwnerChanged" && message.Signature == "sss" {
				args := message.Args()
				name, _, owner := args.String(), args.String(), args.String()
				for _, watcher := range watcherNames {
					if name == watcher && owner != "" {
						for _, item := range notifier.items {
							notifier.register(item)
						}
					}
				}
			}
		}
	}
}

func (app *waylandApplication) syncDesktopWatch() {
	wanted := platform.DesktopWatched()
	switch {
	case wanted && app.desktop == nil && !app.desktopFailed:
		app.startDesktopWatch()
	case !wanted && app.desktop != nil:
		app.stopDesktopWatch()
	}
}

func (app *waylandApplication) startDesktopWatch() {
	desktopState.Lock()
	gone := desktopState.portalGone
	desktopState.Unlock()
	if gone {
		app.desktopFailed = true
		return
	}
	conn, err := dbus.DialSession()
	if err == nil {
		err = addMatch(conn, settingsMatch)
	}
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		app.desktopFailed = true
		return
	}
	values, missing := readPortalValues(conn)
	if missing {
		conn.Close()
		app.desktopFailed = true
		desktopState.Lock()
		desktopState.portalGone = true
		desktopState.Unlock()
		return
	}
	change := replacePortal(values, os.Getenv)
	desktopState.Lock()
	desktopState.watching = true
	desktopState.Unlock()
	app.desktop = &desktopWatch{conn: conn}
	if app.loop != nil {
		app.loop.addExtraFD(int32(conn.FD()), app.serveDesktop)
	}
	app.announce(change)
}

func (app *waylandApplication) stopDesktopWatch() {
	if app.desktop == nil {
		return
	}
	if app.loop != nil {
		app.loop.removeExtraFD(int32(app.desktop.conn.FD()))
	}
	app.desktop.conn.Close()
	app.desktop = nil
	desktopState.Lock()
	desktopState.watching = false
	desktopState.Unlock()
}

func (app *waylandApplication) serveDesktop() {
	watch := app.desktop
	if watch == nil {
		return
	}
	messages, err := watch.conn.Receive()
	if err != nil {
		app.stopDesktopWatch()
		app.desktopFailed = true
		return
	}
	var change settingsChange
	for _, message := range messages {
		if message.Type != dbus.TypeSignal || message.Member != "SettingChanged" || message.Signature != "ssv" {
			continue
		}
		if namespace, key, value, ok := parseSettingChanged(message.Body); ok {
			change |= applyPortal(namespace, key, value, os.Getenv)
		}
	}
	app.announce(change)
}

func (app *waylandApplication) closeDesktop() {
	if app.desktop != nil {
		app.stopDesktopWatch()
	}
}

func (app *waylandApplication) announce(change settingsChange) {
	if change == 0 {
		return
	}
	if change&changedFonts != 0 {
		refreshTextEngine()
		app.mu.Lock()
		windows := append([]*waylandWindowState(nil), app.windows...)
		app.mu.Unlock()
		for _, state := range windows {
			if state != nil && state.win != nil {
				state.win.renderFrame()
			}
		}
	}
	if change&changedAppearance != 0 {
		platform.NotifyAppearanceChanged()
	}
	platform.NotifyDesktopChanged(change.platformChange())
}

func (app *waylandApplication) fail(err error) {
	if err == nil {
		return
	}
	if app.firstErr == nil {
		app.firstErr = err
	}
	app.closeAll()
}

func (app *waylandApplication) inputMethodBus() waylandInputMethod {
	if !app.ibusTried {
		app.ibusTried = true
		if bus, err := ibus.Dial(); err == nil {
			app.ibus = bus
			if app.loop != nil {
				app.ibusFD = int32(bus.FD())
				app.ibusFDSet = true
				app.loop.addExtraFD(app.ibusFD, app.serveInputMethod)
			}
		}
	}
	return app.ibus
}

func (app *waylandApplication) removeInputMethodFD() {
	if app.ibusFDSet {
		if app.loop != nil {
			app.loop.removeExtraFD(app.ibusFD)
		}
		app.ibusFDSet = false
	}
	app.ibusFD = 0
}

func (app *waylandApplication) serveInputMethod() {
	if app.ibus != nil {
		_ = app.ibus.Dispatch()
		app.checkInputMethod()
	}
}

func (app *waylandApplication) checkInputMethod() {
	if app.ibus == nil || app.ibus.Err() == nil {
		return
	}
	app.removeInputMethodFD()
	app.ibus = nil
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, state := range app.windows {
		if state != nil && state.router.ime != nil {
			state.router.ime.lost()
		}
	}
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
	platform.SetWatchHook(nil)
	app.closeStatus()
	app.closeDesktop()
	app.atspiClose()
	if app.ibus != nil {
		app.removeInputMethodFD()
		app.ibus.Close()
		app.ibus = nil
	}
	app.closeAll()
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
