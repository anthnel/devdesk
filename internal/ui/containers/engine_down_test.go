package containers

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/canary"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// OC-24 (DEVDESK-822), criteria 1 to 7 on the :containers view. The engine is the test
// binary answering as docker or podman (fakecli): nothing here reaches a real engine.
//
// What the view must do, decided with the plan (Q3):
//   - a read that could not ask the engine at all (binary absent, daemon down, socket
//     refused) puts the view in a DERIVED state: Status at level Error, rendered on
//     every frame, with no timer (a footer.Error would expire after 3 s, which is D47);
//   - the table is emptied (Rule 139: its body stays m.table.View(), header, no rows);
//   - the state survives the clear of the footer and the refresh tick, and ends at the
//     first list that succeeds;
//   - a failure nobody classified keeps "Failed to load containers — check logs", and
//     the rows it did not touch;
//   - what is on screen is one of six fixed sentences, never a word of the engine.

// engineDownLog captures the log of the test. TestMain discards it for the package;
// the previous writer comes back when the test ends.
func engineDownLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(io.Discard) })
	return &buf
}

// useEngine pins the engine DevDesk is configured for and restores it afterwards.
func useEngine(t *testing.T, name string) {
	t.Helper()
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(name))
}

// answerWith installs a fake engine that fails every `<verb...>` call the way probe says.
func answerWith(t *testing.T, probe engineProbe, verb ...string) {
	t.Helper()
	useEngine(t, probe.tool)
	fakecli.Install(t, fakecli.Scenario{
		{Tool: probe.tool, Args: verb, Prefix: true, Reply: fakecli.Reply{Exit: probe.exit, Stderr: probe.stderr}},
	}, probe.tool)
}

// withoutEngine makes the configured engine's binary absent from PATH: only the OTHER
// engine is installed (never a real one).
func withoutEngine(t *testing.T, name string) {
	t.Helper()
	useEngine(t, name)
	other := engine.Podman
	if name == engine.Podman {
		other = engine.Docker
	}
	fakecli.Install(t, nil, other)
}

// listed runs the Cmd the view returns from Init for the list and returns its message,
// having checked it is the list and, when the read failed, that the failure is typed.
func listed(t *testing.T) ContainersListMsg {
	t.Helper()
	raw := fetchContainers()()
	msg, ok := raw.(ContainersListMsg)
	if !ok {
		t.Fatalf("fetchContainers returned %T, want ContainersListMsg", raw)
	}
	return msg
}

// listedUnavailable is listed for a read that must come back as an unavailable engine.
func listedUnavailable(t *testing.T, want docker.Unavailability) ContainersListMsg {
	t.Helper()
	msg := listed(t)
	var unavailable *docker.UnavailableError
	if !errors.As(msg.Err, &unavailable) {
		t.Fatalf("the list failed with %T (%v), want a *docker.UnavailableError: the probe text is not classified", msg.Err, msg.Err)
	}
	if unavailable.Reason != want {
		t.Fatalf("the list failed with class %d, want %d", unavailable.Reason, want)
	}
	return msg
}

// screen is what is on screen: the table, the footer and the header values, text only.
func screen(m Model) string {
	var header []string
	for _, h := range m.GetHeaderInfo("") {
		header = append(header, h.Key+" "+h.Value)
	}
	return strings.Join([]string{
		ansi.Strip(m.Render()), ansi.Strip(m.RenderFooter(160)), strings.Join(header, " "),
	}, "\n")
}

