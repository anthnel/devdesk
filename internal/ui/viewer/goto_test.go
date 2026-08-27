package viewer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// longLog is taller than any pane a test opens, so a jump has somewhere to go.
func longLog(t *testing.T) Model {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		b.WriteString("line\n")
	}
	return logModel(t, b.String())
}

func goTo(t *testing.T, m Model, number string) (Model, tea.Cmd) {
	t.Helper()
	m = feed(t, m, testutil.Key("g"))
	if !m.gotoActive {
		t.Fatal("g did not open the go-to-line prompt")
	}
	m = feed(t, m, testutil.Type(number)...)
	return step(t, m, testutil.Key("enter"))
}

func TestGoToLineScrollsThere(t *testing.T) {
	m, _ := goTo(t, longLog(t), "120")

	if m.gotoActive {
		t.Error("the prompt stayed open after enter")
	}
	if got := m.textViewport.YOffset; got != 119 {
		t.Errorf("YOffset = %d after going to line 120, want 119 — the line belongs at the top", got)
	}
}

// The prompt is a mode: it claims every key before the pane sees one. Without
// that, a digit would scroll as well as being typed, and `esc` would leave the
// viewer with the field still focused.
func TestThePromptOwnsTheKeyboard(t *testing.T) {
	m := feed(t, longLog(t), testutil.Key("g"))

	if !m.InEditMode() {
		t.Error("InEditMode() is false with the prompt open, so ctrl+p's `:` would reach the router")
	}
	if !m.FilterBarVisible() {
		t.Error("FilterBarVisible() is false with the prompt open, so the viewport border stays open under it")
	}

	m = feed(t, m, testutil.Type("12")...)
	if m.textViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after typing into the prompt, want 0 — a digit must not also scroll", m.textViewport.YOffset)
	}
	if got := m.gotoInput.Value(); got != "12" {
		t.Errorf("the field holds %q, want %q", got, "12")
	}
}

// The footer stands at the same height whether the prompt opened over an active
// search or over nothing — the two share one slot, so the pane never resizes
// under the reader.
func TestThePromptTakesTheFilterBarsSlot(t *testing.T) {
	plain := longLog(t)
	searching := searchFor(t, longLog(t), "line")

	if got, want := feed(t, plain, testutil.Key("g")).GetFooterHeight(), searching.GetFooterHeight(); got != want {
		t.Errorf("footer height = %d with the prompt open and %d with a search; they share the slot", got, want)
	}

	withBoth := feed(t, searching, testutil.Key("g"))
	if got, want := withBoth.GetFooterHeight(), searching.GetFooterHeight(); got != want {
		t.Errorf("footer height = %d with the prompt over a search, want %d", got, want)
	}
	footer := ansi.Strip(withBoth.RenderFooter(80))
	if !strings.Contains(footer, "Go to line") {
		t.Errorf("the prompt is not in the footer: %q", footer)
	}
	if strings.Contains(footer, "/ line") {
		t.Errorf("the filter bar is drawn under the prompt; one slot, one occupant: %q", footer)
	}
}

func TestEscapeClosesThePromptAndChangesNothing(t *testing.T) {
	m := feed(t, longLog(t), testutil.Key("end"))
	atBottom := m.textViewport.YOffset

	m = feed(t, m, testutil.Key("g"))
	m = feed(t, m, testutil.Type("1")...)
	m = feed(t, m, testutil.Key("esc"))

	if m.gotoActive {
		t.Error("esc left the prompt open")
	}
	if m.textViewport.YOffset != atBottom {
		t.Errorf("YOffset = %d after esc, want %d — a cancelled prompt moves nothing", m.textViewport.YOffset, atBottom)
	}
	if m.OriginView != "" {
		t.Error("esc reached the viewer's own handler; the prompt must consume it")
	}
}

// An empty prompt is a change of mind, not a mistake: nothing is said and
// nothing moves.
func TestAnEmptyPromptIsSilent(t *testing.T) {
	m, cmd := goTo(t, longLog(t), "")

	if cmd != nil {
		t.Error("an empty prompt reported something")
	}
	if m.textViewport.YOffset != 0 {
		t.Error("an empty prompt scrolled the pane")
	}
}

