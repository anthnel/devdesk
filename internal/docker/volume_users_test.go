package docker

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-22 (DEVDESK-820), criterion 2: "which containers use this volume", stopped
// ones included, never one engine call per volume. A volume a container
// references, even a stopped one, is never eligible for removal: its data is the
// container's. OC-25, OC-28 and OC-43 read the same map.
//
// What the code is expected to offer (none of it exists yet):
//
//	// Mount is one entry of a container's `Mounts` as `inspect` prints it. Only
//	// Type "volume" with a Name is a volume; a bind mount has an empty Name, and
//	// tmpfs and the others are not volumes either. Nothing is deduced from the
//	// position of a mount in the list: its order is not stable between versions.
//	type Mount struct{ Type, Name, Source, Destination string }
//
//	// ContainerMounts is what the inspect of one container says about its mounts.
//	type ContainerMounts struct {
//		ID, Name string // Name without the leading "/" docker prints
//		Mounts   []Mount
//	}
//
//	// VolumeUsers maps a volume name to the names of the containers that mount
//	// it, whatever their state: sorted, without duplicates, named and anonymous
//	// volumes alike (an anonymous one is a 64-digit hex name), never a bind mount.
//	func VolumeUsers(containers []ContainerMounts) map[string][]string
//
//	// ListVolumeUsers reads every container, stopped ones included, with:
//	//
//	//   1. ONE `<engine> ps -a --no-trunc` that lists the full IDs (`-q`, or
//	//      `--format {{.ID}}`: one ID per line);
//	//   2. when there is at least one ID, ONE grouped
//	//      `<engine> inspect --format <template> <id> <id> ...` (never one per
//	//      volume or per container; zero containers means no inspect at all).
//	//
//	// The template is the same on both engines and prints one JSON object per
//	// line, with these three keys and nothing tab-separated (docker prints a tab
//	// literally, see below):
//	//
//	//   {"Id":{{json .Id}},"Name":{{json .Name}},"Mounts":{{json .Mounts}}}
//	//
//	// The reader keeps only Id, Name and Mounts, so `{{json .}}` would read as well.
//	// The answers are joined to the IDs by Id, never by position (docker prints
//	// the containers in REVERSE order of creation, podman in order), and the
//	// leading "/" of docker's Name is dropped (podman has none).
//	func ListVolumeUsers() (map[string][]string, error)
//
//	// UnusedVolumes keeps, in order, the volumes nobody uses.
//	func UnusedVolumes(volumes []Volume, users map[string][]string) []Volume
//
// ListVolumeUsers fails closed: an error of `ps`, an ID that is not a container
// ID, an error of `inspect` (a "No such container" because one disappeared
// between the two calls included), an output that is empty or unreadable while
// IDs exist, one that does not answer for every ID asked, or a Mounts that is not
// a list, is an error and no map. A caller never mistakes "could not tell" for
// "unused".
//
// WHY NOT `ps --format {{.Mounts}}` (probe of 2026-11 on the four recorded
// configurations, reported by the orchestrator): podman prints the DESTINATIONS in
// brackets (`[/named]`, `[/hosttmp /data2 /named]`), never the volume name, and
// docker prints name or source or hash comma-separated with no type, cut at 14
// characters without --no-trunc. Both would make a volume look unused.
//
// NOT RECORDED: the bytes of the exchanges below are written by hand from that
// probe (docker `Name` prefixed with "/", reverse order of creation; podman in
// order, no "/"; `Name` of a volume mount is the volume's name, a hash of 64 hex
// for an anonymous one, empty for a bind). They are to be replaced by a recorded
// fixture (an OC-09 scenario) and marked as such.

