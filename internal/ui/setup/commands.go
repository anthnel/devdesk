package setup

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge/session"
)

// ContextExistsMsg reports whether the named context already has a file on
// disk.
type ContextExistsMsg struct {
	Exists bool
	Err    error
}

func checkContextExistsCmd(name string) tea.Cmd {
	return func() tea.Msg {
		exists, err := config.ContextExists(name)
		return ContextExistsMsg{Exists: exists, Err: err}
	}
}

// TokenValidatedMsg reports the outcome of authenticating the forge token the
// user typed. A nil Err means the token is both valid and saved — Authenticate
// persists it via the resolved Storage as part of the same call.
type TokenValidatedMsg struct {
	Err error
}

func validateTokenCmd(contextName, secretBackend, forgeType, url, token string) tea.Cmd {
	return func() tea.Msg {
		sel := credentials.Select(contextName, secretBackend)
		_, err := session.NewAuth(sel.Storage).Authenticate(context.Background(), forgeType, url, token)
		return TokenValidatedMsg{Err: err}
	}
}

// ContextSavedMsg reports whether the context file was written.
type ContextSavedMsg struct {
	Err error
}

func saveContextCmd(cfg *config.Config, name string) tea.Cmd {
	return func() tea.Msg {
		return ContextSavedMsg{Err: config.SaveContext(cfg, name)}
	}
}

// CurrentContextSetMsg reports whether the new context became the active one.
type CurrentContextSetMsg struct {
	Err error
}

func setCurrentContextCmd(name string) tea.Cmd {
	return func() tea.Msg {
		return CurrentContextSetMsg{Err: config.SetCurrentContext(name)}
	}
}
