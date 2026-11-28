package containers

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-22 (DEVDESK-820), the follow-ups of the review, on the containers view.

// drainAll hands every message of a command back to Update, the list refreshes
// included (settle leaves them out so they do not replace the list a test drives);
// only the footer's timers are skipped. It is what the runtime does.
func drainAll(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		if _, timer := msg.(sharedcomponents.ClearFooterMsg); !timer {
			m = feed(t, m, msg)
		}
	}
	return m
}

// The bilan of P stays through the refresh that follows it, through the tick, and
// through the next refresh: only its own timer ends it. A failed load still replaces it.
func TestTheBilanOfAPruneSurvivesTheRefreshThatFollows(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		installEngine(t, shape, rmOK(idAPI, idZombie, idFresh)...)
		m := preview(t, feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()}))
		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
		m = drainAll(t, m, cmd)
		want := m.footer.Text()
		if !strings.Contains(want, "3 removed") {
			t.Fatalf("footer = %q, want the bilan", want)
		}

		m = feed(t, m, ContainersListMsg{Containers: nil}, RefreshTickMsg{}, ContainersListMsg{Containers: nil})

		if m.footer.Text() != want {
			t.Errorf("footer = %q after the refresh, want the bilan %q to stay", m.footer.Text(), want)
		}
		bad := feed(t, m, ContainersListMsg{Err: errors.New("daemon down")})
		if bad.footer.Level() != sharedcomponents.LevelError {
			t.Errorf("a failed load left %q at %v, want its Error to replace the bilan", bad.footer.Text(), bad.footer.Level())
		}
	})
}

// D follows the same rule as P.
func TestTheBilanOfADeleteSurvivesTheRefreshThatFollows(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		installEngine(t, shape, rmRefused(idAPI, goneReply(shape, idAPI)))
		m := selectNamed(t, loadedModel(t), "api")
		m, _ = step(t, m, testutil.Key(keymap.Delete))
		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
		m = drainAll(t, m, cmd)
		if !strings.Contains(m.footer.Text(), "already gone") {
			t.Fatalf("footer = %q, want the bilan", m.footer.Text())
		}

		m = feed(t, m, ContainersListMsg{Containers: containerFixtures()}, RefreshTickMsg{})

		if !strings.Contains(m.footer.Text(), "already gone") {
			t.Errorf("footer = %q after the refresh, want the bilan to stay", m.footer.Text())
		}
	})
}

// A second P while the removal of the first runs is refused with the reason.
func TestAPruneIsRefusedWhileAnotherRuns(t *testing.T) {
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: stoppedFixtures()})
	m.pruning = true

	m, _ = step(t, m, testutil.Key(keymap.Prune))

	if m.confirmModal != nil {
		t.Error("P opened a confirmation while a removal is running")
	}
	footerIs(t, m, sharedcomponents.LevelWarning)
}

// A container name is engine text: an escape sequence in it is not drawn.
func TestTheConfirmationNeverDrawsAControlSequenceOfAName(t *testing.T) {
	list := []docker.Container{{ID: idAPI, Name: "api\x1b[31m\u202e", State: "exited"}}
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: list})

	m, _ = step(t, m, testutil.Key(keymap.Prune))

	if m.confirmModal == nil {
		t.Fatal("P opened no confirmation")
	}
	view := m.confirmModal.View()
	if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\u202e") {
		t.Errorf("the confirmation draws the name as the engine wrote it: %q", view)
	}
}
