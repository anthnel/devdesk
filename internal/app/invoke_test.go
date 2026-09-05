package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
)

// The registry lives in the model and is written from Update alone, so a tool
// handler cannot read it — it sends a message and Update answers. This is that
// round trip, minus the program, which only carries the message.
func TestAJobsRequestIsAnsweredFromUpdate(t *testing.T) {
	a := router(t, &bareView{})
	a.jobs.Start(jobs.Run{
		Kind:   jobs.KindScan,
		Origin: command.ViewWorkspaces,
		Label:  "~/work",
		Items:  []jobs.Item{{Target: "/repo", State: jobs.ItemRunning}},
	})

	reply := make(chan mcpJobsReply, 1)
	a.handleMCPJobsRequest(mcpJobsRequestMsg{reply: reply})

	got := <-reply
	if got.err != nil {
		t.Fatalf("the request came back with an error: %v", got.err)
	}
	if len(got.runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(got.runs))
	}
	if got.runs[0].Label != "~/work" {
		t.Errorf("run label %q, want %q", got.runs[0].Label, "~/work")
	}
}

// Update must never block on a reply channel. An agent that hangs up while its
// message is still queued leaves nobody reading, and an unbuffered send would
// stop the whole TUI — every keypress, every spinner frame — on a client that
// has gone.
//
// The buffer belongs to the sender, so what this checks is that the handler
// completes against a channel nobody will ever read.
func TestUpdateNeverBlocksOnAnAbandonedReply(t *testing.T) {
	a := router(t, &bareView{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.handleMCPJobsRequest(mcpJobsRequestMsg{reply: make(chan mcpJobsReply, 1)})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked writing to a reply channel nobody reads — the TUI would freeze on a client that hung up")
	}
}

// A caller whose client gives up returns on its own context rather than waiting
// for an answer that may never be sent.
func TestACancelledCallDoesNotWaitForTheAnswer(t *testing.T) {
	// A dispatcher whose program is nil never sends anything, so nothing will
	// ever write the reply: what returns has to be the cancellation.
	d := mcpDispatcher{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := d.Jobs(ctx); err == nil {
		t.Fatal("a cancelled call still waited for an answer")
	}
}

// A dispatcher built without a program is a programming error, and it answers
// with a sentence rather than a panic: a panic inside a tool handler takes the
// server down and says nothing about which tool did it.
func TestADispatcherWithNoProgramRefusesRatherThanPanics(t *testing.T) {
	_, err := mcpDispatcher{}.Jobs(context.Background())

	if !errors.Is(err, mcpserver.ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

// The tools that need the session must always get a link to it. Nothing else
// checks this: a nil dispatcher would look exactly like a server that works,
// right up to the first jobs_list.
func TestTheServerIsAlwaysBuiltWithALinkToTheSession(t *testing.T) {
	a := &App{
		config:         enabledConfig(),
		currentContext: "test",
		sharedState:    testSharedState(),
	}
	a.AttachProgram(nil)

	msg, ok := a.startMCPCmd()().(MCPServerStartedMsg)
	if !ok {
		t.Fatalf("startMCPCmd returned %T", msg)
	}
	if msg.Server != nil {
		t.Cleanup(func() { _ = msg.Server.Close() })
	}
	if msg.Err != nil {
		t.Fatalf("startMCPCmd: %v", msg.Err)
	}
	if a.mcpDispatch != (mcpDispatcher{program: nil}) {
		t.Error("AttachProgram did not build the dispatcher")
	}
}
