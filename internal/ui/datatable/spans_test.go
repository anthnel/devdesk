package datatable

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Column.Spans (DEVDESK-127): a cell painted in any number of runs. The text is
// measured, truncated and padded first (Rule 122) and the spans are laid over
// the finished text, so the tests read the rendered cell back one cell at a
// time and compare each cell's colour with a reference computed independently
// (spanOracle) rather than with a second copy of the implementation.

const (
	barText  = "0123456789" // ten distinct cells: a cell's digit says where it is
	barWidth = 10
)

// spanColours are the four span foregrounds; spanDefault is the column's Style,
// which paints whatever no span covers. Five colours that must all differ,
// otherwise a leak between two runs could not be seen (checked by
// TestSpansFixtureColoursAreDistinct).
var spanColours = []lipgloss.Color{theme.ColorOK, theme.ColorError, theme.ColorWarn, theme.ColorHighlight}

const spanDefaultIdx = 4 // index of the Style colour in colourOf's answer

func spanDefault() lipgloss.Color { return theme.ColorSeverityLow }

func allFixtureColours() []lipgloss.Color {
	return append(append([]lipgloss.Color{}, spanColours...), spanDefault())
}

func fg(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// spansOf builds the Spans result for the given widths, span i painted with
// spanColours[i % 4].
func spansOf(widths ...int) []Span {
	out := make([]Span, 0, len(widths))
	for i, w := range widths {
		out = append(out, Span{Width: w, Style: fg(spanColours[i%len(spanColours)])})
	}
	return out
}

// spansConfig is the shared fixture: the test config with its Size column
// replaced by a fixed 10-cell bar. withSpans nil leaves the column without
// Spans, which is the reference every "unchanged" assertion compares against.
func spansConfig(cell string, withSpans func(row) []Span) Config[row] {
	cfg := testConfig()
	cfg.Key = func(r row) string { return r.Name }
	cfg.Columns[1] = Column[row]{
		Title: "Bar", Sizing: SizingFixed, MinWidth: barWidth,
		Cell:  func(row) string { return cell },
		Style: func(row) lipgloss.Style { return fg(spanDefault()) },
		Spans: withSpans,
	}
	return cfg
}

// dataRows is the rendered rows that hold an item; the table pads below them.
func dataRows(m *Model[row]) []string { return rowLines(m)[:len(fixtures())] }

func spansTable(t *testing.T, cfg Config[row], width int) Model[row] {
	t.Helper()
	m := New(cfg)
	m.Resize(width, 10)
	m.SetItems(fixtures())
	return m
}

// spanOracle says, for each of the n cells, which colour index (0..3 for a
// span, spanDefaultIdx for Style) the contract gives it: spans apply left to
// right, a negative width counts 0, what overflows is cut, what is left takes
// Style.
func spanOracle(widths []int, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = spanDefaultIdx
	}
	pos := 0
	for i, w := range widths {
		w = max(w, 0)
		end := min(pos+w, n)
		for c := pos; c < end; c++ {
			out[c] = i % len(spanColours)
		}
		pos = end
	}
	return out
}

var sgrSeq = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// cellParams walks a rendered line and returns, for each of the cell's runes,
// the SGR parameters in force when it was drawn. lipgloss opens a run with an
// absolute sequence and closes it with a reset, so "current" is the last
// opening since the last reset.
func cellParams(t *testing.T, line, cellVisible string) []string {
	t.Helper()
	var (
		runes   []rune
		params  []string
		current string
	)
	rest := line
	for len(rest) > 0 {
		loc := sgrSeq.FindStringSubmatchIndex(rest)
		if loc == nil {
			for _, r := range rest {
				runes, params = append(runes, r), append(params, current)
			}
			break
		}
		for _, r := range rest[:loc[0]] {
			runes, params = append(runes, r), append(params, current)
		}
		p := rest[loc[2]:loc[3]]
		if p == "" || p == "0" {
			current = ""
		} else {
			current = p
		}
		rest = rest[loc[1]:]
	}
	text := string(runes)
	at := strings.Index(text, cellVisible)
	if at < 0 {
		t.Fatalf("cell text %q not found in %q", cellVisible, text)
	}
	from := len([]rune(text[:at]))
	return params[from : from+len([]rune(cellVisible))]
}

