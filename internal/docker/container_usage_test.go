package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-25 (DEVDESK-823), step 1 of the plan: who uses which image, volume and network,
// stopped containers included, read in one pass and joined on what the engines print.
// `D` on an image, a volume or a network reads this snapshot and calls the engine for
// nothing (criteria 2 to 4); the views are another PR.
//
// What the code is expected to offer (container_usage.go, today a stub):
//
//	// ContainerUsage is who uses what, read in one pass over every container, stopped
//	// ones included. The zero value was never read: images and networks are unknown.
//	type ContainerUsage struct{ ... }
//
//	// ListContainerUsage is ListContainerUsageContext under a deadline of its own (at
//	// most 60 s, the bound of ImagesInUse). ONE `ps -a -q --no-trunc`, then, when there
//	// is a container, ONE `inspect --format {{json .}} <ids...>`, joined by Id. Never
//	// one call per image, volume or network, never `network inspect .Containers`
//	// (only the running ones), never `ps --filter ancestor=` (podman answers another
//	// question than docker, measurement of 2026-11-28).
//	func ListContainerUsage() (ContainerUsage, error)
//	func ListContainerUsageContext(ctx context.Context) (ContainerUsage, error)
//
//	// ImageUsers: exactly this image (the ID of `inspect`'s Image, "sha256:" or not,
//	// against the short or full ID of the list), never a derived one. known is false
//	// when the usage was not read or the image of a container could not be read.
//	func (u ContainerUsage) ImageUsers(imageID string) (names []string, known bool)
//	// VolumeUsers is the OC-22 computation (VolumeUsers); known is false for a usage
//	// never read and for an empty name (a nil list is "could not tell", not "unused").
//	func (u ContainerUsage) VolumeUsers(name string) (names []string, known bool)
//	// NetworkUsers joins on the NAME (the key of NetworkSettings.Networks), never on
//	// NetworkID alone. known is false when a container's networks could not be read.
//	func (u ContainerUsage) NetworkUsers(n Network) (names []string, known bool)
//
// Names: sorted, without duplicate, docker's leading "/" dropped, an empty name
// replaced by the 12-character ID. Fail closed: "could not tell" is an error, or
// known == false, never "unused". Every test goes through a strict in-process runner
// (guard G1: no engine is reached) and none calls t.Parallel.
//
// The shapes below are the measurement docs/measurements/2026-11-28-container-usage-
// shapes.md, transcribed field by field (probed 2026-11-28 on the Incus test VMs). They
// are NOT versioned fixtures: no real recording exists for these reads yet (an OC-09
// scenario to add, DEVDESK-949).

const usageProbeDate = "2026-11-28"

// The identifiers of the measurement (placeholders of the document, made hex).
var (
	usageImg      = strings.Repeat("ab", 32) // IMG: the image of every probed container
	usageOtherImg = strings.Repeat("cd", 32)
	usageNetID    = strings.Repeat("11", 32) // NET-ddc-oc25-net
	usageNet2ID   = strings.Repeat("22", 32) // NET-ddc-oc25-net2
	usageBridgeID = strings.Repeat("33", 32) // docker's bridge
	usagePodmanID = strings.Repeat("44", 32) // podman's default network
	usageNoneID   = strings.Repeat("55", 32) // docker's none network
	usageAnonVol  = strings.Repeat("ef", 32) // the anonymous volume of `two`
)

const (
	usageNet  = "ddc-oc25-net"
	usageNet2 = "ddc-oc25-net2"
	usageVol  = "ddc-oc25-vol"
	usageVol2 = "ddc-oc25-vol2"
)

// usageID is a full container ID whose first 12 characters differ from every other's.
func usageID(n int) string { return fmt.Sprintf("%012x", n) + strings.Repeat("0", 52) }

// usageCtr is one container as `inspect --format {{json .}}` prints it, reduced to the
// keys the measurement looked at, plus decoys (Config.Image, ImageName) that no join
// may read.
type usageCtr struct {
	id, name, state string
	image           any // "Image"; nil prints null
	mounts          []map[string]any
	networks        json.RawMessage // NetworkSettings.Networks; empty prints null
	networkMode     string          // HostConfig.NetworkMode
	omit            []string        // top-level keys not printed at all
	set             map[string]any  // top-level keys replaced by a value of any shape
}

// json prints the container the way the engine of shape does: docker's Name starts
// with "/", podman has none and adds ImageName.
func (c usageCtr) json(shape engine.Shape) string {
	name := c.name
	if shape.Name == engine.Docker {
		name = "/" + name
	}
	mounts := c.mounts
	if mounts == nil {
		mounts = []map[string]any{}
	}
	doc := map[string]any{
		"Id": c.id, "Name": name, "Image": c.image,
		"State":           map[string]any{"Status": c.state},
		"Config":          map[string]any{"Image": "decoy/typed:1"},
		"Mounts":          mounts,
		"NetworkSettings": map[string]any{"Networks": c.networks},
		"HostConfig":      map[string]any{"NetworkMode": c.networkMode},
	}
	if shape.Name == engine.Podman {
		doc["ImageName"] = "localhost/decoy/typed:1"
	}
	for k, v := range c.set {
		doc[k] = v
	}
	for _, k := range c.omit {
		delete(doc, k)
	}
	line, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return string(line)
}

