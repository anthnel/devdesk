package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// withTrueColor forces the renderer to emit escape sequences for the duration
// of one test. Without it lipgloss detects no TTY, falls back to the Ascii
// profile and strips every colour — which would make every assertion in this
// file pass whatever the code does.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// visible returns what a string puts on screen, escape sequences removed. The
// sequences are exactly what these tests are about, so the text has to be
// recoverable without them.
func visible(s string) string {
	var out strings.Builder
	for {
		before, rest, found := strings.Cut(s, "\x1b[")
		out.WriteString(before)
		if !found {
			return out.String()
		}
		if _, after, ok := strings.Cut(rest, "m"); ok {
			s = after
			continue
		}
		return out.String()
	}
}

// stateStyle colours the State column, which is what the views do with it.
func stateStyle(r row) lipgloss.Style {
	if r.State == "exited" {
		return lipgloss.NewStyle().Foreground(theme.ColorError)
	}
	return lipgloss.NewStyle().Foreground(theme.ColorOK)
}

func colouredConfig() Config[row] {
	cfg := testConfig()
	cfg.Columns[2].Style = stateStyle
	return cfg
}

func colouredTable(t *testing.T) Model[row] {
	t.Helper()
	m := New(colouredConfig())
	m.Resize(120, 10)
	m.SetItems(fixtures())
	return m
}

// rowLines returns the rendered rows, header excluded.
func rowLines(m *Model[row]) []string {
	return strings.Split(m.View(), "\n")[1:]
}

// ── The defect this file exists to make unexpressible ─────────────────────────

// The whole point. bubbles/table measured the cell value with runewidth before
// styling it, and runewidth counts an escape sequence's bytes as width: a
// seven-cell string carrying a colour measures 28, so it was truncated inside
// its own escape and the unterminated sequence bled over every row below.
//
// Colouring after the measurement means the visible width cannot depend on the
// colour, which is what this asserts directly.
func TestAColourDoesNotChangeWhatTheRowRenders(t *testing.T) {
	withTrueColor(t)

	plain := loaded(t)
	coloured := colouredTable(t)

	for i, got := range rowLines(&coloured) {
		want := rowLines(&plain)[i]
		if lipgloss.Width(got) != lipgloss.Width(want) {
			t.Errorf("row %d is %d cells coloured and %d plain", i, lipgloss.Width(got), lipgloss.Width(want))
		}
		// The visible text has to survive intact: the old failure cut it away
		// entirely, leaving "\x1b[38;2;166;2…" where the value should be.
		if visible(got) != visible(want) {
			t.Errorf("row %d reads %q coloured and %q plain", i, visible(got), visible(want))
		}
	}
}

// A cell narrower than its content is still truncated — on the visible text,
// not on the bytes — so the marker is what the user sees and the colour is
// intact around it.
func TestANarrowColumnTruncatesTheTextAndNotTheEscape(t *testing.T) {
	withTrueColor(t)

	cfg := colouredConfig()
	cfg.Columns[2].MinWidth = 5 // "running" does not fit
	m := New(cfg)
	m.Resize(60, 10)
	m.SetItems(fixtures())

	// The second row is not under the cursor, so it carries the column colour.
	got := visible(rowLines(&m)[1])
	if !strings.Contains(got, truncationMarker) {
		t.Errorf("row = %q, want the state truncated with %q", got, truncationMarker)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("row = %q, an escape survived into the visible text", got)
	}
}

// ── The selected row ─────────────────────────────────────────────────────────

// The plain "normal" selection (§3.72) is the opposite of the state-coloured
// case below: it keeps each column's own colour rather than dropping it,
// which is the whole point of the experiment — see cellStyle's doc comment
// for why that is safe here and not in the state-coloured case.
func TestTheDefaultSelectionKeepsTheColumnColours(t *testing.T) {
	withTrueColor(t)

	coloured := colouredTable(t) // no SelectedStyles — the plain, default path

	selected := rowLines(&coloured)[0] // "api", running, under the cursor
	if !strings.Contains(selected, foreground(theme.ColorOK)) {
		t.Errorf("the selected row lost the State column's colour: %q", selected)
	}
	if !strings.Contains(selected, background(theme.ColorTableLineSelected)) {
		t.Errorf("the selected row does not carry the new selection background: %q", selected)
	}
}

// A view that has coloured the row whole — error, busy, a CVE severity, via
// SelectedStyles — still drops per-cell colours: the row is handed to
// styles.Selected, and a colour inside it would close with a reset that takes
// that solid background with it for the rest of the line.
func TestAStateColouredSelectionIgnoresTheColumnColours(t *testing.T) {
	withTrueColor(t)

	plain := errorStyledTable(t, testConfig)        // no column Style
	coloured := errorStyledTable(t, colouredConfig) // the State column is coloured

	if got, want := rowLines(&coloured)[0], rowLines(&plain)[0]; got != want {
		t.Errorf("the selected row renders as\n  %q\nwant it identical to the uncoloured table\n  %q", got, want)
	}
}

