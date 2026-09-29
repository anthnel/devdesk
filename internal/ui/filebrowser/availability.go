package filebrowser

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/fsbrowse"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// What the selected row allows, computed once and read by both GetShortcuts (to
// grey) and the handlers (to refuse) — Rule 130, the pattern of
// internal/ui/workspaces/availability.go.
//
// N is absent for the reason it is absent there: it creates in the browsed
// directory and never acts on the selection, so it applies on every row.
type actionSet struct {
	Enter  shortcut.Availability // open a file / pick the row
	Right  shortcut.Availability // enter a directory
	Delete shortcut.Availability
	Copy   shortcut.Availability
}

// The reasons, written once so the header, the footer and the tests agree.
const (
	reasonNoRow         = "No entry selected"
	reasonNotADir       = "Not a directory"
	reasonADir          = "A directory — → enters it"
	reasonNotAFile      = "Not a file — pick a file"
	reasonSelfRow       = "This is the directory being browsed — ← to leave it first"
	reasonProtected     = "A filesystem root or the home directory is never deleted from here"
	reasonDeleting      = "Being deleted"
	reasonNotADirToPick = "Not a directory — pick a directory"
)

func (m Model) actions() actionSet {
	r, ok := m.selected()
	if !ok {
		none := shortcut.Unavailable(reasonNoRow)
		return actionSet{Enter: none, Right: none, Delete: none, Copy: none}
	}
	a := actionSet{Enter: m.enterState(r), Right: shortcut.Availability{}, Delete: shortcut.Availability{}}
	if !r.IsDir || r.self {
		a.Right = shortcut.Unavailable(reasonNotADir)
	}
	switch {
	case r.self:
		a.Delete = shortcut.Unavailable(reasonSelfRow)
	case fsbrowse.Protected(r.Path):
		a.Delete = shortcut.Unavailable(reasonProtected)
	case m.deleting(r.Path):
		a.Delete = shortcut.Unavailable(reasonDeleting)
	}
	return a
}

// enterState is what enter does to a row: in a picker it chooses a row of the
// requested kind; otherwise it opens a file, and a directory is →'s.
func (m Model) enterState(r row) shortcut.Availability {
	if m.pick != nil {
		switch {
		case m.pick.Kind == PickDir && !r.IsDir:
			return shortcut.Unavailable(reasonNotADirToPick)
		case m.pick.Kind == PickFile && r.IsDir:
			return shortcut.Unavailable(reasonNotAFile)
		}
		return shortcut.Availability{}
	}
	if r.IsDir {
		return shortcut.Unavailable(reasonADir)
	}
	return shortcut.Availability{}
}

// guard refuses a key the header shows greyed, and says why (Rule 128: a Warn).
func (m Model) guard(s shortcut.Availability, act func() (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if !s.Enabled() {
		return m, m.footer.Warn(s.Reason)
	}
	return act()
}
