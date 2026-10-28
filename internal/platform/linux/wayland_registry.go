//go:build linux

package linux

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	wlDisplayGetRegistryOpcode = 1
	wlRegistryBindOpcode       = 0

	wlCompositorCreateSurfaceOpcode = 0
	wlCompositorCreateRegionOpcode  = 1

	wlSurfaceDestroyOpcode         = 0
	wlSurfaceAttachOpcode          = 1
	wlSurfaceDamageOpcode          = 2
	wlSurfaceFrameOpcode           = 3
	wlSurfaceSetOpaqueRegionOpcode = 4
	wlSurfaceSetInputRegionOpcode  = 5
	wlSurfaceCommitOpcode          = 6
	wlSurfaceSetBufferTransform    = 7
	wlSurfaceSetBufferScaleOpcode  = 8
	wlSurfaceDamageBufferOpcode    = 9

	wlRegionDestroyOpcode  = 0
	wlRegionAddOpcode      = 1
	wlRegionSubtractOpcode = 2

	xdgWmBaseDestroyOpcode          = 0
	xdgWmBaseCreatePositionerOpcode = 1
	xdgWmBaseGetXdgSurfaceOpcode    = 2
	xdgWmBasePongOpcode             = 3

	xdgSurfaceDestroyOpcode           = 0
	xdgSurfaceGetToplevelOpcode       = 1
	xdgSurfaceGetPopupOpcode          = 2
	xdgSurfaceSetWindowGeometryOpcode = 3
	xdgSurfaceAckConfigureOpcode      = 4

	xdgToplevelDestroyOpcode         = 0
	xdgToplevelSetParentOpcode       = 1
	xdgToplevelSetTitleOpcode        = 2
	xdgToplevelSetAppIdOpcode        = 3
	xdgToplevelShowWindowMenuOpcode  = 4
	xdgToplevelMoveOpcode            = 5
	xdgToplevelResizeOpcode          = 6
	xdgToplevelSetMaxSizeOpcode      = 7
	xdgToplevelSetMinSizeOpcode      = 8
	xdgToplevelSetMaximizedOpcode    = 9
	xdgToplevelUnsetMaximizedOpcode  = 10
	xdgToplevelSetFullscreenOpcode   = 11
	xdgToplevelUnsetFullscreenOpcode = 12
	xdgToplevelSetMinimizedOpcode    = 13

	zxdgDecorationManagerV1DestroyOpcode               = 0
	zxdgDecorationManagerV1GetToplevelDecorationOpcode = 1

	zxdgToplevelDecorationV1DestroyOpcode   = 0
	zxdgToplevelDecorationV1SetModeOpcode   = 1
	zxdgToplevelDecorationV1UnsetModeOpcode = 2

	zxdgToplevelDecorationModeClientSide uint32 = 1
	zxdgToplevelDecorationModeServerSide uint32 = 2

	xdgToplevelStateMaximized  uint32 = 1
	xdgToplevelStateFullscreen uint32 = 2
	xdgToplevelStateResizing   uint32 = 3
	xdgToplevelStateActivated  uint32 = 4
	xdgToplevelStateSuspended  uint32 = 5
)

// String constants for Wayland interfaces
var (
	strWlDisplay                  = []byte("wl_display\x00")
	strWlRegistry                 = []byte("wl_registry\x00")
	strWlCompositor               = []byte("wl_compositor\x00")
	strWlShm                      = []byte("wl_shm\x00")
	strWlSeat                     = []byte("wl_seat\x00")
	strWlSurface                  = []byte("wl_surface\x00")
	strXdgWmBase                  = []byte("xdg_wm_base\x00")
	strXdgSurface                 = []byte("xdg_surface\x00")
	strXdgToplevel                = []byte("xdg_toplevel\x00")
	strZxdgDecorationManagerV1    = []byte("zxdg_decoration_manager_v1\x00")
	strZxdgToplevelDecorationV1   = []byte("zxdg_toplevel_decoration_v1\x00")
	strWpFractionalScaleManagerV1 = []byte("wp_fractional_scale_manager_v1\x00")
	strWpFractionalScaleV1        = []byte("wp_fractional_scale_v1\x00")
	strWpViewporter               = []byte("wp_viewporter\x00")
	strWpViewport                 = []byte("wp_viewport\x00")
	strWlRegion                   = []byte("wl_region\x00")
	strWlCallback                 = []byte("wl_callback\x00")
)

