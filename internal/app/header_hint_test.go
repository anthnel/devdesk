package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// paletteHint is the permanent header hint that advertises the command palette
// (DEVDESK-109, criterion 7).
const paletteHint = "ctrl+p Commands"

var hintSizes = []struct{ w, h int }{{80, 24}, {120, 40}, {60, 30}, {40, 16}}

// The hint is on the header's last row, flush right, on every view and at all
// four reference sizes, and it costs the header neither a row nor a cell.
func TestTheHeaderAlwaysShowsThePaletteHint(t *testing.T) {
	views := map[string]*fakeView{
		"no shortcuts":   {},
		"few shortcuts":  {shortcuts: manyShortcuts(3)},
		"many shortcuts": {shortcuts: manyShortcuts(12)},
	}
	for name, v := range views {
		for _, sz := range hintSizes {
			a := routerAt(t, v, sz.w, sz.h)
			lines := strings.Split(a.renderHeader(), "\n")
			header := lines[:headerMinHeight]

			last := strings.TrimRight(ansi.Strip(header[headerMinHeight-1]), " ")
			if !strings.HasSuffix(last, paletteHint) {
				t.Errorf("%s %dx%d: last header row %q does not end with %q", name, sz.w, sz.h, last, paletteHint)
			}
			for i, line := range header[:headerMinHeight-1] {
				if strings.Contains(ansi.Strip(line), paletteHint) {
					t.Errorf("%s %dx%d: the hint also appears on row %d", name, sz.w, sz.h, i)
				}
			}
			for i, line := range header {
				if got := lipgloss.Width(line); got != sz.w {
					t.Errorf("%s %dx%d: row %d is %d cells wide, want %d", name, sz.w, sz.h, i, got, sz.w)
				}
			}
		}
	}
}

// The hint is drawn in ShortcutHintStyle (readable), not in the dim colour, and
// clipping the shortcut column around it never leaves a torn escape sequence.
func TestThePaletteHintIsStyledAndNeverTearsAnEscape(t *testing.T) {
	testutil.TrueColor(t)
	want := theme.ShortcutHintStyle.Render(paletteHint)
	info := []shortcut.HeaderInfo{{Key: "Context", Value: "default"}}

	for _, w := range []int{40, 60, 80, 120} {
		lines := strings.Split(renderHeaderContent(info, manyShortcuts(12), w), "\n")
		if !strings.Contains(lines[headerMinHeight-1], want) {
			t.Errorf("width %d: last row lacks the hint rendered in ShortcutHintStyle: %q", w, lines[headerMinHeight-1])
		}
		for i, line := range lines {
			if truncated(line) {
				t.Errorf("width %d: row %d ends inside an escape sequence: %q", w, i, line)
			}
		}
	}
}

// Where the window is wide, the hint moves nothing: the logo stays, rows 0-5
// are untouched, and the shortcut column on the last row is only clipped when
// it would collide with the hint.
func TestThePaletteHintMovesNothingAtNormalWidths(t *testing.T) {
	info := []shortcut.HeaderInfo{{Key: "Context", Value: "default"}}
	for _, w := range []int{80, 120} {
		lines := strings.Split(ansi.Strip(renderHeaderContent(info, manyShortcuts(3), w)), "\n")
		if !strings.Contains(strings.Join(lines, "\n"), logoMarker()) {
			t.Errorf("width %d: the logo disappeared", w)
		}
		if !strings.Contains(lines[0], "Context") {
			t.Errorf("width %d: the info column moved: %q", w, lines[0])
		}
		if !strings.Contains(lines[0], "<a>") {
			t.Errorf("width %d: the shortcut column moved: %q", w, lines[0])
		}
	}
}

// Without a logo (narrow window) the hint takes the end of the last row and the
// shortcut column gives way to it, not the other way round.
func TestThePaletteHintClipsTheShortcutColumnWithoutALogo(t *testing.T) {
	info := []shortcut.HeaderInfo{{Key: "Context", Value: "default"}}
	rows := strings.Split(ansi.Strip(renderHeaderContent(info, manyShortcuts(30), 40)), "\n")

	if strings.Contains(strings.Join(rows, "\n"), logoMarker()) {
		t.Fatal("no logo expected at width 40")
	}
	if got := strings.TrimRight(rows[headerMinHeight-1], " "); !strings.HasSuffix(got, paletteHint) {
		t.Errorf("last row %q does not end with the hint", got)
	}
}
