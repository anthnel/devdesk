package ociresources

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/testutil/canary"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// OC-24 (DEVDESK-822), criteria 1 to 6 on :oci, tab by tab (Images, Networks, Volumes).
// Same decisions as the :containers view (internal/ui/containers/engine_down_test.go):
// a read that could not ask the engine at all is a DERIVED state, Status at level Error
// rendered on every frame, with no timer, that survives the clear of the footer and the
// refresh tick and ends at the first list that succeeds; the table is emptied but stays
// a table (Rule 139); an unclassified failure keeps its generic "... — check logs" and
// its rows; nothing the engine said reaches the screen, and the cause goes to the log
// through docker.LogText. The engine is the test binary answering as docker or podman
// (fakecli): nothing here reaches a real engine.
//
// Only the state of the tab on screen is shown (TestOnlyTheActiveTabsEngineStateIsShown).
// The refusal of an agent's pending scan request is in engine_unavailable_refusal_test.go.

// engineTab describes one tab of the view for the table-driven tests.
type engineTab struct {
	name    string
	presses int      // presses of `tab` from the Images tab
	verb    []string // the argv prefix of its list
	generic string   // what an unclassified failure says
	fetch   func() tea.Cmd
	errOf   func(tea.Msg) error
	rows    func(Model) int
	header  string  // a column title of its table
	listed  tea.Msg // a list that succeeds
	rowName string  // a row of that list, as it is drawn
}

func engineTabs() []engineTab {
	return []engineTab{
		{
			name: "images", presses: 0, verb: []string{"image", "ls"}, generic: "Failed to load images — check logs",
			fetch:  fetchImages,
			errOf:  func(m tea.Msg) error { return m.(ImagesListMsg).Err },
			rows:   func(m Model) int { return max(len(m.imageTable.Items()), len(m.images)) },
			header: "Name", listed: ImagesListMsg{Images: imageFixtures()}, rowName: "cache",
		},
		{
			name: "networks", presses: 1, verb: []string{"network", "ls"}, generic: "Failed to load networks — check logs",
			fetch:  fetchNetworks,
			errOf:  func(m tea.Msg) error { return m.(NetworksListMsg).Err },
			rows:   func(m Model) int { return len(m.networkTable.Items()) },
			header: "Driver", listed: NetworksListMsg{Networks: networkFixtures()}, rowName: "devdesk",
		},
		{
			name: "volumes", presses: 2, verb: []string{"volume", "ls"}, generic: "Failed to load volumes — check logs",
			fetch:  fetchVolumes,
			errOf:  func(m tea.Msg) error { return m.(VolumesListMsg).Err },
			rows:   func(m Model) int { return len(m.volumeTable.Items()) },
			header: "Mountpoint", listed: VolumesListMsg{Volumes: volumeFixtures()}, rowName: "pgdata",
		},
	}
}

// onTab presses `tab` until the tab is on screen.
func (tab engineTab) onTab(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < tab.presses; i++ {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m
}

// answered runs the Cmd the view runs to read this tab's list and returns its message.
func (tab engineTab) answered(t *testing.T) tea.Msg {
	t.Helper()
	return tab.fetch()()
}

// unavailableList is answered for a read that must come back as an unavailable engine.
func (tab engineTab) unavailableList(t *testing.T, want docker.Unavailability) tea.Msg {
	t.Helper()
	msg := tab.answered(t)
	var unavailable *docker.UnavailableError
	if err := tab.errOf(msg); !errors.As(err, &unavailable) {
		t.Fatalf("the %s list failed with %T (%v), want a *docker.UnavailableError: the probe text is not classified", tab.name, err, err)
	}
	if unavailable.Reason != want {
		t.Fatalf("the %s list failed with class %d, want %d", tab.name, unavailable.Reason, want)
	}
	return msg
}

// answerWith installs a fake engine that fails every `<verb...>` call the way probe says.
func answerWith(t *testing.T, probe engineProbe, verb ...string) {
	t.Helper()
	useEngineNamed(t, probe.tool)
	fakecli.Install(t, fakecli.Scenario{
		{Tool: probe.tool, Args: verb, Prefix: true, Reply: fakecli.Reply{Exit: probe.exit, Stderr: probe.stderr}},
	}, probe.tool)
}

// withoutEngine makes the configured engine's binary absent from PATH: only the OTHER
// engine is installed (never a real one).
func withoutEngine(t *testing.T, name string) {
	t.Helper()
	useEngineNamed(t, name)
	other := engine.Podman
	if name == engine.Podman {
		other = engine.Docker
	}
	fakecli.Install(t, nil, other)
}

// engineDownLog captures the log of the test; TestMain discards it for the package.
func engineDownLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(io.Discard) })
	return &buf
}

