package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/help"
)

// The router probes each view for optional interfaces and silently falls back
// when one is missing. That is the right behaviour and a bad failure mode: the
// configuration view implemented GetShortcuts and GetTitle but not GetIcon or
// GetHeaderInfo, so it satisfied none of HeaderView and the viewport rendered
// an empty title — with nothing to say so.
//
// Checking every view turns that into a build-time contract rather than
// something noticed by looking at the screen.
//
// AllViewNames rather than ViewNames: a view the router opens itself — the
// document viewer — renders in the same viewport as the rest and fails in
// exactly the same silence. That it cannot be typed says nothing about whether
// it has a title.
func TestEveryViewSuppliesItsHeaderAndHelp(t *testing.T) {
	for _, name := range command.AllViewNames() {
		view := command.ViewType(name)

		t.Run(name, func(t *testing.T) {
			app := newWithSize(testConfig(), 120, 40)
			app.createView(view)

			built, ok := app.views[view]
			if !ok {
				t.Fatalf("createView(%q) built nothing; the switch has no case for it", view)
			}

			hv, ok := built.(HeaderView)
			if !ok {
				t.Fatalf("%q does not implement HeaderView, so the router renders an empty title for it", view)
			}
			if strings.TrimSpace(hv.GetTitle()) == "" {
				t.Errorf("%q has an empty title", view)
			}
			if len(hv.GetShortcuts()) == 0 {
				t.Errorf("%q advertises no shortcuts", view)
			}

			if _, ok := built.(help.Provider); !ok {
				t.Errorf("%q does not implement help.Provider, so ? shows nothing (Rule 114)", view)
			}
		})
	}
}

// Le cadre du viewport appartient au routeur, et une vue qui s'en passe doit
// dessiner les siens. Une seule le fait — le dashboard, qui est la seule vue à
// ne pas encadrer un objet unique. Ce test tient l'exception à une exception.
func TestOnlyTheDashboardIsFrameless(t *testing.T) {
	for _, name := range command.AllViewNames() {
		view := command.ViewType(name)

		app := newWithSize(testConfig(), 120, 40)
		app.createView(view)
		app.currentView = view

		frameless := app.frameless()
		if want := name == "dashboard"; frameless != want {
			t.Errorf("%q reports frameless=%v, want %v", view, frameless, want)
		}
	}
}

// Une vue dont la hauteur de footer dépend de sa propre taille — le dashboard
// cache sa barre d'onglets quand il n'en reste qu'un — ne peut pas répondre
// juste à la première question du routeur, qui arrive avant qu'elle connaisse
// sa nouvelle taille. resize() itère pour ça, et ce test l'épingle : sans la
// seconde passe, le viewport reste décalé d'une ligne jusqu'au resize suivant.
func TestResizeConvergesOnAFooterThatDependsOnTheSize(t *testing.T) {
	app := newWithSize(testConfig(), 120, 40)
	app.createView(command.ViewType("dashboard"))
	app.currentView = command.ViewType("dashboard")

	// Small enough for a tab bar, then large enough that there is one tab left.
	app.resize(120, 40)
	small := app.lastFooterHeight

	app.resize(240, 60)
	large := app.lastFooterHeight

	if small != 3 {
		t.Errorf("footer height at 120x40 = %d, want 3 (tab bar + blank + info)", small)
	}
	if large != 2 {
		t.Errorf("footer height at 240x60 = %d, want 2 — the tab bar is gone at wide", large)
	}
	if got := lipgloss.Height(app.renderViewFooter()); got != large {
		t.Errorf("the footer renders %d lines while the layout budgeted %d", got, large)
	}
}

// A frameless view gets the line the bottom border used to cost, and its title
// is a rule rather than the top of a box that is not there.
func TestAFramelessViewGetsTheBorderLineBack(t *testing.T) {
	app := newWithSize(testConfig(), 120, 40)

	app.createView(command.ViewType("dashboard"))
	app.currentView = command.ViewType("dashboard")
	app.resize(120, 40)
	frameless := app.viewport.Height

	app.createView(command.ViewType("status"))
	app.currentView = command.ViewType("status")
	app.resize(120, 40)
	framed := app.viewport.Height

	if frameless <= framed-2 {
		t.Errorf("the frameless viewport is %d lines against %d framed — it did not get the border back",
			frameless, framed)
	}
	if strings.Contains(app.renderTitleLine(), "┌") {
		t.Log("the framed view keeps its corner, as it should")
	}

	app.currentView = command.ViewType("dashboard")
	if title := app.renderTitleLine(); strings.Contains(title, "┌") || strings.Contains(title, "┐") {
		t.Errorf("the frameless title line still draws box corners: %q", title)
	}
}
