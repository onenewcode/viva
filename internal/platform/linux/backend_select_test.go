//go:build linux

package linux

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/internal/platform"
)

func TestSelectBackendForcedX11(t *testing.T) {
	t.Setenv("VIVA_LINUX_BACKEND", "x11")
	t.Setenv("DISPLAY", ":0")

	backend, err := selectBackend()
	if err != nil {
		t.Fatalf("selectBackend failed with forced x11: %v", err)
	}
	if _, ok := backend.(*linuxApplication); !ok {
		t.Fatalf("expected *linuxApplication, got %T", backend)
	}

	// Forced x11 with no DISPLAY -> ErrNoGraphics
	t.Setenv("DISPLAY", "")
	_, err = selectBackend()
	if !errors.Is(err, platform.ErrNoGraphics) {
		t.Fatalf("expected ErrNoGraphics when DISPLAY unset, got %v", err)
	}
}

func TestSelectBackendForcedWaylandFailure(t *testing.T) {
	t.Setenv("VIVA_LINUX_BACKEND", "wayland")
	t.Setenv("WAYLAND_DISPLAY", "nonexistent-wayland-socket")
	t.Setenv("XDG_RUNTIME_DIR", "/nonexistent-runtime-dir")
	t.Setenv("DISPLAY", ":0") // DISPLAY is present, but should NOT fallback to X11!

	_, err := selectBackend()
	if err == nil || !strings.Contains(err.Error(), "forced Wayland backend failed") {
		t.Fatalf("expected forced Wayland failure error without fallback, got %v", err)
	}
}

func TestSelectBackendInvalidName(t *testing.T) {
	t.Setenv("VIVA_LINUX_BACKEND", "mir")

	_, err := selectBackend()
	if err == nil || !strings.Contains(err.Error(), "unknown VIVA_LINUX_BACKEND") {
		t.Fatalf("expected unknown backend error, got %v", err)
	}
}

func TestSelectBackendNoDisplayEnvironment(t *testing.T) {
	t.Setenv("VIVA_LINUX_BACKEND", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", "/nonexistent-dir")
	t.Setenv("DISPLAY", "")

	backend, err := selectBackend()
	if backend != nil {
		t.Fatalf("expected nil backend, got %v", backend)
	}
	if !errors.Is(err, platform.ErrNoGraphics) {
		t.Fatalf("expected ErrNoGraphics, got %v", err)
	}
}

func TestSelectBackendFallbackToX11WhenWaylandAbsent(t *testing.T) {
	t.Setenv("VIVA_LINUX_BACKEND", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", "/nonexistent-dir")
	t.Setenv("DISPLAY", ":0")

	backend, err := selectBackend()
	if err != nil {
		t.Fatalf("expected successful fallback to X11, got err: %v", err)
	}
	if _, ok := backend.(*linuxApplication); !ok {
		t.Fatalf("expected *linuxApplication on fallback, got %T", backend)
	}
}
