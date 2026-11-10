package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
	if v := a.views[command.ViewAgentUsage].Render(); v == "" {
		t.Error("the view renders nothing after a resize")
	}
}

// The router must see the view as a FrameTrailView; a signature slip would
// otherwise compile and silently show no trail (DEVDESK-134 step 2).
var _ FrameTrailView = agentusageview.Model{}

// TestAgentUsageTrailSitsOnTheTopBorder is end to end: a session on disk, the
// real view opened to its agents, and the router's title line carrying the
// trail in parentheses right after the title.
func TestAgentUsageTrailSitsOnTheTopBorder(t *testing.T) {
	a := commanding(t, &fakeView{})
	home := os.Getenv("HOME")
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", "-srv-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "0a1b2c3d-0000-4000-8000-000000000001"
	line := `{"type":"user","cwd":"` + filepath.Join(home, "app") + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	a.Update(testutil.Resize(120, 30))
	cmd := a.switchView(command.ViewAgentUsage)
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
	a.commandMode = false // commanding() starts with the palette open
	for _, msg := range testutil.Msgs(feedKey(t, a, testutil.Key("right"))) {
		a.Update(msg)
	}

	got := ansi.Strip(a.renderTitleLine())
	if !strings.Contains(got, "› app (Sessions › ") {
		t.Errorf("title line = %q, want the trail in parentheses after the title", got)
	}
	if !strings.Contains(got, id[:8]+")") {
		t.Errorf("title line = %q, want the short ID closing the trail", got)
	}
}
