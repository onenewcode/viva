//go:build linux

package linux

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/vivaui/viva/internal/input"
	"github.com/vivaui/viva/internal/platform"
)

const (
	xkbContextNoFlags       uint32 = 0
	xkbKeymapFormatTextV1   uint32 = 1
	xkbKeymapCompileNoFlags uint32 = 0
	xkbStateModsEffective   uint32 = 8
)

var (
	strModShift   = []byte("Shift\x00")
	strModControl = []byte("Control\x00")
	strModAlt     = []byte("Mod1\x00")
	strModSuper   = []byte("Mod4\x00")
)

type xkbAPI struct {
	library uintptr

	contextNew           func(flags uint32) uintptr
	contextUnref         func(context uintptr)
	keymapNewFromString  func(context uintptr, str *byte, format uint32, flags uint32) uintptr
	keymapUnref          func(keymap uintptr)
	stateNew             func(keymap uintptr) uintptr
	stateUnref           func(state uintptr)
	stateKeyGetOneSym    func(state uintptr, key uint32) uint32
	stateUpdateMask      func(state uintptr, depressed, latched, locked, depLayout, latchLayout, lockLayout uint32) uint32
	stateModNameIsActive func(state uintptr, name *byte, typ uint32) int32
}

func loadXKB() (*xkbAPI, error) {
	lib, err := purego.Dlopen("libxkbcommon.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("linux: failed to load libxkbcommon.so.0: %w", err)
	}
	api := &xkbAPI{library: lib}
	purego.RegisterLibFunc(&api.contextNew, lib, "xkb_context_new")
	purego.RegisterLibFunc(&api.contextUnref, lib, "xkb_context_unref")
	purego.RegisterLibFunc(&api.keymapNewFromString, lib, "xkb_keymap_new_from_string")
	purego.RegisterLibFunc(&api.keymapUnref, lib, "xkb_keymap_unref")
	purego.RegisterLibFunc(&api.stateNew, lib, "xkb_state_new")
	purego.RegisterLibFunc(&api.stateUnref, lib, "xkb_state_unref")
	purego.RegisterLibFunc(&api.stateKeyGetOneSym, lib, "xkb_state_key_get_one_sym")
	purego.RegisterLibFunc(&api.stateUpdateMask, lib, "xkb_state_update_mask")
	purego.RegisterLibFunc(&api.stateModNameIsActive, lib, "xkb_state_mod_name_is_active")
	return api, nil
}

func (api *xkbAPI) close() {
	if api != nil && api.library != 0 {
		purego.Dlclose(api.library)
		api.library = 0
	}
}

var (
	keyboardListenerOnce sync.Once
	rawKeyboardListener  [6]uintptr
	pinnerKeyboard       runtime.Pinner
)

func initKeyboardListeners() {
	keyboardListenerOnce.Do(func() {
		// 0: keymap(format, fd, size)
		rawKeyboardListener[0] = purego.NewCallback(func(data unsafe.Pointer, keyboard uintptr, format uint32, fd int32, size uint32) {
			if data == nil {
				if fd >= 0 {
					_ = syscall.Close(int(fd))
				}
				return
			}
			app := (*waylandApplication)(data)
			app.handleKeymap(format, fd, size)
		})

		// 1: enter(serial, surface, keys)
		rawKeyboardListener[1] = purego.NewCallback(func(data unsafe.Pointer, keyboard uintptr, serial uint32, surface uintptr, keys unsafe.Pointer) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.lastSerial = serial
			app.keyboardFocus = app.findWindowBySurface(surface)
			if app.keyboardFocus != nil && app.keyboardFocus.state != nil {
				app.atspiActive(app.keyboardFocus.state, true)
				if app.keyboardFocus.state.router.ime != nil {
					app.keyboardFocus.state.router.ime.setFocused(true)
				}
			}
		})

		// 2: leave(serial, surface)
		rawKeyboardListener[2] = purego.NewCallback(func(data unsafe.Pointer, keyboard uintptr, serial uint32, surface uintptr) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.stopKeyRepeat()
			if app.keyboardFocus != nil && app.keyboardFocus.state != nil {
				app.atspiActive(app.keyboardFocus.state, false)
				if app.keyboardFocus.state.router.ime != nil {
					app.keyboardFocus.state.router.ime.setFocused(false)
				}
				if app.keyboardFocus.state.config.OnFocusLost != nil {
					frame, err := app.keyboardFocus.state.config.OnFocusLost()
					if err == nil && app.keyboardFocus.state.router.present != nil {
						err = app.keyboardFocus.state.router.present(frame)
					}
					app.fail(err)
				}
			}
			app.keyboardFocus = nil
		})

		// 3: key(serial, time, key, state)
		rawKeyboardListener[3] = purego.NewCallback(func(data unsafe.Pointer, keyboard uintptr, serial uint32, timeMs uint32, scancode uint32, state uint32) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.lastSerial = serial
			app.handleKey(serial, timeMs, scancode, state)
		})

		// 4: modifiers(serial, modsDepressed, modsLatched, modsLocked, group)
		rawKeyboardListener[4] = purego.NewCallback(func(data unsafe.Pointer, keyboard uintptr, serial uint32, depressed, latched, locked, group uint32) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.handleModifiers(depressed, latched, locked, group)
		})

		// 5: repeat_info(rate, delay)
		rawKeyboardListener[5] = purego.NewCallback(func(data unsafe.Pointer, keyboard uintptr, rate int32, delay int32) {
			if data == nil {
				return
			}
			app := (*waylandApplication)(data)
			app.repeatRate = rate
			app.repeatDelay = delay
		})

		pinnerKeyboard.Pin(&rawKeyboardListener)
	})
}

