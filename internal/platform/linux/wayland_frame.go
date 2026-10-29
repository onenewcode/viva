//go:build linux

package linux

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	frameListenerOnce sync.Once
	rawFrameListener  [1]uintptr
	pinnerFrame       runtime.Pinner
)

func initFrameListener() {
	frameListenerOnce.Do(func() {
		rawFrameListener[0] = purego.NewCallback(func(data unsafe.Pointer, callback uintptr, callbackData uint32) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.onFrameDone(callback, callbackData)
		})
		pinnerFrame.Pin(&rawFrameListener)
	})
}

// requestFrameCallback mounts a wl_callback on wl_surface to drive presentation backpressure.
func (win *waylandWindow) requestFrameCallback() {
	if win.framePending || win.surface == 0 || win.app == nil || win.app.api == nil {
		return
	}
	cbIface := win.app.api.getInterface("wl_callback_interface", &wlCallbackInterface)
	cb := win.app.api.proxyMarshalFlags(win.surface, wlSurfaceFrameOpcode, cbIface, 1, 0, uintptr(0))
	if cb == 0 {
		return
	}
	win.framePending = true
	win.frameCallback = cb

	initFrameListener()
	win.app.api.proxyAddListener(cb, uintptr(unsafe.Pointer(&rawFrameListener[0])), unsafe.Pointer(win))
}

// presentWithFrameCallback pairs a new callback with a successful present.
// A failure destroys only that new callback, preserving any older frame clock.
func (win *waylandWindow) presentWithFrameCallback(present func() error) error {
	if present == nil {
		return nil
	}
	previousCallback := win.frameCallback
	win.requestFrameCallback()
	callback := win.frameCallback
	err := present()
	if err != nil && callback != 0 && callback != previousCallback {
		if win.app != nil && win.app.api != nil {
			win.app.api.proxyDestroy(callback)
		}
		if win.frameCallback == callback {
			win.frameCallback = 0
			win.framePending = false
		}
	}
	return err
}

func (win *waylandWindow) onFrameDone(callback uintptr, callbackData uint32) {
	if win.app != nil && win.app.api != nil && callback != 0 {
		win.app.api.proxyDestroy(callback)
	}
	win.frameCallback = 0
	win.framePending = false

	if win.resizeDeferred {
		win.applyPendingResize()
	} else if win.needsRedraw || win.forcePresent {
		win.renderFrame()
	}
}
