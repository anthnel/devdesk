package scan

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-583, AC3: a proxy on this machine's loopback cannot be reached from a
// scanner container, which has its own loopback. The two functions under test
// are pure so the router and the Remediation tab can both call them from Update
// (Rule 110) and agree.

// forwardOf is the helper of container_run_test.go.

// ── ContainerProxyWarning ────────────────────────────────────────────────────

// The text the plan fixes (step 6): the variable as it was read, its display
// form, never the raw value.
func TestContainerProxyWarningExactText(t *testing.T) {
	fwd := forwardOf(map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"})
	want := "HTTPS_PROXY is http://127.0.0.1:3128, on this machine's loopback: scanner containers cannot reach it"
	if got := ContainerProxyWarning(fwd, true); got != want {
		t.Errorf("ContainerProxyWarning = %q, want %q", got, want)
	}
}

func TestContainerProxyWarningForALoopbackProxyInAContainer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		variable string
	}{
		{"ipv4 literal", map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"}, "HTTPS_PROXY"},
		{"localhost", map[string]string{"HTTPS_PROXY": "http://localhost:3128"}, "HTTPS_PROXY"},
		{"localhost without a scheme", map[string]string{"HTTPS_PROXY": "localhost:3128"}, "HTTPS_PROXY"},
		{"ipv6 literal", map[string]string{"HTTPS_PROXY": "http://[::1]:3128"}, "HTTPS_PROXY"},
		{"http variable only", map[string]string{"HTTP_PROXY": "http://127.0.0.1:8080"}, "HTTP_PROXY"},
		{"lower case read", map[string]string{"https_proxy": "http://127.0.0.1:3128"}, "https_proxy"},
		{"https wins over http", map[string]string{
			"HTTP_PROXY":  "http://127.0.0.1:8080",
			"HTTPS_PROXY": "http://127.0.0.1:3128",
		}, "HTTPS_PROXY"},
		{"loopback behind a remote http proxy", map[string]string{
			"HTTP_PROXY":  "http://proxy.corp:3128",
			"HTTPS_PROXY": "http://127.0.0.1:3128",
		}, "HTTPS_PROXY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fwd := forwardOf(tc.env)
			variable, display, ok := fwd.Loopback()
			if !ok || variable != tc.variable {
				t.Fatalf("fixture is not the loopback case it claims: Loopback() = %q, %q, %v", variable, display, ok)
			}
			got := ContainerProxyWarning(fwd, true)
			if got == "" {
				t.Fatal("no warning for a loopback proxy and a containerised tool")
			}
			if !strings.Contains(got, variable) {
				t.Errorf("warning %q does not name the variable %q", got, variable)
			}
			if !strings.Contains(got, display) {
				t.Errorf("warning %q does not carry the display form %q", got, display)
			}
			if !strings.Contains(got, "cannot reach") {
				t.Errorf("warning %q does not say the proxy is unreachable from the container", got)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Errorf("warning %q is not one footer line", got)
			}
		})
	}
}

// AC2 meets AC3: the warning goes to the footer, the footer goes to the screen
// and to logs, so neither a credential nor the raw value may be in it.
func TestContainerProxyWarningNeverCarriesACredentialOrTheRawValue(t *testing.T) {
	raw := "http://alice:s3cr3t%40x@127.0.0.1:3128/path?token=abc#frag"
	got := ContainerProxyWarning(forwardOf(map[string]string{"HTTPS_PROXY": raw}), true)
	if got == "" {
		t.Fatal("no warning for a loopback proxy with credentials")
	}
	for _, leak := range []string{raw, "alice", "s3cr3t", "s3cr3t%40x", "@", "token", "abc", "/path", "frag"} {
		if strings.Contains(got, leak) {
			t.Errorf("warning %q leaks %q", got, leak)
		}
	}
	if !strings.Contains(got, "http://127.0.0.1:3128") {
		t.Errorf("warning %q does not carry the display form", got)
	}
}

func TestContainerProxyWarningIsEmptyWhenThereIsNothingToWarnAbout(t *testing.T) {
	loopback := map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"}
	for _, tc := range []struct {
		name         string
		fwd          netproxy.Forward
		containerise bool
	}{
		{"loopback proxy, tools run as binaries", forwardOf(loopback), false},
		{"zero Forward, containerised", netproxy.Forward{}, true},
		{"empty Forward, containerised", forwardOf(nil), true},
		{"zero Forward, not containerised", netproxy.Forward{}, false},
		{"proxy on another machine", forwardOf(map[string]string{"HTTPS_PROXY": "http://10.0.0.1:3128"}), true},
		{"proxy named by host", forwardOf(map[string]string{"HTTPS_PROXY": "http://proxy.corp:3128"}), true},
		// H6: only localhost itself and loopback addresses, like isLoopbackHost.
		{"proxy.localhost", forwardOf(map[string]string{"HTTPS_PROXY": "http://proxy.localhost:3128"}), true},
		{"NO_PROXY alone", forwardOf(map[string]string{"NO_PROXY": "127.0.0.1,localhost"}), true},
		{"NO_PROXY names loopback beside a remote proxy", forwardOf(map[string]string{
			"HTTPS_PROXY": "http://proxy.corp:3128",
			"NO_PROXY":    "127.0.0.1,localhost",
		}), true},
		{"unparseable value", forwardOf(map[string]string{"HTTPS_PROXY": "http://[::1"}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContainerProxyWarning(tc.fwd, tc.containerise); got != "" {
				t.Errorf("ContainerProxyWarning = %q, want none", got)
			}
		})
	}
}

