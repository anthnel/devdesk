package ociresources

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-24 (DEVDESK-822), security review: an agent whose scan request waited for the
// image list is refused with the fixed sentence of an engine that could not be asked,
// never with the engine's text, the URL it was reached by or its socket path. The list
// is the real docker.ListImages on a fake CLI whose stderr is stuffed with canaries.
func TestAPendingAgentRequestIsRefusedWithTheFixedSentence(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	fakecli.Install(t, fakecli.Scenario{
		{Tool: "docker", Args: []string{"image", "ls"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: "Cannot connect to the Docker daemon at " +
			"tcp://admin:s3cret@canary.example:2375 (DOCKER_HOST=unix:///run/canary.sock). Is the docker daemon running?\n"}},
	}, "docker")

	m := newTestModel(t)
	m, cmd := step(t, m, ImageScanRequestedMsg{Images: []string{"cache:v2"}, Invocation: "inv-1"})
	failed := testutil.Msgs(cmd) // the fetch of the list, which fails
	if len(failed) != 1 {
		t.Fatalf("the deferred request started %d messages, want the list read", len(failed))
	}
	_, cmd = step(t, m, failed[0])

	var refused *jobs.RefusedMsg
	for _, msg := range testutil.Msgs(cmd) {
		if r, ok := msg.(jobs.RefusedMsg); ok {
			refused = &r
		}
	}
	if refused == nil {
		t.Fatal("the pending request was not refused")
	}
	if want := "Cannot list images: Cannot reach the Docker daemon — is it running?"; refused.Reason != want {
		t.Errorf("reason = %q, want %q", refused.Reason, want)
	}
	for _, fragment := range []string{"canary", "s3cret", "admin", "tcp://", "unix://", "DOCKER_HOST", "2375"} {
		if strings.Contains(refused.Reason, fragment) {
			t.Errorf("the reason sent to the agent holds %q: %q", fragment, refused.Reason)
		}
	}
}

// An error the classifier does not know is refused with a generic reason: no text.
func TestAnUnknownImageListFailureIsRefusedWithoutEngineText(t *testing.T) {
	fakecli.Install(t, fakecli.Scenario{
		{Tool: "docker", Args: []string{"image", "ls"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: "Error: canary exploded at https://u:s3cret@canary.example\n"}},
	}, "docker")

	_, err := docker.ListImages()
	if err == nil {
		t.Fatal("the fake docker answered, want a failure")
	}

	if got := imagesRefusal(err); got != "Cannot list images — check logs" {
		t.Errorf("imagesRefusal = %q", got)
	}
}
