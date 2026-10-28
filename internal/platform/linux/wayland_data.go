//go:build linux

package linux

import (
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/vivaui/viva/internal/platform"
)

type waylandDataOffer struct {
	proxy     uintptr
	mimeTypes []string
}

func (off *waylandDataOffer) hasMime(mime string) bool {
	if off == nil {
		return false
	}
	for _, m := range off.mimeTypes {
		if m == mime {
			return true
		}
	}
	return false
}

func (off *waylandDataOffer) findTextMime() string {
	if off == nil {
		return ""
	}
	candidates := []string{
		"text/plain;charset=utf-8",
		"UTF8_STRING",
		"text/plain",
		"text/plain;charset=us-ascii",
	}
	for _, cand := range candidates {
		if off.hasMime(cand) {
			return cand
		}
	}
	return ""
}

type waylandDragState struct {
	win    *waylandWindow
	offer  *waylandDataOffer
	serial uint32
	x, y   float32
}

var (
	dataListenerOnce       sync.Once
	rawDataDeviceListener  [6]uintptr
	rawDataSourceListener  [6]uintptr
	rawDataOfferListener   [3]uintptr
	pinnerData             runtime.Pinner
)

func initDataListeners() {
	dataListenerOnce.Do(func() {
		// --- wl_data_device listeners ---
		// 0: data_offer(id: new_id wl_data_offer)
		rawDataDeviceListener[0] = purego.NewCallback(func(data unsafe.Pointer, device uintptr, id uintptr) {
			if data == nil || id == 0 {
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			defer app.mu.Unlock()
			if app.dataOffers == nil {
				app.dataOffers = make(map[uintptr]*waylandDataOffer)
			}
			offer := &waylandDataOffer{proxy: id}
			app.dataOffers[id] = offer
			if app.api != nil {
				app.api.proxyAddListener(id, uintptr(unsafe.Pointer(&rawDataOfferListener[0])), unsafe.Pointer(app))
			}
		})

		// 1: enter(serial: uint, surface: wl_surface, x: fixed, y: fixed, id: ?wl_data_offer)
		rawDataDeviceListener[1] = purego.NewCallback(func(data unsafe.Pointer, device uintptr, serial uint32, surface uintptr, sx, sy wlFixed, id uintptr) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.lastSerial = serial
			win := app.findWindowBySurface(surface)
			if win == nil {
				return
			}
			app.mu.Lock()
			offer := app.dataOffers[id]
			if offer == nil && id != 0 {
				offer = &waylandDataOffer{proxy: id}
				if app.dataOffers != nil {
					app.dataOffers[id] = offer
				}
			}
			x, y := win.pointerCoords(sx, sy)
			app.dragState = &waylandDragState{
				win:    win,
				offer:  offer,
				serial: serial,
				x:      x,
				y:      y,
			}
			app.mu.Unlock()

			if offer != nil && offer.hasMime("text/uri-list") {
				if app.api != nil {
					app.api.dataOfferAccept(id, serial, "text/uri-list")
				}
				if win.state != nil && win.state.config.OnFileDrop != nil {
					_, _, _ = win.state.config.OnFileDrop(platform.FileDropEvent{
						Phase: platform.FileDropOver,
						X:     x,
						Y:     y,
					})
				}
			}
		})

		// 2: leave()
		rawDataDeviceListener[2] = purego.NewCallback(func(data unsafe.Pointer, device uintptr) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			drag := app.dragState
			app.dragState = nil
			app.mu.Unlock()

			if drag != nil && drag.win != nil && drag.win.state != nil && drag.win.state.config.OnFileDrop != nil {
				_, _, _ = drag.win.state.config.OnFileDrop(platform.FileDropEvent{
					Phase: platform.FileDropLeave,
					X:     drag.x,
					Y:     drag.y,
				})
			}
		})

		// 3: motion(time: uint, x: fixed, y: fixed)
		rawDataDeviceListener[3] = purego.NewCallback(func(data unsafe.Pointer, device uintptr, timeMs uint32, sx, sy wlFixed) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			drag := app.dragState
			if drag == nil || drag.win == nil {
				app.mu.Unlock()
				return
			}
			x, y := drag.win.pointerCoords(sx, sy)
			drag.x, drag.y = x, y
			win := drag.win
			app.mu.Unlock()

			if win != nil && win.state != nil && win.state.config.OnFileDrop != nil {
				_, _, _ = win.state.config.OnFileDrop(platform.FileDropEvent{
					Phase: platform.FileDropOver,
					X:     x,
					Y:     y,
				})
			}
		})

		// 4: drop()
		rawDataDeviceListener[4] = purego.NewCallback(func(data unsafe.Pointer, device uintptr) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			drag := app.dragState
			app.dragState = nil
			app.mu.Unlock()

			if drag == nil || drag.win == nil {
				return
			}
			win := drag.win
			if drag.offer == nil || !drag.offer.hasMime("text/uri-list") {
				return
			}

			r, w, err := os.Pipe()
			if err != nil {
				return
			}
			if app.api != nil {
				app.api.dataOfferReceive(drag.offer.proxy, "text/uri-list", int32(w.Fd()))
				_ = w.Close()
				data, readErr := io.ReadAll(r)
				_ = r.Close()
				app.api.dataOfferFinish(drag.offer.proxy)
				if readErr == nil {
					paths := parseURIList(string(data))
					if len(paths) > 0 && win.state != nil && win.state.config.OnFileDrop != nil {
						frame, taken, dropErr := win.state.config.OnFileDrop(platform.FileDropEvent{
							Phase: platform.FileDropDrop,
							X:     drag.x,
							Y:     drag.y,
							Paths: paths,
						})
						if taken && dropErr == nil {
							_ = win.state.router.present(frame)
						}
					}
				}
			} else {
				_ = w.Close()
				_ = r.Close()
			}
		})

		// 5: selection(id: ?wl_data_offer)
		rawDataDeviceListener[5] = purego.NewCallback(func(data unsafe.Pointer, device uintptr, id uintptr) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			defer app.mu.Unlock()
			if id == 0 {
				app.clipboardOffer = nil
				return
			}
			offer := app.dataOffers[id]
			if offer == nil {
				offer = &waylandDataOffer{proxy: id}
				if app.dataOffers != nil {
					app.dataOffers[id] = offer
				}
			}
			app.clipboardOffer = offer
		})

		// --- wl_data_source listeners ---
		// 0: target(mime_type: ?string)
		rawDataSourceListener[0] = purego.NewCallback(func(data unsafe.Pointer, source uintptr, mimePtr *byte) {
		})

		// 1: send(mime_type: string, fd: fd)
		// Asynchronous non-blocking write to avoid UI thread deadlocks.
		rawDataSourceListener[1] = purego.NewCallback(func(data unsafe.Pointer, source uintptr, mimePtr *byte, fd int32) {
			if data == nil {
				if fd >= 0 {
					_ = syscall.Close(int(fd))
				}
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			owns := app.ownsClipboard
			text := app.clipboardText
			app.mu.Unlock()

			if !owns {
				if fd >= 0 {
					_ = syscall.Close(int(fd))
				}
				return
			}

			toWrite := []byte(text)
			go func(pipeFd int32, payload []byte) {
				f := os.NewFile(uintptr(pipeFd), "wl_data_source_send_pipe")
				defer f.Close()
				_, _ = f.Write(payload)
			}(fd, toWrite)
		})

		// 2: cancelled()
		rawDataSourceListener[2] = purego.NewCallback(func(data unsafe.Pointer, source uintptr) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.mu.Lock()
			defer app.mu.Unlock()
			app.ownsClipboard = false
			if app.dataSource == source {
				if app.api != nil {
					app.api.dataSourceDestroy(app.dataSource)
				}
				app.dataSource = 0
			}
		})

		// 3: dnd_drop_performed()
		rawDataSourceListener[3] = purego.NewCallback(func(data unsafe.Pointer, source uintptr) {
		})

		// 4: dnd_finished()
		rawDataSourceListener[4] = purego.NewCallback(func(data unsafe.Pointer, source uintptr) {
		})

		// 5: action(dnd_action: uint)
		rawDataSourceListener[5] = purego.NewCallback(func(data unsafe.Pointer, source uintptr, action uint32) {
		})

		// --- wl_data_offer listeners ---
		// 0: offer(mime_type: string)
		rawDataOfferListener[0] = purego.NewCallback(func(data unsafe.Pointer, offer uintptr, mimePtr *byte) {
			if data == nil || mimePtr == nil {
				return
			}
			app := (*waylandApplication)(data)
			mime := waylandGoString(mimePtr)
			app.mu.Lock()
			defer app.mu.Unlock()
			if off := app.dataOffers[offer]; off != nil {
				off.mimeTypes = append(off.mimeTypes, mime)
			}
		})

		// 1: source_actions(source_actions: uint)
		rawDataOfferListener[1] = purego.NewCallback(func(data unsafe.Pointer, offer uintptr, actions uint32) {
		})

		// 2: action(dnd_action: uint)
		rawDataOfferListener[2] = purego.NewCallback(func(data unsafe.Pointer, offer uintptr, action uint32) {
		})

		pinnerData.Pin(&rawDataDeviceListener)
		pinnerData.Pin(&rawDataSourceListener)
		pinnerData.Pin(&rawDataOfferListener)
	})
}

