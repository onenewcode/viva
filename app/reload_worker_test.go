//go:build vivadev && (darwin || windows || linux)

package app

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestReloadWorkerConfigForwardsInputHandlers(t *testing.T) {
	pointer := ui.PointerEvent{Kind: ui.PointerDown, Target: ui.Item(3)}
	key := ui.KeyEvent{Key: ui.KeyEnter, Target: ui.Item(5)}
	scroll := ui.ScrollEvent{
		Modifiers: ui.ModifierControl, X: 10, Y: 20, DeltaY: 7,
		Viewport: ui.Size{Width: 100, Height: 100}, Target: ui.Item(9),
	}
	pointerResult := ui.EventResult{Focus: ui.Item(3), Handled: true}
	keyResult := ui.EventResult{Focus: ui.Item(5), Repaint: true}
	shortcutResult := ui.EventResult{Handled: true, Repaint: true}
	scrollResult := ui.EventResult{Blur: true}
	config := Config{
		Paint: func(*ui.Canvas, ui.Size) {},
		OnPointer: func(event ui.PointerEvent) ui.EventResult {
			if event != pointer {
				t.Fatalf("pointer = %+v, want %+v", event, pointer)
			}
			return pointerResult
		},
		OnKey: func(event ui.KeyEvent) ui.EventResult {
			if event != key {
				t.Fatalf("key = %+v, want %+v", event, key)
			}
			return keyResult
		},
		OnShortcut: func(event ui.KeyEvent) ui.EventResult {
			if event != key {
				t.Fatalf("shortcut = %+v, want %+v", event, key)
			}
			return shortcutResult
		},
		OnScroll: func(event ui.ScrollEvent) ui.EventResult {
			if event != scroll {
				t.Fatalf("scroll = %+v, want %+v", event, scroll)
			}
			return scrollResult
		},
	}
	worker := newReloadWorkerConfig(config, &ui.Canvas{})
	if !worker.Window.HasClick || !worker.Window.HasPointer || !worker.Window.HasKey || !worker.Window.HasShortcut || !worker.Window.HasScroll {
		t.Fatalf("worker capabilities = %+v", worker.Window)
	}
	if got := worker.Pointer(pointer); got != pointerResult {
		t.Fatalf("pointer result = %+v, want %+v", got, pointerResult)
	}
	if got := worker.Key(key); got != keyResult {
		t.Fatalf("key result = %+v, want %+v", got, keyResult)
	}
	if got := worker.Shortcut(key); got != shortcutResult {
		t.Fatalf("shortcut result = %+v, want %+v", got, shortcutResult)
	}
	if got := worker.Scroll(scroll); got != scrollResult {
		t.Fatalf("scroll result = %+v, want %+v", got, scrollResult)
	}
}

// A development host opens the window, so the custom titlebar's height has to
// reach it from the worker's configuration, rounded to a whole pixel.
func TestReloadWorkerCarriesTheTitlebarHeightToTheHost(t *testing.T) {
	worker := newReloadWorkerConfig(Config{Title: "bar", Width: 100, Height: 100, MinWidth: 80, MinHeight: 60, WindowMode: WindowCustomTitlebar, TitlebarHeight: 36}, &ui.Canvas{})
	if worker.Window.MinWidth != 80 || worker.Window.MinHeight != 60 {
		t.Fatalf("minimum lost by reload worker: %+v", worker.Window)
	}
	if worker.Window.TitlebarHeight != 36 || worker.Window.WindowMode != uint8(WindowCustomTitlebar) {
		t.Fatalf("window = %+v", worker.Window)
	}
	if got := newReloadWorkerConfig(Config{TitlebarHeight: 35.6}, &ui.Canvas{}).Window.TitlebarHeight; got != 36 {
		t.Fatalf("35.6 became %d", got)
	}
	if got := newReloadWorkerConfig(Config{}, &ui.Canvas{}).Window.TitlebarHeight; got != 0 {
		t.Fatalf("an unset height became %d", got)
	}
}
