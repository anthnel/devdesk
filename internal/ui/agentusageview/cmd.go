package agentusageview

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// listSessionsCmd resolves the projects root and lists it, all inside the Cmd:
// the model is not touched (Rule 110) and the answer carries gen back.
func listSessionsCmd(gen int) tea.Cmd {
	return func() tea.Msg {
		root, err := agentusage.ProjectsRoot()
		if err != nil {
			return sessionListLoadedMsg{gen: gen, err: err}
		}
		listing, err := agentusage.ListSessions(root)
		return sessionListLoadedMsg{gen: gen, root: root, listing: listing, err: err}
	}
}

// parseSessionCmd locates and parses a session's transcripts, all inside the
// Cmd (Rule 110). parent is copied in by the caller; the answer carries gen.
func parseSessionCmd(gen int, parent string) tea.Cmd {
	return func() tea.Msg {
		files, err := agentusage.LocateSession(parent)
		if err != nil {
			return sessionUsageLoadedMsg{gen: gen, err: err}
		}
		usage, err := agentusage.ParseSession(files)
		return sessionUsageLoadedMsg{gen: gen, usage: usage, err: err}
	}
}
