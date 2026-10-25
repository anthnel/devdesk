package agentusageview

import (
	"strconv"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// baseFooterHeight is the blank line and the message line (Rule 124).
const baseFooterHeight = 2

// View is the table, whatever it holds (Rule 139).
func (m Model) View() string { return m.table.View() }

// GetTitle names the view.
func (m Model) GetTitle() string { return theme.IconAgentUsage + " Agent usage" }

// GetIcon returns nothing: the title carries the icon.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo counts the sessions on screen, and the ones not listed when
// there are some.
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	info := []shortcut.HeaderInfo{
		{Key: "Sessions", Value: strconv.Itoa(len(m.table.Visible())), Style: theme.HeaderValueStyle},
	}
	if m.omitted > 0 {
		info = append(info, shortcut.HeaderInfo{Key: "Skipped", Value: strconv.Itoa(m.omitted), Style: theme.HeaderValueStyle})
	}
	return info
}

// GetShortcuts returns the same keys in every state, none greyed: each one
// always acts here (Rule 130).
func (m Model) GetShortcuts() shortcut.Shortcuts {
	return shortcut.Shortcuts{
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "/", Description: "Filter"},
		{Key: "?", Description: "Help"},
	}
}

// GetFooterHeight budgets the footer (Rules 124, 136).
func (m Model) GetFooterHeight() int {
	return baseFooterHeight + m.table.FilterBar().ExtraHeight()
}

// RenderFooter renders the filter bar when visible, a blank line, and the
// message line.
func (m Model) RenderFooter(width int) string {
	line := theme.EmptyLineBg(width) + "\n" + m.footer.View(width, m.status())
	if bar := m.table.FilterBar(); bar.IsVisible() {
		return bar.View() + "\n" + line
	}
	return line
}

// status is derived on every frame, with no timer (Rule 128).
//
// The empty sentence needs the listing to be empty in total, not after the
// filter: a filter with no match is not a machine with no sessions.
func (m Model) status() sharedcomponents.Status {
	switch {
	case m.loadErr:
		return sharedcomponents.Status{
			Text:  "Failed to list Claude Code sessions — check logs",
			Level: sharedcomponents.LevelError,
		}
	case m.loading:
		return sharedcomponents.Status{Text: "Reading Claude Code sessions...", Spinner: true}
	case len(m.table.Items()) == 0:
		return sharedcomponents.Status{Text: "No Claude Code sessions found in " + theme.FoldHome(m.root)}
	}
	return sharedcomponents.Status{}
}
