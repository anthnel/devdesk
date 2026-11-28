package docker

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-24 (DEVDESK-822), criterion 5: how docker and podman answer when the engine is
// absent, stopped or refuses access, as read by probe on the engine VMs on 2026-11-28
// (docs/measurements/2026-11-28-engine-unavailable.md): docker 29.10.1 and 28.5.2
// rootful, podman 6.2.1 and 5.8.11 rootless and rootful.
//
// Recorded by probe, NOT versioned fixtures (decision Q1 of the plan: the 40 versioned
// fixtures hold none of these cases, and the contract bench cannot produce them; a
// follow-up task versions them, with DEVDESK-949). The model is removal_probe_test.go.
// Each entry carries the case id of the measurement, the client version and the
// configuration it was read on; every one was probed on probeDate.
//
// The texts are the whole stderr, a final line feed included. The measurement writes
// them with placeholders (<endpoint>, <chemin>, <cause>): each entry fills them with
// the value the measurement gives for that case.
const probeDate = "2026-11-28"

const (
	// DK-A, docker 29 and 28: the daemon does not answer (a stopped daemon whose socket
	// file is left, a socket nobody listens on, a refused tcp port; with docker 28 also
	// a socket path that does not exist).
	dkA = "Cannot connect to the Docker daemon at %s. Is the docker daemon running?\n"
	// DK-B, docker 29 only: the same cases when the socket file is absent.
	dkB = "failed to connect to the docker API at %s; check if the path is correct and if the daemon is running: dial unix %s: connect: no such file or directory\n"
	// DK-C, docker 29 only: the socket refuses this user.
	dkC = "permission denied while trying to connect to the docker API at unix:///var/run/docker.sock\n"
	// DK-D, docker 28 only: the same, whose route depends on the verb.
	dkD = "permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: %s \"http://%%2Fvar%%2Frun%%2Fdocker.sock/%s\": dial unix /var/run/docker.sock: connect: permission denied\n"

	// PM-A, podman 6.2.1, remote client: two lines, only the end of the second one
	// (the cause after "connect: ") tells the three causes apart.
	pmA = "Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, " +
		"or try `podman machine init` and `podman machine start` to manage a new Linux VM\n" +
		"Error: unable to connect to Podman socket: Get \"http://d/v6.2.1/libpod/_ping\": dial unix %s: connect: %s\n"
	// PM-B, podman 5.8.11: the same two lines, the version differs and the second one
	// ends with `: unix://<path>` after the cause.
	pmB = "Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, " +
		"or try `podman machine init` and `podman machine start` to manage a new Linux VM\n" +
		"Error: unable to connect to Podman socket: Get \"http://d/v5.8.11/libpod/_ping\": dial unix %s: connect: %s: unix://%s\n"

	// PM-C (6.2.1) and PM-D (5.8.11): a runtime directory the client cannot use, local.
	pmC = "error opening namespace handles: Permission denied\n"
	pmD = "Failed to obtain podman configuration: mkdir /root/libpod: permission denied\n"
	// PM-E, both versions: a storage the client cannot open, local.
	pmE = "Error: creating runtime static files directory \"/home/fixture-user/.local/share/containers/storage/libpod\": " +
		"mkdir /home/fixture-user/.local/share/containers/storage/libpod: permission denied\n"
	// PM-F, both versions: no subuid range. The command SUCCEEDS (exit 0) and these two
	// lines are only warnings; they are in the table to prove they never read as an error.
	pmF = "time=\"<time>\" level=error msg=\"cannot find UID/GID for user fixture-user: no subuid ranges found for user \\\"fixture-user\\\" " +
		"in /etc/subuid - check rootless mode in man pages.\"\n" +
		"time=\"<time>\" level=warning msg=\"Using rootless single mapping into the namespace. This might break some images. " +
		"Check /etc/subuid and /etc/subgid for adding sub*ids if not using a network user\"\n"
)

// probedAnswer is one engine answer of the measurement.
type probedAnswer struct {
	id      string // the case of the measurement (D1, P2a...)
	engine  string
	version string
	mode    string // "rootful" or "rootless"
	stderr  string
	want    Unavailability
}

func (p probedAnswer) String() string {
	return fmt.Sprintf("%s %s %s %s (probed %s)", p.id, p.engine, p.version, p.mode, probeDate)
}

func docker29(id, stderr string, want Unavailability) probedAnswer {
	return probedAnswer{id, engine.Docker, "29.10.1", "rootful", stderr, want}
}

func docker28(id, stderr string, want Unavailability) probedAnswer {
	return probedAnswer{id, engine.Docker, "28.5.2", "rootful", stderr, want}
}