// screen is what is on screen: the table, the footer and the header values, text only.
func screen(m Model) string {
	var header []string
	for _, h := range m.GetHeaderInfo("") {
		header = append(header, h.Key+" "+h.Value)
	}
	return strings.Join([]string{
		ansi.Strip(m.Render()), ansi.Strip(m.RenderFooter(180)), strings.Join(header, " "),
	}, "\n")
}

// assertEngineDown fails unless the view says sentence the way a derived state is said:
// at level Error, in the footer line, with no timed message in front of it.
func assertEngineDown(t *testing.T, m Model, sentence string) {
	t.Helper()
	var problems []string
	if st := m.status(); st.Text != sentence || st.Level != components.LevelError {
		problems = append(problems, fmt.Sprintf("status() = {Text: %q, Level: %d}, want {Text: %q, Level: LevelError}", st.Text, st.Level, sentence))
	}
	if footer := ansi.Strip(m.RenderFooter(180)); !strings.Contains(footer, sentence) {
		problems = append(problems, fmt.Sprintf("the footer line is %q", strings.TrimSpace(footer)))
	} else if !strings.Contains(m.RenderFooter(180), theme.FooterErrorStyle.Render(sentence)) {
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
func (tab engineTab) assertTheTableStays(t *testing.T, m Model) {
	t.Helper()
	if n := tab.rows(m); n != 0 {
		t.Errorf("the %s table holds %d rows, want none: a lost engine empties it", tab.name, n)
	}
	if body := ansi.Strip(m.Render()); !strings.Contains(body, tab.header) {
		t.Errorf("the body lost its %q column header, so it is no longer the table (Rule 139):\n%s", tab.header, body)
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

// forEachTab runs fn as a subtest per tab.
func forEachTab(t *testing.T, fn func(t *testing.T, tab engineTab)) {
	t.Helper()
	for _, tab := range engineTabs() {
		t.Run(tab.name, func(t *testing.T) { fn(t, tab) })
	}
}

// ── Criterion 1: the binary is absent ────────────────────────────────────────

func TestOpeningWithoutTheEngineBinarySaysSo(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		for _, c := range []struct{ engine, sentence string }{
			{engine.Docker, sentenceDockerNotFound},
			{engine.Podman, sentencePodmanNotFound},
		} {
			t.Run(c.engine, func(t *testing.T) {
				withoutEngine(t, c.engine)
				m := tab.onTab(t, newTestModel(t))

				m = feed(t, m, tab.unavailableList(t, docker.EngineNotFound))

				assertEngineDown(t, m, c.sentence)
				tab.assertTheTableStays(t, m)
				if body := ansi.Strip(m.Render()); strings.Contains(body, "not found") {
					t.Errorf("the body says the engine is missing, so it replaced the table (Rule 139):\n%s", body)
				}
			})
		}
	})
}

// ── Criteria 2 and 3: the daemon does not answer, the socket refuses ─────────

func TestAnUnreachableDaemonIsAPersistentError(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		for _, probe := range []engineProbe{probesUnreachable[0], probesUnreachable[4]} {
			t.Run(probe.String(), func(t *testing.T) {
				answerWith(t, probe, tab.verb...)
				m := tab.onTab(t, newTestModel(t))

				m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))
				assertEngineDown(t, m, probe.want)
				tab.assertTheTableStays(t, m)

				// A state, not a message: the clear of the footer does not reach it, nor the
				// refresh that follows, nor a second failed read, nor a resize.
				m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
				assertEngineDown(t, m, probe.want)

				m, _ = step(t, m, RefreshTickMsg{}) // the Cmds it returns are not run
				assertEngineDown(t, m, probe.want)

				m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable), testutil.Resize(150, 30))
				assertEngineDown(t, m, probe.want)
			})
		}
	})
}