// assertEngineDown fails unless the view says sentence the way a derived state is said:
// at level Error, in the footer line, with no timed message in front of it. The
// problems are reported together, on one line, because they share one cause.
func assertEngineDown(t *testing.T, m Model, sentence string) {
	t.Helper()
	var problems []string
	if st := m.status(); st.Text != sentence || st.Level != components.LevelError {
		problems = append(problems, fmt.Sprintf("status() = {Text: %q, Level: %d}, want {Text: %q, Level: LevelError}", st.Text, st.Level, sentence))
	}
	if footer := ansi.Strip(m.RenderFooter(160)); !strings.Contains(footer, sentence) {
		problems = append(problems, fmt.Sprintf("the footer line is %q", strings.TrimSpace(footer)))
	} else if !strings.Contains(m.RenderFooter(160), theme.FooterErrorStyle.Render(sentence)) {
		problems = append(problems, "the sentence is not drawn in the footer's error style")
	}
	if m.footer.IsSet() {
		problems = append(problems, fmt.Sprintf("a timed footer message holds %q (a state is derived on every frame; a message expires after 3 s)", m.footer.Text()))
	}
	if len(problems) > 0 {
		t.Errorf("the view should say %q: %s", sentence, strings.Join(problems, "; "))
	}
}

// assertTheTableStays fails unless the body is still the table, header and no rows (Rule 139).
func assertTheTableStays(t *testing.T, m Model) {
	t.Helper()
	if n := len(m.containerTable.Items()); n != 0 {
		t.Errorf("the table holds %d rows, want none: a lost engine empties it", n)
	}
	body := ansi.Strip(m.Render())
	for _, title := range []string{"Name", "Image"} {
		if !strings.Contains(body, title) {
			t.Errorf("the body lost its %q column header, so it is no longer the table (Rule 139):\n%s", title, body)
		}
	}
}

// assertNoEngineState fails when the view still says the engine is down.
func assertNoEngineState(t *testing.T, m Model) {
	t.Helper()
	if st := m.status(); st.Level == components.LevelError {
		t.Errorf("status() = %+v, want no error state", st)
	}
	for _, sentence := range []string{
		sentenceDockerNotFound, sentenceDockerUnreach, sentenceDockerPermission,
		sentencePodmanNotFound, sentencePodmanUnreach, sentencePodmanPermission,
	} {
		if strings.Contains(screen(m), sentence) {
			t.Errorf("the screen still says %q", sentence)
		}
	}
}

// ── Criterion 1: the binary is absent ────────────────────────────────────────

func TestOpeningWithoutTheEngineBinarySaysSo(t *testing.T) {
	for _, c := range []struct{ engine, sentence string }{
		{engine.Docker, sentenceDockerNotFound},
		{engine.Podman, sentencePodmanNotFound},
	} {
		t.Run(c.engine, func(t *testing.T) {
			withoutEngine(t, c.engine)

			m := feed(t, newTestModel(t), listedUnavailable(t, docker.EngineNotFound))

			assertEngineDown(t, m, c.sentence)
			assertTheTableStays(t, m)
			if body := ansi.Strip(m.Render()); strings.Contains(body, "not found") {
				t.Errorf("the body says the engine is missing, so it replaced the table (Rule 139):\n%s", body)
			}
		})
	}
}

// ── Criterion 2: the daemon does not answer ──────────────────────────────────

func TestAnUnreachableDaemonIsAPersistentError(t *testing.T) {
	for _, probe := range probesUnreachable {
		t.Run(probe.String(), func(t *testing.T) {
			answerWith(t, probe, "ps")

			m := feed(t, newTestModel(t), listedUnavailable(t, docker.EngineUnreachable))
			assertEngineDown(t, m, probe.want)
			assertTheTableStays(t, m)

			// A state, not a message: the clear of the footer does not reach it, nor the
			// refresh that follows, nor a second failed read, nor a resize.
			m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
			assertEngineDown(t, m, probe.want)

			m, _ = step(t, m, RefreshTickMsg(time.Now())) // the Cmds it returns are not run
			assertEngineDown(t, m, probe.want)

			m = feed(t, m, listedUnavailable(t, docker.EngineUnreachable), testutil.Resize(120, 30))
			assertEngineDown(t, m, probe.want)
		})
	}
}

// ── Criterion 3: the socket refuses this user ────────────────────────────────

func TestAPermissionDeniedSocketSaysWhatToDo(t *testing.T) {
	for _, probe := range probesPermission {
		t.Run(probe.String(), func(t *testing.T) {
			answerWith(t, probe, "ps")

			m := feed(t, newTestModel(t), listedUnavailable(t, docker.EnginePermissionDenied))

			assertEngineDown(t, m, probe.want)
			assertTheTableStays(t, m)
			m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
			assertEngineDown(t, m, probe.want)
		})
	}
}

