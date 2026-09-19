package templates

import (
	"context"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Syncing a template reads its source again and replaces the cached copy that a
// preview, a scan and a repository creation all start from. Without it a branch
// that moved on, or a local repository that gained a commit, would be served as
// it was the first time it was read: those readers take the cache and never
// look at the source.

const reasonSyncRunning = "A sync of this template is already running"

// syncTimeout bounds one sync: a fetch that cannot finish should say so.
const syncTimeout = 2 * time.Minute

// SyncStartingMsg says the sync began; it moves the item from queued to running.
type SyncStartingMsg struct{ Slug string }

// SyncCompleteMsg is the sync's outcome.
type SyncCompleteMsg struct {
	Slug  string
	Name  string
	Files int
	Err   error
}

func (m SyncStartingMsg) Transition() jobs.Transition {
	return jobs.Transition{Kind: jobs.KindSync, Target: m.Slug, State: jobs.ItemRunning}
}

func (m SyncCompleteMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindSync, Target: m.Slug, State: jobs.ItemDone}
	if m.Err != nil {
		t.State = jobs.ItemFailed
		t.Detail = "sync failed — check logs"
	}
	return t
}

var (
	_ jobs.Reporter = SyncStartingMsg{}
	_ jobs.Reporter = SyncCompleteMsg{}
)

// syncJob is everything the sync needs, read in Update and carried into the Cmd
// (Rule 110).
type syncJob struct {
	entry template.Entry
	creds template.Credentials
	cache template.Cache
}

func syncTemplateCmd(job syncJob) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg { return SyncStartingMsg{Slug: job.entry.Slug} },
		func() tea.Msg {
			complete := SyncCompleteMsg{Slug: job.entry.Slug, Name: job.entry.Name}
			ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
			defer cancel()

			files, err := job.cache.Sync(ctx, job.entry.Slug, job.entry.Source, job.creds)
			if err != nil {
				log.Printf("ERROR [templates] sync %s: %v", job.entry.Slug, err)
				complete.Err = err
				return complete
			}
			complete.Files = len(files)
			return complete
		},
	)
}

// syncState says whether the selected template can be synced: a sync of it is
// not already running. Whoever started it, the registry is one bookkeeping.
func (m Model) syncState() shortcut.Availability {
	if entry, ok := m.selectedEntry(); ok && m.working(jobs.KindSync, entry.Slug) {
		return shortcut.Unavailable(reasonSyncRunning)
	}
	return shortcut.Availability{}
}

// startSync launches a sync of the selected template.
func (m Model) startSync() (tea.Model, tea.Cmd) {
	if reason := m.refusal(m.availability().Sync); reason != "" {
		return m, m.footer.Warn(reason)
	}
	entry, _ := m.selectedEntry()
	job := syncJob{
		entry: entry,
		creds: template.CredentialsFor(m.config, m.secrets, entry.Source),
		cache: m.cache,
	}
	run := jobs.NewRun(jobs.KindSync, command.ViewTemplates, "", entry.Name, entry.Slug)
	return m, jobs.Start(run, syncTemplateCmd(job))
}

// handleSyncComplete says how the sync ended.
func (m Model) handleSyncComplete(msg SyncCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, m.footer.Error("Sync of " + msg.Name + " failed — check logs")
	}
	return m, m.footer.Info("Synced " + msg.Name + " — " + sharedcomponents.Plural(msg.Files, "file", "files"))
}
