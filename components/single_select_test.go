package components

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func singleSelectFixture() *SingleSelectState {
	s := &SingleSelectState{AllowCreate: true, MatchCaseInsensitive: true}
	s.SetOptions([]SingleSelectOption{{Value: "dev", Label: "Development"}, {Value: "test", Label: "Testing"}, {Value: "blocked", Disabled: true}})
	return s
}

func BenchmarkSingleSelectPaint(b *testing.B) {
	var tree ui.Tree
	f := tree.Feature("single-select-benchmark")
	o := SingleSelectOptions{InputID: tree.ID(f, "input"), TriggerID: tree.ID(f, "trigger"), ClearID: tree.ID(f, "clear"), DismissID: tree.ID(f, "dismiss"), Scope: tree.ID(f, "scope"), ScrollID: tree.ID(f, "scroll"), OptionIDs: tree.Sub(f, tree.Name("option")), Label: "Group", CreatePrefix: "Create: ", Viewport: ui.Rect{Width: 800, Height: 600}, Layer: 4}
	for _, name := range []string{"closed", "open", "create", "long"} {
		b.Run(name, func(b *testing.B) {
			s := singleSelectFixture()
			s.SetValue("dev")
			s.Open = name != "closed"
			if name == "create" {
				s.Query.Insert("custom group")
			}
			if name == "long" {
				s.SetValue(strings.Repeat("长分组名称", 20))
			}
			var canvas ui.Canvas
			paint := func() {
				canvas.Reset()
				PaintSingleSelect(&canvas, ui.LightTheme(), ui.Rect{Width: 320, Height: 32}, s, o)
			}
			paint()
			b.ReportAllocs()
			for b.Loop() {
				paint()
			}
			b.ReportMetric(float64(unsafe.Sizeof(*s)), "state-B")
		})
	}
}

func TestSingleSelectReplacesClosesAndRetainsCreatedOptions(t *testing.T) {
	s := singleSelectFixture()
	s.Open = true
	if !s.Choose(0) || s.Open || s.Value() != "dev" {
		t.Fatal("selection did not close", s.Value())
	}
	s.Open = true
	if !s.Choose(1) || s.Value() != "test" || s.Open {
		t.Fatal("selection did not replace", s.Value())
	}
	s.Open = true
	if !s.Choose(1) || s.Value() != "test" {
		t.Fatal("reselect toggled value off")
	}
	s.Query.Insert("上海机房")
	if choices := s.Choices(); len(choices) != 1 || !choices[0].Create {
		t.Fatal("missing create row", choices)
	}
	s.Key(ui.KeyEvent{Key: ui.KeyEnter})
	if s.Value() != "上海机房" || s.Query.String() != "" || s.Open {
		t.Fatal("create failed", s.Value())
	}
	s.Choose(0)
	if choices := s.Choices(); len(choices) != 4 || choices[3].Value != "上海机房" {
		t.Fatal("created option was lost", choices)
	}
	s.Clear()
	if s.Value() != "" || s.Query.String() != "" {
		t.Fatal("clear retained a value")
	}
}

func TestSingleSelectExactDisabledAndPendingQuery(t *testing.T) {
	s := singleSelectFixture()
	s.Query.Insert("development")
	if choices := s.Choices(); len(choices) != 1 || choices[0].Create {
		t.Fatal("label shadowed by create", choices)
	}
	if !s.CommitQuery() || s.Value() != "dev" {
		t.Fatal("label did not resolve to stable value")
	}
	s.Query.Insert("BLOCKED")
	if s.CommitQuery() || s.Value() != "dev" {
		t.Fatal("disabled value bypassed")
	}
	s.Query = text.Editor{}
	s.SetValidateCreate(func(value string) bool { return value != "invalid" })
	s.Query.Insert("invalid")
	if s.CommitQuery() {
		t.Fatal("invalid create accepted")
	}
	s.Query = text.Editor{}
	s.Query.Insert(" 新分组 ")
	if !s.CommitQuery() || s.Value() != "新分组" {
		t.Fatal("pending query not trimmed/created")
	}
}

func TestSingleSelectKeyboardAndComposition(t *testing.T) {
	s := singleSelectFixture()
	s.SetValue("dev")
	s.Query.SetMarkedText("机房", text.Range{End: 2})
	s.Key(ui.KeyEvent{Key: ui.KeyEnter})
	if s.Value() != "dev" {
		t.Fatal("IME confirmation selected")
	}
	s.Query = text.Editor{}
	s.Query.Insert("custom")
	s.Key(ui.KeyEvent{Key: ui.KeyEnter, Repeat: true})
	if s.Value() != "dev" {
		t.Fatal("repeated Enter created")
	}
	s.Key(ui.KeyEvent{Key: ui.KeyEscape})
	if s.Value() != "dev" || s.Query.String() != "" || s.Open {
		t.Fatal("Escape lost committed value")
	}
	s.Key(ui.KeyEvent{Key: ui.KeyDown})
	s.Key(ui.KeyEvent{Key: ui.KeyDown})
	s.Key(ui.KeyEvent{Key: ui.KeyEnter})
	if s.Value() != "test" || s.Open {
		t.Fatal("keyboard selection failed", s.Value())
	}
	s.Open = true
	if result := s.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierShift}); !result.ContinueTraversal || s.Open {
		t.Fatal("Tab trapped focus")
	}
}