func TestAPermissionDeniedSocketSaysWhatToDo(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		for _, probe := range []engineProbe{probesPermission[0], probesPermission[2]} {
			t.Run(probe.String(), func(t *testing.T) {
				answerWith(t, probe, tab.verb...)
				m := tab.onTab(t, newTestModel(t))

				m = feed(t, m, tab.unavailableList(t, docker.EnginePermissionDenied))

				assertEngineDown(t, m, probe.want)
				tab.assertTheTableStays(t, m)
				m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
				assertEngineDown(t, m, probe.want)
			})
		}
	})
}

// ── Criterion 4: the engine comes back ───────────────────────────────────────

func TestTheErrorLeavesWhenTheEngineComesBack(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		probe := probesUnreachable[0]
		answerWith(t, probe, tab.verb...)
		m := tab.onTab(t, newTestModel(t))
		m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))
		assertEngineDown(t, m, probe.want)

		m = feed(t, m, tab.listed)

		assertNoEngineState(t, m)
		if tab.rows(m) == 0 {
			t.Errorf("the %s table is empty, want the rows of the list that succeeded", tab.name)
		}
		if !strings.Contains(screen(m), tab.rowName) {
			t.Errorf("the screen does not show %q once the engine is back:\n%s", tab.rowName, screen(m))
		}
	})
}

// The bilan of a P or a D is a footer message with a timer of its own. It was set before
// the engine went away and survives the engine coming back (Images and Volumes: the
// Networks list never kept one), while the state ends: the state does not depend on
// ClearUnlessRemoval.
func TestARemovalBilanSurvivesTheEngineComingBack(t *testing.T) {
	report := docker.RemovalReport{{Target: "ddd4444", Status: docker.RemovalRemoved}}
	tabs := engineTabs()
	for _, tab := range []engineTab{tabs[0], tabs[2]} {
		t.Run(tab.name, func(t *testing.T) {
			probe := probesUnreachable[0]
			answerWith(t, probe, tab.verb...)
			m := tab.onTab(t, newTestModel(t))
			m.footer.Removal(report) // the timer is not run
			bilan := m.footer.Text()
			if bilan == "" || bilan != report.Summary() {
				t.Fatalf("setup: the footer holds %q, want the bilan %q", bilan, report.Summary())
			}

			m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))
			if m.footer.Text() != bilan {
				t.Errorf("the failed list replaced the bilan with %q, want it kept until its timer", m.footer.Text())
			}
			if st := m.status(); st.Text != probe.want || st.Level != components.LevelError {
				t.Errorf("status() = %+v while the bilan is shown, want the engine state held under it", st)
			}

			m = feed(t, m, tab.listed)

			assertNoEngineState(t, m)
			if m.footer.Text() != bilan {
				t.Errorf("the footer says %q after the engine came back, want the bilan %q kept", m.footer.Text(), bilan)
			}
		})
	}
}

// ── Criterion 5: an unknown failure keeps the generic message ────────────────

func TestAnUnknownListFailureKeepsCheckLogs(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		for _, probe := range probesUnknown {
			t.Run(probe.String(), func(t *testing.T) {
				answerWith(t, probe, tab.verb...)
				msg := tab.answered(t)
				err := tab.errOf(msg)
				if err == nil {
					t.Fatal("the fake engine answered, want a failure")
				}
				if sentence, ok := docker.UnavailableMessage(err); ok {
					t.Fatalf("the probe %q reads as an unavailable engine (%q): it is not an unknown failure", probe, sentence)
				}

				m := tab.onTab(t, loadedModel(t))
				before := tab.rows(m)
				m = feed(t, m, msg)

				if m.footer.Text() != tab.generic || m.footer.Level() != components.LevelError {
					t.Errorf("footer = (%q, level %d), want (%q, LevelError)", m.footer.Text(), m.footer.Level(), tab.generic)
				}
				if !strings.Contains(ansi.Strip(m.RenderFooter(180)), tab.generic) {
					t.Errorf("the footer line does not say %q", tab.generic)
				}
				// Nothing was learned about the engine, so nothing is taken away.
				if n := tab.rows(m); n != before || n == 0 {
					t.Errorf("the table holds %d rows after an unclassified failure, want the %d it had", n, before)
				}
				m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
				if st := m.status(); st.Level == components.LevelError {
					t.Errorf("status() = %+v after the message expired, want no engine state", st)
				}
			})
		}
	})
}

// ── Criterion 6: nothing the engine said reaches the screen ──────────────────

