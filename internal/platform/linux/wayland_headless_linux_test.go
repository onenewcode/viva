//go:build linux

package linux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/internal/platform"
)

func waitWaylandHeadlessState(app *waylandApplication, runDone <-chan struct{}, check func() bool) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		result := make(chan bool, 1)
		app.post(func() { result <- check() })
		select {
		case ready := <-result:
			if ready {
				return nil
			}
		case <-runDone:
			return errors.New("application stopped before the expected state")
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for window state")
		}
	}
}

func waylandHeadlessSize(app *waylandApplication, win *waylandWindow, runDone <-chan struct{}) ([2]uint32, error) {
	result := make(chan [2]uint32, 1)
	app.post(func() { result <- [2]uint32{win.width, win.height} })
	select {
	case size := <-result:
		return size, nil
	case <-runDone:
		return [2]uint32{}, errors.New("application stopped while reading window size")
	case <-time.After(20 * time.Second):
		return [2]uint32{}, errors.New("timed out reading window size")
	}
}

func driveWaylandHeadlessWindow(app *waylandApplication, win *waylandWindow, runDone <-chan struct{}, compositor *exec.Cmd) error {
	wait := func(check func() bool) error {
		return waitWaylandHeadlessState(app, runDone, check)
	}
	if err := wait(func() bool { return win.surfaceState == waylandSurfaceMapped }); err != nil {
		return fmt.Errorf("initial map: %w", err)
	}

	win.Hide()
	if err := wait(func() bool { return win.surfaceState == waylandSurfaceUnmapped }); err != nil {
		return fmt.Errorf("hide: %w", err)
	}
	win.Show()
	if err := wait(func() bool { return win.surfaceState == waylandSurfaceMapped }); err != nil {
		return fmt.Errorf("show: %w", err)
	}

	before, err := waylandHeadlessSize(app, win, runDone)
	if err != nil {
		return fmt.Errorf("initial size: %w", err)
	}
	app.post(func() { win.setMaximized(true) })
	if err := wait(func() bool {
		return win.maximized && (win.width != before[0] || win.height != before[1])
	}); err != nil {
		return fmt.Errorf("maximize resize: %w", err)
	}
	app.post(func() { win.setMaximized(false) })
	if err := wait(func() bool {
		return !win.maximized && win.width == before[0] && win.height == before[1]
	}); err != nil {
		return fmt.Errorf("restore resize: %w", err)
	}

	if err := compositor.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("stop compositor: %w", err)
	}
	return nil
}

// TestWaylandHeadlessIntegration exercises real libwayland, xdg-shell and
// Vulkan WSI against Weston and the software Vulkan driver selected by the environment.
func TestWaylandHeadlessIntegration(t *testing.T) {
	if os.Getenv("VIVA_WAYLAND_INTEGRATION") != "1" {
		t.Skip("set VIVA_WAYLAND_INTEGRATION=1 to run the Weston/Vulkan integration test")
	}
	weston, err := exec.LookPath("weston")
	if err != nil {
		t.Fatalf("Weston is required for integration validation: %v", err)
	}
	runtimeDir := t.TempDir()
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatalf("runtime directory permissions: %v", err)
	}
	socket := fmt.Sprintf("viva-test-%d", os.Getpid())
	command := exec.Command(weston, "--backend=headless", "--renderer=pixman", "--socket="+socket, "--idle-time=0")
	command.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "WAYLAND_DISPLAY="+socket, "LIBGL_ALWAYS_SOFTWARE=1", "HOME="+runtimeDir)
	var compositorLog bytes.Buffer
	command.Stdout = &compositorLog
	command.Stderr = &compositorLog
	if err := command.Start(); err != nil {
		t.Fatalf("start Weston: %v", err)
	}
	compositorDone := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(compositorDone)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-compositorDone
		if compositorLog.Len() != 0 {
			t.Logf("Weston output:\n%s", compositorLog.String())
		}
	})
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("WAYLAND_DISPLAY", socket)
	t.Setenv("LIBGL_ALWAYS_SOFTWARE", "1")

	socketPath := filepath.Join(runtimeDir, socket)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		select {
		case <-compositorDone:
			t.Fatalf("Weston exited before creating its socket:\n%s", compositorLog.String())
		case <-time.After(25 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the Weston socket")
		}
	}

	app, err := newWaylandApplication()
	if err != nil {
		t.Fatalf("connect to Weston: %v", err)
	}
	runDone := make(chan struct{})
	driverResult := make(chan error, 1)
	driverStarted := false
	runErr := app.Run(func(platform.Application) error {
		win, err := app.openWindow(platform.Config{Width: 320, Height: 240})
		if err != nil {
			return err
		}
		driverStarted = true
		go func() {
			if err := driveWaylandHeadlessWindow(app, win, runDone, command); err != nil {
				driverResult <- err
				app.Quit()
				return
			}
			driverResult <- nil
		}()
		return nil
	})
	close(runDone)
	if !driverStarted {
		t.Fatalf("window integration did not start: %v", runErr)
	}
	if err := <-driverResult; err != nil {
		t.Fatalf("window integration: %v (application error: %v)", err, runErr)
	}
	if runErr == nil {
		t.Fatal("compositor disconnect returned a normal application exit")
	}
}