// usagePs is what `ps -a -q --no-trunc` prints: the IDs, newest first.
func usagePs(ctrs []usageCtr) string {
	var b strings.Builder
	for i := len(ctrs) - 1; i >= 0; i-- {
		b.WriteString(ctrs[i].id + "\n")
	}
	return b.String()
}

// usageInspect is the grouped inspect: one JSON object per line. The measurement of
// 2026-11-28 shows the order of the arguments on both engines; the stub prints docker's
// lines newest first on purpose, so a read that joined by position would fail.
func usageInspect(shape engine.Shape, ctrs []usageCtr) string {
	var b strings.Builder
	emit := func(c usageCtr) { b.WriteString(c.json(shape) + "\n") }
	if shape.Name == engine.Docker {
		for i := len(ctrs) - 1; i >= 0; i-- {
			emit(ctrs[i])
		}
	} else {
		for _, c := range ctrs {
			emit(c)
		}
	}
	return b.String()
}

// usageEngine stands in for the engine: strict, in process, and a journal. Any argv
// other than `ps -a -q --no-trunc` and `inspect --format {{json .}} <ids...>` fails
// the test, which is how "no other call" is held (no `network inspect`, no
// `--filter ancestor=`, no per-volume call). It is stricter than the real engines, which
// would answer other argv: a test of a view must not take it for the engine.
type usageEngine struct {
	t                    testing.TB
	ps, inspect          string
	psErr, inspectErr    error
	afterPs              func() // runs when `ps` is answered, before it returns
	calls                []dockerCmd
	psCalls, inspectRuns int
}

func (e *usageEngine) LookPath() error { return nil }

func (e *usageEngine) Build(args ...string) *exec.Cmd {
	e.t.Errorf("reading the usage handed %q to a terminal, want only Run", args)
	return exec.Command("docker", args...)
}

func (e *usageEngine) Run(dc dockerCmd) ([]byte, error) {
	e.calls = append(e.calls, dc)
	switch {
	case slices.Equal(dc.Args, []string{"ps", "-a", "-q", "--no-trunc"}):
		e.psCalls++
		if e.afterPs != nil {
			e.afterPs()
		}
		return []byte(e.ps), e.psErr
	case len(dc.Args) > 3 && slices.Equal(dc.Args[:3], []string{"inspect", "--format", "{{json .}}"}):
		e.inspectRuns++
		return []byte(e.inspect), e.inspectErr
	}
	e.t.Errorf("unexpected argv %q: the usage is `ps -a -q --no-trunc` then one `inspect --format {{json .}} <ids...>`", dc.Args)
	return nil, errUnexpectedArgv
}

// install pins the engine and the runner for the test.
func (e *usageEngine) install(t *testing.T, engineName string) {
	t.Helper()
	usingEngine(t, engineName)
	stub(t, &stubRunner{}) // restores the runner and the engine when the test ends
	runner = e
}

// usageOf installs an engine that holds ctrs and reads the usage of it.
func usageOf(t *testing.T, shape engine.Shape, ctrs []usageCtr) (ContainerUsage, *usageEngine, error) {
	t.Helper()
	e := &usageEngine{t: t, ps: usagePs(ctrs), inspect: usageInspect(shape, ctrs)}
	e.install(t, shape.Name)
	u, err := ListContainerUsage()
	return u, e, err
}

// wantBudget checks the cost: one `ps`, then ONE grouped inspect carrying every ID once
// (in any order), and nothing when there is no container.
func wantBudget(t *testing.T, e *usageEngine, ids []string) {
	t.Helper()
	if e.psCalls != 1 {
		t.Errorf("`ps -a -q --no-trunc` ran %d times, want once: %q", e.psCalls, callsOf(&stubRunner{calls: e.calls}))
	}
	if len(ids) == 0 {
		if e.inspectRuns != 0 {
			t.Errorf("an inspect ran with no container to inspect")
		}
		return
	}
	if e.inspectRuns != 1 {
		t.Fatalf("the grouped inspect ran %d times, want once", e.inspectRuns)
	}
	for _, c := range e.calls {
		if c.Args[0] != "inspect" {
			continue
		}
		if got := c.Args[3:]; !sameSet(got, ids) || len(got) != len(ids) {
			t.Errorf("inspect carries %q, want each of %q exactly once", got, ids)
		}
	}
}

// ── The probed shapes ────────────────────────────────────────────────────────

// usageProbe is what one configuration printed for the seven containers of the
// measurement, field by field. Strings are verbatim; IDs are the placeholders above.
type usageProbe struct {
	tool, version, mode string
	image               string // "Image" of every container (same image)
	userMode            string // HostConfig.NetworkMode on a user network
	userNets            string // Networks of a running or stopped container on ddc-oc25-net
	createdNets         string // Networks of a container that was only created
	noneNets            string // Networks of `--network none`
	defaultNets         string // Networks with no network asked for; "null" when none is managed
	defaultMode         string // HostConfig.NetworkMode with no network asked for
}

func (p usageProbe) String() string { return fmt.Sprintf("%s %s %s", p.tool, p.version, p.mode) }

