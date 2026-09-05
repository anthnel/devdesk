package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// The selection rule of §3.61 is that an action tool is the headless form of an
// entry in the uppercase vocabulary. A tool whose key is not declared there is
// one somebody invented, and this is what refuses it.
func TestEveryActionToolMapsToADeclaredKey(t *testing.T) {
	actions := keymap.Actions()

	for tool, key := range mcpActionKeys() {
		if _, ok := actions[key]; !ok {
			t.Errorf("action tool %q claims key %q, which is not in the declared vocabulary", tool, key)
		}
	}

	// And the other way: a tool in the table with no key beside it has no
	// justification for existing.
	for tool := range mcpActionTable() {
		if _, ok := mcpActionKeys()[tool]; !ok {
			t.Errorf("action tool %q is routed but names no key it is the headless form of", tool)
		}
	}
	for tool := range mcpActionKeys() {
		if _, ok := mcpActionTable()[tool]; !ok {
			t.Errorf("action tool %q names a key but is routed nowhere", tool)
		}
	}
}

// §3.61 keeps a whole class of action out rather than guarding it: an action
// never registered cannot be wrongly confirmed. This is that list, opposed to
// what is actually routed.
//
// `M` and `N` are here beside the destructive four, and the reason is not that
// they destroy: their only undo is `D`, which is not exposed. An action made
// irreversible by removing its inverse is worse than a destructive one owned
// up to.
func TestNoDestructiveActionIsRouted(t *testing.T) {
	forbidden := map[string]string{
		"D": "deleting",
		"P": "pruning",
		"K": "killing a container — jobs_cancel stops a run, which is not the same key",
		"M": "renaming, whose only undo is a delete that is not exposed",
		"N": "creating, whose only undo is a delete that is not exposed",
		"U": "logging in and out, which touches the secret store",
		"X": "excluding a finding, which writes configuration",
	}

	for tool, key := range mcpActionKeys() {
		if why, bad := forbidden[key]; bad {
			t.Errorf("action tool %q exposes %q — %s", tool, key, why)
		}
	}
}

// An action reaches the view the way a key does, and comes back with the
// identifier of the run the registry allocated. Nothing else knows that number:
// it is minted in handleStartJobs, one Update after the request.
func TestAnActionIsAnsweredWithTheIDOfItsOwnRun(t *testing.T) {
	a := router(t, &bareView{})

	reply := make(chan mcpStartReply, 1)
	a.pendingInvocations = map[invocationID]chan mcpStartReply{"7": reply}

	// What the view would return: a start command carrying the invocation.
	cmd := jobs.WithInvocation("7", jobs.Start(
		jobs.Run{Kind: jobs.KindScan, Origin: command.ViewWorkspaces, Label: "~/work",
			Items: []jobs.Item{{Target: "/repo"}}},
		nil,
	))
	start, ok := cmd().(jobs.StartMsg)
	if !ok {
		t.Fatalf("WithInvocation produced %T, want jobs.StartMsg", start)
	}
	if start.Invocation != "7" {
		t.Fatalf("the run was not stamped with the invocation: %q", start.Invocation)
	}

	a.handleStartJobs(start)

	select {
	case got := <-reply:
		if got.err != nil {
			t.Fatalf("the action came back with an error: %v", got.err)
		}
		if got.id == 0 {
			t.Error("the action came back with no job id")
		}
	case <-time.After(time.Second):
		t.Fatal("handleStartJobs did not answer the invocation that asked for the run")
	}

	if len(a.pendingInvocations) != 0 {
		t.Error("the settled invocation was left in the pending map")
	}
}

// A keypress carries no invocation, and must not be answered as if it did —
// that is the whole reason the identifier travels on the run rather than being
// matched to "the next StartMsg".
func TestAKeyboardLaunchAnswersNobody(t *testing.T) {
	a := router(t, &bareView{})
	reply := make(chan mcpStartReply, 1)
	a.pendingInvocations = map[invocationID]chan mcpStartReply{"7": reply}

	a.handleStartJobs(jobs.StartMsg{
		Run: jobs.Run{Kind: jobs.KindScan, Items: []jobs.Item{{Target: "/repo"}}},
	})

	select {
	case got := <-reply:
		t.Fatalf("a keypress was handed the invocation's answer: %+v", got)
	default:
	}
	if len(a.pendingInvocations) != 1 {
		t.Error("a keypress consumed a pending invocation")
	}
}

// A view that will not honour the request answers with its own sentence — the
// same one the header greys the shortcut with.
func TestARefusalCarriesTheViewsOwnReason(t *testing.T) {
	a := router(t, &bareView{})
	reply := make(chan mcpStartReply, 1)
	a.pendingInvocations = map[invocationID]chan mcpStartReply{"7": reply}

	a.handleMCPRefused(jobs.RefusedMsg{Invocation: "7", Reason: "No scanner available"})

	got := <-reply
	if got.err == nil || !strings.Contains(got.err.Error(), "No scanner available") {
		t.Errorf("err = %v, want the view's reason", got.err)
	}
	if len(a.pendingInvocations) != 0 {
		t.Error("the refused invocation was left in the pending map")
	}
}

