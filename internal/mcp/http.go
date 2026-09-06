package mcp

import (
	"fmt"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler builds the HTTP handler that serves this context over MCP.
//
// Streamable HTTP rather than stdio (§3.61), and the reason is the one §3.38
// left as its open question 1: an agent running in a container cannot execute
// the host binary, so stdio never reaches it. Everything else about the server
// is unchanged — newServer, the declared tool table, the allow-list.
//
// The server instance is built once and handed to every request. The SDK asks
// for a func(*http.Request) *Server so that a host can serve several sessions
// from different servers; here there is one context per running TUI, which is
// the whole point of §3.61's decision 4, so the closure ignores the request.
func Handler(env *Env) (http.Handler, error) {
	s, err := newServer(env)
	if err != nil {
		return nil, err
	}
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, nil), nil
}

// Refused is the error the router reports when the setting is off.
//
// It names the setting *and* the context: turning it on in the wrong context is
// otherwise an hour spent looking at a server that will not start. It used to
// be printed on stderr by `dk mcp`; there is no stderr on this path any more,
// so it reaches the user as a footer message (Rule 128).
func Refused(contextName string) error {
	return fmt.Errorf("the MCP server is disabled for context %q — set `mcp.enabled: true` in that context's config.yaml", contextName)
}
