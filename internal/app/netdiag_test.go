package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// TestANetdiagOpenRequestRecordsWhereItCameFrom mirrors the equivalent viewer
// test: status's H (§3.66) opens netdiag prefilled, and esc from there must
// know to return to status rather than to netdiag's own form.
func TestANetdiagOpenRequestRecordsWhereItCameFrom(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	a.createView(command.ViewStatus)
	a.currentView = command.ViewStatus

	a.Update(netdiag.OpenRequestMsg{Target: netcheck.Target{Host: "example.com", Port: 443}})

	if a.currentView != command.ViewNetdiag {
		t.Fatalf("currentView = %q, want netdiag", a.currentView)
	}
	held, ok := a.views[command.ViewNetdiag].(*netdiag.Model)
	if !ok {
		t.Fatalf("the router installed a %T", a.views[command.ViewNetdiag])
	}
	if held.OriginView != command.ViewStatus {
		t.Errorf("OriginView = %q, want status", held.OriginView)
	}
}

// TestANetdiagOpenRequestWithAutoRunStartsThePipeline covers the http/https/
// ssl case: the port is already known, so the run starts immediately rather
// than waiting on the user to press enter.
func TestANetdiagOpenRequestWithAutoRunStartsThePipeline(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	a.createView(command.ViewStatus)
	a.currentView = command.ViewStatus

	a.Update(netdiag.OpenRequestMsg{Target: netcheck.Target{Host: "example.com", Port: 443}, AutoRun: true})

	held := a.views[command.ViewNetdiag].(*netdiag.Model)
	// StateInput offers no "esc" (Rule 138 — obvious); StateRunning's only
	// shortcut is "esc" to cancel. Its presence is therefore the view's own
	// public signal that the run already started, without this package
	// reaching into netdiag's unexported state field.
	if !hasShortcut(held.GetShortcuts(), "esc") {
		t.Error("esc is not offered, so the pipeline does not appear to have started")
	}
}

// TestEscFromNetdiagReturnsToItsOrigin mirrors the viewer's equivalent test.
func TestEscFromNetdiagReturnsToItsOrigin(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	a.createView(command.ViewStatus)
	a.currentView = command.ViewStatus
	a.Update(netdiag.OpenRequestMsg{Target: netcheck.Target{Host: "example.com", Port: 443}})

	a.Update(netdiag.BackToOriginMsg{Origin: command.ViewStatus})

	if a.currentView != command.ViewStatus {
		t.Errorf("currentView = %q, want status", a.currentView)
	}
}

func hasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	for _, s := range shortcuts {
		if s.Key == key {
			return true
		}
	}
	return false
}
