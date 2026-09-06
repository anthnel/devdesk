package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
)

// The MCP invocation loop.
//
// A tool handler runs on the HTTP server's own goroutine, and the answer it
// needs is in the router's model. There is exactly one legal way across:
// tea.Program.Send, and then Update() replies (Rule 110).
//
//	tool handler  →  p.Send(mcpJobsRequestMsg{reply})
//	                   ↓
//	                 Update()  — reads the registry, writes the channel
//	                   ↓
//	tool handler  ←  reply
//
// **The reply channel is buffered to one**, and that is the load-bearing
// detail: Update() must never block. An agent that hangs up while its message
// is still queued leaves nobody reading the channel, and an unbuffered send
// from Update would stop the whole TUI — every keypress, every spinner frame —
// on a client that has gone. With a buffer of one the send always completes and
// the channel is collected when both sides let go, so there is no registry of
// pending calls to keep and nothing to leak.

// mcpDispatcher is the mcp package's Dispatcher, implemented over the program
// handle.
//
// It is a value holding the program rather than a method on *App, deliberately:
// it is called from a goroutine that is not Update's, so it must be unable to
// reach a field of the model even by accident. The program handle is written
// once by AttachProgram, before the loop starts, and read-only afterwards.
type mcpDispatcher struct {
	program *tea.Program
}

// mcpJobsRequestMsg asks Update for the state of the work in flight.
type mcpJobsRequestMsg struct {
	reply chan mcpJobsReply
}

// mcpJobsReply is what Update sends back.
type mcpJobsReply struct {
	runs []jobs.Run
	err  error
}

