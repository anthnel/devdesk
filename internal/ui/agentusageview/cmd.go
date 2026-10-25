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
