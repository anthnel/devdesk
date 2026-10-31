package app

import (
	"log"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/contextcmd"
	"github.com/anthnel/devdesk/internal/ui/components"
)

const (
	noteNewContext      = "Create a context"
	notePreviousContext = "Previous context"
)

// contextNamesLoadedMsg carries the context names read off disk when the
// command line opened.
type contextNamesLoadedMsg struct {
	Names []string
	Err   error
}

// contextArgs is the in-memory snapshot behind the context argument provider.
// Only Update() writes it (Rule 110); SuggestArgs never touches the disk.
type contextArgs struct {
	names   []string
	ready   bool
	current string
}

// SuggestArgs implements command.ArgProvider from the snapshot only.
func (c *contextArgs) SuggestArgs(prefix string) ([]command.ArgSuggestion, bool) {
	if !c.ready {
		return nil, false
	}
	// "new" alone is a plain prefix (it may start a context name); "new " or
	// "new x" is the creation form, where there is nothing left to complete.
	if p := strings.ToLower(prefix); strings.HasPrefix(p, contextcmd.WordNew+" ") {
		return nil, true
	}
	var out []command.ArgSuggestion
	for _, n := range contextcmd.Match(prefix, c.names) {
		out = append(out, command.ArgSuggestion{Value: n, Current: n == c.current})
	}
	for _, s := range []command.ArgSuggestion{
		{Value: contextcmd.WordPrevious, Note: notePreviousContext},
		{Value: contextcmd.WordNew, Note: noteNewContext},
	} {
		if strings.HasPrefix(s.Value, strings.ToLower(prefix)) {
			out = append(out, s)
		}
	}
	return out, true
}

// loadContextNamesCmd lists the contexts off the Update goroutine.
func loadContextNamesCmd() tea.Cmd {
	return func() tea.Msg {
		names, err := config.ListContexts()
		sort.Strings(names)
		return contextNamesLoadedMsg{Names: names, Err: err}
	}
}

// handleContextNamesLoaded stores the names read when the command line opened.
func (a *App) handleContextNamesLoaded(msg contextNamesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [app/context] list contexts: %v", msg.Err)
		a.ctxArgs.ready = false
		if a.commandMode {
			a.refreshSuggestionsKeepCursor()
		}
		return a, components.PostFooter(components.LevelError, "Failed to list contexts — check logs")
	}
	a.ctxArgs.names, a.ctxArgs.ready = msg.Names, true
	a.ctxArgs.current = a.currentContext
	if a.commandMode {
		a.refreshSuggestionsKeepCursor()
	}
	return a, nil
}

// snapshot returns the contexts known to the router.
func (a *App) snapshot() contextcmd.Snapshot {
	if a.ctxArgs == nil {
		return contextcmd.Snapshot{Current: a.currentContext, Previous: a.previousContext}
	}
	return contextcmd.Snapshot{
		Names:    a.ctxArgs.names,
		Current:  a.currentContext,
		Previous: a.previousContext,
		Ready:    a.ctxArgs.ready,
	}
}
