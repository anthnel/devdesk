package agentusageview

import (
	"path/filepath"
	"strconv"

	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// baseFooterHeight is the blank line and the message line (Rule 124).
const baseFooterHeight = 2

// View is the table, whatever it holds (Rule 139).
func (m Model) View() string {
	if m.level == levelAgents {
		return m.agentTable.View()
	}
	return m.table.View()
}

// GetTitle names the view, and the project of the open session at the agents
// level (its short ID when the transcript named no project).
func (m Model) GetTitle() string {
	title := theme.IconAgentUsage + " Agent usage"
	if m.level != levelAgents {
		return title
	}
	leaf := shortID(m.open.ID)
	if m.open.Project != "" {
		leaf = filepath.Base(m.open.Project)
	}
	return title + " › " + leaf
}

// shortID is the start of a session ID, as the Session column shows it.
func shortID(id string) string {
	r := []rune(id)
	return string(r[:min(sessionIDPrefix, len(r))])
}

// GetIcon returns nothing: the title carries the icon.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo counts the sessions on screen, and the ones not listed when
// there are some.
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	if m.level == levelAgents {
		return m.agentsHeaderInfo()
	}
	info := []shortcut.HeaderInfo{
		{Key: "Sessions", Value: strconv.Itoa(len(m.table.Visible())), Style: theme.HeaderValueStyle},
	}
	if m.omitted > 0 {
		info = append(info, shortcut.HeaderInfo{Key: "Skipped", Value: strconv.Itoa(m.omitted), Style: theme.HeaderValueStyle})
	}
	return info
}

// agentsHeaderInfo counts the agents on screen and their tokens, both following
// the filter.
func (m Model) agentsHeaderInfo() []shortcut.HeaderInfo {
	rows := m.agentTable.Visible()
	var total int64
	for _, r := range rows {
		total += r.Tokens.Total()
	}
	return []shortcut.HeaderInfo{
		{Key: "Agents", Value: strconv.Itoa(len(rows)), Style: theme.HeaderValueStyle},
		{Key: "Tokens", Value: formatTokens(total), Style: theme.HeaderValueStyle},
	}
}

// GetShortcuts returns the same keys at both levels and in every state; what
// varies is Disabled, from the one availability the handlers also read
// (Rule 130).
func (m Model) GetShortcuts() shortcut.Shortcuts {
	a := m.availability()
	return shortcut.Shortcuts{
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "→", Description: "Open agents", Disabled: !a.Open.Enabled()},
		{Key: "esc", Description: "Go back", Disabled: !a.Close.Enabled()},
		{Key: "/", Description: "Filter"},
		{Key: "?", Description: "Help"},
	}
}

// GetFooterHeight budgets the footer (Rules 124, 136).
func (m Model) GetFooterHeight() int {
	h := baseFooterHeight + m.activeFilterBar().ExtraHeight()
	if m.level == levelAgents {
		h++ // the breadcrumb row
	}
	return h
}

// RenderFooter renders the breadcrumb at the agents level, the filter bar when
// visible, a blank line, and the message line.
func (m Model) RenderFooter(width int) string {
	line := theme.EmptyLineBg(width) + "\n" + m.footer.View(width, m.status())
	if bar := m.activeFilterBar(); bar.IsVisible() {
		line = bar.View() + "\n" + line
	}
	if m.level == levelAgents {
		line = m.breadcrumb(width) + "\n" + line
	}
	return line
}

// breadcrumb is Sessions › project › ID, the last one active and its parents
// dim, with the one cell of left padding Rule 123 asks for.
func (m Model) breadcrumb(width int) string {
	project := noProject
	if m.open.Project != "" {
		project = theme.ShortPath(m.open.Project, max(width/2, 1))
	}
	base := lipgloss.NewStyle().Background(theme.ColorBackground)
	parents := base.Foreground(theme.ColorDim).Render("Sessions › " + project + " › ")
	active := base.Foreground(theme.ColorSecondary).Bold(true).Render(shortID(m.open.ID))
	return theme.PadWithBg(theme.Bg(" ")+parents+active, width)
}

// status is derived on every frame, with no timer (Rule 128).
//
// The empty sentence needs the listing to be empty in total, not after the
// filter: a filter with no match is not a machine with no sessions.
func (m Model) status() sharedcomponents.Status {
	if m.level == levelAgents {
		return m.agentsStatus()
	}
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

// agentsStatus is the footer state of the agents level.
func (m Model) agentsStatus() sharedcomponents.Status {
	switch {
	case m.agentsErr:
		return sharedcomponents.Status{
			Text:  "Failed to read this session — check logs",
			Level: sharedcomponents.LevelError,
		}
	case m.agentsLoading:
		return sharedcomponents.Status{Text: "Reading session...", Spinner: true}
	case m.usage.Diagnostics.Degraded():
		return sharedcomponents.Status{
			Text:  "Totals are approximate — some transcript data could not be read",
			Level: sharedcomponents.LevelWarning,
		}
	}
	return sharedcomponents.Status{}
}