// podman6 and podman5 build the remote-client answer of the two versions for a socket
// path and a cause ("no such file or directory", "connection refused", "permission denied").
func podman6(id, mode, path, cause string, want Unavailability) probedAnswer {
	return probedAnswer{id, engine.Podman, "6.2.1", mode, fmt.Sprintf(pmA, path, cause), want}
}

func podman5(id, mode, path, cause string, want Unavailability) probedAnswer {
	return probedAnswer{id, engine.Podman, "5.8.11", mode, fmt.Sprintf(pmB, path, cause, path), want}
}

const (
	causeNoSocket = "no such file or directory"
	causeRefused  = "connection refused"
	causeDenied   = "permission denied"
)

var probedAnswers = []probedAnswer{
	// Docker, rootful. D1: stopped daemon, socket file left; D2: socket bound without
	// listen; D2c, D1b: socket path absent; D4: user outside the docker group; D5: tcp refused.
	docker29("D1", fmt.Sprintf(dkA, "unix:///var/run/docker.sock"), EngineUnreachable),
	docker28("D1", fmt.Sprintf(dkA, "unix:///var/run/docker.sock"), EngineUnreachable),
	docker29("D1b", fmt.Sprintf(dkB, "unix:///var/run/docker.sock", "/var/run/docker.sock"), EngineUnreachable),
	docker28("D1b", fmt.Sprintf(dkA, "unix:///var/run/docker.sock"), EngineUnreachable),
	docker29("D2", fmt.Sprintf(dkA, "unix:///tmp/probe-stale.sock"), EngineUnreachable),
	docker28("D2", fmt.Sprintf(dkA, "unix:///tmp/probe-stale.sock"), EngineUnreachable),
	docker29("D2c", fmt.Sprintf(dkB, "unix:///tmp/none.sock", "/tmp/none.sock"), EngineUnreachable),
	docker28("D2c", fmt.Sprintf(dkA, "unix:///tmp/none.sock"), EngineUnreachable),
	docker29("D4", dkC, EnginePermissionDenied),
	docker28("D4 ps", fmt.Sprintf(dkD, "Get", "v1.51/containers/json?all=1"), EnginePermissionDenied),
	docker28("D4 image ls", fmt.Sprintf(dkD, "Head", "_ping"), EnginePermissionDenied),
	docker29("D5", fmt.Sprintf(dkA, "tcp://127.0.0.1:1"), EngineUnreachable),
	docker28("D5", fmt.Sprintf(dkA, "tcp://127.0.0.1:1"), EngineUnreachable),

	// Podman, remote client (--remote, --url or CONTAINER_HOST). The same two lines come
	// with three causes; only the end of the second line tells them apart.
	podman6("P2a", "rootless", "/run/user/2000/podman/podman.sock", causeNoSocket, EngineUnreachable),
	podman5("P2a", "rootless", "/run/user/2000/podman/podman.sock", causeNoSocket, EngineUnreachable),
	podman6("P2c", "rootful", "/run/podman/podman.sock", causeNoSocket, EngineUnreachable),
	podman5("P2c", "rootful", "/run/podman/podman.sock", causeNoSocket, EngineUnreachable),
	podman6("P2d", "rootless", "/tmp/none.sock", causeNoSocket, EngineUnreachable),
	podman5("P2d", "rootless", "/tmp/none.sock", causeNoSocket, EngineUnreachable),
	podman6("P2e", "rootless", "/run/user/2000/podman/podman.sock", causeRefused, EngineUnreachable),
	podman5("P2e", "rootless", "/run/user/2000/podman/podman.sock", causeRefused, EngineUnreachable),
	podman6("P3c", "rootful", "/run/podman/podman.sock", causeRefused, EngineUnreachable),
	podman5("P3c", "rootful", "/run/podman/podman.sock", causeRefused, EngineUnreachable),
	// P3: the rootful socket, asked by an ordinary user. The line also says "Cannot
	// connect to Podman", so permission has to be read before unreachable.
	podman6("P3", "rootless", "/run/podman/podman.sock", causeDenied, EnginePermissionDenied),
	podman5("P3", "rootless", "/run/podman/podman.sock", causeDenied, EnginePermissionDenied),

	// Podman, local failures: not an unavailable engine (decision Q2: they keep the
	// generic "check logs"), even though two of them say "permission denied".
	{"P4c", engine.Podman, "6.2.1", "rootless", pmC, UnavailabilityUnknown},
	{"P4c", engine.Podman, "5.8.11", "rootless", pmD, UnavailabilityUnknown},
	{"P4d", engine.Podman, "6.2.1", "rootless", pmE, UnavailabilityUnknown},
	{"P4d", engine.Podman, "5.8.11", "rootless", pmE, UnavailabilityUnknown},
	// P5: exit 0, so never an error in production; handed as one all the same, it still
	// holds nothing a classifier may read.
	{"P5", engine.Podman, "6.2.1", "rootless", pmF, UnavailabilityUnknown},
	{"P5", engine.Podman, "5.8.11", "rootless", pmF, UnavailabilityUnknown},
}

