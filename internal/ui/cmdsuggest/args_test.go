package cmdsuggest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// argRow builds an argument row; current adds the "(current)" tag.
func argRow(value, note string, current bool) Row {
	return Row{Arg: &command.ArgSuggestion{Value: value, Note: note}, Current: current}
}

func ctxRows() []Row {
	return []Row{
		argRow("web", "", false),
		argRow("work", "", true),
		argRow("-", "Previous context", false),
		argRow("new", "Create a context", false),
	}
}

func manyCtx(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = argRow(fmt.Sprintf("ctx%02d", i), "", false)
	}
	return rows
}

func argInput(width int) Input {
	rows := ctxRows()
	return Input{Rows: rows, Total: 5, Heading: "Contexts · 4 of 5", Cursor: -1, Width: width, Visible: len(rows)}
}

func hasIcon(l string) bool { return strings.Contains(l, theme.IconCircleSmall) }

func TestArgFiletShowsHeadingAndRowsShowValues(t *testing.T) {
	lines := plain(render(t, argInput(80)))
	last := lines[len(lines)-1]
	if !strings.Contains(last, "Contexts · 4 of 5") {
		t.Errorf("filet lacks the heading: %q", last)
	}
	if strings.Contains(last, "available") || strings.Contains(last, "of 17") {
		t.Errorf("filet still shows the command count: %q", last)
	}
	for i, want := range []string{"web", "work", "-", "new"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("row %d = %q, want it to contain %q", i, lines[i], want)
		}
	}
}

func TestArgRowShowsCurrentAsTextAfterTheName(t *testing.T) {
	lines := plain(render(t, argInput(80)))
	if !strings.Contains(lines[1], "work (current)") {
		t.Errorf("row = %q, want %q", lines[1], "work (current)")
	}
	for i, l := range lines {
		if i != 1 && strings.Contains(l, "(current)") {
			t.Errorf("line %d carries (current): %q", i, l)
		}
	}
}

func TestArgCurrentSurvivesANarrowTerminal(t *testing.T) {
	for _, w := range []int{40, 34, 30} {
		in := argInput(w)
		if l := plain(render(t, in))[1]; !strings.Contains(l, "work (current)") {
			t.Errorf("w=%d: current lost (A2): %q", w, l)
		}
	}
}

func TestArgReservedWordsCarryTheirNotes(t *testing.T) {
	lines := plain(render(t, argInput(80)))
	if !strings.Contains(lines[2], "Previous context") || !strings.Contains(lines[3], "Create a context") {
		t.Errorf("notes missing: %q / %q", lines[2], lines[3])
	}
	if strings.Contains(lines[0], "Previous context") {
		t.Errorf("note leaked onto another row: %q", lines[0])
	}
}

func TestArgFocusIsVisibleOnTheHighlightedRowOnly(t *testing.T) {
	testutil.TrueColor(t)
	in := argInput(80)
	in.Cursor = 1
	raw := render(t, in)
	lines := plain(raw)
	for i, l := range lines[:in.Visible] {
		if got, want := hasIcon(l), i == 1; got != want {
			t.Errorf("row %d focus icon = %v, want %v: %q", i, got, want, l)
		}
	}
	sel := bgSequence(t, theme.ColorTableLineSelected)
	if !strings.Contains(raw[1], sel) {
		t.Errorf("highlighted arg row lacks the selection background: %q", raw[1])
	}
	for i, l := range raw {
		if i != 1 && strings.Contains(l, sel) {
			t.Errorf("line %d carries the selection background", i)
		}
	}
	if !strings.Contains(lines[1], "work (current)") {
		t.Errorf("highlight lost the current tag: %q", lines[1])
	}
}

func TestArgNoFocusWhenNothingIsHighlighted(t *testing.T) {
	for _, l := range plain(render(t, argInput(80))) {
		if hasIcon(l) {
			t.Errorf("focus icon with Cursor -1: %q", l)
		}
	}
}

func TestArgLoadingRow(t *testing.T) {
	in := Input{Loading: true, Empty: "should not show", Heading: "Contexts", Cursor: -1, Width: 80, Visible: 1}
	lines := plain(render(t, in))
	if !strings.Contains(lines[0], "Loading contexts…") {
		t.Errorf("row = %q", lines[0])
	}
	if strings.Contains(lines[0], "should not show") || hasIcon(lines[0]) {
		t.Errorf("loading row wrong: %q", lines[0])
	}
}