// The measurement, section (a) and (b), probed on 2026-11-28 (usageProbeDate).
var usageProbes = []usageProbe{
	{engine.Docker, "29.10.1", "rootful", "sha256:" + usageImg, usageNet,
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"ddc-oc25-net":{"NetworkID":""}}`, // a docker container that was never started: the ID is empty
		`{"none":{"NetworkID":"` + usageNoneID + `"}}`,
		`{"bridge":{"NetworkID":"` + usageBridgeID + `"}}`, "bridge"},
	{engine.Docker, "28.5.2", "rootful", "sha256:" + usageImg, usageNet,
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"ddc-oc25-net":{"NetworkID":""}}`,
		`{"none":{"NetworkID":"` + usageNoneID + `"}}`,
		`{"bridge":{"NetworkID":"` + usageBridgeID + `"}}`, "bridge"},
	{engine.Podman, "6.2.1", "rootful", usageImg, "bridge",
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"none":{"NetworkID":"none"}}`, // not an ID: `none` is not in `network ls`
		`{"podman":{"NetworkID":"` + usagePodmanID + `"}}`, "bridge"},
	{engine.Podman, "6.2.1", "rootless", usageImg, "bridge",
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"none":{"NetworkID":"none"}}`,
		`null`, "pasta"}, // no network asked for: no managed network at all
	{engine.Podman, "5.8.11", "rootful", usageImg, "bridge",
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"none":{"NetworkID":"none"}}`,
		`{"podman":{"NetworkID":"` + usagePodmanID + `"}}`, "bridge"},
	{engine.Podman, "5.8.11", "rootless", usageImg, "bridge",
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"}}`,
		`{"none":{"NetworkID":"none"}}`,
		`null`, "pasta"},
}

// probedContainers are the seven containers of the measurement ("Les conteneurs sondés"),
// in order of creation. All on one image, which carries two tags (two `image ls` lines on
// docker): the tag a container was created with is in Config.Image, a decoy here.
func (p usageProbe) probedContainers() []usageCtr {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	both := raw(`{"ddc-oc25-net":{"NetworkID":"` + usageNetID + `"},"ddc-oc25-net2":{"NetworkID":"` + usageNet2ID + `"}}`)
	named := func(name string) string { return "ddc-oc25-" + name }
	return []usageCtr{
		{id: usageID(1), name: named("run"), state: "running", image: p.image,
			mounts: []map[string]any{volumeMount(usageVol)}, networks: raw(p.userNets), networkMode: p.userMode},
		{id: usageID(2), name: named("stopped"), state: "exited", image: p.image,
			mounts: []map[string]any{volumeMount(usageVol)}, networks: raw(p.userNets), networkMode: p.userMode},
		{id: usageID(3), name: named("created"), state: "created", image: p.image,
			mounts: []map[string]any{volumeMount(usageVol2)}, networks: raw(p.createdNets), networkMode: p.userMode},
		{id: usageID(4), name: named("none"), state: "running", image: p.image,
			networks: raw(p.noneNets), networkMode: "none"},
		{id: usageID(5), name: named("default"), state: "running", image: p.image,
			networks: raw(p.defaultNets), networkMode: p.defaultMode},
		{id: usageID(6), name: named("two"), state: "running", image: p.image,
			mounts:   []map[string]any{bindMount("/tmp", "/hosttmp"), volumeMount(usageAnonVol)},
			networks: both, networkMode: p.userMode},
		{id: usageID(7), name: named("byid2"), state: "running", image: p.image,
			networks: raw(p.defaultNets), networkMode: p.defaultMode},
	}
}

func idsOf(ctrs []usageCtr) []string {
	ids := make([]string, 0, len(ctrs))
	for _, c := range ctrs {
		ids = append(ids, c.id)
	}
	return ids
}

func ddc(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, "ddc-oc25-"+n)
	}
	return out
}

// Criteria 2 to 4, on the six probed configurations: the image joins by ID whatever
// its form, the volume by Mounts, the network by the NAME, a container that was only
// created included, and the whole read costs two calls.
func TestContainerUsageReadsTheProbedShapes(t *testing.T) {
	for _, p := range usageProbes {
		t.Run(p.String()+" probed "+usageProbeDate, func(t *testing.T) {
			shape := engine.ShapeFor(p.tool)
			ctrs := p.probedContainers()

			u, e, err := usageOf(t, shape, ctrs)

			if err != nil {
				t.Fatalf("ListContainerUsage() error = %v", err)
			}
			wantBudget(t, e, idsOf(ctrs))
			everyone := ddc("byid2", "created", "default", "none", "run", "stopped", "two")
			for _, id := range []string{usageImg[:12], usageImg, "sha256:" + usageImg} {
				got, known := u.ImageUsers(id)
				if !known || !slices.Equal(got, everyone) {
					t.Errorf("ImageUsers(%.20q) = %v, %v; want all seven containers, stopped and created included, known", id, got, known)
				}
			}
			if got, known := u.ImageUsers(usageOtherImg[:12]); !known || len(got) != 0 {
				t.Errorf("ImageUsers(another image) = %v, %v; want nobody, and known", got, known)
			}
			volumes := map[string][]string{
				usageVol: ddc("run", "stopped"), usageVol2: ddc("created"), usageAnonVol: ddc("two"),
				"/tmp": nil, "ddc-oc25-ghost": nil,
			}
			for name, want := range volumes {
				if got, _ := u.VolumeUsers(name); !slices.Equal(got, want) {
					t.Errorf("VolumeUsers(%.20q) = %v, want %v (a bind mount is not a volume, an anonymous one is)", name, got, want)
				}
			}
			networks := []struct {
				name string
				want []string
			}{
				{usageNet, ddc("created", "run", "stopped", "two")},
				{usageNet2, ddc("two")},
				{"none", ddc("none")},
			}
			for _, n := range networks {
				got, known := u.NetworkUsers(Network{Name: n.name})
				if !known || !slices.Equal(got, n.want) {
					t.Errorf("NetworkUsers(%q) = %v, %v; want %v, known", n.name, got, known, n.want)
				}
			}
			// The default network: docker's bridge, podman's podman, and rootless podman's
			// pasta, which is no managed network: a KNOWN "nobody", not an unknown.
			defaultName := map[string]string{engine.Docker: "bridge", engine.Podman: "podman"}[p.tool]
			wantDefault := ddc("byid2", "default")
			if p.defaultNets == "null" {
				wantDefault = nil
			}
			if got, known := u.NetworkUsers(Network{Name: defaultName}); !known || !slices.Equal(got, wantDefault) {
				t.Errorf("NetworkUsers(%q) = %v, %v; want %v, known", defaultName, got, known, wantDefault)
			}
		})
	}
}

