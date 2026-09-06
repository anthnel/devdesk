package workspaces

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The router builds this view on demand, and building it is not filling it:
// `New` returns an empty model and the entries only arrive from loadEntries(),
// which Init() dispatches asynchronously. A request served against that empty
// model resolved no path and refused with "No git repository in this context's
// workspaces directory" — a statement about the disk that the view was in no
// position to make. It recurred after every context switch, since
// reinitializeViews drops every view and only Inits the current one.
func TestARequestArrivingBeforeTheListingWaitsInsteadOfRefusing(t *testing.T) {
	m := New(config.Default(), nil)

	updated, cmd := m.Update(ScanRequestedMsg{Paths: []string{"/repo"}, Invocation: "1"})
	m = updated.(Model)

	// Nothing is refused — the request is held, and a listing is asked for.
	for _, msg := range testutil.Msgs(cmd) {
		if refused, ok := msg.(jobs.RefusedMsg); ok {
			t.Fatalf("the request was refused before the view had read anything: %q", refused.Reason)
		}
	}
	if len(m.pendingRequests) != 1 {
		t.Fatalf("the request was not held: %d pending", len(m.pendingRequests))
	}
}

// Once the listing lands, what waited goes back through Update as a message —
// so a request that waited takes exactly the path one that did not takes.
func TestAHeldRequestIsReplayedWhenTheListingLands(t *testing.T) {
	m := New(config.Default(), nil)

	updated, _ := m.Update(ScanRequestedMsg{Paths: []string{"/repo"}, Invocation: "1"})
	m = updated.(Model)

	updated, cmd := m.Update(EntriesLoadedMsg{Path: m.currentPath})
	m = updated.(Model)

	if len(m.pendingRequests) != 0 {
		t.Error("the listing landed and the request was still held")
	}

	var replayed bool
	for _, msg := range testutil.Msgs(cmd) {
		if req, ok := msg.(ScanRequestedMsg); ok && req.Invocation == "1" {
			replayed = true
		}
	}
	if !replayed {
		t.Error("the held request was dropped rather than replayed")
	}
}

// A listing that fails answers what waited for it. An agent would otherwise
// hang until its own timeout, with the reason in a log it cannot see.
func TestAFailedListingRefusesWhatWaitedForIt(t *testing.T) {
	m := New(config.Default(), nil)

	updated, _ := m.Update(ScanRequestedMsg{Invocation: "1"})
	m = updated.(Model)

	updated, cmd := m.Update(LoadErrorMsg{Path: m.currentPath, Error: errRead{}})
	m = updated.(Model)

	if len(m.pendingRequests) != 0 {
		t.Error("the listing failed and the request was still held")
	}

	var refused bool
	for _, msg := range testutil.Msgs(cmd) {
		if r, ok := msg.(jobs.RefusedMsg); ok && r.Invocation == "1" {
			refused = true
		}
	}
	if !refused {
		t.Error("nothing answered the request that waited for a listing that failed")
	}
}

// Several requests queueing behind one listing ask for one read, not one each:
// a second would race the first and the loser is dropped by the currentPath
// guard, taking nothing with it but a directory read.
func TestQueuedRequestsAskForOneListing(t *testing.T) {
	m := New(config.Default(), nil)

	updated, first := m.Update(ScanRequestedMsg{Invocation: "1"})
	m = updated.(Model)
	updated, second := m.Update(SyncRequestedMsg{Invocation: "2"})
	m = updated.(Model)

	if first == nil {
		t.Error("the first request did not ask for a listing")
	}
	if second != nil {
		t.Error("the second request asked for a listing of its own")
	}
	if len(m.pendingRequests) != 2 {
		t.Errorf("%d requests held, want 2", len(m.pendingRequests))
	}
}

type errRead struct{}

func (errRead) Error() string { return "permission denied" }

var _ tea.Msg = ScanRequestedMsg{}
