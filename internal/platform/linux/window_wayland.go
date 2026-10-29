//go:build linux

package linux

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/vivaui/viva/internal/input"
	"github.com/vivaui/viva/internal/platform"
	"github.com/vivaui/viva/ui"
)

var (
	windowListenersOnce    sync.Once
	rawXdgSurfaceListener  [1]uintptr
	rawXdgToplevelListener [4]uintptr
	rawDecorationListener  [1]uintptr
	pinnerWindowListeners  runtime.Pinner
)

func initWindowListeners() {
	windowListenersOnce.Do(func() {
		// xdg_surface.configure(serial)
		rawXdgSurfaceListener[0] = purego.NewCallback(func(data unsafe.Pointer, xdgSurface uintptr, serial uint32) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.handleXdgSurfaceConfigure(serial)
		})

		// xdg_toplevel.configure(width, height, states)
		rawXdgToplevelListener[0] = purego.NewCallback(func(data unsafe.Pointer, xdgToplevel uintptr, width int32, height int32, states unsafe.Pointer) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.handleXdgToplevelConfigure(width, height, (*wlArray)(states))
		})

		// xdg_toplevel.close()
		rawXdgToplevelListener[1] = purego.NewCallback(func(data unsafe.Pointer, xdgToplevel uintptr) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.handleClose()
		})

		// xdg_toplevel.configure_bounds(width, height)
		rawXdgToplevelListener[2] = purego.NewCallback(func(data unsafe.Pointer, xdgToplevel uintptr, width int32, height int32) {
			// Optional bounds configuration from compositor
		})

		// xdg_toplevel.wm_capabilities(capabilities)
		rawXdgToplevelListener[3] = purego.NewCallback(func(data unsafe.Pointer, xdgToplevel uintptr, capabilities unsafe.Pointer) {
			// Optional compositor capabilities
		})

		// zxdg_toplevel_decoration_v1.configure(mode)
		rawDecorationListener[0] = purego.NewCallback(func(data unsafe.Pointer, decoration uintptr, mode uint32) {
			if data == nil {
				return
			}
			win := (*waylandWindow)(data)
			win.handleDecorationConfigure(mode)
		})

		pinnerWindowListeners.Pin(&rawXdgSurfaceListener)
		pinnerWindowListeners.Pin(&rawXdgToplevelListener)
		pinnerWindowListeners.Pin(&rawDecorationListener)
	})
}

// waylandWindow coordinates an XDG-Shell toplevel window, surface state, and frame rendering.
type waylandWindow struct {
	mu sync.Mutex

	app            *waylandApplication
	surface        uintptr
	xdgSurface     uintptr
	xdgToplevel    uintptr
	decoration     uintptr
	decorationMode uint32

	fractionalScale uintptr
	viewport        uintptr
	activeOutputs   []uintptr

	width, height uint32 // device pixels
	scaleBits     atomic.Uint32
	margin        uint32
	savedLogicalW float32
	savedLogicalH float32
	frameWanted   bool
	geometryDirty bool
	lastFrame     ui.Rect

	pendingSerial      uint32
	pendingWidth       int32
	pendingHeight      int32
	configured         bool
	firstConfigureDone bool
	framePending       bool
	frameCallback      uintptr
	needsRedraw        bool
	maximized          bool
	fullscreen         bool
	resizing           bool
	activated          bool
	hidden             bool
	closed             atomic.Bool

	pointerX, pointerY float32
	lastClickTime      uint32

	pinner runtime.Pinner
	pinned bool
	state  *waylandWindowState
}

// waylandWindowState maintains platform window state and render loop pipeline.
type waylandWindowState struct {
	app      *waylandApplication
	win      *waylandWindow
	config   platform.Config
	renderer *vulkanRenderer
	router   eventRouter
	text     *linuxTextInput
	profile  bool
	started  time.Time
}

