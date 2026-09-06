package mcp

import (
	"context"
	"errors"

	"github.com/anthnel/devdesk/internal/jobs"
)

// Dispatcher is how a tool asks the running TUI something the disk cannot
// answer.
//
// Almost nothing needs it. §3.38 established that everything the read tools
// serve is on disk or on the daemon — which is exactly why a headless process
// could answer the same questions — and internal/cache/readonly.go is still the
// road they take. What is left over is the work in flight: jobs.Registry lives
// in the router's model and is written from Update() alone, so a tool cannot
// read it, and a second process could not know about it at all.
//
// The interface is typed per question rather than generic over a payload. An
// `Invoke(name string, args any) (any, error)` would put a type assertion at
// both ends of every call and buy nothing: there is one implementation, in
// internal/app, and the compiler is the cheapest place to catch a mismatch.
//
// The implementation must never touch the model directly. It sends a message
// and waits for the answer, which is the only door Rule 110 leaves open.
type Dispatcher interface {
	// Jobs returns what the session knows is running, or has run.
	//
	// The runs come from Registry.Snapshot, which clears the cancel functions
	// on the copies it hands out — so a caller cannot stop a job behind the
	// router's back, here any more than in a view.
	Jobs(ctx context.Context) ([]jobs.Run, error)

	// Start asks the session to do what a key would do, and returns the
	// identifier of the run it registered.
	//
	// The session builds the run, exactly as it does for a keypress: the view
	// is the only place that knows how to turn "scan these paths" into targets,
	// options and a cache to purge, and duplicating that here would undo what
	// §3.58 spent an entry unifying. What comes back is the identifier, because
	// the work outlives the call — jobs_get is how it is followed.
	//
	// A refusal is an error carrying the view's own reason, the same sentence
	// the header greys a shortcut with.
	Start(ctx context.Context, act Action) (jobs.JobID, error)

	// Cancel stops a run. What stopping means differs by kind, and it is
	// §3.58's table that says so, not this: the queue always stops, and work
	// already in flight is cut only where cutting leaves nothing behind — a
	// scan and a pull do, a clone in progress is waited out.
	Cancel(ctx context.Context, id jobs.JobID) error
}

// Action is one thing an action tool asks the session to do.
//
// Tool is the name from the declared table, and it is what the session routes
// on: every action names its view in its own name (workspace_, image_), so
// there is no discriminator argument for an agent to fill in and no scope to
// get wrong.
type Action struct {
	Tool    string
	Targets []string
}

// ErrNoSession is what a tool answers when it was built without a link to a
// running TUI.
//
// It cannot happen in production — startMCPCmd always passes one, and a test
// holds it to that — so it is a programming error rather than a user-facing
// condition. It is still an error and not a panic: an agent gets a sentence it
// can report, where a panic inside a tool handler would take the server down
// and say nothing about which tool did it.
var ErrNoSession = errors.New("this tool needs the running DevDesk session and the server was built without a link to one")