// ── The cost ─────────────────────────────────────────────────────────────────

// Two calls whatever the size, on both engines: 40 containers, one `ps`, one `inspect`.
func TestContainerUsageCostsTwoCallsWhateverTheSize(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		var ctrs []usageCtr
		for i := 1; i <= 40; i++ {
			ctrs = append(ctrs, usageCtr{id: usageID(i), name: fmt.Sprintf("c%02d", i), state: "exited", image: usageImg,
				mounts: []map[string]any{volumeMount(fmt.Sprintf("vol%d", i)), volumeMount("shared")}, networks: json.RawMessage(`{"appnet":{"NetworkID":""}}`)})
		}

		u, e, err := usageOf(t, shape, ctrs)

		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}
		wantBudget(t, e, idsOf(ctrs))
		if len(e.calls) != 2 {
			t.Errorf("the engine was started %d times, want 2", len(e.calls))
		}
		if got, _ := u.VolumeUsers("shared"); len(got) != 40 {
			t.Errorf("VolumeUsers(shared) has %d names, want 40", len(got))
		}
		if got, known := u.NetworkUsers(Network{Name: "appnet"}); !known || len(got) != 40 {
			t.Errorf("NetworkUsers(appnet) has %d names (known %v), want 40", len(got), known)
		}
	})
}

// No container: one call, a usage that is known and says nobody uses anything. This is
// the case that tells a read that found nothing from one that was never done.
func TestContainerUsageWithNoContainerDoesNotInspect(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		u, e, err := usageOf(t, shape, nil)

		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}
		wantBudget(t, e, nil)
		if got, known := u.ImageUsers(usageImg[:12]); !known || len(got) != 0 {
			t.Errorf("ImageUsers = %v, %v; want nobody, known", got, known)
		}
		if got, known := u.NetworkUsers(Network{Name: "appnet"}); !known || len(got) != 0 {
			t.Errorf("NetworkUsers = %v, %v; want nobody, known", got, known)
		}
		if got, _ := u.VolumeUsers("pgdata"); len(got) != 0 {
			t.Errorf("VolumeUsers = %v, want nobody", got)
		}
	})
}

// The zero value is a usage nobody read: it never says "unused".
func TestAUsageNeverReadIsUnknown(t *testing.T) {
	var u ContainerUsage

	if _, known := u.ImageUsers(usageImg[:12]); known {
		t.Error("ImageUsers on a usage never read is known: it would read as an unused image")
	}
	if _, known := u.NetworkUsers(Network{Name: "appnet"}); known {
		t.Error("NetworkUsers on a usage never read is known: it would read as an unused network")
	}
	if got, _ := u.VolumeUsers("pgdata"); len(got) != 0 {
		t.Errorf("VolumeUsers = %v, want none", got)
	}
}

// ListVolumeUsers (OC-22, `P` on Volumes) is the volume half of the same read: same
// answers, so the two cannot drift. Its own tests (volume_users_test.go) do not change.
func TestListVolumeUsersIsTheVolumePartOfTheUsage(t *testing.T) {
	for _, p := range usageProbes {
		t.Run(p.String(), func(t *testing.T) {
			shape := engine.ShapeFor(p.tool)
			ctrs := p.probedContainers()
			u, _, err := usageOf(t, shape, ctrs)
			if err != nil {
				t.Fatalf("ListContainerUsage() error = %v", err)
			}

			users, err := ListVolumeUsers()

			if err != nil {
				t.Fatalf("ListVolumeUsers() error = %v", err)
			}
			for volume, names := range users {
				if got, _ := u.VolumeUsers(volume); !slices.Equal(got, names) {
					t.Errorf("VolumeUsers(%.20q) = %v, ListVolumeUsers has %v", volume, got, names)
				}
			}
			if len(users) != 3 {
				t.Errorf("ListVolumeUsers() = %v, want the 3 volumes of the probe", users)
			}
		})
	}
}

// ── Names ────────────────────────────────────────────────────────────────────

