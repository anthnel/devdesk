package cmdsuggest

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func TestGeometry(t *testing.T) {
	tests := []struct {
		name          string
		w, h, hdr, ft int
		want          Rect
	}{
		{"80x24", 80, 24, 8, 2, Rect{X: 2, Y: 8, Width: 76, Rows: 8, Height: 12}},
		{"120x40", 120, 40, 8, 2, Rect{X: 20, Y: 8, Width: 80, Rows: 8, Height: 12}},
		{"60x30", 60, 30, 8, 2, Rect{X: 2, Y: 8, Width: 56, Rows: 8, Height: 12}},
		{"40x16 footer 2", 40, 16, 8, 2, Rect{X: 0, Y: 8, Width: 40, Rows: 2, Height: 6}},
		{"40x16 no footer", 40, 16, 8, 0, Rect{X: 0, Y: 8, Width: 40, Rows: 4, Height: 8}},
		{"53 wide is full width", 53, 30, 8, 2, Rect{X: 0, Y: 8, Width: 53, Rows: 8, Height: 12}},
		{"54 wide leaves a margin", 54, 30, 8, 2, Rect{X: 2, Y: 8, Width: 50, Rows: 8, Height: 12}},
		{"200 wide caps at 80", 200, 40, 8, 2, Rect{X: 60, Y: 8, Width: 80, Rows: 8, Height: 12}},
		{"V=4 reduced frame", 80, 14, 8, 2, Rect{X: 2, Y: 8, Width: 76, Rows: 0, Height: 3}},
		{"V=5 one row", 80, 15, 8, 2, Rect{X: 2, Y: 8, Width: 76, Rows: 1, Height: 5}},
		{"80x10 pulled up", 80, 10, 8, 2, Rect{X: 2, Y: 7, Width: 76, Rows: 0, Height: 3}},
		{"80x9 pulled up, clipped later", 80, 9, 8, 2, Rect{X: 2, Y: 7, Width: 76, Rows: 0, Height: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Geometry(tt.w, tt.h, tt.hdr, tt.ft); got != tt.want {
				t.Errorf("Geometry(%d,%d,%d,%d) = %+v, want %+v", tt.w, tt.h, tt.hdr, tt.ft, got, tt.want)
			}
		})
	}
}

func TestGeometryNeverPanicsOrGoesNegative(t *testing.T) {
	for _, d := range [][4]int{{0, 0, 0, 0}, {1, 1, 8, 2}, {-5, -5, -1, -1}, {10, 3, 8, 2}, {80, 1000, 8, 2}} {
		r := Geometry(d[0], d[1], d[2], d[3])
		if r.Width < 0 || r.Height < 0 || r.Rows < 0 || r.X < 0 || r.Y < 0 {
			t.Errorf("Geometry%v = %+v has a negative field", d, r)
		}
	}
}

// The frame is centered: the margin on each side differs by at most one cell.
func TestGeometryIsCentered(t *testing.T) {
	for w := 54; w <= 200; w++ {
		r := Geometry(w, 40, 8, 2)
		if left, right := r.X, w-r.X-r.Width; left != right && left+1 != right {
			t.Errorf("width %d: margins %d/%d are not centered", w, left, right)
		}
	}
}

func TestPaletteRows(t *testing.T) {
	want := []int{0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 8, 8}
	for v, w := range want {
		if got := PaletteRows(v); got != w {
			t.Errorf("PaletteRows(%d) = %d, want %d", v, got, w)
		}
	}
	if got := PaletteRows(-3); got != 0 {
		t.Errorf("PaletteRows(-3) = %d, want 0", got)
	}
}

// matches returns the first n registry rows as the list a query would give.
func matches(n int) []Row {
	rows := allRows()
	return rows[:min(n, len(rows))]
}

// inputLine stands in for the caller's rendered text input.
func inputLine(text string) string {
	return lipgloss.NewStyle().Foreground(theme.ColorText).Background(theme.ColorBackground).Render("❯ " + text)
}

