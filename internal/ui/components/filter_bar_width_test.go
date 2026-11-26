package components

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-789 (step E4 of DEVDESK-785): FilterBar.Resize remembers the width but
// never hands it to its textinput, so a long query overflows the frame the bar
// draws (BarFrame does not cut), and a confirmed query is never cut either. The
// field must scroll inside the room the frame leaves it, and a confirmed query
// must be cut from its end with an ellipsis.
//
// Layout of the content line, for reference (width cells in all):
//
//	│ + " " + "/" + " " + field + cursor cell + tokens + padding + │

// barTextRunes is how many runes a "long query" holds. The bar's CharLimit is
// 100, so typing this many leaves a 100-rune value: still wider than the field
// at every width these tests use.
const barTextRunes = 120

// barQuery returns n distinct runes (Latin Extended-A, one cell each), so the
// glyph under the cursor, or the last glyph on screen, tells where the window is.
func barQuery(n int) string {
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = rune(0x100 + i)
	}
	return string(runes)
}

// barFrameLine fails unless the two lines of view are exactly width cells wide
// and returns the content line.
func barFrameLine(t *testing.T, view string, width int) string {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != 2 {
		t.Fatalf("width %d: View() produced %d lines, want 2", width, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != width {
			t.Errorf("width %d: line %d is %d cells wide, want exactly %d: %q",
				width, i, got, width, ansi.Strip(line))
		}
	}
	return lines[0]
}

// barCursorOn fails unless the content line draws exactly one virtual cursor
// cell, strictly inside the side borders, on the glyph want.
func barCursorOn(t *testing.T, line string, width int, want string) {
	t.Helper()
	cols, glyphs := reverseCells(line)
	if len(cols) != 1 {
		t.Errorf("width %d: %d cursor cells in the bar, want 1: %q", width, len(cols), ansi.Strip(line))
		return
	}
	if cols[0] < 1 || cols[0] > width-2 {
		t.Errorf("width %d: the cursor is in column %d, outside the frame (columns 1 to %d): %q",
			width, cols[0], width-2, ansi.Strip(line))
	}
	if glyphs[0] != want {
		t.Errorf("width %d: the cursor sits on %q, want %q: %q", width, glyphs[0], want, ansi.Strip(line))
	}
}

// barVisibleQueryRunes counts the runes of barQuery drawn on the line.
func barVisibleQueryRunes(line string) int {
	n := 0
	for _, r := range ansi.Strip(line) {
		if r >= 0x100 && r < 0x200 {
			n++
		}
	}
	return n
}

// searchingBar builds a bar already resized to width and in search mode, with
// the tokens in active switched on.
func searchingBar(width int, active ...string) FilterBar {
	fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}, {Label: "udp"}, {Label: "LISTEN"}})
	for _, label := range active {
		fb.SetTokenActive(label, true)
	}
	fb.Resize(width)
	fb.ActivateSearch()
	return fb
}

func TestFilterBarFieldScrollsInsteadOfOverflowing(t *testing.T) {
	tests := []struct {
		name   string
		widths []int // the first is applied before typing, the others after
		lefts  int   // how many times "left" is pressed once the query is typed
	}{
		{"typed at 80", []int{80}, 0},
		{"typed at 40", []int{40}, 0},
		{"typed at 20", []int{20}, 0},
		{"shrunk from 120 to 80", []int{120, 80}, 0},
		{"shrunk from 120 through 80 and 40 to 20", []int{120, 80, 40, 20}, 0},
		{"shrunk after typing, cursor right of the middle", []int{120, 40}, 10},
		{"shrunk after typing, cursor in the middle", []int{120, 40}, 50},
		{"cursor in the middle at 20", []int{20}, 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := searchingBar(tc.widths[0])
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			for range tc.lefts {
				feedFilterBar(&fb, testutil.Key("left"))
			}

			query := []rune(fb.SearchQuery())
			if len(query) == 0 {
				t.Fatal("setup: the typed query is empty")
			}
			want := " "
			if tc.lefts > 0 {
				want = string(query[len(query)-tc.lefts])
			}
			check := func(width int) {
				line := barFrameLine(t, fb.View(), width)
				barCursorOn(t, line, width, want)
				if tc.lefts == 0 && !strings.Contains(ansi.Strip(line), string(query[len(query)-1])) {
					t.Errorf("width %d: the end of the query is not on screen: %q", width, ansi.Strip(line))
				}
			}
			check(tc.widths[0])
			for _, width := range tc.widths[1:] {
				fb.Resize(width)
				check(width)
			}
		})
	}
}

