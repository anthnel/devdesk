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
