package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/cmdsuggest"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// paletteInputPrompt is the prompt glyph and the cell after it.
const paletteInputPrompt = "❯ "

// paletteInputChrome is the cells of an input row that are not the prompt or
// the text: two borders and one margin on each side.
const paletteInputChrome = 4

// noticeEmptyLine is the warning for an enter on a line with nothing typed.
const noticeEmptyLine = "Type a name or choose one"

// refreshSuggestions ranks the registry for the text in the command line. The
// first row is highlighted when a query is typed (a bare ":" is none) and
// something is found; otherwise nothing is, so enter cannot run a command nobody chose.
func (a *App) refreshSuggestions() {
	a.suggestions = a.suggester.Suggest(a.commandInput.Value())
	a.sugOffset = 0
	a.sugCursor = -1
	if a.argStage() {
		a.sugCursor = a.defaultArgCursor()
		return
	}
	if a.suggestions.Query != "" && len(a.suggestions.Entries) > 0 {
		a.sugCursor = 0
	}
}

// refreshSuggestionsKeepCursor recomputes the list after an asynchronous load
// without undoing a move the user already made: the cursor stays where it was,
// bounded to the new number of rows.
func (a *App) refreshSuggestionsKeepCursor() {
	cursor, offset := a.sugCursor, a.sugOffset
	a.refreshSuggestions()
	if cursor < 0 || a.sugCursor < 0 {
		return
	}
	a.sugCursor = min(cursor, a.listRowCount()-1)
	a.sugOffset = max(min(offset, a.sugCursor), 0)
}

// resetSuggestions forgets the list; the command line is closed or about to be
// reopened empty.
func (a *App) resetSuggestions() {
	a.suggestions = command.Suggestions{}
	a.sugCursor = -1
	a.sugOffset = 0
	a.sugNotice = ""
}

// highlightedEntry returns the highlighted row, if there is one. With no list
// drawn there is no highlight: a choice nobody can see is never made.
func (a *App) highlightedEntry() (command.Entry, bool) {
	if a.paletteRows() == 0 || a.sugCursor < 0 || a.sugCursor >= len(a.suggestions.Entries) {
		return command.Entry{}, false
	}
	return a.suggestions.Entries[a.sugCursor], true
}

// canComplete reports whether tab has something to complete: a highlighted
// row, at the command stage (an argument is typed, not chosen).
func (a *App) canComplete() bool {
	if a.argStage() {
		return a.paletteRows() > 0 && len(a.suggestions.Args) > 0
	}
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
	if a.argStage() {
		a.completeArg()
		return
	}
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

// paletteRect is where the palette sits for the current size.
func (a *App) paletteRect() cmdsuggest.Rect {
	return cmdsuggest.Geometry(a.width, a.height, a.lastHeaderHeight, a.lastFooterHeight)
}

// paletteRows is how many suggestion rows the palette draws; 0 means none.
func (a *App) paletteRows() int {
	return a.paletteRect().Rows
}

// moveHighlight moves the highlight by delta rows without wrapping. With no
// highlighted row, down takes the first visible row and up the last.
func (a *App) moveHighlight(delta int) {
	n := a.listRowCount()
	if n == 0 {
		return
	}
	visible := a.paletteRows()
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
	a.sugOffset = cmdsuggest.Window(a.sugCursor, a.sugOffset, a.paletteRows())
}

// paletteLines renders the open palette, or nil when it is closed.
func (a *App) paletteLines() []string {
	if !a.commandMode {
		return nil
	}
	r := a.paletteRect()
	rows := a.entryRows()
	var heading, empty string
	var loading bool
	if a.argStage() {
		rows = a.argRows()
		heading, empty = a.argHeading(), a.argEmpty()
		loading = !a.suggestions.ArgsReady
	}
	return cmdsuggest.RenderPalette(cmdsuggest.PaletteInput{
		Title: a.paletteTitle(),
		Input: paletteInputView(a.commandInput),
		List: cmdsuggest.Input{
			Rows:    rows,
			Total:   a.suggestions.Total,
			Query:   a.suggestions.Query,
			Cursor:  a.sugCursor,
			Offset:  a.sugOffset,
			Notice:  a.sugNotice,
			Heading: heading,
			Empty:   empty,
			Loading: loading,
		},
	}, r)
}

// paletteTitle is "Contexts" at the ctx argument stage, else "Go to".
func (a *App) paletteTitle() string {
	if a.argStage() {
		return "Contexts"
	}
	return "Go to"
}

// sizePaletteInput gives the command input the width of the open frame minus
// borders, margins, the prompt and the cursor cell the input draws after its
// Width when the line scrolls, and re-seats the cursor so the scroll window
// follows. Called from Update paths only (resize), never from View.
func (a *App) sizePaletteInput() {
	prompt := lipgloss.Width(a.commandInput.Prompt)
	a.commandInput.Width = max(a.paletteRect().Width-paletteInputChrome-prompt-1, 1)
	a.commandInput.SetCursor(a.commandInput.Position())
}

// paletteInputView renders a styled copy of the command input. Styling a copy
// in View() mutates nothing (Rule 110) and follows a theme applied at run
// time. The prompt is the border colour, the text is ColorText, every segment
// carries an explicit background.
func paletteInputView(in textinput.Model) string {
	bg := lipgloss.NewStyle().Background(theme.ColorBackground)
	in.PromptStyle = bg.Foreground(theme.ColorPaletteBorder)
	in.TextStyle = bg.Foreground(theme.ColorText)
	in.PlaceholderStyle = bg.Foreground(theme.ColorText)
	in.Cursor.Style = bg.Foreground(theme.ColorText)
	in.Cursor.TextStyle = bg.Foreground(theme.ColorText)
	return in.View()
}

// entryRows renders the command-stage suggestions as list rows.
func (a *App) entryRows() []cmdsuggest.Row {
	rows := make([]cmdsuggest.Row, len(a.suggestions.Entries))
	for i, e := range a.suggestions.Entries {
		rows[i] = cmdsuggest.Row{Entry: e, Current: e.Kind == command.KindView && e.Name == string(a.currentView)}
	}
	return rows
}