// ── Criterion 4: the engine comes back ───────────────────────────────────────

func TestTheErrorLeavesWhenTheEngineComesBack(t *testing.T) {
	probe := probesUnreachable[0]
	answerWith(t, probe, "ps")
	m := feed(t, newTestModel(t), listedUnavailable(t, docker.EngineUnreachable))
	assertEngineDown(t, m, probe.want)

	// The refresh finds the daemon running: a list with two rows.
	m = allStates(t, feed(t, m, ContainersListMsg{Containers: containerFixtures()[:2]}))

	assertNoEngineState(t, m)
	if n := len(m.containerTable.Items()); n != 2 {
		t.Errorf("the table holds %d rows, want the 2 of the list that succeeded", n)
	}
	for _, name := range []string{"web", "api"} {
		if !strings.Contains(screen(m), name) {
			t.Errorf("the screen does not show %q once the engine is back:\n%s", name, screen(m))
		}
	}
}

// The bilan of a P or a D is a footer message with a timer of its own. It was set
// before the engine went away, and it survives the engine coming back (it leaves with
// its timer), while the state ends: the state does not depend on ClearUnlessRemoval.
func TestARemovalBilanSurvivesTheEngineComingBack(t *testing.T) {
	report := docker.RemovalReport{{Target: "aaaa111122223333", Status: docker.RemovalRemoved}}
	probe := probesUnreachable[0]
	answerWith(t, probe, "ps")

	m := newTestModel(t)
	m.footer.Removal(report) // the timer is not run
	bilan := m.footer.Text()
	if bilan == "" || bilan != report.Summary() {
		t.Fatalf("setup: the footer holds %q, want the bilan %q", bilan, report.Summary())
	}

	m = feed(t, m, listedUnavailable(t, docker.EngineUnreachable))
	if m.footer.Text() != bilan {
		t.Errorf("the failed list replaced the bilan with %q, want it kept until its timer", m.footer.Text())
	}
	if st := m.status(); st.Text != probe.want || st.Level != components.LevelError {
		t.Errorf("status() = %+v while the bilan is shown, want the engine state held under it", st)
	}

	m = feed(t, m, ContainersListMsg{Containers: containerFixtures()[:2]})

	assertNoEngineState(t, m)
	if m.footer.Text() != bilan {
		t.Errorf("the footer says %q after the engine came back, want the bilan %q kept", m.footer.Text(), bilan)
	}
}

// …and when the bilan expires on a view whose engine is still down, the state is what
// the footer says next.
func TestTheEngineStateAppearsWhenTheBilanExpires(t *testing.T) {
	report := docker.RemovalReport{{Target: "aaaa111122223333", Status: docker.RemovalRemoved}}
	probe := probesPermission[0]
	answerWith(t, probe, "ps")

	m := newTestModel(t)
	m.footer.Removal(report)
	m = feed(t, m, listedUnavailable(t, docker.EnginePermissionDenied))
	if shown := ansi.Strip(m.RenderFooter(160)); !strings.Contains(shown, report.Summary()) || strings.Contains(shown, probe.want) {
		t.Fatalf("the footer is %q, want the bilan alone while it lasts", strings.TrimSpace(shown))
	}

	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})

	assertEngineDown(t, m, probe.want)
}

// ── Criterion 5: an unknown failure keeps the generic message ────────────────