// Signature constants
var (
	sigEmpty = []byte("\x00")
	sigU     = []byte("u\x00")
	sigN     = []byte("n\x00")
	sigNO    = []byte("no\x00")
	sigII    = []byte("ii\x00")
	sigIIA   = []byte("iia\x00")
	sigIIII  = []byte("iiii\x00")
	sigS     = []byte("s\x00")
	sigA     = []byte("a\x00")
	sigO     = []byte("?o\x00")
	sigOII   = []byte("?oii\x00")
	sigI     = []byte("i\x00")
)

// Core static wlInterface definitions for extension protocols (and fallback)
var (
	wlRegistryRequests = [1]wlMessage{
		{Name: &[]byte("bind\x00")[0], Signature: &[]byte("usun\x00")[0], Types: nil},
	}
	wlRegistryInterface = wlInterface{
		Name:        &strWlRegistry[0],
		Version:     1,
		MethodCount: 1,
		Methods:     &wlRegistryRequests[0],
	}

	wlCompositorRequests = [2]wlMessage{
		{Name: &[]byte("create_surface\x00")[0], Signature: &sigN[0]},
		{Name: &[]byte("create_region\x00")[0], Signature: &sigN[0]},
	}
	wlCompositorInterface = wlInterface{
		Name:        &strWlCompositor[0],
		Version:     4,
		MethodCount: 2,
		Methods:     &wlCompositorRequests[0],
	}

	wlShmInterface = wlInterface{
		Name:    &strWlShm[0],
		Version: 1,
	}

	wlSeatInterface = wlInterface{
		Name:    &strWlSeat[0],
		Version: 5,
	}

	wlSurfaceRequests = [10]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("attach\x00")[0], Signature: &sigOII[0]},
		{Name: &[]byte("damage\x00")[0], Signature: &sigIIII[0]},
		{Name: &[]byte("frame\x00")[0], Signature: &sigN[0]},
		{Name: &[]byte("set_opaque_region\x00")[0], Signature: &sigO[0]},
		{Name: &[]byte("set_input_region\x00")[0], Signature: &sigO[0]},
		{Name: &[]byte("commit\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_buffer_transform\x00")[0], Signature: &sigI[0]},
		{Name: &[]byte("set_buffer_scale\x00")[0], Signature: &sigI[0]},
		{Name: &[]byte("damage_buffer\x00")[0], Signature: &sigIIII[0]},
	}
	wlSurfaceInterface = wlInterface{
		Name:        &strWlSurface[0],
		Version:     4,
		MethodCount: 10,
		Methods:     &wlSurfaceRequests[0],
	}

	wlRegionRequests = [3]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("add\x00")[0], Signature: &sigIIII[0]},
		{Name: &[]byte("subtract\x00")[0], Signature: &sigIIII[0]},
	}
	wlRegionInterface = wlInterface{
		Name:        &strWlRegion[0],
		Version:     1,
		MethodCount: 3,
		Methods:     &wlRegionRequests[0],
	}

	wlCallbackEvents = [1]wlMessage{
		{Name: &[]byte("done\x00")[0], Signature: &sigU[0]},
	}
	wlCallbackInterface = wlInterface{
		Name:       &strWlCallback[0],
		Version:    1,
		EventCount: 1,
		Events:     &wlCallbackEvents[0],
	}

	// xdg_wm_base
	xdgWmBaseRequests = [4]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("create_positioner\x00")[0], Signature: &sigN[0]},
		{Name: &[]byte("get_xdg_surface\x00")[0], Signature: &sigNO[0]},
		{Name: &[]byte("pong\x00")[0], Signature: &sigU[0]},
	}
	xdgWmBaseEvents = [1]wlMessage{
		{Name: &[]byte("ping\x00")[0], Signature: &sigU[0]},
	}
	xdgWmBaseInterface = wlInterface{
		Name:        &strXdgWmBase[0],
		Version:     1,
		MethodCount: 4,
		Methods:     &xdgWmBaseRequests[0],
		EventCount:  1,
		Events:      &xdgWmBaseEvents[0],
	}

	// xdg_surface
	xdgSurfaceRequests = [5]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("get_toplevel\x00")[0], Signature: &sigN[0]},
		{Name: &[]byte("get_popup\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_window_geometry\x00")[0], Signature: &sigIIII[0]},
		{Name: &[]byte("ack_configure\x00")[0], Signature: &sigU[0]},
	}
	xdgSurfaceEvents = [1]wlMessage{
		{Name: &[]byte("configure\x00")[0], Signature: &sigU[0]},
	}
	xdgSurfaceInterface = wlInterface{
		Name:        &strXdgSurface[0],
		Version:     1,
		MethodCount: 5,
		Methods:     &xdgSurfaceRequests[0],
		EventCount:  1,
		Events:      &xdgSurfaceEvents[0],
	}

	// xdg_toplevel
	xdgToplevelRequests = [14]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_parent\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_title\x00")[0], Signature: &sigS[0]},
		{Name: &[]byte("set_app_id\x00")[0], Signature: &sigS[0]},
		{Name: &[]byte("show_window_menu\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("move\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("resize\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_max_size\x00")[0], Signature: &sigII[0]},
		{Name: &[]byte("set_min_size\x00")[0], Signature: &sigII[0]},
		{Name: &[]byte("set_maximized\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("unset_maximized\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_fullscreen\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("unset_fullscreen\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_minimized\x00")[0], Signature: &sigEmpty[0]},
	}
	xdgToplevelEvents = [4]wlMessage{
		{Name: &[]byte("configure\x00")[0], Signature: &sigIIA[0]},
		{Name: &[]byte("close\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("configure_bounds\x00")[0], Signature: &sigII[0]},
		{Name: &[]byte("wm_capabilities\x00")[0], Signature: &sigA[0]},
	}
	xdgToplevelInterface = wlInterface{
		Name:        &strXdgToplevel[0],
		Version:     1,
		MethodCount: 14,
		Methods:     &xdgToplevelRequests[0],
		EventCount:  4,
		Events:      &xdgToplevelEvents[0],
	}

	// zxdg_decoration_manager_v1
	zxdgDecorationManagerV1Requests = [2]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("get_toplevel_decoration\x00")[0], Signature: &sigNO[0]},
	}
	zxdgDecorationManagerV1Interface = wlInterface{
		Name:        &strZxdgDecorationManagerV1[0],
		Version:     1,
		MethodCount: 2,
		Methods:     &zxdgDecorationManagerV1Requests[0],
	}

	// zxdg_toplevel_decoration_v1
	zxdgToplevelDecorationV1Requests = [3]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_mode\x00")[0], Signature: &sigU[0]},
		{Name: &[]byte("unset_mode\x00")[0], Signature: &sigEmpty[0]},
	}
	zxdgToplevelDecorationV1Events = [1]wlMessage{
		{Name: &[]byte("configure\x00")[0], Signature: &sigU[0]},
	}
	zxdgToplevelDecorationV1Interface = wlInterface{
		Name:        &strZxdgToplevelDecorationV1[0],
		Version:     1,
		MethodCount: 3,
		Methods:     &zxdgToplevelDecorationV1Requests[0],
		EventCount:  1,
		Events:      &zxdgToplevelDecorationV1Events[0],
	}

	// wp_fractional_scale_manager_v1
	wpFractionalScaleManagerV1Requests = [2]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("get_fractional_scale\x00")[0], Signature: &sigNO[0]},
	}
	wpFractionalScaleManagerV1Interface = wlInterface{
		Name:        &strWpFractionalScaleManagerV1[0],
		Version:     1,
		MethodCount: 2,
		Methods:     &wpFractionalScaleManagerV1Requests[0],
	}

	// wp_fractional_scale_v1
	wpFractionalScaleV1Requests = [1]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
	}
	wpFractionalScaleV1Events = [1]wlMessage{
		{Name: &[]byte("preferred_scale\x00")[0], Signature: &sigU[0]},
	}
	wpFractionalScaleV1Interface = wlInterface{
		Name:        &strWpFractionalScaleV1[0],
		Version:     1,
		MethodCount: 1,
		Methods:     &wpFractionalScaleV1Requests[0],
		EventCount:  1,
		Events:      &wpFractionalScaleV1Events[0],
	}

	// wp_viewporter
	wpViewporterRequests = [2]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("get_viewport\x00")[0], Signature: &sigNO[0]},
	}
	wpViewporterInterface = wlInterface{
		Name:        &strWpViewporter[0],
		Version:     1,
		MethodCount: 2,
		Methods:     &wpViewporterRequests[0],
	}

	// wp_viewport
	wpViewportRequests = [3]wlMessage{
		{Name: &[]byte("destroy\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_source\x00")[0], Signature: &sigEmpty[0]},
		{Name: &[]byte("set_destination\x00")[0], Signature: &sigII[0]},
	}
	wpViewportInterface = wlInterface{
		Name:        &strWpViewport[0],
		Version:     1,
		MethodCount: 3,
		Methods:     &wpViewportRequests[0],
	}
)

