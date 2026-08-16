package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// handleViewerOpenRequest opens a document, wherever the request came from.
//
// The router does no I/O here, and that is the difference from
// handleWorkspaceScanDetails: a scan result is a cache file the router owns,
// while a file on disk and a `docker inspect` belong to their producers. The
// message carries a source, the viewer loads it in its own Init, and one place
// therefore decides what is too large or not text — and reports it.
func (a *App) handleViewerOpenRequest(msg uiviewer.OpenRequestMsg) (tea.Model, tea.Cmd) {
	if msg.Source == nil {
		return a, nil
	}

	view := uiviewer.NewWithSource(a.config, msg.Source)
	// Where esc goes back to is read from the router rather than carried on the
	// message: the view the user is looking at *is* the one that asked, and a
	// producer naming itself would be a second copy of that fact.
	view.OriginView = a.currentView

	a.views[command.ViewViewer] = view
	a.currentView = command.ViewViewer
	return a, tea.Batch(view.Init(), a.requestResize())
}