func TestAnUnknownListFailureKeepsCheckLogs(t *testing.T) {
	const generic = "Failed to load containers — check logs"
	for _, probe := range probesUnknown {
		t.Run(probe.String(), func(t *testing.T) {
			answerWith(t, probe, "ps")
			msg := listed(t)
			if msg.Err == nil {
				t.Fatal("the fake engine answered, want a failure")
			}
			if sentence, ok := docker.UnavailableMessage(msg.Err); ok {
				t.Fatalf("the probe %q reads as an unavailable engine (%q): it is not an unknown failure", probe, sentence)
			}

			m := feed(t, loadedModel(t), msg)

			if m.footer.Text() != generic || m.footer.Level() != components.LevelError {
				t.Errorf("footer = (%q, level %d), want (%q, LevelError)", m.footer.Text(), m.footer.Level(), generic)
			}
			if !strings.Contains(ansi.Strip(m.RenderFooter(160)), generic) {
				t.Errorf("the footer line does not say %q", generic)
			}
			// Nothing was learned about the engine, so nothing is taken away: the rows stay.
			if n := len(m.containerTable.Items()); n != len(containerFixtures()) {
				t.Errorf("the table holds %d rows after an unclassified failure, want the %d it had", n, len(containerFixtures()))
			}
			// And no engine state is derived: once the message expires the line is empty again.
			m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
			if st := m.status(); st.Level == components.LevelError || strings.Contains(st.Text, "Docker") || strings.Contains(st.Text, "Podman") {
				t.Errorf("status() = %+v after the message expired, want no engine state", st)
			}
		})
	}
}

// ── Criterion 6: nothing the engine said reaches the screen ──────────────────

func TestNoEngineTextReachesTheScreen(t *testing.T) {
	for _, probe := range trapped {
		t.Run(probe.String(), func(t *testing.T) {
			answerWith(t, probe, "ps")
			m := feed(t, newTestModel(t), listed(t))

			if probe.want != "" {
				assertEngineDown(t, m, probe.want)
			}

			var surfaces canary.Surfaces
			surfaces.Add(
				canary.Text("containers body", func() string { return ansi.Strip(m.Render()) }),
				canary.Text("containers footer", func() string { return ansi.Strip(m.RenderFooter(160)) }),
				canary.Text("containers footer, styled", func() string { return m.RenderFooter(160) }),
				canary.Text("containers status", func() string { return m.status().Text }),
				canary.Text("containers header", func() string { return screen(m) }),
				canary.Text("footer message", func() string { return m.footer.Text() }),
			)
			surfaces.AssertAbsent(t)

			for _, fragment := range neverOnScreen {
				if strings.Contains(screen(m), fragment) {
					t.Errorf("the screen holds %q:\n%s", fragment, screen(m))
				}
			}
			// The message the user sees one frame later, once any timed message is gone.
			m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
			for _, fragment := range neverOnScreen {
				if strings.Contains(screen(m), fragment) {
					t.Errorf("after the footer message expired, the screen holds %q:\n%s", fragment, screen(m))
				}
			}
		})
	}
}

// The journal is where the cause goes: the log line of the failed list holds the engine's
// first line, its URLs reduced to scheme and host and their credentials gone
// (docker.LogText), and nothing else of it.
func TestTheLogGetsTheRedactedCauseAndNeverACredential(t *testing.T) {
	for _, probe := range trapped {
		t.Run(probe.String(), func(t *testing.T) {
			answerWith(t, probe, "ps")
			msg := listed(t)
			buf := engineDownLog(t)

			feed(t, newTestModel(t), msg)

			logged := buf.String()
			if !strings.Contains(logged, "***@") {
				t.Errorf("the log holds %q, want the cause of the failure with its credentials replaced by ***@ (docker.LogText)", logged)
			}
			for _, secret := range []string{"s3cret", "admin:", "u:s3cret", "p:q@"} {
				if strings.Contains(logged, secret) {
					t.Errorf("the log holds the credential %q: %q", secret, logged)
				}
			}
		})
	}
}

// ── Q3: a lost engine empties the table ──────────────────────────────────────

func TestAStoppedEngineEmptiesTheTable(t *testing.T) {
	probe := probesUnreachable[0]
	answerWith(t, probe, "ps")
	m := loadedModel(t)
	if n := len(m.containerTable.Items()); n != 4 {
		t.Fatalf("setup: %d rows, want 4", n)
	}
	if !strings.Contains(screen(m), "web") {
		t.Fatal("setup: the rows are not on screen")
	}

	m = feed(t, m, listedUnavailable(t, docker.EngineUnreachable))

	assertTheTableStays(t, m)
	assertEngineDown(t, m, probe.want)
	if strings.Contains(ansi.Strip(m.Render()), "web") {
		t.Error("a row of the previous list is still drawn")
	}
	if got := m.GetHeaderInfo(""); len(got) == 0 || got[0].Value != "0" {
		t.Errorf("the header says %+v, want the count at 0 (the count lives in GetHeaderInfo, Rule 139)", got)
	}
}