// The consequence, stated as its own property: whatever the columns ask for,
// a state-coloured selection still closes its styling once, at the end.
func TestAStateColouredSelectionKeepsItsHighlightToTheEnd(t *testing.T) {
	withTrueColor(t)
	m := errorStyledTable(t, colouredConfig)

	selected := strings.TrimSuffix(rowLines(&m)[0], "\x1b[0m")
	if body, _, early := strings.Cut(selected, "\x1b[0m"); early {
		t.Errorf("the selected row resets its styling after %q, losing the highlight for the rest of the line", visible(body))
	}
}

// errorStyledTable forces the row under the cursor into the "error" selection
// state via SelectedStyles, the way a view derives it for an exited container
// or a CRITICAL finding — never by colouring a column.
func errorStyledTable(t *testing.T, baseConfig func() Config[row]) Model[row] {
	t.Helper()
	cfg := baseConfig()
	cfg.SelectedStyles = func(row) table.Styles { return theme.TableStylesForState("error") }
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())
	return m
}

// ── The rest of the line ─────────────────────────────────────────────────────

// lipgloss does not inherit a background (Rule 115), and the app's viewport
// style only reaches cells that emit nothing of their own. One coloured cell
// would otherwise end its line with a reset and strip the background from
// everything to its right — so every cell on an unselected row opens its own
// styling, coloured column or not.
func TestEveryCellOnAnUnselectedRowOpensItsOwnStyling(t *testing.T) {
	withTrueColor(t)
	m := colouredTable(t)

	unselected := rowLines(&m)[1] // row 0 is under the cursor
	segments := strings.Split(unselected, "\x1b[0m")

	if tail := segments[len(segments)-1]; tail != "" {
		t.Errorf("%q trails the last reset, so it renders with no background", tail)
	}
	// Every run between two resets, padding included — lipgloss emits the cell
	// padding as runs of its own — has to re-open the background, or the line
	// shows the terminal's own wherever one did not.
	for i, segment := range segments[:len(segments)-1] {
		if !strings.HasPrefix(segment, "\x1b[") {
			t.Errorf("run %d opens with %q rather than a styling sequence", i, visible(segment))
			continue
		}
		if !strings.Contains(segment, background(theme.ColorBackground)) {
			t.Errorf("run %d (%q) carries no background", i, visible(segment))
		}
	}
}

// The colour has to be the one the column asked for, or the mechanism is
// decorative. Two rows differing only in state must not render alike.
func TestTheColumnColourReachesTheCell(t *testing.T) {
	withTrueColor(t)
	m := colouredTable(t)

	// cache is exited and web is running; neither is under the cursor.
	exited, running := rowLines(&m)[1], rowLines(&m)[2]
	if !strings.Contains(exited, foreground(theme.ColorError)) {
		t.Errorf("the exited row does not carry the error colour: %q", exited)
	}
	if !strings.Contains(running, foreground(theme.ColorOK)) {
		t.Errorf("the running row does not carry the ok colour: %q", running)
	}
}

// foreground and background return the SGR parameters lipgloss emits for a
// colour, without the escape or the terminating "m" — a cell carrying both has
// them merged into one sequence, so neither full sequence appears on its own.
func foreground(c lipgloss.Color) string { return sgrParams(lipgloss.NewStyle().Foreground(c)) }
func background(c lipgloss.Color) string { return sgrParams(lipgloss.NewStyle().Background(c)) }

func sgrParams(style lipgloss.Style) string {
	opening, _, _ := strings.Cut(style.Render(" "), "m")
	return strings.TrimPrefix(opening, "\x1b[")
}

// A column that declares only a foreground still gets the app background, so a
// view cannot half-implement Rule 115 and leave a band of terminal background
// across its table.
func TestAColumnThatDeclaresNoBackgroundGetsTheAppOne(t *testing.T) {
	withTrueColor(t)
	m := colouredTable(t)

	coloured := rowLines(&m)[1]
	if !strings.Contains(coloured, foreground(theme.ColorError)) {
		t.Fatalf("the row is not the coloured one: %q", coloured)
	}
	if !strings.Contains(coloured, background(theme.ColorBackground)) {
		t.Errorf("the coloured row carries no app background: %q", coloured)
	}
}

// ── Geometry ─────────────────────────────────────────────────────────────────