func TestVolumeUsers(t *testing.T) {
	anon := strings.Repeat("ab", 32)
	vol := func(name string) Mount { return Mount{Type: "volume", Name: name, Destination: "/" + name} }
	bind := Mount{Type: "bind", Source: "/tmp", Destination: "/hosttmp"}
	tests := []struct {
		name       string
		containers []ContainerMounts
		want       map[string][]string
	}{
		{"a stopped container still uses its volume",
			[]ContainerMounts{{ID: "1", Name: "db", Mounts: []Mount{vol("pgdata")}}},
			map[string][]string{"pgdata": {"db"}}},
		{"running and stopped users of one volume are all named, sorted",
			[]ContainerMounts{
				{Name: "web", Mounts: []Mount{vol("data")}},
				{Name: "old", Mounts: []Mount{vol("data")}},
			},
			map[string][]string{"data": {"old", "web"}}},
		{"a bind mount is not a volume",
			[]ContainerMounts{{Name: "web", Mounts: []Mount{bind}}},
			map[string][]string{}},
		{"a bind mount whose source looks like a volume name is not one",
			[]ContainerMounts{{Name: "web", Mounts: []Mount{{Type: "bind", Source: "pgdata", Destination: "/d"}}}},
			map[string][]string{}},
		{"tmpfs and other types are not volumes",
			[]ContainerMounts{{Name: "web", Mounts: []Mount{{Type: "tmpfs", Destination: "/run"}, {Type: "npipe", Name: "x"}}}},
			map[string][]string{}},
		{"an anonymous volume is a 64-digit hex name and counts",
			[]ContainerMounts{{Name: "web", Mounts: []Mount{{Type: "volume", Name: anon, Destination: "/data"}}}},
			map[string][]string{anon: {"web"}}},
		{"one container with a named volume, a bind mount and an anonymous volume",
			[]ContainerMounts{{Name: "all", Mounts: []Mount{bind, vol("named"), {Type: "volume", Name: anon}}}},
			map[string][]string{"named": {"all"}, anon: {"all"}}},
		{"the order of the mounts decides nothing",
			[]ContainerMounts{
				{Name: "a", Mounts: []Mount{vol("one"), bind, vol("two")}},
				{Name: "b", Mounts: []Mount{vol("two"), vol("one"), bind}},
			},
			map[string][]string{"one": {"a", "b"}, "two": {"a", "b"}}},
		{"the same volume mounted twice by one container counts once",
			[]ContainerMounts{{Name: "web", Mounts: []Mount{vol("data"), vol("data")}}},
			map[string][]string{"data": {"web"}}},
		{"a volume mount with no name is ignored",
			[]ContainerMounts{{Name: "web", Mounts: []Mount{{Type: "volume"}}}},
			map[string][]string{}},
		{"a container with no mount uses none", []ContainerMounts{{Name: "web"}}, map[string][]string{}},
		{"no container", nil, map[string][]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VolumeUsers(tt.containers)

			if len(got) != len(tt.want) {
				t.Fatalf("VolumeUsers = %v, want %v", got, tt.want)
			}
			for volume, users := range tt.want {
				if !slices.Equal(got[volume], users) {
					t.Errorf("users of %q = %v, want %v", volume, got[volume], users)
				}
			}
		})
	}
}

func TestUnusedVolumesNeverKeepsAVolumeSomeoneReferences(t *testing.T) {
	anon := strings.Repeat("ab", 32)
	volumes := []Volume{{Name: "pgdata"}, {Name: "redis"}, {Name: "scratch"}, {Name: "spare"}, {Name: anon}}

	tests := []struct {
		name  string
		users map[string][]string
		want  []string
	}{
		{"a volume of a stopped container is kept out, anonymous ones included",
			map[string][]string{"pgdata": {"db-stopped"}, "redis": {"cache"}, anon: {"old"}}, []string{"scratch", "spare"}},
		{"nobody uses anything", map[string][]string{}, []string{"pgdata", "redis", "scratch", "spare", anon}},
		{"nil users", nil, []string{"pgdata", "redis", "scratch", "spare", anon}},
		{"everything is used", map[string][]string{"pgdata": {"a"}, "redis": {"b"}, "scratch": {"c"}, "spare": {"d"}, anon: {"e"}}, nil},
		{"a user of a volume that does not exist changes nothing", map[string][]string{"ghost": {"a"}}, []string{"pgdata", "redis", "scratch", "spare", anon}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, v := range UnusedVolumes(volumes, tt.users) {
				got = append(got, v.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("UnusedVolumes = %v, want %v (the order of the list is kept)", got, tt.want)
			}
		})
	}
}

// ── The engine side ──────────────────────────────────────────────────────────

// probeCtr is a container as the fake engine holds it, in order of creation.
type probeCtr struct {
	id, name string
	mounts   []map[string]any
}

func volumeMount(name string) map[string]any {
	return map[string]any{"Type": "volume", "Name": name, "Source": "/var/lib/containers/storage/volumes/" + name + "/_data", "Destination": "/" + name, "RW": true}
}