// ── Actions ──────────────────────────────────────────────────────────────────

// A mutation that finds the engine unavailable says so the way a list does; any other
// failure keeps "Action failed — check logs". The typed error is built here because the
// mutators still return the engine's own error (a later change types them).
func TestAnActionOnAnUnavailableEngineSaysSo(t *testing.T) {
	useEngine(t, engine.Docker)
	err := &docker.UnavailableError{Reason: docker.EngineUnreachable, Engine: engine.Docker, Label: "docker stop"}

	m := feed(t, selectNamed(t, loadedModel(t), "web"), ContainerActionMsg{Action: "stop", ID: webID, Name: "web", Err: err})

	assertEngineDownSentence(t, m, sentenceDockerUnreach)
	if m.footer.Text() == "Action failed — check logs" {
		t.Error(`the footer still says "Action failed — check logs"`)
	}
}

// assertEngineDownSentence is assertEngineDown for a view that keeps its rows and may
// hold a timed message: only the derived line is checked.
func assertEngineDownSentence(t *testing.T, m Model, sentence string) {
	t.Helper()
	if st := m.status(); st.Text != sentence || st.Level != components.LevelError {
		t.Errorf("status() = {Text: %q, Level: %d}, want {Text: %q, Level: LevelError}", st.Text, st.Level, sentence)
	}
}

func TestAnUnknownActionFailureKeepsCheckLogs(t *testing.T) {
	answerWith(t, engineProbe{tool: engine.Docker, exit: 1, stderr: "Error response from daemon: something nobody probed\n"}, "stop")

	msg, ok := stopContainer(webID, "web")().(ContainerActionMsg)
	if !ok || msg.Err == nil {
		t.Fatalf("stopContainer produced %#v, want a failed ContainerActionMsg", msg)
	}
	m := feed(t, selectNamed(t, loadedModel(t), "web"), msg)

	if m.footer.Text() != "Action failed — check logs" || m.footer.Level() != components.LevelError {
		t.Errorf("footer = (%q, level %d), want (\"Action failed — check logs\", LevelError)", m.footer.Text(), m.footer.Level())
	}
	if st := m.status(); st.Level == components.LevelError {
		t.Errorf("status() = %+v, want no engine state for an unknown failure", st)
	}
}

// ── Criterion 7: an inspect that fails is typed ──────────────────────────────

// openInspect presses enter on the only row and returns the viewer the router would
// open, loaded: the real docker.InspectContainer against the fake engine.
func openInspect(t *testing.T, id string) uiviewer.Model {
	t.Helper()
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: []docker.Container{
		{ID: id, Name: "web", Image: "nginx:1.27", State: "running"},
	}})
	_, cmd := step(t, m, testutil.Key("enter"))
	source := openRequestSource(t, cmd)

	v := uiviewer.NewWithSource(config.Default(), source)
	next, _ := v.Update(testutil.Resize(100, 30))
	v = next.(uiviewer.Model)
	for _, msg := range testutil.Msgs(v.Init()) {
		if loaded, ok := msg.(uiviewer.DocumentLoadedMsg); ok {
			next, _ := v.Update(loaded)
			v = next.(uiviewer.Model)
		}
	}
	return v
}

func TestInspectWithTheDaemonDownShowsTheSentence(t *testing.T) {
	for _, probe := range []engineProbe{probesUnreachable[0], probesPermission[0], probesUnreachable[4]} {
		t.Run(probe.String(), func(t *testing.T) {
			answerWith(t, probe, "inspect")

			v := openInspect(t, webID)

			footer := ansi.Strip(v.RenderFooter(120))
			if !strings.Contains(footer, probe.want) {
				t.Errorf("the viewer footer is %q, want it to say %q", strings.TrimSpace(footer), probe.want)
			}
			if raw := v.RenderFooter(120); !strings.Contains(raw, theme.FooterErrorStyle.Render(probe.want)) {
				t.Errorf("the sentence is not drawn at level Error: %q", raw)
			}
			for _, fragment := range neverOnScreen {
				if strings.Contains(footer, fragment) {
					t.Errorf("the viewer footer holds %q: %q", fragment, footer)
				}
			}
		})
	}
}

