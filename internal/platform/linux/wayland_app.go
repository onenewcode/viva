//go:build linux

package linux

import (
	"errors"
	"sync"
)

type waylandWindowState struct{}

// waylandApplication coordinates the Wayland client display connection,
// event loop, and window lifecycle.
type waylandApplication struct {
	api     *waylandAPI
	libc    *libcAPI
	display uintptr
	loop    *waylandLoop
	globals waylandGlobals

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
	if err := app.globals.bindGlobals(api, display); err != nil {
		app.close()
		return nil, err
	}
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

// close releases all Wayland proxies, sockets, and dynamically linked libraries.
func (app *waylandApplication) close() {
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