// The two refusals are named. A key that is advertised and does nothing is the
// defect this view has had to fix twice already (Rule 130).
func TestALineBeyondTheDocumentIsRefused(t *testing.T) {
	m, cmd := goTo(t, longLog(t), "9999")

	if cmd == nil {
		t.Fatal("a number past the end of the document was refused in silence")
	}
	if m.textViewport.YOffset != 0 {
		t.Error("a refused jump moved the pane anyway")
	}
	if got := footerText(t, m, cmd); !strings.Contains(got, "200 lines") {
		t.Errorf("the refusal reads %q, want it to name the document's length", got)
	}
}

func TestALineHiddenByTheFilterIsRefusedByName(t *testing.T) {
	m := searchFor(t, logModel(t, "alpha\nbeta\ngamma"), "alpha")

	m, cmd := goTo(t, m, "3")
	if cmd == nil {
		t.Fatal("a line hidden by the search was refused in silence")
	}
	if m.textViewport.YOffset != 0 {
		t.Error("the pane jumped to a line that is not on screen")
	}
	if got := footerText(t, m, cmd); !strings.Contains(got, "Line 3 is hidden") {
		t.Errorf("the refusal reads %q, want it to name the line and the filter", got)
	}
}

// A jump lands on the first row of a wrapped line, never inside one.
func TestAJumpLandsOnAWrappedLinesFirstRow(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(strings.Repeat("x", 100) + "\n")
	}
	m := logModel(t, b.String())
	m = feed(t, m, tea.WindowSizeMsg{Width: 40, Height: 20}, testutil.Key("w"))

	m, _ = goTo(t, m, "3")

	// Two source lines wrapped into three rows each at this width, so line 3
	// starts at row 6 — not at row 2, which is what an unwrapped count would say.
	if got, want := m.textViewport.YOffset, m.rowOfLine[3]; got != want {
		t.Errorf("YOffset = %d, want %d — the jump must use the rendered row", got, want)
	}
	if m.rowOfLine[3] <= 2 {
		t.Errorf("line 3 is at row %d, so the wrap was not counted", m.rowOfLine[3])
	}
}

// ── The case toggle ──────────────────────────────────────────────────────────

func TestTheSearchIgnoresCaseUntilSAsksItNotTo(t *testing.T) {
	m := searchFor(t, logModel(t, "ERROR here\nerror there\nnothing"), "error")

	if m.matchedLines != 2 {
		t.Fatalf("the search kept %d lines, want both spellings", m.matchedLines)
	}

	// The toggle re-filters the query already in force: comparing the two
	// readings is the whole point, and retyping would make it a chore.
	m = feed(t, m, testutil.Key("s"))
	if !m.caseSensitive {
		t.Fatal("s did not turn the case on")
	}
	if m.matchedLines != 1 {
		t.Errorf("the sensitive search kept %d lines, want only the lowercase one", m.matchedLines)
	}

	m = feed(t, m, testutil.Key("s"))
	if m.matchedLines != 2 {
		t.Errorf("turning the case back off kept %d lines, want both again", m.matchedLines)
	}
}

// Rule 136: the bar shows what is filtering, and shows nothing when nothing is.
func TestTheCaseTokenShowsOnlyWhileItIsOn(t *testing.T) {
	m := logModel(t, "ERROR here\nerror there")

	if m.bar.IsTokenActive(caseToken) {
		t.Error("the case token is active on a document nobody has touched")
	}

	m = feed(t, m, testutil.Key("s"))
	if !m.bar.IsTokenActive(caseToken) {
		t.Error("s left the bar silent about a filter it turned on")
	}

	m = feed(t, m, testutil.Key("s"))
	if m.bar.IsTokenActive(caseToken) {
		t.Error("the token outlived the filter")
	}
}

// footerText is the message the model is already carrying.
//
// The Cmd is deliberately not run: it is the expiry timer, tea.Tick blocks for
// its whole three seconds, and the message itself was set in Update (Rule 128).
// Every caller asserts the Cmd is non-nil separately — a message posted without
// its timer never clears.
func footerText(t *testing.T, m Model, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("the footer message came with no expiry timer, so it would never clear")
	}
	return ansi.Strip(m.footer.View(80, components.Status{}))
}