func TestNoEngineTextReachesTheScreen(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		for _, probe := range trapped {
			t.Run(probe.String(), func(t *testing.T) {
				answerWith(t, probe, tab.verb...)
				m := feed(t, tab.onTab(t, newTestModel(t)), tab.answered(t))

				if probe.want != "" {
					assertEngineDown(t, m, probe.want)
				}

				var surfaces canary.Surfaces
				surfaces.Add(
					canary.Text("oci body", func() string { return ansi.Strip(m.Render()) }),
					canary.Text("oci footer", func() string { return ansi.Strip(m.RenderFooter(180)) }),
					canary.Text("oci footer, styled", func() string { return m.RenderFooter(180) }),
					canary.Text("oci status", func() string { return m.status().Text }),
					canary.Text("oci screen", func() string { return screen(m) }),
					canary.Text("footer message", func() string { return m.footer.Text() }),
				)
				surfaces.AssertAbsent(t)

				for _, fragment := range neverOnScreen {
					if strings.Contains(screen(m), fragment) {
						t.Errorf("the screen holds %q:\n%s", fragment, screen(m))
					}
				}
				m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
				for _, fragment := range neverOnScreen {
					if strings.Contains(screen(m), fragment) {
						t.Errorf("after the footer message expired, the screen holds %q:\n%s", fragment, screen(m))
					}
				}
			})
		}
	})
}

// The cause goes to the log, redacted: docker.LogText, never the engine's text as it came.
func TestTheLogGetsTheRedactedCauseAndNeverACredential(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		for _, probe := range trapped {
			t.Run(probe.String(), func(t *testing.T) {
				answerWith(t, probe, tab.verb...)
				msg := tab.answered(t)
				buf := engineDownLog(t)

				feed(t, tab.onTab(t, newTestModel(t)), msg)

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
	})
}

// ── Q3: a lost engine empties the table ──────────────────────────────────────

func TestAStoppedEngineEmptiesTheTable(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		probe := probesUnreachable[0]
		answerWith(t, probe, tab.verb...)
		m := tab.onTab(t, loadedModel(t))
		if tab.rows(m) == 0 || !strings.Contains(screen(m), tab.rowName) {
			t.Fatalf("setup: the %s rows are not on screen", tab.name)
		}

		m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))

		tab.assertTheTableStays(t, m)
		assertEngineDown(t, m, probe.want)
		if strings.Contains(ansi.Strip(m.Render()), tab.rowName) {
			t.Errorf("a row of the previous list (%q) is still drawn", tab.rowName)
		}
	})
}

// ── Only the active tab's state is shown ─────────────────────────────────────

func TestOnlyTheActiveTabsEngineStateIsShown(t *testing.T) {
	// Images: the daemon does not answer. Networks: the socket refuses. Volumes: fine.
	useEngineNamed(t, engine.Docker)
	fakecli.Install(t, fakecli.Scenario{
		{Tool: "docker", Args: []string{"image", "ls"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: probesUnreachable[0].stderr}},
		{Tool: "docker", Args: []string{"network", "ls"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: probesPermission[0].stderr}},
	}, "docker")
	tabs := engineTabs()

	m := newTestModel(t)
	m = feed(t, m,
		tabs[0].unavailableList(t, docker.EngineUnreachable),
		tabs[1].unavailableList(t, docker.EnginePermissionDenied),
		tabs[2].listed,
	)

	// Walk the one model through the three tabs: images, networks, volumes.
	if st := m.status(); st.Text != sentenceDockerUnreach || st.Level != components.LevelError {
		t.Errorf("on Images, status() = %+v, want %q", st, sentenceDockerUnreach)
	}
	m = feed(t, m, testutil.Key("tab"))
	if st := m.status(); st.Text != sentenceDockerPermission || st.Level != components.LevelError {
		t.Errorf("on Networks, status() = %+v, want %q", st, sentenceDockerPermission)
	}
	if shown := ansi.Strip(m.RenderFooter(180)); strings.Contains(shown, sentenceDockerUnreach) {
		t.Errorf("on Networks, the footer also says the Images state: %q", strings.TrimSpace(shown))
	}
	m = feed(t, m, testutil.Key("tab"))
	if st := m.status(); st.Level == components.LevelError {
		t.Errorf("on Volumes, whose list succeeded, status() = %+v, want no engine state", st)
	}
	for _, sentence := range []string{sentenceDockerUnreach, sentenceDockerPermission} {
		if strings.Contains(screen(m), sentence) {
			t.Errorf("on Volumes, the screen says %q, the state of another tab", sentence)
		}
	}
	// And back: the Images state was kept for the tab it belongs to.
	m = feed(t, m, testutil.Key("tab"), testutil.Key("tab")) // volumes -> registries -> images
	if st := m.status(); st.Text != sentenceDockerUnreach {
		t.Errorf("back on Images, status() = %+v, want %q again", st, sentenceDockerUnreach)
	}
}

