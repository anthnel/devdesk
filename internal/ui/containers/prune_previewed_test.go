package containers

import (
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

// OC-22 (DEVDESK-820), requirements S2 and C2, on the containers view: `P` removes
// the stopped containers the user was shown, each by its own identifier, and
// nothing else.
//
// What the code does today (and these tests fail on): `P` opens a generic
// confirmation ("Remove all stopped containers?") and confirming runs
// `<engine> container prune -f`, so the list removed is whatever the engine finds
// when the command starts. The expected behavior:
//
//   - `P` freezes, at that moment, the containers that are stopped (state exited,
//     created or dead; not running, paused, restarting or removing) among ALL the
//     containers of the engine, whatever state filter the table shows, as it did
//     when it pruned. With none, no confirmation opens and the footer says so.
//   - Confirming removes exactly that frozen list: one `rm <id>` per container,
//     nothing else, never `-f`, never a prune. A refresh of the table between the
//     preview and the confirmation changes nothing about the list.
//   - The outcome is a structured docker.RemovalReport (the message that carries
//     it is the view's own; what these tests read is the engine journal and the
//     footer), and the footer says it: `<N> removed`, then, when some were not,
//     `<M> not removed` and why, then `<K> already gone`. All removed or gone is
//     level Info; a refusal the engine explains (running, in use) is a Warning; an
//     unexplained failure is an Error (Rule 128).
//
// The modal that shows the list (OC-27) and the detailed outcome (OC-28) are not
// part of this story: nothing here reads the text of the modal.
//
// The engine is the test binary answering as docker or podman (fakecli, strict:
// an argv no exchange declares fails the test). The refusals of a running
// container are the recorded fixtures containers/rm-running of the four recorded
// configurations. No test here calls t.Parallel (the seams are global).

const (
	idAPI    = "bbbb111122223333" // exited
	idZombie = "dddd111122223333" // dead
	idFresh  = "eeee111122223333" // created: the third stopped container
	idLate   = "ffff111122223333" // exited, appears after the preview
)

// stoppedFixtures is containerFixtures() (web running, api exited, cache paused,
// zombie dead) and one created container: three stopped containers.
func stoppedFixtures() []docker.Container {
	fresh := docker.Container{ID: idFresh, Name: "fresh", Image: "busybox", State: "created"}
	return append(containerFixtures(), fresh)
}

// runEngine pins the engine for a test and puts the previous one back.
func runEngine(t *testing.T, name string) engine.Shape {
	t.Helper()
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(name))
	return engine.Current()
}

// forEachEngine runs fn once per declared engine, with the engine pinned.
func forEachEngine(t *testing.T, fn func(t *testing.T, shape engine.Shape)) {
	t.Helper()
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) { fn(t, runEngine(t, name)) })
	}
}

// installEngine puts the fake engine on PATH. Listings answer with nothing (a
// refresh the view may issue); everything else is what the test declares, so an
// argv the test did not expect (`container prune -f`, `rm -f ...`) fails the test.
func installEngine(t *testing.T, shape engine.Shape, exchanges ...fakecli.Exchange) *fakecli.Fake {
	t.Helper()
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)
	scenario := fakecli.Scenario{{Tool: shape.Binary, Args: []string{"ps"}, Prefix: true}}
	for _, e := range exchanges {
		e.Tool = shape.Binary
		scenario = append(scenario, e)
	}
	return fakecli.Install(t, scenario, shape.Binary)
}

// rmOK answers `rm <id>` with success for each id.
func rmOK(ids ...string) []fakecli.Exchange {
	var out []fakecli.Exchange
	for _, id := range ids {
		out = append(out, fakecli.Exchange{Args: []string{"rm", id}})
	}
	return out
}

// rmRefused answers `rm <id>` with a refusal.
func rmRefused(id string, reply fakecli.Reply) fakecli.Exchange {
	return fakecli.Exchange{Args: []string{"rm", id}, Reply: reply}
}

