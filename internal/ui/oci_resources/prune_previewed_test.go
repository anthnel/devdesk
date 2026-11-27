package ociresources

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-22 (DEVDESK-820), requirements S2 and C2, on the Images and Volumes tabs: `P`
// removes the items the user was shown, each by its own identifier, and nothing
// else. The same contract as the containers view (see
// internal/ui/containers/prune_previewed_test.go, which also lists the footer
// wording these tests read).
//
// Today `P` confirms a generic question ("Remove all dangling (unused) images?",
// "Remove all unused volumes?") and runs `image prune -f` or `volume prune -f`.
// Expected:
//
//   - Images: `P` freezes the dangling images (repository and tag both `<none>`)
//     no container uses (Image.Containers == 0), from the table's list, and
//     confirming runs one `rmi <id>` per image. A tagged image is never on the
//     list, nor is a dangling one a container uses.
//   - Volumes: `P` first asks the engine which containers use which volumes, with
//     ONE `ps -a --no-trunc` then ONE grouped `inspect` (docker.ListVolumeUsers, never one per volume), and
//     freezes the volumes of the table that nobody uses, stopped containers
//     counted. Confirming runs one `volume rm <name>` per volume. When the read
//     fails, no confirmation opens and nothing is removed (fail closed, footer
//     level Error).
//   - Whatever changes after the preview (a refreshed list, a new unused item, a
//     container that starts and uses a volume) does not change the list. The
//     engine refuses what is in use now, nothing is forced, and the footer says
//     `<N> removed`, `<M> not removed` and why (`in use`).
//
// The engine is the test binary answering as docker or podman (fakecli, strict).
// The refusals of something in use are the recorded fixtures images/rmi-used and
// volumes/rm-used of the four recorded configurations.

const (
	imgAPI     = "a1a1a1a1a1a1" // api:v1, tagged
	imgCache   = "b2b2b2b2b2b2" // cache:v2, tagged, no container
	imgDangle1 = "d1d1d1d1d1d1" // <none>:<none>
	imgDangle2 = "d2d2d2d2d2d2" // <none>:<none>
	imgInUse   = "d3d3d3d3d3d3" // <none>:<none>, a container uses it
	imgLate    = "d4d4d4d4d4d4" // <none>:<none>, appears after the preview
)

func ociImages() []docker.Image {
	return []docker.Image{
		{ID: imgAPI, Repository: "api", Tag: "v1", Containers: 1},
		{ID: imgCache, Repository: "cache", Tag: "v2"},
		{ID: imgDangle1, Repository: "<none>", Tag: "<none>"},
		{ID: imgDangle2, Repository: "<none>", Tag: "<none>"},
		{ID: imgInUse, Repository: "<none>", Tag: "<none>", Containers: 1},
	}
}

func ociVolumes() []docker.Volume {
	return []docker.Volume{
		{Name: "pgdata", Driver: "local"},
		{Name: "redis", Driver: "local"},
		{Name: "scratch", Driver: "local"},
		{Name: "spare", Driver: "local"},
		{Name: "projectname_database_data", Driver: "local"},
		{Name: anonVolume, Driver: "local"},
	}
}

// ociCtr is a container as the fake engine holds it, in order of creation.
type ociCtr struct {
	id, name string
	mounts   []map[string]any
}

func ociID(n int) string { return fmt.Sprintf("%064x", n) }

func volMount(name string) map[string]any {
	return map[string]any{"Type": "volume", "Name": name, "Source": "/var/lib/containers/storage/volumes/" + name + "/_data", "Destination": "/" + name, "RW": true}
}

func bindMount(source, dest string) map[string]any {
	return map[string]any{"Type": "bind", "Source": source, "Destination": dest, "RW": true}
}

// ociContainers is the machine of the volume tests: a stopped container (db)
// holding pgdata and a bind mount, a running one (cache) holding redis, a stopped
// one (olddb) holding a volume whose name is longer than the 15 characters an
// untruncated `.Mounts` would cut, and a stopped one (scratchy) holding an
// anonymous volume (a 64-digit hex name). Mount order differs between the
// containers on purpose: nothing may depend on it.
func ociContainers() []ociCtr {
	return []ociCtr{
		{id: ociID(1), name: "db", mounts: []map[string]any{volMount("pgdata"), bindMount("/srv/bind", "/b")}},
		{id: ociID(2), name: "cache", mounts: []map[string]any{volMount("redis")}},
		{id: ociID(3), name: "olddb", mounts: []map[string]any{bindMount("/tmp", "/t"), volMount("projectname_database_data")}},
		{id: ociID(4), name: "scratchy", mounts: []map[string]any{volMount(anonVolume)}},
	}
}

