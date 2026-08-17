package viewer

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders whichever half is on screen.
func (m Model) View() string {
	contentStyle := lipgloss.NewStyle().Background(theme.ColorBackground).PaddingLeft(1)

	switch {
	case m.loading:
		return contentStyle.Render(theme.DimStyle.Render("\nLoading..."))
	case !m.hasDocument():
		return contentStyle.Render(theme.HelpStyle.Render("\nNothing open\n\nOpen a file from workspaces, or inspect a container."))
	case m.display == displayTree:
		return m.tree.View()
	default:
		return m.textViewport.View()
	}
}

// GetFooterHeight is Rule 124's budget: an empty line and an info line, plus the
// filter bar when it is showing.
func (m Model) GetFooterHeight() int {
	return 2 + m.bar.ExtraHeight()
}

// RenderFooter draws the filter bar and the one line of state.
func (m Model) RenderFooter(width int) string {
	var parts []string
	if m.bar.IsVisible() {
		parts = append(parts, m.bar.View())
	}
	parts = append(parts, theme.EmptyLineBg(width), m.renderInfoLine(width))
	return strings.Join(parts, "\n")
}

func (m Model) renderInfoLine(width int) string {
	if m.footerError != "" {
		return theme.PadWithBg(theme.StatusErrorStyle.Render(m.footerError), width)
	}
	if m.footerInfo == "" {
		return theme.EmptyLineBg(width)
	}
	return lipgloss.NewStyle().
		Foreground(theme.ColorHighlight).
		Background(theme.ColorBackground).
		Width(width).
		Align(lipgloss.Center).
		Render(m.footerInfo)
}

// ── HeaderView ───────────────────────────────────────────────────────────────
//
// All four methods. The router probes for the interface as a whole and falls
// back in silence, so a view supplying two of them renders an empty title with
// nothing to say why — which is what happened to the configuration view.

func (m Model) GetTitle() string {
	if name := m.Name(); name != "" {
		return theme.IconCodeFile + " Viewer · " + name
	}
	return theme.IconCodeFile + " Viewer"
}

func (m Model) GetIcon() string { return "" }

func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	info := []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
		{Key: "Format", Value: m.formatLabel(), Style: theme.HeaderValueStyle},
	}
	if !m.hasDocument() {
		return info
	}
	if m.structured() && m.display == displayTree {
		return append(info, shortcut.HeaderInfo{
			Key: "Nodes", Value: fmt.Sprintf("%d", m.doc.NodeCount()), Style: theme.HeaderValueStyle,
		})
	}
	return append(info, shortcut.HeaderInfo{
		Key: "Lines", Value: m.lineCount(), Style: theme.HeaderValueStyle,
	})
}

// lineCount says how many lines are showing, and out of how many when a filter
// is narrowing them. A bare count under an active filter would read as the size
// of the document, which is the one thing it is not.
func (m Model) lineCount() string {
	total := len(m.lines)
	if m.matchedLines == total {
		return fmt.Sprintf("%d", total)
	}
	return fmt.Sprintf("%d/%d", m.matchedLines, total)
}

