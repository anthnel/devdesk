package docker

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// The contracts of OC-09a2 (DEVDESK-943), step 9 of plan OC-09a: they reach a real
// docker or podman in a test VM, never in `go test`, where requireEngine(t) skips
// them. They create resources, so they need the lab marks (the run id of
// contract.sh and /etc/dd/current) on top of the gate, and every call goes through
// the recording runner and its argv rule.

// A running container cannot be removed without force. The exit code and the
// words are what the engine says, so they are logged and not required: the
// codes measured on 2026-11-27 (docker 1, podman 2, docs/measurements/
// 2026-11-27-engines.md) differ, and a difference is noted in a dated measure, not
// turned into a failure here.
func TestContractRemovingARunningContainerIsRefused(t *testing.T) {
	c := requireEngine(t)
	id := c.container("c1")

	var err error
	ex := c.exchange("containers", "rm-running", "rm of a running container is refused", func() {
		err = RemoveContainer(id, false)
	})

	if err == nil {
		t.Fatalf("RemoveContainer(%s, false) = nil on a running container, want the engine to refuse it", id)
	}
	if ex.Exit == 0 {
		t.Errorf("exit code = 0 for a refused removal, want non-zero")
	}
	if len(bytes.TrimSpace(ex.Stderr)) == 0 {
		t.Errorf("stderr is empty, want the engine's reason")
	}
	t.Logf("%s %s %s: rm of a running container exits %d, stderr %q",
		c.s.Engine.Tool, c.s.Engine.Version, c.s.Engine.Mode, ex.Exit, strings.TrimSpace(string(ex.Stderr)))
}

// The cleanup of a run removes the resources of that run and nothing else, with a
// real engine: the self-check that completes the one of contract.sh (a container
// created with no label at all). Here the stranger is a container that DOES carry
// a label of the same key, with the value of another run.
func TestContractCleanupTouchesOnlyThisRun(t *testing.T) {
	c := requireEngine(t)
	mine := c.container("c1")
	other := c.sibling("x")
	foreign := other.container("f1")

	removed := c.cleanupNow()

	if !slices.Contains(removed, mine) {
		t.Errorf("cleanupNow() = %q, want it to have removed %s, which this run created", removed, mine)
	}
	if slices.Contains(removed, foreign) {
		t.Errorf("cleanupNow() = %q: it removed %s, which carries the label of another run", removed, foreign)
	}
	all, err := ListContainers(true)
	if err != nil {
		t.Fatalf("ListContainers(true) = %v", err)
	}
	var mineLeft, foreignLeft bool
	for _, ctr := range all {
		mineLeft = mineLeft || strings.HasPrefix(mine, ctr.ID)
		foreignLeft = foreignLeft || strings.HasPrefix(foreign, ctr.ID)
	}
	if mineLeft {
		t.Errorf("the container of this run, %s, is still there after the cleanup", mine)
	}
	if !foreignLeft {
		t.Errorf("the container of another run, %s, is gone: the cleanup touched what is not of its run", foreign)
	}

	// Its owner removes it, by its own label.
	if got := other.cleanupNow(); !slices.Contains(got, foreign) {
		t.Errorf("the sibling's cleanupNow() = %q, want it to have removed %s", got, foreign)
	}
}
