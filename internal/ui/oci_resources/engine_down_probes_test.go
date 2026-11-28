package ociresources

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-24 (DEVDESK-822): how docker and podman answered when the engine was absent,
// stopped or refused access, as read by probe on the engine VMs on 2026-11-28
// (docs/measurements/2026-11-28-engine-unavailable.md): docker 29.10.1 and 28.5.2
// rootful, podman 6.2.1 and 5.8.11.
//
// These are the texts of internal/docker/unavailable_probe_test.go, copied verbatim
// (that table is unexported and belongs to package docker): the whole stderr, final
// line feed included, with the measurement's placeholders filled the way each case
// of that table fills them. A view test is never given a text nobody measured, so a
// new case is probed and added there first.
const (
	// DK-A, docker 29 and 28: the daemon does not answer.
	probeDKA = "Cannot connect to the Docker daemon at %s. Is the docker daemon running?\n"
	// DK-B, docker 29 only: the socket file is absent.
	probeDKB = "failed to connect to the docker API at %s; check if the path is correct and if the daemon is running: dial unix %s: connect: no such file or directory\n"
	// DK-C, docker 29 only: the socket refuses this user.
	probeDKC = "permission denied while trying to connect to the docker API at unix:///var/run/docker.sock\n"
	// DK-D, docker 28 only: the same, whose route depends on the verb.
	probeDKD = "permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: %s \"http://%%2Fvar%%2Frun%%2Fdocker.sock/%s\": dial unix /var/run/docker.sock: connect: permission denied\n"

	// PM-A, podman 6.2.1, remote client: two lines, the cause is the end of the second.
	probePMA = "Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, " +
		"or try `podman machine init` and `podman machine start` to manage a new Linux VM\n" +
		"Error: unable to connect to Podman socket: Get \"http://d/v6.2.1/libpod/_ping\": dial unix %s: connect: %s\n"
	// PM-B, podman 5.8.11: the same two lines, ending with `: unix://<path>`.
	probePMB = "Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, " +
		"or try `podman machine init` and `podman machine start` to manage a new Linux VM\n" +
		"Error: unable to connect to Podman socket: Get \"http://d/v5.8.11/libpod/_ping\": dial unix %s: connect: %s: unix://%s\n"

	// PM-C, podman 6.2.1: a runtime directory the client cannot use, local. Not an
	// unavailable engine (plan, Q2): the generic message stays.
	probePMC = "error opening namespace handles: Permission denied\n"
)

// The three sentences of the acceptance criteria and their Podman counterparts.
const (
	sentenceDockerNotFound   = "Docker not found on PATH — install it or set app.container_engine"
	sentenceDockerUnreach    = "Cannot reach the Docker daemon — is it running?"
	sentenceDockerPermission = "Permission denied on the Docker socket — add your user to the docker group"

	// To be validated by the techwriter (plan, Q2), like the classes behind them.
	sentencePodmanNotFound   = "Podman not found on PATH — install it or set app.container_engine"
	sentencePodmanUnreach    = "Cannot reach Podman — is the Podman machine or service running?"
	sentencePodmanPermission = "Permission denied on the Podman socket — check that your user may open it"
)

// engineProbe is one probed answer and what a view must say about it.
type engineProbe struct {
	id     string // the case of the measurement (D1, P2e...)
	tool   string // the engine that answered, and the one the view is configured for
	exit   int    // the exit code the measurement gives
	stderr string
	want   string // the fixed sentence, "" when the failure stays unknown
}

func (p engineProbe) String() string { return fmt.Sprintf("%s %s", p.tool, p.id) }

var (
	// Unreachable (criterion 2).
	probesUnreachable = []engineProbe{
		{"D1 docker 29.10.1", engine.Docker, 1, fmt.Sprintf(probeDKA, "unix:///var/run/docker.sock"), sentenceDockerUnreach},
		{"D1b docker 29.10.1", engine.Docker, 1, fmt.Sprintf(probeDKB, "unix:///var/run/docker.sock", "/var/run/docker.sock"), sentenceDockerUnreach},
		{"D1b docker 28.5.2", engine.Docker, 1, fmt.Sprintf(probeDKA, "unix:///var/run/docker.sock"), sentenceDockerUnreach},
		{"D5 docker 29.10.1", engine.Docker, 1, fmt.Sprintf(probeDKA, "tcp://127.0.0.1:1"), sentenceDockerUnreach},
		{"P2e podman 6.2.1", engine.Podman, 125, fmt.Sprintf(probePMA, "/run/user/2000/podman/podman.sock", "connection refused"), sentencePodmanUnreach},
		{"P2a podman 5.8.11", engine.Podman, 125, fmt.Sprintf(probePMB, "/run/user/2000/podman/podman.sock", "no such file or directory", "/run/user/2000/podman/podman.sock"), sentencePodmanUnreach},
	}
	// Permission denied on the socket (criterion 3).
	probesPermission = []engineProbe{
		{"D4 docker 29.10.1", engine.Docker, 1, probeDKC, sentenceDockerPermission},
		{"D4 docker 28.5.2 (ps)", engine.Docker, 1, fmt.Sprintf(probeDKD, "Get", "v1.51/containers/json?all=1"), sentenceDockerPermission},
		{"P3 podman 6.2.1", engine.Podman, 125, fmt.Sprintf(probePMA, "/run/podman/podman.sock", "permission denied"), sentencePodmanPermission},
		{"P3 podman 5.8.11", engine.Podman, 125, fmt.Sprintf(probePMB, "/run/podman/podman.sock", "permission denied", "/run/podman/podman.sock"), sentencePodmanPermission},
	}
	// A failure nothing classifies keeps the generic message (criterion 5).
	probesUnknown = []engineProbe{
		{"P4c podman 6.2.1", engine.Podman, 1, probePMC, ""},
		{"an exit code and no words", engine.Docker, 1, "", ""},
		{"an error nobody measured", engine.Docker, 1, "Error response from daemon: something nobody probed\n", ""},
	}
)

// trapped are answers whose stderr is stuffed with what must never reach a screen:
// a user name and password in an URL, a socket path, the name of the variable that
// sent the client there, and a proxy. The first line of each is one of the measured
// shapes, so it is classified the way a real one is; "canary" is the value of
// internal/testutil/canary.
var trapped = []engineProbe{
	{"unreachable, URL with credentials", engine.Docker, 1,
		"Cannot connect to the Docker daemon at tcp://admin:s3cret@canary.example:2375 (DOCKER_HOST=unix:///run/canary.sock). Is the docker daemon running?\n",
		sentenceDockerUnreach},
	{"permission, socket and variable", engine.Docker, 1,
		"permission denied while trying to connect to the docker API at unix:///run/canary.sock (DOCKER_HOST=tcp://admin:s3cret@canary.example:2375)\n",
		sentenceDockerPermission},
	{"unknown, URL and proxy", engine.Docker, 1,
		"Error: canary exploded at https://u:s3cret@canary.example (HTTPS_PROXY=http://p:q@canary.example:3128)\n",
		""},
}

// neverOnScreen are fragments no screen may show, whatever the engine said.
var neverOnScreen = []string{
	"canary", "s3cret", "admin", "tcp://", "unix://", "https://", "http://", "://",
	"DOCKER_HOST", "HTTPS_PROXY", "CONTAINER_HOST", "2375", "3128", "docker.sock", "podman.sock", "libpod",
	"Is the docker daemon", "Please verify", "podman machine", "dial unix", "connect:",
}
