package ociresources

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// "Open tags" (DEVDESK-300, IS-11): the button that always lists the tags of
// the typed name, whatever the name looks like and whether or not a Searcher is
// wired. These tests call submit(action) directly after typing the name.

// fullTagCall is one request the tag lookup sent to a registry.
type fullTagCall struct{ apiURL, repo, user, pass string }

type fullTagCalls struct {
	mu    sync.Mutex
	calls []fullTagCall
}

// withFullTagRecorder swaps the listRegistryTags seam for a recorder of every
// argument that reaches the registry (withTagRecorder keeps only the repo).
func withFullTagRecorder(t *testing.T) *fullTagCalls {
	t.Helper()
	r := &fullTagCalls{}
	prev := listRegistryTags
	listRegistryTags = func(_ context.Context, apiURL, repo, user, pass string) ([]string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls = append(r.calls, fullTagCall{apiURL, repo, user, pass})
		return []string{"v1", "latest"}, nil
	}
	t.Cleanup(func() { listRegistryTags = prev })
	return r
}

// sorted returns the calls as strings, in a stable order.
func (r *fullTagCalls) sorted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, fmt.Sprintf("%q", []string{c.apiURL, c.repo, c.user, c.pass}))
	}
	sort.Strings(out)
	return out
}

func (r *fullTagCalls) repos() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.repo)
	}
	sort.Strings(out)
	return out
}

// openedTags is everything observable about a submit that lists tags.
type openedTags struct {
	state    browserState
	requests []string // what reached the registries
	msgs     []string // MultiRegistryTagsLoadedMsg minus Gen, sorted
	refs     []string // multiImageName of every table row, sorted
	cmdNil   bool
}

// runTagSubmit types name, calls submit(action) and plays the answers back.
func runTagSubmit(t *testing.T, m Model, name string, action formAction, rec *fullTagCalls) openedTags {
	t.Helper()
	m = typeInto(t, m, name)
	_, cmd := m.registryBrowser.submit(action)
	out := openedTags{state: m.registryBrowser.state, cmdNil: cmd == nil}
	var loaded []tea.Msg
	for _, msg := range testutil.Msgs(cmd) {
		v, ok := msg.(MultiRegistryTagsLoadedMsg)
		if !ok {
			continue
		}
		loaded = append(loaded, v)
		v.Gen = 0
		out.msgs = append(out.msgs, fmt.Sprintf("%#v", v))
	}
	m = feed(t, m, loaded...)
	for _, tag := range m.registryBrowser.tags {
		out.refs = append(out.refs, multiImageName(tag.RegistryURL, tag.Repo, tag.Tag))
	}
	sort.Strings(out.msgs)
	sort.Strings(out.refs)
	out.requests = rec.sorted()
	return out
}

func sameList(a, b []string) bool { return sameStrings(a, b) }