// colourOf maps SGR parameters to a fixture colour index, -1 for none.
func colourOf(params string) int {
	for i, c := range allFixtureColours() {
		if strings.Contains(params, foreground(c)) {
			return i
		}
	}
	return -1
}

// assertWellFormed fails on any escape sequence that is not a complete SGR (a
// cut sequence), and on a rendered width different from lipgloss's.
func assertWellFormed(t *testing.T, line string) {
	t.Helper()
	if got, want := strings.Count(line, "\x1b"), len(sgrSeq.FindAllString(line, -1)); got != want {
		t.Errorf("%d escape characters but %d complete SGR sequences — a sequence was cut: %q", got, want, line)
	}
	if plain := ansi.Strip(line); strings.ContainsRune(plain, '\x1b') {
		t.Errorf("x/ansi still finds an escape after stripping: %q", plain)
	}
	if a, b := ansi.StringWidth(line), lipgloss.Width(line); a != b {
		t.Errorf("x/ansi measures %d cells, lipgloss %d", a, b)
	}
}

func assertCellColours(t *testing.T, line, cell string, want []int) {
	t.Helper()
	got := cellParams(t, line, cell)
	for i, w := range want {
		if c := colourOf(got[i]); c != w {
			t.Errorf("cell %d (%q) has colour index %d, want %d (params %q)", i, string([]rune(cell)[i]), c, w, got[i])
		}
	}
}

func TestSpansFixtureColoursAreDistinct(t *testing.T) {
	withTrueColor(t)
	seen := map[string]bool{}
	for _, c := range allFixtureColours() {
		key := foreground(c)
		if seen[key] {
			t.Fatalf("two fixture colours render the same (%s): the tests could not tell their runs apart", key)
		}
		seen[key] = true
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 distinct colours, got %d", len(seen))
	}
}

// ── Exact ranges ──────────────────────────────────────────────────────────────

// Four spans [2,1,2,5] on ten distinct cells: each colour sits on exactly its
// interval, the text is untouched and the row spans what the plain one does.
func TestSpansEachRangeCarriesItsOwnColour(t *testing.T) {
	withTrueColor(t)
	m := spansTable(t, spansConfig(barText, func(row) []Span { return spansOf(2, 1, 2, 5) }), 120)
	plain := spansTable(t, spansConfig(barText, nil), 120)

	line := rowLines(&m)[1] // row 0 is under the cursor
	assertCellColours(t, line, barText, []int{0, 0, 1, 2, 2, 3, 3, 3, 3, 3})
	assertWellFormed(t, line)
	if got, want := visible(line), visible(rowLines(&plain)[1]); got != want {
		t.Errorf("text changed: %q, want %q", got, want)
	}
	if got, want := lipgloss.Width(line), lipgloss.Width(rowLines(&plain)[1]); got != want {
		t.Errorf("row is %d cells with spans, %d without", got, want)
	}
}

// Widths that are zero, negative or too large, and cells left over, in one table.
func TestSpansWidthEdgeCases(t *testing.T) {
	withTrueColor(t)
	cases := []struct {
		name   string
		widths []int
	}{
		{"zero width renders nothing", []int{0, 3, 0, 2}},
		{"negative counts as zero", []int{-4, 3, -1, 2}},
		{"overflow is cut", []int{4, 100, 3}},
		{"exact fit leaves no remainder", []int{3, 3, 4}},
		{"short leaves the rest in Style", []int{3, 2}},
		{"only zeros: everything in Style", []int{0, 0, 0}},
		{"spans after the cell is full are ignored", []int{10, 5, 5}},
	}
	ran := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran++
			m := spansTable(t, spansConfig(barText, func(row) []Span { return spansOf(tc.widths...) }), 120)
			plain := spansTable(t, spansConfig(barText, nil), 120)
			line := rowLines(&m)[1]

			assertCellColours(t, line, barText, spanOracle(tc.widths, barWidth))
			assertWellFormed(t, line)
			if got, want := visible(line), visible(rowLines(&plain)[1]); got != want {
				t.Errorf("text changed: %q, want %q", got, want)
			}
			if got, want := lipgloss.Width(line), lipgloss.Width(rowLines(&plain)[1]); got != want {
				t.Errorf("width %d, want %d", got, want)
			}
			// A zero-width run must not leave an empty styled run behind: an
			// opening sequence immediately followed by a reset.
			if empty := regexp.MustCompile("\x1b\\[[0-9;]+m\x1b\\[0m").FindString(line); empty != "" {
				t.Errorf("empty styled run %q in %q", empty, line)
			}
		})
	}
	if ran != len(cases) {
		t.Fatalf("ran %d of %d cases", ran, len(cases))
	}
}

