package mcp

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-24 (DEVDESK-822), security review: an agent that asks for the containers or the
// images while the engine cannot be asked is told one fixed sentence. The engine's
// text, the URL it was reached by (host and credentials) and its socket path never
// reach the client of the MCP server. The engine is the real docker.ListContainers /
// docker.ListImages on a fake CLI (fakecli) that fails with a stderr stuffed with
// canaries.

const unavailableCanaryStderr = "Cannot connect to the Docker daemon at tcp://admin:s3cret@canary.example:2375 " +
	"(DOCKER_HOST=unix:///run/canary.sock). Is the docker daemon running?\n"

func TestAnUnavailableEngineIsOneFixedSentenceForAnAgent(t *testing.T) {
	fakeCacheHome(t)
	for _, tool := range []string{"containers_list", "images_list"} {
		t.Run(tool, func(t *testing.T) {
			fakecli.Install(t, fakecli.Scenario{
				{Tool: "docker", Args: []string{"ps"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: unavailableCanaryStderr}},
				{Tool: "docker", Args: []string{"image", "ls"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: unavailableCanaryStderr}},
			}, "docker")

			res := callToolExpectingError(t, connect(t, testEnv(nil)), tool, nil)

			if !strings.Contains(res, "Cannot reach the Docker daemon — is it running?") {
				t.Errorf("the error is %s, want the fixed sentence", res)
			}
			for _, fragment := range []string{"canary", "s3cret", "admin", "tcp://", "unix://", "DOCKER_HOST", "2375"} {
				if strings.Contains(res, fragment) {
					t.Errorf("the error sent to the agent holds %q: %s", fragment, res)
				}
			}
		})
	}
}

// A missing binary is told the same way.
func TestAMissingEngineIsOneFixedSentenceForAnAgent(t *testing.T) {
	fakeCacheHome(t)
	fakecli.Install(t, fakecli.Scenario{}, "podman") // docker is not on PATH

	res := callToolExpectingError(t, connect(t, testEnv(nil)), "containers_list", nil)

	if !strings.Contains(res, "Docker not found on PATH — install it or set app.container_engine") {
		t.Errorf("the error is %s, want the fixed sentence", res)
	}
}