// destructiveCalls are the invocations that destroy something, as argv.
func destructiveCalls(fake *fakecli.Fake) [][]string {
	var out [][]string
	for _, inv := range fake.Invocations() {
		if isDestructiveArgv(inv.Args) {
			out = append(out, inv.Args)
		}
	}
	return out
}

func isDestructiveArgv(args []string) bool {
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

// wantRemoved fails unless the destructive calls are exactly one bare `rm <id>`
// per id, in any order (the order of the table is the table's business).
func wantRemoved(t *testing.T, fake *fakecli.Fake, ids ...string) {
	t.Helper()
	got := destructiveCalls(fake)
	var gotIDs []string
	for _, argv := range got {
		if len(argv) != 2 || argv[0] != "rm" {
			t.Errorf("destructive argv %q, want exactly `rm <id>`: no prune, no -f, no other word", argv)
			continue
		}
		gotIDs = append(gotIDs, argv[1])
	}
	want := slices.Clone(ids)
	slices.Sort(want)
	slices.Sort(gotIDs)
	if !slices.Equal(gotIDs, want) {
		t.Errorf("removed %v, want exactly the previewed %v", gotIDs, want)
	}
}

// refreshOrTimer reports the messages a test does not hand back to Update: the
// view's own refresh (it would replace the list the test is driving) and the
// footer's timer.
func refreshOrTimer(msg tea.Msg) bool {
	switch msg.(type) {
	case ContainersListMsg, ContainerMetricsMsg, ContainerUpdatesCheckedMsg, sharedcomponents.ClearFooterMsg:
		return true
	}
	return false
}

// settle runs the command a key returned and hands its answers back to Update,
// which is what the runtime does: a preview that needs the engine ends here.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		if !refreshOrTimer(msg) {
			m = feed(t, m, msg)
		}
	}
	return m
}

// preview presses P and returns the model with the confirmation open.
func preview(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key(keymap.Prune))
	m = settle(t, m, cmd)
	if m.confirmModal == nil {
		t.Fatalf("P opened no confirmation; footer = %q", m.footer.Text())
	}
	return m
}

// confirm answers Yes and lets the removal run to its end.
func confirm(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	return settle(t, m, cmd)
}

// recordedRefusals returns the recorded answer of a refusal on every recorded
// configuration of a tool (docker 28.5.2 and 29.10.1, podman 5.8.11 and 6.2.1).
func recordedRefusals(t *testing.T, campaign, scenario string, tool enginefixture.Tool) map[string]fakecli.Reply {
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

// goneReply is what an engine says for a container that does not exist.
//
// NOT RECORDED (see goneText in internal/docker/removal_report_test.go): the
// wordings are the engines' known ones, to be recorded in an OC-09 scenario.
func goneReply(shape engine.Shape, id string) fakecli.Reply {
	if shape.Name == engine.Podman {
		return fakecli.Reply{Stderr: `Error: no container with name or ID "` + id + `" found: no such container` + "\n", Exit: 1}
	}
	return fakecli.Reply{Stderr: "Error response from daemon: No such container: " + id + "\n", Exit: 1}
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

// ── Criterion 1 ──────────────────────────────────────────────────────────────

// P with three stopped containers: after the confirmation the engine was asked to
// remove each of the three by its identifier, and nothing else. The state filter
// of the table (running only by default, or every state) does not change it.
func TestPruneRemovesEachStoppedContainerByIDAndNeverPrunes(t *testing.T) {
	for _, filter := range []string{"every state", "the default filter (running only)"} {
		t.Run(filter, func(t *testing.T) {
			forEachEngine(t, func(t *testing.T, shape engine.Shape) {
				fake := installEngine(t, shape, rmOK(idAPI, idZombie, idFresh)...)
				m := feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()})
				if filter == "every state" {
					m = allStates(t, m)
				}

				m = preview(t, m)
				if calls := destructiveCalls(fake); len(calls) != 0 {
					t.Fatalf("the preview already ran %q: nothing may be removed before the confirmation", calls)
				}
				m = confirm(t, m)

				wantRemoved(t, fake, idAPI, idZombie, idFresh)
				footerIs(t, m, sharedcomponents.LevelInfo, "3 removed")
			})
		})
	}
}

