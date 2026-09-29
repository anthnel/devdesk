package filebrowser

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// A delete is work like the workspaces one: it goes through the registry, so a
// large tree spins in `:jobs` and a second D on it is refused while it runs.

func deleteRun(path string) jobs.Run {
	return jobs.NewRun(jobs.KindDelete, command.ViewFiles, "", filepath.Base(path), path)
}

// deleting reports whether path is being deleted, by this view or another.
func (m Model) deleting(path string) bool {
	for _, run := range jobs.Unfinished(m.jobs) {
		if run.Kind != jobs.KindDelete {
			continue
		}
		for _, item := range run.Items {
			if item.Target == path && !item.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// handleJobsChanged takes the router's snapshot.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.footer.SetSpinnerFrame(msg.RenderedFrame)
	return m, nil
}

// Transition applies a delete's outcome to the registry.
func (m EntryDeletedMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindDelete, Target: m.Path, State: jobs.ItemDone}
	if m.Err != nil {
		t.State = jobs.ItemFailed
		t.Detail = "delete failed — check logs"
	}
	return t
}

var _ jobs.Reporter = EntryDeletedMsg{}
