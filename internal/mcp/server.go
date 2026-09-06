// Package mcp serves what DevDesk knows over the Model Context Protocol, in
// Streamable HTTP, from inside the running TUI (§3.61).
//
// The direction is the point. §3.10 had DevDesk assemble a payload, pseudonymise
// it and send it to a model; here DevDesk exposes what it knows and the agent
// comes to read it. That deletes the client, the pseudonymiser, the confirmation
// panel and the streaming — half a feature, because a protocol exists for it.
//
// # Why it is no longer a subcommand
//
// §3.38 served this on stdio, from `dk mcp`, because Bubble Tea owns stdin and
// stdout in full and there is no room for a second protocol in one process.
// That was right, and it left one thing out: an agent running in a container
// cannot execute the host binary at all. It was §3.38's own open question 1,
// and it became the only case anyone had. HTTP is the answer, the TUI is where
// it is served, and stdio is gone rather than kept beside it.
//
// # Still no lock, for a different reason
//
// ~/.devdesk/ has no lock and nothing warns when two writers cross. §3.38
// bought the absence of the question with read-only tools. §3.61 lets the
// server act, and buys it back a different way: the server is not a second
// writer. It lives in the TUI's process, so the one thing that writes is
// Update(), exactly as it already was. The guarantee moves from "the server
// does not write" to "the server writes through the same door as the keyboard".
//
// What follows from that is a hard rule for this package: **nothing here ever
// touches the router's model.** A tool that reads gets its answer from disk or
// from the daemon, through internal/cache/readonly.go and the same packages the
// views call; a tool that acts sends a message and waits for the reply. The
// model is in the same process, which is exactly why reading it would be the
// data race Rule 110 exists to forbid.
//
// # No destructive action is exposed
//
// §3.38 refused an `act` tier because an agent that picks the wrong row meets
// no modal. The TUI now runs by definition, so there *is* somebody — but a
// confirmation dialog raised by an MCP call would block the agent on an event
// the user is not looking at. So the class of action that would have needed one
// is not registered at all: deleting, pruning, killing, and creating and
// renaming too, whose only undo is a delete that is not exposed. An action that
// was never registered cannot be wrongly confirmed, which is the shape of the
// other guarantees here — contextGetOut has no field for a credential, finding
// has no Match, expose is an allow-list.
package mcp

import (
	"github.com/anthnel/devdesk/internal/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName and serverVersion identify DevDesk to the client. The name is what
// an agent sees beside every tool, so it is the binary's name and not the
// package's.
const (
	serverName    = "devdesk"
	serverVersion = "1"
)

// Env is what every tool reads: the configuration of the one context this
// process serves, and its name.
//
// One context at a time, and it is the session's (§3.61). §3.38 fixed it at
// startup so it could not change underneath an agent mid-conversation; there is
// no separate process to fix it in any more, so the server serves what the
// screen serves. What that costs is paid back two ways: every answer carries
// the name of the context that served it, and a switch emits a notification.
type Env struct {
	Config  *config.Config
	Context string

	// Dispatch is how the few tools that need the live session reach it — the
	// work in flight, which lives in the router's model and nowhere else. Nil
	// is refused by the tools that need it (ErrNoSession) rather than panicked
	// on, and startMCPCmd always sets it.
	//
	// The token is deliberately *not* here beside it: Env is handed to every
	// tool's register closure, and a secret reachable from there finds its way
	// into an answer eventually. A dispatcher is a door, not a secret.
	Dispatch Dispatcher
}

// newServer builds the server and registers the exposed tools. It is separate
// from Serve so a test can drive it over an in-memory transport rather than
// over the process's own stdio.
func newServer(env *Env) (*sdk.Server, error) {
	exposed, err := exposedTools(env.Config.MCP.Expose)
	if err != nil {
		return nil, err
	}
	s := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	for _, t := range exposed {
		t.register(s, env)
	}
	return s, nil
}
