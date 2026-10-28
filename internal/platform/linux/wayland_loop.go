//go:build linux

package linux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	efdCloexec  = 0x80000
	efdNonblock = 0x800
)

type waylandCommand func()

type libcAPI struct {
	library uintptr
	poll    func(*pollFD, uintptr, int32) int32
	eventfd func(uint32, int32) int32
	read    func(int32, unsafe.Pointer, uintptr) int
	write   func(int32, unsafe.Pointer, uintptr) int
	close   func(int32) int32
}

func loadLibc() (*libcAPI, error) {
	library, err := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("linux: failed to load libc.so.6: %w", err)
	}
	api := &libcAPI{library: library}
	purego.RegisterLibFunc(&api.poll, library, "poll")
	purego.RegisterLibFunc(&api.eventfd, library, "eventfd")
	purego.RegisterLibFunc(&api.read, library, "read")
	purego.RegisterLibFunc(&api.write, library, "write")
	purego.RegisterLibFunc(&api.close, library, "close")
	return api, nil
}

func (c *libcAPI) teardown() {
	if c != nil && c.library != 0 {
		purego.Dlclose(c.library)
		c.library = 0
	}
}

// waylandLoop coordinates the Wayland display socket, D-Bus sockets, and cross-goroutine eventfd waker in a single poll loop.
type waylandLoop struct {
	libc      *libcAPI
	wayland   *waylandAPI
	display   uintptr
	waylandFD int32
	wakeFD    int32

	cmdMu    sync.Mutex
	cmdQueue []waylandCommand

	running atomic.Bool
	pollFDs []pollFD

	extraFDs []int32
	extraCBs []func()
}

func newWaylandLoop(api *waylandAPI, display uintptr, libc *libcAPI) (*waylandLoop, error) {
	if api == nil || display == 0 {
		return nil, errors.New("linux: invalid wayland API or display")
	}
	if libc == nil {
		var err error
		libc, err = loadLibc()
		if err != nil {
			return nil, err
		}
	}
	wfd := api.displayGetFd(display)
	if wfd < 0 {
		return nil, fmt.Errorf("linux: failed to get wayland display fd: %d", wfd)
	}
	wakeFD := int32(-1)
	if libc.eventfd != nil {
		wakeFD = libc.eventfd(0, efdCloexec|efdNonblock)
	}
	if wakeFD < 0 {
		return nil, errors.New("linux: failed to create eventfd for wayland loop waker")
	}

	loop := &waylandLoop{
		libc:      libc,
		wayland:   api,
		display:   display,
		waylandFD: wfd,
		wakeFD:    wakeFD,
	}
	loop.running.Store(true)
	loop.rebuildPollFDs()
	return loop, nil
}

func (l *waylandLoop) rebuildPollFDs() {
	total := 2 + len(l.extraFDs)
	if cap(l.pollFDs) >= total {
		l.pollFDs = l.pollFDs[:total]
	} else {
		l.pollFDs = make([]pollFD, total)
	}
	l.pollFDs[0] = pollFD{FD: l.waylandFD, Events: 1} // POLLIN = 1
	l.pollFDs[1] = pollFD{FD: l.wakeFD, Events: 1}
	for i, fd := range l.extraFDs {
		l.pollFDs[2+i] = pollFD{FD: fd, Events: 1}
	}
}

func (l *waylandLoop) addExtraFD(fd int32, onReady func()) {
	if fd < 0 {
		return
	}
	l.extraFDs = append(l.extraFDs, fd)
	l.extraCBs = append(l.extraCBs, onReady)
	l.rebuildPollFDs()
}

func (l *waylandLoop) post(cmd waylandCommand) {
	if cmd == nil {
		return
	}
	l.cmdMu.Lock()
	l.cmdQueue = append(l.cmdQueue, cmd)
	l.cmdMu.Unlock()

	var val [8]byte
	binary.NativeEndian.PutUint64(val[:], 1)
	if l.libc != nil && l.libc.write != nil && l.wakeFD >= 0 {
		l.libc.write(l.wakeFD, unsafe.Pointer(&val[0]), 8)
	}
}

func (l *waylandLoop) drainCommands() {
	l.cmdMu.Lock()
	cmds := l.cmdQueue
	l.cmdQueue = nil
	l.cmdMu.Unlock()

	for _, cmd := range cmds {
		if cmd != nil {
			cmd()
		}
	}
}

func (l *waylandLoop) step(timeoutMs int32) bool {
	if !l.running.Load() {
		return false
	}

	if l.wayland != nil && l.wayland.displayFlush != nil {
		l.wayland.displayFlush(l.display)
	}

	if l.libc != nil && l.libc.poll != nil {
		ret := l.libc.poll(&l.pollFDs[0], uintptr(len(l.pollFDs)), timeoutMs)
		if ret < 0 {
			return l.running.Load()
		}
	}

	if len(l.pollFDs) > 1 && l.pollFDs[1].REvents&1 != 0 {
		var val [8]byte
		if l.libc != nil && l.libc.read != nil && l.wakeFD >= 0 {
			l.libc.read(l.wakeFD, unsafe.Pointer(&val[0]), 8)
		}
		l.drainCommands()
	}

	for i, cb := range l.extraCBs {
		if len(l.pollFDs) > 2+i && l.pollFDs[2+i].REvents&1 != 0 && cb != nil {
			cb()
		}
	}

	if len(l.pollFDs) > 0 && l.pollFDs[0].REvents&1 != 0 {
		if l.wayland != nil && l.wayland.displayDispatch != nil {
			if l.wayland.displayDispatch(l.display) < 0 {
				l.running.Store(false)
				return false
			}
		}
	} else if l.wayland != nil && l.wayland.displayDispatchPending != nil {
		l.wayland.displayDispatchPending(l.display)
	}

	if len(l.pollFDs) > 0 && l.pollFDs[0].REvents&(0x0008|0x0010) != 0 {
		l.running.Store(false)
		return false
	}

	return l.running.Load()
}

func (l *waylandLoop) stop() {
	l.running.Store(false)
	l.post(func() {})
}

func (l *waylandLoop) close() {
	l.stop()
	if l.libc != nil && l.libc.close != nil && l.wakeFD >= 0 {
		l.libc.close(l.wakeFD)
		l.wakeFD = -1
	}
}