// errorShapes are the three forms the engine's text reaches ClassifyUnavailable in:
// the error of a read (an *exec.ExitError carrying Stderr), that error wrapped by the
// caller, and the error of a mutation (an *engineError holding the combined output).
func errorShapes(stderr string) map[string]error {
	exit := &exec.ExitError{Stderr: []byte(stderr)}
	return map[string]error{
		"exit error":           exit,
		"wrapped exit error":   fmt.Errorf("docker ps failed: %w", exit),
		"mutation (combined)":  &engineError{Label: "docker rm", Output: strings.TrimSpace(stderr), Err: errExit},
		"wrapped mutation":     fmt.Errorf("remove: %w", &engineError{Label: "docker rm", Output: stderr, Err: errExit}),
		"combined, no newline": &engineError{Label: "podman ps", Output: strings.TrimRight(stderr, "\n"), Err: errExit},
	}
}

// Criterion 5, on the four configurations: every probed answer reads as the class the
// measurement shows, whichever shape the error has.
func TestClassifyUnavailableReadsTheProbedAnswers(t *testing.T) {
	for _, p := range probedAnswers {
		t.Run(p.String(), func(t *testing.T) {
			for shape, err := range errorShapes(p.stderr) {
				if got := ClassifyUnavailable(err); got != p.want {
					t.Errorf("%s: ClassifyUnavailable(%q) = %d, want %d", shape, p.stderr, got, p.want)
				}
			}
		})
	}
}

// The probes must cover the four configurations of the measurement and the three
// classes, or the table above would pass with half of it gone.
func TestTheProbeTableCoversEveryConfigurationAndClass(t *testing.T) {
	seenConfig := map[string]bool{}
	seenClass := map[Unavailability]bool{}
	for _, p := range probedAnswers {
		seenConfig[p.engine+" "+p.version] = true
		seenClass[p.want] = true
	}
	for _, config := range []string{"docker 29.10.1", "docker 28.5.2", "podman 6.2.1", "podman 5.8.11"} {
		if !seenConfig[config] {
			t.Errorf("no probed answer for %s", config)
		}
	}
	for _, class := range []Unavailability{EngineUnreachable, EnginePermissionDenied, UnavailabilityUnknown} {
		if !seenClass[class] {
			t.Errorf("no probed answer expected to read as class %d", class)
		}
	}
}

// Criterion 5, a control on the recorded answers: none of the versioned fixtures
// (the 40 of internal/enginefixture/fixtures: rm-running, rmi-used, inspect-absent,
// name-taken, port-taken, ps-health, volumes...) is an unavailable engine, so the
// classifier never reads an ordinary engine answer (a refusal about an object, a
// healthy listing) as one.
//
// What it does NOT guarantee: these 40 hold no answer of an absent, stopped or refusing
// engine, and none that says "permission denied" or "connection refused" on its own, so
// a marker that grew too wide would not necessarily fail here. That is covered by the
// probe table above (its "unknown" entries, the local Podman failures P4c and P4d that say
// "permission denied", and the exit-0 warnings P5) and by TestAnUnknownFailureStaysUnknown.
func TestNoRecordedEngineAnswerReadsAsUnavailable(t *testing.T) {
	fixtures, err := enginefixture.All()
	if err != nil {
		t.Fatalf("loading the versioned fixtures: %v", err)
	}
	// minVersionedFixtures of internal/enginefixture (OC-09b2); never lower.
	if len(fixtures) < 40 {
		t.Fatalf("%d versioned fixtures, want at least 40: the control would be thinner than the plan says", len(fixtures))
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			stderr, stdout := string(f.Exchange.Stderr), string(f.Exchange.Stdout)
			shapes := errorShapes(stderr)
			shapes["combined stdout then stderr"] = &engineError{Label: "docker x", Output: strings.TrimSpace(stdout + stderr), Err: errExit}
			for shape, err := range shapes {
				if got := ClassifyUnavailable(err); got != UnavailabilityUnknown {
					t.Errorf("%s of %s: ClassifyUnavailable = %d, want unknown", shape, f.Name, got)
				}
			}
		})
	}
}