var (
	registryListenerOnce sync.Once
	rawRegistryListener  [2]uintptr
	rawXdgWmBaseListener [1]uintptr
	pinnerRegistry       runtime.Pinner
)

func initRegistryListeners() {
	registryListenerOnce.Do(func() {
		rawRegistryListener[0] = purego.NewCallback(func(data unsafe.Pointer, registry uintptr, name uint32, ifacePtr *byte, version uint32) {
			if data == nil {
				return
			}
			g := (*waylandGlobals)(data)
			ifaceName := waylandGoString(ifacePtr)
			g.handleGlobal(registry, name, ifaceName, version)
		})
		rawRegistryListener[1] = purego.NewCallback(func(data unsafe.Pointer, registry uintptr, name uint32) {
			if data == nil {
				return
			}
			g := (*waylandGlobals)(data)
			g.handleGlobalRemove(name)
		})

		rawXdgWmBaseListener[0] = purego.NewCallback(func(data unsafe.Pointer, xdgWmBase uintptr, serial uint32) {
			if data == nil {
				return
			}
			g := (*waylandGlobals)(data)
			if g.api != nil {
				g.api.proxyMarshalFlags(xdgWmBase, xdgWmBasePongOpcode, 0, g.xdgWmBaseVer, 0, serial)
			}
		})

		pinnerRegistry.Pin(&rawRegistryListener)
		pinnerRegistry.Pin(&rawXdgWmBaseListener)
	})
}

