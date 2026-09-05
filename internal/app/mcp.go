package app

import (
	"errors"
	"log"
	"net"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
)

// MCPServerStartedMsg reports what became of the attempt to serve MCP.
//
// It is a message and not a return value because opening a listener is I/O, and
// I/O belongs in a Cmd (Rule 110). Addr is what the listener actually bound —
// not what the setting asked for — so a port chosen by the OS would be reported
// truthfully; Err is why there is nothing listening.
type MCPServerStartedMsg struct {
	Addr   string
	Server *http.Server
	Err    error
}

// startMCPCmd is a method so it reads the router's config, context and secret
// store; those three are copied into the closure rather than read from the
// receiver inside it, because a Cmd runs on its own goroutine and the receiver
// is the model (Rule 110).

// startMCPCmd serves this context over MCP, or explains why it does not.
//
// Nothing is attempted when the context has not enabled it: the refusal is
// carried on the message rather than logged and dropped, because "off" and
// "could not bind" look identical from the outside and only one of them is a
// problem.
//
// The listener is opened here, inside the Cmd, and handed back on the message.
// That is the same shape as a scan's context.CancelFunc travelling on its
// starting message: the thing is created where the I/O happens and stored by
// the one place allowed to store it.
func (a *App) startMCPCmd() tea.Cmd {
	cfg := a.config
	contextName := a.currentContext
	secrets := a.sharedState.Secrets

	return func() tea.Msg {
		if !cfg.MCP.Enabled {
			return MCPServerStartedMsg{Err: mcpserver.Refused(contextName)}
		}

		addr := cfg.MCP.Listen
		if addr == "" {
			addr = config.DefaultMCPListen
		}

		// Before the listener, not after: a port that answers without a token
		// is what this whole step exists to prevent, and binding first would
		// open one for the width of the failure path.
		token, err := mcpserver.ResolveToken(secrets)
		if err != nil {
			return MCPServerStartedMsg{Err: err}
		}

		handler, err := mcpserver.Handler(&mcpserver.Env{Config: cfg, Context: contextName})
		if err != nil {
			return MCPServerStartedMsg{Err: err}
		}
		handler = mcpserver.Authorize(handler, token)

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return MCPServerStartedMsg{Err: err}
		}

		srv := &http.Server{Handler: handler}
		go func() {
			// ErrServerClosed is what Shutdown produces, so it is the ordinary
			// end of a session and not a failure. Anything else happened to a
			// server the user believed was up, and the log is the only place
			// left to say so — the footer's message is three seconds long and
			// this can arrive an hour in.
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("ERROR [app/mcp] serve: %v", err)
			}
		}()

		return MCPServerStartedMsg{Addr: ln.Addr().String(), Server: srv}
	}
}

// handleMCPServerStarted stores the running server, or records why there is
// none.
//
// Both outcomes are kept. A view that wants to tell the user where to point an
// agent needs the address; one that wants to say why nothing is listening needs
// the error, and "the setting is off" is one of the answers rather than the
// absence of one.
func (a *App) handleMCPServerStarted(msg MCPServerStartedMsg) (tea.Model, tea.Cmd) {
	a.mcpServer = msg.Server
	a.mcpAddr = msg.Addr
	a.mcpErr = msg.Err

	if msg.Err != nil {
		log.Printf("MCP server not serving context %q: %v", a.currentContext, msg.Err)
		return a, nil
	}
	log.Printf("MCP server for context %q listening on %s", a.currentContext, msg.Addr)
	return a, nil
}
