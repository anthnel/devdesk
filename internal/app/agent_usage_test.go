package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/agentusageview"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// TestAuOpensTheAgentUsageView types the alias and presses enter, the way a
// user does, and checks the router built the real view rather than a stand-in.
func TestAuOpensTheAgentUsageView(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "au")
	feedKey(t, a, testutil.Key("enter"))

	if a.currentView != command.ViewAgentUsage {
		t.Fatalf("current view = %s, want %s", a.currentView, command.ViewAgentUsage)
	}
	if _, ok := a.views[command.ViewAgentUsage].(agentusageview.Model); !ok {
		t.Fatalf("the router installed a %T", a.views[command.ViewAgentUsage])
	}
}

// TestAgentUsageViewIsSizedAndReceivesMessages checks the router resizes the
// view it has just opened and keeps forwarding messages to it.
func TestAgentUsageViewIsSizedAndReceivesMessages(t *testing.T) {
	a := commanding(t, &fakeView{})
	testutil.Msgs(a.switchView(command.ViewAgentUsage))

	if _, ok := a.views[command.ViewAgentUsage].(agentusageview.Model); !ok {
		t.Fatalf("the router installed a %T", a.views[command.ViewAgentUsage])
	}
	a.Update(testutil.Resize(100, 30))
	if v := a.views[command.ViewAgentUsage].View(); v == "" {
		t.Error("the view renders nothing after a resize")
	}
}