// anonVolume is the name of an anonymous volume: a hash of 64 hex digits.
var anonVolume = strings.Repeat("ab", 32)

// fakeUsers is how the fake engine answers the two reads that say who uses which
// volume: ONE `ps -a --no-trunc` (the full IDs) and ONE grouped `inspect`
// (docker prints the containers in the REVERSE order of creation, with a Name that
// starts with "/"; podman in order, with no "/"). Both are reproduced here: the
// join is by ID, never by position.
//
// NOT RECORDED: written by hand from the probe of 2026-11 on the four recorded
// configurations; to be replaced by a recorded fixture.
type fakeUsers struct{ ps, inspect fakecli.Reply }

func usersOf(shape engine.Shape, ctrs []ociCtr) fakeUsers {
	var ps, in strings.Builder
	for i := len(ctrs) - 1; i >= 0; i-- {
		ps.WriteString(ctrs[i].id + "\n")
	}
	emit := func(c ociCtr) {
		name := c.name
		if shape.Name == engine.Docker {
			name = "/" + name
		}
		line, err := json.Marshal(map[string]any{"Id": c.id, "Name": name, "Mounts": c.mounts})
		if err != nil {
			panic(err)
		}
		in.Write(line)
		in.WriteString("\n")
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
	return fakeUsers{ps: fakecli.Reply{Stdout: ps.String()}, inspect: fakecli.Reply{Stdout: in.String()}}
}

func pinEngine(t *testing.T, name string) engine.Shape {
	t.Helper()
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(name))
	return engine.Current()
}

func eachEngine(t *testing.T, fn func(t *testing.T, shape engine.Shape)) {
	t.Helper()
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) { fn(t, pinEngine(t, name)) })
	}
}

// engineWith puts the fake engine on PATH: the listings the view may refresh
// with answer nothing, `ps` and `inspect` answer users, and everything else is declared by the
// test, so an argv it did not expect (`image prune -f`, `rmi -f ...`) fails.
func engineWith(t *testing.T, shape engine.Shape, users fakeUsers, exchanges ...fakecli.Exchange) *fakecli.Fake {
	t.Helper()
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)
	tool := shape.Binary
	scenario := fakecli.Scenario{
		{Tool: tool, Args: []string{"ps"}, Prefix: true, Reply: users.ps},
		{Tool: tool, Args: []string{"inspect"}, Prefix: true, Reply: users.inspect},
		{Tool: tool, Args: []string{"image", "ls"}, Prefix: true},
		{Tool: tool, Args: []string{"image", "inspect"}, Prefix: true},
		{Tool: tool, Args: []string{"system", "df"}, Prefix: true},
		{Tool: tool, Args: []string{"volume", "ls"}, Prefix: true},
		{Tool: tool, Args: []string{"network", "ls"}, Prefix: true},
	}
	for _, e := range exchanges {
		e.Tool = tool
		scenario = append(scenario, e)
	}
	return fakecli.Install(t, scenario, tool)
}

func exchange(reply fakecli.Reply, args ...string) fakecli.Exchange {
	return fakecli.Exchange{Args: args, Reply: reply}
}

// anyOf is an exchange for every argv that starts with args (any target).
func anyOf(reply fakecli.Reply, args ...string) fakecli.Exchange {
	return fakecli.Exchange{Args: args, Prefix: true, Reply: reply}
}

func isDestructive(args []string) bool {
	for i, w := range args {
		switch w {
		case "rm", "rmi", "stop", "kill", "prune":
			return true
		case "container", "image", "volume", "network", "system":
			if i+1 < len(args) && slices.Contains([]string{"rm", "prune", "remove", "kill", "stop"}, args[i+1]) {
				return true
			}
		}
	}
	return false
}

func destructive(fake *fakecli.Fake) [][]string {
	var out [][]string
	for _, inv := range fake.Invocations() {
		if isDestructive(inv.Args) {
			out = append(out, inv.Args)
		}
	}
	return out
}