func (app *waylandApplication) setupKeyboard() {
	if app.keyboard != 0 || app.globals.seat == 0 || app.api == nil {
		return
	}
	keyboard := app.api.seatGetKeyboard(app.globals.seat)
	if keyboard == 0 {
		return
	}
	app.keyboard = keyboard

	if app.xkb == nil {
		if xkb, err := loadXKB(); err == nil {
			app.xkb = xkb
			app.xkbContext = xkb.contextNew(xkbContextNoFlags)
		}
	}

	initKeyboardListeners()
	app.api.proxyAddListener(keyboard, uintptr(unsafe.Pointer(&rawKeyboardListener[0])), unsafe.Pointer(app))
}

func (app *waylandApplication) teardownKeyboard() {
	app.stopKeyRepeat()
	if app.keyboard != 0 && app.api != nil {
		app.api.keyboardRelease(app.keyboard)
		app.keyboard = 0
		app.keyboardFocus = nil
	}
	if app.xkbState != 0 && app.xkb != nil {
		app.xkb.stateUnref(app.xkbState)
		app.xkbState = 0
	}
	if app.xkbKeymap != 0 && app.xkb != nil {
		app.xkb.keymapUnref(app.xkbKeymap)
		app.xkbKeymap = 0
	}
	if app.xkbContext != 0 && app.xkb != nil {
		app.xkb.contextUnref(app.xkbContext)
		app.xkbContext = 0
	}
	if app.xkb != nil {
		app.xkb.close()
		app.xkb = nil
	}
}

func (app *waylandApplication) handleKeymap(format uint32, fd int32, size uint32) {
	defer func() {
		if app.libc != nil && app.libc.close != nil && fd >= 0 {
			app.libc.close(fd)
		} else if fd >= 0 {
			_ = syscall.Close(int(fd))
		}
	}()

	if size == 0 || app.xkb == nil || app.xkbContext == 0 || app.libc == nil || app.libc.read == nil {
		return
	}

	_, _ = syscall.Seek(int(fd), 0, 0)
	data := make([]byte, size+1)
	totalRead := 0
	for totalRead < int(size) {
		n := app.libc.read(fd, unsafe.Pointer(&data[totalRead]), uintptr(int(size)-totalRead))
		if n <= 0 {
			break
		}
		totalRead += n
	}
	data[totalRead] = 0 // null terminate

	keymap := app.xkb.keymapNewFromString(app.xkbContext, &data[0], xkbKeymapFormatTextV1, xkbKeymapCompileNoFlags)
	if keymap == 0 {
		return
	}
	state := app.xkb.stateNew(keymap)
	if state == 0 {
		app.xkb.keymapUnref(keymap)
		return
	}

	if app.xkbState != 0 {
		app.xkb.stateUnref(app.xkbState)
	}
	if app.xkbKeymap != 0 {
		app.xkb.keymapUnref(app.xkbKeymap)
	}
	app.xkbKeymap = keymap
	app.xkbState = state
}

func (app *waylandApplication) handleModifiers(depressed, latched, locked, group uint32) {
	if app.xkb != nil && app.xkbState != 0 {
		app.xkb.stateUpdateMask(app.xkbState, depressed, latched, locked, 0, 0, group)

		var mods input.Modifiers
		if app.xkb.stateModNameIsActive(app.xkbState, &strModShift[0], xkbStateModsEffective) > 0 {
			mods |= input.ModifierShift
		}
		if app.xkb.stateModNameIsActive(app.xkbState, &strModControl[0], xkbStateModsEffective) > 0 {
			mods |= input.ModifierControl
		}
		if app.xkb.stateModNameIsActive(app.xkbState, &strModAlt[0], xkbStateModsEffective) > 0 {
			mods |= input.ModifierAlt
		}
		if app.xkb.stateModNameIsActive(app.xkbState, &strModSuper[0], xkbStateModsEffective) > 0 {
			mods |= input.ModifierMeta
		}
		app.modifiers = mods
	}
}

func ibusModifierMask(modifiers input.Modifiers) uint32 {
	var state uint32
	if modifiers&input.ModifierShift != 0 {
		state |= xcbModShift
	}
	if modifiers&input.ModifierControl != 0 {
		state |= xcbModControl
	}
	if modifiers&input.ModifierAlt != 0 {
		state |= xcbMod1
	}
	if modifiers&input.ModifierMeta != 0 {
		state |= xcbMod4
	}
	return state
}

