package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/netproxy"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-583, AC3: when a scan is launched with a proxy on this machine's
// loopback and a scanner runs in a container, the router tells the user, at
// level Warning, that the container cannot reach that proxy. The router decides
// in Update from a snapshot (Rule 110) and speaks through the footer (Rule 128).
//
// The seam is the package variable containerForward, which TestMain sets to an
// empty Forward so a developer's own HTTPS_PROXY never reaches a test.

// useContainerForward makes the router read env as the process environment.
func useContainerForward(t *testing.T, env map[string]string) netproxy.Forward {
	t.Helper()
	fwd := netproxy.ForwardFromLookup(func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	})
	previous := containerForward
	containerForward = func() netproxy.Forward { return fwd }
	t.Cleanup(func() { containerForward = previous })
	return fwd
}

var loopbackProxy = map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"}

// detection builds a report where the listed tools run from container and every
// other tool a default scan needs runs from a binary.
func detection(t *testing.T, cfg *config.Config, containerised ...scan.ToolID) *scan.Report {
	t.Helper()
	required := scan.Required(cfg.Scan.Categories)
	if len(required) == 0 {
		t.Fatal("the default configuration requires no scanner: the fixture is wrong")
	}
	report := &scan.Report{Tools: map[scan.ToolID]scan.ToolStatus{}, EngineAvailable: true}
	for _, id := range append(required, scan.ToolCosign) {
		report.Tools[id] = scan.ToolStatus{Available: true, Source: scan.ToolSourceBinary, Binary: "/usr/bin/" + string(id)}
	}
	for _, id := range containerised {
		report.Tools[id] = scan.ToolStatus{Available: true, Source: scan.ToolSourceContainer, Image: "example/" + string(id) + ":1"}
	}
	return report
}

// startMsgs launches a run through the router and returns every footer post
// that came out of it, plus the run's work having gone out.
func startMsgs(t *testing.T, a *App, kind jobs.Kind) (posts []components.PostFooterMsg, worked bool) {
	t.Helper()
	testutil.FastTimers(t, &animationBlinkInterval)
	testutil.FastTimers(t, &components.FooterMsgDuration)
	_, cmd := a.Update(jobs.StartMsg{
		Run: jobs.NewRun(kind, command.ViewWorkspaces, "", "~/work", "/repos/a"),
		Work: func(string) tea.Cmd {
			return func() tea.Msg { worked = true; return nil }
		},
	})
	return footerPosts(testutil.Msgs(cmd)), worked
}

func wantWarning(t *testing.T, posts []components.PostFooterMsg, want string) {
	t.Helper()
	if len(posts) != 1 {
		t.Fatalf("got %d footer posts, want exactly 1 warning: %+v", len(posts), posts)
	}
	if posts[0].Level != components.LevelWarning {
		t.Errorf("post level = %v, want LevelWarning", posts[0].Level)
	}
	if posts[0].Text != want {
		t.Errorf("post text = %q, want %q", posts[0].Text, want)
	}
	if posts[0].ID == 0 {
		t.Error("the post carries no ID, so nothing would ever clear it (Rule 128)")
	}
}

func TestProxyWarningOnAScanLaunchedWithALoopbackProxyInAContainer(t *testing.T) {
	fwd := useContainerForward(t, loopbackProxy)
	a := router(t, &fakeView{})
	a.sharedState.Tools = detection(t, a.config, scan.ToolTrivy)

	want := scan.ContainerProxyWarning(fwd, true)
	if !strings.Contains(want, "127.0.0.1:3128") {
		t.Fatalf("the pure function does not name the proxy: %q", want)
	}
	posts, worked := startMsgs(t, a, jobs.KindScan)

	wantWarning(t, posts, want)
	if !worked {
		t.Error("the warning replaced the work: the scan never went out")
	}
	if n := len(a.jobs.Snapshot()); n != 1 {
		t.Errorf("the registry holds %d runs, want 1: the warning must not block the run", n)
	}
}

// Credentials in the proxy variable stay out of the footer (AC2).
func TestProxyWarningNeverCarriesTheProxyCredentials(t *testing.T) {
	useContainerForward(t, map[string]string{"HTTPS_PROXY": "http://alice:s3cr3t@127.0.0.1:3128"})
	a := router(t, &fakeView{})
	a.sharedState.Tools = detection(t, a.config, scan.ToolGitleaks)

	posts, _ := startMsgs(t, a, jobs.KindScan)
	if len(posts) != 1 {
		t.Fatalf("got %d footer posts, want 1", len(posts))
	}
	for _, leak := range []string{"alice", "s3cr3t", "@"} {
		if strings.Contains(posts[0].Text, leak) {
			t.Errorf("footer %q leaks %q", posts[0].Text, leak)
		}
	}
}

// Before the detection lands, the configured source decides (nil Report).
func TestProxyWarningBeforeTheDetectionReadsTheConfiguredSource(t *testing.T) {
	fwd := useContainerForward(t, loopbackProxy)
	a := router(t, &fakeView{})
	a.sharedState.Tools = nil
	a.config.Scan.Tools.Trivy.Source = config.ToolSourceImage

	posts, _ := startMsgs(t, a, jobs.KindScan)
	wantWarning(t, posts, scan.ContainerProxyWarning(fwd, true))

	a.config.Scan.Tools.Trivy.Source = config.ToolSourceAuto
	if posts, _ = startMsgs(t, a, jobs.KindScan); len(posts) != 0 {
		t.Errorf("auto without a detection warned: %+v", posts)
	}
}