// wantArgv fails unless the destructive calls are exactly the bare `<verb...>
// <target>` for each target, in any order: no prune, no option.
func wantArgv(t *testing.T, fake *fakecli.Fake, verb []string, targets ...string) {
	t.Helper()
	var got []string
	for _, argv := range destructive(fake) {
		if len(argv) != len(verb)+1 || !slices.Equal(argv[:len(verb)], verb) {
			t.Errorf("destructive argv %q, want exactly `%s <target>`: no prune, no -f, no other word", argv, strings.Join(verb, " "))
			continue
		}
		got = append(got, argv[len(verb)])
	}
	want := slices.Clone(targets)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("removed %v, want exactly the previewed %v", got, want)
	}
}

// refreshOrTimer: what a test does not hand back to Update (the view's own
// refresh would replace the list the test drives, the footer timer would clear the
// message the test reads).
func refreshOrTimer(msg tea.Msg) bool {
	name := fmt.Sprintf("%T", msg)
	return strings.HasSuffix(name, "ListMsg") || strings.HasSuffix(name, "ClearFooterMsg") ||
		strings.HasSuffix(name, "ScanCacheLoadedMsg") || strings.HasSuffix(name, "ImageUpdatesCheckedMsg")
}

// drive runs the command a key returned and hands its answers back to Update.
func drive(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		if !refreshOrTimer(msg) {
			m = feed(t, m, msg)
		}
	}
	return m
}

func previewP(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key(keymap.Prune))
	m = drive(t, m, cmd)
	if m.confirmModal == nil {
		t.Fatalf("P opened no confirmation; footer = %q", m.footer.Text())
	}
	return m
}

func confirmYes(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	return drive(t, m, cmd)
}

func imagesModel(t *testing.T, images ...docker.Image) Model {
	t.Helper()
	return feed(t, newTestModel(t), ImagesListMsg{Images: images})
}

func volumesModel(t *testing.T, volumes ...docker.Volume) Model {
	t.Helper()
	m := feed(t, newTestModel(t), VolumesListMsg{Volumes: volumes}, testutil.Key("tab"), testutil.Key("tab"))
	if m.activeTab != tabVolumes {
		t.Fatalf("active tab = %d, want the Volumes tab", m.activeTab)
	}
	return m
}

func footerIs(t *testing.T, m Model, level sharedcomponents.Level, contains ...string) {
	t.Helper()
	text := strings.ToLower(m.footer.Text())
	if m.footer.Level() != level {
		t.Errorf("footer level = %v (%q), want %v", m.footer.Level(), m.footer.Text(), level)
	}
	for _, want := range contains {
		if !strings.Contains(text, strings.ToLower(want)) {
			t.Errorf("footer = %q, want it to say %q", m.footer.Text(), want)
		}
	}
}

func recorded(t *testing.T, campaign, scenario string, tool enginefixture.Tool) map[string]fakecli.Reply {
	t.Helper()
	all, err := enginefixture.All()
	if err != nil {
		t.Fatalf("loading the recorded fixtures: %v", err)
	}
	out := map[string]fakecli.Reply{}
	for _, f := range all {
		if f.Campaign == campaign && f.Scenario == scenario && f.Meta.Tool == tool && !strings.HasPrefix(f.Meta.Record, "skip") {
			out[f.Name] = fakecli.Reply{Stderr: string(f.Exchange.Stderr), Exit: f.Exchange.Exit}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no recorded %s/%s fixture for %s", campaign, scenario, tool)
	}
	return out
}

// goneImage and goneVolume are what an engine says for something that does not
// exist.
//
// NOT RECORDED (see goneText in internal/docker/removal_report_test.go): known
// wordings, to be recorded in an OC-09 scenario.
func goneImage(shape engine.Shape, id string) fakecli.Reply {
	if shape.Name == engine.Podman {
		return fakecli.Reply{Stderr: "Error: " + id + ": image not known\n", Exit: 1}
	}
	return fakecli.Reply{Stderr: "Error response from daemon: No such image: " + id + "\n", Exit: 1}
}

func goneVolume(shape engine.Shape, name string) fakecli.Reply {
	if shape.Name == engine.Podman {
		return fakecli.Reply{Stderr: `Error: no volume with name "` + name + `" found: no such volume` + "\n", Exit: 1}
	}
	return fakecli.Reply{Stderr: "Error response from daemon: get " + name + ": no such volume\n", Exit: 1}
}

// ── Images ───────────────────────────────────────────────────────────────────

// Criterion 1, images: P removes the dangling images by id, bare, and nothing
// else: not a tagged image, not a dangling one a container uses, never a prune.
func TestPruneImagesRemovesEachDanglingImageByIDAndNeverPrunes(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{},
			exchange(fakecli.Reply{}, "rmi", imgDangle1), exchange(fakecli.Reply{}, "rmi", imgDangle2))
		m := imagesModel(t, ociImages()...)

		m = previewP(t, m)
		if calls := destructive(fake); len(calls) != 0 {
			t.Fatalf("the preview already ran %q: nothing may be removed before the confirmation", calls)
		}
		m = confirmYes(t, m)

		wantArgv(t, fake, []string{"rmi"}, imgDangle1, imgDangle2)
		footerIs(t, m, sharedcomponents.LevelInfo, "2 removed")
	})
}

