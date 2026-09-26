package app

import (
	"context"
	"errors"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// The forge index (internal/forgeindex) is the router's to build and to keep,
// for the same reason the scanners' detection is (scan_tools.go): it answers
// for the session, not for a view, and a view can be dropped and rebuilt at
// any time while the session goes on.
//
// A session reads the file the previous walk left first, and walks the forge
// only when that file cannot stand in for a walk: missing, written for another
// host or account, older than forgeIndexMaxAge, or incomplete. Switching away
// from a context and back within minutes therefore costs no walk at all. The
// file is only taken while nothing fresher is installed; a walk always replaces
// what is there. A generation number retires both when the session changes
// under them.

// forgeIndexMaxAge is how old the file may be and still spare a session its
// walk. The file already carries every edit made through the explorer since
// that walk; what it misses is what changed on the forge by other means, and
// ctrl+r in the explorer walks again whatever the file's age.
//
// A var rather than a const so a test can move the boundary.
var forgeIndexMaxAge = 15 * time.Minute

// forgeIndexState is the router's bookkeeping of the index, kept apart so App
// reads as the list of concerns it holds.
type forgeIndexState struct {
	// forgeIndexGen numbers sessions; an answer from an older one is dropped.
	forgeIndexGen int
	// forgeIndexCancel stops the walk in flight, nil when there is none.
	forgeIndexCancel context.CancelFunc
	// forgeIndexFresh says the walk of this session has landed, so the file,
	// if it is slower than the walk, must not overwrite it.
	forgeIndexFresh bool
	// forgeIndexPending are the views' edits received while a walk is in
	// flight. The walk read the forge before them, so its answer is replayed
	// through them before it is installed — otherwise a create, a delete or a
	// level just re-read would be undone by an index older than they are.
	forgeIndexPending []func(*forgeindex.Index) *forgeindex.Index
}

// forgeIndexLoadedMsg is the file read at session start.
type forgeIndexLoadedMsg struct {
	gen   int
	index *forgeindex.Index
	err   error
}

// forgeIndexStartedMsg says the walk is under way, so `:jobs` shows it running
// rather than queued. It is the first half of a Sequence with the walk, which
// is what guarantees it lands before the walk's own answer.
type forgeIndexStartedMsg struct {
	target string
	cancel context.CancelFunc
}

// forgeIndexBuiltMsg is the walk's answer.
type forgeIndexBuiltMsg struct {
	gen    int
	target string
	index  *forgeindex.Index
	err    error
}

// forgeIndexOwner is what an index is keyed on besides the host: the account.
// The ID when the forge gives one, since a username can be renamed.
func forgeIndexOwner(user forge.User) string {
	if user.ID != "" {
		return user.ID
	}
	return user.Username
}

// startForgeIndex reads the last index from disk; whether the forge is walked
// is decided once the file is in (handleForgeIndexLoaded). It is called when a
// session opens, whichever way it opened.
func (a *App) startForgeIndex() tea.Cmd {
	a.stopForgeIndex()
	if a.sharedState.Forge == nil {
		return nil
	}
	return a.loadForgeIndex()
}

// forgeIndexNeedsWalk reports whether ix, read from disk, cannot stand in for
// a walk. nil covers a missing or unreadable file as well as one written for
// another host or account (loadForgeIndex drops those). An index with Unlisted
// namespaces is missing their content, so it is walked again however recent.
func forgeIndexNeedsWalk(ix *forgeindex.Index, now time.Time) bool {
	return ix == nil || ix.Skipped() > 0 || now.Sub(ix.BuiltAt) > forgeIndexMaxAge
}

// handleForgeIndexRefresh walks the forge again, keeping the index in place
// until the new one lands. A walk already under way is the answer to the
// request, so a second one is not started beside it.
func (a *App) handleForgeIndexRefresh() (tea.Model, tea.Cmd) {
	if a.sharedState.Forge == nil || a.forgeIndexCancel != nil {
		return a, nil
	}
	return a, a.walkForgeIndex()
}

// loadForgeIndex reads the file the previous walk left, if it was written for
// this host and this account.
func (a *App) loadForgeIndex() tea.Cmd {
	gen := a.forgeIndexGen
	host := a.config.Forge.URL
	owner := forgeIndexOwner(a.sharedState.CurrentUser)
	path := forgeindex.Path(a.currentContext)
	return func() tea.Msg {
		ix, err := forgeindex.Load(path)
		if ix != nil && !ix.Matches(host, owner) {
			ix = nil
		}
		return forgeIndexLoadedMsg{gen: gen, index: ix, err: err}
	}
}

// walkForgeIndex starts the walk as a job: `:jobs` lists it, and can stop it.
func (a *App) walkForgeIndex() tea.Cmd {
	backend := a.sharedState.Forge
	gen := a.forgeIndexGen
	host := a.config.Forge.URL
	owner := forgeIndexOwner(a.sharedState.CurrentUser)

	ctx, cancel := context.WithCancel(context.Background())
	a.forgeIndexCancel = cancel
	run := jobs.NewRun(jobs.KindIndex, command.ViewGitExplorer, "", host, host)
	work := tea.Sequence(
		func() tea.Msg { return forgeIndexStartedMsg{target: host, cancel: cancel} },
		func() tea.Msg {
			ix, err := forgeindex.Build(ctx, backend, host, owner, time.Now())
			return forgeIndexBuiltMsg{gen: gen, target: host, index: ix, err: err}
		},
	)
	return jobs.StartCancellable(run, work, cancel)
}

// stopForgeIndex retires the session's index: a walk in flight is cancelled,
// and any answer still on its way is dropped by the generation check. The
// file stays — it is the next session's head start.
func (a *App) stopForgeIndex() {
	a.forgeIndexGen++
	if a.forgeIndexCancel != nil {
		a.forgeIndexCancel()
		a.forgeIndexCancel = nil
	}
	a.forgeIndexFresh = false
	a.forgeIndexPending = nil
	a.sharedState.ForgeIndex = nil
}

func (a *App) handleForgeIndexLoaded(msg forgeIndexLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		log.Printf("ERROR [app/forge-index] read: %v", msg.err)
	}
	if msg.gen != a.forgeIndexGen || a.forgeIndexFresh {
		return a, nil
	}
	var cmds []tea.Cmd
	if msg.index != nil {
		cmds = append(cmds, a.installForgeIndex(msg.index, false))
	}
	// A walk already out — ctrl+r before the file came back — is the walk.
	if a.forgeIndexCancel == nil && forgeIndexNeedsWalk(msg.index, time.Now()) {
		cmds = append(cmds, a.walkForgeIndex())
	}
	return a, tea.Batch(cmds...)
}

