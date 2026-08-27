package viewer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// gotoDigitLimit is the widest line number the field accepts. A document is
// capped at 5 MiB, so nine digits is past any line count that can exist here and
// the limit is a guard against a paste, not a policy.
const gotoDigitLimit = 9

// digitsOnly refuses anything that is not a line number, in the field rather
// than after it.
//
// The refusal has to happen here: a submit that had to explain "42a is not a
// number" would be a second failure mode beside the two real ones — out of
// range, and hidden by the filter — and the only one of the three the user could
// not have been stopped from creating.
func digitsOnly(s string) error {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("not a digit: %q", r)
		}
	}
	return nil
}

// activateGoto is `g`. It opens the prompt over the filter bar's slot.
func (m *Model) activateGoto() tea.Cmd {
	m.gotoActive = true
	m.gotoInput.SetValue("")
	m.gotoInput.Focus()
	return textinput.Blink
}

// closeGoto is the one place the prompt is dismissed, so no path can leave the
// field focused with the flag clear, or the other way round.
func (m *Model) closeGoto() {
	m.gotoActive = false
	m.gotoInput.SetValue("")
	m.gotoInput.Blur()
}

// handleGotoKey owns the keyboard while the prompt is open.
//
// It takes every key before the pane sees one — the prompt is a mode, the way a
// confirmation modal is — so a digit typed into it cannot also scroll, and `esc`
// closes the prompt rather than leaving the viewer.
func (m Model) handleGotoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeGoto()
		return m, nil
	case "enter":
		return m.submitGoto()
	}
	var cmd tea.Cmd
	m.gotoInput, cmd = m.gotoInput.Update(msg)
	return m, cmd
}

// submitGoto scrolls to the line the user asked for, or says why it will not.
//
// Three outcomes, and the two refusals are named rather than swallowed
// (Rule 130): a key that is advertised and does nothing is the defect this view
// has had to fix twice elsewhere.
func (m Model) submitGoto() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.gotoInput.Value())
	m.closeGoto()

	// Nothing typed is not a mistake, it is a change of mind.
	if value == "" {
		return m, nil
	}

	num, err := strconv.Atoi(value)
	if err != nil || num < 1 || num > len(m.lines) {
		return m, m.footer.Warn(fmt.Sprintf("Document has %d lines", len(m.lines)))
	}

	// The line exists but is not on screen. Scrolling to the nearest visible one
	// would put a different number under the cursor while reporting success, so
	// the filter is named instead and nothing moves: the user can lift it, or
	// not.
	row, shown := m.rowOfLine[num]
	if !shown {
		return m, m.footer.Warn(fmt.Sprintf("Line %d is hidden by the filter", num))
	}

	m.textViewport.SetYOffset(row)
	return m, nil
}

// renderGotoBar draws the prompt in the filter bar's frame, so the rectangle
// that closes the viewport is the same one either way.
func (m Model) renderGotoBar(width int) string {
	inner := theme.Bg(" ") + theme.KeyStyle.Render("Go to line") +
		theme.Bg(" ") + theme.DimStyle.Render(theme.IconChevronRight) + theme.Bg(" ") +
		m.gotoInput.View() +
		theme.Bg("  ") + theme.DimStyle.Render(fmt.Sprintf("1-%d", len(m.lines)))
	return components.BarFrame(width, inner)
}
