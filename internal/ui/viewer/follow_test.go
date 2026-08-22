package viewer

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The defect this replaced: `F` handed the terminal to `docker logs -f` through
// tea.ExecProcess, and the only way out of that process is ctrl+c — which the
// suspended TUI never sees, so it killed DevDesk and returned the terminal in
// the child's mode with keys no longer answering.
//
// The test that says it cannot come back is on the *source* interface: a
// Followable that returns an *exec.Cmd no longer compiles. What is left to
// check here is that following happens in the pane.

func TestFollowSuspendsNothing(t *testing.T) {
	m := logModel(t, "line")

	m, cmd := step(t, m, testutil.Key(keymap.Fetch))

	if !m.following {
		t.Fatal("F did not start following")
	}
	if cmd == nil {
		t.Fatal("F issued no command, so nothing is ever re-read")
	}
	// Every message the command produces is one this view handles itself. A
	// tea.ExecProcess would surface as neither a load nor a tick.
	for _, msg := range resolve(t, cmd) {
		switch msg.(type) {
		case DocumentLoadedMsg, followTickMsg:
		default:
			t.Errorf("following produced a %T, want only a re-read and its tick", msg)
		}
	}
}

// The first read is immediate: waiting a whole interval would leave the user
// unsure the key did anything.
func TestFollowReadsImmediately(t *testing.T) {
	m := logModel(t, "line")

	_, cmd := step(t, m, testutil.Key(keymap.Fetch))

	var loaded bool
	for _, msg := range resolve(t, cmd) {
		if _, ok := msg.(DocumentLoadedMsg); ok {
			loaded = true
		}
	}
	if !loaded {
		t.Error("F scheduled a tick but did not read, so the pane sits stale for a whole interval")
	}
}

func TestFollowIsAToggle(t *testing.T) {
	m := feed(t, logModel(t, "line"), testutil.Key(keymap.Fetch))
	if !m.following {
		t.Fatal("F did not start following")
	}

	m, cmd := step(t, m, testutil.Key(keymap.Fetch))

	if m.following {
		t.Error("a second F did not stop following")
	}
	if cmd != nil {
		t.Error("stopping following issued a command, so the loop outlives the stop")
	}
}

// The generation is what actually ends a loop: the tick already in flight
// blocks its whole interval and arrives regardless.
func TestATickFromAStoppedLoopDoesNothing(t *testing.T) {
	m := feed(t, logModel(t, "line"), testutil.Key(keymap.Fetch))
	stale := followTickMsg{gen: m.followGen}

	m = feed(t, m, testutil.Key(keymap.Fetch)) // stop
	_, cmd := step(t, m, stale)

	if cmd != nil {
		t.Error("a tick from the stopped loop kept it running")
	}
}

// Turning follow off and on again before the old tick lands must not leave two
// loops reading at once for the life of the view.
func TestRestartingFollowDoesNotLeaveTwoLoops(t *testing.T) {
	m := feed(t, logModel(t, "line"), testutil.Key(keymap.Fetch))
	stale := followTickMsg{gen: m.followGen}

	m = feed(t, m, testutil.Key(keymap.Fetch)) // stop
	m = feed(t, m, testutil.Key(keymap.Fetch)) // start again

	if _, cmd := step(t, m, stale); cmd != nil {
		t.Error("the old loop's tick was honoured by the new run")
	}
	// The new loop is still alive: its own tick keeps going.
	if _, cmd := step(t, m, followTickMsg{gen: m.followGen}); cmd == nil {
		t.Error("the current loop stopped reading")
	}
}

// A followed document lands at the bottom, where the new lines are. Every other
// arrival lands at the top.
func TestAFollowedDocumentStaysAtTheBottom(t *testing.T) {
	long := strings.Repeat("a line\n", 200)
	m := logModel(t, long)

	if m.textViewport.YOffset != 0 {
		t.Fatalf("a freshly opened document is at offset %d, want the top", m.textViewport.YOffset)
	}

	m = feed(t, m, testutil.Key(keymap.Fetch))
	m = feed(t, m, resolve(t, loadCmd(m.source))...)

	if !m.textViewport.AtBottom() {
		t.Error("a followed document did not land at the bottom, so new lines are off screen")
	}
}