// ── Actions ──────────────────────────────────────────────────────────────────

// An action that finds the engine unavailable says so the way a list does; any other
// failure keeps its own "... — check logs". The typed error is built here because the
// mutators still return the engine's own error (a later change types them).
func TestAnActionOnAnUnavailableEngineSaysSo(t *testing.T) {
	useEngineNamed(t, engine.Docker)
	err := &docker.UnavailableError{Reason: docker.EngineUnreachable, Engine: engine.Docker, Label: "docker network create"}
	tabs := engineTabs()

	for _, c := range []struct {
		name    string
		tab     engineTab
		msg     tea.Msg
		generic string
	}{
		{"image action", tabs[0], ImageActionMsg{Action: "pull", ID: "aaa1111", Name: "api:v1", Err: err}, "Action failed — check logs"},
		{"network create", tabs[1], NetworkActionMsg{Action: "create", Err: err}, "Network action failed — check logs"},
		{"network prune", tabs[1], NetworkPruneCompleteMsg{Err: err}, "Network prune failed — check logs"},
		{"volume create", tabs[2], VolumeActionMsg{Action: "create", Err: err}, "Volume action failed — check logs"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := feed(t, c.tab.onTab(t, loadedModel(t)), c.msg)

			if st := m.status(); st.Text != sentenceDockerUnreach || st.Level != components.LevelError {
				t.Errorf("status() = %+v, want %q at level Error", st, sentenceDockerUnreach)
			}
			if m.footer.Text() == c.generic {
				t.Errorf("the footer still says %q", c.generic)
			}
		})
	}
}

func TestAnUnknownActionFailureKeepsItsCheckLogs(t *testing.T) {
	tabs := engineTabs()
	boom := errors.New("docker network create failed: Error response from daemon: something nobody probed")
	for _, c := range []struct {
		name    string
		tab     engineTab
		msg     tea.Msg
		generic string
	}{
		{"image action", tabs[0], ImageActionMsg{Action: "pull", ID: "aaa1111", Name: "api:v1", Err: boom}, "Action failed — check logs"},
		{"network create", tabs[1], NetworkActionMsg{Action: "create", Err: boom}, "Network action failed — check logs"},
		{"network prune", tabs[1], NetworkPruneCompleteMsg{Err: boom}, "Network prune failed — check logs"},
		{"volume create", tabs[2], VolumeActionMsg{Action: "create", Err: boom}, "Volume action failed — check logs"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := feed(t, c.tab.onTab(t, loadedModel(t)), c.msg)

			if m.footer.Text() != c.generic || m.footer.Level() != components.LevelError {
				t.Errorf("footer = (%q, level %d), want (%q, LevelError)", m.footer.Text(), m.footer.Level(), c.generic)
			}
			if st := m.status(); st.Level == components.LevelError {
				t.Errorf("status() = %+v, want no engine state for an unknown failure", st)
			}
		})
	}
}

// ── The network inspect ──────────────────────────────────────────────────────

// openedNetworkInspect opens the inspect overlay of the selected network and runs the
// Cmd the view returned for it: the real docker.InspectNetwork against the fake engine.
func openedNetworkInspect(t *testing.T) (Model, NetworkInspectLoadedMsg) {
	t.Helper()
	m := engineTabs()[1].onTab(t, loadedModel(t))
	m, cmd := step(t, m, testutil.Key("enter"))
	if m.networkInspectForm == nil {
		t.Fatal("enter on a network did not open the inspect overlay")
	}
	msg, ok := testutil.MsgOf[NetworkInspectLoadedMsg](cmd)
	if !ok {
		t.Fatal("opening the overlay returned no network inspect read")
	}
	return m, msg
}

