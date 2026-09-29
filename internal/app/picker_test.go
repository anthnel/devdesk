package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/filebrowser"
)

// A form's Browse button lends the file browser as a picker (§3.95).
func TestAPickRequestLendsThePickerAndRemembersWhoAsked(t *testing.T) {
	a := router(t, &fakeView{})
	a.currentView = command.ViewConfiguration
	a.views[command.ViewConfiguration] = &fakeView{}

	a.Update(filebrowser.PickRequestMsg{Kind: filebrowser.PickDir, Start: t.TempDir(), Tag: "workspaces-dir"})

	if a.currentView != command.ViewFiles {
		t.Fatalf("current view = %s, want the picker", a.currentView)
	}
	if held, ok := a.views[command.ViewFiles].(filebrowser.Model); !ok || !held.Picking() {
		t.Fatal("the files view must be built as a picker")
	}
	if a.pickerReturnView != command.ViewConfiguration {
		t.Errorf("return view = %s, want the configuration that asked", a.pickerReturnView)
	}
}

func TestThePickedPathGoesBackToTheBorrowerAndThePickerIsDropped(t *testing.T) {
	origin := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewTemplates] = origin
	a.currentView = command.ViewTemplates
	a.Update(filebrowser.PickRequestMsg{Kind: filebrowser.PickDir, Tag: "template-path"})

	a.Update(filebrowser.PathPickedMsg{Tag: "template-path", Path: "/srv/templates/go"})

	if a.currentView != command.ViewTemplates {
		t.Errorf("current view = %s, want the templates view back", a.currentView)
	}
	got, ok := receivedOf[filebrowser.PathPickedMsg](origin)
	if !ok || got.Path != "/srv/templates/go" || got.Tag != "template-path" {
		t.Fatalf("the borrower received %+v", got)
	}
	if _, kept := a.views[command.ViewFiles]; kept {
		t.Error("the lent picker must be dropped, so :files is the browser next time")
	}
}

func TestACancelledPickReturnsWithoutAPath(t *testing.T) {
	origin := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewConfiguration] = origin
	a.currentView = command.ViewConfiguration
	a.Update(filebrowser.PickRequestMsg{Tag: "log"})

	a.Update(filebrowser.PickCancelledMsg{Tag: "log"})

	if a.currentView != command.ViewConfiguration {
		t.Errorf("current view = %s, want the configuration back", a.currentView)
	}
	if _, ok := receivedOf[filebrowser.PickCancelledMsg](origin); !ok {
		t.Error("the borrower must hear the cancellation")
	}
}

// A picker abandoned by typing a command must not be what `:files` shows.
func TestTypingFilesDropsAnAbandonedPicker(t *testing.T) {
	a := router(t, &fakeView{})
	a.Update(filebrowser.PickRequestMsg{Tag: "x"})

	a.resetSelectionModeFor(command.ViewFiles)

	if _, kept := a.views[command.ViewFiles]; kept {
		t.Error(":files must rebuild the browser rather than show the abandoned picker")
	}
}