// What is not stopped is not on the list: a running, a paused, a restarting or a
// removing container is never an argv.
func TestPruneLeavesEveryOtherStateAlone(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := installEngine(t, shape, rmOK(idAPI)...)
		list := []docker.Container{
			{ID: webID, Name: "web", State: "running"},
			{ID: "cccc111122223333", Name: "cache", State: "paused"},
			{ID: "1212121212121212", Name: "loop", State: "restarting"},
			{ID: "3434343434343434", Name: "going", State: "removing"},
			{ID: idAPI, Name: "api", State: "exited"},
		}
		m := feed(t, newTestModel(t), ContainersListMsg{Containers: list})

		m = confirm(t, preview(t, m))

		wantRemoved(t, fake, idAPI)
		_ = m
	})
}

// Nothing stopped: no confirmation to answer, nothing to run, and the footer says
// there was nothing to remove (it is information, not an error).
func TestPruneWithNothingStoppedOpensNoConfirmation(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := installEngine(t, shape)
		list := []docker.Container{{ID: webID, Name: "web", State: "running"}}
		m := feed(t, newTestModel(t), ContainersListMsg{Containers: list})

		m, cmd := step(t, m, testutil.Key(keymap.Prune))
		m = settle(t, m, cmd)

		if m.confirmModal != nil {
			t.Error("P opened a confirmation with nothing to remove")
		}
		if !m.footer.IsSet() {
			t.Error("P with nothing to remove said nothing")
		}
		footerIs(t, m, sharedcomponents.LevelInfo)
		if calls := destructiveCalls(fake); len(calls) != 0 {
			t.Errorf("ran %q with nothing to remove", calls)
		}
	})
}

// Answering No, or opening the preview alone, destroys nothing.
func TestPruneDestroysNothingUntilConfirmed(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := installEngine(t, shape)
		m := preview(t, feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()}))

		m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})
		m = settle(t, m, cmd)

		if m.confirmModal != nil {
			t.Error("the confirmation stayed open after No")
		}
		if calls := destructiveCalls(fake); len(calls) != 0 {
			t.Errorf("answering No ran %q", calls)
		}
	})
}

// ── Criterion 4 ──────────────────────────────────────────────────────────────

// A container that becomes eligible after the preview is not on the list: what is
// removed is what was shown, frozen when the confirmation opened.
func TestAContainerThatStopsAfterThePreviewIsNotRemoved(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		// No exchange for idLate: a `rm` of it fails the test with the argv.
		fake := installEngine(t, shape, rmOK(idAPI, idZombie, idFresh)...)
		m := preview(t, feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()}))

		late := docker.Container{ID: idLate, Name: "late", Image: "busybox", State: "exited"}
		m = feed(t, m, ContainersListMsg{Containers: append(stoppedFixtures(), late)})
		confirm(t, m)

		wantRemoved(t, fake, idAPI, idZombie, idFresh)
		for _, argv := range destructiveCalls(fake) {
			if slices.Contains(argv, idLate) {
				t.Errorf("the container that appeared after the preview was removed: %q", argv)
			}
		}
	})
}

// ── Criterion 3 ──────────────────────────────────────────────────────────────

// A container that starts between the preview and the confirmation: the engine is
// asked, refuses it (running), nothing is forced, the others are removed anyway,
// and the footer says one was not removed and why. The refusal is the recorded one
// of each engine and each recorded version.
func TestAContainerThatStartsAfterThePreviewIsRefusedNotForced(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for name, refusal := range recordedRefusals(t, "containers", "rm-running", enginefixture.Tool(shape.Name)) {
			t.Run(name, func(t *testing.T) {
				fake := installEngine(t, shape, append(rmOK(idZombie, idFresh), rmRefused(idAPI, refusal))...)
				m := preview(t, feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()}))

				started := stoppedFixtures()
				for i := range started {
					if started[i].ID == idAPI {
						started[i].State = "running"
					}
				}
				m = feed(t, m, ContainersListMsg{Containers: started})
				m = confirm(t, m)

				wantRemoved(t, fake, idAPI, idZombie, idFresh) // the engine was asked for all three, bare
				attempts := 0
				for _, argv := range destructiveCalls(fake) {
					if slices.Contains(argv, idAPI) {
						attempts++
					}
				}
				if attempts != 1 {
					t.Errorf("the container that started was asked about %d times, want once and never again with a force option", attempts)
				}
				footerIs(t, m, sharedcomponents.LevelWarning, "2 removed", "1 not removed", "running")
			})
		}
	})
}

