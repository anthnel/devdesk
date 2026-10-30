package cmdsuggest

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func allRows() []Row {
	var rows []Row
	for _, e := range command.Entries() {
		rows = append(rows, Row{Entry: e})
	}
	return rows
}

func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// render fails the test instead of panicking when the line count is wrong.
func render(t *testing.T, in Input) []string {
	t.Helper()
	lines := Render(in)
	if len(lines) != in.Visible+1 {
		t.Fatalf("Render returned %d lines, want %d", len(lines), in.Visible+1)
	}
	return lines
}

func base(width int) Input {
	rows := allRows()
	return Input{Rows: rows, Total: len(rows), Cursor: -1, Width: width, Visible: 4}
}

func TestRegistryHasSeventeenEntries(t *testing.T) {
	if n := len(command.Entries()); n != 17 {
		t.Fatalf("fixture assumes 17 entries, registry has %d", n)
	}
}

func TestVisibleRows(t *testing.T) {
	tests := []struct{ matches, usable, want int }{
		{5, 0, 0}, {5, 1, 0}, {5, 2, 1}, {5, 9, 5}, {17, 9, 8},
		{17, 30, 8}, {0, 9, 1}, {0, 2, 1}, {3, 3, 2},
	}
	for _, tt := range tests {
		if got := VisibleRows(tt.matches, tt.usable); got != tt.want {
			t.Errorf("VisibleRows(%d,%d) = %d, want %d", tt.matches, tt.usable, got, tt.want)
		}
	}
}

func TestWindow(t *testing.T) {
	tests := []struct {
		name                          string
		cursor, offset, visible, want int
	}{
		{"inside", 3, 0, 8, 0},
		{"below window scrolls", 8, 0, 8, 1},
		{"above window scrolls", 2, 5, 8, 2},
		{"top does not wrap", 0, 0, 8, 0},
		{"no highlight keeps offset", -1, 3, 8, 3},
		{"never negative", 0, -2, 8, 0},
	}
	for _, tt := range tests {
		if got := Window(tt.cursor, tt.offset, tt.visible); got != tt.want {
			t.Errorf("%s: Window(%d,%d,%d) = %d, want %d", tt.name, tt.cursor, tt.offset, tt.visible, got, tt.want)
		}
	}
}

func TestColumns(t *testing.T) {
	tests := []struct {
		width            int
		alias, desc, nam bool
	}{
		{120, true, true, true}, {80, true, true, true}, {60, true, true, true},
		{54, true, true, true}, {50, true, false, true}, {40, true, false, true},
		{34, true, false, true}, {30, false, false, true},
	}
	for _, tt := range tests {
		l := Columns(tt.width)
		if l.Marker != 2 {
			t.Errorf("w=%d: Marker = %d, want 2", tt.width, l.Marker)
		}
		if (l.Name > 0) != tt.nam || (l.Alias > 0) != tt.alias || (l.Desc > 0) != tt.desc {
			t.Errorf("w=%d: layout %+v, want name=%v alias=%v desc=%v", tt.width, l, tt.nam, tt.alias, tt.desc)
		}
		if l.Desc > 0 && l.Desc < 20 {
			t.Errorf("w=%d: Desc = %d, never below 20", tt.width, l.Desc)
		}
		if used := 1 + l.Marker + l.Name + l.Alias + l.Desc; used > tt.width {
			t.Errorf("w=%d: columns use %d cells", tt.width, used)
		}
	}
	if l := Columns(80); l.Name != 15 || l.Alias != 14 {
		t.Errorf("w=80: name/alias = %d/%d, want 15/14", l.Name, l.Alias)
	}
	if l := Columns(120); l.Name != 18 || l.Alias != 18 {
		t.Errorf("w=120: name/alias = %d/%d, want 18/18", l.Name, l.Alias)
	}
	if l := Columns(30); l.Name > 30-6 {
		t.Errorf("w=30: Name = %d, want at most W-6", l.Name)
	}
}