func TestNetworkInspectWithTheEngineDownSaysSo(t *testing.T) {
	for _, c := range []struct {
		probe engineProbe
		class docker.Unavailability
	}{
		{probesUnreachable[0], docker.EngineUnreachable},
		{probesPermission[0], docker.EnginePermissionDenied},
		{probesUnreachable[4], docker.EngineUnreachable},
	} {
		t.Run(c.probe.String(), func(t *testing.T) {
			answerWith(t, c.probe, "network", "inspect")
			m, msg := openedNetworkInspect(t)
			var unavailable *docker.UnavailableError
			if !errors.As(msg.Err, &unavailable) || unavailable.Reason != c.class {
				t.Fatalf("the inspect failed with %v, want an unavailable engine of class %d", msg.Err, c.class)
			}
			buf := engineDownLog(t)

			m = feed(t, m, msg)

			if m.networkInspectForm != nil {
				t.Error("the overlay is still open after the inspect failed")
			}
			if st := m.status(); st.Text != c.probe.want || st.Level != components.LevelError {
				t.Errorf("status() = %+v, want %q at level Error", st, c.probe.want)
			}
			if m.footer.Text() == "Failed to inspect network — check logs" {
				t.Error(`the footer says "Failed to inspect network — check logs" for an engine that could not be asked`)
			}
			for _, fragment := range neverOnScreen {
				if strings.Contains(screen(m), fragment) {
					t.Errorf("the screen holds %q", fragment)
				}
			}
			if strings.Contains(buf.String(), "s3cret") {
				t.Errorf("the log holds a credential: %q", buf.String())
			}
		})
	}
}

func TestNetworkInspectWithAnUnknownFailureKeepsCheckLogs(t *testing.T) {
	answerWith(t, trapped[2], "network", "inspect")
	m, msg := openedNetworkInspect(t)
	if _, ok := docker.UnavailableMessage(msg.Err); ok {
		t.Fatalf("the probe reads as an unavailable engine: %v", msg.Err)
	}
	buf := engineDownLog(t)

	m = feed(t, m, msg)

	if m.networkInspectForm != nil {
		t.Error("the overlay is still open after the inspect failed")
	}
	if m.footer.Text() != "Failed to inspect network — check logs" || m.footer.Level() != components.LevelError {
		t.Errorf("footer = (%q, level %d), want (\"Failed to inspect network — check logs\", LevelError)", m.footer.Text(), m.footer.Level())
	}
	if st := m.status(); st.Level == components.LevelError {
		t.Errorf("status() = %+v, want no engine state for an unknown failure", st)
	}
	if logged := buf.String(); !strings.Contains(logged, "***@") || strings.Contains(logged, "s3cret") {
		t.Errorf("the log is %q, want the redacted cause (***@) and no credential", logged)
	}
}

// ── What the lost engine leaves behind (review of OC-24, PR 2) ───────────────

// A classified Images failure takes the list away and says the daemon has not answered
// (`listed` goes back to false): an agent's scan request that comes next then waits for a
// list that answers instead of being told "No image ... on this machine", which this
// view is in no position to say. It is refused with the fixed sentence if the list fails
// again, and served when one answers.
func TestARequestAfterTheEngineWentAwayWaitsForTheNextList(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	probe := probesUnreachable[0]
	answerWith(t, probe, "image", "ls")
	images := engineTabs()[0]

	m := loadedModel(t)
	if !m.listed {
		t.Fatal("setup: the view has not listed yet")
	}
	m = feed(t, m, images.unavailableList(t, docker.EngineUnreachable))
	if m.listed || len(m.images) != 0 {
		t.Fatalf("after the failure listed = %v and %d images, want false and none", m.listed, len(m.images))
	}

	// The request waits, and the list is asked again (the Cmd is not run here).
	m, cmd := step(t, m, ImageScanRequestedMsg{Images: []string{"cache:v2"}, Invocation: "inv-1"})
	if cmd == nil || len(m.pendingRequests) != 1 {
		t.Fatalf("the request was not held: cmd %v, %d pending", cmd != nil, len(m.pendingRequests))
	}

	// The list fails again: refused with the sentence, no engine text.
	m, cmd = step(t, m, images.unavailableList(t, docker.EngineUnreachable))
	var refused *jobs.RefusedMsg
	for _, msg := range testutil.Msgs(cmd) {
		if r, ok := msg.(jobs.RefusedMsg); ok {
			refused = &r
		}
	}
	if refused == nil {
		t.Fatal("the pending request was not refused when the list failed again")
	}
	if want := "Cannot list images: " + probe.want; refused.Reason != want {
		t.Errorf("reason = %q, want %q", refused.Reason, want)
	}
	if len(m.pendingRequests) != 0 {
		t.Errorf("%d requests are still pending after the refusal", len(m.pendingRequests))
	}

	// A request held while the engine is still away is served once a list answers.
	m, _ = step(t, m, ImageScanRequestedMsg{Images: []string{"cache:v2"}, Invocation: "inv-2"})
	if len(m.pendingRequests) != 1 {
		t.Fatalf("%d requests pending, want the one held", len(m.pendingRequests))
	}
	m, _ = step(t, m, ImagesListMsg{Images: imageFixtures()}) // the Cmd (update checks) is not run
	if !m.listed || len(m.pendingRequests) != 0 {
		t.Errorf("listed = %v with %d pending after a list answered, want true and none", m.listed, len(m.pendingRequests))
	}
}

