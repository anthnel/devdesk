package agentusageview

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/agentusage/pricing"
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
	estimates := make([]pricing.Estimate, len(rows))
	for i, r := range rows {
		total += r.Tokens.Total()
		estimates[i] = r.cost
	}
	info := []shortcut.HeaderInfo{
		{Key: "Agents", Value: strconv.Itoa(len(rows)), Style: theme.HeaderValueStyle},
		{Key: "Tokens", Value: formatTokens(total), Style: theme.HeaderValueStyle},
		{Key: "Est. cost", Value: estCostValue(pricing.Sum(estimates)), Style: theme.HeaderValueStyle},
	}
	info = append(info, m.billedHeaderInfo()...)
	info = append(info, shortcut.HeaderInfo{Key: "Updates", Value: m.updatesState(), Style: theme.HeaderValueStyle})
	if n := m.idleCount(); n > 0 {
		info = append(info, shortcut.HeaderInfo{Key: "Idle", Value: strconv.Itoa(n), Style: theme.HeaderValueStyle})
	}
	return info
}

// negativeUSD is half a cent: an Unattributed amount at or below its negative
// shows as -$0.01 or lower, so only then is the price table "above the bill".
// Float noise (-1e-12) renders $0.00 and must not look stale.
const negativeUSD = 0.005

// billedHeaderInfo is Billed and Unattributed, both facts of the whole session
// that ignore the filter. Neither shows while Claude Code has not recorded a
// bill. Unattributed is a dash when an agent has no price, and a negative one
// (the price table is above what was billed) is orange.
func (m Model) billedHeaderInfo() []shortcut.HeaderInfo {
	if !m.cost.Billed.Known {
		return nil
	}
	unattributed := shortcut.HeaderInfo{Key: "Unattributed", Value: noValue, Style: theme.HeaderValueStyle}
	if m.cost.UnattributedKnown {
		unattributed.Value = formatUSD(m.cost.Unattributed)
		if m.cost.Unattributed <= -negativeUSD {
			unattributed.Style = theme.HeaderWarnValueStyle()
		}
	}
	return []shortcut.HeaderInfo{
		{Key: "Billed", Value: formatUSD(m.cost.Billed.USD), Style: theme.HeaderValueStyle},
		unattributed,
	}
}

// estCostValue is the Est. cost header value: an estimate, or a dash when the
// sum is unknown or there is nothing to price.
func estCostValue(e pricing.Estimate) string {
	if e.Status != pricing.Priced {
		return noValue
	}
	return estimateMark + formatUSD(e.USD)
}

// idleCount counts the idle agents over every row, not just the visible ones:
// an alert must not hide behind a filter.
func (m Model) idleCount() int {
	n := 0
	for _, r := range m.agentTable.Items() {
		if r.state == stateIdle {
			n++
		}
	}
	return n
}

// updatesState says whether the open session is polled: live while the chain
// runs, paused once it stopped (settled session, deleted transcript).
func (m Model) updatesState() string {
	if m.level == levelAgents && m.agg != nil && !m.pollStopped {
		return "live"
	}
	return "paused"
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
		{Key: ".", Description: "Sort"},
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
	return m.priceHint()
}

// priceHint is the footer state about missing prices: where to set them when
// the table is empty, which model lacks one otherwise. It is a state derived on
// every frame, not a message (Rule 128).
func (m Model) priceHint() sharedcomponents.Status {
	if m.prices.Len() == 0 {
		if m.usage.Total().Total() == 0 {
			return sharedcomponents.Status{}
		}
		return sharedcomponents.Status{Text: "Set agent_usage.prices in this context's config file to estimate costs — see ?"}
	}
	switch n := len(m.cost.Unpriced); n {
	case 0:
		return sharedcomponents.Status{}
	case 1:
		return sharedcomponents.Status{Text: "No price for " + m.cost.Unpriced[0] + " in agent_usage.prices — its cost shows -"}
	default:
		return sharedcomponents.Status{Text: fmt.Sprintf(
			"No price for %s and %d more models in agent_usage.prices — their cost shows -", m.cost.Unpriced[0], n-1)}
	}
}