// Names come out sorted, without docker's "/", whatever order the engine printed them
// in (the stub prints docker's lines newest first and podman's in order, the engines
// themselves keep the order of the arguments), and a container with no name is
// told by its 12-character ID. Stable: a second read gives the same lists.
func TestUsersAreSortedNamedAndStable(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		nets := json.RawMessage(`{"appnet":{"NetworkID":"` + usageNetID + `"}}`)
		mk := func(n int, name string) usageCtr {
			return usageCtr{id: usageID(n), name: name, state: "exited", image: usageImg,
				mounts: []map[string]any{volumeMount("data"), volumeMount("data")}, networks: nets, networkMode: "appnet"}
		}
		ctrs := []usageCtr{mk(1, "web"), mk(2, "api"), mk(3, ""), mk(4, "db")}
		want := []string{usageID(3)[:12], "api", "db", "web"}

		u, _, err := usageOf(t, shape, ctrs)

		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}
		if got, _ := u.ImageUsers(usageImg[:12]); !slices.Equal(got, want) {
			t.Errorf("ImageUsers = %v, want %v", got, want)
		}
		if got, _ := u.VolumeUsers("data"); !slices.Equal(got, want) {
			t.Errorf("VolumeUsers = %v, want %v (one name per container even when it mounts the volume twice)", got, want)
		}
		if got, _ := u.NetworkUsers(Network{Name: "appnet"}); !slices.Equal(got, want) {
			t.Errorf("NetworkUsers = %v, want %v", got, want)
		}
		again, _, err := usageOf(t, shape, ctrs)
		if err != nil {
			t.Fatalf("second read: %v", err)
		}
		if a, _ := again.ImageUsers(usageImg[:12]); !slices.Equal(a, want) {
			t.Errorf("a second read gave %v, want the same %v", a, want)
		}
	})
}

// ── The joins ────────────────────────────────────────────────────────────────

// The image joins on the ID of `inspect`'s Image and on nothing else: not on the tag a
// container was created with (Config.Image, ImageName: decoys here), and not on an image
// built on it (what `ps --filter ancestor=` would count, and `rmi <id>` does not refuse).
func TestImageUsersIsExactAndReadsOnlyTheImageID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		image := usageImg
		if shape.Name == engine.Docker {
			image = "sha256:" + usageImg
		}
		other := usageOtherImg
		if shape.Name == engine.Docker {
			other = "sha256:" + usageOtherImg
		}
		ctrs := []usageCtr{
			{id: usageID(1), name: "web", state: "running", image: image, networks: json.RawMessage(`{}`)},
			{id: usageID(2), name: "old", state: "exited", image: image, networks: json.RawMessage(`{}`)},
			{id: usageID(3), name: "derived", state: "running", image: other, networks: json.RawMessage(`{}`),
				set: map[string]any{"Config": map[string]any{"Image": usageImg[:12]}}},
		}

		u, _, err := usageOf(t, shape, ctrs)

		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}
		if got, known := u.ImageUsers(usageImg[:12]); !known || !slices.Equal(got, []string{"old", "web"}) {
			t.Errorf("ImageUsers(base) = %v, %v; want old and web: a stopped container counts, `derived` (another image whose Config.Image spells this ID) does not", got, known)
		}
		if got, known := u.ImageUsers(usageOtherImg); !known || !slices.Equal(got, []string{"derived"}) {
			t.Errorf("ImageUsers(other) = %v, %v; want derived", got, known)
		}
	})
}

// An empty ID asks about no image: it says "unknown", it does not join everything or
// nothing.
func TestImageUsersOfNoIDIsUnknown(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ctrs := []usageCtr{{id: usageID(1), name: "web", state: "running", image: usageImg, networks: json.RawMessage(`{}`)}}
		u, _, err := usageOf(t, shape, ctrs)
		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}

		if got, known := u.ImageUsers(usageImg[:12]); !known || !slices.Equal(got, []string{"web"}) {
			t.Fatalf("ImageUsers(its image) = %v, %v; want web, known", got, known)
		}
		if got, known := u.ImageUsers(""); known {
			t.Errorf("ImageUsers(\"\") = %v, known; want unknown", got)
		}
		if got, known := u.ImageUsers("sha256:"); known {
			t.Errorf("ImageUsers(\"sha256:\") = %v, known; want unknown", got)
		}
		if got, known := u.NetworkUsers(Network{ID: usageNetID}); known {
			t.Errorf("NetworkUsers(a network with no name) = %v, known; want unknown: the name is the join key", got)
		}
	})
}

// A network joins on its NAME. The NetworkID is empty for a docker container that was
// only created and is "none" for podman's none network, so it cannot be the key.
func TestNetworkUsersJoinsOnTheNameNotOnTheID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ctrs := []usageCtr{
			{id: usageID(1), name: "run", state: "running", image: usageImg, networks: json.RawMessage(`{"appnet":{"NetworkID":"` + usageNetID + `"}}`)},
			{id: usageID(2), name: "created", state: "created", image: usageImg, networks: json.RawMessage(`{"appnet":{"NetworkID":""}}`)},
			{id: usageID(3), name: "isolated", state: "running", image: usageImg, networks: json.RawMessage(`{"none":{"NetworkID":"none"}}`)},
			{id: usageID(4), name: "elsewhere", state: "running", image: usageImg, networks: json.RawMessage(`{"other":{"NetworkID":"` + usageNet2ID + `"}}`)},
		}
		u, _, err := usageOf(t, shape, ctrs)
		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}

		// The Network the list gives carries its ID too: it must not change the answer.
		got, known := u.NetworkUsers(Network{ID: usageNetID[:12], Name: "appnet"})

		if !known || !slices.Equal(got, []string{"created", "run"}) {
			t.Errorf("NetworkUsers(appnet) = %v, %v; want created and run (the empty NetworkID of `created` is not \"no network\")", got, known)
		}
		if got, known := u.NetworkUsers(Network{Name: "none"}); !known || !slices.Equal(got, []string{"isolated"}) {
			t.Errorf("NetworkUsers(none) = %v, %v; want isolated", got, known)
		}
		if got, known := u.NetworkUsers(Network{Name: "free"}); !known || len(got) != 0 {
			t.Errorf("NetworkUsers(free) = %v, %v; want nobody, known", got, known)
		}
	})
}

