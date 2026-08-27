package viewer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// paneLines is the pane as the reader sees it: escapes stripped, trailing
// padding gone, and the blank rows below the document dropped.
func paneLines(m Model) []string {
	var out []string
	for _, line := range strings.Split(m.textViewport.View(), "\n") {
		line = strings.TrimRight(ansi.Strip(line), " ")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func TestLineNumbersAppearAndGoAway(t *testing.T) {
	m := logModel(t, "alpha\nbeta\ngamma")

	if got := paneLines(m)[0]; got != "alpha" {
		t.Errorf("the pane opens on %q, want %q — the gutter is off by default", got, "alpha")
	}

	m = feed(t, m, testutil.Key("n"))
	if got := paneLines(m)[0]; got != "1 alpha" {
		t.Errorf("first line = %q after n, want %q", got, "1 alpha")
	}

	m = feed(t, m, testutil.Key("n"))
	if got := paneLines(m)[0]; got != "alpha" {
		t.Errorf("first line = %q after a second n, want the gutter gone", got)
	}
}

// The gutter is as wide as the widest number and no wider, and the numbers are
// right-aligned inside it — otherwise the text starts at a different column
// depending on the line, which is the one thing a gutter must not do.
func TestTheGutterIsSizedForTheLastLine(t *testing.T) {
	m := feed(t, logModel(t, strings.Repeat("x\n", 120)), testutil.Key("n"))

	lines := paneLines(m)
	if got, want := lines[0], "  1 x"; got != want {
		t.Errorf("line 1 renders %q, want %q — 120 lines need three columns", got, want)
	}

	column := strings.Index(lines[0], "x")
	for i, line := range lines {
		if strings.Index(line, "x") != column {
			t.Fatalf("row %d starts its text at column %d, want %d: %q", i, strings.Index(line, "x"), column, line)
		}
	}
}

// The number is the document's, not the row's. Under a search the surviving
// lines keep the numbers they had — that is what makes them worth showing.
func TestTheNumbersAreTheDocumentsUnderASearch(t *testing.T) {
	m := feed(t, logModel(t, "alpha\nbeta\ngamma\nbetter"), testutil.Key("n"))
	m = searchFor(t, m, "bet")

	want := []string{"2 beta", "4 better"}
	got := paneLines(m)
	if len(got) != len(want) {
		t.Fatalf("the search left %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q — a filtered pane must not renumber", i, got[i], want[i])
		}
	}
}

// A wrapped line numbers its first row and leaves the rest blank: a number says
// where a source line begins, and repeating it would claim the document holds
// several lines bearing the same one.
func TestAWrappedLineNumbersItsFirstRowOnly(t *testing.T) {
	m := logModel(t, strings.Repeat("x", 120)+"\nshort")
	m = feed(t, m, tea.WindowSizeMsg{Width: 40, Height: 20}, testutil.Key("n"), testutil.Key("w"))

	lines := paneLines(m)
	if len(lines) < 3 {
		t.Fatalf("the long line did not wrap: %v", lines)
	}
	if !strings.HasPrefix(lines[0], "1 ") {
		t.Errorf("first row = %q, want it numbered", lines[0])
	}
	if strings.HasPrefix(strings.TrimLeft(lines[1], " "), "1 ") {
		t.Errorf("a continuation row repeats its number: %q", lines[1])
	}
}

// The gutter comes off the width before the wrap, not after it. Prefixing a
// segment wrapped at the full width would push every row past the right margin
// by exactly the gutter — on every line, so it would look like a border problem.
func TestTheGutterDoesNotPushWrappedLinesPastTheMargin(t *testing.T) {
	m := logModel(t, strings.Repeat("x", 300))
	m = feed(t, m, tea.WindowSizeMsg{Width: 40, Height: 20}, testutil.Key("w"), testutil.Key("n"))

	for _, line := range strings.Split(m.textViewport.View(), "\n") {
		if width := len([]rune(ansi.Strip(line))); width > 40 {
			t.Fatalf("a line is %d cells wide with the gutter on, want 40 at most: %q", width, line)
		}
	}
}

// The gutter is not part of the text, so it is not part of what is searched.
// This holds by construction — the numbers never enter docLine.Plain — and the
// test is here because the cheap implementation (prefix, then filter) would pass
// every other test in this file.
func TestTheGutterIsNotSearchable(t *testing.T) {
	m := feed(t, logModel(t, "alpha\nbeta\ngamma"), testutil.Key("n"))
	m = searchFor(t, m, "2")

	if m.matchedLines != 0 {
		t.Errorf("searching for a gutter number kept %d lines, want none", m.matchedLines)
	}
}