// The loop of the plan: each of four spans takes every width from 0 to past the
// cell, the others staying put. No panic, the same text, the same width, and
// every cell has the colour the oracle gives it. The count is asserted so the
// loop cannot pass by running nothing.
func TestSpansEveryWidthOfEverySpan(t *testing.T) {
	withTrueColor(t)
	base := []int{2, 1, 2, 3}
	plain := spansTable(t, spansConfig(barText, nil), 120)
	wantLine := rowLines(&plain)[1]

	ran := 0
	for which := range base {
		for w := 0; w <= barWidth+2; w++ {
			widths := append([]int{}, base...)
			widths[which] = w
			m := spansTable(t, spansConfig(barText, func(row) []Span { return spansOf(widths...) }), 120)

			line := rowLines(&m)[1]
			label := fmt.Sprintf("span %d width %d", which, w)
			if visible(line) != visible(wantLine) || lipgloss.Width(line) != lipgloss.Width(wantLine) {
				t.Errorf("%s: text or width changed: %q", label, line)
			}
			got := cellParams(t, line, barText)
			for i, want := range spanOracle(widths, barWidth) {
				if c := colourOf(got[i]); c != want {
					t.Errorf("%s: cell %d colour index %d, want %d", label, i, c, want)
				}
			}
			ran++
		}
	}
	if want := len(base) * (barWidth + 3); ran != want {
		t.Fatalf("ran %d cases, want %d", ran, want)
	}
}

// ── Truncation happens before colouring ───────────────────────────────────────

// A value longer than its column is cut (with the marker) while still plain; the
// spans then cover the fitted text. Nothing is cut inside an escape sequence.
func TestSpansColourTheTruncatedTextNotTheOriginal(t *testing.T) {
	withTrueColor(t)
	const long = "01234567890123456789" // 20 cells into a 10-cell column
	widths := []int{4, 3, 3}
	m := spansTable(t, spansConfig(long, func(row) []Span { return spansOf(widths...) }), 120)
	plain := spansTable(t, spansConfig(long, nil), 120)

	line := rowLines(&m)[1]
	fitted := "012345678…"
	assertCellColours(t, line, fitted, spanOracle(widths, barWidth))
	assertWellFormed(t, line)
	if got, want := visible(line), visible(rowLines(&plain)[1]); got != want {
		t.Errorf("text %q, want %q", got, want)
	}
	if got, want := lipgloss.Width(line), lipgloss.Width(rowLines(&plain)[1]); got != want {
		t.Errorf("width %d, want %d", got, want)
	}
}

// ── nil Spans ─────────────────────────────────────────────────────────────────

// A row whose Spans answers nil (or empty) takes the one-run path, byte for
// byte: this is how a "-" placeholder stays dim.
func TestSpansNilAndEmptyTakeTheOneRunPath(t *testing.T) {
	withTrueColor(t)
	plain := spansTable(t, spansConfig(barText, nil), 120)

	variants := map[string]func(row) []Span{
		"nil":   func(row) []Span { return nil },
		"empty": func(row) []Span { return []Span{} },
		"nil for cache only": func(r row) []Span {
			if r.Name == "cache" {
				return nil
			}
			return spansOf(2, 2)
		},
	}
	ran := 0
	for name, fn := range variants {
		m := spansTable(t, spansConfig(barText, fn), 120)
		for i, line := range dataRows(&m) {
			if name == "nil for cache only" && i != 1 {
				continue // only cache is compared; the others do carry spans
			}
			if want := rowLines(&plain)[i]; line != want {
				t.Errorf("%s, row %d differs from the one-run rendering:\n got %q\nwant %q", name, i, line, want)
			}
			ran++
		}
	}
	if ran != 3+3+1 {
		t.Fatalf("compared %d rows, want 7", ran)
	}
}