func bindMount(source, dest string) map[string]any {
	return map[string]any{"Type": "bind", "Source": source, "Destination": dest, "RW": true}
}

func probeID(n int) string { return fmt.Sprintf("%064x", n) }

// psOutput is what `ps -a -q --no-trunc` prints: the full IDs, one per line
// (newest first on both engines, which nothing may rely on).
func psOutput(ctrs []probeCtr) string {
	var b strings.Builder
	for i := len(ctrs) - 1; i >= 0; i-- {
		b.WriteString(ctrs[i].id + "\n")
	}
	return b.String()
}

// inspectOutput is what the grouped inspect prints with the template of the
// package: one JSON object per line. Docker prints the containers in the REVERSE
// order of creation and its Name starts with "/"; podman prints them in order and
// has no "/".
func inspectOutput(shape engine.Shape, ctrs []probeCtr) string {
	var b strings.Builder
	emit := func(c probeCtr) {
		name := c.name
		if shape.Name == engine.Docker {
			name = "/" + name
		}
		line, err := json.Marshal(map[string]any{"Id": c.id, "Name": name, "Mounts": c.mounts})
		if err != nil {
			panic(err)
		}
		b.Write(line)
		b.WriteString("\n")
	}
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

// listOnFake runs ListVolumeUsers against a fake engine that answers the `ps`
// and the `inspect` with the given replies, and returns what the engine was asked.
func listOnFake(t *testing.T, shape engine.Shape, ps, inspect fakecli.Reply) (map[string][]string, []fakecli.Invocation, error) {
	t.Helper()
	fake := fakecli.Install(t, fakecli.Scenario{
		{Tool: shape.Binary, Args: []string{"ps"}, Prefix: true, Reply: ps},
		{Tool: shape.Binary, Args: []string{"inspect"}, Prefix: true, Reply: inspect},
	}, shape.Binary)
	useRealRunner(t)

	users, err := ListVolumeUsers()
	return users, fake.Invocations(), err
}

func callsTo(invocations []fakecli.Invocation, verb string) []fakecli.Invocation {
	var out []fakecli.Invocation
	for _, inv := range invocations {
		if len(inv.Args) > 0 && inv.Args[0] == verb {
			out = append(out, inv)
		}
	}
	return out
}

// wantTwoCalls checks the budget: one `ps -a` that lists the full IDs, then one
// grouped `inspect` that carries every ID once, and nothing else.
func wantTwoCalls(t *testing.T, invocations []fakecli.Invocation, ids []string) {
	t.Helper()
	if len(invocations) != 2 {
		t.Fatalf("the engine was started %d times, want one `ps -a` and one grouped `inspect`: %+v", len(invocations), invocations)
	}
	ps, inspect := invocations[0].Args, invocations[1].Args
	if ps[0] != "ps" || (!slices.Contains(ps, "-a") && !slices.Contains(ps, "--all")) {
		t.Errorf("first call = %q, want a `ps -a`: stopped containers hold volumes too", ps)
	}
	if !slices.Contains(ps, "--no-trunc") {
		t.Errorf("ps argv = %q, want --no-trunc: the IDs handed to inspect must be whole", ps)
	}
	if !slices.Contains(ps, "-q") && !slices.Contains(ps, "--quiet") && !slices.Contains(ps, "--format") {
		t.Errorf("ps argv = %q, want -q or a --format that prints the ID alone", ps)
	}
	if inspect[0] != "inspect" || !slices.Contains(inspect, "--format") {
		t.Fatalf("second call = %q, want `inspect --format <template> <ids...>`", inspect)
	}
	for _, id := range ids {
		if n := countElement(inspect, id); n != 1 {
			t.Errorf("the inspect carries %s %d time(s), want once: %q", id, n, inspect)
		}
	}
	if !sameSet(inspect[len(inspect)-len(ids):], ids) {
		t.Errorf("the inspect does not end with the IDs asked for: %q", inspect)
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// The cases of the probe: (a) a named volume, (b) a bind mount, (c) an anonymous
// volume, (d) one container with all three, stopped containers included. The mount
// order of (d) differs between the two engines on purpose.
func probeContainers(shape engine.Shape) []probeCtr {
	anonC, anonD := strings.Repeat("c1", 32), strings.Repeat("d4", 32)
	dMounts := []map[string]any{volumeMount("ddprobe-named"), bindMount("/tmp", "/hosttmp"), volumeMount(anonD)}
	if shape.Name == engine.Podman {
		dMounts = []map[string]any{bindMount("/tmp", "/hosttmp"), volumeMount(anonD), volumeMount("ddprobe-named")}
	}
	return []probeCtr{
		{id: probeID(1), name: "ddprobe-a", mounts: []map[string]any{volumeMount("ddprobe-named")}},
		{id: probeID(2), name: "ddprobe-b", mounts: []map[string]any{bindMount("/tmp", "/hosttmp")}},
		{id: probeID(3), name: "ddprobe-c", mounts: []map[string]any{volumeMount(anonC)}},
		{id: probeID(4), name: "ddprobe-d", mounts: dMounts},
	}
}

// Criterion 2 on both engines: ONE `ps -a`, ONE grouped `inspect`, the answers
// joined by ID whatever their order, the leading "/" dropped, a bind mount never a
// volume, an anonymous volume of a stopped container used.
func TestListVolumeUsersReadsPsThenOneGroupedInspect(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ctrs := probeContainers(shape)
		users, invocations, err := listOnFake(t, shape,
			fakecli.Reply{Stdout: psOutput(ctrs)}, fakecli.Reply{Stdout: inspectOutput(shape, ctrs)})

		if err != nil {
			t.Fatalf("ListVolumeUsers() error = %v", err)
		}
		wantTwoCalls(t, invocations, []string{probeID(1), probeID(2), probeID(3), probeID(4)})
		want := map[string][]string{
			"ddprobe-named":          {"ddprobe-a", "ddprobe-d"},
			strings.Repeat("c1", 32): {"ddprobe-c"},
			strings.Repeat("d4", 32): {"ddprobe-d"},
		}
		if len(users) != len(want) {
			t.Fatalf("ListVolumeUsers() = %v, want %v (the bind mount of /tmp is not a volume)", users, want)
		}
		for volume, names := range want {
			if !slices.Equal(users[volume], names) {
				t.Errorf("users of %q = %v, want %v", volume, users[volume], names)
			}
		}
		// The whole pipeline of the story: the anonymous volume of a stopped
		// container is not eligible, whatever its age.
		var unused []string
		list := []Volume{{Name: "ddprobe-named"}, {Name: strings.Repeat("c1", 32)}, {Name: "scratch"}}
		for _, v := range UnusedVolumes(list, users) {
			unused = append(unused, v.Name)
		}
		if !slices.Equal(unused, []string{"scratch"}) {
			t.Errorf("unused volumes = %v, want only scratch", unused)
		}
	})
}

// The cost does not grow with the machine: 40 containers, one `ps`, one `inspect`
// (carrying 40 IDs), nothing per volume.
func TestListVolumeUsersCostsTwoCallsWhateverTheSize(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		var ctrs []probeCtr
		var ids []string
		for i := 1; i <= 40; i++ {
			ctrs = append(ctrs, probeCtr{id: probeID(i), name: fmt.Sprintf("c%d", i),
				mounts: []map[string]any{volumeMount(fmt.Sprintf("vol%d", i)), volumeMount("shared")}})
			ids = append(ids, probeID(i))
		}

		users, invocations, err := listOnFake(t, shape,
			fakecli.Reply{Stdout: psOutput(ctrs)}, fakecli.Reply{Stdout: inspectOutput(shape, ctrs)})

		if err != nil {
			t.Fatalf("ListVolumeUsers() error = %v", err)
		}
		wantTwoCalls(t, invocations, ids)
		if len(users["shared"]) != 40 || len(users) != 41 {
			t.Errorf("shared has %d users and there are %d volumes, want 40 and 41", len(users["shared"]), len(users))
		}
	})
}

