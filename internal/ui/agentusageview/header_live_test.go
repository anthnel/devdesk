package agentusageview

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Live updates, step 6 (DEVDESK-45, DEVDESK-80): the "Updates" header slot
// (live while the chain runs, paused once it stopped) and the help text.

const updatesKey = "Updates"

// updatesValue returns the value of the Updates header entry, and whether the
// entry exists.
func updatesValue(m Model) (string, bool) {
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == updatesKey {
			return h.Value, true
		}
	}
	return "", false
}

func wantUpdates(t *testing.T, name string, m Model, want string) {
	t.Helper()
	got, ok := updatesValue(m)
	if !ok {
		t.Errorf("%s: no %q header entry, want %q", name, updatesKey, want)
		return
	}
	if got != want {
		t.Errorf("%s: Updates = %q, want %q", name, got, want)
	}
}

// settledModel is a fixture session quiet for 10 minutes, its first read answered.
func settledModel(t *testing.T) (Model, string) {
	t.Helper()
	parent := copyFixture(t)
	finishTester(t, parent)
	ageAll(t, parent, 10*time.Minute)
	m := withSessions(t, parent)
	m, cmd := opened(t, m, "right")
	m, _ = answered(t, m, firstDelta(t, cmd))
	return m, parent
}

func TestHeaderSaysLiveWhileTheChainRuns(t *testing.T) {
	m, _, cmd := openedFixture(t)
	wantUpdates(t, "just opened, first read in flight", m, "live")

	m, msgs := answered(t, m, firstDelta(t, cmd))
	if len(ticksOf(msgs)) != 1 {
		t.Fatalf("precondition: an active session schedules a tick, got %+v", msgs)
	}
	wantUpdates(t, "after the answer of an active session", m, "live")

	m, _ = pollTick(t, m, m.agentGen) // a poll read is now in flight
	wantUpdates(t, "poll read in flight", m, "live")
}

func TestHeaderSaysPausedOnceTheSessionSettled(t *testing.T) {
	m, _ := settledModel(t)
	wantUpdates(t, "settled", m, "paused")
	for _, key := range []string{"Agents", "Tokens"} {
		found := false
		for _, h := range m.GetHeaderInfo("") {
			found = found || h.Key == key
		}
		if !found {
			t.Errorf("the %s entry must stay beside Updates", key)
		}
	}
}

func TestHeaderSaysPausedWhenTheTranscriptIsGone(t *testing.T) {
	m, parent := atFixture(t)
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("precondition: one read, got %+v", msgs)
	}
	m, _ = answered(t, m, reads[0])
	wantUpdates(t, "after ErrNotExist", m, "paused")
}

func TestHeaderStaysLiveWhenAReadFailsForAnotherReason(t *testing.T) {
	m, _ := atFixture(t)
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("precondition: one read, got %+v", msgs)
	}
	reads[0].err = os.ErrPermission // retried by the chain: still live
	m, _ = answered(t, m, reads[0])
	wantUpdates(t, "after a transient failure", m, "live")
}

func TestHeaderHasNoUpdatesEntryOutsideAnOpenSession(t *testing.T) {
	states := map[string]Model{
		"sessions loading": newModel(t),
		"sessions listed":  withSessions(t, "/p/a.jsonl"),
	}
	back, _ := atFixture(t)
	back, _ = opened(t, back, "esc")
	states["back from a session"] = back
	for name, m := range states {
		if v, ok := updatesValue(m); ok {
			t.Errorf("%s: unexpected Updates entry %q at the sessions level", name, v)
		}
	}
}

func TestCtrlRBringsTheHeaderBackToLiveWhenTheSessionResumed(t *testing.T) {
	m, parent := settledModel(t)
	wantUpdates(t, "precondition", m, "paused")

	appendLine(t, parent, usageLine("msg_wake", "model-alpha", 1, 2))
	m, cmd := opened(t, m, "ctrl+r")
	m, _ = answered(t, m, firstDelta(t, cmd))
	// The fixture files were aged, but the appended line is fresh.
	wantUpdates(t, "ctrl+r on a session written to again", m, "live")
}

func TestCtrlRKeepsTheHeaderPausedWhenTheSessionIsStillQuiet(t *testing.T) {
	m, _ := settledModel(t)
	m, cmd := opened(t, m, "ctrl+r")
	m, _ = answered(t, m, firstDelta(t, cmd))
	wantUpdates(t, "ctrl+r on a quiet session", m, "paused")
}

func TestHeaderGoesLiveAgainWhenTheViewIsResumed(t *testing.T) {
	m, _ := settledModel(t)
	m, _ = resumed(t, m) // the chain restarts and a read is in flight
	wantUpdates(t, "resumed", m, "live")
}

func TestShortcutKeysDoNotChangeBetweenLivePausedLoadingAndError(t *testing.T) {
	live, _ := atFixture(t)
	paused, _ := settledModel(t)
	loading, _, _ := openedFixture(t)
	failed, _, _ := openedFixture(t)
	failed = feed(t, failed, usageDeltaMsg{gen: failed.agentGen, err: os.ErrNotExist})

	want := strings.Join(testutil.ShortcutKeys(live.GetShortcuts()), ",")
	for name, m := range map[string]Model{"paused": paused, "loading": loading, "error": failed} {
		if got := strings.Join(testutil.ShortcutKeys(m.GetShortcuts()), ","); got != want {
			t.Errorf("%s: keys %s, want %s", name, got, want)
		}
		if !testutil.ShortcutEnabled(m.GetShortcuts(), "ctrl+r") {
			t.Errorf("%s: ctrl+r must stay enabled", name)
		}
	}
	for _, sc := range live.GetShortcuts() {
		if sc.Key == "ctrl+r" && sc.Description != "Refresh" {
			t.Errorf("ctrl+r description = %q, want Refresh", sc.Description)
		}
	}
}

func TestHelpDescribesLiveUpdates(t *testing.T) {
	c := New(nil).GetHelpContent()

	var body string
	for _, s := range c.Sections {
		if s.Title == "Live updates" {
			body = strings.ToLower(s.Body)
		}
	}
	if body == "" {
		t.Fatal(`help needs a section titled "Live updates"`)
	}
	for _, want := range []string{"2 s", "5 min", "ctrl+r", "paused", "live"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Live updates section should mention %q:\n%s", want, body)
		}
	}

	found := false
	for _, b := range c.KeyBindings {
		if b.Key == "ctrl+r" {
			found = true
			if !strings.Contains(b.Description, "Re-read the whole session from the start") {
				t.Errorf("ctrl+r help = %q, want it to say \"Re-read the whole session from the start\"", b.Description)
			}
		}
	}
	if !found {
		t.Error("help lacks ctrl+r")
	}
}
