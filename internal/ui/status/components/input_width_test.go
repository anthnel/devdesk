package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/anthnel/devdesk/internal/config"
)

// DEVDESK-201 (Bubbles v2): textinput.Model.Width became SetWidth(n) / Width().
// Each field of the monitor form keeps the width it was built with across a
// resize; a width lost to 0 would show one rune of the placeholder.
func TestTextFieldsKeepTheirWidthAfterResize(t *testing.T) {
	f := NewComponentForm(&config.ComponentConfig{Name: "api", Type: "http", Target: "https://x"})

	widths := func(f *ComponentForm) map[string]int {
		return map[string]int{
			"name":    f.nameInput.Width(),
			"target":  f.targetInput.Width(),
			"timeout": f.timeoutInput.Width(),
			"count":   f.countInput.Width(),
			"ns":      f.nsInput.Width(),
		}
	}
	want := map[string]int{"name": 40, "target": 40, "timeout": 10, "count": 10, "ns": 40}

	for name, got := range widths(f) {
		if got != want[name] {
			t.Fatalf("a new form's %s field is %d wide, want %d", name, got, want[name])
		}
	}

	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 200, Height: 50}, {Width: 30, Height: 8}} {
		f, _ = f.Update(size)
		for name, got := range widths(f) {
			if got != want[name] {
				t.Errorf("after a %dx%d resize the %s field is %d wide, want %d",
					size.Width, size.Height, name, got, want[name])
			}
		}
	}
}