// waylandGlobals tracks bound global singleton protocol objects.
type waylandGlobals struct {
	api     *waylandAPI
	display uintptr
	pinner  runtime.Pinner

	compositor       uintptr
	compositorVer    uint32
	shm              uintptr
	shmVer           uint32
	seat             uintptr
	seatVer          uint32
	xdgWmBase        uintptr
	xdgWmBaseVer     uint32
	decorationMgr    uintptr
	decorationMgrVer uint32
	scaleMgr         uintptr
	scaleMgrVer      uint32
	viewporter       uintptr
	viewporterVer    uint32
}

func (api *waylandAPI) getInterface(name string, fallback *wlInterface) uintptr {
	if api != nil && api.library != 0 {
		if sym, err := purego.Dlsym(api.library, name); err == nil && sym != 0 {
			return sym
		}
	}
	if fallback != nil {
		return uintptr(unsafe.Pointer(fallback))
	}
	return 0
}

func (api *waylandAPI) getRegistry(display uintptr) uintptr {
	regIface := api.getInterface("wl_registry_interface", &wlRegistryInterface)
	return api.proxyMarshalFlags(display, wlDisplayGetRegistryOpcode, regIface, 1, 0, uintptr(0))
}

func (api *waylandAPI) bindRegistry(registry uintptr, name uint32, iface uintptr, ifaceName *byte, version uint32) uintptr {
	if iface == 0 || ifaceName == nil {
		return 0
	}
	return api.proxyMarshalFlags(registry, wlRegistryBindOpcode, iface, version, 0, name, uintptr(unsafe.Pointer(ifaceName)), version, uintptr(0))
}

