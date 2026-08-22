package configuration

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// savedMsg returns the ConfigSavedMsg a command carries, if any.
func savedMsg(t *testing.T, m Model, key string) (Model, *ConfigSavedMsg) {
	t.Helper()
	updated, cmd := m.Update(testutil.Key(key))
	m = updated.(Model)
	for _, msg := range testutil.Msgs(cmd) {
		if s, ok := msg.(ConfigSavedMsg); ok {
			return m, &s
		}
	}
	return m, nil
}

// TestTheForgeIsTheFirstFieldOfTheConnectionGroup — everything below it
// reconfigures from it, so it has to be above them for the user to see it
// happen.
func TestTheForgeIsTheFirstFieldOfTheConnectionGroup(t *testing.T) {
	m := newModel(t)
	for _, s := range m.sections {
		if s.Title != config.ForgeGitLab {
			continue
		}
		if s.Fields[0].Label != forgeLabel {
			t.Errorf("the forge tab opens on %q, want %q", s.Fields[0].Label, forgeLabel)
		}
		if s.Fields[1].Label != "URL" {
			t.Errorf("the field under the forge is %q, want URL", s.Fields[1].Label)
		}
		return
	}
	t.Fatalf("no tab titled %q", config.ForgeGitLab)
}

// TestCyclingTheForgeWritesNothingUntilBlur — the change closes the session, so
// cycling through the list would close it once per keypress, including on the
// way back to where it started. Same call as the secret backend's.
func TestCyclingTheForgeWritesNothingUntilBlur(t *testing.T) {
	m := focusOn(t, newModel(t), forgeLabel)

	m, saved := savedMsg(t, m, "right")
	if saved != nil {
		t.Error("cycling the forge saved immediately")
	}
	if m.config.Forge.Type != config.ForgeGitHub {
		t.Errorf("Type = %q after one ←→, want the cycled value held in memory", m.config.Forge.Type)
	}
}

// TestLeavingTheForgeClosesTheSessionAndSaysSo is the decision §3.6 settled:
// the change is not forbidden and not confirmed — the user is told what it did.
func TestLeavingTheForgeClosesTheSessionAndSaysSo(t *testing.T) {
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := focusOn(t, newModel(t), forgeLabel)
	m, _ = savedMsg(t, m, "right")

	m, saved := savedMsg(t, m, "down")
	if saved == nil {
		t.Fatal("leaving the forge saved nothing")
	}
	if !saved.ForgeChanged {
		t.Error("ForgeChanged is false, so the router would keep a session on the old platform")
	}
	if !strings.Contains(m.footer.Text(), "GitHub") {
		t.Errorf("footer = %q, want it to name the new platform", m.footer.Text())
	}
	if !strings.Contains(m.footer.Text(), ":"+string(command.ViewGitlabAuth)) {
		t.Errorf("footer = %q, want it to say where to sign in again", m.footer.Text())
	}
}

// Cycling back to where it started is not a change, and must not close a
// working session.
func TestReturningToTheSameForgeChangesNothing(t *testing.T) {
	m := focusOn(t, newModel(t), forgeLabel)

	m, _ = savedMsg(t, m, "right")
	m, _ = savedMsg(t, m, "left")
	m, saved := savedMsg(t, m, "down")

	if saved != nil && saved.ForgeChanged {
		t.Error("returning to the starting forge was reported as a change")
	}
	if m.footer.IsSet() {
		t.Errorf("footer = %q for an unchanged forge", m.footer.Text())
	}
}

// TestChangingTheForgeRebuildsTheFieldTable — the tab's title, its icon, the
// URL example and two labels all come from the platform, and the router keeps
// this view on a save, so nothing else would rebuild it.
func TestChangingTheForgeRebuildsTheFieldTable(t *testing.T) {
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := focusOn(t, newModel(t), forgeLabel)
	m, _ = savedMsg(t, m, "right")
	m, _ = savedMsg(t, m, "down")

	var forgeTab *section
	for i, s := range m.sections {
		if s.Title == config.ForgeGitHub {
			forgeTab = &m.sections[i]
		}
	}
	if forgeTab == nil {
		t.Fatalf("no tab titled %q after the change; titles are %v", config.ForgeGitHub, titles(m))
	}
	for _, f := range forgeTab.Fields {
		if strings.Contains(f.Label, "group") {
			t.Errorf("a label still says group: %q", f.Label)
		}
	}
}

// TestSwitchingToGitHubDropsAVisibilityItDoesNotHave is the correctness half:
// GitHub.com has no `internal`, so a context carrying it would keep a value the
// server refuses — and the cycle field would open on a value absent from its
// own list.
func TestSwitchingToGitHubDropsAVisibilityItDoesNotHave(t *testing.T) {
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := newModel(t)
	m.config.Forge.DefaultVisibility = "internal"
	m = focusOn(t, m, forgeLabel)

	m, _ = savedMsg(t, m, "right")
	m, _ = savedMsg(t, m, "down")

	if m.config.Forge.DefaultVisibility != "private" {
		t.Errorf("DefaultVisibility = %q after switching to GitHub, want it coerced to the most private",
			m.config.Forge.DefaultVisibility)
	}
	if !forge.ShapeFor(config.ForgeGitHub).AllowsVisibility(m.config.Forge.DefaultVisibility) {
		t.Errorf("DefaultVisibility = %q, which GitHub refuses", m.config.Forge.DefaultVisibility)
	}
}

// TestAKnownHostSetsTheForge — the two public instances are unambiguous, so
// typing one is enough.
func TestAKnownHostSetsTheForge(t *testing.T) {
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := focusOn(t, newModel(t), "URL")
	m.input.SetValue("https://github.com")

	m, _ = savedMsg(t, m, "down")
	if m.config.Forge.Type != config.ForgeGitHub {
		t.Errorf("Type = %q after typing github.com, want it detected", m.config.Forge.Type)
	}
}

// TestAnUnknownHostLeavesTheForgeAlone — self-hosted is the case that matters,
// and `git.acme.test` could be either.
func TestAnUnknownHostLeavesTheForgeAlone(t *testing.T) {
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := focusOn(t, newModel(t), "URL")
	m.input.SetValue("https://git.acme.test")

	m, _ = savedMsg(t, m, "down")
	if m.config.Forge.Type != config.ForgeGitLab {
		t.Errorf("Type = %q, want the configured value kept for an unrecognised host", m.config.Forge.Type)
	}
}

// TestDetectionLeavesAnExplicitChoiceAlone is the dirty flag, and the whole
// difference between helpful and possessive: once the user has said which
// platform it is, typing a URL must not contradict them.
func TestDetectionLeavesAnExplicitChoiceAlone(t *testing.T) {
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	// The user cycles the forge to GitHub, then types a GitLab.com URL.
	m := focusOn(t, newModel(t), forgeLabel)
	m, _ = savedMsg(t, m, "right")
	m, _ = savedMsg(t, m, "down")

	m = focusOn(t, m, "URL")
	m.input.SetValue("https://gitlab.com")
	m, _ = savedMsg(t, m, "down")

	if m.config.Forge.Type != config.ForgeGitHub {
		t.Errorf("Type = %q, want the explicit choice kept against the URL's host", m.config.Forge.Type)
	}
}

// titles lists the tab titles, for a failure message that says what is there.
func titles(m Model) []string {
	out := make([]string, 0, len(m.sections))
	for _, s := range m.sections {
		out = append(out, s.Title)
	}
	return out
}
