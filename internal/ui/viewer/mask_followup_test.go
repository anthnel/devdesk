package viewer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The footer message lasts three seconds; the header keeps the state and the
// count for as long as the document is open.
func TestTheHeaderKeepsTheStateAndTheCount(t *testing.T) {
	m := open(t, newMaskable(maskFixture))
	if got, _ := envHeader(m, "Env"); got != "masked (1)" {
		t.Errorf("header Env = %q, want masked (1)", got)
	}

	m = feed(t, m, testutil.Key("m"))
	if got, _ := envHeader(m, "Env"); got != "revealed (1)" {
		t.Errorf("header Env = %q after m, want revealed (1)", got)
	}

	// A reload reads the count again and keeps it.
	m, cmd := step(t, m, testutil.Key("ctrl+r"))
	m = feed(t, m, resolve(t, cmd)...)
	if got, _ := envHeader(m, "Env"); got != "revealed (1)" {
		t.Errorf("header Env = %q after a reload, want revealed (1)", got)
	}

	none := open(t, newMaskable(`{"Env":["LOG_LEVEL=info"]}`))
	if got, _ := envHeader(none, "Env"); got != "masked (0)" {
		t.Errorf("header Env = %q with nothing to hide, want masked (0)", got)
	}
}

// A query typed while the values were shown may be the secret itself: masking
// again must not leave it in the search bar.
func TestMaskingAgainDropsTheSearch(t *testing.T) {
	m := inTextDisplay(t, open(t, newMaskable(maskFixture)))
	m = feed(t, m, testutil.Key("m"))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("canary")...)
	m = feed(t, m, testutil.Key("enter"))
	if m.matchedLines != 1 {
		t.Fatalf("the search matched %d lines while revealed, want 1", m.matchedLines)
	}

	m = feed(t, m, testutil.Key("m"))

	if q := m.bar.SearchQuery(); q != "" {
		t.Errorf("the search bar still holds %q after masking again", q)
	}
	if m.bar.IsVisible() {
		t.Error("the search bar is still showing after masking again")
	}
	if m.matchedLines != len(m.lines) {
		t.Errorf("matchedLines = %d, want all %d lines", m.matchedLines, len(m.lines))
	}
	if strings.Contains(shownText(m), "canary") {
		t.Errorf("the secret is on screen:\n%s", shownText(m))
	}
}

// Revealing keeps a query typed while masked: it is what the reader typed and
// shows nothing of the secret.
func TestRevealingKeepsTheSearch(t *testing.T) {
	m := inTextDisplay(t, open(t, newMaskable(maskFixture)))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("LOG")...)
	m = feed(t, m, testutil.Key("enter"))

	m = feed(t, m, testutil.Key("m"))

	if m.bar.SearchQuery() != "LOG" {
		t.Errorf("revealing dropped the search: %q", m.bar.SearchQuery())
	}
}

func TestTheHelpSaysWhatMRevealsAndWhereMasksStop(t *testing.T) {
	help := jsonModel(t).GetHelpContent()

	var body string
	for _, s := range help.Sections {
		if s.Title == "Masked values" {
			body = s.Body
		}
	}
	for _, want := range []string{"Config.Env", "Config.CreateCommand", "Podman"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Masked values help does not mention %q", want)
		}
	}
}