// displayScale returns the device pixels per logical pixel for this window.
func (win *waylandWindow) displayScale() float32 {
	if bits := win.scaleBits.Load(); bits != 0 {
		return math.Float32frombits(bits)
	}
	return 1
}

func (win *waylandWindow) storeScale(scale float32) {
	win.scaleBits.Store(math.Float32bits(scale))
}

// logicalSize computes the content area size in logical pixels.
func (win *waylandWindow) logicalSize() platform.Size {
	scale := win.displayScale()
	return platform.Size{
		Width:  float32(contentSize(win.width, win.margin)) / scale,
		Height: float32(contentSize(win.height, win.margin)) / scale,
		Scale:  scale,
	}
}

func (win *waylandWindow) reportFrame() {
	if win.state == nil || win.state.config.OnFrameChange == nil || win.maximized || win.fullscreen {
		return
	}
	scale := win.displayScale()
	frame := ui.Rect{
		Width:  float32(contentSize(win.width, win.margin)) / scale,
		Height: float32(contentSize(win.height, win.margin)) / scale,
	}
	if frame == win.lastFrame {
		return
	}
	win.lastFrame = frame
	win.state.config.OnFrameChange(frame)
}

func (win *waylandWindow) updateWindowGeometry() {
	if win.xdgSurface == 0 || win.app == nil || win.app.api == nil {
		return
	}
	scale := win.displayScale()
	if scale <= 0 {
		scale = 1.0
	}
	if win.margin > 0 {
		geomX := int32(math.Round(float64(float32(win.margin) / scale)))
		geomY := int32(math.Round(float64(float32(win.margin) / scale)))
		geomW := int32(math.Round(float64(float32(contentSize(win.width, win.margin)) / scale)))
		geomH := int32(math.Round(float64(float32(contentSize(win.height, win.margin)) / scale)))
		win.app.api.xdgSurfaceSetWindowGeometry(win.xdgSurface, geomX, geomY, geomW, geomH)
	} else {
		geomW := int32(math.Round(float64(float32(win.width) / scale)))
		geomH := int32(math.Round(float64(float32(win.height) / scale)))
		win.app.api.xdgSurfaceSetWindowGeometry(win.xdgSurface, 0, 0, geomW, geomH)
	}
}

func (win *waylandWindow) updateInputRegion() {
	if win.surface == 0 || win.app == nil || win.app.api == nil || win.app.globals.compositor == 0 {
		return
	}
	if win.margin > 0 {
		scale := win.displayScale()
		if scale <= 0 {
			scale = 1.0
		}
		grip := frameGrip(scale)
		cW := contentSize(win.width, win.margin)
		cH := contentSize(win.height, win.margin)
		reach := min(grip, win.margin)
		rx := int32(math.Round(float64(float32(win.margin-reach) / scale)))
		ry := int32(math.Round(float64(float32(win.margin-reach) / scale)))
		rw := int32(math.Round(float64(float32(cW+2*reach) / scale)))
		rh := int32(math.Round(float64(float32(cH+2*reach) / scale)))

		region := win.app.api.createRegion(win.app.globals.compositor)
		if region != 0 {
			win.app.api.regionAdd(region, rx, ry, rw, rh)
			win.app.api.surfaceSetInputRegion(win.surface, region)
			win.app.api.destroyRegion(region)
		}
	} else {
		// Reset input region to default (whole surface)
		win.app.api.surfaceSetInputRegion(win.surface, 0)
	}
}

func (win *waylandWindow) updateViewportDestination() {
	if win.viewport == 0 || win.app == nil || win.app.api == nil {
		return
	}
	scale := win.displayScale()
	if scale <= 0 {
		scale = 1.0
	}
	totalLogicalW := int32(math.Round(float64(float32(win.width) / scale)))
	totalLogicalH := int32(math.Round(float64(float32(win.height) / scale)))
	if totalLogicalW <= 0 {
		totalLogicalW = 1
	}
	if totalLogicalH <= 0 {
		totalLogicalH = 1
	}
	win.app.api.viewportSetDestination(win.viewport, totalLogicalW, totalLogicalH)
}