func (api *waylandAPI) destroyProxy(proxy *uintptr) {
	if proxy != nil && *proxy != 0 {
		api.proxyDestroy(*proxy)
		*proxy = 0
	}
}

func (g *waylandGlobals) handleGlobal(registry uintptr, name uint32, iface string, version uint32) {
	if g.api == nil {
		return
	}
	switch iface {
	case "wl_compositor":
		ver := min(version, 6)
		if ver >= 4 && g.compositor == 0 {
			ifacePtr := g.api.getInterface("wl_compositor_interface", &wlCompositorInterface)
			g.compositor = g.api.bindRegistry(registry, name, ifacePtr, &strWlCompositor[0], ver)
			g.compositorVer = ver
		}
	case "xdg_wm_base":
		ver := min(version, 3)
		if ver >= 1 && g.xdgWmBase == 0 {
			ifacePtr := g.api.getInterface("xdg_wm_base_interface", &xdgWmBaseInterface)
			g.xdgWmBase = g.api.bindRegistry(registry, name, ifacePtr, &strXdgWmBase[0], ver)
			g.xdgWmBaseVer = ver
			if g.xdgWmBase != 0 {
				initRegistryListeners()
				g.api.proxyAddListener(g.xdgWmBase, uintptr(unsafe.Pointer(&rawXdgWmBaseListener[0])), unsafe.Pointer(g))
			}
		}
	case "wl_seat":
		ver := min(version, 7)
		if ver >= 5 && g.seat == 0 {
			ifacePtr := g.api.getInterface("wl_seat_interface", &wlSeatInterface)
			g.seat = g.api.bindRegistry(registry, name, ifacePtr, &strWlSeat[0], ver)
			g.seatVer = ver
		}
	case "wl_shm":
		ver := uint32(1)
		if g.shm == 0 {
			ifacePtr := g.api.getInterface("wl_shm_interface", &wlShmInterface)
			g.shm = g.api.bindRegistry(registry, name, ifacePtr, &strWlShm[0], ver)
			g.shmVer = ver
		}
	case "zxdg_decoration_manager_v1":
		ver := uint32(1)
		if g.decorationMgr == 0 {
			ifacePtr := g.api.getInterface("zxdg_decoration_manager_v1_interface", &zxdgDecorationManagerV1Interface)
			g.decorationMgr = g.api.bindRegistry(registry, name, ifacePtr, &strZxdgDecorationManagerV1[0], ver)
			g.decorationMgrVer = ver
		}
	case "wp_fractional_scale_manager_v1":
		ver := uint32(1)
		if g.scaleMgr == 0 {
			ifacePtr := g.api.getInterface("wp_fractional_scale_manager_v1_interface", &wpFractionalScaleManagerV1Interface)
			g.scaleMgr = g.api.bindRegistry(registry, name, ifacePtr, &strWpFractionalScaleManagerV1[0], ver)
			g.scaleMgrVer = ver
		}
	case "wp_viewporter":
		ver := uint32(1)
		if g.viewporter == 0 {
			ifacePtr := g.api.getInterface("wp_viewporter_interface", &wpViewporterInterface)
			g.viewporter = g.api.bindRegistry(registry, name, ifacePtr, &strWpViewporter[0], ver)
			g.viewporterVer = ver
		}
	}
}