// ── Shapes that cannot be read ───────────────────────────────────────────────

// usagePair is a container nobody can read well ("web", altered by the case) beside one
// that is fine ("api"): the snapshot is read, its volumes are right, and the kind that
// was altered says "unknown" for every question, since "web" might be the user.
func usagePair(alter func(*usageCtr)) []usageCtr {
	web := usageCtr{id: usageID(1), name: "web", state: "running", image: usageImg,
		mounts:   []map[string]any{volumeMount("pgdata")},
		networks: json.RawMessage(`{"appnet":{"NetworkID":"` + usageNetID + `"}}`), networkMode: "appnet"}
	alter(&web)
	api := usageCtr{id: usageID(2), name: "api", state: "running", image: usageImg,
		mounts:   []map[string]any{volumeMount("cache")},
		networks: json.RawMessage(`{"appnet":{"NetworkID":"` + usageNetID + `"}}`), networkMode: "appnet"}
	return []usageCtr{web, api}
}

// The measurement's "À classer inconnu": a shape that is not the one probed is unknown
// for what it carries, never unused, and does not take the volumes down with it (a
// regression on `P`). Image and Networks are read apart from Mounts, which stays strict.
func TestAnUnreadableShapeIsUnknownAndLeavesTheOtherKindsKnown(t *testing.T) {
	str := func(s string) json.RawMessage { return json.RawMessage(s) }
	imageCases := []struct {
		name  string
		alter func(*usageCtr)
	}{
		{"Image is absent", func(c *usageCtr) { c.omit = []string{"Image"} }},
		{"Image is null", func(c *usageCtr) { c.image = nil }},
		{"Image is empty", func(c *usageCtr) { c.image = "" }},
		{"Image is only the prefix", func(c *usageCtr) { c.image = "sha256:" }},
		{"Image is a number", func(c *usageCtr) { c.image = 12345 }},
		{"Image is a list", func(c *usageCtr) { c.image = []string{usageImg} }},
	}
	networkCases := []struct {
		name  string
		alter func(*usageCtr)
	}{
		{"Networks is a string", func(c *usageCtr) { c.networks = str(`"oops"`) }},
		{"Networks is a list", func(c *usageCtr) { c.networks = str(`["appnet"]`) }},
		{"Networks is a number", func(c *usageCtr) { c.networks = str(`7`) }},
		{"a network entry is not an object", func(c *usageCtr) { c.networks = str(`{"appnet":"oops"}`) }},
		{"NetworkSettings is absent", func(c *usageCtr) { c.omit = []string{"NetworkSettings"} }},
		{"NetworkSettings is a string", func(c *usageCtr) { c.set = map[string]any{"NetworkSettings": "oops"} }},
		{"Networks is null and the mode is a bridge", func(c *usageCtr) { c.networks, c.networkMode = nil, "bridge" }},
		{"Networks is null and the mode is empty", func(c *usageCtr) { c.networks, c.networkMode = nil, "" }},
		{"Networks is null and HostConfig is absent", func(c *usageCtr) { c.networks, c.omit = nil, []string{"HostConfig"} }},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range imageCases {
			t.Run(tt.name, func(t *testing.T) {
				u, _, err := usageOf(t, shape, usagePair(tt.alter))

				if err != nil {
					t.Fatalf("ListContainerUsage() error = %v, want a usage whose images are unknown", err)
				}
				for _, id := range []string{usageImg[:12], usageOtherImg[:12]} {
					if got, known := u.ImageUsers(id); known {
						t.Errorf("ImageUsers(%.12s) = %v, known; want unknown (`web` may use it)", id, got)
					}
				}
				if got, _ := u.VolumeUsers("pgdata"); !slices.Equal(got, []string{"web"}) {
					t.Errorf("VolumeUsers(pgdata) = %v, want web: an unreadable Image must not take the volumes down", got)
				}
				if got, known := u.NetworkUsers(Network{Name: "appnet"}); !known || !slices.Equal(got, []string{"api", "web"}) {
					t.Errorf("NetworkUsers(appnet) = %v, %v; want api and web, known", got, known)
				}
			})
		}
		for _, tt := range networkCases {
			t.Run(tt.name, func(t *testing.T) {
				u, _, err := usageOf(t, shape, usagePair(tt.alter))

				if err != nil {
					t.Fatalf("ListContainerUsage() error = %v, want a usage whose networks are unknown", err)
				}
				for _, n := range []string{"appnet", "other"} {
					if got, known := u.NetworkUsers(Network{Name: n}); known {
						t.Errorf("NetworkUsers(%s) = %v, known; want unknown (`web` may be on it)", n, got)
					}
				}
				if got, _ := u.VolumeUsers("pgdata"); !slices.Equal(got, []string{"web"}) {
					t.Errorf("VolumeUsers(pgdata) = %v, want web: an unreadable Networks must not take the volumes down", got)
				}
				if got, known := u.ImageUsers(usageImg[:12]); !known || !slices.Equal(got, []string{"api", "web"}) {
					t.Errorf("ImageUsers = %v, %v; want api and web, known", got, known)
				}
			})
		}
	})
}

