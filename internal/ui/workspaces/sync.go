package workspaces

import (
	"log"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/git"
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
func (m Model) busy(path string) bool {
	return m.scanningPaths[path] || m.syncingPaths[path] || m.deletingPaths[path]
}

// anyBusy reports whether any operation is running at all. It is what decides
// whether the spinner chain keeps going, and it is one function rather than
// the predicate written out at each site — a fourth map would otherwise have
// to be remembered in three places.
func (m Model) anyBusy() bool {
	return len(m.scanningPaths) > 0 || len(m.syncingPaths) > 0 || len(m.deletingPaths) > 0
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

	m.sync = &syncRun{total: len(toSync), unreadable: entry.SubRepoSkipped}
	return m, batchSyncCmd(toSync, m.syncSpec())
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
func (m Model) handleWorkspaceSyncComplete(msg WorkspaceSyncCompleteMsg) (tea.Model, tea.Cmd) {
	delete(m.syncingPaths, msg.RepoPath)

	// The listing may have been reloaded or navigated away from while the sync
	// ran. The run's counters still apply either way — they are about the
	// batch, not about what is on screen.
	m.applyGitStatus(msg.RepoPath, msg.Status)

	if m.sync != nil {
		m.sync.record(msg)
	}
	if msg.Error != nil {
		log.Printf("ERROR [workspaces] sync %s: %v", msg.RepoPath, msg.Error)
	}

	if m.sync != nil && m.sync.finished() {
		return m, clearSyncSummaryCmd()
	}
	return m, nil
}

// syncRun is one batch in flight, and the summary that outlives it.
type syncRun struct {
	total    int
	done     int
	updated  int
	upToDate int
	skipped  int
	failed   int

	// unreadable is not a sync outcome. It counts the directories the *walk*
	// could not read before the batch started, so a repository under one of
	// them was never a target at all. It rides on the run rather than going to
	// the footer as a Warn because the run's line is the one that survives the
	// three-second timer — and because "12 repositories synced" is a different
	// claim from "12 repositories synced, and I could not look in 3 places".
	unreadable int

	// firstSkipped and firstFailed name one repository each. The footer is one
	// line, so it names one and counts the rest; the log has them all.
	firstSkipped       string
	firstSkippedReason string
	firstFailed        string
}

func (r *syncRun) finished() bool { return r.done >= r.total }

func (r *syncRun) record(msg WorkspaceSyncCompleteMsg) {
	r.done++
	switch {
	case msg.Error != nil:
		r.failed++
		if r.firstFailed == "" {
			r.firstFailed = pathBaseName(msg.RepoPath)
		}
	case msg.Outcome == git.SyncUpdated:
		r.updated++
	case msg.Outcome == git.SyncSkipped:
		r.skipped++
		if r.firstSkipped == "" {
			r.firstSkipped = pathBaseName(msg.RepoPath)
			r.firstSkippedReason = msg.Reason
		}
	default:
		r.upToDate++
	}
}

// syncStatusLine is what the footer says while a batch runs, and after it.
//
// It is rendered from the run rather than assigned to footerInfo because a
// batch outlives the three-second timer that clears footer messages: a progress
// line set on the first repository would vanish while the tenth was still
// fetching (Rule 128).
func (m Model) syncStatusLine() string {
	if m.sync == nil {
		return ""
	}
	if !m.sync.finished() {
		return "Syncing — " + strconv.Itoa(m.sync.done) + "/" + strconv.Itoa(m.sync.total)
	}

	var parts []string
	if m.sync.updated > 0 {
		parts = append(parts, plural(m.sync.updated, "repository", "repositories")+" updated")
	}
	if m.sync.upToDate > 0 {
		parts = append(parts, strconv.Itoa(m.sync.upToDate)+" up to date")
	}
	if m.sync.skipped > 0 {
		parts = append(parts, strconv.Itoa(m.sync.skipped)+" skipped ("+
			m.sync.firstSkipped+": "+m.sync.firstSkippedReason+")")
	}
	if m.sync.failed > 0 {
		parts = append(parts, strconv.Itoa(m.sync.failed)+" failed ("+m.sync.firstFailed+") — check logs")
	}
	if m.sync.unreadable > 0 {
		parts = append(parts, plural(m.sync.unreadable, "directory", "directories")+" unreadable — check logs")
	}
	if len(parts) == 0 {
		return "Nothing to sync"
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
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