// H7: every launch warns again, with no deduplication.
func TestProxyWarningRepeatsOnEveryLaunch(t *testing.T) {
	fwd := useContainerForward(t, loopbackProxy)
	a := router(t, &fakeView{})
	a.sharedState.Tools = detection(t, a.config, scan.ToolTrivy)

	first, _ := startMsgs(t, a, jobs.KindScan)
	second, _ := startMsgs(t, a, jobs.KindScan)
	want := scan.ContainerProxyWarning(fwd, true)
	wantWarning(t, first, want)
	wantWarning(t, second, want)
	if first[0].ID == second[0].ID {
		t.Error("two launches share a footer ID: the second timer would clear the first message early")
	}
}

func TestNoProxyWarningWhenThereIsNothingToWarnAbout(t *testing.T) {
	for _, tc := range []struct {
		name          string
		env           map[string]string
		kind          jobs.Kind
		containerised []scan.ToolID
		mutate        func(*config.Config)
	}{
		{"loopback proxy, every tool a binary", loopbackProxy, jobs.KindScan, nil, nil},
		{"no proxy at all", nil, jobs.KindScan, []scan.ToolID{scan.ToolTrivy}, nil},
		{"proxy on another machine", map[string]string{"HTTPS_PROXY": "http://10.0.0.1:3128"}, jobs.KindScan, []scan.ToolID{scan.ToolTrivy}, nil},
		{"NO_PROXY alone", map[string]string{"NO_PROXY": "127.0.0.1,localhost"}, jobs.KindScan, []scan.ToolID{scan.ToolTrivy}, nil},
		{"loopback proxy, a sync", loopbackProxy, jobs.KindSync, []scan.ToolID{scan.ToolTrivy}, nil},
		{"loopback proxy, a clone", loopbackProxy, jobs.KindClone, []scan.ToolID{scan.ToolTrivy}, nil},
		{"loopback proxy, a delete", loopbackProxy, jobs.KindDelete, []scan.ToolID{scan.ToolTrivy}, nil},
		// A scan needs Required(categories); cosign is not one of them.
		{"loopback proxy, cosign alone in a container, a scan", loopbackProxy, jobs.KindScan, []scan.ToolID{scan.ToolCosign}, nil},
		// H3: a pull needs cosign only, and only while images are verified.
		{"loopback proxy, a pull, cosign a binary", loopbackProxy, jobs.KindPull, []scan.ToolID{scan.ToolTrivy}, nil},
		{"loopback proxy, a pull, verification off", loopbackProxy, jobs.KindPull, []scan.ToolID{scan.ToolCosign}, func(c *config.Config) {
			c.Scan.ImageVerification = config.ImageVerificationOff
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useContainerForward(t, tc.env)
			a := router(t, &fakeView{})
			if tc.mutate != nil {
				tc.mutate(a.config)
			}
			a.sharedState.Tools = detection(t, a.config, tc.containerised...)

			posts, worked := startMsgs(t, a, tc.kind)
			if len(posts) != 0 {
				t.Errorf("unexpected footer posts: %+v", posts)
			}
			if !worked {
				t.Error("the run's work never went out")
			}
		})
	}
}

// H3: cosign runs in a container for a verified pull and fails the same way.
func TestProxyWarningOnAVerifiedPullWithCosignInAContainer(t *testing.T) {
	fwd := useContainerForward(t, loopbackProxy)
	a := router(t, &fakeView{})
	a.config.Scan.ImageVerification = config.ImageVerificationOn
	a.sharedState.Tools = detection(t, a.config, scan.ToolCosign)

	posts, worked := startMsgs(t, a, jobs.KindPull)
	wantWarning(t, posts, scan.ContainerProxyWarning(fwd, true))
	if !worked {
		t.Error("the pull's work never went out")
	}
}

// The message must reach the screen, at level Warning, in the view the user is
// looking at: the router has no footer of its own, it posts to the active view.
func TestProxyWarningIsRenderedByTheActiveViewAtLevelWarning(t *testing.T) {
	fwd := useContainerForward(t, loopbackProxy)
	a := router(t, &fakeView{})
	a.sharedState.Tools = detection(t, a.config, scan.ToolTrivy)
	a.openSecurityView(security.New(a.config, nil), command.ViewWorkspaces)

	posts, _ := startMsgs(t, a, jobs.KindScan)
	text := scan.ContainerProxyWarning(fwd, true)
	wantWarning(t, posts, text)

	// Only the post is fed back: the test timers make its expiry instant.
	if _, cmd := a.Update(posts[0]); cmd != nil {
		testutil.Msgs(cmd)
	}
	raw := a.Render()
	if !strings.Contains(ansi.Strip(raw), text) {
		t.Fatalf("the active view does not show %q:\n%s", text, ansi.Strip(raw))
	}
	if styled := components.LevelWarning.Style().Render(text); !strings.Contains(raw, styled) {
		t.Errorf("the message is on screen but not styled as a warning (level Warning style %q not found)", styled)
	}
}
