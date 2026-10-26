//go:build darwin || windows || linux

package app

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

// TestDriverReportsWindowFrameAndMinimizeState exercises the headless driver the
// way a window manager would move, resize and minimize a real window.
func TestDriverReportsWindowFrameAndMinimizeState(t *testing.T) {
	var frames []ui.Rect
	var minimizes []bool
	var maximizes []bool
	origin := ui.Point{X: 10, Y: 20}
	driver, err := NewDriver(Config{
		Title: "main", Width: 200, Height: 100, Origin: &origin,
		OnFrameChange: func(frame ui.Rect) { frames = append(frames, frame) },
		OnMinimize:    func(minimized bool) { minimizes = append(minimizes, minimized) },
		OnMaximize:    func(maximized bool) { maximizes = append(maximizes, maximized) },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	if got, want := driver.Frame(), (ui.Rect{X: 10, Y: 20, Width: 200, Height: 100}); got != want {
		t.Fatalf("initial frame = %+v, want %+v", got, want)
	}
	if err := driver.MoveWindow(30, 40); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Frame(), (ui.Rect{X: 30, Y: 40, Width: 200, Height: 100}); got != want {
		t.Fatalf("frame after move = %+v, want %+v", got, want)
	}
	if err := driver.Resize(400, 300); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Frame(), (ui.Rect{X: 30, Y: 40, Width: 400, Height: 300}); got != want {
		t.Fatalf("frame after resize = %+v, want %+v", got, want)
	}
	if len(frames) != 2 || frames[0] != (ui.Rect{X: 30, Y: 40, Width: 200, Height: 100}) ||
		frames[1] != (ui.Rect{X: 30, Y: 40, Width: 400, Height: 300}) {
		t.Fatalf("frame changes = %+v", frames)
	}

	if err := driver.SetMinimized(true); err != nil {
		t.Fatal(err)
	}
	if !driver.Minimized() {
		t.Fatal("Minimized did not follow SetMinimized")
	}
	if err := driver.SetMinimized(true); err != nil {
		t.Fatal(err)
	}
	if err := driver.SetMinimized(false); err != nil {
		t.Fatal(err)
	}
	if driver.Minimized() {
		t.Fatal("Minimized did not follow the restore")
	}
	if len(minimizes) != 2 || !minimizes[0] || minimizes[1] {
		t.Fatalf("minimize changes = %v", minimizes)
	}

	if err := driver.SetMaximized(true); err != nil {
		t.Fatal(err)
	}
	if !driver.Maximized() {
		t.Fatal("Maximized did not follow SetMaximized")
	}
	if err := driver.SetMaximized(true); err != nil {
		t.Fatal(err)
	}
	if err := driver.SetMaximized(false); err != nil {
		t.Fatal(err)
	}
	if driver.Maximized() {
		t.Fatal("Maximized did not follow the restore")
	}
	if len(maximizes) != 2 || !maximizes[0] || maximizes[1] {
		t.Fatalf("maximize changes = %v", maximizes)
	}
}