func TestPruneImagesWithNothingDanglingOpensNoConfirmation(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{})
		m := imagesModel(t, docker.Image{ID: imgAPI, Repository: "api", Tag: "v1"})

		m, cmd := step(t, m, testutil.Key(keymap.Prune))
		m = drive(t, m, cmd)

		if m.confirmModal != nil {
			t.Error("P opened a confirmation with nothing to remove")
		}
		footerIs(t, m, sharedcomponents.LevelInfo)
		if !m.footer.IsSet() {
			t.Error("P with nothing to remove said nothing")
		}
		if calls := destructive(fake); len(calls) != 0 {
			t.Errorf("ran %q with nothing to remove", calls)
		}
	})
}

func TestAnImageThatBecomesDanglingAfterThePreviewIsNotRemoved(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{},
			exchange(fakecli.Reply{}, "rmi", imgDangle1), exchange(fakecli.Reply{}, "rmi", imgDangle2))
		m := previewP(t, imagesModel(t, ociImages()...))

		m = feed(t, m, ImagesListMsg{Images: append(ociImages(), docker.Image{ID: imgLate, Repository: "<none>", Tag: "<none>"})})
		confirmYes(t, m)

		wantArgv(t, fake, []string{"rmi"}, imgDangle1, imgDangle2)
		for _, argv := range destructive(fake) {
			if slices.Contains(argv, imgLate) {
				t.Errorf("the image that appeared after the preview was removed: %q", argv)
			}
		}
	})
}

// Criterion 3, for an image: one that a container started to use between the
// preview and the confirmation is refused by the engine (recorded wording of each
// configuration), not forced, the others are removed, and the footer says why.
func TestAnImageThatBecomesUsedAfterThePreviewIsRefusedNotForced(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		for name, refusal := range recorded(t, "images", "rmi-used", enginefixture.Tool(shape.Name)) {
			t.Run(name, func(t *testing.T) {
				fake := engineWith(t, shape, fakeUsers{},
					exchange(fakecli.Reply{}, "rmi", imgDangle1), exchange(refusal, "rmi", imgDangle2))
				m := previewP(t, imagesModel(t, ociImages()...))

				m = confirmYes(t, m)

				wantArgv(t, fake, []string{"rmi"}, imgDangle1, imgDangle2)
				footerIs(t, m, sharedcomponents.LevelWarning, "1 removed", "1 not removed", "in use")
			})
		}
	})
}

// Criterion 6, images.
func TestAnImageAlreadyGoneIsInformationNotAnError(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{},
			exchange(fakecli.Reply{}, "rmi", imgDangle1), exchange(goneImage(shape, imgDangle2), "rmi", imgDangle2))
		m := previewP(t, imagesModel(t, ociImages()...))

		m = confirmYes(t, m)

		wantArgv(t, fake, []string{"rmi"}, imgDangle1, imgDangle2)
		footerIs(t, m, sharedcomponents.LevelInfo, "1 removed", "1 already gone")
		if strings.Contains(strings.ToLower(m.footer.Text()), "not removed") {
			t.Errorf("footer = %q: an image that is already gone is not a failure", m.footer.Text())
		}
	})
}

func TestDeletingAnImageAlreadyGoneIsInformation(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{}, anyOf(goneImage(shape, "x"), "rmi"))
		m := imagesModel(t, ociImages()...)

		m, _ = step(t, m, testutil.Key(keymap.Delete))
		if m.confirmModal == nil {
			t.Fatal("D opened no confirmation")
		}
		m = confirmYes(t, m)

		calls := destructive(fake)
		if len(calls) != 1 || len(calls[0]) != 2 || calls[0][0] != "rmi" {
			t.Errorf("destructive calls = %q, want exactly one bare `rmi <id>`", calls)
		}
		footerIs(t, m, sharedcomponents.LevelInfo, "already gone")
	})
}