// c.2: the former "Browse Tags" (no Searcher, the old search-or-tags routing)
// and the new "Open tags" (a Searcher wired, actionOpenTags) are the same flow.
func TestOpenTagsMatchesTheFormerBrowseTagsToTheCharacter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		model   func(*testing.T) Model
		typed   string
		wantLen int
	}{
		{"short name, flat registries", browsingModel, "nginx", 2},
		{"short name, grouped registries", groupedModel, "nginx", 3},
		{"name with a slash, flat registries", browsingModel, "myorg/api", 2},
		{"name with a slash, grouped registries", groupedModel, "myorg/api", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Former behaviour: no Searcher, the single "Browse Tags" button.
			withImageSearcher(t, nil)
			recOld := withFullTagRecorder(t)
			old := runTagSubmit(t, tc.model(t), tc.typed, actionSearch, recOld)

			// New behaviour: a Searcher is wired, "Open tags" is pressed.
			fake := &fakeSearcher{}
			withImageSearcher(t, fake)
			recNew := withFullTagRecorder(t)
			got := runTagSubmit(t, tc.model(t), tc.typed, actionOpenTags, recNew)

			if len(old.requests) != tc.wantLen {
				t.Fatalf("reference flow sent %d requests, want %d (%v): fixture drifted", len(old.requests), tc.wantLen, old.requests)
			}
			if old.state != browserStateTags {
				t.Fatalf("reference flow state = %d, want browserStateTags", old.state)
			}
			if got.state != old.state {
				t.Errorf("state = %d, want %d", got.state, old.state)
			}
			if got.cmdNil {
				t.Fatal("Open tags returned no Cmd")
			}
			if !sameList(got.requests, old.requests) {
				t.Errorf("requests differ\n got  %v\n want %v", got.requests, old.requests)
			}
			if !sameList(got.msgs, old.msgs) {
				t.Errorf("messages differ\n got  %v\n want %v", got.msgs, old.msgs)
			}
			if !sameList(got.refs, old.refs) || len(got.refs) == 0 {
				t.Errorf("image references differ\n got  %v\n want %v", got.refs, old.refs)
			}
			if fake.calls() != 0 {
				t.Errorf("the Searcher was called %d times by Open tags", fake.calls())
			}
		})
	}
}

func TestOpenTagsOnAShortNameUsesLibraryOnHubAndThePrefixOnce(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	rec := withFullTagRecorder(t)

	m := typeInto(t, groupedModel(t), "nginx")
	_, cmd := m.registryBrowser.submit(actionOpenTags)
	testutil.Msgs(cmd)

	want := []string{"dhi-proxy/nginx", "docker-hosted/nginx", "library/nginx"}
	if got := rec.repos(); !sameList(got, want) {
		t.Errorf("repositories = %v, want %v", got, want)
	}
	for _, r := range rec.repos() {
		if r == "docker-hosted/library/nginx" || r == "dhi-proxy/library/nginx" ||
			r == "docker-hosted/docker-hosted/nginx" || r == "dhi-proxy/dhi-proxy/nginx" {
			t.Errorf("wrong repository %q", r)
		}
	}
	if fake.calls() != 0 {
		t.Errorf("the Searcher was called %d times", fake.calls())
	}
}

