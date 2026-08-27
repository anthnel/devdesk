package viewer

import (
	"fmt"
	"strconv"
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
	query := strings.TrimSpace(m.bar.SearchQuery())
	isLog := m.doc.Kind == viewer.KindLog

	// The gutter comes off the width before anything is wrapped. wrapTokens
	// counts runes and knows nothing about what is put in front of a segment, so
	// wrapping at the full width and prefixing afterwards would push every line
	// past the right margin, by exactly the gutter, on every row.
	gutter := m.gutterWidth()
	textWidth := max(width-gutter, 0)

	var out []string
	m.matchedLines = 0
	m.rowOfLine = make(map[int]int, len(m.lines))

	for _, line := range m.lines {
		if isLog && !line.Level.Passes(m.minLevel) {
			continue
		}

		// One rule decides both that a line matches and where — and, since `s`,
		// in which case. The filter *is* the absence of an occurrence, so "a
		// line the search kept carries at least one highlight" holds by
		// construction — two calculations for one question is what
		// scan.Categorize and SecretVerdict each had to undo.
		var ranges []viewer.Range
		if query != "" {
			if ranges = viewer.MatchRanges(line.Plain, query, m.caseSensitive); len(ranges) == 0 {
				continue
			}
		}
		m.matchedLines++

		// Where this line starts, recorded before its segments are appended: `g`
		// lands on the first row of a wrapped line, never inside one.
		m.rowOfLine[line.Num] = len(out)

		// Marking comes after the filter, so it only ever runs on the lines that
		// are about to be drawn. With no ranges it returns the slice untouched.
		tokens := viewer.MarkMatches(line.Tokens, ranges)
		segments := [][]viewer.Token{tokens}
		if m.wrap && textWidth > 0 {
			segments = wrapTokens(tokens, textWidth)
		}
		for i, segment := range segments {
			// Padded to the full width: lipgloss inherits no background
			// (Rule 115), so a line shorter than the pane would show the
			// terminal's own from its last character to the right margin.
			body := renderSegment(segment, line.Level, isLog)
			out = append(out, theme.PadWithBg(m.renderGutter(line.Num, i == 0)+body, width))
		}
	}

	if len(out) == 0 {
		out = append(out, theme.PadWithBg(theme.DimStyle.Render(m.emptyTextMessage()), width))
	}

	m.textViewport.SetContent(strings.Join(out, "\n"))
}

// gutterWidth is what the numbers cost, the space that separates them from the
// text included. Zero when the gutter is off, so every calculation downstream is
// the same expression either way.
func (m Model) gutterWidth() int {
	if !m.showLineNumbers || len(m.lines) == 0 {
		return 0
	}
	return len(strconv.Itoa(len(m.lines))) + 1
}

// renderGutter draws one line's number, right-aligned, or the blank that holds
// its place.
//
// A wrapped line's continuation rows carry the blank rather than repeating the
// number: a number marks where a source line *begins*, and repeating it would
// claim the document holds several lines bearing the same one.
func (m Model) renderGutter(num int, first bool) string {
	width := m.gutterWidth()
	if width == 0 {
		return ""
	}
	if !first {
		return theme.Bg(strings.Repeat(" ", width))
	}
	// Dim, and dim on purpose: the gutter is on every row, so a colour here
	// would inform no one while competing with the log levels and the search
	// highlights, which are what the eye is actually looking for.
	return theme.DimStyle.Render(fmt.Sprintf("%*d ", width-1, num))
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
	m.syncFilterTokens()
	m.rebuildText()
}

// caseToken is what `s` shows in the bar. "Aa" rather than a word because it is
// the one thing on that line the eye is not meant to read — it is a state, and
// the editors this borrows from have made the glyph mean it.
const caseToken = "Aa"

// syncFilterTokens keeps the filter bar showing the filters in force: the
// verbosity, and the case.
//
// The labels carry their values, so SetTokens is given the active flag
// explicitly: it preserves an active state by matching labels, and "≥ warn" is
// not the label "≥ info" was.
//
// The list is rebuilt whole rather than each toggle setting its own — the bar
// disappears when nothing is active (Rule 136), so what must be right is which
// tokens exist at all, and that is one question with one answer.
func (m *Model) syncFilterTokens() {
	var tokens []components.FilterToken
	if m.doc.Kind == viewer.KindLog && m.minLevel != viewer.LevelUnknown {
		tokens = append(tokens, components.FilterToken{Label: minLevelLabel(m.minLevel), Active: true})
	}
	if m.caseSensitive {
		tokens = append(tokens, components.FilterToken{Label: caseToken, Active: true})
	}
	m.bar.SetTokens(tokens)
}
