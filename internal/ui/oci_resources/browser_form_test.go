package ociresources

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The browser form's buttons (DEVDESK-300, IS-11, step 2). formButtons() is the
// single source for the keyboard, the rendering and the header: these tests pin
// its content, the field indices derived from it and what enter does on each.

func labelsOf(bs []formButton) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.label)
	}
	return out
}

func actionsOf(bs []formButton) []formAction {
	out := make([]formAction, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.action)
	}
	return out
}

// downTo presses down until the focus reaches field, failing if it never does.
func downTo(t *testing.T, m Model, field int) Model {
	t.Helper()
	for i := 0; i < 64 && m.registryBrowser.focusedField != field; i++ {
		m = feed(t, m, testutil.Key("down"))
	}
	if got := m.registryBrowser.focusedField; got != field {
		t.Fatalf("could not focus field %d, stuck on %d", field, got)
	}
	return m
}

func TestFormButtonsDependOnTheSearcher(t *testing.T) {
	for _, tc := range []struct {
		name        string
		searcher    bool
		wantLabels  []string
		wantActions []formAction
	}{
		{"no Searcher", false, []string{"Browse Tags"}, []formAction{actionSearch}},
		{"with a Searcher", true, []string{"Search", "Open Tags"}, []formAction{actionSearch, actionOpenTags}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.searcher {
				withImageSearcher(t, &fakeSearcher{})
			} else {
				withImageSearcher(t, nil)
			}
			bs := browsingModel(t).registryBrowser.formButtons()
			if got := labelsOf(bs); !sameStrings(got, tc.wantLabels) {
				t.Errorf("labels = %v, want %v", got, tc.wantLabels)
			}
			got, want := actionsOf(bs), tc.wantActions
			if len(got) != len(want) {
				t.Fatalf("actions = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("action %d = %d, want %d", i, got[i], want[i])
				}
			}
		})
	}
}

// Repository = 0, checkboxes (group rows included) = 1..n, then the buttons.
func TestFormFieldIndices(t *testing.T) {
	for _, tc := range []struct {
		name     string
		model    func(*testing.T) Model
		searcher bool
		// wantButtons is the number of buttons the form must show.
		wantButtons int
	}{
		{"flat, no Searcher", browsingModel, false, 1},
		{"flat, Searcher", browsingModel, true, 2},
		{"grouped, no Searcher", groupedModel, false, 1},
		{"grouped, Searcher", groupedModel, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.searcher {
				withImageSearcher(t, &fakeSearcher{})
			} else {
				withImageSearcher(t, nil)
			}
			b := tc.model(t).registryBrowser
			n := len(b.rows)
			if b.brFieldReg(n-1) != n {
				t.Errorf("last checkbox = %d, want %d", b.brFieldReg(n-1), n)
			}
			buttons := len(b.formButtons())
			if buttons != tc.wantButtons {
				t.Fatalf("%d buttons, want %d", buttons, tc.wantButtons)
			}
			for i := 0; i < buttons; i++ {
				if got, want := b.brFieldButton(i), n+1+i; got != want {
					t.Errorf("brFieldButton(%d) = %d, want %d", i, got, want)
				}
			}
			if got, want := b.brFieldLast(), n+buttons; got != want {
				t.Errorf("brFieldLast() = %d, want %d", got, want)
			}
		})
	}
}

func TestWithASearcherTheFormWalksToSearchThenOpenTags(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := browsingModel(t)
	b := m.registryBrowser

	m = downTo(t, m, b.brFieldReg(len(b.rows)-1))
	m = feed(t, m, testutil.Key("down"))
	if b.focusedField != b.brFieldButton(0) {
		t.Fatalf("focus = %d, want the Search button %d", b.focusedField, b.brFieldButton(0))
	}
	m = feed(t, m, testutil.Key("down"))
	if b.focusedField != b.brFieldButton(1) {
		t.Fatalf("focus = %d, want the Open Tags button %d", b.focusedField, b.brFieldButton(1))
	}
	m = feed(t, m, testutil.Key("down"))
	if b.focusedField != b.brFieldButton(1) {
		t.Errorf("focus = %d, want it held on Open Tags", b.focusedField)
	}
	feed(t, m, testutil.Key("up"))
	if b.focusedField != b.brFieldButton(0) {
		t.Errorf("focus = %d, want Search after up", b.focusedField)
	}
}