func palette(title, query string, rows []Row, _ Rect) PaletteInput {
	return PaletteInput{
		Title: title,
		Input: inputLine(query),
		List:  Input{Rows: rows, Total: len(allRows()), Query: query, Cursor: -1},
	}
}

func renderPalette(t *testing.T, p PaletteInput, r Rect) []string {
	t.Helper()
	lines := RenderPalette(p, r)
	if len(lines) != r.Height {
		t.Fatalf("RenderPalette returned %d lines for Rect.Height %d", len(lines), r.Height)
	}
	return lines
}

func TestRenderPaletteLineOrder(t *testing.T) {
	testutil.TrueColor(t)
	r := Geometry(80, 24, 8, 2)
	lines := plain(renderPalette(t, palette("Go to", "", allRows(), r), r))

	if !strings.HasPrefix(lines[0], "┌─ Go to ") || !strings.HasSuffix(lines[0], "┐") {
		t.Errorf("title line = %q, want ┌─ Go to … ┐", lines[0])
	}
	if !strings.HasPrefix(lines[1], "│") || !strings.Contains(lines[1], "❯") || !strings.HasSuffix(lines[1], "│") {
		t.Errorf("input line = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "├") || !strings.HasSuffix(lines[2], "┤") || strings.Trim(lines[2], "├─┤") != "" {
		t.Errorf("separator = %q, want ├──…──┤", lines[2])
	}
	for i := 3; i < 3+r.Rows; i++ {
		if !strings.HasPrefix(lines[i], "│") || !strings.HasSuffix(lines[i], "│") {
			t.Errorf("row line %d = %q, want │ … │", i, lines[i])
		}
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "└") || !strings.HasSuffix(last, "┘") {
		t.Errorf("last line = %q, want └ … ┘", last)
	}
	if !strings.Contains(last, "17 available · ▼ 9 more") {
		t.Errorf("status rule = %q, want '17 available · ▼ 9 more'", last)
	}
	if r.Height != r.Rows+4 {
		t.Errorf("Height %d != Rows+4", r.Height)
	}
}

func TestRenderPaletteTitleAtContextStage(t *testing.T) {
	testutil.TrueColor(t)
	r := Geometry(80, 24, 8, 2)
	lines := plain(renderPalette(t, palette("Contexts", "", nil, r), r))
	if !strings.HasPrefix(lines[0], "┌─ Contexts ") {
		t.Errorf("title line = %q, want ┌─ Contexts …", lines[0])
	}
}

// The height depends on the geometry only: typing, filtering and scrolling
// never make the frame grow or shrink.
func TestRenderPaletteHeightIsConstant(t *testing.T) {
	testutil.TrueColor(t)
	r := Geometry(80, 24, 8, 2)
	cases := map[string]PaletteInput{
		"all rows":      palette("Go to", "", allRows(), r),
		"3 matches":     palette("Go to", "c", matches(3), r),
		"one match":     palette("Go to", "conf", matches(1), r),
		"no match":      palette("Go to", "zzz", nil, r),
		"loading":       func() PaletteInput { p := palette("Contexts", "", nil, r); p.List.Loading = true; return p }(),
		"scrolled down": func() PaletteInput { p := palette("Go to", "", allRows(), r); p.List.Offset = 9; return p }(),
		"cursor":        func() PaletteInput { p := palette("Go to", "", allRows(), r); p.List.Cursor = 4; return p }(),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			lines := plain(renderPalette(t, p, r))
			if got := len(lines); got != r.Height {
				t.Fatalf("%d lines, want %d", got, r.Height)
			}
			for i, l := range lines[3 : 3+r.Rows] {
				if !strings.HasPrefix(l, "│") {
					t.Errorf("row slot %d = %q is not a framed row", i, l)
				}
			}
		})
	}
}

