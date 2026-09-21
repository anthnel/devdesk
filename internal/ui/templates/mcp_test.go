package templates

import (
	"errors"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// An agent's sync is the run a key starts: same kind, same origin, same item,
// carrying the invocation so the router can answer the caller with its id.
func TestAnAgentsSyncIsTheRunAKeyStarts(t *testing.T) {
	m := synced(t, entry("spring-api", "Spring API"))

	_, cmd := m.Update(SyncRequestedMsg{Slug: "spring-api", Invocation: "7"})

	start, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		t.Fatalf("the request produced %v, want a jobs.StartMsg", testutil.Msgs(cmd))
	}
	if start.Invocation != "7" {
		t.Errorf("invocation = %q, want it carried onto the run", start.Invocation)
	}
	run := start.Run
	if run.Kind != jobs.KindSync || run.Origin != command.ViewTemplates || run.Label != "Spring API" {
		t.Errorf("run = %+v, want the sync F starts", run)
	}
	if len(run.Items) != 1 || run.Items[0].Target != "spring-api" {
		t.Errorf("items = %+v, want the one template", run.Items)
	}
}

func TestASyncOfATemplateNotInTheCatalogIsRefusedWithTheReason(t *testing.T) {
	m := synced(t, entry("spring-api", "Spring API"))

	for slug, want := range map[string]string{
		"nope": "No template nope in the catalog — templates_list is what it holds",
		"":     "No template named",
	} {
		_, cmd := m.Update(SyncRequestedMsg{Slug: slug, Invocation: "1"})
		refused, ok := testutil.MsgOf[jobs.RefusedMsg](cmd)
		if !ok || refused.Invocation != "1" || refused.Reason != want {
			t.Errorf("slug %q: got %+v (%v), want a refusal %q", slug, refused, ok, want)
		}
		if _, started := testutil.MsgOf[jobs.StartMsg](cmd); started {
			t.Errorf("slug %q started a run", slug)
		}
	}
}

// One calculation, shared with the header: a sync already under way is refused
// with the sentence the shortcut is greyed with.
func TestASyncAlreadyRunningIsRefusedWithTheHeadersReason(t *testing.T) {
	m := synced(t, entry("spring-api", "Spring API"))
	start, m := pressF(t, m)
	r := jobs.New()
	r.Start(start.Run)
	m.jobs = r.Snapshot()

	_, cmd := m.Update(SyncRequestedMsg{Slug: "spring-api", Invocation: "2"})

	refused, ok := testutil.MsgOf[jobs.RefusedMsg](cmd)
	if !ok || refused.Reason != reasonSyncRunning {
		t.Errorf("got %+v (%v), want a refusal with %q", refused, ok, reasonSyncRunning)
	}
}

// The router builds this view on demand and building it is not filling it, so a
// request served against the empty model would refuse with a statement about a
// file the view has not read.
func TestARequestArrivingBeforeTheCatalogWaitsInsteadOfRefusing(t *testing.T) {
	path := catalogWith(t, entry("spring-api", "Spring API"))
	m := NewWithPath(config.Default(), nil, path)

	updated, cmd := m.Update(SyncRequestedMsg{Slug: "spring-api", Invocation: "1"})
	m = updated.(Model)

	if _, refused := testutil.MsgOf[jobs.RefusedMsg](cmd); refused {
		t.Fatal("the request was refused before the catalog had been read")
	}
	if len(m.pendingRequests) != 1 {
		t.Fatalf("the request was not held: %d pending", len(m.pendingRequests))
	}

	// The load it asked for lands, and the request goes back through Update.
	loaded := testutil.Msgs(loadCatalogCmd(path))[0]
	updated, cmd = m.Update(loaded)
	m = updated.(Model)
	if len(m.pendingRequests) != 0 {
		t.Error("the catalog landed and the request was still held")
	}
	replayed, ok := testutil.MsgOf[SyncRequestedMsg](cmd)
	if !ok || replayed.Invocation != "1" {
		t.Fatalf("the held request was not replayed: %v", testutil.Msgs(cmd))
	}

	_, cmd = m.Update(replayed)
	if _, started := testutil.MsgOf[jobs.StartMsg](cmd); !started {
		t.Errorf("the replayed request did not start a run: %v", testutil.Msgs(cmd))
	}
}

func TestSeveralRequestsBeforeTheCatalogAskForOneLoad(t *testing.T) {
	m := NewWithPath(config.Default(), nil, catalogWith(t, entry("a", "A")))

	updated, first := m.Update(SyncRequestedMsg{Slug: "a", Invocation: "1"})
	m = updated.(Model)
	_, second := m.Update(SyncRequestedMsg{Slug: "a", Invocation: "2"})

	if first == nil || second != nil {
		t.Errorf("first asked for a load: %v, second: %v — want one load for both", first != nil, second != nil)
	}
}

// A catalog that cannot be read answers what waited for it, or an agent hangs
// until its own timeout with the reason in a log it cannot see.
func TestAnUnreadableCatalogRefusesWhatWaitedForIt(t *testing.T) {
	m := NewWithPath(config.Default(), nil, catalogWith(t, entry("a", "A")))
	updated, _ := m.Update(SyncRequestedMsg{Slug: "a", Invocation: "1"})
	m = updated.(Model)

	updated, cmd := m.Update(CatalogLoadedMsg{Err: errors.New("bad yaml")})
	m = updated.(Model)

	refused, ok := testutil.MsgOf[jobs.RefusedMsg](cmd)
	if !ok || refused.Invocation != "1" || refused.Reason != reasonUnreadable {
		t.Errorf("got %+v (%v), want a refusal %q", refused, ok, reasonUnreadable)
	}
	if len(m.pendingRequests) != 0 {
		t.Error("the request was still held")
	}

	// And a request arriving after the failure is refused at once.
	_, cmd = m.Update(SyncRequestedMsg{Slug: "a", Invocation: "2"})
	if r, ok := testutil.MsgOf[jobs.RefusedMsg](cmd); !ok || r.Reason != reasonUnreadable {
		t.Errorf("a request after the failure got %+v (%v)", r, ok)
	}
}