func TestArgEmptyMessage(t *testing.T) {
	msg := `No context starts with "wrok"`
	in := Input{Empty: msg, Total: 5, Heading: "Contexts · 0 of 5", Cursor: -1, Width: 80, Visible: 1}
	lines := plain(render(t, in))
	if !strings.Contains(lines[0], msg) {
		t.Errorf("row = %q, want %q", lines[0], msg)
	}
	if hasIcon(lines[0]) || strings.Contains(lines[0], "No match for") {
		t.Errorf("empty row wrong: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Contexts · 0 of 5") {
		t.Errorf("filet = %q", lines[1])
	}
}

func TestArgWindowScrollsWithinEightRows(t *testing.T) {
	rows := manyCtx(12)
	in := Input{Rows: rows, Total: 12, Heading: "Contexts · 12 of 12", Cursor: 9, Width: 80, Visible: MaxVisibleRows}
	if in.Visible != 8 {
		t.Fatalf("Visible = %d, want 8", in.Visible)
	}
	in.Offset = Window(in.Cursor, 0, in.Visible)
	lines := plain(render(t, in))
	if !strings.Contains(lines[0], "ctx02") || !strings.Contains(lines[7], "ctx09") {
		t.Errorf("window = %q ... %q, want ctx02..ctx09", lines[0], lines[7])
	}
	if strings.Contains(strings.Join(lines[:8], "\n"), "ctx01") || strings.Contains(strings.Join(lines[:8], "\n"), "ctx10") {
		t.Errorf("rows outside the window are drawn: %q", lines)
	}
	if !hasIcon(lines[7]) {
		t.Errorf("focus not on the last visible row: %q", lines[7])
	}
	last := lines[8]
	if !strings.Contains(last, "Contexts · 12 of 12") || !strings.Contains(last, "▲ 2 more") || !strings.Contains(last, "▼ 2 more") {
		t.Errorf("filet = %q", last)
	}
	if got := FiletLabel(in); got != "Contexts · 12 of 12 · ▲ 2 more · ▼ 2 more" {
		t.Errorf("FiletLabel = %q", got)
	}
}

func TestArgNoticeWinsOverHeading(t *testing.T) {
	in := argInput(80)
	in.Notice = `"w" matches web, work — type more letters or press tab`
	last := plain(render(t, in))
	l := last[len(last)-1]
	if !strings.Contains(l, theme.IconWarning) || !strings.Contains(l, in.Notice) {
		t.Errorf("filet = %q", l)
	}
	if strings.Contains(l, "Contexts") {
		t.Errorf("heading shown next to the notice: %q", l)
	}
}

func TestArgLongNameIsTruncatedOnANarrowTerminal(t *testing.T) {
	in := argInput(30)
	in.Rows = []Row{argRow("a-very-long-context-name-indeed", "Create a context", false)}
	in.Visible = 1
	if l := plain(render(t, in))[0]; !strings.Contains(l, "…") {
		t.Errorf("no ellipsis: %q", l)
	}
}

func TestArgLinesHaveExactWidth(t *testing.T) {
	testutil.TrueColor(t)
	long := argRow("a-very-long-context-name-indeed", "A note that is much longer than any column can hold", true)
	wide := argRow("コンテキスト長い名前", "注記", true)
	for _, w := range []int{120, 80, 60, 40, 34, 30, 20, 8, 7, 6, 5, 3, 0} {
		cases := map[string]Input{
			"rows":    argInput(w),
			"long":    {Rows: []Row{long, wide}, Total: 2, Heading: "Contexts · 2 of 2", Cursor: 0, Width: w, Visible: 3},
			"loading": {Loading: true, Heading: "Contexts", Cursor: -1, Width: w, Visible: 2},
			"empty":   {Empty: `No context starts with "wrok"`, Heading: "Contexts · 0 of 5", Cursor: -1, Width: w, Visible: 2},
			"notice":  {Rows: ctxRows(), Notice: `"w" matches web, work — type more letters or press tab`, Cursor: 1, Width: w, Visible: 4},
		}
		for name, in := range cases {
			for i, l := range render(t, in) {
				if got := lipgloss.Width(l); got != w {
					t.Errorf("%s w=%d line %d: width %d: %q", name, w, i, got, ansi.Strip(l))
				}
			}
		}
	}
}

func TestPaletteCoversArgumentColors(t *testing.T) {
	names := map[string]bool{}
	for _, p := range Palette() {
		names[p.Name] = true
	}
	for _, want := range []string{"argument note", "highlighted argument note", "message"} {
		if !names[want] {
			t.Errorf("Palette lacks pair %q, so the contrast test cannot cover it", want)
		}
	}
}
