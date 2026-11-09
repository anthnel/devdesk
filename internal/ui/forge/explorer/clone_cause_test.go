package explorer

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

const leakyCloneErr = "fatal: unable to access 'https://oauth2:glpat-SECRET@gitlab.example/alpha/api.git/': 403"

// D97: a git error can carry a URL with credentials; the Detail is shown on
// screen and in screenshots.
func TestACloneFailureDetailCarriesNoSecret(t *testing.T) {
	for _, kind := range []cloneEventKind{cloneEnded, cloneWalkFailed} {
		tr := CloneEventMsg{event: cloneEvent{kind: kind, path: "alpha/api", err: errors.New(leakyCloneErr)}}.Transition()
		if tr.State != jobs.ItemFailed {
			t.Fatalf("kind %v: state = %v, want failed", kind, tr.State)
		}
		for _, secret := range []string{"glpat-SECRET", "oauth2:"} {
			if strings.Contains(tr.Detail, secret) {
				t.Errorf("kind %v: detail %q leaks %q", kind, tr.Detail, secret)
			}
		}
		if !strings.Contains(tr.Detail, "gitlab.example/alpha/api.git") || !strings.Contains(tr.Detail, "403") {
			t.Errorf("kind %v: detail %q lost the useful part of the cause", kind, tr.Detail)
		}
	}
}

func TestAWalkFailureKeepsItsDiscoveryPrefix(t *testing.T) {
	tr := CloneEventMsg{event: cloneEvent{kind: cloneWalkFailed, path: "alpha/sub", err: errors.New("403")}}.Transition()
	if tr.Detail != "discovery: 403" {
		t.Errorf("detail = %q, want %q", tr.Detail, "discovery: 403")
	}
}

// H2: a clone skipped because the directory exists says why.
func TestASkippedCloneSaysItIsAlreadyOnDisk(t *testing.T) {
	tr := CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api", skipped: true}}.Transition()
	if tr.State != jobs.ItemSkipped {
		t.Errorf("state = %v, want skipped", tr.State)
	}
	if tr.Detail != "already on disk" {
		t.Errorf("detail = %q, want %q", tr.Detail, "already on disk")
	}
}

// The log is the other place the raw error went.
func TestTheCloneLogCarriesNoSecret(t *testing.T) {
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })

	h := cloning(t)
	h.emit(
		CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api", err: errors.New(leakyCloneErr)}},
		CloneEventMsg{event: cloneEvent{kind: cloneWalkFailed, path: "alpha/sub", err: errors.New(leakyCloneErr)}},
	)

	if buf.Len() == 0 {
		t.Fatal("nothing was logged for a failed clone")
	}
	if strings.Contains(buf.String(), "glpat-SECRET") {
		t.Errorf("log leaks the token: %s", buf.String())
	}
}