func TestEnterOnOpenTagsListsTagsOfTheShortName(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	rec := withFullTagRecorder(t)

	m := typeInto(t, groupedModel(t), "nginx")
	m = downTo(t, m, m.registryBrowser.brFieldButton(1))
	m, cmd := step(t, m, testutil.Key("enter"))
	testutil.Msgs(cmd)

	if m.registryBrowser.state != browserStateTags {
		t.Errorf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	if fake.calls() != 0 {
		t.Errorf("the Searcher was called %d times", fake.calls())
	}
	if n := len(rec.repos()); n != 3 {
		t.Errorf("%d tag requests, want 3 (%v)", n, rec.repos())
	}
}

// IS-01 c.3 by keyboard: enter on Repository and on Search both search.
func TestEnterOnTheSearchButtonOrTheFieldSearches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		focus func(*RegistryBrowser) int
	}{
		{"Repository field", func(*RegistryBrowser) int { return brFieldRepo }},
		{"Search button", func(b *RegistryBrowser) int { return b.brFieldButton(0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSearcher{}
			withImageSearcher(t, fake)
			rec := withFullTagRecorder(t)

			m := typeInto(t, browsingModel(t), "nginx")
			want := checkedKeys(m)
			m = downTo(t, m, tc.focus(m.registryBrowser))
			m, cmd := step(t, m, testutil.Key("enter"))
			testutil.Msgs(cmd)

			if m.registryBrowser.state != browserStateImages {
				t.Errorf("state = %d, want browserStateImages", m.registryBrowser.state)
			}
			if got := fake.keys(); !sameStrings(got, want) {
				t.Errorf("searched %v, want %v", got, want)
			}
			if n := len(rec.repos()); n != 0 {
				t.Errorf("%d tag requests, want none", n)
			}
		})
	}
}

// Without a Searcher the single button is the former flow: the tags.
func TestEnterOnBrowseTagsListsTagsWithoutASearcher(t *testing.T) {
	withImageSearcher(t, nil)
	rec := withFullTagRecorder(t)

	m := typeInto(t, browsingModel(t), "nginx")
	m = downTo(t, m, m.registryBrowser.brFieldButton(0))
	m, cmd := step(t, m, testutil.Key("enter"))
	testutil.Msgs(cmd)

	if m.registryBrowser.state != browserStateTags {
		t.Errorf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	if n, want := len(rec.repos()), len(checkedKeys(m)); n != want {
		t.Errorf("%d tag requests, want %d", n, want)
	}
}

func TestEnterOnACheckboxStillAdvances(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := typeInto(t, browsingModel(t), "nginx")
	b := m.registryBrowser
	m = downTo(t, m, b.brFieldReg(0))
	before := b.selected(b.entries[b.rows[0].entry])

	_, cmd := step(t, m, testutil.Key("enter"))

	if b.focusedField != b.brFieldReg(0)+1 {
		t.Errorf("focus = %d, want %d", b.focusedField, b.brFieldReg(0)+1)
	}
	if cmd != nil {
		t.Error("enter on a checkbox returned a Cmd")
	}
	if b.state != browserStateInput {
		t.Errorf("state = %d, want the form", b.state)
	}
	if after := b.selected(b.entries[b.rows[0].entry]); after != before {
		t.Error("enter toggled the checkbox")
	}
}

func TestSpaceOnAButtonDoesNothing(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := typeInto(t, browsingModel(t), "nginx")
	b := m.registryBrowser
	before := checkedKeys(m)

	for i := range b.formButtons() {
		m = downTo(t, m, b.brFieldButton(i))
		var c tea.Cmd
		m, c = step(t, m, testutil.Key("space"))
		if c != nil {
			t.Errorf("space on button %d returned a Cmd", i)
		}
		if got := checkedKeys(m); !sameStrings(got, before) {
			t.Errorf("space on button %d changed the selection: %v", i, got)
		}
		if b.state != browserStateInput {
			t.Errorf("space on button %d left the form", i)
		}
	}
}

// Rendering: buttons on one plain line, Search before Open Tags.
func TestWithASearcherTheFormShowsSearchAndOpenTagsOnOneLine(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	view := ansi.Strip(groupedModel(t).registryBrowser.View())

	if strings.Contains(view, "Browse Tags") {
		t.Error("Browse Tags is still shown next to Search and Open Tags")
	}
	found := false
	for _, line := range strings.Split(view, "\n") {
		s, o := strings.Index(line, "Search"), strings.Index(line, "Open Tags")
		if s >= 0 && o >= 0 {
			found = true
			if s > o {
				t.Errorf("Search must come before Open Tags: %q", line)
			}
		}
	}
	if !found {
		t.Errorf("no single line holds both buttons:\n%s", view)
	}
}

func TestEscFromTagsReturnsToTheOpenTagsButton(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	withFullTagRecorder(t)

	m := typeInto(t, browsingModel(t), "nginx")
	b := m.registryBrowser
	m = downTo(t, m, b.brFieldButton(1))
	m, cmd := step(t, m, testutil.Key("enter"))
	m = feed(t, m, testutil.Msgs(cmd)...)
	m = feed(t, m, testutil.Key("esc"))

	if b.state != browserStateInput {
		t.Fatalf("state = %d, want the form", b.state)
	}
	if b.focusedField != b.brFieldButton(1) {
		t.Errorf("focus = %d, want the Open Tags button %d", b.focusedField, b.brFieldButton(1))
	}
	if got := b.repoInput.Value(); got != "nginx" {
		t.Errorf("name = %q, want it kept", got)
	}
}