func (g *waylandGlobals) handleGlobalRemove(name uint32) {
}

// bindGlobals attaches to the display registry, registers listeners, performs a roundtrip,
// and populates g with bound globals.
func (g *waylandGlobals) bindGlobals(api *waylandAPI, display uintptr) error {
	if api == nil || display == 0 {
		return errors.New("linux: invalid wayland API or display")
	}
	initRegistryListeners()
	g.api = api
	g.display = display
	g.pinner.Pin(g)

	registry := api.getRegistry(display)
	if registry == 0 {
		g.pinner.Unpin()
		return errors.New("linux: failed to get wayland display registry")
	}
	defer api.proxyDestroy(registry)

	api.proxyAddListener(registry, uintptr(unsafe.Pointer(&rawRegistryListener[0])), unsafe.Pointer(g))
	if api.displayRoundtrip(display) < 0 {
		g.destroy(api)
		return errors.New("linux: wayland registry discovery roundtrip failed")
	}

	if g.compositor == 0 {
		g.destroy(api)
		return errors.New("linux: compositor does not support wl_compositor")
	}
	if g.xdgWmBase == 0 {
		g.destroy(api)
		return errors.New("linux: compositor does not support xdg_wm_base")
	}
	return nil
}

// destroy releases all held global proxies.
func (g *waylandGlobals) destroy(api *waylandAPI) {
	if api == nil {
		return
	}
	api.destroyProxy(&g.decorationMgr)
	api.destroyProxy(&g.scaleMgr)
	api.destroyProxy(&g.viewporter)
	api.destroyProxy(&g.xdgWmBase)
	api.destroyProxy(&g.seat)
	api.destroyProxy(&g.shm)
	api.destroyProxy(&g.compositor)
	g.pinner.Unpin()
}

func waylandGoString(ptr *byte) string {
	if ptr == nil {
		return ""
	}
	var length int
	for *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + uintptr(length))) != 0 {
		length++
	}
	return string(unsafe.Slice(ptr, length))
}

func (api *waylandAPI) createSurface(compositor uintptr) uintptr {
	if api == nil || compositor == 0 {
		return 0
	}
	iface := api.getInterface("wl_surface_interface", &wlSurfaceInterface)
	return api.proxyMarshalFlags(compositor, wlCompositorCreateSurfaceOpcode, iface, 4, 0, uintptr(0))
}

func (api *waylandAPI) createRegion(compositor uintptr) uintptr {
	if api == nil || compositor == 0 {
		return 0
	}
	iface := api.getInterface("wl_region_interface", &wlRegionInterface)
	return api.proxyMarshalFlags(compositor, wlCompositorCreateRegionOpcode, iface, 1, 0, uintptr(0))
}

func (api *waylandAPI) regionAdd(region uintptr, x, y, width, height int32) {
	if api != nil && region != 0 {
		api.proxyMarshalFlags(region, wlRegionAddOpcode, 0, 1, 0, x, y, width, height)
	}
}

func (api *waylandAPI) regionSubtract(region uintptr, x, y, width, height int32) {
	if api != nil && region != 0 {
		api.proxyMarshalFlags(region, wlRegionSubtractOpcode, 0, 1, 0, x, y, width, height)
	}
}

