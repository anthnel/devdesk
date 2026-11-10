package ociresources

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-153, criterion 2bis: a group's discovery is told by the row's dot,
// and the Members cell is the static word, never a spinner frame (Rule 122).

const membersCol = 6

func TestRefreshingGroupShowsTheDotAndAStaticMembersCell(t *testing.T) {
	m := feed(t, registriesTab(t), testutil.Key("ctrl+r"))

	if got := m.registryTable.Table().Rows()[0][membersCol]; got != "refreshing" {
		t.Errorf("Members cell = %q, want the static text %q", got, "refreshing")
	}
	for _, on := range []bool{true, false} {
		m = feed(t, m, jobs.FrameMsg{Blink: on})
		line := renderedRow(t, m.registryTable.View(), "prod")
		if want := indicator.Glyph(indicator.Running, on); !strings.Contains(line, want) {
			t.Errorf("blink=%v: the refreshing group row lacks %q:\n%s", on, want, line)
		}
		if !strings.Contains(line, "refreshing") {
			t.Errorf("blink=%v: the row lost its refreshing text:\n%s", on, line)
		}
		if hasSpinnerFrame(line) {
			t.Errorf("blink=%v: the row still carries a spinner frame:\n%s", on, line)
		}
		if other := renderedRow(t, m.registryTable.View(), "hub"); hasAnyActivityGlyph(other) {
			t.Errorf("blink=%v: the other registry shows a dot:\n%s", on, other)
		}
	}
}

func TestAFinishedDiscoveryPutsTheDotOutAndTheCountBack(t *testing.T) {
	m := feed(t, registriesTab(t), testutil.Key("ctrl+r"), jobs.FrameMsg{Blink: true})
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: "registry.example.com", Slug: "prod"})
	m = feed(t, m, jobs.FrameMsg{Blink: true})

	line := renderedRow(t, m.registryTable.View(), "prod")
	if hasAnyActivityGlyph(line) {
		t.Errorf("the dot survived the discovery:\n%s", line)
	}
	got := m.registryTable.Table().Rows()[0][membersCol]
	if strings.Contains(got, "refreshing") {
		t.Errorf("Members cell = %q, still refreshing", got)
	}
	if !strings.Contains(got, " · ") {
		t.Errorf("Members cell = %q, want the count back", got)
	}
	if m.WantsBlink() {
		t.Error("WantsBlink stays true after the discovery ended")
	}
}

func TestAFailedDiscoveryAlsoPutsTheDotOut(t *testing.T) {
	m := feed(t, registriesTab(t), testutil.Key("ctrl+r"))
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: "registry.example.com", Slug: "prod", Err: errUnreachable{}})
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if line := renderedRow(t, m.registryTable.View(), "prod"); hasAnyActivityGlyph(line) {
		t.Errorf("the dot survived a failed discovery:\n%s", line)
	}
	if m.WantsBlink() {
		t.Error("WantsBlink stays true after a failed discovery")
	}
}

type errUnreachable struct{}

func (errUnreachable) Error() string { return "unreachable" }

func TestWantsBlinkFollowsAGroupRefresh(t *testing.T) {
	m := registriesTab(t)
	if m.WantsBlink() {
		t.Fatal("WantsBlink is true at rest")
	}
	m = feed(t, m, testutil.Key("ctrl+r"))
	if !m.WantsBlink() {
		t.Error("WantsBlink is false while a group is refreshing")
	}
}

// The periodic refresh is not an operation: it must not make anything blink.
func TestARefreshTickAloneDoesNotWantABlink(t *testing.T) {
	m := feed(t, registriesTab(t), RefreshTickMsg(time.Now()))
	if m.WantsBlink() {
		t.Error("a RefreshTickMsg made WantsBlink true")
	}
	if line := renderedRow(t, m.registryTable.View(), "prod"); hasAnyActivityGlyph(line) {
		t.Errorf("a RefreshTickMsg lit a dot:\n%s", line)
	}
}

// The Cmd that starts a discovery is the login-status probe and the discovery
// itself; the third entry used to be spinner.Tick. Children are counted, never
// run: the discovery is a network call.
func TestRefreshRegistriesSchedulesNoSpinnerTick(t *testing.T) {
	m := registriesTab(t)
	_, cmd := m.refreshRegistries()
	if cmd == nil {
		t.Fatal("refreshRegistries returned no Cmd")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("refreshRegistries returned %T, want a batch", cmd())
	}
	if len(batch) != 2 {
		t.Errorf("the batch holds %d Cmds, want 2 (login status, discovery) and no spinner.Tick", len(batch))
	}
}
