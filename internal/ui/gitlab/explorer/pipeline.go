package explorer

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/gitlab"
)

// The clone pipeline: a discovery walk feeding a bounded pool of clones
// (§3.16, decision 7).
//
// Building the whole list first and then cloning it would only move the dead
// screen one step earlier — the walk is what takes the minutes. Here a
// repository becomes a row the moment it is found and starts cloning as soon as
// a slot frees, so the list fills and the count climbs while the work happens.
//
// Nothing here touches the model. Everything the run needs is copied into a
// cloneSpec before the first goroutine starts, and the only way back is the
// event channel (Rule 110).

// cloneEventKind is what happened to one path.
type cloneEventKind int

const (
	// cloneFound: discovery turned up a repository. The row appears.
	cloneFound cloneEventKind = iota
	// cloneBegan: a slot freed and `git clone` started. The row spins.
	cloneBegan
	// cloneEnded: the clone finished, was skipped, or failed.
	cloneEnded
	// cloneWalkFailed: a group could not be listed. It is reported as its own
	// row rather than folded into a repository's error, because the repositories
	// under it were never discovered and no other row can stand for them.
	cloneWalkFailed
)

type cloneEvent struct {
	kind    cloneEventKind
	path    string
	skipped bool
	err     error
}

// cloneEventBuffer keeps the workers off the UI's critical path. Three events
// per repository, so this absorbs a burst of some seventy without a worker
// waiting on Update.
const cloneEventBuffer = 256

// cloneSpec is everything a run needs, read out of the model up front.
type cloneSpec struct {
	client    *gitlabclient.Client
	roots     []*TreeNode
	selection cloneSelection
	target    string
	// secrets is where the HTTPS token comes from. The storage rather than the
	// token itself, so the keyring read happens on the run's goroutine — Update
	// does no I/O.
	secrets         credentials.Storage
	cloneMethod     string
	gitlabURL       string
	jobs            int
	includeArchived bool
}

// cloneToken is the token the clones authenticate with, or "" for SSH and for
// a context whose secret store has nothing.
//
// A missing token is not an error here: a public repository clones without one,
// and a clone that does need it fails with git's own reason, on its own row.
func (s cloneSpec) cloneToken() string {
	if s.cloneMethod == "ssh" || s.secrets == nil {
		return ""
	}
	token, err := gitlab.NewAuth(s.secrets).LoadCredentials(s.gitlabURL)
	if err != nil {
		log.Printf("ERROR [explorer] loading the clone token: %v", err)
		return ""
	}
	return token
}

// cloneRun is a pipeline in flight.
type cloneRun struct {
	events <-chan cloneEvent
	// cancel stops **discovery only**. A `git clone` is never interrupted: a
	// context that kills one leaves half a repository on disk, which is exactly
	// the mess decision 12 exists to avoid. Cancelling therefore means the walk
	// stops and the scheduler issues no more work — the clones already running
	// are awaited, and the view says so.
	cancel context.CancelFunc
}

// startCloneRun launches the pipeline and returns immediately.
func startCloneRun(spec cloneSpec) *cloneRun {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan cloneEvent, cloneEventBuffer)
	go runPipeline(ctx, spec, events)
	return &cloneRun{events: events, cancel: cancel}
}

// runPipeline walks the roots and clones what the walk finds.
func runPipeline(ctx context.Context, spec cloneSpec, events chan<- cloneEvent) {
	defer close(events)

	// Once, here, rather than per clone: the keyring is not free, and four
	// workers hitting it at the same time is four prompts on some backends.
	token := spec.cloneToken()

	found := make(chan *TreeNode)
	go func() {
		defer close(found)
		for _, root := range spec.roots {
			discover(ctx, spec, root, found, events)
		}
	}()

	var wg sync.WaitGroup
	slots := make(chan struct{}, max(spec.jobs, 1))

	for node := range found {
		if ctx.Err() != nil {
			break
		}
		events <- cloneEvent{kind: cloneFound, path: node.FullPath}
		slots <- struct{}{}
		wg.Add(1)
		go func(n *TreeNode) {
			defer wg.Done()
			defer func() { <-slots }()

			events <- cloneEvent{kind: cloneBegan, path: n.FullPath}
			skipped, err := cloneOne(n, spec, token)
			events <- cloneEvent{kind: cloneEnded, path: n.FullPath, skipped: skipped, err: err}
		}(node)
	}

	// Drain rather than abandon. Breaking out of the loop above leaves the walker
	// parked on a send nobody will ever read, and a goroutine holding a live HTTP
	// client is a leak that outlives the view.
	//
	//nolint:revive // the drain is the point; there is nothing to do with a node here
	for range found {
	}
	wg.Wait()
}

// discover walks one root, sending every included project to the pool.
//
// The selection is consulted on the way down, so an excluded subtree costs no
// requests at all: the walk simply does not enter it.
func discover(ctx context.Context, spec cloneSpec, node *TreeNode, found chan<- *TreeNode, events chan<- cloneEvent) {
	if ctx.Err() != nil || !spec.selection.includes(node.FullPath) {
		return
	}

	if node.Type == NodeTypeProject {
		select {
		case found <- node:
		case <-ctx.Done():
		}
		return
	}

	children, err := discoverGroupChildren(spec.client, node, spec.includeArchived)
	if err != nil {
		// A cancelled walk reports nothing: the error is the cancellation, and a
		// failed row for it would read as a repository nobody could clone.
		if ctx.Err() == nil {
			events <- cloneEvent{kind: cloneWalkFailed, path: node.FullPath, err: err}
		}
		return
	}
	for _, child := range children {
		discover(ctx, spec, child, found, events)
	}
}

// cloneOne clones a repository, or reports that it was already there.
//
// The destination mirrors the forge path in full (decision 4). Keeping only the
// last segment, as the old pull did, made `acme/platform` and `other/platform`
// land on the same directory and merge — which could not happen while one
// subtree was cloned at a time, and can now that several roots are confirmed at
// once.
func cloneOne(node *TreeNode, spec cloneSpec, token string) (skipped bool, err error) {
	dir := filepath.Join(spec.target, filepath.FromSlash(node.FullPath))
	if git.DirExists(dir) {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(dir), err)
	}
	url := cloneURL(spec.gitlabURL, spec.cloneMethod, node.FullPath)
	if err := git.Clone(url, dir, git.CloneOptions{Token: token}); err != nil {
		return false, err
	}
	return false, nil
}

// cloneURL builds the git URL for a project from the configured GitLab URL and
// the clone method. SSH form takes the host alone, so the scheme is stripped.
func cloneURL(gitlabURL, cloneMethod, fullPath string) string {
	cleanURL := strings.TrimSuffix(gitlabURL, "/")
	if cloneMethod != "ssh" {
		return fmt.Sprintf("%s/%s.git", cleanURL, fullPath)
	}

	host := strings.TrimPrefix(cleanURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	return fmt.Sprintf("git@%s:%s.git", host, fullPath)
}

// waitForCloneEvent blocks on the run's channel and hands one event to Update.
// It is re-issued for each event, which is what keeps the model mutation in
// Update and the blocking read out of it.
func waitForCloneEvent(events <-chan cloneEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return CloneRunFinishedMsg{}
		}
		return CloneEventMsg{event: event}
	}
}