// ── Volumes ──────────────────────────────────────────────────────────────────

// Criteria 1 and 2, volumes: P reads who uses what with one `ps -a` and one grouped `inspect`, and removes
// by name the volumes nobody uses. pgdata is held by a stopped container, redis by
// a running one, and the long name by a stopped one: none is on the list.
func TestPruneVolumesRemovesOnlyTheUnusedOnesByNameAndNeverPrunes(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, usersOf(shape, ociContainers()),
			exchange(fakecli.Reply{}, "volume", "rm", "scratch"), exchange(fakecli.Reply{}, "volume", "rm", "spare"))
		m := volumesModel(t, ociVolumes()...)

		m = previewP(t, m)
		if calls := destructive(fake); len(calls) != 0 {
			t.Fatalf("the preview already ran %q: nothing may be removed before the confirmation", calls)
		}
		m = confirmYes(t, m)

		wantArgv(t, fake, []string{"volume", "rm"}, "scratch", "spare")
		footerIs(t, m, sharedcomponents.LevelInfo, "2 removed")

		// Criterion 2: the whole question cost one `ps -a` and one grouped
		// `inspect` of every container, and never a call per volume.
		var ps, inspect []fakecli.Invocation
		for _, inv := range fake.Invocations() {
			switch {
			case inv.Args[0] == "ps":
				ps = append(ps, inv)
			case inv.Args[0] == "inspect":
				inspect = append(inspect, inv)
			case slices.Contains(inv.Args, "inspect"):
				t.Errorf("a per-resource inspect was run (%q): the volume users come from one `ps -a` and one grouped `inspect`", inv.Args)
			}
		}
		if len(ps) != 1 || len(inspect) != 1 {
			t.Fatalf("%d `ps` and %d `inspect` calls, want exactly one of each for the whole preview", len(ps), len(inspect))
		}
		if !slices.Contains(ps[0].Args, "-a") && !slices.Contains(ps[0].Args, "--all") {
			t.Errorf("ps argv = %q, want `ps -a`: a stopped container still holds its volume", ps[0].Args)
		}
		if !slices.Contains(ps[0].Args, "--no-trunc") {
			t.Errorf("ps argv = %q, want --no-trunc: the IDs handed to inspect must be whole", ps[0].Args)
		}
		for _, c := range ociContainers() {
			if n := slices.Index(inspect[0].Args, c.id); n < 0 {
				t.Errorf("the inspect %q does not carry the container %s: it would miss its volumes", inspect[0].Args, c.name)
			}
		}
	})
}

// A volume every container uses is never eligible: with nothing unused, no
// confirmation, no removal.
func TestPruneVolumesWithEveryVolumeInUseOpensNoConfirmation(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, usersOf(shape, ociContainers()))
		m := volumesModel(t, docker.Volume{Name: "pgdata"}, docker.Volume{Name: "redis"})

		m, cmd := step(t, m, testutil.Key(keymap.Prune))
		m = drive(t, m, cmd)

		if m.confirmModal != nil {
			t.Error("P opened a confirmation although every volume is used")
		}
		footerIs(t, m, sharedcomponents.LevelInfo)
		if calls := destructive(fake); len(calls) != 0 {
			t.Errorf("ran %q although every volume is used", calls)
		}
	})
}

