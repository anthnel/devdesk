package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/contextcmd"
	"github.com/anthnel/devdesk/internal/ui/cmdsuggest"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// argStage reports whether the list shows argument rows: the :ctx command at
// its argument stage. Other commands keep the single command row.
func (a *App) argStage() bool {
	return a.suggestions.Stage == command.StageArgument &&
		command.ParseCommand(a.commandInput.Value()).Type == command.CommandContext
}

// listRowCount is how many rows the list holds at the current stage.
func (a *App) listRowCount() int {
	if a.argStage() {
		return len(a.suggestions.Args)
	}
	return len(a.suggestions.Entries)
}

// ctxArgText is the text typed after the command word, trimmed on the left.
func (a *App) ctxArgText() string {
	_, rest, _ := strings.Cut(strings.TrimLeft(a.commandInput.Value(), ": "), " ")
	return strings.TrimLeft(rest, " ")
}

// defaultArgCursor picks the row highlighted when the argument list is drawn:
// the exact match; else the only match when contextcmd accepts it as a valid
// name; else none.
func (a *App) defaultArgCursor() int {
	args := a.suggestions.Args
	prefix := a.ctxArgText()
	if prefix == "" || !a.suggestions.ArgsReady {
		return -1
	}
	for i, s := range args {
		if s.Value == prefix {
			return i
		}
	}
	if len(args) != 1 {
		return -1
	}
	if !contextcmd.ValidName(prefix) {
		return -1
	}
	return 0
}

// argHeading is the rule's label at the argument stage: contexts shown out of
// all contexts, the reserved words not counted.
func (a *App) argHeading() string {
	shown := 0
	for _, s := range a.suggestions.Args {
		if s.Value != contextcmd.WordPrevious && s.Value != contextcmd.WordNew {
			shown++
		}
	}
	return fmt.Sprintf("Contexts · %d of %d", shown, len(a.snapshot().Names))
}

// argRows renders the argument suggestions as list rows.
func (a *App) argRows() []cmdsuggest.Row {
	rows := make([]cmdsuggest.Row, len(a.suggestions.Args))
	for i := range a.suggestions.Args {
		s := a.suggestions.Args[i]
		rows[i] = cmdsuggest.Row{Arg: &s, Current: s.Current}
	}
	return rows
}

// argEmpty is the message of an argument list with no rows.
func (a *App) argEmpty() string {
	prefix := a.ctxArgText()
	if strings.HasPrefix(strings.ToLower(prefix), contextcmd.WordNew+" ") {
		return ""
	}
	return fmt.Sprintf("No context starts with %q", prefix)
}

// highlightedArg returns the value of the highlighted argument row, or "".
func (a *App) highlightedArg() string {
	if !a.argStage() || a.sugCursor < 0 || a.sugCursor >= len(a.suggestions.Args) {
		return ""
	}
	return a.suggestions.Args[a.sugCursor].Value
}

// completeArg is tab at the argument stage: it highlights the first row when
// none is, otherwise writes the highlighted value into the line.
func (a *App) completeArg() {
	if len(a.suggestions.Args) == 0 {
		return
	}
	if a.sugCursor < 0 {
		a.sugCursor = a.sugOffset
		return
	}
	word, _, _ := strings.Cut(strings.TrimLeft(a.commandInput.Value(), ": "), " ")
	text := word + " " + a.highlightedArg()
	if a.highlightedArg() == contextcmd.WordNew {
		text += " "
	}
	a.commandInput.SetValue(text)
	a.commandInput.CursorEnd()
	a.refreshSuggestions()
}

// runContext is enter on a :ctx command line.
func (a *App) runContext(args []string) (tea.Model, tea.Cmd) {
	highlighted := a.highlightedArg()
	if highlighted == contextcmd.WordNew && (len(args) == 0 || args[0] != contextcmd.WordNew) {
		// "new" takes an argument of its own: enter completes it like tab.
		a.completeArg()
		return a, nil
	}
	if highlighted == contextcmd.WordPrevious {
		args = []string{highlighted}
	}
	if len(args) == 0 && highlighted != "" {
		args = []string{highlighted}
	}
	action := contextcmd.Resolve(args, highlighted, a.snapshot())
	if action.Kind == contextcmd.KeepOpen {
		a.sugNotice = action.Text
		return a, nil
	}
	a.closeCommandLineQuiet()
	switch action.Kind {
	case contextcmd.Switch:
		return a, a.switchContext(action.Name)
	case contextcmd.Create:
		return a, a.createContext(action.Name)
	case contextcmd.Refuse:
		return a, components.PostFooter(footerLevel(action.Level), action.Text)
	}
	return a, a.listContexts()
}

// closeCommandLineQuiet leaves command mode; the caller returns the Cmd that
// follows (the re-layout happens on the next resize).
func (a *App) closeCommandLineQuiet() {
	a.commandMode = false
	a.commandInput.Blur()
	a.resetSuggestions()
}

// footerLevel maps a contextcmd level to a footer level.
func footerLevel(l contextcmd.Level) components.Level {
	switch l {
	case contextcmd.Warn:
		return components.LevelWarning
	case contextcmd.Error:
		return components.LevelError
	}
	return components.LevelInfo
}