// ── Selection ─────────────────────────────────────────────────────────────────

// On the plain selection every run repaints the selection background in bold
// and keeps its own foreground.
func TestSpansOnTheNormalSelectionRepaintEachRun(t *testing.T) {
	withTrueColor(t)
	widths := []int{2, 1, 2, 5}
	m := spansTable(t, spansConfig(barText, func(row) []Span { return spansOf(widths...) }), 120)

	line := rowLines(&m)[0] // under the cursor
	assertWellFormed(t, line)
	got := cellParams(t, line, barText)
	wantBg := background(theme.ColorTableLineSelected)
	for i, want := range spanOracle(widths, barWidth) {
		if c := colourOf(got[i]); c != want {
			t.Errorf("cell %d has colour index %d, want %d (params %q)", i, c, want, got[i])
		}
		if !strings.Contains(got[i], wantBg) {
			t.Errorf("cell %d lacks the selection background: %q", i, got[i])
		}
		if got[i] != "1" && !strings.HasPrefix(got[i], "1;") {
			t.Errorf("cell %d is not bold: %q", i, got[i])
		}
	}
}

// A row a view coloured whole drops the spans entirely: the line is exactly what
// the same table renders without Spans, one style and the state's background.
func TestSpansAreDroppedOnAWholeColouredRow(t *testing.T) {
	withTrueColor(t)
	spans := func(row) []Span { return spansOf(2, 1, 2, 5) }

	t.Run("error selection", func(t *testing.T) {
		with := errorStyledTable(t, func() Config[row] { return spansConfig(barText, spans) })
		without := errorStyledTable(t, func() Config[row] { return spansConfig(barText, nil) })
		got := rowLines(&with)[0]
		if got != rowLines(&without)[0] {
			t.Errorf("spans reached the error row:\n got %q\nwant %q", got, rowLines(&without)[0])
		}
		for _, c := range spanColours {
			if strings.Contains(got, foreground(c)) {
				t.Errorf("span colour %s leaked into the error row", foreground(c))
			}
		}
		if !strings.Contains(got, background(theme.ColorError)) {
			t.Errorf("error background missing: %q", got)
		}
	})

	t.Run("severity selection", func(t *testing.T) {
		build := func(withSpans func(row) []Span) Model[row] {
			cfg := spansConfig(barText, withSpans)
			cfg.SelectedStyles = func(row) table.Styles { return theme.TableStylesForSeverity("CRITICAL") }
			return spansTable(t, cfg, 120)
		}
		with, without := build(spans), build(nil)
		if got, want := rowLines(&with)[0], rowLines(&without)[0]; got != want {
			t.Errorf("spans reached the severity row:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("busy row, selected and not", func(t *testing.T) {
		build := func(withSpans func(row) []Span) Model[row] {
			m := spansTable(t, spansConfig(barText, withSpans), 120)
			m.MarkBusy("api", "Working") // under the cursor
			m.MarkBusy("web", "Working") // not
			return m
		}
		with, without := build(spans), build(nil)
		ran := 0
		for i, line := range dataRows(&with) {
			if i == 1 {
				continue // cache is not busy; it keeps its spans
			}
			if want := rowLines(&without)[i]; line != want {
				t.Errorf("busy row %d keeps spans:\n got %q\nwant %q", i, line, want)
			}
			ran++
		}
		if ran != 2 {
			t.Fatalf("compared %d busy rows, want 2", ran)
		}
	})
}

// ── Rule 116 ──────────────────────────────────────────────────────────────────

// A Spans column does not change what the table spans, at any width, rows
// selected or not.
func TestSpansDoNotChangeTheRenderedWidth(t *testing.T) {
	withTrueColor(t)
	ran := 0
	for _, width := range []int{50, 80, 120, 200} {
		with := spansTable(t, spansConfig(barText, func(row) []Span { return spansOf(2, 1, 2, 5) }), width)
		plain := spansTable(t, spansConfig(barText, nil), width)

		if got, want := with.RenderedWidth(), plain.RenderedWidth(); got != want {
			t.Errorf("width %d: RenderedWidth %d with spans, %d without", width, got, want)
		}
		wantLines := rowLines(&plain)
		for i, line := range dataRows(&with) {
			if got, want := lipgloss.Width(line), lipgloss.Width(wantLines[i]); got != want {
				t.Errorf("width %d, row %d: %d cells with spans, %d without", width, i, got, want)
			}
			ran++
		}
	}
	if ran != 4*len(fixtures()) {
		t.Fatalf("compared %d rows, want %d", ran, 4*len(fixtures()))
	}
}

// ── Precedence over Cut/TailStyle ─────────────────────────────────────────────

func TestSpansTakePrecedenceOverCutAndTailStyle(t *testing.T) {
	withTrueColor(t)
	widths := []int{2, 1, 2, 5}
	cfg := spansConfig(barText, func(row) []Span { return spansOf(widths...) })
	cfg.Columns[1].Cut = func(row) int { return 5 }
	cfg.Columns[1].TailStyle = func(row) lipgloss.Style { return fg(spanColours[3]) }
	m := spansTable(t, cfg, 120)

	assertCellColours(t, rowLines(&m)[1], barText, spanOracle(widths, barWidth))
}

// ── Source scan ───────────────────────────────────────────────────────────────

// columnsMixingSpans names the Column literals of a file that set Spans together
// with Cut or TailStyle.
func columnsMixingSpans(fset *token.FileSet, file *ast.File) []string {
	var bad []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, column := range columnLiterals(lit) {
			if fieldValue(column, "Spans") == nil {
				continue
			}
			if fieldValue(column, "Cut") != nil || fieldValue(column, "TailStyle") != nil {
				bad = append(bad, fset.Position(column.Pos()).String()+" "+titleOf(column))
			}
		}
		return true
	})
	return bad
}

