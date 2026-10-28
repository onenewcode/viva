//go:build linux

package linux

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/internal/platform"
)

type platformBackend interface {
	platform.Application
	Run(start platform.ApplicationStart) error
}

func selectBackend() (platformBackend, error) {
	target := os.Getenv("VIVA_LINUX_BACKEND")

	switch target {
	case "wayland":
		waylandApp, err := newWaylandApplication()
		if err != nil {
			return nil, fmt.Errorf("linux: forced Wayland backend failed: %w", err)
		}
		return waylandApp, nil

	case "x11":
		if os.Getenv("DISPLAY") == "" {
			return nil, platform.ErrNoGraphics
		}
		return &linuxApplication{}, nil

	case "":
		// Automatic probing: prefer Wayland, fallback to X11
		if hasWaylandEnvironment() {
			if waylandApp, err := newWaylandApplication(); err == nil {
				return waylandApp, nil
			}
		}

		if os.Getenv("DISPLAY") != "" {
			return &linuxApplication{}, nil
		}

		return nil, platform.ErrNoGraphics

	default:
		return nil, fmt.Errorf("linux: unknown VIVA_LINUX_BACKEND %q (must be 'wayland' or 'x11')", target)
	}
}

func hasWaylandEnvironment() bool {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir != "" {
		socketPath := filepath.Join(runtimeDir, "wayland-0")
		if _, err := os.Stat(socketPath); err == nil {
			return true
		}
	}
	return false
}
