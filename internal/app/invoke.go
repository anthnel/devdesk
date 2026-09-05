package app

import (
	"context"

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