func TestRenderPaletteStatusText(t *testing.T) {
	testutil.TrueColor(t)
	r := Geometry(80, 24, 8, 2)
	tests := []struct {
		name string
		p    PaletteInput
		want string
		not  string
	}{
		{"filtered count", palette("Go to", "c", matches(3), r), "3 of 17", "more"},
		{"scrolled", func() PaletteInput { p := palette("Go to", "", allRows(), r); p.List.Offset = 9; return p }(), "▲ 9 more", "▼"},
		{"notice", func() PaletteInput {
			p := palette("Go to", "zz", nil, r)
			p.List.Notice = "Unknown command: zz"
			return p
		}(), theme.IconWarning + " Unknown command: zz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := plain(renderPalette(t, tt.p, r))
			last := lines[len(lines)-1]
			if !strings.Contains(last, tt.want) {
				t.Errorf("status rule = %q, want it to contain %q", last, tt.want)
			}
			if tt.not != "" && strings.Contains(last, tt.not) {
				t.Errorf("status rule = %q must not contain %q", last, tt.not)
			}
		})
	}
}

// A viewport under 5 lines draws title, input and rule only, and never panics.
func TestRenderPaletteReducedFrame(t *testing.T) {
	testutil.TrueColor(t)
	for _, size := range [][2]int{{80, 14}, {80, 12}, {80, 10}, {40, 9}} {
		r := Geometry(size[0], size[1], 8, 2)
		if r.Rows != 0 || r.Height != 3 {
			t.Fatalf("%v: Rect = %+v, want Rows 0 / Height 3", size, r)
		}
		p := palette("Go to", "", allRows(), r)
		p.List.Notice = "Unknown command: x"
		for _, in := range []PaletteInput{palette("Go to", "", allRows(), r), p} {
			lines := plain(renderPalette(t, in, r))
			if !strings.HasPrefix(lines[0], "┌─ Go to ") || !strings.Contains(lines[1], "❯") ||
				!strings.HasPrefix(lines[2], "└") || !strings.HasSuffix(lines[2], "┘") {
				t.Errorf("%v: reduced frame = %q", size, lines)
			}
			if strings.Contains(lines[2], "more") {
				t.Errorf("%v: reduced rule %q must not announce hidden rows", size, lines[2])
			}
		}
	}
	r := Geometry(80, 14, 8, 2)
	if got := plain(renderPalette(t, palette("Go to", "", allRows(), r), r))[2]; !strings.Contains(got, "17 available") {
		t.Errorf("reduced rule = %q, want '17 available'", got)
	}
	notice := palette("Go to", "x", nil, r)
	notice.List.Notice = "Unknown command: x"
	if got := plain(renderPalette(t, notice, r))[2]; !strings.Contains(got, theme.IconWarning+" Unknown command: x") {
		t.Errorf("reduced rule = %q, want the warning", got)
	}
}

func TestRenderPaletteEveryLineHasExactWidth(t *testing.T) {
	testutil.TrueColor(t)
	for _, w := range []int{4, 5, 6, 10, 30, 40, 53, 54, 60, 80, 99, 100, 120, 200} {
		r := Geometry(w, 30, 8, 2)
		for name, p := range map[string]PaletteInput{
			"all":     palette("Go to", "", allRows(), r),
			"filter":  palette("Go to", "co", matches(3), r),
			"none":    palette("Contexts", "zzz", nil, r),
			"current": func() PaletteInput { p := palette("Go to", "", allRows(), r); p.List.Cursor = 1; return p }(),
		} {
			for i, l := range renderPalette(t, p, r) {
				if got := lipgloss.Width(l); got != r.Width {
					t.Errorf("W=%d %s line %d: width %d, want %d: %q", w, name, i, got, r.Width, l)
				}
			}
		}
	}
}

// Column thresholds are the list's own (Columns): the description column
// appears exactly when Columns says so for the frame width.
func TestRenderPaletteKeepsColumnThresholds(t *testing.T) {
	testutil.TrueColor(t)
	entry := command.Entries()[0]
	if len(entry.Description) < 8 {
		t.Skip("first entry has no description to look for")
	}
	probe := entry.Description[:8]
	for _, w := range []int{30, 40, 54, 80, 120} {
		r := Geometry(w, 30, 8, 2)
		lines := plain(renderPalette(t, palette("Go to", "", allRows(), r), r))
		body := strings.Join(lines[3:3+r.Rows], "\n")
		if want := Columns(r.Width).Desc > 0; strings.Contains(body, probe) != want {
			t.Errorf("W=%d (frame %d): description visible = %v, want %v", w, r.Width, !want, want)
		}
	}
}

