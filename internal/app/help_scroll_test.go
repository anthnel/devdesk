package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D2. bubbles/viewport v2 binds Left/Right (and h/l) to horizontal
// scrolling by default. Rule 135 gives <-/-> to drill-down and value cycling and
// a bare letter is an action, so the help overlay must not scroll sideways.
//
// These two tests are non-regression guards: the overlay already sets a
// KeyMap without Left/Right (arrowOnlyScroll), so they are green on the
// current code. The red of the rule is proven in keymap's
// TestViewportKeyMapDetector, which feeds the guard a violating source.

func TestArrowOnlyScrollBindsNoHorizontalKey(t *testing.T) {
	km := arrowOnlyScroll()
	for name, b := range map[string]key.Binding{"Left": km.Left, "Right": km.Right} {
		if keys := b.Keys(); len(keys) != 0 {
			t.Errorf("%s is bound to %v; the help overlay must not scroll horizontally", name, keys)
		}
	}
}

func TestHelpOverlayIgnoresHorizontalKeys(t *testing.T) {
	view := &fakeView{title: "view"}
	a := router(t, view)
	a.maybeOpenHelp(testutil.Key("?"))
	if !a.showHelp {
		t.Fatal("the help did not open (the fake view must implement help.Provider)")
	}
	// Content both taller and wider than the viewport, so any scroll key that
	// is bound has room to move the offset.
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = strings.Repeat("0123456789", 40)
	}
	a.helpViewport.SetContent(strings.Join(lines, "\n"))

	for _, name := range []string{"left", "right", "h", "l"} {
		a.handleHelpKeyMsg(testutil.Key(name))
		if x, y := a.helpViewport.XOffset(), a.helpViewport.YOffset(); x != 0 || y != 0 {
			t.Fatalf("after %q: XOffset=%d YOffset=%d, want 0 and 0", name, x, y)
		}
	}
	for _, name := range []string{"down", "pgdown"} {
		before := a.helpViewport.YOffset()
		a.handleHelpKeyMsg(testutil.Key(name))
		if a.helpViewport.YOffset() <= before {
			t.Errorf("%q did not scroll the help down (YOffset %d -> %d)", name, before, a.helpViewport.YOffset())
		}
	}
	if x := a.helpViewport.XOffset(); x != 0 {
		t.Errorf("XOffset = %d after vertical scrolling, want 0", x)
	}
}
