package app

import (
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// themeDetectTimeout is how long app.theme: auto waits for the terminal's
// answer to the background query before settling on the dark theme. A variable
// so tests can shorten it.
var themeDetectTimeout = 500 * time.Millisecond

// themeQueryDelay is how long a choice of auto made in session waits before the
// terminal is asked. A variable so tests can shorten it.
var themeQueryDelay = 100 * time.Millisecond

// themeQueryDueMsg is the deferred background query coming due. gen ties it to
// the choice that deferred it: a newer choice of theme makes it stale.
type themeQueryDueMsg struct{ gen int }

// themeDetectTimeoutMsg ends the wait for the terminal's answer. gen ties it to
// the wait that armed it: a newer choice of theme makes it stale.
type themeDetectTimeoutMsg struct{ gen int }

// themeState is what the router knows about the theme in force and about the
// terminal-background detection behind app.theme: auto. Only Update() writes it
// (Rule 110); the Cmds it hands out carry copies of what they need.
type themeState struct {
	// pending: the terminal was asked and neither answered nor timed out.
	// View() leaves the terminal background alone meanwhile, so the terminal
	// cannot answer with DevDesk's own colour.
	pending bool
	// gen counts theme choices; a timeout of an older one is ignored.
	gen int
	// answer is the first thing learned about the terminal, kept for the
	// process: the terminal is asked at most once.
	answer theme.Background
	// resolution is the theme in force.
	resolution theme.Resolution
	// startInitDeferred: App.Init held back the start view's Init because the
	// detection was pending at startup; settleTheme runs it, once. A choice of
	// auto made in session never sets it, the view having been initialised.
	startInitDeferred bool
	// startWarns are the warnings the startup choice produced, posted by Init.
	startWarns []string
	// warned holds the candidate names already reported for the current choice,
	// so the terminal's answer does not report one a second time.
	warned map[string]bool
}

// themeResolution returns the theme in force and why.
func (a *App) themeResolution() theme.Resolution { return a.theme.resolution }

// themeFacts is what the configuration view shows about the theme in force:
// its name and, for auto, why it was chosen. A named theme has no reason.
func (a *App) themeFacts() configuration.ThemeFacts {
	r := a.theme.resolution
	facts := configuration.ThemeFacts{InUse: r.Name}
	if r.Reason != "" {
		facts.Reason = "auto: " + r.Reason
	}
	return facts
}

// sendThemeFacts hands the resolution to the configuration view when the
// router holds one; a view built later reads themeFacts itself.
func (a *App) sendThemeFacts() {
	view, ok := a.views[command.ViewConfiguration]
	if !ok {
		return
	}
	f := a.themeFacts()
	// The view stores the facts and asks for nothing, so the Cmd is nil.
	updated, _ := view.Update(configuration.ThemeFactsMsg(f))
	a.setView(command.ViewConfiguration, updated)
}

// themeDetectionPending reports whether the terminal's answer is awaited.
func (a *App) themeDetectionPending() bool { return a.theme.pending }

// chooseTheme resolves app.theme, applies the palette and records the outcome.
// It returns the footer warnings, one per invalid candidate not reported yet
// (theme_dark and theme_light both count in auto, selected or not). fresh starts
// a new choice (startup, a save) and forgets what was reported before; the
// terminal's answer passes false. It is the single entry point for the startup
// choice, for a save from the configuration view and for the answer.
//
// "auto" with a known answer applies at once without asking again. "auto"
// without one starts from the dark theme and waits for the terminal: at startup
// themeInitCmd arms the query, in session themeQueryDueCmd defers it. A named
// theme cancels any wait.
func (a *App) chooseTheme(app config.AppConfig, fresh bool) []string {
	type candidate struct{ name, warn string }
	var r theme.Resolution
	var candidates []candidate
	if app.IsAutoTheme() {
		if !a.theme.pending {
			a.theme.gen++
		}
		bg := a.theme.answer
		dark, light := app.EffectiveThemeDark(), app.EffectiveThemeLight()
		r = theme.ResolveAuto(bg, dark, light)
		a.theme.pending = bg == theme.BackgroundUnknown
		selected, other, otherBg := dark, light, theme.BackgroundLight
		if bg == theme.BackgroundLight {
			selected, other, otherBg = light, dark, theme.BackgroundDark
		}
		candidates = []candidate{
			{selected, r.Warning},
			{other, theme.ResolveAuto(otherBg, dark, light).Warning},
		}
	} else {
		a.theme.gen++
		a.theme.pending = false
		r = theme.ResolveNamed(app.Theme)
		candidates = []candidate{{app.Theme, r.Warning}}
	}
	a.theme.resolution = r
	theme.ApplyTheme(r.Theme)
	theme.CurrentThemeName = r.Name
	if r.Reason != "" {
		log.Printf("INFO [theme] using %q (auto: %s)", r.Name, r.Reason)
	} else {
		log.Printf("INFO [theme] using %q (app.theme: %s)", r.Name, r.Requested)
	}

	if fresh || a.theme.warned == nil {
		a.theme.warned = map[string]bool{}
	}
	var warns []string
	for _, c := range candidates {
		if c.warn != "" && !a.theme.warned[c.name] {
			a.theme.warned[c.name] = true
			warns = append(warns, c.warn)
		}
	}
	return warns
}

// themeQueryDueCmd defers the background query by themeQueryDelay, so the frame
// that resets the terminal background (View leaves it nil while pending) is
// drawn first and the terminal answers with its own colour, not DevDesk's.
func (a *App) themeQueryDueCmd() tea.Cmd {
	gen := a.theme.gen
	return tea.Tick(themeQueryDelay, func(time.Time) tea.Msg { return themeQueryDueMsg{gen: gen} })
}

// handleThemeQueryDue sends the deferred query and arms its timeout, unless the
// choice that deferred it is stale or already settled.
func (a *App) handleThemeQueryDue(msg themeQueryDueMsg) (tea.Model, tea.Cmd) {
	if !a.theme.pending || msg.gen != a.theme.gen {
		return a, nil
	}
	return a, a.themeQueryCmd()
}

// themeQueryCmd is the background query and the timeout that ends its wait.
func (a *App) themeQueryCmd() tea.Cmd {
	gen := a.theme.gen
	return tea.Batch(
		tea.RequestBackgroundColor,
		tea.Tick(themeDetectTimeout, func(time.Time) tea.Msg { return themeDetectTimeoutMsg{gen: gen} }),
	)
}

// themeInitCmd is the theme part of Init: the one background query and its
// timeout when the answer is awaited, and the startup warning. Nil otherwise.
func (a *App) themeInitCmd() tea.Cmd {
	var cmds []tea.Cmd
	if a.theme.pending {
		cmds = append(cmds, a.themeQueryCmd())
	}
	cmds = append(cmds, a.warnTheme(a.theme.startWarns))
	return tea.Batch(cmds...)
}

// handleBackgroundColor takes the terminal's answer. Only the first one while a
// query is pending counts; the router owns the message, no view sees it.
func (a *App) handleBackgroundColor(msg tea.BackgroundColorMsg) (tea.Model, tea.Cmd) {
	if !a.theme.pending {
		log.Printf("INFO [theme] ignoring a terminal background answer: none is awaited")
		return a, nil
	}
	bg := theme.BackgroundNoAnswer
	if msg.Color != nil {
		bg = theme.BackgroundDark
		if !msg.IsDark() {
			bg = theme.BackgroundLight
		}
	}
	return a, a.settleTheme(bg)
}

// handleThemeDetectTimeout falls back to the dark theme, silently, when the wait
// it belongs to is still the current one.
func (a *App) handleThemeDetectTimeout(msg themeDetectTimeoutMsg) (tea.Model, tea.Cmd) {
	if !a.theme.pending || msg.gen != a.theme.gen {
		return a, nil
	}
	log.Printf("INFO [theme] no answer from the terminal after %v, using the dark theme", themeDetectTimeout)
	return a, a.settleTheme(theme.BackgroundNoAnswer)
}

// settleTheme records what the terminal said and applies the matching theme.
// The views are rebuilt when the effective theme changed: their styles were
// captured with the previous palette. The start view's deferred Init runs last.
func (a *App) settleTheme(bg theme.Background) tea.Cmd {
	before := a.theme.resolution.Name
	a.theme.answer = bg
	a.theme.pending = false
	cmds := []tea.Cmd{a.warnTheme(a.chooseTheme(a.config.App, false))}
	if a.theme.resolution.Name != before {
		cmds = append(cmds, a.rebuildViews())
	}
	cmds = append(cmds, a.runDeferredStartInit())
	a.sendThemeFacts()
	return tea.Batch(cmds...)
}

// runDeferredStartInit returns the Init of the view on screen, once, when
// App.Init held it back. It reads a.views after any rebuild, so the Cmd belongs
// to the view that will receive its result.
func (a *App) runDeferredStartInit() tea.Cmd {
	if !a.theme.startInitDeferred {
		return nil
	}
	a.theme.startInitDeferred = false
	if view, ok := a.views[a.currentView]; ok {
		return view.Init()
	}
	return nil
}

// warnTheme turns theme warnings into footer messages, nil when there are none.
func (a *App) warnTheme(texts []string) tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range texts {
		cmds = append(cmds, components.PostFooter(components.LevelWarning, t))
	}
	return tea.Batch(cmds...)
}