func TestRenderEveryLineHasExactWidth(t *testing.T) {
	testutil.TrueColor(t)
	for _, w := range []int{120, 80, 60, 50, 40, 30} {
		for _, frame := range []Frame{FrameJoined, FrameClosed} {
			in := base(w)
			in.Cursor, in.Frame = 1, frame
			lines := render(t, in)
			for i, l := range lines {
				if got := lipgloss.Width(l); got != w {
					t.Errorf("w=%d line %d: width %d, want %d: %q", w, i, got, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestRenderFrameCorners(t *testing.T) {
	in := base(60)
	last := func(f Frame) string {
		in.Frame = f
		l := plain(render(t, in))
		return l[len(l)-1]
	}
	if j := last(FrameJoined); !strings.HasPrefix(j, "├") || !strings.HasSuffix(j, "┤") {
		t.Errorf("joined filet = %q", j)
	}
	if c := last(FrameClosed); !strings.HasPrefix(c, "└") || !strings.HasSuffix(c, "┘") {
		t.Errorf("closed filet = %q", c)
	}
}

func TestRenderHighlightedRow(t *testing.T) {
	testutil.TrueColor(t)
	in := base(80)
	in.Cursor = 2
	raw := render(t, in)
	lines := plain(raw)
	n := 0
	for i, l := range lines[:in.Visible] {
		body := strings.TrimPrefix(l, "│")
		body = strings.TrimLeft(body, " ")
		if strings.HasPrefix(body, theme.IconCircleSmall) {
			n++
			if i != 2 {
				t.Errorf("marker on row %d, want 2", i)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d highlighted rows, want 1: %q", n, lines)
	}
	if !strings.Contains(raw[2], "\x1b[1") && !strings.Contains(raw[2], ";1m") {
		t.Errorf("highlighted row is not bold: %q", raw[2])
	}
	if raw[2] == raw[1] {
		t.Error("highlighted row renders like a plain one")
	}
}

func TestRenderNoHighlightWhenCursorIsMinusOne(t *testing.T) {
	in := base(80)
	for _, l := range plain(render(t, in)) {
		if strings.Contains(l, theme.IconCircleSmall) {
			t.Errorf("marker found with Cursor -1: %q", l)
		}
	}
}

func TestRenderCurrentMarker(t *testing.T) {
	mk := func(w int) string {
		in := base(w)
		in.Rows[0].Current = true // about
		return plain(render(t, in))[0]
	}
	if l := mk(80); !strings.Contains(l, "(current) Build version") {
		t.Errorf("w=80: (current) should lead the description: %q", l)
	}
	l50 := mk(50)
	if !strings.Contains(l50, "(current)") || strings.Contains(l50, "version") {
		t.Errorf("w=50: (current) should replace the aliases: %q", l50)
	}
	if l := mk(30); strings.Contains(l, "(current)") {
		t.Errorf("w=30: (current) must be gone: %q", l)
	}
}

func TestContextStaysRecognizableAtAnyWidth(t *testing.T) {
	for _, w := range []int{120, 80, 60, 50, 40, 30} {
		in := base(w)
		var ctx []Row
		for _, r := range in.Rows {
			if r.Entry.Name == "context" {
				ctx = append(ctx, r)
			}
		}
		if len(ctx) != 1 {
			t.Skip("registry has no single context entry")
		}
		in.Rows, in.Visible = ctx, 1
		if l := plain(render(t, in))[0]; !strings.Contains(l, "context") {
			t.Errorf("w=%d: context not recognizable: %q", w, l)
		}
	}
}

func TestNameIsTruncatedWithEllipsisWhenNarrow(t *testing.T) {
	in := base(30)
	in.Rows = []Row{{Entry: command.Entry{Name: "a-very-long-command-name-indeed"}}}
	in.Visible = 1
	if l := plain(render(t, in))[0]; !strings.Contains(l, "…") {
		t.Errorf("no ellipsis: %q", l)
	}
}

func TestNoMatchRow(t *testing.T) {
	in := Input{Total: 17, Query: "zz", Cursor: -1, Width: 80, Visible: 1}
	lines := plain(render(t, in))
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], `No match for "zz"`) {
		t.Errorf("row = %q", lines[0])
	}
	if strings.Contains(lines[0], theme.IconCircleSmall) {
		t.Errorf("no-match row must not be selectable: %q", lines[0])
	}
	if !strings.Contains(lines[1], "0 of 17") {
		t.Errorf("filet = %q", lines[1])
	}
}

func TestFiletLabel(t *testing.T) {
	empty := base(80)
	empty.Visible = 8
	if got := FiletLabel(empty); got != "17 available · ▼ 9 more" {
		t.Errorf("empty input: %q", got)
	}

	scrolled := base(80)
	scrolled.Visible, scrolled.Cursor = 8, 8
	scrolled.Offset = Window(8, 0, 8)
	if got, want := FiletLabel(scrolled), "17 available · ▲ 1 more · ▼ 8 more"; got != want {
		t.Errorf("scrolled: %q, want %q", got, want)
	}

	typed := base(80)
	typed.Rows, typed.Query, typed.Visible = typed.Rows[:9], "c", 4
	typed.Offset = 2
	typed.Cursor = 2
	want := "9 of 17 · ▲ 2 more · ▼ 3 more"
	if got := FiletLabel(typed); got != want {
		t.Errorf("typed: %q, want %q", got, want)
	}

	short := base(80)
	short.Rows, short.Query, short.Visible, short.Cursor = short.Rows[:3], "co", 3, 0
	if got := FiletLabel(short); got != "3 of 17" {
		t.Errorf("short: %q", got)
	}

	none := Input{Total: 17, Query: "zz", Cursor: -1, Width: 80, Visible: 1}
	if got := FiletLabel(none); got != "0 of 17" {
		t.Errorf("none: %q", got)
	}
}

func TestFiletLabelWarning(t *testing.T) {
	in := base(80)
	in.Notice = `"zz" is not a view or a command`
	got := FiletLabel(in)
	if !strings.Contains(got, theme.IconWarning) || !strings.Contains(got, in.Notice) {
		t.Errorf("warning label = %q", got)
	}
	if line := plain(render(t, in)); !strings.Contains(line[len(line)-1], in.Notice) {
		t.Errorf("filet does not show the notice: %q", line[len(line)-1])
	}
}

func TestPalette(t *testing.T) {
	p := Palette()
	if len(p) == 0 {
		t.Fatal("Palette is empty")
	}
	for _, pair := range p {
		if pair.Name == "" || pair.Fg == "" || pair.Bg == "" {
			t.Errorf("incomplete pair %+v", pair)
		}
	}
}

// bgSequence returns the SGR parameters lipgloss emits for a background
// color ("48;2;r;g;b" in true color), which sit inside a longer sequence once
// a foreground and bold are combined.
func bgSequence(t *testing.T, c lipgloss.Color) string {
	t.Helper()
	out := lipgloss.NewStyle().Background(c).Render("x")
	m := regexp.MustCompile(`48;[0-9;]+`).FindString(out)
	if m == "" {
		t.Fatalf("no background sequence in %q", out)
	}
	return m
}

func TestHighlightedRowPaintsTheSelectionBackground(t *testing.T) {
	testutil.TrueColor(t)
	in := base(80)
	in.Cursor = 2
	raw := render(t, in)
	sel := bgSequence(t, theme.ColorTableLineSelected)
	if !strings.Contains(raw[2], sel) {
		t.Errorf("highlighted row lacks the selection background %q: %q", sel, raw[2])
	}
	for i, l := range raw {
		if i != 2 && strings.Contains(l, sel) {
			t.Errorf("line %d carries the selection background", i)
		}
	}
}

func TestNoMatchWithNoUsableRowDrawsNoListLine(t *testing.T) {
	in := Input{Total: 17, Query: "zz", Cursor: -1, Width: 80, Visible: 0}
	lines := plain(render(t, in))
	if len(lines) != 1 || !strings.Contains(lines[0], "0 of 17") {
		t.Errorf("want only the filet, got %q", lines)
	}
}

func TestRenderTinyWidthsKeepExactWidth(t *testing.T) {
	testutil.TrueColor(t)
	for w := 0; w <= 7; w++ {
		for _, rows := range [][]Row{allRows(), nil} {
			in := base(w)
			in.Rows, in.Query, in.Cursor, in.Notice = rows, "zz", 0, ""
			for _, notice := range []string{"", "something went wrong"} {
				in.Notice = notice
				for i, l := range render(t, in) {
					if got := lipgloss.Width(l); got != w {
						t.Errorf("w=%d rows=%d notice=%q line %d: width %d: %q", w, len(rows), notice, i, got, ansi.Strip(l))
					}
				}
			}
		}
	}
}

func TestRenderNegativeVisibleDoesNotPanic(t *testing.T) {
	in := base(80)
	in.Visible, in.Offset = -2, 3
	lines := Render(in)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want only the filet", len(lines))
	}
	if got := lipgloss.Width(lines[0]); got != 80 {
		t.Errorf("filet width %d, want 80", got)
	}
	_ = FiletLabel(in)
}

func TestWideCharactersKeepExactWidth(t *testing.T) {
	testutil.TrueColor(t)
	cjk := command.Entry{Name: "コマンド名前長い", Aliases: []string{"別名一つ", "別名二つ"}, Description: "これは非常に長い説明文です。日本語の幅を確認します"}
	for _, w := range []int{120, 80, 60, 40, 30, 20, 8} {
		in := Input{Rows: []Row{{Entry: cjk}, {Entry: cjk, Current: true}}, Total: 2, Cursor: 0, Width: w, Visible: 3, Notice: ""}
		for _, q := range []string{"", "漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字"} {
			in.Query = q
			for i, l := range render(t, in) {
				if got := lipgloss.Width(l); got != w {
					t.Errorf("w=%d line %d: width %d, want %d: %q", w, i, got, w, ansi.Strip(l))
				}
			}
		}
		in.Rows, in.Query = nil, "漢字漢字漢字漢字漢字漢字漢字漢字"
		for i, l := range render(t, in) {
			if got := lipgloss.Width(l); got != w {
				t.Errorf("no-match w=%d line %d: width %d: %q", w, i, got, ansi.Strip(l))
			}
		}
	}
}
