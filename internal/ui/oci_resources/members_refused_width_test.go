package ociresources

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-510 (NX-07), review round 1: the Members column is the rightmost
// Optional one, so it is the first the table drops. Widening it for the refusal
// text pushed the width at which it shows from 100 columns (MinWidth 16, "12 ·
// 30 days ago") to 120, and every group lost the column in between, "never" and
// "N · age" included. The refusal text has to fit the column that was there.

// membersColumnVisibleFrom is the narrowest terminal width at which the Members
// column was shown before DEVDESK-510 (MinWidth 16).
const membersColumnVisibleFrom = 100

// refusedLongest is the widest refusal the cell can say: 99 members known and the
// longest relative age Rule 127 produces.
func refusedLongest(t *testing.T, width int) (Model, string) {
	t.Helper()
	longestAge := 29 * 24 * time.Hour // "29 days ago", the widest TimeAgo output
	entry := listing(99, 1)
	refusal := &cache.RegistryGroupRefusal{At: time.Now().Add(-longestAge), Status: 403}
	m := tabWith(t, testConfig(), entry, refusal)
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
	cell := m.registryTable.Table().Rows()[0][membersCol]
	if !strings.HasPrefix(cell, "denied") {
		t.Fatalf("Members cell = %q, want the refusal", cell)
	}
	return m, cell
}

func TestTheMembersColumnStaysVisibleWhereItWasBeforeTheRefusalText(t *testing.T) {
	for _, width := range []int{membersColumnVisibleFrom, membersColumnVisibleFrom + 10} {
		m, _ := refusedLongest(t, width)
		view := ansi.Strip(m.registryTable.View())

		if !strings.Contains(view, "Members") {
			t.Errorf("at width %d the Members column is dropped:\n%s", width, view)
		}
		// Squeezed to its floor the cell is cut at the end, so the state, which
		// comes first, still reads.
		if line := renderedRow(t, view, "prod"); !strings.Contains(line, "denied") {
			t.Errorf("at width %d the refused row no longer says denied:\n%s", width, line)
		}
	}
}

// membersTextFitsFrom is the width from which the longest refusal text is shown
// whole. At 100 the minimum widths of the columns add up to the whole interior
// (98 cells), so no room is left for more than the floor there: the column grows
// with its content as the terminal does.
const membersTextFitsFrom = 120

func TestTheLongestRefusalTextFitsWholeOnceThereIsRoom(t *testing.T) {
	for _, width := range []int{membersTextFitsFrom, 160} {
		m, cell := refusedLongest(t, width)
		line := ansi.Strip(renderedRow(t, m.registryTable.View(), "prod"))

		if !strings.Contains(line, cell) {
			t.Errorf("at width %d the longest refusal %q does not fit the column whole:\n%s", width, cell, line)
		}
	}
}

// The refusal must not move the threshold of any other group's cell: the column
// shows at the same width with "never" or "N · age" in it.
func TestTheRefusalTextDoesNotMoveTheMembersColumnThreshold(t *testing.T) {
	visible := func(m Model) bool { return strings.Contains(ansi.Strip(m.registryTable.View()), "Members") }

	refused, _ := refusedLongest(t, membersColumnVisibleFrom)
	plain := feed(t, tabWith(t, testConfig(), listing(99, 3), nil), tea.WindowSizeMsg{Width: membersColumnVisibleFrom, Height: 30})

	if !visible(plain) {
		t.Fatalf("baseline: the Members column is not visible at width %d with a plain cell", membersColumnVisibleFrom)
	}
	if !visible(refused) {
		t.Errorf("the Members column is visible at width %d with a plain cell, not with a refusal", membersColumnVisibleFrom)
	}
}

// A member declared with auth `inherit` is logged in through its group, as a
// discovered member is: its Logged cell is the group's, not the `-` of an
// unresolved mode.
func TestADeclaredMemberThatInheritsShowsItsGroupsLoginStatus(t *testing.T) {
	m := tabWith(t, declaredConfig(), nil, refusalAt(7))
	m = feed(t, m, testutil.Key("right"))
	if m.registryGroupSlug != "prod" {
		t.Fatalf("registryGroupSlug = %q, want the group entered", m.registryGroupSlug)
	}
	group := m.registries[0]
	want := m.loggedCell(group.AuthMode, group.URL)

	row, ok := findRow(m, "dhi")
	if !ok {
		t.Fatalf("the group level lacks the declared member dhi: %+v", m.registryTable.Items())
	}
	if want == "-" {
		t.Fatalf("fixture: the group's own Logged cell is %q, the test would prove nothing", want)
	}
	if row.logged != want {
		t.Errorf("declared member Logged = %q, want the group's %q", row.logged, want)
	}
}