// ...and stopping hands scrolling back: a reload after that is a plain
// "here is the document" again.
func TestStoppingFollowRestoresTheTopOnReload(t *testing.T) {
	long := strings.Repeat("a line\n", 200)
	m := feed(t, logModel(t, long), testutil.Key(keymap.Fetch))
	m = feed(t, m, testutil.Key(keymap.Fetch)) // stop

	m = feed(t, m, resolve(t, loadCmd(m.source))...)

	if m.textViewport.YOffset != 0 {
		t.Errorf("offset = %d after a reload with following off, want the top", m.textViewport.YOffset)
	}
}

// Leaving stops the clock: the router keeps this view, so a loop left running
// would shell out every interval for a document nobody is looking at.
func TestLeavingStopsFollowing(t *testing.T) {
	m := feed(t, logModel(t, "line"), testutil.Key(keymap.Fetch))
	m.OriginView = "containers"

	m, _ = step(t, m, testutil.Key("esc"))

	if m.following {
		t.Error("esc left the follow loop running behind the view")
	}
}

// Rule 128: following is a state, so it is derived on every frame rather than
// posted as a message that would expire in three seconds.
func TestTheFooterSaysItIsFollowing(t *testing.T) {
	m := logModel(t, "line")
	if got := m.status().Text; got != "" {
		t.Errorf("status = %q before following, want nothing", got)
	}

	m = feed(t, m, testutil.Key(keymap.Fetch))

	if got := m.status().Text; !strings.Contains(strings.ToLower(got), "following") {
		t.Errorf("status = %q while following, want it to say so", got)
	}
	if m.status().Spinner {
		t.Error("the follow status carries a spinner; it would blink on every read")
	}
}

// Rule 130: the wording says which way the key goes.
func TestTheFollowShortcutSaysWhichWayItGoes(t *testing.T) {
	m := logModel(t, "line")
	if got := shortcutText(m, keymap.Fetch); !strings.Contains(got, "Follow") {
		t.Errorf("F = %q with following off, want it to offer following", got)
	}

	m = feed(t, m, testutil.Key(keymap.Fetch))

	if got := shortcutText(m, keymap.Fetch); !strings.Contains(got, "Stop") {
		t.Errorf("F = %q while following, want it to offer stopping", got)
	}
}

// A source that cannot be followed must not be put into a state it can never
// leave by a key that does nothing.
func TestFollowOnAPlainSourceIsInert(t *testing.T) {
	m := open(t, fakeSource{name: "notes.md", content: "hello"})

	m, cmd := step(t, m, testutil.Key(keymap.Fetch))

	if m.following || cmd != nil {
		t.Error("F acted on a source that cannot be followed")
	}
}

// ── The pager ────────────────────────────────────────────────────────────────

// A pager that cannot start comes back in milliseconds and looks exactly like
// one the user quit at once, so a silent failure is indistinguishable from `V`
// doing nothing. That is how the broken Windows command line went unnoticed:
// the log line was there, and nobody reads a log to find out why a key did
// nothing.
func TestAFailedPagerSaysSo(t *testing.T) {
	m := logModel(t, "line")

	m, _ = step(t, m, PagerExitMsg{Err: errors.New("exit status 1")})

	if !m.footer.IsSet() {
		t.Fatal("a pager that failed said nothing, so V reads as a key that does nothing")
	}
	if strings.Contains(m.footer.Text(), "exit status") {
		t.Errorf("footer = %q leaks the raw error; Rule 128 wants a short message plus a log", m.footer.Text())
	}
}

// A pager the user simply quit is not a failure, and must not be reported as one.
func TestAPagerThatSucceededSaysNothing(t *testing.T) {
	m := logModel(t, "line")

	m, _ = step(t, m, PagerExitMsg{})

	if m.footer.IsSet() {
		t.Errorf("footer = %q after a clean pager exit, want silence", m.footer.Text())
	}
}

func shortcutText(m Model, key string) string {
	for _, s := range m.GetShortcuts() {
		if s.Key == key {
			return s.Description
		}
	}
	return ""
}