// ── Criterion 6 ──────────────────────────────────────────────────────────────

// A container already gone when its turn comes (somebody removed it) is an
// information, not an error: the others are removed, and the footer is level Info.
func TestAContainerAlreadyGoneIsInformationNotAnError(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := installEngine(t, shape, append(rmOK(idAPI, idFresh), rmRefused(idZombie, goneReply(shape, idZombie)))...)
		m := preview(t, feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()}))

		m = confirm(t, m)

		wantRemoved(t, fake, idAPI, idZombie, idFresh)
		footerIs(t, m, sharedcomponents.LevelInfo, "2 removed", "1 already gone")
		if strings.Contains(strings.ToLower(m.footer.Text()), "not removed") || strings.Contains(strings.ToLower(m.footer.Text()), "fail") {
			t.Errorf("footer = %q: a container that is already gone is not a failure", m.footer.Text())
		}
	})
}

// A failure the engine does not explain stays an error, and is not hidden behind
// "already gone": the engine being unreachable is not a removal somebody else did.
func TestAnUnexplainedFailureIsStillAnError(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		denied := fakecli.Reply{Stderr: "permission denied while trying to connect to the Docker daemon socket\n", Exit: 1}
		installEngine(t, shape, append(rmOK(idAPI, idFresh), rmRefused(idZombie, denied))...)
		m := preview(t, feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()}))

		m = confirm(t, m)

		footerIs(t, m, sharedcomponents.LevelError, "2 removed", "1 not removed")
		if strings.Contains(strings.ToLower(m.footer.Text()), "already gone") {
			t.Errorf("footer = %q: a refused connection is not an item that is already gone", m.footer.Text())
		}
	})
}

// The single `D` follows the same rule: a container that is already gone is an
// information, and still exactly one bare `rm <id>`.
func TestDeletingAContainerAlreadyGoneIsInformation(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := installEngine(t, shape, rmRefused(idAPI, goneReply(shape, idAPI)))
		m := selectNamed(t, loadedModel(t), "api")

		m, _ = step(t, m, testutil.Key(keymap.Delete))
		if m.confirmModal == nil {
			t.Fatal("D opened no confirmation")
		}
		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
		m = settle(t, m, cmd)

		if calls := destructiveCalls(fake); len(calls) != 1 || !slices.Equal(calls[0], []string{"rm", idAPI}) {
			t.Errorf("destructive calls = %q, want exactly [rm %s]", calls, idAPI)
		}
		footerIs(t, m, sharedcomponents.LevelInfo, "already gone")
		if m.containerTable.IsBusy(idAPI) {
			t.Error("the row is still marked busy after the engine answered")
		}
	})
}

// ── Criterion 5, for the view ────────────────────────────────────────────────

// An identifier that is not one (a table that read a line wrongly) is never
// an argv word, and it does not stop the others.
func TestAMalformedIdentifierIsNeverSentToTheEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := installEngine(t, shape, rmOK(idAPI)...)
		list := []docker.Container{
			{ID: "-f", Name: "evil1", State: "exited"},
			{ID: "--all", Name: "evil2", State: "exited"},
			{ID: "", Name: "empty", State: "exited"},
			{ID: idAPI, Name: "api", State: "exited"},
		}
		m := feed(t, newTestModel(t), ContainersListMsg{Containers: list})

		m, cmd := step(t, m, testutil.Key(keymap.Prune))
		m = settle(t, m, cmd)
		if m.confirmModal != nil {
			m = confirm(t, m)
		}

		wantRemoved(t, fake, idAPI)
	})
}