func (api *waylandAPI) destroyRegion(region uintptr) {
	if api != nil && region != 0 {
		api.proxyMarshalFlags(region, wlRegionDestroyOpcode, 0, 1, 0)
		api.proxyDestroy(region)
	}
}

func (api *waylandAPI) surfaceDestroy(surface uintptr) {
	if api != nil && surface != 0 {
		api.proxyMarshalFlags(surface, wlSurfaceDestroyOpcode, 0, 1, 0)
		api.proxyDestroy(surface)
	}
}

func (api *waylandAPI) surfaceAttach(surface uintptr, buffer uintptr, x, y int32) {
	if api != nil && surface != 0 {
		api.proxyMarshalFlags(surface, wlSurfaceAttachOpcode, 0, 1, 0, buffer, x, y)
	}
}

func (api *waylandAPI) surfaceDamage(surface uintptr, x, y, width, height int32) {
	if api != nil && surface != 0 {
		api.proxyMarshalFlags(surface, wlSurfaceDamageOpcode, 0, 1, 0, x, y, width, height)
	}
}

func (api *waylandAPI) surfaceFrame(surface uintptr) uintptr {
	if api == nil || surface == 0 {
		return 0
	}
	iface := api.getInterface("wl_callback_interface", &wlCallbackInterface)
	return api.proxyMarshalFlags(surface, wlSurfaceFrameOpcode, iface, 1, 0, uintptr(0))
}

func (api *waylandAPI) surfaceSetInputRegion(surface uintptr, region uintptr) {
	if api != nil && surface != 0 {
		api.proxyMarshalFlags(surface, wlSurfaceSetInputRegionOpcode, 0, 1, 0, region)
	}
}

func (api *waylandAPI) surfaceSetOpaqueRegion(surface uintptr, region uintptr) {
	if api != nil && surface != 0 {
		api.proxyMarshalFlags(surface, wlSurfaceSetOpaqueRegionOpcode, 0, 1, 0, region)
	}
}

func (api *waylandAPI) surfaceCommit(surface uintptr) {
	if api != nil && surface != 0 {
		api.proxyMarshalFlags(surface, wlSurfaceCommitOpcode, 0, 1, 0)
	}
}

func (api *waylandAPI) getXdgSurface(xdgWmBase uintptr, surface uintptr) uintptr {
	if api == nil || xdgWmBase == 0 || surface == 0 {
		return 0
	}
	iface := api.getInterface("xdg_surface_interface", &xdgSurfaceInterface)
	return api.proxyMarshalFlags(xdgWmBase, xdgWmBaseGetXdgSurfaceOpcode, iface, 1, 0, uintptr(0), surface)
}

func (api *waylandAPI) xdgSurfaceDestroy(xdgSurface uintptr) {
	if api != nil && xdgSurface != 0 {
		api.proxyMarshalFlags(xdgSurface, xdgSurfaceDestroyOpcode, 0, 1, 0)
		api.proxyDestroy(xdgSurface)
	}
}

func (api *waylandAPI) xdgSurfaceGetToplevel(xdgSurface uintptr) uintptr {
	if api == nil || xdgSurface == 0 {
		return 0
	}
	iface := api.getInterface("xdg_toplevel_interface", &xdgToplevelInterface)
	return api.proxyMarshalFlags(xdgSurface, xdgSurfaceGetToplevelOpcode, iface, 1, 0, uintptr(0))
}

func (api *waylandAPI) xdgSurfaceSetWindowGeometry(xdgSurface uintptr, x, y, width, height int32) {
	if api != nil && xdgSurface != 0 {
		api.proxyMarshalFlags(xdgSurface, xdgSurfaceSetWindowGeometryOpcode, 0, 1, 0, x, y, width, height)
	}
}

func (api *waylandAPI) xdgSurfaceAckConfigure(xdgSurface uintptr, serial uint32) {
	if api != nil && xdgSurface != 0 {
		api.proxyMarshalFlags(xdgSurface, xdgSurfaceAckConfigureOpcode, 0, 1, 0, serial)
	}
}