// renderFrame presents the current content atomically to the Wayland surface.
func (win *waylandWindow) renderFrame() {
	if win.closed.Load() || win.surface == 0 || win.state == nil {
		return
	}
	if win.hidden {
		win.needsRedraw = false
		if win.state.config.OnRedraw != nil {
			if frame, err := win.state.config.OnRedraw(); err == nil && win.state.router.present != nil {
				_ = win.state.router.present(frame)
			}
		}
		return
	}
	if win.framePending {
		win.needsRedraw = true
		return
	}
	win.needsRedraw = false

	if win.pendingSerial != 0 && win.app != nil && win.app.api != nil && win.xdgSurface != 0 {
		win.app.api.xdgSurfaceAckConfigure(win.xdgSurface, win.pendingSerial)
		win.pendingSerial = 0
	}

	if win.geometryDirty {
		win.updateWindowGeometry()
		win.updateInputRegion()
		win.updateViewportDestination()
		win.geometryDirty = false
	}

	// Request next frame callback BEFORE committing current frame
	win.requestFrameCallback()

	if win.state.renderer != nil {
		if win.state.config.OnRedraw != nil {
			if frame, err := win.state.config.OnRedraw(); err == nil {
				if win.state.router.present != nil {
					_ = win.state.router.present(frame)
				}
			} else {
				_ = win.state.renderer.present()
			}
		} else {
			_ = win.state.renderer.present()
		}
	} else if win.app != nil && win.app.api != nil {
		win.app.api.surfaceCommit(win.surface)
	}

	if win.app != nil && win.app.api != nil && win.app.api.displayFlush != nil && win.app.display != 0 {
		win.app.api.displayFlush(win.app.display)
	}
}

func (win *waylandWindow) handleXdgToplevelConfigure(width, height int32, states *wlArray) {
	maximized := false
	fullscreen := false
	resizing := false
	activated := false

	if states != nil && states.Data != nil && states.Size >= 4 {
		count := int(states.Size / 4)
		stateSlice := unsafe.Slice((*uint32)(states.Data), count)
		for _, s := range stateSlice {
			switch s {
			case xdgToplevelStateMaximized:
				maximized = true
			case xdgToplevelStateFullscreen:
				fullscreen = true
			case xdgToplevelStateResizing:
				resizing = true
			case xdgToplevelStateActivated:
				activated = true
			}
		}
	}

	win.resizing = resizing
	if win.activated != activated {
		win.activated = activated
		if !activated {
			if win.state != nil && win.state.config.WindowMode == platform.WindowPanel {
				win.Hide()
			}
			if win.state != nil && win.state.config.OnFocusLost != nil {
				if frame, err := win.state.config.OnFocusLost(); err == nil {
					if win.state.router.present != nil {
						_ = win.state.router.present(frame)
					}
				}
			}
		}
	}

	if win.maximized != maximized || win.fullscreen != fullscreen {
		oldMax := win.maximized || win.fullscreen
		newMax := maximized || fullscreen
		win.maximized = maximized
		win.fullscreen = fullscreen
		if oldMax != newMax {
			if newMax {
				// Strip margin on maximized/fullscreen
				win.margin = 0
				if win.state != nil && win.state.renderer != nil {
					_ = win.state.renderer.setFrameMargin(0)
				}
			} else if win.frameWanted {
				// Restore margin on unmaximize
				win.margin = frameMargin(win.displayScale())
				if win.state != nil && win.state.renderer != nil {
					_ = win.state.renderer.setFrameMargin(win.margin)
				}
			}
			win.geometryDirty = true
			if win.state != nil && win.state.config.OnMaximize != nil {
				win.state.config.OnMaximize(maximized)
			}
		}
	}

	win.pendingWidth = width
	win.pendingHeight = height
}