// GetShortcuts is state-aware (Rule 130): every key that depends on the kind of
// document or on what its source can do is offered only when it would work.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	if !m.hasDocument() {
		return []shortcut.Shortcut{{Key: "esc", Description: "Go back"}}
	}

	var shortcuts []shortcut.Shortcut

	if m.structured() {
		if m.display == displayTree {
			shortcuts = append(shortcuts,
				shortcut.Shortcut{Key: "←→", Description: "Collapse/Expand"},
				shortcut.Shortcut{Key: "f", Description: "Show as text"},
			)
		} else {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "f", Description: "Show as tree"})
		}
	}

	if m.display == displayText {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "w", Description: "Toggle wrap"})
		if m.isLog() {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "v", Description: "Verbosity"})
		}
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "/", Description: "Search"})
	}

	shortcuts = append(shortcuts, shortcut.Shortcut{Key: "c", Description: "Toggle coloring"})

	if _, ok := m.timestamps(); ok {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "t", Description: "Toggle timestamps"})
	}
	if m.source != nil {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+r", Description: "Reload"})
	}
	if _, ok := m.followable(); ok {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "F", Description: "Follow live output"})
	}
	if _, ok := m.pageable(); ok {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "V", Description: "Open in system pager"})
	}

	return append(shortcuts,
		shortcut.Shortcut{Key: "esc", Description: "Go back"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
}

// GetHelpContent implements help.Provider (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Viewer",
		Description: "A read-only view of one document. JSON and XML open on a navigable tree; " +
			"YAML and TOML open as colored text; logs open as text with a verbosity filter; " +
			"everything else opens as text. " +
			"The viewer is opened from another view — a file in workspaces, an inspect or a log in containers — and Esc returns there.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑/k", Description: "Move up"},
			{Key: "↓/j", Description: "Move down"},
			{Key: "→/l", Description: "Expand the selected node (tree)"},
			{Key: "←/h", Description: "Collapse the selected node (tree)"},
			{Key: "f", Description: "Switch between the tree and the document's own text"},
			{Key: "c", Description: "Turn syntax coloring on or off"},
			{Key: "w", Description: "Soft-wrap long lines (text)"},
			{Key: "v", Description: "Cycle the minimum log level shown (logs)"},
			{Key: "/", Description: "Search the text — matching lines only, occurrences highlighted"},
			{Key: "t", Description: "Show or hide timestamps (container logs)"},
			{Key: "ctrl+r", Description: "Reload from the source"},
			{Key: "F", Description: "Follow live output (container logs)"},
			{Key: "V", Description: "Open in the system pager (container logs)"},
			{Key: "esc", Description: "Return to the view the document was opened from"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Tree and text",
				Body: "A JSON or XML document opens on its tree: → expands a node, ← collapses it, and a closed " +
					"container shows how many children it holds. Press f for the document's own text, exactly as it " +
					"is on disk — press f again to come back. A document with no structure has no tree and f does " +
					"nothing: that includes a log, and it includes YAML and TOML, which are colored but not walkable.",
			},
			{
				Title: "Coloring",
				Body: "c turns syntax coloring on and off, in the tree and in the text alike. JSON, XML, YAML and " +
					"TOML are colored; a file is recognized by its extension, never by what its content looks like. " +
					"With coloring off the document reads as plain text. Colors follow the current theme; no document " +
					"has colors of its own.",
			},
			{
				Title: "Search",
				Body: "/ searches the text. Lines with no match are hidden and the header counts what is left, so " +
					"a search reads as a filter — and every occurrence in the lines that remain is highlighted, which " +
					"is what says where the match is in a long line. The highlight is not syntax coloring: it stays " +
					"on with c off, it survives soft wrap, and in a log the line keeps its level color around it.",
			},
			{
				Title: "Log verbosity",
				Body: "v cycles the minimum level shown: all, trace, debug, info, warn, error. A line with no level " +
					"of its own inherits the level of the line above it, so a stack trace stays with the error that " +
					"raised it — filtering to 'warn and above' never breaks one apart. A line before any level at all " +
					"is always shown.",
			},
			{
				Title: "Reload and follow",
				Body: "ctrl+r re-reads the document from where it came from — the file on disk, or docker. For " +
					"container logs, ctrl+f follows live output and e opens the system pager; both suspend the TUI " +
					"until you leave them, and the document is re-read on the way back.",
			},
			{
				Title: "What will not open",
				Body: "A file over 5 MB is refused, and so is a binary one: the footer says which. A malformed JSON " +
					"or XML is not refused — it opens as text, with a note saying it would not parse, because it is " +
					"exactly the file you need to look at.",
			},
		},
	}
}