// The table occupies exactly the height it was given: one header and Height()
// rows, padded when the list is shorter. The app's layout arithmetic (Rule 124)
// is written against that, and nothing re-measures it.
func TestTheViewIsOneHeaderPlusItsHeightInRows(t *testing.T) {
	m := loaded(t)

	for _, height := range []int{4, 10, 30} {
		m.Resize(120, height)
		if got, want := len(strings.Split(m.View(), "\n")), m.Table().Height()+1; got != want {
			t.Errorf("at height %d the view is %d lines, want %d", height, got, want)
		}
	}
}

// Rule 116 at the level it is actually seen: every rendered line spans the same
// width, so the viewport border closes on a rectangle.
func TestEveryRenderedLineSpansTheSameWidth(t *testing.T) {
	withTrueColor(t)
	m := colouredTable(t)
	m.Resize(100, 6)

	lines := strings.Split(m.View(), "\n")
	want := lipgloss.Width(lines[0])
	if want != 100-borderWidth {
		t.Errorf("the header spans %d cells, want %d", want, 100-borderWidth)
	}
	for i, line := range lines[1:] {
		if got := lipgloss.Width(line); got != want {
			t.Errorf("row %d spans %d cells, want %d", i, got, want)
		}
	}
}

// ── The scroll window ────────────────────────────────────────────────────────

func manyRows(n int) []row {
	out := make([]row, n)
	for i := range out {
		out[i] = row{Name: strings.Repeat("x", 1+i%9), Size: i % 5, State: "running"}
	}
	return out
}

// The window moves the least it can: it follows the cursor off its bottom edge
// one row at a time rather than recentring, which is what makes a long list
// readable while holding ↓.
func TestTheWindowFollowsTheCursorOffItsEdge(t *testing.T) {
	m := New(testConfig())
	m.Resize(120, 6)
	m.SetItems(manyRows(40))

	for i := 0; i < m.Table().Height(); i++ {
		if m.Offset() != 0 {
			t.Fatalf("the window moved to %d while the cursor was still on screen", m.Offset())
		}
		m.Update(testutil.Key("down"))
	}
	if got := m.Offset(); got != 1 {
		t.Errorf("offset = %d after stepping one row past the edge, want 1", got)
	}
}

// The cursor is never outside the window, whatever route it took there. This is
// the invariant; the one above is how it feels.
func TestTheCursorIsAlwaysInsideTheWindow(t *testing.T) {
	m := New(testConfig())
	m.Resize(120, 6)
	m.SetItems(manyRows(40))

	for _, key := range []string{"G", "g", "pgdown", "pgdown", "up", "pgup"} {
		m.Update(testutil.Key(key))
		cursor, height := m.Cursor(), m.Table().Height()
		if cursor < m.Offset() || cursor >= m.Offset()+height {
			t.Errorf("after %q the cursor is at %d, outside the window [%d, %d)",
				key, cursor, m.Offset(), m.Offset()+height)
		}
	}
}

// A reload that leaves the cursor on screen leaves the scroll position alone —
// the property SetItems was written to protect, now that the offset is this
// package's to lose.
func TestAReloadKeepsTheScrollPosition(t *testing.T) {
	m := New(testConfig())
	m.Resize(120, 6)
	m.SetItems(manyRows(40))
	m.Update(testutil.Key("end"))

	before := m.Offset()
	if before == 0 {
		t.Fatal("the fixture does not scroll, so this proves nothing")
	}
	m.SetItems(manyRows(40))

	if got := m.Offset(); got != before {
		t.Errorf("offset = %d after a reload, want it left at %d", got, before)
	}
}

// A list that shrinks under the window pulls it back, rather than leaving it
// scrolled past the end where every row would be blank.
func TestAShorterListPullsTheWindowBack(t *testing.T) {
	m := New(testConfig())
	m.Resize(120, 6)
	m.SetItems(manyRows(40))
	m.Update(testutil.Key("end"))

	m.SetItems(manyRows(3))

	if got := m.Offset(); got != 0 {
		t.Errorf("offset = %d with three rows and room for five, want 0", got)
	}
	if _, ok := m.Selected(); !ok {
		t.Error("nothing is selected after the list shrank")
	}
}

// runewidth is what measures the cells, and it is only safe because Cell is
// plain. Stated as a test so the reason survives the next person to read
// Column: a styled string of seven visible cells measures 28, and a column of
// twelve would truncate it.
func TestRunewidthMisreadsStyledTextWhichIsWhyCellIsPlain(t *testing.T) {
	withTrueColor(t)

	styled := lipgloss.NewStyle().Foreground(theme.ColorOK).Render("running")
	if runewidth.StringWidth(styled) <= lipgloss.Width(styled) {
		t.Fatal("the renderer emitted no escape sequence, so this proves nothing")
	}
	if runewidth.Truncate(styled, 12, truncationMarker) == styled {
		t.Fatal("runewidth no longer over-measures escape sequences — Cell could take a styled value")
	}
}
