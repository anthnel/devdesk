package setup

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
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
// user typed. A nil Err means the token itself is valid; SaveWarning is set
// whenever it did not actually end up somewhere durable — either because
// Authenticate's own write failed, or because Select had already resolved to
// memory-only storage. A token that "validates" but is never asked about
// again — this used to be silent, which is exactly what sent a user back to
// a logged-out app after being told setup succeeded.
type TokenValidatedMsg struct {
	Err         error
	SaveWarning string
}

func validateTokenCmd(contextName, secretBackend, forgeType, url, token string) tea.Cmd {
	return func() tea.Msg {
		sel := credentials.Select(contextName, secretBackend)
		result, err := session.NewAuth(sel.Storage).Authenticate(context.Background(), forgeType, url, token)
		if err != nil {
			return TokenValidatedMsg{Err: err}
		}
		warning := result.SaveWarning
		if warning == "" && !sel.Persists() {
			warning = sel.Detail
		}
		return TokenValidatedMsg{SaveWarning: warning}
	}
}

// SecretBackendCheckedMsg reports whether a candidate secret backend
// actually resolves to somewhere durable right now — the same question
// credentials.Select answers, surfaced before the user has typed a token
// into it rather than only after (Rule: detect this as early as possible).
type SecretBackendCheckedMsg struct {
	Persists bool
	Detail   string
}

func checkSecretBackendCmd(contextName, preference string) tea.Cmd {
	return func() tea.Msg {
		sel := credentials.Select(contextName, preference)
		return SecretBackendCheckedMsg{Persists: sel.Persists(), Detail: sel.Detail}
	}
}

// ContainerEngineCheckedMsg reports whether a candidate container engine is
// not just installed but actually running. Missing means no binary was found
// at all; Err means the binary exists but its daemon did not answer — the
// distinction the app's own tool detection does not make today (it only
// checks the client, which answers even with the daemon down).
type ContainerEngineCheckedMsg struct {
	Missing bool
	Err     error
	Name    string
}

func checkContainerEngineCmd(preference string) tea.Cmd {
	return func() tea.Msg {
		shape, err := engine.Resolve(preference)
		if err != nil {
			return ContainerEngineCheckedMsg{Missing: true}
		}
		// Safe to mutate: this is a separate, short-lived process from the
		// real `dk`, which re-resolves its own engine.Current() fresh at
		// startup (app.resolveContainerEngine) — nothing here leaks into it.
		engine.SetCurrent(shape)
		if _, err := docker.FetchCapacity(); err != nil {
			return ContainerEngineCheckedMsg{Err: err, Name: shape.Name}
		}
		return ContainerEngineCheckedMsg{Name: shape.Name}
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
