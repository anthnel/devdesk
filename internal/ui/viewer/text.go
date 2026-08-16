package viewer

import (
	"strings"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/viewer"
)

// rebuildText renders the text pane and hands it to the viewport.
//
// It is called when the document loads, when `c`, `w` or `v` changes, when the
// search query changes and when the width changes — and at no other time.
// View() must stay read-only (Rule 110), and re-rendering five megabytes per
// frame would not be viable even if it did not.
func (m *Model) rebuildText() {
	width := m.textViewport.Width
	query := strings.ToLower(strings.TrimSpace(m.bar.SearchQuery()))
	isLog := m.doc.Kind == viewer.KindLog

	var out []string
	m.matchedLines = 0

	for _, line := range m.lines {
		if isLog && !line.Level.Passes(m.minLevel) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(line.Plain), query) {
			continue
		}
		m.matchedLines++

		segments := [][]viewer.Token{line.Tokens}
		if m.wrap && width > 0 {
			segments = wrapTokens(line.Tokens, width)
		}
		for _, segment := range segments {
			// Padded to the full width: lipgloss inherits no background
			// (Rule 115), so a line shorter than the pane would show the
			// terminal's own from its last character to the right margin.
			out = append(out, theme.PadWithBg(renderSegment(segment, line.Level, isLog), width))
		}
	}

	if len(out) == 0 {
		out = append(out, theme.PadWithBg(theme.DimStyle.Render(m.emptyTextMessage()), width))
	}

	m.textViewport.SetContent(strings.Join(out, "\n"))
}

// emptyTextMessage says why the pane is empty, which is never obvious: an empty
// document, a filter that matched nothing and a search that matched nothing all
// look identical, and only one of them is the user's own doing.
func (m *Model) emptyTextMessage() string {
	switch {
	case m.bar.SearchQuery() != "":
		return "No line matches " + m.bar.SearchQuery()
	case m.minLevel != viewer.LevelUnknown:
		return "No line at " + minLevelLabel(m.minLevel) + " or above"
	default:
		return "Empty document"
	}
}

// minLevelLabel is how a verbosity filter reads, in the footer token and in the
// header alike — one spelling, so the two cannot drift.
func minLevelLabel(level viewer.Level) string {
	if level == viewer.LevelUnknown {
		return "all"
	}
	return "≥ " + level.String()
}

// cycleVerbosity advances the minimum level: all, then each level in turn, then
// back to all.
//
// One key rather than four toggles, because a minimum is what "verbosity" means
// and log levels are monotone — nobody wants warn but not error. The bar shows
// it as a single token, and disappears entirely at `all` (Rule 136: no filter,
// no bar).
func (m *Model) cycleVerbosity() {
	next := viewer.LevelUnknown
	switch m.minLevel {
	case viewer.LevelUnknown:
		next = viewer.Levels[0]
	default:
		for i, level := range viewer.Levels {
			if level == m.minLevel && i+1 < len(viewer.Levels) {
				next = viewer.Levels[i+1]
			}
		}
	}
	m.minLevel = next
	m.syncVerbosityToken()
	m.rebuildText()
}

// syncVerbosityToken keeps the filter bar's token showing the level in force.
//
// The label carries the value, so SetTokens is given the active flag explicitly:
// it preserves an active state by matching labels, and "≥ warn" is not the label
// "≥ info" was.
func (m *Model) syncVerbosityToken() {
	if m.doc.Kind != viewer.KindLog || m.minLevel == viewer.LevelUnknown {
		m.bar.SetTokens(nil)
		return
	}
	m.bar.SetTokens([]components.FilterToken{{Label: minLevelLabel(m.minLevel), Active: true}})
}