// Jobs implements mcpserver.Dispatcher.
//
// It waits for Update, or for the client to give up — whichever comes first. A
// cancelled call abandons its channel; the buffer is what makes that safe.
func (d mcpDispatcher) Jobs(ctx context.Context) ([]jobs.Run, error) {
	if d.program == nil {
		return nil, mcpserver.ErrNoSession
	}

	reply := make(chan mcpJobsReply, 1)
	d.program.Send(mcpJobsRequestMsg{reply: reply})

	select {
	case r := <-reply:
		return r.runs, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// handleMCPJobsRequest answers from Update, where reading the registry is legal.
//
// Snapshot hands out copies with their cancel functions cleared, so what leaves
// here cannot stop a job behind the router's back — the same asymmetry a view
// gets (D1 of §3.58), and the reason no filtering or copying is needed on top.
func (a *App) handleMCPJobsRequest(msg mcpJobsRequestMsg) (tea.Model, tea.Cmd) {
	// Buffered to one by the sender, so this never blocks — see the note above.
	msg.reply <- mcpJobsReply{runs: a.jobs.Snapshot()}
	return a, nil
}

// compile-time proof that the adapter satisfies the port.
var _ mcpserver.Dispatcher = mcpDispatcher{}

// ── the deferred half: actions ──────────────────────────────────────────────
//
// A reading call is answered inside the same Update that receives it, so the
// buffered channel is the whole mechanism. An action is not: the router hands
// the request to a view, the view returns a Cmd, and the run is registered one
// Update later, in handleStartJobs. The answer is therefore deferred, and
// something has to hold the caller's channel in between — that is
// pendingInvocations, and it is mutated from Update alone.
//
// The correlation is an identifier carried by the request and stamped onto the
// run (jobs.StartMsg.Invocation). It is deliberately not "the next StartMsg
// after an invocation": that window is one Update cycle wide, a keypress fits
// in it, and the agent would be handed the identifier of the scan the user just
// started by hand.

// invocationID names one action call for as long as it is in flight.
type invocationID string

// mcpStartRequestMsg asks Update to do what a key would do.
type mcpStartRequestMsg struct {
	id    invocationID
	act   mcpserver.Action
	reply chan mcpStartReply
}

// mcpCancelRequestMsg asks Update to stop a run.
type mcpCancelRequestMsg struct {
	id    jobs.JobID
	reply chan error
}

// mcpStartReply is what the caller gets: the run's identifier, or why there
// will not be one.
type mcpStartReply struct {
	id  jobs.JobID
	err error
}

// nextInvocationID numbers action calls. It is atomic because Start runs on the
// HTTP server's goroutines, several at once.
var nextInvocationID atomic.Uint64

// Start implements mcpserver.Dispatcher.
func (d mcpDispatcher) Start(ctx context.Context, act mcpserver.Action) (jobs.JobID, error) {
	if d.program == nil {
		return 0, mcpserver.ErrNoSession
	}

	reply := make(chan mcpStartReply, 1)
	d.program.Send(mcpStartRequestMsg{
		id:    invocationID(strconv.FormatUint(nextInvocationID.Add(1), 10)),
		act:   act,
		reply: reply,
	})

	select {
	case r := <-reply:
		return r.id, r.err
	case <-ctx.Done():
		// The run may still be registered — the work was already dispatched, or
		// is about to be. Abandoning the channel is all that is needed: the
		// buffer means Update's reply completes into nothing, and the entry is
		// removed when it does. What the caller loses is the identifier, and
		// jobs_list is where it is found again.
		return 0, ctx.Err()
	}
}

// Cancel implements mcpserver.Dispatcher.
func (d mcpDispatcher) Cancel(ctx context.Context, id jobs.JobID) error {
	if d.program == nil {
		return mcpserver.ErrNoSession
	}

	reply := make(chan error, 1)
	d.program.Send(mcpCancelRequestMsg{id: id, reply: reply})

	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handleMCPStartRequest routes an action to the view that owns it.
//
// The view is created if it does not exist yet: views are built lazily, and an
// agent asking for a scan before anyone has opened `ws` is the ordinary case
// rather than an error.
//
// What the view does with the request is what it does with the key: it resolves
// the targets against what it knows, consults the same Availability the header
// greys with, and either returns a jobs.Start* command — wrapped so the run
// carries this invocation — or refuses with the reason.
func (a *App) handleMCPStartRequest(msg mcpStartRequestMsg) (tea.Model, tea.Cmd) {
	target, request, ok := mcpAction(msg.act, string(msg.id))
	if !ok {
		msg.reply <- mcpStartReply{err: fmt.Errorf("no action named %q", msg.act.Tool)}
		return a, nil
	}

	if a.pendingInvocations == nil {
		a.pendingInvocations = make(map[invocationID]chan mcpStartReply)
	}
	a.pendingInvocations[msg.id] = msg.reply

	a.createView(target)
	return a.routeToView(target, request)
}

// handleMCPCancelRequest stops a run by identifier.
//
// Registry.Cancel decides, and what stopping *means* differs by kind (D7 of
// §3.58): the queue always stops, so nothing further starts, and work already
// in flight is interrupted only where interrupting it leaves nothing behind — a
// git clone is never cut, because a cancelled one leaves half a repository on
// disk. None of that is re-decided here.
//
// The one refusal is a run that has already settled: reporting a cancellation
// that did nothing would be worse than saying so.
func (a *App) handleMCPCancelRequest(msg mcpCancelRequestMsg) (tea.Model, tea.Cmd) {
	if !a.jobs.Cancel(msg.id) {
		msg.reply <- fmt.Errorf("job %d cannot be stopped: there is no such run in this session, or it has already settled", msg.id)
		return a, nil
	}
	msg.reply <- nil
	return a, a.jobsChanged()
}

// handleMCPRefused answers a call the view would not honour.
func (a *App) handleMCPRefused(msg jobs.RefusedMsg) (tea.Model, tea.Cmd) {
	a.settleInvocation(msg.Invocation, 0, errors.New(msg.Reason))
	return a, nil
}

// settleInvocation hands the answer to whoever is waiting, and forgets it.
//
// Called from handleStartJobs with the identifier the registry just allocated,
// and from handleMCPRefused with the view's reason. An invocation nobody is
// waiting for — the caller hung up — is simply absent from the map, which is
// why this says nothing about it.
//
// The channel is buffered to one by the sender, so this never blocks (Rule
// 110's practical corollary: Update must not wait on anyone).
func (a *App) settleInvocation(invocation string, id jobs.JobID, err error) {
	if invocation == "" {
		return
	}
	key := invocationID(invocation)
	reply, ok := a.pendingInvocations[key]
	if !ok {
		return
	}
	delete(a.pendingInvocations, key)
	reply <- mcpStartReply{id: id, err: err}
}
