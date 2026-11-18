package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netproxy"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-583, AC3, remediation path: the Remediation tab's S scans candidate
// images from their registry through scanCandidates, which is not a job and so
// never crosses the router's handleStartJobs. It warns itself, in its own
// footer, when a loopback proxy meets a containerised Trivy.
//
// The seam is the package variable containerForward, which TestMain sets to an
// empty Forward so a developer's own HTTPS_PROXY never reaches a test.

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

// trivyFrom is a detection where only Trivy is known, from the given source.
func trivyFrom(source scan.ToolSource) *scan.Report {
	return &scan.Report{
		EngineAvailable: true,
		Tools: map[scan.ToolID]scan.ToolStatus{
			scan.ToolTrivy: {Available: source != scan.ToolSourceNone, Source: source, Image: "aquasec/trivy:latest"},
		},
	}
}

func remediationWith(t *testing.T, report *scan.Report, mutate func(*config.Config)) Model {
	t.Helper()
	m := onRemediation(t, baseEntries(), nil)
	m.deps = report
	if mutate != nil {
		mutate(m.config)
	}
	return m
}

func TestProxyWarningWhenRemediationScansWithALoopbackProxyAndTrivyInAContainer(t *testing.T) {
	fwd := useContainerForward(t, map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"})
	m := remediationWith(t, trivyFrom(scan.ToolSourceContainer), nil)

	m, cmd := step(t, m, testutil.Key(keymap.Scan))

	want := scan.ContainerProxyWarning(fwd, true)
	if !strings.Contains(want, "127.0.0.1:3128") {
		t.Fatalf("the pure function does not name the proxy: %q", want)
	}
	if m.footer.Text() != want || m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("footer = %q at level %v, want %q at LevelWarning", m.footer.Text(), m.footer.Level(), want)
	}
	if cmd == nil {
		t.Error("no Cmd: the footer timer and the scans are both missing")
	}
	// The warning accompanies the scans, it does not replace them.
	if len(m.remediation.scanning) == 0 {
		t.Error("the warning stopped the scan from starting")
	}
}

func TestProxyWarningOnRemediationNeverCarriesTheCredentials(t *testing.T) {
	useContainerForward(t, map[string]string{"HTTPS_PROXY": "http://alice:s3cr3t@127.0.0.1:3128"})
	m := remediationWith(t, trivyFrom(scan.ToolSourceContainer), nil)

	m, _ = step(t, m, testutil.Key(keymap.Scan))

	if m.footer.Level() != sharedcomponents.LevelWarning {
		t.Fatalf("no warning: footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
	for _, leak := range []string{"alice", "s3cr3t", "@"} {
		if strings.Contains(m.footer.Text(), leak) {
			t.Errorf("footer %q leaks %q", m.footer.Text(), leak)
		}
	}
}

// Before the detection lands (nil report) the configured source decides.
func TestProxyWarningOnRemediationBeforeTheDetectionReadsTheConfiguredSource(t *testing.T) {
	fwd := useContainerForward(t, map[string]string{"HTTPS_PROXY": "http://localhost:3128"})
	m := remediationWith(t, nil, func(c *config.Config) { c.Scan.Tools.Trivy.Source = config.ToolSourceImage })

	m, _ = step(t, m, testutil.Key(keymap.Scan))

	if want := scan.ContainerProxyWarning(fwd, true); m.footer.Text() != want || want == "" {
		t.Errorf("footer = %q, want %q", m.footer.Text(), want)
	}
}

func TestNoProxyWarningWhenRemediationHasNothingToWarnAbout(t *testing.T) {
	loopback := map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"}
	for _, tc := range []struct {
		name   string
		env    map[string]string
		report *scan.Report
		mutate func(*config.Config)
	}{
		{"loopback proxy, Trivy a binary", loopback, trivyFrom(scan.ToolSourceBinary), nil},
		{"no proxy, Trivy in a container", nil, trivyFrom(scan.ToolSourceContainer), nil},
		{"proxy on another machine", map[string]string{"HTTPS_PROXY": "http://10.0.0.1:3128"}, trivyFrom(scan.ToolSourceContainer), nil},
		{"NO_PROXY alone", map[string]string{"NO_PROXY": "127.0.0.1"}, trivyFrom(scan.ToolSourceContainer), nil},
		{"loopback proxy, auto without a detection", loopback, nil, nil},
		{"loopback proxy, Trivy unavailable", loopback, trivyFrom(scan.ToolSourceNone), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useContainerForward(t, tc.env)
			m := remediationWith(t, tc.report, tc.mutate)

			m, cmd := step(t, m, testutil.Key(keymap.Scan))

			if strings.Contains(strings.ToLower(m.footer.Text()), "proxy") {
				t.Errorf("unexpected proxy warning: %q", m.footer.Text())
			}
			if m.footer.Level() == sharedcomponents.LevelWarning && m.footer.IsSet() {
				t.Errorf("unexpected footer warning: %q", m.footer.Text())
			}
			if cmd == nil || len(m.remediation.scanning) == 0 {
				t.Error("the scans did not start: the case no longer exercises scanCandidates")
			}
		})
	}
}

// A refused S (off the tab, nothing to measure) launches nothing, so it has
// nothing to warn about: its footer keeps the refusal's reason.
func TestProxyWarningIsNotShownWhenTheScanIsRefused(t *testing.T) {
	useContainerForward(t, map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"})
	m := scannedModel(t) // the CVE tab, not the Remediation tab
	m.deps = trivyFrom(scan.ToolSourceContainer)

	m, _ = step(t, m, testutil.Key(keymap.Scan))

	if m.footer.Text() != reasonNotRemediationTab {
		t.Errorf("footer = %q, want the refusal %q", m.footer.Text(), reasonNotRemediationTab)
	}
}