// A null Networks is a fact, not an unknown, in one case only: the container's mode is
// one that manages no network (rootless podman's pasta, or slirp4netns). It is on no
// network, so it never holds one.
func TestNullNetworksIsKnownOnlyForAModeThatManagesNone(t *testing.T) {
	for _, mode := range []string{"pasta", "slirp4netns"} {
		t.Run(mode, func(t *testing.T) {
			shape := engine.ShapeFor(engine.Podman)
			ctrs := usagePair(func(c *usageCtr) { c.networks, c.networkMode = nil, mode })

			u, _, err := usageOf(t, shape, ctrs)

			if err != nil {
				t.Fatalf("ListContainerUsage() error = %v", err)
			}
			if got, known := u.NetworkUsers(Network{Name: "appnet"}); !known || !slices.Equal(got, []string{"api"}) {
				t.Errorf("NetworkUsers(appnet) = %v, %v; want only api, known: `web` is on %s, not on appnet", got, known, mode)
			}
		})
	}
}

// Fail closed: "could not tell" is an error and a usage that claims nothing. Each case is
// a way the answer cannot be trusted; none is read as "nobody uses it". An ID that is not
// an ID is never handed to the engine.
func TestContainerUsageFailsClosed(t *testing.T) {
	two := []usageCtr{
		{id: usageID(1), name: "a", state: "running", image: usageImg, mounts: []map[string]any{volumeMount("v1")}, networks: json.RawMessage(`{}`)},
		{id: usageID(2), name: "b", state: "running", image: usageImg, mounts: []map[string]any{volumeMount("v2")}, networks: json.RawMessage(`{}`)},
	}
	cases := []struct {
		name      string
		setup     func(shape engine.Shape, e *usageEngine)
		noInspect bool // the inspect must not even be started
	}{
		{"ps fails", func(_ engine.Shape, e *usageEngine) {
			e.psErr = errors.New("exit status 1")
		}, true},
		{"ps prints an option instead of an ID", func(_ engine.Shape, e *usageEngine) { e.ps = "-f\n" }, true},
		{"ps prints a truncated ID", func(_ engine.Shape, e *usageEngine) { e.ps = usageID(1)[:12] + "x\n" }, true},
		{"a container disappears between the two calls", func(_ engine.Shape, e *usageEngine) {
			e.ps, e.inspect, e.inspectErr = usagePs(two), "", errors.New("exit status 1")
		}, false},
		{"inspect prints nothing for IDs that exist", func(_ engine.Shape, e *usageEngine) {
			e.ps, e.inspect = usagePs(two), ""
		}, false},
		{"inspect prints something that is not JSON", func(_ engine.Shape, e *usageEngine) {
			e.ps, e.inspect = usagePs(two), "a\tb\tv1,v2\n"
		}, false},
		{"inspect answers for fewer containers than asked (a missing line is never \"unused\")", func(shape engine.Shape, e *usageEngine) {
			e.ps, e.inspect = usagePs(two), usageInspect(shape, two[:1])
		}, false},
		{"inspect answers for a container nobody asked about", func(shape engine.Shape, e *usageEngine) {
			e.ps, e.inspect = usagePs(two[:1]), usageInspect(shape, two)
		}, false},
		{"inspect answers twice for one container", func(shape engine.Shape, e *usageEngine) {
			e.ps, e.inspect = usagePs(two[:1]), usageInspect(shape, two[:1])+usageInspect(shape, two[:1])
		}, false},
		{"an answer has no Id", func(shape engine.Shape, e *usageEngine) {
			e.ps, e.inspect = usagePs(two[:1]), `{"Name":"a","Image":"`+usageImg+`","Mounts":[]}`+"\n"
		}, false},
		{"Mounts is not a list", func(_ engine.Shape, e *usageEngine) {
			e.ps = usageID(1) + "\n"
			e.inspect = `{"Id":"` + usageID(1) + `","Name":"a","Image":"` + usageImg + `","Mounts":"oops"}` + "\n"
		}, false},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				e := &usageEngine{t: t}
				tt.setup(shape, e)
				e.install(t, shape.Name)

				u, err := ListContainerUsage()

				if err == nil {
					t.Error("ListContainerUsage() error = nil; want an error: a usage that was not understood must not read as \"unused\"")
				}
				if got, known := u.ImageUsers(usageImg[:12]); known {
					t.Errorf("the usage returned with the error knows its images (%v)", got)
				}
				if got, known := u.NetworkUsers(Network{Name: "appnet"}); known {
					t.Errorf("the usage returned with the error knows its networks (%v)", got)
				}
				if got, _ := u.VolumeUsers("v1"); len(got) != 0 {
					t.Errorf("the usage returned with the error holds volume users %v", got)
				}
				if tt.noInspect && e.inspectRuns != 0 {
					t.Errorf("an inspect was started after a ps that could not be trusted: %q", callsOf(&stubRunner{calls: e.calls}))
				}
				if e.inspectRuns > 1 {
					t.Errorf("%d inspects ran, want at most one grouped call", e.inspectRuns)
				}
			})
		}
	})
}