func (win *waylandWindow) applyPendingResize() {
	if win.closed.Load() || win.surface == 0 {
		return
	}
	win.needsRedraw = false

	scale := win.displayScale()
	w, h := win.pendingWidth, win.pendingHeight
	if (w <= 0 || h <= 0) && !win.maximized && !win.fullscreen && win.savedLogicalW > 0 && win.savedLogicalH > 0 {
		w = int32(math.Round(float64(win.savedLogicalW)))
		h = int32(math.Round(float64(win.savedLogicalH)))
	}
	if w > 0 && h > 0 {
		if !win.maximized && !win.fullscreen {
			win.savedLogicalW = float32(w)
			win.savedLogicalH = float32(h)
		}
		newWidth := deviceLength(int(w), scale)
		newHeight := deviceLength(int(h), scale)
		if win.margin > 0 {
			newWidth += 2 * win.margin
			newHeight += 2 * win.margin
		}
		if newWidth != win.width || newHeight != win.height {
			win.width = newWidth
			win.height = newHeight
			win.geometryDirty = true
		}
	}

	// Before the renderer exists, retain the compositor's geometry for openWindow.
	// Its initial present acknowledges the serial together with the first buffer.
	if win.state == nil {
		return
	}

	// Always acknowledge configure BEFORE presenting the new geometry/buffer.
	// This ensures compositor matches the acknowledged serial with the committed frame.
	if win.app != nil && win.app.api != nil && win.xdgSurface != 0 && win.pendingSerial != 0 {
		win.app.api.xdgSurfaceAckConfigure(win.xdgSurface, win.pendingSerial)
		win.pendingSerial = 0
	}

	// Synchronize window geometry, viewport destination and input region prior
	// to Vulkan present/commit so the compositor updates anchors and geometry atomically.
	if win.geometryDirty {
		win.updateWindowGeometry()
		win.updateInputRegion()
		win.updateViewportDestination()
		win.geometryDirty = false
	}

	// Request next frame callback BEFORE committing current frame
	win.requestFrameCallback()

	if win.state.renderer != nil {
		_ = win.state.renderer.resize(win.width, win.height)
		presented := false
		if win.state.config.OnResize != nil {
			if frame, err := win.state.config.OnResize(win.logicalSize()); err == nil {
				if win.state.router.present != nil {
					_ = win.state.router.present(frame)
					presented = true
				}
			}
		}
		if !presented {
			_ = win.state.renderer.present()
		}
		win.reportFrame()
	} else {
		if win.state.config.OnResize != nil {
			if frame, err := win.state.config.OnResize(win.logicalSize()); err == nil {
				if win.state.router.present != nil {
					_ = win.state.router.present(frame)
				}
			}
		}
		if win.app != nil && win.app.api != nil {
			win.app.api.surfaceCommit(win.surface)
		}
	}
	if win.app != nil && win.app.api != nil && win.app.api.displayFlush != nil && win.app.display != 0 {
		win.app.api.displayFlush(win.app.display)
	}
}

func (win *waylandWindow) handleXdgSurfaceConfigure(serial uint32) {
	win.pendingSerial = serial
	win.configured = true

	scale := win.displayScale()
	w, h := win.pendingWidth, win.pendingHeight
	if (w <= 0 || h <= 0) && !win.maximized && !win.fullscreen && win.savedLogicalW > 0 && win.savedLogicalH > 0 {
		w = int32(math.Round(float64(win.savedLogicalW)))
		h = int32(math.Round(float64(win.savedLogicalH)))
	}
	resized := false

	if w > 0 && h > 0 {
		newWidth := deviceLength(int(w), scale)
		newHeight := deviceLength(int(h), scale)
		if win.margin > 0 {
			newWidth += 2 * win.margin
			newHeight += 2 * win.margin
		}
		if newWidth != win.width || newHeight != win.height {
			resized = true
		}
	}

	if resized {
		win.applyPendingResize()
	} else if win.state != nil {
		win.renderFrame()
	}
}