func (api *waylandAPI) xdgToplevelDestroy(xdgToplevel uintptr) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelDestroyOpcode, 0, 1, 0)
		api.proxyDestroy(xdgToplevel)
	}
}

func (api *waylandAPI) xdgToplevelSetTitle(xdgToplevel uintptr, title *byte) {
	if api != nil && xdgToplevel != 0 && title != nil {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetTitleOpcode, 0, 1, 0, uintptr(unsafe.Pointer(title)))
	}
}

func (api *waylandAPI) xdgToplevelSetAppId(xdgToplevel uintptr, appId *byte) {
	if api != nil && xdgToplevel != 0 && appId != nil {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetAppIdOpcode, 0, 1, 0, uintptr(unsafe.Pointer(appId)))
	}
}

func (api *waylandAPI) xdgToplevelSetMaxSize(xdgToplevel uintptr, width, height int32) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetMaxSizeOpcode, 0, 1, 0, width, height)
	}
}

func (api *waylandAPI) xdgToplevelSetMinSize(xdgToplevel uintptr, width, height int32) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetMinSizeOpcode, 0, 1, 0, width, height)
	}
}

func (api *waylandAPI) xdgToplevelSetMaximized(xdgToplevel uintptr) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetMaximizedOpcode, 0, 1, 0)
	}
}

func (api *waylandAPI) xdgToplevelUnsetMaximized(xdgToplevel uintptr) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelUnsetMaximizedOpcode, 0, 1, 0)
	}
}

func (api *waylandAPI) xdgToplevelSetFullscreen(xdgToplevel uintptr, output uintptr) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetFullscreenOpcode, 0, 1, 0, output)
	}
}

func (api *waylandAPI) xdgToplevelUnsetFullscreen(xdgToplevel uintptr) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelUnsetFullscreenOpcode, 0, 1, 0)
	}
}

func (api *waylandAPI) xdgToplevelSetMinimized(xdgToplevel uintptr) {
	if api != nil && xdgToplevel != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelSetMinimizedOpcode, 0, 1, 0)
	}
}

func (api *waylandAPI) xdgToplevelMove(xdgToplevel uintptr, seat uintptr, serial uint32) {
	if api != nil && xdgToplevel != 0 && seat != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelMoveOpcode, 0, 1, 0, seat, serial)
	}
}

func (api *waylandAPI) xdgToplevelResize(xdgToplevel uintptr, seat uintptr, serial uint32, edges uint32) {
	if api != nil && xdgToplevel != 0 && seat != 0 {
		api.proxyMarshalFlags(xdgToplevel, xdgToplevelResizeOpcode, 0, 1, 0, seat, serial, edges)
	}
}

func (api *waylandAPI) getToplevelDecoration(decorationMgr uintptr, toplevel uintptr) uintptr {
	if api == nil || decorationMgr == 0 || toplevel == 0 {
		return 0
	}
	iface := api.getInterface("zxdg_toplevel_decoration_v1_interface", &zxdgToplevelDecorationV1Interface)
	return api.proxyMarshalFlags(decorationMgr, zxdgDecorationManagerV1GetToplevelDecorationOpcode, iface, 1, 0, uintptr(0), toplevel)
}

func (api *waylandAPI) setDecorationMode(decoration uintptr, mode uint32) {
	if api != nil && decoration != 0 {
		api.proxyMarshalFlags(decoration, zxdgToplevelDecorationV1SetModeOpcode, 0, 1, 0, mode)
	}
}

func (api *waylandAPI) decorationDestroy(decoration uintptr) {
	if api != nil && decoration != 0 {
		api.proxyMarshalFlags(decoration, zxdgToplevelDecorationV1DestroyOpcode, 0, 1, 0)
		api.proxyDestroy(decoration)
	}
}