// P with the engine away opens no confirmation: the list that held what could be
// removed is gone, and nothing asks the engine about an empty one.
func TestPruneWithTheEngineAwayOpensNoConfirmation(t *testing.T) {
	probe := probesUnreachable[0]
	tabs := engineTabs()
	for _, tab := range []engineTab{tabs[0], tabs[2]} {
		t.Run(tab.name, func(t *testing.T) {
			// The list and whatever P asks next all fail the same way.
			useEngineNamed(t, probe.tool)
			reply := fakecli.Reply{Exit: probe.exit, Stderr: probe.stderr}
			fakecli.Install(t, fakecli.Scenario{
				{Tool: probe.tool, Args: tab.verb, Prefix: true, Reply: reply},
				{Tool: probe.tool, Args: []string{"ps"}, Prefix: true, Reply: reply},
				{Tool: probe.tool, Args: []string{"inspect"}, Prefix: true, Reply: reply},
			}, probe.tool)
			m := tab.onTab(t, prunableModel(t))
			m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))
			assertEngineDown(t, m, probe.want)

			m, cmd := step(t, m, testutil.Key(keymap.Prune))
			for _, msg := range testutil.Msgs(cmd) {
				m, _ = step(t, m, msg)
			}

			if m.confirmModal != nil {
				t.Errorf("P opened a confirmation (%q) with the engine away", m.pendingAction)
			}
		})
	}
}

// The footer is shared by the tabs and every refresh lists the three: a list that
// succeeds on Networks does not wipe the bilan of a removal made on Images.
func TestANetworksListKeepsTheBilanOfAnotherTab(t *testing.T) {
	report := docker.RemovalReport{{Target: "ddd4444", Status: docker.RemovalRemoved}}
	m := loadedModel(t)
	m.footer.Removal(report) // the timer is not run
	bilan := m.footer.Text()
	if bilan == "" || bilan != report.Summary() {
		t.Fatalf("setup: the footer holds %q, want the bilan %q", bilan, report.Summary())
	}

	m = feed(t, m, NetworksListMsg{Networks: networkFixtures()})

	if m.footer.Text() != bilan {
		t.Errorf("the footer says %q after a networks list, want the bilan %q kept until its timer", m.footer.Text(), bilan)
	}
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if m.footer.Text() != "" {
		t.Errorf("the bilan is still there after its expiry: %q", m.footer.Text())
	}
}

// ctrl+r with the engine away: the load of the empty table is what the footer says
// while it lasts (the dot is drawn and WantsBlink agrees), then the state comes back
// with the list that failed again. WantsBlink is never true for a dot that is not drawn.
func TestARefreshWithTheEngineAwayShowsTheLoadThenTheState(t *testing.T) {
	forEachTab(t, func(t *testing.T, tab engineTab) {
		probe := probesUnreachable[0]
		answerWith(t, probe, tab.verb...)
		m := tab.onTab(t, newTestModel(t))
		m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))
		assertEngineDown(t, m, probe.want)
		if m.WantsBlink() {
			t.Error("WantsBlink is true while the engine state is shown: the dot is not drawn")
		}

		m = feed(t, m, testutil.Key("ctrl+r"))

		if st := m.status(); !st.Activity || !strings.HasPrefix(st.Text, "Loading") {
			t.Errorf("status() = %+v during ctrl+r, want the load with its dot", st)
		}
		if !m.WantsBlink() {
			t.Error("WantsBlink is false while the dot is drawn")
		}

		m = feed(t, m, tab.unavailableList(t, docker.EngineUnreachable))

		assertEngineDown(t, m, probe.want)
		if m.WantsBlink() {
			t.Error("WantsBlink is true again once the engine state is shown")
		}
	})
}
