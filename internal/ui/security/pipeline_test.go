package security

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// The key belongs to the CI tab. A tab is not a different screen (Rule 130), so
// it is greyed elsewhere rather than dropped — the column must not re-order
// itself as the user cycles tabs.
func TestTheResolvedPipelineKeyIsGreyedOffTheCITab(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) { r.CIScanned, r.CIScore = true, "B" }))

	m.switchTab(TabCVE)
	if !testutil.HasShortcut(m.GetShortcuts(), openPipelineKey) {
		t.Fatal("the key vanished off the CI tab instead of being greyed")
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), openPipelineKey) {
		t.Error("the key is offered on the CVE tab")
	}

	m.switchTab(TabCIScore)
	if testutil.ShortcutDisabled(m.GetShortcuts(), openPipelineKey) {
		t.Error("the key is greyed on the CI tab, where it applies")
	}
}

// An image has no pipeline. The refusal is decided before anything is read, so
// the column says so rather than a keystroke finding out.
func TestAnImageOffersNoResolvedPipeline(t *testing.T) {
	result := ciResult(func(r *scan.Result) {
		r.Target, r.TargetType = "nginx:1.25", scan.TargetImage
		r.CIScanned = true
	})
	m := ciModel(t, result)
	m.switchTab(TabCIScore)

	if !testutil.ShortcutDisabled(m.GetShortcuts(), openPipelineKey) {
		t.Error("an image offers the resolved pipeline")
	}
}

// A forge that cannot resolve one says so in the column, not after a git call.
func TestAForgeThatCannotResolveSaysSoInTheColumn(t *testing.T) {
	cfg := testConfig()
	cfg.Forge.Type = config.ForgeGitHub
	m := feed(t, NewWithPreloadedResult(cfg, nil, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIScore = true, "B"
	})), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.switchTab(TabCIScore)

	if !testutil.ShortcutDisabled(m.GetShortcuts(), openPipelineKey) {
		t.Error("a GitHub context offers a resolved pipeline")
	}
}

// The refusal is never silent (Rule 130): the reason canOpenPipeline computed
// is the one the footer prints.
func TestARefusedPipelineKeySaysWhy(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) { r.CIScanned, r.CIScore = true, "B" }))
	m.switchTab(TabCVE)

	m = feed(t, m, testutil.Key(openPipelineKey))
	if got := plain(m.RenderFooter(160)); !strings.Contains(got, reasonNotCITab) {
		t.Errorf("the footer does not carry the reason: %q", got)
	}
}

// The key hands a source to the router, which is what opens the viewer. The
// fetch is the source's, so it runs on the viewer's own Cmd (Rule 110).
func TestTheKeyHandsAPipelineSourceToTheRouter(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) { r.CIScanned, r.CIScore = true, "B" }))
	m.switchTab(TabCIScore)

	_, cmd := step(t, m, testutil.Key(openPipelineKey))
	msg, ok := testutil.MsgOf[uiviewer.OpenRequestMsg](cmd)
	if !ok {
		t.Fatal("no OpenRequestMsg came back")
	}
	source, ok := msg.Source.(pipelineSource)
	if !ok {
		t.Fatalf("the source is %T, want pipelineSource", msg.Source)
	}
	if source.RepoPath != "/repos/devdesk" {
		t.Errorf("RepoPath = %q, want the result's target", source.RepoPath)
	}
}

// The forge-side path is everything after the host, whatever form the remote
// takes — an SCP-like remote addresses the same project as an HTTPS one.
func TestTheProjectPathIsReadFromEitherRemoteForm(t *testing.T) {
	for _, tc := range []struct{ remote, want string }{
		{"https://git.example.test/group/sub/app.git", "group/sub/app"},
		{"git@git.example.test:group/sub/app.git", "group/sub/app"},
		{"https://user:token@git.example.test/group/app", "group/app"},
		{"git.example.test", ""},
	} {
		if got := projectPathOf(tc.remote); got != tc.want {
			t.Errorf("projectPathOf(%q) = %q, want %q", tc.remote, got, tc.want)
		}
	}
}
