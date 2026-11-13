package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-307 (IS-09), step 2: at level 1, left goes back to the search form.

// c.4: left returns to the form with the typed name and the selection kept,
// and the abandoned lot's late answers are ignored.
func TestLeftAtLevelOneGoesBackToTheForm(t *testing.T) {
	m := drillRows(t)
	b := m.registryBrowser
	old, selected := b.gen, checkedKeys(m)

	m, cmd := step(t, m, testutil.Key("left"))

	b = m.registryBrowser
	if b.state != browserStateInput {
		t.Fatalf("state = %d after left, want the form (browserStateInput)", b.state)
	}
	if cmd != nil {
		t.Errorf("Cmd = %T, want nil", testutil.Msg(cmd))
	}
	if got := b.repoInput.Value(); got != "nginx" {
		t.Errorf("name = %q, want it kept", got)
	}
	if got := checkedKeys(m); !sameStrings(got, selected) {
		t.Errorf("selection = %v, want %v", got, selected)
	}
	if b.gen == old {
		t.Error("gen did not change: the lot was not abandoned")
	}
	if b.AddImageResults(ImageSearchResultMsg{Gen: old, EntryKey: "prod"}) {
		t.Error("a late answer to the abandoned lot was accepted")
	}
}

// Regression: esc keeps its behaviour, it also goes back to the form with the
// name and the selection kept.
func TestEscAtLevelOneStillGoesBackToTheForm(t *testing.T) {
	m := drillRows(t)
	selected := checkedKeys(m)

	m, cmd := step(t, m, testutil.Key("esc"))

	b := m.registryBrowser
	if b.state != browserStateInput {
		t.Fatalf("state = %d after esc, want the form", b.state)
	}
	if cmd != nil {
		t.Errorf("Cmd = %T, want nil", testutil.Msg(cmd))
	}
	if got := b.repoInput.Value(); got != "nginx" {
		t.Errorf("name = %q, want it kept", got)
	}
	if got := checkedKeys(m); !sameStrings(got, selected) {
		t.Errorf("selection = %v, want %v", got, selected)
	}
}

// c.5: while the level 1 filter takes text, left belongs to the text field and
// esc closes the bar first.
func TestLeftWhileTypingTheLevelOneFilterStaysInTheFilter(t *testing.T) {
	m := drillRows(t)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("ngi")...)
	m = feed(t, m, testutil.Key("left"))

	b := m.registryBrowser
	if b.state != browserStateImages || !b.searchTable.InEditMode() {
		t.Fatalf("state %d, InEditMode %v; want level 1 with the bar still taking text",
			b.state, b.searchTable.InEditMode())
	}

	m = feed(t, m, testutil.Key("esc"))
	b = m.registryBrowser
	if b.state != browserStateImages || b.searchTable.InEditMode() {
		t.Errorf("after esc: state %d, InEditMode %v; want level 1 with the bar closed",
			b.state, b.searchTable.InEditMode())
	}
}

// H3: with a confirmed filter query (bar closed), left goes straight back to
// the form, name and selection kept; esc keeps its order and clears the query
// first.
func TestLeftAtLevelOneWithAConfirmedQueryGoesStraightBack(t *testing.T) {
	confirmed := func(t *testing.T) (Model, []string) {
		m := drillRows(t)
		selected := checkedKeys(m)
		m = feed(t, m, testutil.Key("/"))
		m = feed(t, m, testutil.Type("ngi")...)
		m = feed(t, m, testutil.Key("enter"))
		if q := m.registryBrowser.searchTable.FilterBar().SearchQuery(); q != "ngi" || m.registryBrowser.searchTable.InEditMode() {
			t.Fatalf("query = %q, InEditMode %v; want a confirmed ngi", q, m.registryBrowser.searchTable.InEditMode())
		}
		return m, selected
	}

	t.Run("left", func(t *testing.T) {
		m, selected := confirmed(t)
		m, cmd := step(t, m, testutil.Key("left"))
		b := m.registryBrowser
		if b.state != browserStateInput {
			t.Fatalf("state = %d after left, want the form", b.state)
		}
		if cmd != nil {
			t.Errorf("Cmd = %T, want nil", testutil.Msg(cmd))
		}
		if got := b.repoInput.Value(); got != "nginx" {
			t.Errorf("name = %q, want it kept", got)
		}
		if got := checkedKeys(m); !sameStrings(got, selected) {
			t.Errorf("selection = %v, want %v", got, selected)
		}
	})

	t.Run("esc clears the query first", func(t *testing.T) {
		m, _ := confirmed(t)
		m = feed(t, m, testutil.Key("esc"))
		b := m.registryBrowser
		if b.state != browserStateImages {
			t.Fatalf("state = %d after the first esc, want level 1", b.state)
		}
		if q := b.searchTable.FilterBar().SearchQuery(); q != "" {
			t.Errorf("query = %q, want it cleared", q)
		}
		m = feed(t, m, testutil.Key("esc"))
		if m.registryBrowser.state != browserStateInput {
			t.Errorf("state = %d after the second esc, want the form", m.registryBrowser.state)
		}
	})
}