// Rule 134: shortcuts belong to the header, not to the frame.
func TestRenderPaletteHasNoInlineKeyHelp(t *testing.T) {
	testutil.TrueColor(t)
	inline := regexp.MustCompile(`\[[^\]]+\]\s+\S`)
	r := Geometry(120, 40, 8, 2)
	for _, l := range plain(renderPalette(t, palette("Go to", "", allRows(), r), r)) {
		if inline.MatchString(l) {
			t.Errorf("line %q looks like a '[key] action' hint (Rule 134)", l)
		}
	}
}

var (
	wellFormedSeq = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)
	anySeq        = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]|[^\x1b]+`)
)

// A line must carry no truncated escape sequence and no visible cell
// painted without an explicit background (a reset drops it).
func TestRenderPaletteLinesAreWholeAndPainted(t *testing.T) {
	testutil.TrueColor(t)
	for _, w := range []int{30, 54, 80, 120} {
		r := Geometry(w, 30, 8, 2)
		for name, p := range map[string]PaletteInput{
			"all":    palette("Go to", "", allRows(), r),
			"cursor": func() PaletteInput { p := palette("Go to", "", allRows(), r); p.List.Cursor = 2; return p }(),
			"notice": func() PaletteInput {
				p := palette("Go to", "x", nil, r)
				p.List.Notice = "Unknown command: x"
				return p
			}(),
		} {
			for i, l := range renderPalette(t, p, r) {
				if strings.Contains(wellFormedSeq.ReplaceAllString(l, ""), "\x1b") {
					t.Errorf("W=%d %s line %d has a truncated escape sequence: %q", w, name, i, l)
					continue
				}
				hasBg := false
				for _, tok := range anySeq.FindAllString(l, -1) {
					if strings.HasPrefix(tok, "\x1b") {
						params := strings.TrimSuffix(strings.TrimPrefix(tok, "\x1b["), "m")
						switch {
						case !strings.HasSuffix(tok, "m"):
						case params == "" || params == "0":
							hasBg = false
						case strings.Contains(";"+params, ";48;"):
							hasBg = true
						}
						continue
					}
					if !hasBg {
						t.Errorf("W=%d %s line %d: text %q has no explicit background: %q", w, name, i, tok, l)
					}
				}
			}
		}
	}
}

// Degenerate widths keep the exact line count and width.
func TestRenderPaletteOnWidthsBelowTheMinimum(t *testing.T) {
	for _, w := range []int{0, 1, 3, 5} {
		r := Rect{Width: w, Height: 7, Rows: 3}
		lines := RenderPalette(palette("Go to", "", allRows(), r), r)
		if len(lines) != r.Height {
			t.Errorf("W=%d: %d lines, want %d", w, len(lines), r.Height)
		}
		for i, l := range lines {
			if got := lipgloss.Width(l); got != w {
				t.Errorf("W=%d line %d: width %d", w, i, got)
			}
		}
	}
}

// With no rows drawn the rule counts what exists and announces nothing hidden.
func TestFiletLabelWithNoVisibleRows(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want string
	}{
		{"empty query", Input{Rows: allRows(), Total: 17, Visible: 0}, "17 available"},
		{"filtered", Input{Rows: matches(3), Total: 17, Query: "c", Visible: 0}, "3 of 17"},
		{"notice unchanged", Input{Rows: allRows(), Total: 17, Visible: 0, Notice: "nope"}, theme.IconWarning + " nope"},
	}
	for _, tt := range tests {
		if got := FiletLabel(tt.in); got != tt.want {
			t.Errorf("%s: FiletLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := FiletLabel(Input{Rows: allRows(), Total: 17, Visible: 8}); got != fmt.Sprintf("17 available · ▼ %d more", 9) {
		t.Errorf("with 8 visible rows FiletLabel = %q", got)
	}
}