// The detector itself, on source it must refuse and source it must accept — the
// tree scan below is green today because no column mixes them, so this is what
// shows it would not stay green for the wrong reason.
func TestTheSpansScanRecognisesAMixedColumn(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"spans and cut", `var _ = Column[row]{Title: "A", Spans: f, Cut: g}`, 1},
		{"spans and tailstyle", `var _ = Column[row]{Title: "A", Spans: f, TailStyle: g}`, 1},
		{"spans, cut and tailstyle in a slice", `var _ = []Column[row]{{Title: "A", Spans: f, Cut: g, TailStyle: h}, {Title: "B", Spans: f}}`, 1},
		{"spans alone", `var _ = Column[row]{Title: "A", Spans: f}`, 0},
		{"cut and tailstyle alone", `var _ = Column[row]{Title: "A", Cut: g, TailStyle: h}`, 0},
		{"qualified type", `var _ = datatable.Column[row]{Title: "A", Spans: f, Cut: g}`, 1},
	}
	ran := 0
	for _, tc := range cases {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "x.go", "package x\n"+tc.src, 0)
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if got := columnsMixingSpans(fset, file); len(got) != tc.want {
			t.Errorf("%s: flagged %d column(s) %v, want %d", tc.name, len(got), got, tc.want)
		}
		ran++
	}
	if ran != len(cases) {
		t.Fatalf("ran %d of %d", ran, len(cases))
	}
}

func TestNoColumnDeclaresSpansWithCutOrTailStyle(t *testing.T) {
	var bad []string
	files := 0
	err := filepath.WalkDir(sizingScannedTree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		files++
		bad = append(bad, columnsMixingSpans(fset, file)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", sizingScannedTree, err)
	}
	if files == 0 {
		t.Fatal("the scan read no file")
	}
	if len(bad) > 0 {
		t.Errorf("%d column(s) set Spans with Cut/TailStyle — Spans replaces them, declare one mechanism:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}
