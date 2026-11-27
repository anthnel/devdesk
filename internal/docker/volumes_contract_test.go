package docker

import (
	"slices"
	"strings"
	"testing"
)

// A volume a container mounts cannot be removed. Docker says the volume is in use, podman that
// it is being used by a container; the exit code differs, so it is logged and not required.
// OC-09b1 (DEVDESK-807), scenario volumes/rm-used.
func TestContractRemovingAVolumeInUseIsRefused(t *testing.T) {
	c := requireEngine(t)
	volume, _ := c.usedVolume("v1", "c1")

	var err error
	ex := c.exchange("volumes", "rm-used", "rm of a volume a container mounts is refused", func() {
		err = RemoveVolume(volume)
	})

	if err == nil {
		t.Fatalf("RemoveVolume(%s) = nil on a volume a container mounts, want the engine to refuse it", volume)
	}
	if ex.Exit == 0 {
		t.Errorf("exit code = 0 for a refused removal, want non-zero")
	}
	if !saysAny(ex.Stderr, "in use", "being used", "used by") {
		t.Errorf("stderr = %q, want the engine to say that a container uses the volume", strings.TrimSpace(string(ex.Stderr)))
	}
	t.Logf("%s %s %s: rm of a volume in use exits %d, stderr %q",
		c.s.Engine.Tool, c.s.Engine.Version, c.s.Engine.Mode, ex.Exit, strings.TrimSpace(string(ex.Stderr)))
}

// The listing of the volumes holds the volume this run made, with its driver. The fixture
// holds the whole row, the mount point included (its home and run id are redacted). A VM with a
// volume of its own would show it here: after a reset the store is empty.
// OC-09b1 (DEVDESK-807), scenario volumes/ls.
func TestContractListingVolumesShowsTheVolumesOfTheRun(t *testing.T) {
	c := requireEngine(t)
	name := c.volume("v1")

	var (
		list []Volume
		err  error
	)
	c.exchange("volumes", "ls", "volume ls shows the volume of the run", func() {
		list, err = ListVolumes()
	})

	if err != nil {
		t.Fatalf("ListVolumes() = %v", err)
	}
	i := slices.IndexFunc(list, func(v Volume) bool { return v.Name == name })
	if i < 0 {
		t.Fatalf("ListVolumes() = %d volume(s), none is %s, which this run made", len(list), name)
	}
	if list[i].Driver != "local" {
		t.Errorf("Driver = %q, want %q", list[i].Driver, "local")
	}
}

// A volume prune of a store that holds only the volumes of this run is allowed (expectPrune reads the
// store first). What it removes depends on the engine: docker 23 and later removes unused
// anonymous volumes only, so the volume of the run stays and its cleanup removes it, podman
// removes every unused volume. The report is the engine's; the contract requires none of
// the two outcomes and logs which one happened.
// OC-09b1 (DEVDESK-807), scenario volumes/prune-unused.
func TestContractVolumePruneOfAStoreOnlyThisRuns(t *testing.T) {
	c := requireEngine(t)
	name := c.volume("v1")

	var (
		report string
		err    error
	)
	c.expectPrune("volumes", "prune-unused", "volume prune of a store that holds only the volume of the run", "volume", func() {
		report, err = PruneVolumes()
	})

	if err != nil {
		t.Fatalf("PruneVolumes() = %v on a store that holds only %s", err, name)
	}
	left, listErr := ListVolumes()
	if listErr != nil {
		t.Fatalf("ListVolumes() = %v", listErr)
	}
	gone := !slices.ContainsFunc(left, func(v Volume) bool { return v.Name == name })
	t.Logf("%s %s %s: volume prune removed the unused named volume: %v, report %q",
		c.s.Engine.Tool, c.s.Engine.Version, c.s.Engine.Mode, gone, report)
}