func TestFilterBarFieldShrinksWithActiveTokens(t *testing.T) {
	// With "tcp" and "LISTEN" active the bar spends 16 cells on its tokens
	// ("  tcp  ·  LISTEN"), and those cells are not the field's.
	active := []string{"tcp", "LISTEN"}
	tests := []struct {
		name  string
		width int
		// build returns a bar in search mode holding the long query, with the
		// tokens switched on in the way under test.
		build func(width int) FilterBar
		// widens is true when the case ends without an active token.
		widens bool
	}{
		{"tokens active before the search starts, width 60", 60, func(w int) FilterBar {
			fb := searchingBar(w, active...)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			return fb
		}, false},
		{"tokens active before the search starts, width 40", 40, func(w int) FilterBar {
			fb := searchingBar(w, active...)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			return fb
		}, false},
		{"tokens switched on after typing, width 60", 60, func(w int) FilterBar {
			fb := searchingBar(w)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			for _, label := range active {
				fb.SetTokenActive(label, true)
			}
			return fb
		}, false},
		{"tokens switched on after typing, width 40", 40, func(w int) FilterBar {
			fb := searchingBar(w)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			for _, label := range active {
				fb.SetTokenActive(label, true)
			}
			return fb
		}, false},
		{"tokens replaced by SetTokens, active ones kept", 40, func(w int) FilterBar {
			fb := searchingBar(w)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			fb.SetTokens([]FilterToken{{Label: "tcp", Active: true}, {Label: "LISTEN", Active: true}})
			return fb
		}, false},
		{"the active tokens are dropped by SetTokens", 40, func(w int) FilterBar {
			fb := searchingBar(w, active...)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			fb.SetTokens([]FilterToken{{Label: "udp"}})
			return fb
		}, true},
		{"tokens switched off after typing", 40, func(w int) FilterBar {
			fb := searchingBar(w, active...)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			for _, label := range active {
				fb.SetTokenActive(label, false)
			}
			return fb
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// What the bar shows of the query when no token takes room.
			bare := searchingBar(tc.width)
			feedFilterBar(&bare, testutil.Type(barQuery(barTextRunes))...)
			bareLine := barFrameLine(t, bare.View(), tc.width)
			room := barVisibleQueryRunes(bareLine)
			if room == 0 {
				t.Fatal("setup: the bar without tokens shows no query at all")
			}

			fb := tc.build(tc.width)
			line := barFrameLine(t, fb.View(), tc.width)
			barCursorOn(t, line, tc.width, " ")

			got := barVisibleQueryRunes(line)
			switch {
			case tc.widens && got != room:
				t.Errorf("without an active token the field shows %d runes of the query, want the %d of a bar that never had one",
					got, room)
			case !tc.widens && got >= room:
				t.Errorf("with active tokens the field shows %d runes of the query, want fewer than the %d it shows without", got, room)
			}
		})
	}
}

