package app

import (
	"fmt"
	"strings"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/cmdsuggest"
)

// minListHeight is the smallest usable viewport height that holds a list: one
// row and the bottom rule.
const minListHeight = 2

// noticeEmptyLine is the warning for an enter on a line with nothing typed.
const noticeEmptyLine = "Type a name or choose one"

// refreshSuggestions ranks the registry for the text in the command line. The
// first row is highlighted when a query is typed (a bare ":" is none) and
// something is found; otherwise nothing is, so enter cannot run a command nobody chose.
func (a *App) refreshSuggestions() {
	a.suggestions = a.suggester.Suggest(a.commandInput.Value())
	a.sugOffset = 0
	a.sugCursor = -1
	if a.suggestions.Query != "" && len(a.suggestions.Entries) > 0 {
		a.sugCursor = 0
	}
}

// resetSuggestions forgets the list; the command line is closed or about to be
// reopened empty.
func (a *App) resetSuggestions() {
	a.suggestions = command.Suggestions{}
	a.sugCursor = -1
	a.sugOffset = 0
	a.sugNotice = ""
}

// highlightedEntry returns the highlighted row, if there is one.
func (a *App) highlightedEntry() (command.Entry, bool) {
	if a.sugCursor < 0 || a.sugCursor >= len(a.suggestions.Entries) {
		return command.Entry{}, false
	}
	return a.suggestions.Entries[a.sugCursor], true
}

// canComplete reports whether tab has something to complete: a highlighted
// row, at the command stage (an argument is typed, not chosen).
func (a *App) canComplete() bool {
	_, ok := a.highlightedEntry()
	return ok && a.suggestions.Stage == command.StageCommand
}

// nothingToRunNotice is the warning for an enter with no highlighted row.
func (a *App) nothingToRunNotice() string {
	q := a.suggestions.Query
	if q == "" {
		return noticeEmptyLine
	}
	return fmt.Sprintf("%q is not a view or a command", q)
}

// completeHighlighted writes the highlighted row's name into the line, with a
// trailing space when the command takes an argument. It runs nothing.
func (a *App) completeHighlighted() {
	if !a.canComplete() {
		return
	}
	entry, _ := a.highlightedEntry()
	text := entry.Name
	if entry.TakesArg() {
		text += " "
	}
	a.commandInput.SetValue(text)
	a.commandInput.CursorEnd()
	a.refreshSuggestions()
}

// listUsable is the number of viewport content lines the list may cover.
func (a *App) listUsable() int {
	if a.frameless() {
		return a.viewport.Height
	}
	return a.viewport.Height - 1 // the bottom border stays
}

// listVisible is how many suggestion rows are drawn; 0 means no list.
func (a *App) listVisible() int {
	usable := a.listUsable()
	if usable < minListHeight {
		return 0
	}
	return cmdsuggest.VisibleRows(len(a.suggestions.Entries), usable)
}

// moveHighlight moves the highlight by delta rows without wrapping. With no
// highlighted row, down takes the first visible row and up the last.
func (a *App) moveHighlight(delta int) {
	n := len(a.suggestions.Entries)
	if n == 0 {
		return
	}
	visible := a.listVisible()
	switch {
	case a.sugCursor < 0 && delta > 0:
		a.sugCursor = a.sugOffset
	case a.sugCursor < 0:
		a.sugCursor = min(n-1, a.sugOffset+max(visible, 1)-1)
	default:
		a.sugCursor = max(min(a.sugCursor+delta, n-1), 0)
	}
	a.sugOffset = cmdsuggest.Window(a.sugCursor, a.sugOffset, visible)
}

// keepHighlightVisible re-windows the list after the viewport changed size.
func (a *App) keepHighlightVisible() {
	a.sugOffset = cmdsuggest.Window(a.sugCursor, a.sugOffset, a.listVisible())
}

// suggestionLines renders the list and its bottom rule, or nil when the command
// line is closed or the viewport is too short for a list.
func (a *App) suggestionLines() []string {
	visible := a.listVisible()
	if !a.commandMode || visible == 0 {
		return nil
	}
	rows := make([]cmdsuggest.Row, len(a.suggestions.Entries))
	for i, e := range a.suggestions.Entries {
		rows[i] = cmdsuggest.Row{Entry: e, Current: e.Kind == command.KindView && e.Name == string(a.currentView)}
	}
	frame := cmdsuggest.FrameJoined
	if a.frameless() {
		frame = cmdsuggest.FrameClosed
	}
	return cmdsuggest.Render(cmdsuggest.Input{
		Rows:    rows,
		Total:   a.suggestions.Total,
		Query:   a.suggestions.Query,
		Cursor:  a.sugCursor,
		Offset:  a.sugOffset,
		Notice:  a.sugNotice,
		Width:   a.viewport.Width,
		Visible: visible,
		Frame:   frame,
	})
}

// overlaySuggestions replaces the top lines of the viewport with the list.
// Whole lines are swapped, so no styled text is ever cut.
func (a *App) overlaySuggestions(body string) string {
	list := a.suggestionLines()
	if len(list) == 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	copy(lines, list)
	return strings.Join(lines, "\n")
}