func TestSingleSelectChoiceCacheDoesNotAllocate(t *testing.T) {
	s := singleSelectFixture()
	s.Query.Insert("De")
	s.Choices()
	if count := testing.AllocsPerRun(100, func() { s.Choices() }); count != 0 {
		t.Fatalf("stable choices allocate %g times", count)
	}
}

func TestSingleSelectKeepsItsNativeInputAndPopupInBounds(t *testing.T) {
	var tree ui.Tree
	f := tree.Feature("single-select-layout")
	o := SingleSelectOptions{InputID: tree.ID(f, "input"), TriggerID: tree.ID(f, "trigger"), ClearID: tree.ID(f, "clear"), DismissID: tree.ID(f, "dismiss"), Scope: tree.ID(f, "scope"), OptionIDs: tree.Sub(f, tree.Name("option")), Label: "Group", Viewport: ui.Rect{Width: 800, Height: 600}, Layer: 4}
	for _, theme := range []ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		for _, width := range []float32{180, 320, 600} {
			s := singleSelectFixture()
			s.SetValue("dev")
			s.Open = true
			var canvas ui.Canvas
			layout := PaintSingleSelect(&canvas, theme, ui.Rect{X: 20, Y: 540, Width: width}, s, o)
			if layout.Input.Width <= 0 || layout.Input.X < layout.Bounds.X || layout.Input.X+layout.Input.Width > layout.Bounds.X+layout.Bounds.Width {
				t.Fatal("native query escaped the control", layout)
			}
			if layout.Popup.Y < 0 || layout.Popup.Y+layout.Popup.Height > 540 {
				t.Fatal("popup did not fit above the field", layout)
			}
			if canvas.TrappedFocusScope() != o.Scope || canvas.InitialFocus() != o.InputID {
				t.Fatal("popup lost its native input focus scope")
			}
			inputs := 0
			for _, hit := range canvas.Hits() {
				if hit.ID == o.InputID {
					inputs++
					if hit.Role != ui.RoleCombobox || hit.TextBounds != layout.Input || hit.Value != "Development" || !hit.Expanded {
						t.Fatal("native input geometry or selected-value semantics were lost", hit)
					}
				}
			}
			if inputs != 1 {
				t.Fatal("duplicate native inputs", inputs)
			}
		}
	}
}

func TestSingleSelectDismissDropsTheQueryAndKeepsTheValue(t *testing.T) {
	s := singleSelectFixture()
	s.SetValue("dev")
	s.Open = true
	s.Query.Insert("cust")
	s.Key(ui.KeyEvent{Key: ui.KeyTab})
	if s.Open || s.Query.String() != "" || s.Value() != "dev" {
		t.Fatalf("Tab left open=%v query=%q value=%q", s.Open, s.Query.String(), s.Value())
	}
	s.Open = true
	s.Query.Insert("cust")
	s.Dismiss()
	if s.Open || s.Query.String() != "" || s.Value() != "dev" {
		t.Fatalf("Dismiss left open=%v query=%q value=%q", s.Open, s.Query.String(), s.Value())
	}
}

func TestSingleSelectPopupStaysInsideAShortViewport(t *testing.T) {
	var tree ui.Tree
	f := tree.Feature("single-select-short-viewport")
	o := SingleSelectOptions{InputID: tree.ID(f, "input"), TriggerID: tree.ID(f, "trigger"), ClearID: tree.ID(f, "clear"), DismissID: tree.ID(f, "dismiss"), Scope: tree.ID(f, "scope"), OptionIDs: tree.Sub(f, tree.Name("option")), Label: "Group", Layer: 4}
	for _, test := range []struct {
		name     string
		viewport ui.Rect
		field    ui.Rect
	}{
		{"below", ui.Rect{Width: 800, Height: 100}, ui.Rect{X: 20, Y: 40, Width: 240}},
		{"near the bottom", ui.Rect{Width: 800, Height: 90}, ui.Rect{X: 20, Y: 20, Width: 240}},
		{"tiny", ui.Rect{Width: 800, Height: 24}, ui.Rect{X: 20, Y: 2, Width: 240}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := singleSelectFixture()
			s.SetValue("dev")
			s.Open = true
			var canvas ui.Canvas
			o.Viewport = test.viewport
			layout := PaintSingleSelect(&canvas, ui.DarkTheme(), test.field, s, o)
			if layout.Popup.Y < test.viewport.Y || layout.Popup.Height < 1 ||
				layout.Popup.Y+layout.Popup.Height > test.viewport.Y+test.viewport.Height {
				t.Fatalf("popup %+v escaped viewport %+v", layout.Popup, test.viewport)
			}
		})
	}
}
