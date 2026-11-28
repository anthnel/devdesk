package docker

import (
	"errors"
	"testing"
)

// OC-22: the engines' own words for a removal, as read by probe on jakku, docker
// 29.10.1 and 28.5.2, podman 6.2.1 and 5.8.11 rootless (2026-11). Recorded by probe,
// NOT a versioned fixture: the exit codes (1 on docker, 2 on podman) are not part of
// what ClassifyRemoval reads, only the text. Each runs through the real wrappers
// (mutate, so the error is an engineError) and through ClassifyRemoval.

const (
	probedCtrID = "c6fb2d45647efd80cdf694c554e07f5eafee91aaedbeb2b654fba8c4f27cd66f"
	probedImgID = "3ea06e4cb95e726ea6c6cc4377570ac4c4050d78710004cbd631ca809daf7e8e"
)

type probeText struct {
	name       string
	engine     string
	kind       string
	text       string
	wantStatus RemovalStatus
	wantReason RefusalReason
}

var probeTexts = []probeText{
	// Already gone.
	{"docker: no such container", "docker", "container", "Error response from daemon: No such container: " + probedCtrID, RemovalGone, ""},
	{"docker: no such image", "docker", "image", "Error response from daemon: No such image: " + probedImgID, RemovalGone, ""},
	{"docker: no such volume", "docker", "volume", "Error response from daemon: get pgdata: no such volume", RemovalGone, ""},
	{"podman: no such container", "podman", "container", `Error: no container with ID or name "` + probedCtrID + `" found: no such container`, RemovalGone, ""},
	{"podman: image not known", "podman", "image", "Error: " + probedImgID + ": image not known", RemovalGone, ""},
	{"podman: no such volume", "podman", "volume", `Error: no volume with name "pgdata" found: no such volume`, RemovalGone, ""},
	// Running.
	{"docker: container is running", "docker", "container",
		`Error response from daemon: cannot remove container "web": container is running: stop the container before removing or force remove`, RemovalRefused, ReasonRunning},
	{"podman: container state improper", "podman", "container",
		"Error: cannot remove container " + probedCtrID + " as it is running - running or paused containers cannot be removed without force: container state improper", RemovalRefused, ReasonRunning},
	// In use.
	{"docker: volume is in use", "docker", "volume",
		"Error response from daemon: remove pgdata: volume is in use - [" + probedCtrID + ", " + probedCtrID + "]", RemovalRefused, ReasonInUse},
	{"podman: volume is being used", "podman", "volume",
		"Error: volume pgdata is being used by the following container(s): " + probedCtrID + ": volume is being used", RemovalRefused, ReasonInUse},
}

func TestClassifyRemovalReadsTheProbedTexts(t *testing.T) {
	for _, tt := range probeTexts {
		t.Run(tt.name, func(t *testing.T) {
			usingEngine(t, tt.engine)
			target := map[string]string{"container": "0123456789ab", "image": "0123456789ab", "volume": "pgdata"}[tt.kind]
			stubStrict(t, reply{argv: argvOne(tt.kind, target), out: tt.text + "\n", err: errors.New("exit status 1")})

			err := removeOne(tt.kind, target)
			status, reason := ClassifyRemoval(err)

			if status != tt.wantStatus || reason != tt.wantReason {
				t.Errorf("ClassifyRemoval(%q) = (%q, %q), want (%q, %q)", err, status, reason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

// Only the engine's words count. DevDesk's own label ("docker rm") and the rest of its
// error text are never read: a label that spells a marker does not make "boom" a
// removal that is already gone, a running container or a thing in use.
func TestClassifyRemovalReadsNothingOfDevDesksLabel(t *testing.T) {
	for _, label := range []string{"docker no such container", "docker volume is in use", "docker container is running", "podman image not known"} {
		err := &engineError{Label: label, Output: "boom", Err: errors.New("exit status 1")}

		status, reason := ClassifyRemoval(err)

		if status != RemovalRefused || reason != ReasonOther {
			t.Errorf("ClassifyRemoval(%q) = (%q, %q), want a refusal of unknown reason", err, status, reason)
		}
	}
}

// docker's `network <name> not found` is read for `network rm` and for the whole
// engine text only: the same words around other text, or under another verb, are not
// "already gone".
func TestDockerNetworkNotFoundIsReadOnlyForNetworkRm(t *testing.T) {
	exit := errors.New("exit status 1")
	tests := []struct {
		name string
		err  error
		want RemovalStatus
	}{
		{"network rm, the whole text", &engineError{Label: "docker network rm", Output: "Error response from daemon: network backend not found", Err: exit}, RemovalGone},
		{"network rm, more text around it", &engineError{Label: "docker network rm", Output: "Error response from daemon: network backend not found in the file /x", Err: exit}, RemovalRefused},
		{"network rm, a path that is not found", &engineError{Label: "docker network rm", Output: "open /var/run/network x not found", Err: exit}, RemovalRefused},
		{"network rm, a name with capitals", &engineError{Label: "docker network rm", Output: "Error response from daemon: network Backend_Net-1 not found", Err: exit}, RemovalGone},
		{"another verb", &engineError{Label: "docker volume rm", Output: "Error response from daemon: network backend not found", Err: exit}, RemovalRefused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if status, _ := ClassifyRemoval(tt.err); status != tt.want {
				t.Errorf("ClassifyRemoval(%q) = %q, want %q", tt.err, status, tt.want)
			}
		})
	}
}