func TestFilterBarConfirmedQueryIsCutToTheBar(t *testing.T) {
	cjk := strings.Repeat("日本語の検索", 10) // 60 runes, 120 cells
	tests := []struct {
		name   string
		query  string
		widths []int // the first is applied before the query is confirmed, the others after; the last one is checked
		active []string
	}{
		{"80 cells", barQuery(barTextRunes), []int{80}, nil},
		{"40 cells", barQuery(barTextRunes), []int{40}, nil},
		{"20 cells", barQuery(barTextRunes), []int{20}, nil},
		{"with active tokens", barQuery(barTextRunes), []int{60}, []string{"tcp", "LISTEN"}},
		{"wide characters", cjk, []int{40}, nil},
		{"wide characters in an odd room", cjk, []int{41}, nil},
		{"shrunk after confirming", barQuery(barTextRunes), []int{120, 40}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := searchingBar(tc.widths[0], tc.active...)
			feedFilterBar(&fb, testutil.Type(tc.query)...)
			fb.Update(testutil.Key("enter"))

			full := fb.SearchQuery()
			if fb.InEditMode() || full == "" {
				t.Fatalf("setup: editing=%v query=%q after enter", fb.InEditMode(), full)
			}
			check := func(width int) {
				line := barFrameLine(t, fb.View(), width)
				inner := strings.TrimSuffix(strings.TrimPrefix(ansi.Strip(line), "│"), "│")
				rest, ok := strings.CutPrefix(inner, " / ")
				if !ok {
					t.Fatalf("width %d: the bar does not open with \" / \": %q", width, inner)
				}
				before, _, cut := strings.Cut(rest, "…")
				if !cut {
					t.Fatalf("width %d: a %d-rune query is not cut with an ellipsis: %q", width, utf8.RuneCountInString(full), inner)
				}
				if before == "" || !strings.HasPrefix(full, before) {
					t.Errorf("width %d: the text before the ellipsis, %q, is not the start of the query", width, before)
				}
				if fb.SearchQuery() != full {
					t.Errorf("width %d: the bar rewrote the query to %q", width, fb.SearchQuery())
				}
			}
			for _, width := range tc.widths[1:] {
				fb.Resize(width)
			}
			check(tc.widths[len(tc.widths)-1])
		})
	}

	t.Run("widening shows the whole query again", func(t *testing.T) {
		fb := searchingBar(40)
		feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
		fb.Update(testutil.Key("enter"))
		fb.Resize(120)

		line := barFrameLine(t, fb.View(), 120)
		if strings.Contains(ansi.Strip(line), "…") {
			t.Errorf("a query that fits is cut: %q", ansi.Strip(line))
		}
		if got, want := barVisibleQueryRunes(line), utf8.RuneCountInString(fb.SearchQuery()); got != want {
			t.Errorf("%d runes of the query on screen, want all %d", got, want)
		}
	})
}

// oldBarView draws the bar the way it was drawn before the field had a width,
// from the same parts: it is what a short query must keep looking like.
func oldBarView(width int, mode, query string, active []string) string {
	var right string
	if len(active) > 0 {
		parts := make([]string, len(active))
		for i, label := range active {
			parts[i] = theme.KeyStyle.Render(label)
		}
		right = "  " + strings.Join(parts, theme.DimStyle.Render("  ·  "))
	}
	var left string
	switch mode {
	case "editing":
		in := textinput.New()
		theme.StyleTextInput(&in)
		in.CharLimit = 100
		in.SetValue(query)
		in.Focus()
		left = theme.KeyStyle.Render("/") + theme.Bg(" ") + in.View()
	case "confirmed":
		left = theme.DimStyle.Render("/ ") + theme.KeyStyle.Render(query)
	default:
		left = theme.DimStyle.Render("/ search...")
	}
	return BarFrame(width, theme.Bg(" ")+left+theme.Bg(right))
}

// blankFG forgets the foreground of blank cells: nothing is drawn there, so two
// frames that differ only by it look the same.
func blankFG(rows [][]testutil.Cell) [][]testutil.Cell {
	for _, row := range rows {
		for i := range row {
			if row[i].Glyph == " " {
				row[i].FG = testutil.DefaultColor
			}
		}
	}
	return rows
}

