package viewer

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders whichever half is on screen.
func (m Model) View() string {
	contentStyle := lipgloss.NewStyle().Background(theme.ColorBackground).PaddingLeft(1)

	switch {
	case m.loading && !m.hasDocument():
		// The load says so in the footer, with a spinner. Nothing is shown here
		// because there is nothing yet to show — and a reload keeps the
		// document on screen rather than blanking it, which is the case below.
		return ""
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
//
// The go-to-line prompt shares that slot with the filter bar and the two never
// hold it at once, so the height is the same whichever is there — which is what
// keeps the pane from resizing under the reader when the prompt opens over an
// active search.
func (m Model) GetFooterHeight() int {
	return 2 + m.barHeight()
}

func (m Model) barHeight() int {
	if m.gotoActive {
		return 2
	}
	return m.bar.ExtraHeight()
}

// RenderFooter draws the bar and the one line of state.
func (m Model) RenderFooter(width int) string {
	var parts []string
	switch {
	case m.gotoActive:
		parts = append(parts, m.renderGotoBar(width))
	case m.bar.IsVisible():
		parts = append(parts, m.bar.View())
	}
	parts = append(parts, theme.EmptyLineBg(width), m.footer.View(width, m.status()))
	return strings.Join(parts, "\n")
}

// status is what the view derives on every frame: the read, reported here so
// the pane does not swap itself for a message.
func (m Model) status() components.Status {
	if m.loading {
		return components.Status{Text: "Loading " + m.Name() + "...", Spinner: true}
	}
	// Following is a state, not an event, so it is derived here rather than
	// posted as a footer message — one would expire after three seconds while
	// the pane went on refreshing (Rule 128). It is also the only thing on
	// screen saying the document is live: the reads themselves are deliberately
	// silent, because a spinner blinking every two seconds would read as
	// something going wrong with the thing that is working.
	if m.following {
		return components.Status{Text: "Following — " + keymap.Fetch + " to stop"}
	}
	return components.Status{}
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
		// ←→ collapses a node, which only the tree has — greyed in the text
		// pane rather than dropped, for the same reason as w and / below.
		inTree := m.display == displayTree
		display := "Show as tree"
		if inTree {
			display = "Show as text"
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "←→", Description: "Collapse/Expand", Disabled: !inTree},
			shortcut.Shortcut{Key: "f", Description: display},
		)
	}

	// The same key, and the wording says which way it goes — "Show raw" on a
	// rendered document, "Show rendered" on its source.
	if m.renderable() {
		if m.display == displayRendered {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "f", Description: "Show raw markdown"})
		} else {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "f", Description: "Show rendered markdown"})
		}
	}

	// The tree is the same document seen another way, and `f` toggles between
	// the two — so the text pane's keys are greyed there rather than dropped
	// (Rule 130). Hiding them made three entries appear and disappear on every
	// press of a key whose whole job is to switch back and forth.
	//
	// What depends on the *document* rather than on the display stays out of
	// the list entirely: a Markdown file has no verbosity and never will, which
	// is a different screen's worth of difference, not a "not now".
	inTree := m.display == displayTree
	shortcuts = append(shortcuts, shortcut.Shortcut{Key: "w", Description: "Toggle wrap", Disabled: inTree})
	if m.isLog() {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "v", Description: "Verbosity", Disabled: inTree})
	}
	shortcuts = append(shortcuts,
		shortcut.Shortcut{Key: "/", Description: "Search", Disabled: inTree},
		// Beside the search it modifies, and greyed with it: the case is a
		// property of the `/` filter, and a document has one whether or not a
		// query is running — so it is offered before one is typed, the way `w`
		// is offered before there is anything to wrap.
		shortcut.Shortcut{Key: "s", Description: "Toggle case", Disabled: inTree},
		// The gutter and the jump belong to the text pane for the same reason as
		// the wrap: the tree has rows, not lines, and a line number there would
		// name nothing.
		shortcut.Shortcut{Key: "n", Description: "Toggle line numbers", Disabled: inTree},
		shortcut.Shortcut{Key: "g", Description: "Go to line", Disabled: inTree},
	)

	shortcuts = append(shortcuts, shortcut.Shortcut{Key: "c", Description: "Toggle coloring"})

	shortcuts = append(shortcuts, shortcut.Shortcut{Key: keymap.Copy, Description: "Copy content to the clipboard"})
	if _, ok := m.pathed(); ok {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: keymap.IDE, Description: "Open in the configured IDE"})
	}

	if _, ok := m.timestamps(); ok {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "t", Description: "Toggle timestamps"})
	}
	if m.source != nil {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+r", Description: "Reload"})
	}
	if _, ok := m.followable(); ok {
		// The wording says which way the key goes, the way `f` does above.
		if m.following {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: keymap.Fetch, Description: "Stop following"})
		} else {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: keymap.Fetch, Description: "Follow live output"})
		}
	}
	if _, ok := m.pageable(); ok {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: keymap.Pager, Description: "Open in system pager"})
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
			"Markdown opens rendered, with its markers applied rather than shown; " +
			"YAML, TOML, Dockerfiles and shell scripts open as colored text; " +
			"logs open as text with a verbosity filter; everything else opens as text. " +
			"The viewer is opened from another view — a file in workspaces, an inspect or a log in containers — and Esc returns there.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑/k", Description: "Move up"},
			{Key: "↓/j", Description: "Move down"},
			{Key: "→/l", Description: "Expand the selected node (tree)"},
			{Key: "←/h", Description: "Collapse the selected node (tree)"},
			{Key: "f", Description: "Switch between the derived view — tree, or rendered Markdown — and the source"},
			{Key: "c", Description: "Turn syntax coloring on or off"},
			{Key: keymap.Copy, Description: "Copy the document's content to the clipboard"},
			{Key: keymap.IDE, Description: "Open the file in the configured IDE (files only)"},
			{Key: "w", Description: "Soft-wrap long lines (text)"},
			{Key: "v", Description: "Cycle the minimum log level shown (logs)"},
			{Key: "/", Description: "Search the text — matching lines only, occurrences highlighted"},
			{Key: "s", Description: "Search with the case, or without it (text)"},
			{Key: "n", Description: "Show or hide line numbers (text)"},
			{Key: "g", Description: "Go to a line by its number (text)"},
			{Key: "t", Description: "Show or hide timestamps (container logs)"},
			{Key: "ctrl+r", Description: "Reload from the source"},
			{Key: keymap.Fetch, Description: "Follow live output — re-reads the log every 2s in this pane, and keeps the view pinned to the bottom. Press F again to stop (container logs)"},
			{Key: keymap.Pager, Description: "Open in the system pager, which streams rather than polls. Leave it with 'q' (container logs)"},
			{Key: "esc", Description: "Return to the view the document was opened from"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "The source, and the view derived from it",
				Body: "f switches between the document as it is on disk and the one view its kind derives from it. " +
					"A JSON or XML document derives a tree: → expands a node, ← collapses it, and a closed container " +
					"shows how many children it holds. A Markdown document derives its rendered form: the #, the ** " +
					"and the backticks are applied as weight, italics and color instead of being shown, list bullets " +
					"become •, a quote becomes a rule down the margin, and a fenced code block keeps the coloring of " +
					"its own language. Press f again for the source, to the character. " +
					"A kind derives at most one view, so f is never ambiguous — and for a kind that derives none it " +
					"is hidden rather than offered and ignored: that covers logs, YAML, TOML, Dockerfiles and shell " +
					"scripts, all of which are colored but not walkable.",
			},
			{
				Title: "What rendering does not do",
				Body: "Links, tables and horizontal rules are shown as they are written. A link's brackets are " +
					"ordinary text to the lexer, indistinguishable from a bracket in a sentence, and this viewer " +
					"guesses at nothing — the link text and its URL are colored apart instead. Nothing is out of " +
					"reach either way: f shows the source exactly.",
			},
			{
				Title: "Coloring",
				Body: "c turns syntax coloring on and off, in the tree and in the text alike. JSON, XML, YAML, TOML, " +
					"Markdown, Dockerfiles and shell scripts are colored. A file is recognized by its extension or by " +
					"its whole name — Dockerfile, Dockerfile.dev, .bashrc — and never by what its content looks like; " +
					"the one exception is a file with no extension at all opening on a shebang, which is the file " +
					"naming its own interpreter. With coloring off the document reads as plain text, and a rendered " +
					"Markdown keeps its markers hidden: c is coloring, f is the display. Colors follow the current " +
					"theme; no document has colors of its own.",
			},
			{
				Title: "Search",
				Body: "/ searches the text. Lines with no match are hidden and the header counts what is left, so " +
					"a search reads as a filter — and every occurrence in the lines that remain is highlighted, which " +
					"is what says where the match is in a long line. The highlight is not syntax coloring: it stays " +
					"on with c off, it survives soft wrap, and in a log the line keeps its level color around it. " +
					"s decides whether the case counts. It is off by default, so ERROR and error are the same " +
					"search; turn it on and the bar shows Aa. It applies to the search already running, without " +
					"retyping it, which is what makes the two readings comparable.",
			},
			{
				Title: "Line numbers, and going to one",
				Body: "n shows the line numbers, in a gutter down the left. They are the document's own numbers " +
					"and not a count of what is on screen: under a search or a verbosity filter they stay as they " +
					"are, with gaps where the hidden lines were — which is the only reading that lets a line be " +
					"quoted by its number. A soft-wrapped line numbers its first row and leaves the rest blank, " +
					"because a number says where a line begins. " +
					"g asks for a number and goes there, putting that line at the top of the pane. A number past " +
					"the end of the document is refused, and so is one the filter is hiding: it says which, and " +
					"nothing moves — jumping somewhere else while showing a number that is not the one you asked " +
					"for would be worse than not jumping at all.",
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
				Title: "Copying and editing",
				Body: "Y copies the document's own text to the clipboard — the source, not what f currently " +
					"renders, so a rendered Markdown still copies its markers. O hands the file to the configured " +
					"IDE (app.ide_command); it only appears for a document backed by a real file on disk, so an " +
					"inspect or a container log does not offer it.",
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