func (win *waylandWindow) handleDecorationConfigure(mode uint32) {
	win.decorationMode = mode
	if mode == zxdgToplevelDecorationModeServerSide {
		if win.frameWanted {
			win.margin = 0
			win.frameWanted = false
			if win.state != nil && win.state.renderer != nil {
				_ = win.state.renderer.setFrameMargin(0)
			}
			win.geometryDirty = true
		}
	} else if mode == zxdgToplevelDecorationModeClientSide {
		if win.state != nil && win.state.config.WindowMode == platform.WindowCustomTitlebar {
			win.frameWanted = true
			win.margin = frameMargin(win.displayScale())
			if win.state.renderer != nil {
				_ = win.state.renderer.setFrameMargin(win.margin)
			}
			win.geometryDirty = true
		}
	}
}

func (win *waylandWindow) handleClose() {
	if win.state != nil && win.state.config.OnCloseRequest != nil && win.state.config.WindowMode != platform.WindowPanel {
		win.state.config.OnCloseRequest(func() {
			win.Close()
		})
	} else {
		win.Close()
	}
}

func (win *waylandWindow) requestClose() {
	win.app.post(func() {
		win.handleClose()
	})
}

func (win *waylandWindow) setMaximized(maximized bool) {
	if win.xdgToplevel == 0 || win.app == nil || win.app.api == nil {
		return
	}
	if maximized {
		win.app.api.xdgToplevelSetMaximized(win.xdgToplevel)
	} else {
		win.app.api.xdgToplevelUnsetMaximized(win.xdgToplevel)
	}
}

func (win *waylandWindow) toggleMaximize() {
	win.setMaximized(!win.maximized)
}

func (win *waylandWindow) minimize() {
	if win.xdgToplevel != 0 && win.app != nil && win.app.api != nil {
		win.app.api.xdgToplevelSetMinimized(win.xdgToplevel)
	}
}

func (win *waylandWindow) startMove(serial uint32) {
	if win.xdgToplevel != 0 && win.app != nil && win.app.api != nil && win.app.globals.seat != 0 {
		win.app.api.xdgToplevelMove(win.xdgToplevel, win.app.globals.seat, serial)
		if win.app.api.displayFlush != nil && win.app.display != 0 {
			win.app.api.displayFlush(win.app.display)
		}
	}
}

func (win *waylandWindow) startResize(serial uint32, edge platform.WindowEdge) {
	if win.xdgToplevel != 0 && win.app != nil && win.app.api != nil && win.app.globals.seat != 0 {
		waylandEdge := xdgResizeEdge(edge)
		win.app.api.xdgToplevelResize(win.xdgToplevel, win.app.globals.seat, serial, waylandEdge)
		if win.app.api.displayFlush != nil && win.app.display != 0 {
			win.app.api.displayFlush(win.app.display)
		}
	}
}

const (
	xdgToplevelResizeEdgeNone        uint32 = 0
	xdgToplevelResizeEdgeTop         uint32 = 1
	xdgToplevelResizeEdgeBottom      uint32 = 2
	xdgToplevelResizeEdgeLeft        uint32 = 4
	xdgToplevelResizeEdgeTopLeft     uint32 = 5
	xdgToplevelResizeEdgeBottomLeft  uint32 = 6
	xdgToplevelResizeEdgeRight       uint32 = 8
	xdgToplevelResizeEdgeTopRight    uint32 = 9
	xdgToplevelResizeEdgeBottomRight uint32 = 10
)

