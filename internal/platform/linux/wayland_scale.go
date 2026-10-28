//go:build linux

package linux

import (
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	scaleListenerOnce          sync.Once
	rawFractionalScaleListener [1]uintptr
	pinnerScale                runtime.Pinner

	surfaceListenerOnce sync.Once
	rawSurfaceListener  [4]uintptr
	pinnerSurface       runtime.Pinner
)

func initScaleListeners() {
	scaleListenerOnce.Do(func() {
		// 0: preferred_scale(scaleObj, numerator)
		rawFractionalScaleListener[0] = purego.NewCallback(func(data unsafe.Pointer, scaleObj uintptr, numerator uint32) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			scale := float32(numerator) / 120.0
			win.applyScale(scale)
		})

		pinnerScale.Pin(&rawFractionalScaleListener)
	})
}

func initSurfaceListeners() {
	surfaceListenerOnce.Do(func() {
		// 0: enter(surface, output)
		rawSurfaceListener[0] = purego.NewCallback(func(data unsafe.Pointer, surface uintptr, output uintptr) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.handleSurfaceEnter(output)
		})

		// 1: leave(surface, output)
		rawSurfaceListener[1] = purego.NewCallback(func(data unsafe.Pointer, surface uintptr, output uintptr) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.handleSurfaceLeave(output)
		})

		// 2: preferred_buffer_scale(surface, factor)
		rawSurfaceListener[2] = purego.NewCallback(func(data unsafe.Pointer, surface uintptr, factor int32) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			if win.fractionalScale == 0 && factor > 0 {
				win.applyScale(float32(factor))
			}
		})

		// 3: preferred_buffer_transform(surface, transform)
		rawSurfaceListener[3] = purego.NewCallback(func(data unsafe.Pointer, surface uintptr, transform uint32) {
			// No-op for now
		})

		pinnerSurface.Pin(&rawSurfaceListener)
	})
}

func (app *waylandApplication) setupWindowScale(win *waylandWindow) {
	if win == nil || win.surface == 0 || app == nil || app.api == nil {
		return
	}

	// 1. Fractional Scale Manager
	if app.globals.scaleMgr != 0 {
		win.fractionalScale = app.api.getFractionalScale(app.globals.scaleMgr, win.surface)
		if win.fractionalScale != 0 {
			initScaleListeners()
			app.api.proxyAddListener(win.fractionalScale, uintptr(unsafe.Pointer(&rawFractionalScaleListener[0])), unsafe.Pointer(win))
		}
	}

	// 2. Viewporter
	if app.globals.viewporter != 0 {
		win.viewport = app.api.getViewport(app.globals.viewporter, win.surface)
	}

	// 3. Surface enter/leave for output tracking
	initSurfaceListeners()
	app.api.proxyAddListener(win.surface, uintptr(unsafe.Pointer(&rawSurfaceListener[0])), unsafe.Pointer(win))
}

func (win *waylandWindow) handleSurfaceEnter(output uintptr) {
	if output == 0 {
		return
	}
	win.mu.Lock()
	found := false
	for _, o := range win.activeOutputs {
		if o == output {
			found = true
			break
		}
	}
	if !found {
		win.activeOutputs = append(win.activeOutputs, output)
	}
	win.mu.Unlock()

	// If fractional scale manager is not available, fallback to output scale
	if win.fractionalScale == 0 && win.app != nil {
		maxScale := win.app.getOutputScale(output)
		if maxScale > 0 {
			win.applyScale(float32(maxScale))
		}
	}
}

func (win *waylandWindow) handleSurfaceLeave(output uintptr) {
	if output == 0 {
		return
	}
	win.mu.Lock()
	for i, o := range win.activeOutputs {
		if o == output {
			win.activeOutputs = append(win.activeOutputs[:i], win.activeOutputs[i+1:]...)
			break
		}
	}
	win.mu.Unlock()
}

func (win *waylandWindow) applyScale(scale float32) {
	if scale <= 0 {
		scale = 1.0
	}
	oldScale := win.displayScale()
	if oldScale == scale {
		return
	}

	logicalW := float32(contentSize(win.width, win.margin)) / oldScale
	logicalH := float32(contentSize(win.height, win.margin)) / oldScale

	win.storeScale(scale)

	// Recompute physical dimensions
	physContentW := deviceLength(int(logicalW), scale)
	physContentH := deviceLength(int(logicalH), scale)

	if win.margin > 0 {
		win.margin = frameMargin(scale)
		win.width = physContentW + 2*win.margin
		win.height = physContentH + 2*win.margin
		if win.state != nil && win.state.renderer != nil {
			_ = win.state.renderer.setFrameMargin(win.margin)
		}
	} else {
		win.width = physContentW
		win.height = physContentH
	}

	// Update Vulkan renderer scale & resize swapchain
	if win.state != nil && win.state.renderer != nil {
		_ = win.state.renderer.setScale(scale)
		_ = win.state.renderer.resize(win.width, win.height)
	}

	// Configure viewport destination logical dimensions (entire surface)
	if win.viewport != 0 && win.app != nil && win.app.api != nil {
		totalLogicalW := int32(math.Round(float64(float32(win.width) / scale)))
		totalLogicalH := int32(math.Round(float64(float32(win.height) / scale)))
		win.app.api.viewportSetDestination(win.viewport, totalLogicalW, totalLogicalH)
	}

	win.geometryDirty = true
	win.updateWindowGeometry()
	win.updateInputRegion()

	if win.state != nil && win.state.config.OnResize != nil {
		if frame, err := win.state.config.OnResize(win.logicalSize()); err == nil {
			if win.state.router.present != nil {
				_ = win.state.router.present(frame)
			}
		}
	}
	win.reportFrame()

	win.renderFrame()
}

func (app *waylandApplication) getOutputScale(output uintptr) int32 {
	// Defaults to 1 for basic output scaling
	return 1
}
