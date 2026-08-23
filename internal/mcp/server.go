// Package mcp serves what DevDesk knows over the Model Context Protocol, on
// stdio, read only (§3.38).
//
// The direction is the point. §3.10 had DevDesk assemble a payload, pseudonymise
// it and send it to a model; here DevDesk exposes what it knows and the agent
// comes to read it. That deletes the client, the pseudonymiser, the confirmation
// panel and the streaming — half a feature, because a protocol exists for it.
//
// # Nothing writes to stdout but the protocol
//
// In stdio, stdout *is* the channel: a stray fmt.Println anywhere on the `dk mcp`
// path corrupts the frame, and the client reports a JSON parse error that names
// nothing. Diagnostics go to stderr, refusals included.
// TestNothingInThisPackageWritesToStdout is what keeps that true.
//
// # Read only, so there is no lock
//
// ~/.devdesk/ has no lock and nothing warns when two writers cross. A server
// that writes nothing removes the question: it can run while the TUI runs, in
// another context, without anyone having to think about it. That is also why
// there is no `act` tier — not now, and not behind a flag: an agent that picks
// the wrong row in the TUI meets a modal, and here there is nobody.
package mcp

import (
	"context"
	"fmt"

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
// One context per process (§3.38), resolved once at startup. A server that
// followed ~/.devdesk/.current-context would change what it answers underneath
// an agent mid-conversation, because the TUI writes that file.
type Env struct {
	Config  *config.Config
	Context string
}

// Serve runs the server on stdio until the client disconnects or ctx is done.
func Serve(ctx context.Context, env *Env) error {
	s, err := newServer(env)
	if err != nil {
		return err
	}
	return s.Run(ctx, &sdk.StdioTransport{})
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

// Refused is the error `dk mcp` reports when the setting is off.
//
// It names the setting *and* the context: turning it on in the wrong context is
// otherwise an hour spent looking at a server that will not start.
func Refused(contextName string) error {
	return fmt.Errorf("the MCP server is disabled for context %q — set `mcp.enabled: true` in that context's config.yaml", contextName)
}