// An engine that is absent is the typed unavailability (OC-24), found with errors.As,
// and nothing is run.
func TestContainerUsageWithNoEngineIsUnavailableAndRunsNothing(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		s := stub(t, &stubRunner{missing: true})

		_, err := ListContainerUsage()

		var unavailable *UnavailableError
		if !errors.As(err, &unavailable) {
			t.Errorf("error = %v, want an *UnavailableError", err)
		}
		if len(s.calls) != 0 {
			t.Errorf("the engine was asked %q although its binary is absent", callsOf(s))
		}
	})
}

// ── The deadline ─────────────────────────────────────────────────────────────

type usageCtxKey struct{}

// Both calls carry a context, and the plain ListContainerUsage bounds it: a machine
// whose engine hangs must not freeze the refresh of `:oci` (the bound of ImagesInUse is
// 60 s).
func TestListContainerUsageHasADeadlineOfItsOwn(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ctrs := []usageCtr{{id: usageID(1), name: "web", state: "running", image: usageImg, networks: json.RawMessage(`{}`)}}

		_, e, err := usageOf(t, shape, ctrs)

		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}
		if len(e.calls) != 2 {
			t.Fatalf("%d calls, want 2", len(e.calls))
		}
		for _, c := range e.calls {
			if c.Ctx == nil {
				t.Fatalf("%q ran with no context: nothing would stop a hung engine", c.Args)
			}
			deadline, ok := c.Ctx.Deadline()
			if !ok || time.Until(deadline) > time.Minute {
				t.Errorf("%q ran with deadline %v (set %v), want one within 60 s", c.Args, deadline, ok)
			}
		}
	})
}

// The caller's context reaches the engine (it, or a child of it): that is how a view can
// cancel the read.
func TestListContainerUsageContextPassesTheCallersContext(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ctrs := []usageCtr{{id: usageID(1), name: "web", state: "running", image: usageImg, networks: json.RawMessage(`{}`)}}
		e := &usageEngine{t: t, ps: usagePs(ctrs), inspect: usageInspect(shape, ctrs)}
		e.install(t, shape.Name)
		ctx := context.WithValue(context.Background(), usageCtxKey{}, "mine")

		_, err := ListContainerUsageContext(ctx)

		if err != nil {
			t.Fatalf("ListContainerUsageContext() error = %v", err)
		}
		if len(e.calls) != 2 {
			t.Fatalf("%d calls, want 2", len(e.calls))
		}
		for _, c := range e.calls {
			if c.Ctx == nil || c.Ctx.Value(usageCtxKey{}) != "mine" {
				t.Errorf("%q ran without the context of the caller", c.Args)
			}
		}
	})
}

// A context that is over is an error and no usage, before the call it would have cut
// and between the two calls (read between them, as ImagesInUse does): never a half read
// that looks complete.
func TestAnExpiredContextFailsTheReadWithoutAnInspect(t *testing.T) {
	expired := func() (context.Context, context.CancelFunc) {
		return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	}
	cancelled := func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	cases := []struct {
		name   string
		ctx    func() (context.Context, context.CancelFunc)
		during bool // the context ends while `ps` runs, not before the read
		want   error
	}{
		{"expired before the read", expired, false, context.DeadlineExceeded},
		{"cancelled before the read", cancelled, false, context.Canceled},
		{"cancelled while ps ran", nil, true, context.Canceled},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				ctrs := []usageCtr{{id: usageID(1), name: "web", state: "running", image: usageImg, networks: json.RawMessage(`{}`)}}
				e := &usageEngine{t: t, ps: usagePs(ctrs), inspect: usageInspect(shape, ctrs)}
				e.install(t, shape.Name)
				var ctx context.Context
				if tt.during {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(context.Background())
					e.afterPs = cancel
					defer cancel()
				} else {
					var cancel context.CancelFunc
					ctx, cancel = tt.ctx()
					defer cancel()
				}

				u, err := ListContainerUsageContext(ctx)

				if !errors.Is(err, tt.want) {
					t.Errorf("error = %v, want it to wrap %v", err, tt.want)
				}
				if e.inspectRuns != 0 {
					t.Error("the inspect ran although the context was over")
				}
				if _, known := u.ImageUsers(usageImg[:12]); known {
					t.Error("the usage returned with the error knows its images")
				}
			})
		}
	})
}

// usageProbesCoverTheSixConfigurations keeps the table honest: the measurement covers
// docker 29.10.1 and 28.5.2 and podman 6.2.1 and 5.8.11, rootful and rootless for podman.
func TestUsageProbesCoverTheSixConfigurations(t *testing.T) {
	want := []string{"docker 29.10.1 rootful", "docker 28.5.2 rootful", "podman 6.2.1 rootful", "podman 6.2.1 rootless", "podman 5.8.11 rootful", "podman 5.8.11 rootless"}
	var got []string
	for _, p := range usageProbes {
		got = append(got, p.String())
	}
	if !slices.Equal(got, want) {
		t.Errorf("usageProbes = %v, want %v", got, want)
	}
}