// Fail closed: when the engine cannot say who uses what, nothing is "unused". Two
// ways it can fail: the `ps -a`, and the grouped `inspect` (a container that
// disappeared between the two calls is "No such container" there).
func TestPruneVolumesRemovesNothingWhenTheUsersCannotBeRead(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		healthy := usersOf(shape, ociContainers())
		cases := []struct {
			name  string
			users fakeUsers
		}{
			{"ps fails", fakeUsers{ps: fakecli.Reply{Stderr: "Cannot connect to the Docker daemon\n", Exit: 1}}},
			{"inspect fails: a container is gone", fakeUsers{ps: healthy.ps,
				inspect: fakecli.Reply{Stderr: "Error response from daemon: No such container: " + ociID(2) + "\n", Exit: 1}}},
			{"inspect prints nothing for containers that exist", fakeUsers{ps: healthy.ps}},
			{"inspect prints something unreadable", fakeUsers{ps: healthy.ps, inspect: fakecli.Reply{Stdout: "not json\n"}}},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				fake := engineWith(t, shape, tt.users)
				m := volumesModel(t, ociVolumes()...)

				m, cmd := step(t, m, testutil.Key(keymap.Prune))
				m = drive(t, m, cmd)

				if m.confirmModal != nil {
					t.Error("P opened a confirmation without knowing which volumes are used")
				}
				footerIs(t, m, sharedcomponents.LevelError)
				if calls := destructive(fake); len(calls) != 0 {
					t.Errorf("ran %q without knowing which volumes are used", calls)
				}
			})
		}
	})
}

// Criterion 4, volumes.
func TestAVolumeThatBecomesUnusedAfterThePreviewIsNotRemoved(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, usersOf(shape, ociContainers()),
			exchange(fakecli.Reply{}, "volume", "rm", "scratch"), exchange(fakecli.Reply{}, "volume", "rm", "spare"))
		m := previewP(t, volumesModel(t, ociVolumes()...))

		m = feed(t, m, VolumesListMsg{Volumes: append(ociVolumes(), docker.Volume{Name: "late", Driver: "local"})})
		confirmYes(t, m)

		wantArgv(t, fake, []string{"volume", "rm"}, "scratch", "spare")
	})
}

// Criterion 3, for a volume: a container created between the preview and the
// confirmation mounts `scratch`; the engine refuses (recorded wording of each
// configuration), nothing is forced, `spare` is still removed, the footer says
// `in use`.
func TestAVolumeThatBecomesUsedAfterThePreviewIsRefusedNotForced(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		for name, refusal := range recorded(t, "volumes", "rm-used", enginefixture.Tool(shape.Name)) {
			t.Run(name, func(t *testing.T) {
				fake := engineWith(t, shape, usersOf(shape, ociContainers()),
					exchange(refusal, "volume", "rm", "scratch"), exchange(fakecli.Reply{}, "volume", "rm", "spare"))
				m := previewP(t, volumesModel(t, ociVolumes()...))

				m = confirmYes(t, m)

				wantArgv(t, fake, []string{"volume", "rm"}, "scratch", "spare")
				footerIs(t, m, sharedcomponents.LevelWarning, "1 removed", "1 not removed", "in use")
			})
		}
	})
}

// Criterion 6, volumes.
func TestAVolumeAlreadyGoneIsInformationNotAnError(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, usersOf(shape, ociContainers()),
			exchange(fakecli.Reply{}, "volume", "rm", "scratch"), exchange(goneVolume(shape, "spare"), "volume", "rm", "spare"))
		m := previewP(t, volumesModel(t, ociVolumes()...))

		m = confirmYes(t, m)

		wantArgv(t, fake, []string{"volume", "rm"}, "scratch", "spare")
		footerIs(t, m, sharedcomponents.LevelInfo, "1 removed", "1 already gone")
	})
}

func TestDeletingAVolumeAlreadyGoneIsInformation(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{}, anyOf(goneVolume(shape, "x"), "volume", "rm"))
		m := volumesModel(t, ociVolumes()...)

		m, _ = step(t, m, testutil.Key(keymap.Delete))
		if m.confirmModal == nil {
			t.Fatal("D opened no confirmation")
		}
		m = confirmYes(t, m)

		calls := destructive(fake)
		if len(calls) != 1 || len(calls[0]) != 3 || calls[0][0] != "volume" || calls[0][1] != "rm" {
			t.Errorf("destructive calls = %q, want exactly one bare `volume rm <name>`", calls)
		}
		footerIs(t, m, sharedcomponents.LevelInfo, "already gone")
	})
}

// Answering No, on either tab, destroys nothing.
func TestAnsweringNoToAPruneDestroysNothing(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, usersOf(shape, ociContainers()))
		for _, m := range []Model{imagesModel(t, ociImages()...), volumesModel(t, ociVolumes()...)} {
			m = previewP(t, m)
			m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})
			m = drive(t, m, cmd)
			if m.confirmModal != nil {
				t.Error("the confirmation stayed open after No")
			}
		}
		if calls := destructive(fake); len(calls) != 0 {
			t.Errorf("answering No ran %q", calls)
		}
	})
}