// A tool name nothing routes is refused rather than silently dropped, and the
// caller is not left waiting on a channel nobody will write.
func TestAnUnroutedActionIsRefusedAtOnce(t *testing.T) {
	a := router(t, &bareView{})

	reply := make(chan mcpStartReply, 1)
	a.handleMCPStartRequest(mcpStartRequestMsg{
		id:    "1",
		act:   mcpserver.Action{Tool: "container_delete"},
		reply: reply,
	})

	got := <-reply
	if got.err == nil {
		t.Fatal("an unrouted tool was accepted")
	}
	if len(a.pendingInvocations) != 0 {
		t.Error("an unrouted tool left an entry in the pending map")
	}
}

// The target view is built on demand: views are lazy, and an agent asking for a
// scan before anyone has opened `ws` is the ordinary case.
func TestAnActionBuildsItsViewIfNobodyHasOpenedIt(t *testing.T) {
	a := router(t, &bareView{})
	if _, ok := a.views[command.ViewWorkspaces]; ok {
		t.Fatal("the workspaces view already existed; this test cannot say anything")
	}

	a.handleMCPStartRequest(mcpStartRequestMsg{
		id:    "1",
		act:   mcpserver.Action{Tool: "workspace_scan_start"},
		reply: make(chan mcpStartReply, 1),
	})

	if _, ok := a.views[command.ViewWorkspaces]; !ok {
		t.Error("the action did not build the view that owns it")
	}
}

// The request that reaches the view is the one it understands, carrying the
// invocation — the view stamps it onto the run, which is what closes the loop.
func TestTheRequestReachingTheViewCarriesTheInvocation(t *testing.T) {
	_, msg, ok := mcpAction(mcpserver.Action{Tool: "workspace_scan_start", Targets: []string{"/repo"}}, "42")
	if !ok {
		t.Fatal("workspace_scan_start is not routed")
	}

	req, ok := msg.(workspaces.ScanRequestedMsg)
	if !ok {
		t.Fatalf("the view was sent %T", msg)
	}
	if req.Invocation != "42" {
		t.Errorf("invocation = %q, want 42", req.Invocation)
	}
	if len(req.Paths) != 1 || req.Paths[0] != "/repo" {
		t.Errorf("paths = %v, want the targets asked for", req.Paths)
	}
}

// Cancelling decides nothing of its own: Registry.Cancel answers, and what it
// means differs by kind (D7 of §3.58) — the queue always stops, and work
// already in flight is cut only where cutting leaves nothing behind. So a clone
// is stopped as a queue and its running git is waited out; the tool must not
// claim more than that, and must not refuse either.
func TestCancellingStopsTheQueueWhateverTheKind(t *testing.T) {
	for _, kind := range []jobs.Kind{jobs.KindScan, jobs.KindClone} {
		t.Run(string(kind), func(t *testing.T) {
			a := router(t, &bareView{})
			id := a.jobs.Start(jobs.Run{
				Kind:  kind,
				Items: []jobs.Item{{Target: "one", State: jobs.ItemRunning}, {Target: "two", State: jobs.ItemQueued}},
			})

			reply := make(chan error, 1)
			a.handleMCPCancelRequest(mcpCancelRequestMsg{id: id, reply: reply})

			if err := <-reply; err != nil {
				t.Fatalf("the queue refused to stop: %v", err)
			}
			// The queued item will not run, which is the half that always
			// holds. Whether the running one was cut is the kind's business.
			for _, item := range a.jobs.Snapshot()[0].Items {
				if item.Target == "two" && item.State != jobs.ItemSkipped {
					t.Errorf("the queued item is %q, want it skipped", item.State)
				}
			}
		})
	}
}

// A run that has already settled cannot be stopped, and says so rather than
// reporting a cancellation that did nothing.
func TestCancellingASettledRunIsRefused(t *testing.T) {
	a := router(t, &bareView{})
	id := a.jobs.Start(jobs.Run{
		Kind:  jobs.KindScan,
		Items: []jobs.Item{{Target: "/repo", State: jobs.ItemDone}},
	})

	reply := make(chan error, 1)
	a.handleMCPCancelRequest(mcpCancelRequestMsg{id: id, reply: reply})

	if err := <-reply; err == nil {
		t.Error("a finished run was reported as stopped")
	}
}

// Update must never block, on this path any more than on the reading one.
func TestUpdateNeverBlocksOnAnAbandonedActionReply(t *testing.T) {
	a := router(t, &bareView{})
	a.pendingInvocations = map[invocationID]chan mcpStartReply{
		"7": make(chan mcpStartReply, 1),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.settleInvocation("7", 3, nil)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked answering an invocation nobody reads")
	}
}

// A dispatcher with no program refuses both halves rather than panicking.
func TestActionsRefuseWithoutASession(t *testing.T) {
	d := mcpDispatcher{}

	if _, err := d.Start(context.Background(), mcpserver.Action{Tool: "workspace_scan_start"}); err == nil {
		t.Error("Start answered without a session")
	}
	if err := d.Cancel(context.Background(), 1); err == nil {
		t.Error("Cancel answered without a session")
	}
}