func (a *App) handleForgeIndexStarted(msg forgeIndexStartedMsg) (tea.Model, tea.Cmd) {
	t := jobs.Transition{Kind: jobs.KindIndex, Target: msg.target, State: jobs.ItemRunning, Cancel: msg.cancel}
	if !a.jobs.Apply(t) {
		return a, nil
	}
	return a, a.jobsChanged()
}

// handleForgeIndexBuilt settles the job whatever the generation — the run is
// in the registry either way — and installs the index only for the session
// that asked for it.
func (a *App) handleForgeIndexBuilt(msg forgeIndexBuiltMsg) (tea.Model, tea.Cmd) {
	t := jobs.Transition{Kind: jobs.KindIndex, Target: msg.target, State: jobs.ItemDone}
	switch {
	case errors.Is(msg.err, context.Canceled):
		t.State, t.Detail = jobs.ItemSkipped, "cancelled"
	case msg.err != nil:
		log.Printf("ERROR [app/forge-index] walk: %v", msg.err)
		t.State, t.Detail = jobs.ItemFailed, msg.err.Error()
	case msg.index.Skipped() > 0:
		t.Detail = components.Plural(msg.index.Skipped(), "namespace", "namespaces") + " could not be listed"
	}

	var cmds []tea.Cmd
	if a.jobs.Apply(t) {
		cmds = append(cmds, a.jobsChanged())
	}
	if msg.gen == a.forgeIndexGen {
		a.forgeIndexCancel = nil
		pending := a.forgeIndexPending
		a.forgeIndexPending = nil
		if msg.err == nil {
			ix := msg.index
			for _, edit := range pending {
				ix = edit(ix)
			}
			a.forgeIndexFresh = true
			cmds = append(cmds, a.installForgeIndex(ix, true))
		}
	}
	return a, tea.Batch(cmds...)
}

// handleForgeIndexEdit applies a view's edit to the current index, and keeps
// it for the walk in flight if there is one. An edit that changes nothing
// returns the same index, and then nothing is written or broadcast: re-reading
// a level the forge left as it was is the common case, and it must not cost a
// rewrite of the whole file.
func (a *App) handleForgeIndexEdit(msg shared.ForgeIndexEditMsg) (tea.Model, tea.Cmd) {
	if msg.Edit == nil {
		return a, nil
	}
	if a.forgeIndexCancel != nil {
		a.forgeIndexPending = append(a.forgeIndexPending, msg.Edit)
	}
	current := a.sharedState.ForgeIndex
	if current == nil {
		return a, nil
	}
	next := msg.Edit(current)
	if next == current {
		return a, nil
	}
	return a, a.installForgeIndex(next, true)
}

// installForgeIndex makes ix the session's index, tells every view, and writes
// it to disk when it is not the file itself coming back.
func (a *App) installForgeIndex(ix *forgeindex.Index, save bool) tea.Cmd {
	a.sharedState.ForgeIndex = ix
	cmds := []tea.Cmd{a.broadcast(shared.ForgeIndexChangedMsg{})}
	if save {
		path := forgeindex.Path(a.currentContext)
		cmds = append(cmds, func() tea.Msg {
			if err := forgeindex.Save(path, ix); err != nil {
				log.Printf("ERROR [app/forge-index] write: %v", err)
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}

// broadcast hands msg to every view the router holds, on screen or not —
// broadcastJobs's reasoning.
func (a *App) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.views))
	for name, view := range a.views {
		updated, cmd := view.Update(msg)
		a.views[name] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}