func (app *waylandApplication) setupDataDevice() {
	if app.globals.dataDeviceManager == 0 || app.globals.seat == 0 || app.dataDevice != 0 || app.api == nil {
		return
	}
	initDataListeners()
	app.dataOffers = make(map[uintptr]*waylandDataOffer)
	app.dataDevice = app.api.getDataDevice(app.globals.dataDeviceManager, app.globals.seat)
	if app.dataDevice != 0 {
		app.api.proxyAddListener(app.dataDevice, uintptr(unsafe.Pointer(&rawDataDeviceListener[0])), unsafe.Pointer(app))
	}
}

func (app *waylandApplication) teardownDataDevice() {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.dataSource != 0 && app.api != nil {
		app.api.dataSourceDestroy(app.dataSource)
		app.dataSource = 0
	}
	if app.dataDevice != 0 && app.api != nil {
		if app.globals.dataDeviceManagerVer >= 2 {
			app.api.dataDeviceRelease(app.dataDevice)
		} else {
			app.api.proxyDestroy(app.dataDevice)
		}
		app.dataDevice = 0
	}
	app.clipboardOffer = nil
	app.dataOffers = nil
	app.dragState = nil
}

func (app *waylandApplication) setClipboardText(text string) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.globals.dataDeviceManager == 0 || app.dataDevice == 0 || app.api == nil {
		return errors.New("linux: Wayland data device not available")
	}

	if app.dataSource != 0 {
		app.api.dataSourceDestroy(app.dataSource)
		app.dataSource = 0
	}

	app.dataSource = app.api.createDataSource(app.globals.dataDeviceManager)
	if app.dataSource == 0 {
		return errors.New("linux: failed to create Wayland data source")
	}

	app.api.proxyAddListener(app.dataSource, uintptr(unsafe.Pointer(&rawDataSourceListener[0])), unsafe.Pointer(app))
	app.clipboardText = text
	app.ownsClipboard = true

	app.api.dataSourceOffer(app.dataSource, "text/plain;charset=utf-8")
	app.api.dataSourceOffer(app.dataSource, "UTF8_STRING")
	app.api.dataSourceOffer(app.dataSource, "text/plain")

	serial := app.lastSerial
	if serial == 0 {
		serial = app.pointerSerial
	}
	app.api.dataDeviceSetSelection(app.dataDevice, app.dataSource, serial)
	if app.display != 0 {
		app.api.displayFlush(app.display)
	}
	return nil
}

func (app *waylandApplication) getClipboardText() (string, error) {
	app.mu.Lock()
	if app.ownsClipboard {
		text := app.clipboardText
		app.mu.Unlock()
		return text, nil
	}

	off := app.clipboardOffer
	if off == nil || off.proxy == 0 || app.api == nil {
		app.mu.Unlock()
		return "", nil
	}

	chosenMime := off.findTextMime()
	if chosenMime == "" {
		app.mu.Unlock()
		return "", nil
	}

	r, w, err := os.Pipe()
	if err != nil {
		app.mu.Unlock()
		return "", err
	}

	app.api.dataOfferReceive(off.proxy, chosenMime, int32(w.Fd()))
	_ = w.Close()
	if app.display != 0 {
		app.api.displayFlush(app.display)
	}
	app.mu.Unlock()

	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		defer r.Close()
		d, e := io.ReadAll(r)
		ch <- readResult{data: d, err: e}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		return string(res.data), nil
	case <-time.After(2 * time.Second):
		_ = r.Close()
		return "", errors.New("linux: Wayland clipboard read timed out")
	}
}
