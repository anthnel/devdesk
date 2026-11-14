package netdiag

import (
	"strings"
	"testing"
)

// pathHelpContainerParagraphs returns only the paragraphs added for the
// container source (from the one that opens with "ctrl+o opens Trace from"
// to the end), so a phrase the older paragraphs already contain cannot satisfy
// a check by accident.
func pathHelpContainerParagraphs(t *testing.T) string {
	t.Helper()
	for _, s := range New(testConfig()).GetHelpContent().Sections {
		if s.Title != "Path Tab — How it works" {
			continue
		}
		paras := strings.Split(s.Body, "\n\n")
		for i, p := range paras {
			if strings.HasPrefix(p, "ctrl+o opens Trace from") {
				return strings.Join(strings.Fields(strings.Join(paras[i:], " ")), " ")
			}
		}
		t.Fatal(`no paragraph starting with "ctrl+o opens Trace from"`)
	}
	t.Fatal(`no section "Path Tab — How it works"`)
	return ""
}

// DEVDESK-330 step 7, Rule 114: the help says what the container source does,
// what it does not do, and where it stops. Every phrase is checked against the
// container paragraphs only.
func TestPathHelpExplainsTheContainerSource(t *testing.T) {
	body := pathHelpContainerParagraphs(t)
	cases := []struct{ what, want string }{
		{"the key", "ctrl+o"},
		{"the engine offered", "Docker Engine"},
		{"this machine remains the vantage point", "the probes still leave from this machine"},
		{"nothing runs in the container", "nothing runs inside the container"},
		{"Docker Desktop is not offered", "Docker Desktop"},
		{"podman is not offered", "podman"},
		{"not offered", "are not offered"},
		{"no container running", "With no running container, only this machine is offered"},
		{"the engine is only read", "ps, info, inspect"},
		{"nothing is started", "starts nothing"},
		{"a remote engine is not detected", "is not detected as remote"},
		{"a remote engine still traces from here", "the trace still leaves from this machine"},
	}
	for _, tc := range cases {
		if !strings.Contains(body, tc.want) {
			t.Errorf("Path help lacks %q (%s)", tc.want, tc.what)
		}
	}
}