// A container that is gone is the engine answering: the answer is its own words, which no
// screen shows. The recorded refusal of each engine, replayed byte for byte.
func TestInspectOfAnAbsentContainerShowsNoEngineText(t *testing.T) {
	all, err := enginefixture.Load("containers")
	if err != nil {
		t.Fatalf("loading the recorded fixtures: %v", err)
	}
	for _, c := range []struct {
		tool    enginefixture.Tool
		version string
		mode    enginefixture.Mode
	}{
		{enginefixture.Docker, "29.10.1", enginefixture.Rootful},
		{enginefixture.Podman, "6.2.1", enginefixture.Rootless},
	} {
		t.Run(string(c.tool)+" "+c.version, func(t *testing.T) {
			var absent []enginefixture.Fixture
			for _, f := range enginefixture.ForConfig(all, c.tool, c.version, c.mode) {
				if f.Scenario == "inspect-absent" {
					absent = append(absent, f)
				}
			}
			if len(absent) != 1 || len(absent[0].Exchange.Argv) != 2 {
				t.Fatalf("found %d inspect-absent fixtures for %s %s, want exactly one of the shape [inspect <id>]", len(absent), c.tool, c.version)
			}
			useEngine(t, string(c.tool))
			fakecli.Install(t, fakecli.Fixtures(t, absent...), string(c.tool))

			v := openInspect(t, absent[0].Exchange.Argv[1])

			footer := ansi.Strip(v.RenderFooter(120))
			if !strings.Contains(footer, "Failed to open — check logs") {
				t.Errorf("the viewer footer is %q, want \"Failed to open — check logs\"", strings.TrimSpace(footer))
			}
			if strings.Contains(strings.ToLower(footer), "no such") {
				t.Errorf("the viewer footer shows the engine's words: %q", footer)
			}
		})
	}
}

// ── What the lost engine leaves behind (review of OC-24, PR 2) ───────────────

// ctrl+r with the engine away: the load of the empty table is what the footer says
// while it lasts (the dot is drawn and WantsBlink agrees), then the state comes back
// with the list that failed again. WantsBlink is never true for a dot that is not drawn.
func TestARefreshWithTheEngineAwayShowsTheLoadThenTheState(t *testing.T) {
	probe := probesUnreachable[0]
	answerWith(t, probe, "ps")
	m := feed(t, newTestModel(t), listedUnavailable(t, docker.EngineUnreachable))
	assertEngineDown(t, m, probe.want)
	if m.WantsBlink() {
		t.Error("WantsBlink is true while the engine state is shown: the dot is not drawn")
	}

	m = feed(t, m, testutil.Key("ctrl+r"))

	if st := m.status(); !st.Activity || st.Text != "Loading containers..." {
		t.Errorf("status() = %+v during ctrl+r, want the load with its dot", st)
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink is false while the dot is drawn")
	}

	m = feed(t, m, listedUnavailable(t, docker.EngineUnreachable))

	assertEngineDown(t, m, probe.want)
	if m.WantsBlink() {
		t.Error("WantsBlink is true again once the engine state is shown")
	}
}

// P with the engine away opens no confirmation: the list that held what could be
// removed is gone, so there is nothing to remove.
func TestPruneWithTheEngineAwayOpensNoConfirmation(t *testing.T) {
	probe := probesUnreachable[0]
	answerWith(t, probe, "ps")
	m := feed(t, loadedModel(t), listedUnavailable(t, docker.EngineUnreachable))
	assertEngineDown(t, m, probe.want)

	m, _ = step(t, m, testutil.Key(keymap.Prune))

	if m.confirmModal != nil {
		t.Error("P opened a confirmation with the engine away")
	}
}
