package app

import (
	"log"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// handleConfigSaved takes a change made in the configuration view and makes it
// take effect.
//
// Most settings are read from a.config when a view is built, so rebuilding the
// views is all it takes — the same machinery a context switch already uses. A
// few are not:
//
//   - the theme is global process state, applied through theme.ApplyTheme;
//   - the secret backend decides which credentials.Storage the context holds,
//     which no view can rebuild itself into.
//
// The configuration view is deliberately kept rather than rebuilt: it is on
// screen, holds the cursor, and edits the very config object being handed back.
func (a *App) handleConfigSaved(msg configuration.ConfigSavedMsg) (tea.Model, tea.Cmd) {
	a.config = msg.Config
	// Cheap and idempotent, so no flag: every view is rebuilt below anyway.
	theme.SetGaugeGlyph(msg.Config.App.GaugeGlyph)

	var themeCmd tea.Cmd
	if msg.ThemeChanged {
		wasPending := a.theme.pending
		themeCmd = a.warnTheme(a.chooseTheme(msg.Config.App, true))
		if a.theme.pending && !wasPending {
			themeCmd = tea.Batch(themeCmd, a.themeQueryDueCmd())
		}
		a.sendThemeFacts()
	}
	if msg.BackendChanged {
		a.resolveSecretBackend()
	}
	if msg.EngineChanged {
		a.resolveContainerEngine()
	}
	if msg.ForgeChanged {
		a.closeForgeSession()
	}

	resizeCmd := a.rebuildViews()
	motionCmd := tea.Batch(a.applyMotion(msg.Config), a.applyWhenUnfocused(msg.Config))

	// A save that settles the wait without a terminal answer (a named theme)
	// still owes the start view its held-back Init.
	var initCmd tea.Cmd
	if !a.theme.pending {
		initCmd = a.runDeferredStartInit()
	}

	return a, tea.Batch(initCmd, resizeCmd, a.syncProxyPortCmd(), a.redetectIfToolsMoved(msg.EngineChanged), motionCmd, themeCmd)
}

// rebuildViews drops every view except the configuration view so it is rebuilt
// against the saved config or the new palette on next use, recreates the one on
// screen and returns the resize that sizes it. Keeping the configuration view is
// what stops a save from throwing away the cursor after every keystroke. The
// recreated view's Init is not run, as in a save;
// settleTheme runs the start view's deferred Init itself.
func (a *App) rebuildViews() tea.Cmd {
	current := a.views[command.ViewConfiguration]
	a.views = make(map[command.ViewType]Screen)
	if current != nil {
		a.setView(command.ViewConfiguration, current)
	}
	a.createView(a.currentView)
	return a.requestResize()
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

// resolveContainerEngine picks up the new app.container_engine for this
// context.
//
// Every view that lists containers, images, networks or volumes is rebuilt by
// the caller right after this, so nothing carries the previous engine's
// listings forward — which matters here more than it does for a theme: two
// engines do not hold the same containers, and a stale row would name something
// that does not exist.
//
// A preference that does not resolve is **not** fatal and does not fall back to
// the other engine. The previous shape is kept and the views report the absence
// the way they always have when the binary is missing (requireEngine) — an
// error at the moment of use, naming the engine that was asked for, rather than
// a silent substitution that answers about the wrong machine (§3.67).
func (a *App) resolveContainerEngine() {
	preference := a.config.App.ContainerEngine
	shape, err := engine.Resolve(preference)
	if err != nil {
		log.Printf("ERROR [app] resolve container engine %q: %v", preference, err)
		return
	}
	engine.SetCurrent(shape)
	log.Printf("Container engine for context %s resolved to %s (%s)",
		a.currentContext, shape.Name, shape.Binary)
}

// closeForgeSession drops the client-side session after the forge's URL or its
// platform changed. The session was established against the previous one, so
// keeping it would mean the next call fails somewhere far from the cause.
//
// Nothing is revoked and no token is deleted: the user changed an address or a
// platform, not their credentials, and a token for the old host is still
// theirs — and it is still what the store holds for that URL.
func (a *App) closeForgeSession() {
	if !a.sharedState.IsAuthenticated {
		return
	}
	log.Printf("Forge changed; closing the session for context %s", a.currentContext)
	a.clearAuthenticated()
}