func xdgResizeEdge(edge platform.WindowEdge) uint32 {
	switch edge {
	case platform.EdgeTop:
		return xdgToplevelResizeEdgeTop
	case platform.EdgeBottom:
		return xdgToplevelResizeEdgeBottom
	case platform.EdgeLeft:
		return xdgToplevelResizeEdgeLeft
	case platform.EdgeRight:
		return xdgToplevelResizeEdgeRight
	case platform.EdgeTopLeft:
		return xdgToplevelResizeEdgeTopLeft
	case platform.EdgeTopRight:
		return xdgToplevelResizeEdgeTopRight
	case platform.EdgeBottomLeft:
		return xdgToplevelResizeEdgeBottomLeft
	case platform.EdgeBottomRight:
		return xdgToplevelResizeEdgeBottomRight
	default:
		return xdgToplevelResizeEdgeNone
	}
}

func (win *waylandWindow) destroy() {
	if win == nil {
		return
	}
	win.closed.Store(true)
	var api *waylandAPI
	if win.app != nil {
		api = win.app.api
	}

	if api != nil {
		if win.decoration != 0 {
			api.decorationDestroy(win.decoration)
			win.decoration = 0
		}
		if win.xdgToplevel != 0 {
			api.xdgToplevelDestroy(win.xdgToplevel)
			win.xdgToplevel = 0
		}
		if win.xdgSurface != 0 {
			api.xdgSurfaceDestroy(win.xdgSurface)
			win.xdgSurface = 0
		}
		if win.frameCallback != 0 {
			api.proxyDestroy(win.frameCallback)
			win.frameCallback = 0
		}
		win.framePending = false
		win.needsRedraw = false
		if win.fractionalScale != 0 {
			api.fractionalScaleDestroy(win.fractionalScale)
			win.fractionalScale = 0
		}
		if win.viewport != 0 {
			api.viewportDestroy(win.viewport)
			win.viewport = 0
		}
	}

	// Vulkan swapchain and VkSurfaceKHR MUST be destroyed before wl_surface is destroyed!
	if win.state != nil {
		if win.state.config.Waker != nil && win.state.config.OnRedraw != nil {
			win.state.config.Waker.Unbind()
		}
		if win.state.renderer != nil {
			win.state.renderer.close()
			win.state.renderer = nil
		}
		if win.state.text != nil {
			win.state.text.close()
			win.state.text = nil
		}
		if win.app != nil {
			win.app.atspiClosed(win.state)
			if win.app.pointerFocus == win {
				win.app.pointerFocus = nil
			}
			if win.app.keyboardFocus == win {
				win.app.stopKeyRepeat()
				win.app.keyboardFocus = nil
			}
			if win.app.dragState != nil && win.app.dragState.win == win {
				win.app.dragState = nil
			}
			win.app.removeWindow(win.state)
		}
		if win.state.config.OnClosed != nil {
			win.state.config.OnClosed()
		}
		win.state = nil
	}

	if api != nil && win.surface != 0 {
		api.surfaceDestroy(win.surface)
		win.surface = 0
	}

	if win.pinned {
		win.pinner.Unpin()
		win.pinned = false
	}
}

// Close closes the window and releases all resources.
func (win *waylandWindow) Close() {
	if win != nil && !win.closed.Swap(true) {
		win.app.post(func() {
			win.destroy()
		})
	}
}

// Hide hides the window without destroying its state or renderer.
func (win *waylandWindow) Hide() {
	if win == nil || win.closed.Load() {
		return
	}
	win.app.post(func() {
		if win.hidden {
			return
		}
		win.hidden = true
		if win.frameCallback != 0 && win.app != nil && win.app.api != nil {
			win.app.api.proxyDestroy(win.frameCallback)
			win.frameCallback = 0
		}
		win.framePending = false
		win.needsRedraw = false
		if win.surface != 0 && win.app != nil && win.app.api != nil {
			win.app.api.surfaceAttach(win.surface, 0, 0, 0)
			win.app.api.surfaceCommit(win.surface)
			win.app.api.displayFlush(win.app.display)
		}
	})
}