func (app *waylandApplication) resolveKeysym(scancode uint32) uint32 {
	if app.xkb != nil && app.xkbState != 0 {
		return app.xkb.stateKeyGetOneSym(app.xkbState, scancode+8)
	}
	return fallbackEvdevToKeysym(scancode)
}

func (app *waylandApplication) handleKey(serial, timeMs, scancode, state uint32) {
	if app.keyboardFocus == nil || app.keyboardFocus.state == nil {
		return
	}
	win := app.keyboardFocus
	isDown := (state == 1)

	keysym := app.resolveKeysym(scancode)
	normKey := normalizedKeysym(keysym)

	keyEvent := platform.KeyEvent{
		Key:       normKey,
		Modifiers: app.modifiers,
		Release:   !isDown,
		Code:      scancode,
	}

	if r, ok := keysymRune(keysym); ok {
		keyEvent.Text = string(r)
		keyEvent.Typed = string(r)
	}

	if isDown {
		app.stopKeyRepeat()
		if win.state.router.ime != nil && win.state.router.ime.processKeyDirect(keysym, scancode, ibusModifierMask(app.modifiers), keyEvent) {
			return
		}
		app.startKeyRepeat(keyEvent, scancode)
		app.fail(win.state.router.dispatchKey(keyEvent))
	} else {
		app.mu.Lock()
		isRepeatingKey := (scancode == app.repeatScancode)
		app.mu.Unlock()
		if isRepeatingKey {
			app.stopKeyRepeat()
		}
		if win.state.router.ime != nil {
			win.state.router.ime.releaseDirect(keysym, scancode, ibusModifierMask(app.modifiers))
		}
		if win.state.router.wantsKeyReleases() {
			app.fail(win.state.router.dispatchKey(keyEvent))
		}
	}
}

func (app *waylandApplication) startKeyRepeat(keyEvent platform.KeyEvent, scancode uint32) {
	if app.repeatRate <= 0 {
		return
	}

	delay := time.Duration(app.repeatDelay) * time.Millisecond
	if delay <= 0 {
		delay = 400 * time.Millisecond
	}
	interval := time.Second / time.Duration(app.repeatRate)
	if interval <= 0 {
		interval = 40 * time.Millisecond
	}
	now := app.timeNow()

	app.mu.Lock()
	app.repeatKey = keyEvent
	app.repeatKey.Repeat = true
	app.repeatScancode = scancode
	app.repeatWindow = app.keyboardFocus
	app.repeatInterval = interval
	app.repeatDeadline = now.Add(delay)
	app.mu.Unlock()
}

func (app *waylandApplication) runKeyRepeat(now time.Time) {
	app.mu.Lock()
	if app.repeatScancode == 0 || now.Before(app.repeatDeadline) {
		app.mu.Unlock()
		return
	}
	win := app.repeatWindow
	keyEvent := app.repeatKey
	scancode := app.repeatScancode
	generation := app.repeatGeneration
	deadline := app.repeatDeadline
	interval := app.repeatInterval
	app.mu.Unlock()

	if win == nil || win.closed.Load() || win.state == nil {
		app.stopKeyRepeat()
		return
	}
	app.dispatchKeyRepeat(win, keyEvent, scancode, generation)
	if interval <= 0 {
		interval = 40 * time.Millisecond
	}

	app.mu.Lock()
	if app.repeatGeneration == generation && app.repeatScancode == scancode && app.repeatWindow == win {
		next := deadline.Add(interval)
		if !next.After(now) {
			next = now.Add(interval)
		}
		app.repeatDeadline = next
	}
	app.mu.Unlock()
}

func (app *waylandApplication) dispatchKeyRepeat(win *waylandWindow, keyEvent platform.KeyEvent, scancode uint32, generation uint64) {
	app.mu.Lock()
	active := app.repeatScancode == scancode && app.repeatGeneration == generation && app.repeatWindow == win && app.keyboardFocus == win
	app.mu.Unlock()
	if !active || win == nil || win.closed.Load() || win.state == nil {
		return
	}
	app.fail(win.state.router.dispatchKey(keyEvent))
}

func (app *waylandApplication) stopKeyRepeat() {
	app.mu.Lock()
	defer app.mu.Unlock()
	app.repeatGeneration++
	app.repeatScancode = 0
	app.repeatWindow = nil
	app.repeatDeadline = time.Time{}
	app.repeatInterval = 0
}

// fallbackEvdevToKeysym handles basic ASCII keys when libxkbcommon is not present.
func fallbackEvdevToKeysym(scancode uint32) uint32 {
	switch scancode {
	case 1:
		return keysymEscape
	case 14:
		return keysymBackspace
	case 15:
		return keysymTab
	case 28:
		return keysymEnter
	case 57:
		return ' '
	case 103:
		return keysymUp
	case 105:
		return keysymLeft
	case 106:
		return keysymRight
	case 108:
		return keysymDown
	case 111:
		return keysymDelete
	default:
		return 0
	}
}