// No container, nothing to inspect: one call, an empty map, no error.
func TestListVolumeUsersWithNoContainerDoesNotInspect(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		users, invocations, err := listOnFake(t, shape, fakecli.Reply{}, fakecli.Reply{Stdout: "unexpected\n"})

		if err != nil {
			t.Fatalf("ListVolumeUsers() error = %v", err)
		}
		if len(users) != 0 {
			t.Errorf("users = %v, want none", users)
		}
		if len(invocations) != 1 || invocations[0].Args[0] != "ps" {
			t.Errorf("invocations = %+v, want the one `ps -a` and no inspect when there is no container", invocations)
		}
	})
}

// A template the reader does not own: the inspect may print more than the three
// keys (`{{json .}}`), and the reader keeps the three it needs.
func TestListVolumeUsersReadsOnlyIdNameAndMounts(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		name := "web"
		if shape.Name == engine.Docker {
			name = "/web"
		}
		line := fmt.Sprintf(`{"Id":%q,"Name":%q,"State":{"Status":"exited","ExitCode":0},"Config":{"Env":["A=b"],"Labels":{"x":"y"}},`+
			`"Mounts":[{"Type":"volume","Name":"pgdata","Source":"/v/pgdata/_data","Destination":"/d","Driver":"local","Mode":"z","RW":true,"Propagation":""}],`+
			`"HostConfig":{"Binds":null}}`+"\n", probeID(1), name)

		users, _, err := listOnFake(t, shape, fakecli.Reply{Stdout: probeID(1) + "\n"}, fakecli.Reply{Stdout: line})

		if err != nil {
			t.Fatalf("ListVolumeUsers() error = %v", err)
		}
		if !slices.Equal(users["pgdata"], []string{"web"}) {
			t.Errorf("users = %v, want pgdata used by web", users)
		}
	})
}