func TestOpenTagsNeverCallsTheSearcher(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	rec := withFullTagRecorder(t)

	m := typeInto(t, browsingModel(t), "nginx")
	_, cmd := m.registryBrowser.submit(actionOpenTags)
	testutil.Msgs(cmd)

	if m.registryBrowser.state != browserStateTags {
		t.Errorf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	if fake.calls() != 0 {
		t.Errorf("the Searcher was called %d times, want 0", fake.calls())
	}
	if n, want := len(rec.repos()), len(checkedKeys(m)); n != want {
		t.Errorf("%d tag requests, want one per checked registry (%d)", n, want)
	}
}

// c.3: the search action is the IS-01 search, not the tags.
func TestSearchActionOnAShortNameSearches(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	rec := withFullTagRecorder(t)

	m := typeInto(t, browsingModel(t), "nginx")
	want := checkedKeys(m)
	_, cmd := m.registryBrowser.submit(actionSearch)
	testutil.Msgs(cmd)

	if m.registryBrowser.state != browserStateImages {
		t.Errorf("state = %d, want browserStateImages", m.registryBrowser.state)
	}
	if got := fake.keys(); !sameList(got, want) {
		t.Errorf("searched %v, want one call per checked registry %v", got, want)
	}
	if n := len(rec.repos()); n != 0 {
		t.Errorf("%d tag requests, want none for a search", n)
	}
}

// c.4: the registry selection applies to both actions and is not altered.
func TestOpenTagsHonoursTheSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action formAction
	}{{"open tags", actionOpenTags}, {"search on a path", actionSearch}} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSearcher{}
			withImageSearcher(t, fake)
			rec := withFullTagRecorder(t)

			m := groupedModel(t)
			m.registryBrowser.selectedRegs["hub"] = false
			before := append([]string(nil), m.registryBrowser.Deselected()...)
			if len(before) == 0 {
				t.Fatal("fixture: nothing is unchecked")
			}
			m = typeInto(t, m, "myorg/nginx")
			_, cmd := m.registryBrowser.submit(tc.action)
			testutil.Msgs(cmd)

			want := []string{"dhi-proxy/myorg/nginx", "docker-hosted/myorg/nginx"}
			if got := rec.repos(); !sameList(got, want) {
				t.Errorf("repositories = %v, want only the two members %v", got, want)
			}
			if after := m.registryBrowser.Deselected(); !sameList(after, before) {
				t.Errorf("Deselected() = %v, was %v", after, before)
			}
		})
	}

	t.Run("open tags on a short name", func(t *testing.T) {
		withImageSearcher(t, &fakeSearcher{})
		rec := withFullTagRecorder(t)
		m := groupedModel(t)
		m.registryBrowser.selectedRegs["hub"] = false
		m = typeInto(t, m, "nginx")
		_, cmd := m.registryBrowser.submit(actionOpenTags)
		testutil.Msgs(cmd)
		want := []string{"dhi-proxy/nginx", "docker-hosted/nginx"}
		if got := rec.repos(); !sameList(got, want) {
			t.Errorf("repositories = %v, want %v", got, want)
		}
	})

	t.Run("search on a short name", func(t *testing.T) {
		fake := &fakeSearcher{}
		withImageSearcher(t, fake)
		m := groupedModel(t)
		m.registryBrowser.selectedRegs["hub"] = false
		want := checkedKeys(m)
		m = typeInto(t, m, "nginx")
		_, cmd := m.registryBrowser.submit(actionSearch)
		testutil.Msgs(cmd)
		if got := fake.keys(); !sameList(got, want) {
			t.Errorf("searched %v, want %v", got, want)
		}
	})
}

// c.4: same validation as Search, for both actions.
func TestOpenTagsValidatesLikeSearch(t *testing.T) {
	for _, action := range []formAction{actionOpenTags, actionSearch} {
		for _, text := range []string{"", "   "} {
			t.Run(fmt.Sprintf("action %d empty %q", action, text), func(t *testing.T) {
				fake := &fakeSearcher{}
				withImageSearcher(t, fake)
				rec := withFullTagRecorder(t)

				m := typeInto(t, browsingModel(t), text)
				gen := m.RegistryBrowserGen()
				_, cmd := m.registryBrowser.submit(action)

				if cmd != nil {
					t.Error("an empty name returned a Cmd")
				}
				if m.registryBrowser.state != browserStateInput {
					t.Errorf("state = %d, want the form", m.registryBrowser.state)
				}
				if m.RegistryBrowserGen() != gen {
					t.Error("gen changed")
				}
				if fake.calls() != 0 || len(rec.repos()) != 0 {
					t.Error("something was requested")
				}
			})
		}
		t.Run(fmt.Sprintf("action %d no registry", action), func(t *testing.T) {
			fake := &fakeSearcher{}
			withImageSearcher(t, fake)
			rec := withFullTagRecorder(t)

			m := browsingModel(t)
			uncheckAll(m)
			m = typeInto(t, m, "nginx")
			_, cmd := m.registryBrowser.submit(action)

			if m.registryBrowser.state != browserStateInput {
				t.Errorf("state = %d, want the form", m.registryBrowser.state)
			}
			warn, ok := testutil.MsgOf[RegistryBrowserWarnMsg](cmd)
			if !ok {
				t.Fatal("no RegistryBrowserWarnMsg")
			}
			if warn.Text != "No registry selected" {
				t.Errorf("Text = %q", warn.Text)
			}
			m = feed(t, m, warn)
			if m.footer.Level() != components.LevelWarning {
				t.Errorf("footer level = %v, want warning", m.footer.Level())
			}
			if fake.calls() != 0 || len(rec.repos()) != 0 {
				t.Error("something was requested")
			}
		})
	}
}