// Show shows a hidden window.
func (win *waylandWindow) Show() {
	if win == nil || win.closed.Load() {
		return
	}
	win.app.post(func() {
		if !win.hidden {
			return
		}
		win.hidden = false
		win.renderFrame()
	})
}

// Frame returns the content area rectangle in logical pixels.
func (win *waylandWindow) Frame() (ui.Rect, bool) {
	if win == nil {
		return ui.Rect{}, false
	}
	scale := win.displayScale()
	w := float32(contentSize(win.width, win.margin)) / scale
	h := float32(contentSize(win.height, win.margin)) / scale
	return ui.Rect{Width: w, Height: h}, false
}

func (win *waylandWindow) contentOrigin() (x, y int32) {
	scale := win.displayScale()
	margin := int32(float32(win.margin) / scale)
	return margin, margin
}

// openWindow instantiates a new Wayland XDG-Shell window.
func (app *waylandApplication) openWindow(config platform.Config) (*waylandWindow, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if app.globals.compositor == 0 {
		return nil, fmt.Errorf("linux: compositor global not available")
	}
	if app.globals.xdgWmBase == 0 {
		return nil, fmt.Errorf("linux: xdg_wm_base global not available")
	}

	scale := float32(1)
	deviceW := deviceLength(config.Width, scale)
	deviceH := deviceLength(config.Height, scale)
	margin := uint32(0)
	frameWanted := false

	if config.WindowMode == platform.WindowCustomTitlebar {
		margin = frameMargin(scale)
		frameWanted = true
	}

	totalW := deviceW + 2*margin
	totalH := deviceH + 2*margin

	surface := app.api.createSurface(app.globals.compositor)
	if surface == 0 {
		return nil, fmt.Errorf("linux: failed to create wl_surface")
	}

	xdgSurface := app.api.getXdgSurface(app.globals.xdgWmBase, surface)
	if xdgSurface == 0 {
		app.api.surfaceDestroy(surface)
		return nil, fmt.Errorf("linux: failed to create xdg_surface")
	}

	xdgToplevel := app.api.xdgSurfaceGetToplevel(xdgSurface)
	if xdgToplevel == 0 {
		app.api.proxyDestroy(xdgSurface)
		app.api.surfaceDestroy(surface)
		return nil, fmt.Errorf("linux: failed to create xdg_toplevel")
	}

	if config.Title != "" {
		cTitle := []byte(config.Title + "\x00")
		app.api.xdgToplevelSetTitle(xdgToplevel, &cTitle[0])
	}
	appId := config.Class
	if appId == "" {
		appId = "viva"
	}
	cAppId := []byte(appId + "\x00")
	app.api.xdgToplevelSetAppId(xdgToplevel, &cAppId[0])

	var decoration uintptr
	var decorationMode uint32
	if config.WindowMode == platform.WindowTitled {
		if app.globals.decorationMgr != 0 {
			decoration = app.api.getToplevelDecoration(app.globals.decorationMgr, xdgToplevel)
			app.api.setDecorationMode(decoration, zxdgToplevelDecorationModeServerSide)
		}
	} else {
		if app.globals.decorationMgr != 0 {
			decoration = app.api.getToplevelDecoration(app.globals.decorationMgr, xdgToplevel)
			app.api.setDecorationMode(decoration, zxdgToplevelDecorationModeClientSide)
		}
	}

	win := &waylandWindow{
		app:            app,
		surface:        surface,
		xdgSurface:     xdgSurface,
		xdgToplevel:    xdgToplevel,
		decoration:     decoration,
		decorationMode: decorationMode,
		width:          totalW,
		height:         totalH,
		margin:         margin,
		savedLogicalW:  float32(config.Width),
		savedLogicalH:  float32(config.Height),
		frameWanted:    frameWanted,
		geometryDirty:  true,
	}
	win.storeScale(scale)
	win.pinner.Pin(win)
	win.pinned = true

	initWindowListeners()
	app.api.proxyAddListener(xdgSurface, uintptr(unsafe.Pointer(&rawXdgSurfaceListener[0])), unsafe.Pointer(win))
	app.api.proxyAddListener(xdgToplevel, uintptr(unsafe.Pointer(&rawXdgToplevelListener[0])), unsafe.Pointer(win))
	if decoration != 0 {
		app.api.proxyAddListener(decoration, uintptr(unsafe.Pointer(&rawDecorationListener[0])), unsafe.Pointer(win))
	}
	app.setupWindowScale(win)

	if config.WindowMode == platform.WindowCustomTitlebar && config.Actions != nil {
		*config.Actions = platform.WindowActions{
			Minimize:       win.minimize,
			ToggleMaximize: win.toggleMaximize,
			RequestClose:   win.requestClose,
			Maximized:      func() bool { return win.maximized },
		}
	}

	// Trigger initial surface commit to receive configure sequence
	app.api.surfaceCommit(surface)
	_ = app.api.displayRoundtrip(app.display)

	textInput, err := newLinuxTextInput(&config)
	if err != nil {
		win.destroy()
		return nil, err
	}
	initialRectangles, initialLabels := textInput.initial()

	surfaceProvider := newWaylandSurfaceProvider(app.display, surface)
	renderer, err := newVulkanRenderer(surfaceProvider, win.width, win.height, win.displayScale(), config.ClearColor, initialRectangles, initialLabels, win.margin)
	if err != nil {
		textInput.close()
		win.destroy()
		return nil, err
	}

	if win.margin > 0 && !renderer.frame.alpha {
		win.margin = 0
		win.frameWanted = false
		if err := renderer.setFrameMargin(0); err != nil {
			renderer.close()
			textInput.close()
			win.destroy()
			return nil, err
		}
	}

	state := &waylandWindowState{
		app:      app,
		win:      win,
		config:   config,
		renderer: renderer,
		text:     textInput,
		profile:  os.Getenv("VIVA_PROFILE_STARTUP") != "",
	}
	if state.profile {
		state.started = time.Now()
	}
	win.state = state

	var ime *linuxIME
	state.router = eventRouter{
		config:  config,
		text:    textInput,
		pressed: new([256]bool),
		scale:   win.displayScale,
		clicks:  &input.ClickCounter{Interval: 500 * time.Millisecond, Distance: 4},
		present: func(frame platform.Frame) error {
			if ime != nil {
				defer ime.sync()
			}
			if frame.Accessibility != nil && app != nil {
				app.atspiFrame(state, frame.Accessibility)
			}
			merged, dirty, err := textInput.prepare(frame)
			if err != nil || !dirty {
				return err
			}
			changed, err := renderer.apply(merged)
			if err != nil || !changed {
				return err
			}
			return renderer.present()
		},
	}
	if (config.DynamicTextInput || config.TextInput != nil) && app != nil {
		if ime = app.newIME(win, textInput, state.router); ime != nil {
			state.router.ime = ime
		}
	}

	win.updateWindowGeometry()
	win.updateInputRegion()
	win.updateViewportDestination()
	win.geometryDirty = false

	if win.pendingSerial != 0 {
		app.api.xdgSurfaceAckConfigure(win.xdgSurface, win.pendingSerial)
		win.pendingSerial = 0
	}
	win.requestFrameCallback()
	if err := renderer.present(); err != nil {
		renderer.close()
		textInput.close()
		win.destroy()
		return nil, err
	}
	app.api.surfaceCommit(surface)
	app.api.displayFlush(app.display)
	win.firstConfigureDone = true

	if config.Waker != nil && config.OnRedraw != nil {
		config.Waker.Bind(func() {
			app.post(func() {
				win.needsRedraw = true
				if !win.framePending {
					win.renderFrame()
				}
			})
		})
	}

	app.mu.Lock()
	app.windows = append(app.windows, state)
	app.mu.Unlock()
	app.atspiOpened(state)

	return win, nil
}