// Fail closed: "could not tell" is never "nobody uses it". Each case is a way the
// answer cannot be trusted; none gives a map, and an ID that is not an ID is not
// handed to the engine.
func TestListVolumeUsersFailsClosed(t *testing.T) {
	two := []probeCtr{
		{id: probeID(1), name: "a", mounts: []map[string]any{volumeMount("v1")}},
		{id: probeID(2), name: "b", mounts: []map[string]any{volumeMount("v2")}},
	}
	cases := []struct {
		name      string
		ps        func(engine.Shape) fakecli.Reply
		inspect   func(engine.Shape) fakecli.Reply
		noInspect bool // the inspect must not even be started
	}{
		{"ps fails",
			func(engine.Shape) fakecli.Reply {
				return fakecli.Reply{Stderr: "Cannot connect to the Docker daemon\n", Exit: 1}
			},
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{} }, true},
		{"ps prints an option instead of an ID",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: "-f\n"} },
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{} }, true},
		{"ps prints a truncated ID",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: probeID(1)[:12] + "x\n"} },
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{} }, true},
		{"a container disappears between the two calls",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: psOutput(two)} },
			func(engine.Shape) fakecli.Reply {
				return fakecli.Reply{Stderr: "Error response from daemon: No such container: " + probeID(2) + "\n", Exit: 1}
			}, false},
		{"inspect prints nothing for IDs that exist",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: psOutput(two)} },
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{} }, false},
		{"inspect prints something that is not JSON",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: psOutput(two)} },
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: "a\tb\tv1,v2\n"} }, false},
		{"inspect answers for fewer containers than asked",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: psOutput(two)} },
			func(shape engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: inspectOutput(shape, two[:1])} }, false},
		{"inspect answers for a container nobody asked about",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: psOutput(two[:1])} },
			func(shape engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: inspectOutput(shape, two)} }, false},
		{"Mounts is not a list",
			func(engine.Shape) fakecli.Reply { return fakecli.Reply{Stdout: probeID(1) + "\n"} },
			func(engine.Shape) fakecli.Reply {
				return fakecli.Reply{Stdout: `{"Id":"` + probeID(1) + `","Name":"a","Mounts":"oops"}` + "\n"}
			}, false},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				users, invocations, err := listOnFake(t, shape, tt.ps(shape), tt.inspect(shape))

				if err == nil {
					t.Errorf("ListVolumeUsers() = %v, nil; want an error (a volume must not read as unused when the answer was not understood)", users)
				}
				if len(users) != 0 {
					t.Errorf("ListVolumeUsers() returned %v alongside the error, want no map", users)
				}
				if tt.noInspect && len(callsTo(invocations, "inspect")) != 0 {
					t.Errorf("an inspect was started after a ps that could not be trusted: %+v", invocations)
				}
				if len(callsTo(invocations, "inspect")) > 1 || len(callsTo(invocations, "volume")) != 0 {
					t.Errorf("more than one grouped inspect, or a per-volume call: %+v", invocations)
				}
			})
		}
	})
}
