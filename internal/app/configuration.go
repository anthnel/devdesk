package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// handleConfigSaved takes a change made in the configuration view and makes it
// take effect.
//
// Most settings are read from a.config when a view is built, so rebuilding the
// views is all it takes — the same machinery a context switch already uses. Two
// are not:
//
//   - the theme is global process state, applied through theme.ApplyTheme;
//   - the secret backend decides which credentials.Storage the context holds,
//     which no view can rebuild itself into.
//
// The configuration view is deliberately kept rather than rebuilt: it is on
// screen, holds the cursor, and edits the very config object being handed back.
func (a *App) handleConfigSaved(msg configuration.ConfigSavedMsg) (tea.Model, tea.Cmd) {
	a.config = msg.Config

	if msg.ThemeChanged {
		a.applyThemeNow(msg.Config.App.Theme)
	}
	if msg.BackendChanged {
		a.resolveSecretBackend()
	}
	if msg.GitLabURLChanged {
		a.closeGitLabSession()
	}

	// Every view except this one is dropped so it is rebuilt against the saved
	// config on next use. Keeping the configuration view is what stops a save
	// from throwing away the cursor after every keystroke.
	current := a.views[command.ViewConfiguration]
	a.views = make(map[command.ViewType]tea.Model)
	if current != nil {
		a.views[command.ViewConfiguration] = current
	}
	a.createView(a.currentView)

	return a, a.requestResize()
}

// applyThemeNow switches the palette in place.
//
// It does not save: the configuration view already did, and writing twice would
// make the view's own config object and the file disagree about who is
// authoritative. That double write is what the `:theme` command did — it is
// gone, and this replaced it.
func (a *App) applyThemeNow(name string) {
	t, err := theme.LoadTheme(name)
	if err != nil {
		log.Printf("ERROR [app] load theme %q: %v", name, err)
		return
	}
	theme.ApplyTheme(t)
	theme.CurrentThemeName = name
}

// resolveSecretBackend picks up the new app.secret_backend for this context.
//
// Nothing is migrated between backends. §3.9 removed the option that wrote a
// token to two stores at once, and copying one here would rebuild exactly that.
// The configuration view has already told the user their secrets will need
// re-entering, which is why this can be a plain re-resolve.
func (a *App) resolveSecretBackend() {
	selection := credentials.Select(a.currentContext, a.config.App.SecretBackend)
	a.sharedState.Secrets = selection
	a.sharedState.SecretNotices = nil

	// The GitLab session was authenticated against the previous store, so it no
	// longer has a token behind it. Saying so beats a client that fails on its
	// next call.
	a.clearAuthenticated()

	log.Printf("Secret backend for context %s resolved to %s", a.currentContext, selection.Backend)
}

// closeGitLabSession drops the client-side session after the GitLab URL
// changed. The session was established against the previous host, so keeping it
// would mean the next call fails somewhere far from the cause.
//
// Nothing is revoked and no token is deleted: the user changed an address, not
// their credentials, and a token for the old host is still theirs.
func (a *App) closeGitLabSession() {
	if !a.sharedState.IsAuthenticated {
		return
	}
	log.Printf("GitLab URL changed; closing the session for context %s", a.currentContext)
	a.clearAuthenticated()
}
