package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// Where esc goes back to is read from the router rather than carried on the
// message: the view the user is looking at *is* the one that asked.
func TestAViewerOpenRequestRecordsWhereItCameFrom(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	a.createView(command.ViewContainers)
	a.currentView = command.ViewContainers

	a.Update(uiviewer.OpenRequestMsg{Source: viewer.NewFileSource("notes.txt")})

	if a.currentView != command.ViewViewer {
		t.Fatalf("currentView = %q, want the viewer", a.currentView)
	}
	held, ok := a.views[command.ViewViewer].(uiviewer.Model)
	if !ok {
		t.Fatalf("the router installed a %T", a.views[command.ViewViewer])
	}
	if held.OriginView != command.ViewContainers {
		t.Errorf("OriginView = %q, want containers", held.OriginView)
	}
}

// A request with no source is ignored rather than opening a viewer on nothing.
func TestAnOpenRequestWithNoSourceIsIgnored(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	before := a.currentView

	a.Update(uiviewer.OpenRequestMsg{})

	if a.currentView != before {
		t.Errorf("currentView = %q, want it unchanged at %q", a.currentView, before)
	}
}

func TestEscFromTheViewerReturnsToItsOrigin(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	a.createView(command.ViewWorkspaces)
	a.currentView = command.ViewWorkspaces
	a.Update(uiviewer.OpenRequestMsg{Source: viewer.NewFileSource("notes.txt")})

	a.Update(uiviewer.BackToOriginMsg{Origin: command.ViewWorkspaces})

	if a.currentView != command.ViewWorkspaces {
		t.Errorf("currentView = %q, want workspaces", a.currentView)
	}
}

// `:viewer` on an empty viewer would be a screen saying there is nothing in it,
// and app.default_view would offer it as a landing view — the defect ViewNames
// was split from FullNames to fix.
func TestTheViewerIsNotTypeableAsACommand(t *testing.T) {
	if got := command.ParseCommand("viewer"); got.Type != command.CommandUnknown {
		t.Errorf("`:viewer` parsed as %v, want unknown", got.Type)
	}
	for _, name := range command.ViewNames() {
		if name == string(command.ViewViewer) {
			t.Error("viewer is listed in ViewNames, so default_view offers it")
		}
	}
}

// The whole path, on a real file: the request goes to the router, the router
// installs the viewer, the viewer's Init reads the disk and the tree comes back
// with the document on it. Every step is covered on its own; this is the one
// test that would catch two of them agreeing about nothing.
func TestAFileOpensThroughTheRouterAndRendersItsTree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"name":"web","port":8080}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newWithSize(testConfig(), 120, 40)
	a.createView(command.ViewWorkspaces)
	a.currentView = command.ViewWorkspaces

	_, cmd := a.Update(uiviewer.OpenRequestMsg{Source: viewer.NewFileSource(path)})
	if cmd == nil {
		t.Fatal("opening the viewer issued no command, so the file is never read")
	}
	for _, msg := range testutil.Msgs(cmd) {
		updated, _ := a.views[command.ViewViewer].Update(msg)
		a.views[command.ViewViewer] = updated
	}

	rendered := a.views[command.ViewViewer].View()
	for _, want := range []string{"name", "web", "port", "8080"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the tree does not show %q:\n%s", want, rendered)
		}
	}

	title := a.views[command.ViewViewer].(uiviewer.Model).GetTitle()
	if !strings.Contains(title, "config.json") {
		t.Errorf("GetTitle() = %q, want it to name the document", title)
	}
}

// It is still a view, and the contract tests have to reach it.
func TestTheViewerIsListedAmongAllViews(t *testing.T) {
	for _, name := range command.AllViewNames() {
		if name == string(command.ViewViewer) {
			return
		}
	}
	t.Error("viewer is missing from AllViewNames, so nothing checks its header or help")
}
