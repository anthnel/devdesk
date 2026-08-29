package workspaces

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// D68: a batch of scans runs for minutes and the context can change while it
// does. The results belong to the context the batch was launched in, and every
// command of the launch has to agree on which one that is.
//
// These drive the two writers directly rather than through Update: the launch
// site returns a jobs.StartMsg whose builder the router calls, and running what
// the builder returns would run Trivy.

// switchedTo makes name the current context, which is what the old code read at
// the end of a scan, and puts the previous one back when the test ends.
//
// The restore is not tidiness: the current context is a file under the home
// directory, so it is shared by every test in the package, and a title asserting
// on it is one of them.
func switchedTo(t *testing.T, name string) {
	t.Helper()
	before := config.CurrentContextName()
	if err := config.SetCurrentContext(name); err != nil {
		t.Fatalf("set the current context to %q: %v", name, err)
	}
	t.Cleanup(func() { _ = config.SetCurrentContext(before) })
}

func workspaceCache(t *testing.T, contextName string) *cache.WorkspaceScanCache {
	t.Helper()
	c, err := cache.NewWorkspaceScanCache(contextName)
	if err != nil {
		t.Fatalf("open the workspace cache for context %q: %v", contextName, err)
	}
	return c
}

func TestAScanLandsInTheContextItWasLaunchedIn(t *testing.T) {
	const repoPath = "/srv/stamped-repo"
	switchedTo(t, "launched-in")
	entry := cache.WorkspaceScanEntry{
		RepoPath:  repoPath,
		Critical:  4,
		ScannedAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
	}

	// The switch happens between the launch and the write, which is the whole
	// of the defect: the scan was dispatched under "launched-in" and finishes
	// while the user is looking at "switched-to".
	switchedTo(t, "switched-to")
	storeWorkspaceScan("launched-in", repoPath, entry)

	if got := workspaceCache(t, "launched-in").Get(repoPath); got == nil || got.Critical != 4 {
		t.Errorf("the launching context holds %+v, want the scan it asked for", got)
	}
	if got := workspaceCache(t, "switched-to").Get(repoPath); got != nil {
		t.Errorf("the context switched to during the scan holds %+v, want nothing", got)
	}
}

// The purge is half of ctrl+a and it travels in the same builder as the scan
// that replaces it. Reading the current context on its own goroutine is what
// let it blank one context's counts while the scan filled another's.
func TestAPurgeReachesTheSameContextAsItsScan(t *testing.T) {
	const repoPath = "/srv/purge-stamped"
	switchedTo(t, "launched-in")
	other := workspaceCache(t, "switched-to")
	if err := other.Set(repoPath, cache.WorkspaceScanEntry{RepoPath: repoPath, Critical: 9}); err != nil {
		t.Fatalf("seed the other context: %v", err)
	}
	launching := workspaceCache(t, "launched-in")
	if err := launching.Set(repoPath, cache.WorkspaceScanEntry{RepoPath: repoPath, Critical: 1}); err != nil {
		t.Fatalf("seed the launching context: %v", err)
	}

	switchedTo(t, "switched-to")
	testutil.Msgs(deleteScanCacheCmd([]string{repoPath}, "launched-in"))

	launching.Reload()
	if got := launching.Get(repoPath); got != nil {
		t.Errorf("the launching context kept %+v, want it purged", got)
	}
	other.Reload()
	if got := other.Get(repoPath); got == nil || got.Critical != 9 {
		t.Errorf("the other context holds %+v, want its own entry untouched", got)
	}
}