// A query that fits keeps the cells it had before the field got a width. This
// passes today and guards the change. The editing cases with active tokens hold
// the tokens in place while typing: the field draws no trailing padding.
func TestFilterBarShortQueryLooksAsBefore(t *testing.T) {
	tests := []struct {
		name   string
		mode   string // editing, confirmed or placeholder
		query  string
		active []string
	}{
		{"confirmed query", "confirmed", "web", nil},
		{"confirmed query with active tokens", "confirmed", "web", []string{"tcp", "LISTEN"}},
		{"placeholder with an active token", "placeholder", "", []string{"tcp"}},
		{"typing a short query", "editing", "web", nil},
		{"typing nothing yet", "editing", "", nil},
		{"typing a short query with active tokens", "editing", "web", []string{"tcp", "LISTEN"}},
		{"typing nothing yet with an active token", "editing", "", []string{"tcp"}},
	}
	for _, width := range []int{40, 80} {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%s, width %d", tc.name, width), func(t *testing.T) {
				fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}, {Label: "udp"}, {Label: "LISTEN"}})
				for _, label := range tc.active {
					fb.SetTokenActive(label, true)
				}
				fb.Resize(width)
				if tc.mode != "placeholder" {
					fb.ActivateSearch()
					feedFilterBar(&fb, testutil.Type(tc.query)...)
				}
				if tc.mode == "confirmed" {
					fb.Update(testutil.Key("enter"))
				}

				got := blankFG(testutil.Cells(fb.View()))
				want := blankFG(testutil.Cells(oldBarView(width, tc.mode, tc.query, tc.active)))
				if !reflect.DeepEqual(got, want) {
					t.Errorf("width %d: the bar does not look as before.\n got:\n%s\nwant:\n%s",
						width, testutil.FormatCells(got), testutil.FormatCells(want))
				}
			})
		}
	}
}

func TestFilterBarResizeKeepsTheQuery(t *testing.T) {
	// Widths 1 to 3 are narrower than the frame itself: nothing is drawn there,
	// the bar only has to survive them (the field width has a floor of 1).
	widths := []int{80, 40, 1, 3, 20, 120, 60, 2, 30}
	tests := []struct {
		name   string
		active []string
		lefts  int // how many times "left" is pressed before the first resize
	}{
		{"cursor at the end", nil, 0},
		{"cursor three runes from the end", nil, 3},
		{"cursor in the middle", nil, 30},
		{"with active tokens", []string{"tcp", "LISTEN"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name+", while typing", func(t *testing.T) {
			// 60 runes, so the bar's CharLimit of 100 leaves room for the extra key.
			fb := searchingBar(120, tc.active...)
			feedFilterBar(&fb, testutil.Type(barQuery(60))...)
			typed := []rune(fb.SearchQuery())
			for range tc.lefts {
				feedFilterBar(&fb, testutil.Key("left"))
			}

			for _, width := range widths {
				fb.Resize(width)
				if got := fb.SearchQuery(); got != string(typed) {
					t.Fatalf("after Resize(%d) the query is %q, want %q", width, got, string(typed))
				}
				if !fb.InEditMode() {
					t.Fatalf("after Resize(%d) the bar left edit mode", width)
				}
			}

			// One more key lands where the cursor was left, so neither the value
			// nor the cursor position moved with the width.
			feedFilterBar(&fb, testutil.Type("Z")...)
			at := len(typed) - tc.lefts
			want := string(typed[:at]) + "Z" + string(typed[at:])
			if got := fb.SearchQuery(); got != want {
				t.Errorf("typing after the resizes gives %q, want %q", got, want)
			}
			fb.Update(testutil.Key("enter"))
			if got := fb.SearchQuery(); got != want {
				t.Errorf("confirmed query %q, want %q", got, want)
			}
		})

		t.Run(tc.name+", once confirmed", func(t *testing.T) {
			fb := searchingBar(120, tc.active...)
			feedFilterBar(&fb, testutil.Type(barQuery(barTextRunes))...)
			fb.Update(testutil.Key("enter"))
			want := fb.SearchQuery()

			for _, width := range widths {
				fb.Resize(width)
				if got := fb.SearchQuery(); got != want {
					t.Fatalf("after Resize(%d) the query is %q, want %q", width, got, want)
				}
			}
			if !fb.IsVisible() || fb.InEditMode() {
				t.Errorf("visible=%v editing=%v after the resizes, want a visible bar out of edit mode",
					fb.IsVisible(), fb.InEditMode())
			}
		})
	}
}
