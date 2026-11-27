package containers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-08a criterion 6, at the level of a view: the containers view receives the
// bytes of a recorded fixture through the code it ships (fetchContainers, then
// docker.ListContainers, then the engine process), and shows its rows.
//
// The engine is the test binary answering as docker (fakecli.Install with
// fakecli.Fixtures); this package's TestMain already calls fakecli.AsTool. The
// fixture is the synthetic "ps-all" of internal/enginefixture/testdata, whose
// argv is the one DevDesk builds from the template of the engine.

func psFixture(t *testing.T) enginefixture.Fixture {
	t.Helper()
	fixtures, err := enginefixture.LoadFS(os.DirFS(filepath.Join("..", "..", "enginefixture", "testdata")), "synthetic")
	if err != nil {
		t.Fatalf("loading the synthetic fixtures: %v", err)
	}
	for _, f := range fixtures {
		if f.Scenario == "ps-all" && f.Meta.Tool == enginefixture.Docker {
			return f
		}
	}
	t.Fatal("no synthetic ps-all fixture for docker")
	return enginefixture.Fixture{}
}

func TestTheViewShowsTheRowsOfARecordedPS(t *testing.T) {
	f := psFixture(t)
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(engine.Docker))
	fake := fakecli.Install(t, fakecli.Fixtures(t, f), "docker")

	// The Cmd the view returns from Init runs docker.ListContainers(true).
	msg := fetchContainers()()

	list, ok := msg.(ContainersListMsg)
	if !ok {
		t.Fatalf("fetchContainers returned %T, want ContainersListMsg", msg)
	}
	if list.Err != nil {
		t.Fatalf("ContainersListMsg.Err = %v, want the recorded list", list.Err)
	}
	if len(list.Containers) != 3 {
		t.Fatalf("%d containers, want the 3 recorded rows: %+v", len(list.Containers), list.Containers)
	}
	if inv := fake.Invocations(); len(inv) == 0 || !inv[0].Matched {
		t.Errorf("journal = %+v, want the fake engine asked once with the recorded argv", inv)
	}

	m := allStates(t, feed(t, newTestModel(t), list))
	view := ansi.Strip(m.Render())

	rows := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "ddm-") {
			rows++
		}
	}
	if rows != 3 {
		t.Errorf("the view shows %d container rows, want 3:\n%s", rows, view)
	}
	for _, name := range []string{"ddm-web", "ddm-db", "ddm-cache"} {
		if !strings.Contains(view, name) {
			t.Errorf("the view does not show %s:\n%s", name, view)
		}
	}
	// Plain text in the cells (Rule 122): the strip left no escape sequence.
	if strings.Contains(view, "\x1b") {
		t.Errorf("the stripped view still holds an escape sequence: %q", view)
	}
}