// ── RunsInContainer ──────────────────────────────────────────────────────────

func reportOf(sources map[ToolID]ToolSource) *Report {
	r := &Report{Tools: map[ToolID]ToolStatus{}}
	for id, src := range sources {
		r.Tools[id] = ToolStatus{Available: src != ToolSourceNone, Source: src, Image: "example/" + string(id) + ":1"}
	}
	return r
}

func toolsWith(sources map[ToolID]string) config.ScanTools {
	var tools config.ScanTools
	for id, src := range sources {
		tools.Tool(string(id)).Source = src
	}
	return tools
}

func TestRunsInContainerFromTheDetection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources map[ToolID]ToolSource
		ids     []ToolID
		want    bool
	}{
		{"one id, container", map[ToolID]ToolSource{ToolTrivy: ToolSourceContainer}, []ToolID{ToolTrivy}, true},
		{"one id, binary", map[ToolID]ToolSource{ToolTrivy: ToolSourceBinary}, []ToolID{ToolTrivy}, false},
		{"every id a binary", map[ToolID]ToolSource{
			ToolTrivy: ToolSourceBinary, ToolGitleaks: ToolSourceBinary,
		}, []ToolID{ToolTrivy, ToolGitleaks}, false},
		{"one of several in a container", map[ToolID]ToolSource{
			ToolTrivy: ToolSourceBinary, ToolGitleaks: ToolSourceContainer,
		}, []ToolID{ToolTrivy, ToolGitleaks}, true},
		{"a container tool nobody asked about", map[ToolID]ToolSource{
			ToolTrivy: ToolSourceBinary, ToolGitleaks: ToolSourceContainer,
		}, []ToolID{ToolTrivy}, false},
		{"no ids", map[ToolID]ToolSource{ToolTrivy: ToolSourceContainer}, nil, false},
		// A tool that cannot run runs nowhere: no container will start for it.
		{"unavailable tool", map[ToolID]ToolSource{ToolTrivy: ToolSourceNone}, []ToolID{ToolTrivy}, false},
		{"tool the report does not hold", map[ToolID]ToolSource{ToolGitleaks: ToolSourceContainer}, []ToolID{ToolTrivy}, false},
		{"cosign", map[ToolID]ToolSource{ToolCosign: ToolSourceContainer}, []ToolID{ToolCosign}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RunsInContainer(reportOf(tc.sources), config.ScanTools{}, tc.ids); got != tc.want {
				t.Errorf("RunsInContainer = %v, want %v", got, tc.want)
			}
		})
	}
}

// With a detection the configuration is not consulted: the detection is what
// will run (auto falls back to the image only when no binary was found).
func TestRunsInContainerPrefersTheDetectionOverTheConfiguration(t *testing.T) {
	tools := toolsWith(map[ToolID]string{ToolTrivy: config.ToolSourceImage})
	report := reportOf(map[ToolID]ToolSource{ToolTrivy: ToolSourceBinary})
	if RunsInContainer(report, tools, []ToolID{ToolTrivy}) {
		t.Error("a detected binary was reported as a container because the configuration says image")
	}

	tools = toolsWith(map[ToolID]string{ToolTrivy: config.ToolSourceBinary})
	report = reportOf(map[ToolID]ToolSource{ToolTrivy: ToolSourceContainer})
	if !RunsInContainer(report, tools, []ToolID{ToolTrivy}) {
		t.Error("a detected container was ignored because the configuration says binary")
	}
}

// Before the detection lands (a nil report) the configured source stands in:
// only "image" means a container for certain; auto may well find a binary.
func TestRunsInContainerWithoutADetectionReadsTheConfiguredSource(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools config.ScanTools
		ids   []ToolID
		want  bool
	}{
		{"image", toolsWith(map[ToolID]string{ToolTrivy: config.ToolSourceImage}), []ToolID{ToolTrivy}, true},
		{"auto", toolsWith(map[ToolID]string{ToolTrivy: config.ToolSourceAuto}), []ToolID{ToolTrivy}, false},
		{"binary", toolsWith(map[ToolID]string{ToolTrivy: config.ToolSourceBinary}), []ToolID{ToolTrivy}, false},
		{"unset, a config that predates the setting", config.ScanTools{}, []ToolID{ToolTrivy}, false},
		{"image on another tool than the one asked", toolsWith(map[ToolID]string{ToolGitleaks: config.ToolSourceImage}), []ToolID{ToolTrivy}, false},
		{"image on one of several", toolsWith(map[ToolID]string{ToolGitleaks: config.ToolSourceImage}), []ToolID{ToolTrivy, ToolGitleaks}, true},
		{"cosign image", toolsWith(map[ToolID]string{ToolCosign: config.ToolSourceImage}), []ToolID{ToolCosign}, true},
		{"no ids", toolsWith(map[ToolID]string{ToolTrivy: config.ToolSourceImage}), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RunsInContainer(nil, tc.tools, tc.ids); got != tc.want {
				t.Errorf("RunsInContainer(nil, ...) = %v, want %v", got, tc.want)
			}
		})
	}
}
