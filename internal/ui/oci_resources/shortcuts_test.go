package ociresources

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// selectedImageName is the row the Images tab opens on, so a test can mark it
// as scanning without assuming an index.
func selectedImageName(t *testing.T, m Model) string {
	t.Helper()
	img := m.getSelectedImage()
	if img == nil {
		t.Fatal("the loaded fixture has no selected image")
	}
	return img.Name()
}

// A scan in flight greys the three row actions. They used to be replaced by a
// spinner and the word "scanning..." — a status line in the column that lists
// keys, where the row's own Scanned cell already carries the spinner
// (Rule 139).
func TestAScanningImageGreysItsRowActionsInsteadOfReplacingThem(t *testing.T) {
	idle := loadedModel(t)
	settled := testutil.ShortcutKeys(idle.GetShortcuts())

	m := loadedModel(t)
	m = scanning(t, m, selectedImageName(t, m))

	keys := testutil.ShortcutKeys(m.GetShortcuts())
	if strings.Join(keys, " ") != strings.Join(settled, " ") {
		t.Errorf("a scanning row advertises %v, want the same keys as an idle one %v", keys, settled)
	}

	for _, key := range []string{keymap.New, keymap.Scan, keymap.Delete} {
		if !testutil.ShortcutDisabled(m.GetShortcuts(), key) {
			t.Errorf("%q is offered while the selected image is being scanned", key)
		}
		if !testutil.ShortcutEnabled(idle.GetShortcuts(), key) {
			t.Errorf("%q is greyed on an idle image", key)
		}
	}

	// P and A act on the whole list rather than on the row, so they stay lit.
	for _, key := range []string{keymap.Prune, keymap.ScanAll} {
		if !testutil.ShortcutEnabled(m.GetShortcuts(), key) {
			t.Errorf("%q is greyed by a scan on one row, which it does not act on", key)
		}
	}
}

// The three refuse with a reason rather than returning in silence (Rule 130).
func TestARowActionOnAScanningImageSaysWhyItDeclined(t *testing.T) {
	for _, key := range []string{keymap.New, keymap.Scan, keymap.Delete} {
		t.Run(key, func(t *testing.T) {
			m := loadedModel(t)
			m = scanning(t, m, selectedImageName(t, m))

			next, _ := step(t, m, testutil.Key(key))

			if got := next.footer.Text(); !strings.Contains(got, reasonImageScanned) {
				t.Errorf("footer = %q, want it to carry %q", got, reasonImageScanned)
			}
			if next.launchForm != nil || next.confirmModal != nil {
				t.Error("the action fired on an image being scanned")
			}
		})
	}
}

// No shortcut may carry something that is not a key. The Images tab advertised
// a spinner frame as one, which the header renders in the same column as
// <ctrl+r> — a status dressed as a binding.
//
// The test is a range rather than a list of glyphs: every Nerd Font icon this
// application uses lives in the Private Use Area, so nothing has to be kept in
// step with theme/icons.go.
func TestNoShortcutAdvertisesAGlyphAsAKey(t *testing.T) {
	m := loadedModel(t)
	m = scanning(t, m, selectedImageName(t, m))

	for _, s := range m.GetShortcuts() {
		for _, r := range s.Key {
			if r >= 0xE000 && r <= 0xF8FF {
				t.Errorf("shortcut %q carries the private-use glyph %U as its key", s.Key, r)
			}
		}
	}
}
