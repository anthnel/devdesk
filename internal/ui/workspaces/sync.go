package workspaces

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/jobs"
)

// The sync flow (§3.17), the counterpart of the explorer's clone.
//
// **It has no screen of its own, and that is the difference from the clone.**
// The clone opens a list because its rows do not exist yet — discovery invents
// them one at a time. Here every repository is already a row the user is
// looking at, so a second list would show the same names twice. Sync decorates
// what is on screen instead: a spinner in the Git Status cell, exactly as a
// scan decorates the Scanned cell, and the counts tell the truth again when it
// finishes.
//
// The target follows S's rule rather than inventing a selection mode: a
// git repository syncs itself, a plain directory syncs every repository nested
// under it, anything else does nothing.

// busyMessage is what every action says when another one holds the repository.
// One message rather than three, because the user's next move is the same
// whichever holds it: wait.
const busyMessage = "Already busy — a scan, sync or delete is running here"

// busy reports whether a repository is already being scanned, synced or
// deleted.
//
// A scan reads the working tree while a fast-forward rewrites it; running both
// at once is a race whose visible result is a report describing a tree that no
// longer exists. A delete is worse on both counts — it takes the tree away
// from under either of them, and re-issuing it fails on a path that is already
// gone. Whichever started first keeps the repository.
//
// It answers from the registry now, so it sees work started **anywhere**: the
// three maps it used to read knew only what this view had launched, which let a
// scan started from `:sec` through the guard and had the two write the same
// cache entry.
func (m Model) busy(path string) bool {
	return m.working(path)
}

// startSync syncs the row under the cursor, or everything beneath it.
func (m Model) startSync() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok || !entry.IsDir {
		return m, nil
	}

	targets := []string{entry.Path}
	if !entry.IsGitRepo {
		targets = entry.SubRepoPaths
	}
	if len(targets) == 0 {
		// Unreachable through F: actions() refuses a directory with no nested
		// repository before the key gets here, and it is that refusal which
		// carries the "I could not read part of the tree" wording (Rule 130).
		return m, nil
	}

	var toSync []string
	for _, path := range targets {
		if !m.busy(path) {
			toSync = append(toSync, path)
		}
	}
	if len(toSync) == 0 {
		return m, m.footer.Warn(busyMessage)
	}

	m.syncUnreadable = entry.SubRepoSkipped
	return m, jobs.Start(m.syncRun(toSync), batchSyncCmd(toSync, m.syncSpec()))
}

// syncSpec is everything a sync needs from the model, read out up front so the
// commands touch nothing (Rule 110).
func (m Model) syncSpec() syncSpec {
	return syncSpec{
		gitlabURL: m.config.Forge.URL,
		token:     tokenLoader(m.secrets, m.config.Forge.URL),
		jobs:      m.config.Forge.Pull.ParallelJobs,
	}
}

// handleWorkspaceSyncComplete folds one repository's result in (Rule 128).
//
// The registry has already recorded the transition by the time this runs — the
// router applies it before handing the message on — so the run read here is
// current, and the counting this function used to do is gone with the syncRun
// that held it.
func (m Model) handleWorkspaceSyncComplete(msg WorkspaceSyncCompleteMsg) (tea.Model, tea.Cmd) {
	// The listing may have been reloaded or navigated away from while the sync
	// ran. Re-reading the row still applies either way — it is about the
	// repository, not about what is on screen.
	m.applyGitStatus(msg.RepoPath, msg.Status)

	if msg.Error != nil {
		log.Printf("ERROR [workspaces] sync %s: %v", msg.RepoPath, msg.Error)
	}

	// The summary is an event, not a state, so it is a footer message with the
	// three seconds Rule 128 gives one — unlike the progress line, which is
	// derived every frame because a batch outlives that timer. They shared a
	// function while both came from the same struct; they no longer do.
	run, ok := m.settledSyncRun(msg.RepoPath)
	if !ok {
		return m, nil
	}
	summary := m.syncSummary(run)
	m.syncUnreadable = 0
	return m, m.footer.Info(summary)
}

// settledSyncRun returns the sync run holding a repository, if that run has
// just finished. A batch reports once, on its last repository.
func (m Model) settledSyncRun(repoPath string) (jobs.Run, bool) {
	for _, run := range m.jobs {
		if run.Kind != jobs.KindSync || !run.Finished() {
			continue
		}
		for _, item := range run.Items {
			if item.Target == repoPath {
				return run, true
			}
		}
	}
	return jobs.Run{}, false
}

// applyGitStatus copies a repository's re-read git fields onto the row that
// holds it, so the Git Status column stops describing the state before the sync.
//
// Only the git fields move: Name, Path, ModTime and the project type belong to
// the listing, and the sync had no reason to look at them.
//
// The rows are rebuilt whether or not the repository is still among them — that
// rebuild is also what takes the spinner off, and a row that vanished mid-sync
// (a reload, a drill-down) must not leave the ones still on screen stale.
func (m *Model) applyGitStatus(repoPath string, status Entry) {
	entries := m.entries()
	for i := range entries {
		if entries[i].Path != repoPath || !status.IsGitRepo {
			continue
		}
		entries[i].GitBranch = status.GitBranch
		entries[i].GitRemote = status.GitRemote
		entries[i].GitRemoteURL = status.GitRemoteURL
		entries[i].GitModified = status.GitModified
		entries[i].GitUntracked = status.GitUntracked
		entries[i].GitUnpushed = status.GitUnpushed
		entries[i].GitUnpulled = status.GitUnpulled
	}
	m.setEntries(entries)
}

// tokenForRemote returns the configured GitLab token when the repository's
// remote is on the configured GitLab host, and "" otherwise.
//
// A personal access token authenticates one host. The workspaces view holds
// whatever the user has cloned — GitHub, a customer's Gitea, a bare path on a
// network share — and offering DevDesk's GitLab credential to any of them would
// hand it to a third party over the wire. So the host has to match, and a
// repository elsewhere fetches anonymously or fails saying so.
func tokenForRemote(remoteURL, gitlabURL string, load func() string) string {
	if !git.SameHost(remoteURL, gitlabURL) {
		return ""
	}
	return load()
}
