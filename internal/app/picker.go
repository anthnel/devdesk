package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/filebrowser"
)

// The path picker (§3.95): any view lends itself the file browser to have a
// path chosen, and gets the answer back as filebrowser.PathPickedMsg or
// PickCancelledMsg.
//
// It is not selection.go, on purpose. That file translates each borrower's
// request into its own vocabulary, because what the explorer borrows is two
// different views answering two different questions. Here the question is
// always "which path", so one message pair serves every borrower and Tag says
// which field it was for. The return slot is its own for the same reason the
// pair is: the templates view can itself be lent to the explorer, and a picker
// opened from its form must not overwrite where that borrow returns to.

// handlePickRequest lends a picker, remembering who asked.
func (a *App) handlePickRequest(msg filebrowser.PickRequestMsg) (tea.Model, tea.Cmd) {
	a.pickerReturnView = a.currentView
	log.Printf("Path picker requested by %s (tag %q)", a.currentView, msg.Tag)
	picker := filebrowser.NewPicker(a.config, msg)
	a.views[command.ViewFiles] = picker
	a.currentView = command.ViewFiles
	return a, tea.Batch(picker.Init(), a.requestResize())
}

// handlePickAnswer returns to the borrower with its answer — chosen or not —
// and drops the lent picker, so `:files` next time is the browser, not it.
func (a *App) handlePickAnswer(msg tea.Msg) (tea.Model, tea.Cmd) {
	delete(a.views, command.ViewFiles)
	a.currentView = a.pickerReturnView
	a.pickerReturnView = ""
	return a, a.returnToOrigin(msg)
}
