package security

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-750 (step 5): the details footer says which lines are on screen. It is
// a Status, derived at render from the viewport: a message would expire after
// three seconds (Rule 128), and the height stays two lines (Rule 124).

func footerText(m Model, width int) string { return ansi.Strip(m.RenderFooter(width)) }

func TestTheFooterSaysWhichLinesOfALongDetailAreOnScreen(t *testing.T) {
	m := openDetail(t, renderFailureFixture(), 80, 12)
	total := m.detailsViewport.TotalLineCount()
	if total <= 12 {
		t.Fatalf("the fixture has %d lines, it must overflow 12", total)
	}

	steps := []struct {
		name string
		keys []string
		want string
	}{
		{"on opening", nil, fmt.Sprintf("Lines 1-12 of %d", total)},
		{"one line down", []string{"down"}, fmt.Sprintf("Lines 2-13 of %d", total)},
		{"at the end", []string{"end"}, fmt.Sprintf("Lines %d-%d of %d", total-11, total, total)},
		{"back at the top", []string{"end", "home"}, fmt.Sprintf("Lines 1-12 of %d", total)},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			got := footerText(feed(t, m, testutil.Keys(step.keys...)...), 80)
			if !strings.Contains(got, step.want) {
				t.Errorf("footer = %q, want it to contain %q", got, step.want)
			}
		})
	}
}

// Rule 128: a state of the screen, not an event. It is in the status, and no
// message was set, so no timer can take it away.
func TestTheScrollPositionIsAStatusNotAMessage(t *testing.T) {
	m := openDetail(t, renderFailureFixture(), 80, 12)
	want := fmt.Sprintf("Lines 1-12 of %d", m.detailsViewport.TotalLineCount())

	if got := m.status().Text; !strings.Contains(got, want) {
		t.Errorf("status().Text = %q, want %q", got, want)
	}
	if m.footer.Text() != "" {
		t.Errorf("a footer message was set: %q", m.footer.Text())
	}
	expired := feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if got := footerText(expired, 80); !strings.Contains(got, want) {
		t.Errorf("the position went with the footer's expiry: %q", got)
	}
}

func TestAShortDetailShowsNoPosition(t *testing.T) {
	m := detailsModel(t)
	if m.detailsViewport.TotalLineCount() > m.detailsViewport.Height() {
		t.Skip("the fixture overflows, it is not the short case")
	}

	if got := footerText(m, 160); strings.Contains(got, "Lines") {
		t.Errorf("a detail that fits says where it is: %q", got)
	}
	if got := m.status().Text; got != "" {
		t.Errorf("status().Text = %q, want nothing when everything fits", got)
	}
}

// Rule 124: the router sizes the viewport from this, so it cannot change with
// the scroll position.
func TestTheFooterStaysTwoLinesWhateverTheDetail(t *testing.T) {
	for name, m := range map[string]Model{
		"long":  openDetail(t, renderFailureFixture(), 80, 12),
		"short": detailsModel(t),
	} {
		t.Run(name, func(t *testing.T) {
			if got := m.GetFooterHeight(); got != 2 {
				t.Errorf("GetFooterHeight() = %d, want 2", got)
			}
			if lines := strings.Count(m.RenderFooter(80), "\n") + 1; lines != 2 {
				t.Errorf("RenderFooter() emitted %d lines, want 2", lines)
			}
		})
	}
}

// Rule 128: a warning the user has not read takes priority over a state.
func TestAFooterWarningWinsOverTheScrollPosition(t *testing.T) {
	m := openDetail(t, renderFailureFixture(), 80, 12)

	m = feed(t, m, testutil.Key(keymap.Web))

	got := footerText(m, 80)
	if !strings.Contains(got, "No references available") {
		t.Fatalf("the warning is not shown: %q", got)
	}
	if strings.Contains(got, "Lines") {
		t.Errorf("the position is shown beside the warning: %q", got)
	}
}
