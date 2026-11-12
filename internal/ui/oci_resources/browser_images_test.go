package ociresources

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Level 1 of the image search (DEVDESK-394, IS-01 c.1 to c.5): a name with no
// "/" is a search, but only when a Searcher is injected (defaultImageSearcher
// is nil in production, Q1 option A); a name with a "/" stays the tag flow.

// fakeSearcher records every call and answers one image per call.
type fakeSearcher struct {
	mu      sync.Mutex
	targets []imagesearch.Target
	queries []imagesearch.Query
	ctxs    []context.Context
}

func (f *fakeSearcher) Search(ctx context.Context, t imagesearch.Target, q imagesearch.Query) ([]imagesearch.Image, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = append(f.targets, t)
	f.queries = append(f.queries, q)
	f.ctxs = append(f.ctxs, ctx)
	return []imagesearch.Image{{EntryKey: t.EntryKey, Name: q.Text, Repo: q.Text}}, false, nil
}

func (f *fakeSearcher) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.targets)
}

func (f *fakeSearcher) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.targets))
	for _, t := range f.targets {
		out = append(out, t.EntryKey)
	}
	sort.Strings(out)
	return out
}

// withImageSearcher injects s for the test. Call it BEFORE opening the
// browser: newRegistryBrowser reads defaultImageSearcher.
func withImageSearcher(t *testing.T, s imagesearch.Searcher) {
	t.Helper()
	prev := defaultImageSearcher
	defaultImageSearcher = s
	t.Cleanup(func() { defaultImageSearcher = prev })
}

// tagCalls swaps the listRegistryTags seam for a recorder of (apiURL, repo).
type tagCalls struct {
	mu    sync.Mutex
	repos []string
}

func withTagRecorder(t *testing.T) *tagCalls {
	t.Helper()
	r := &tagCalls{}
	prev := listRegistryTags
	listRegistryTags = func(_ context.Context, _, repo, _, _ string) ([]string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.repos = append(r.repos, repo)
		return []string{"v1"}, nil
	}
	t.Cleanup(func() { listRegistryTags = prev })
	return r
}

func (r *tagCalls) sorted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.repos...)
	sort.Strings(out)
	return out
}

func checkedKeys(m Model) []string {
	var out []string
	for _, e := range m.registryBrowser.entries {
		if m.registryBrowser.selected(e) {
			out = append(out, e.key)
		}
	}
	sort.Strings(out)
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func submitRaw(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	m = typeInto(t, m, text)
	return step(t, m, testutil.Key("enter"))
}

func uncheckAll(m Model) {
	for _, e := range m.registryBrowser.entries {
		m.registryBrowser.selectedRegs[e.key] = false
	}
}

// c.1
func TestAShortNameWithASearcherSearchesEveryCheckedRegistry(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	tags := withTagRecorder(t)

	m := browsingModel(t)
	want := checkedKeys(m)
	if len(want) != 2 {
		t.Fatalf("fixture checks %d registries, want 2", len(want))
	}
	m, cmd := submitRaw(t, m, "nginx")
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want browserStateImages", m.registryBrowser.state)
	}
	msgs := testutil.Msgs(cmd)

	if got := fake.calls(); got != 2 {
		t.Fatalf("the Searcher was called %d times, want one per checked registry (2)", got)
	}
	if got := fake.keys(); !sameStrings(got, want) {
		t.Errorf("searched %v, want %v", got, want)
	}
	for _, q := range fake.queries {
		if q.Text != "nginx" {
			t.Errorf("Query.Text = %q, want nginx", q.Text)
		}
	}
	if len(tags.sorted()) != 0 {
		t.Errorf("tag requests = %v, want none at level 1", tags.sorted())
	}
	got := 0
	for _, msg := range msgs {
		if r, ok := msg.(ImageSearchResultMsg); ok {
			got++
			if r.Gen != m.registryBrowser.gen {
				t.Errorf("result Gen = %d, want %d", r.Gen, m.registryBrowser.gen)
			}
			m, _ = step(t, m, r)
		}
	}
	if got != 2 {
		t.Fatalf("%d ImageSearchResultMsg, want 2", got)
	}
	if n := len(m.registryBrowser.searchTable.Visible()); n != 2 {
		t.Errorf("results table holds %d rows, want 2", n)
	}
	if m.registryBrowser.pendingSearches != 0 {
		t.Errorf("pendingSearches = %d, want 0", m.registryBrowser.pendingSearches)
	}
}

// Q1 option A: production has no Searcher, a short name stays the tag flow.
func TestAShortNameWithoutASearcherStillListsTags(t *testing.T) {
	withImageSearcher(t, nil)
	tags := withTagRecorder(t)

	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	testutil.Msgs(cmd)
	if got := len(tags.sorted()); got != 2 {
		t.Errorf("tag requests = %d, want 2", got)
	}
}

// c.2
func TestANameWithASlashKeepsTheTagFlow(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	tags := withTagRecorder(t)

	m, cmd := submitRaw(t, browsingModel(t), "library/nginx")
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	testutil.Msgs(cmd)
	if fake.calls() != 0 {
		t.Errorf("the Searcher was called %d times for a name with a slash", fake.calls())
	}
	want := []string{"library/nginx", "library/nginx"}
	if got := tags.sorted(); !sameStrings(got, want) {
		t.Errorf("tag repositories = %v, want %v (one per registry, as typed)", got, want)
	}
}

func TestThePrefixIsAppliedOnceToANameWithASlash(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	tags := withTagRecorder(t)

	m, cmd := submitRaw(t, groupedModel(t), "myorg/api")
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	testutil.Msgs(cmd)
	if fake.calls() != 0 {
		t.Errorf("the Searcher was called %d times", fake.calls())
	}
	got := tags.sorted()
	found := map[string]int{}
	for _, r := range got {
		found[r]++
	}
	if found["docker-hosted/myorg/api"] != 1 || found["dhi-proxy/myorg/api"] != 1 {
		t.Errorf("tag repositories = %v, want docker-hosted/myorg/api and dhi-proxy/myorg/api once each", got)
	}
	for _, r := range got {
		if r == "library/myorg/api" || r == "docker-hosted/library/myorg/api" {
			t.Errorf("library/ was added: %q", r)
		}
	}
}

// c.3
func TestAnEmptyNameStartsNothing(t *testing.T) {
	for _, text := range []string{"", "   "} {
		fake := &fakeSearcher{}
		withImageSearcher(t, fake)
		tags := withTagRecorder(t)

		m := browsingModel(t)
		gen := m.RegistryBrowserGen()
		m, cmd := submitRaw(t, m, text)

		if cmd != nil {
			t.Errorf("%q: a Cmd was returned", text)
		}
		if m.registryBrowser.state != browserStateInput {
			t.Errorf("%q: state = %d, want the form", text, m.registryBrowser.state)
		}
		if fake.calls() != 0 || len(tags.sorted()) != 0 {
			t.Errorf("%q: something was requested", text)
		}
		if m.RegistryBrowserGen() != gen {
			t.Errorf("%q: gen changed", text)
		}
	}
}

// c.4
func TestNoCheckedRegistryWarnsAndStaysOnTheForm(t *testing.T) {
	for _, name := range []string{"nginx", "myorg/api"} {
		fake := &fakeSearcher{}
		withImageSearcher(t, fake)
		tags := withTagRecorder(t)

		m := browsingModel(t)
		uncheckAll(m)
		m, cmd := submitRaw(t, m, name)

		if m.registryBrowser.state != browserStateInput {
			t.Errorf("%q: state = %d, want the form", name, m.registryBrowser.state)
		}
		warn, ok := testutil.MsgOf[RegistryBrowserWarnMsg](cmd)
		if !ok {
			t.Fatalf("%q: the Cmd did not return a RegistryBrowserWarnMsg", name)
		}
		if warn.Text != "No registry selected" {
			t.Errorf("%q: Text = %q", name, warn.Text)
		}
		m = feed(t, m, warn)
		if m.footer.Level() != components.LevelWarning || m.footer.Text() != "No registry selected" {
			t.Errorf("%q: footer = %v %q", name, m.footer.Level(), m.footer.Text())
		}
		if fake.calls() != 0 || len(tags.sorted()) != 0 {
			t.Errorf("%q: something was requested", name)
		}
	}
}

// c.5
func TestSearchSkipsUncheckedMembersAndKeepsTheSelection(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)

	m := groupedModel(t)
	dropped := m.registryBrowser.entries[0].key
	m.registryBrowser.selectedRegs[dropped] = false
	want := checkedKeys(m)

	m, cmd := submitRaw(t, m, "nginx")
	testutil.Msgs(cmd)
	got := fake.keys()
	if !sameStrings(got, want) {
		t.Errorf("searched %v, want only %v", got, want)
	}
	for _, k := range got {
		if k == dropped {
			t.Errorf("the unchecked %q was searched", k)
		}
	}

	m = feed(t, m, RegistryBrowserCloseMsg{})
	if !m.browserDeselected[dropped] || len(m.browserDeselected) != 1 {
		t.Errorf("remembered selection = %v, want only %q", m.browserDeselected, dropped)
	}
}

// An anonymous entry sends no credentials (D12).
func TestAnAnonymousEntryIsSearchedWithoutCredentials(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)

	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	_ = m
	testutil.Msgs(cmd)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, tg := range fake.targets {
		if tg.EntryKey == "hub" && (tg.Username != "" || tg.Password != "") {
			t.Errorf("anonymous hub got credentials: %+v", tg)
		}
	}
}

// IS-18: a new validation cancels the previous lot, a stale answer is dropped.
func TestANewSearchCancelsThePreviousOneAndDropsItsAnswers(t *testing.T) {
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)

	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	first := testutil.Msgs(cmd)
	firstCtx := fake.ctxs[0]
	oldGen := m.registryBrowser.gen

	m = feed(t, m, testutil.Key("esc"))
	m, cmd = step(t, m, testutil.Key("enter"))
	if cmd == nil {
		t.Fatal("the second validation started nothing")
	}
	if firstCtx.Err() != context.Canceled {
		t.Errorf("the first lot's context = %v, want Canceled", firstCtx.Err())
	}
	if m.registryBrowser.gen == oldGen {
		t.Error("gen did not change")
	}
	pending := m.registryBrowser.pendingSearches
	for _, msg := range first {
		if r, ok := msg.(ImageSearchResultMsg); ok {
			m, _ = step(t, m, r)
		}
	}
	if m.registryBrowser.pendingSearches != pending {
		t.Errorf("pendingSearches = %d, want %d: a stale answer must not count", m.registryBrowser.pendingSearches, pending)
	}
	if n := len(m.registryBrowser.searchTable.Visible()); n != 0 {
		t.Errorf("%d rows from a stale answer", n)
	}
}

func TestTheRegistryDeadlineIsFifteenSeconds(t *testing.T) {
	if got := imageSearchDeadline.Seconds(); got != 15 {
		t.Errorf("imageSearchDeadline = %vs, want 15", got)
	}
}

// blockingSearcher announces its context, then waits for it to end and returns
// its error: it answers only when the lot is cancelled or the deadline hits.
type blockingSearcher struct {
	started chan context.Context
}

func newBlockingSearcher() *blockingSearcher {
	return &blockingSearcher{started: make(chan context.Context, 8)}
}

func (b *blockingSearcher) Search(ctx context.Context, _ imagesearch.Target, _ imagesearch.Query) ([]imagesearch.Image, bool, error) {
	b.started <- ctx
	<-ctx.Done()
	return nil, false, ctx.Err()
}

// withSearchDeadline shrinks imageSearchDeadline and restores it on cleanup.
// Call it before opening the browser.
func withSearchDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	prev := imageSearchDeadline
	imageSearchDeadline = d
	t.Cleanup(func() { imageSearchDeadline = prev })
}

// runCmds runs every Cmd of the lot concurrently and returns the channel their
// messages arrive on.
func runCmds(t *testing.T, cmd tea.Cmd) <-chan ImageSearchResultMsg {
	t.Helper()
	out := make(chan ImageSearchResultMsg, 8)
	for _, c := range flattenBatch(cmd) {
		go func() {
			if r, ok := c().(ImageSearchResultMsg); ok {
				out <- r
			}
		}()
	}
	return out
}

// flattenBatch splits a tea.Batch into its Cmds.
func flattenBatch(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		return []tea.Cmd(b)
	}
	return nil
}

func recvResult(t *testing.T, ch <-chan ImageSearchResultMsg) ImageSearchResultMsg {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no ImageSearchResultMsg arrived")
	}
	return ImageSearchResultMsg{}
}

// IS-18 c.2: a registry that never answers is cut at the deadline; the result
// carries DeadlineExceeded, adds no row and still counts as answered.
func TestARegistryThatNeverAnswersIsCutAtTheDeadline(t *testing.T) {
	withSearchDeadline(t, 20*time.Millisecond)
	withImageSearcher(t, newBlockingSearcher())

	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	ch := runCmds(t, cmd)
	if m.registryBrowser.pendingSearches != 2 {
		t.Fatalf("pendingSearches = %d, want 2", m.registryBrowser.pendingSearches)
	}
	r := recvResult(t, ch)
	if !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("Err = %v, want context.DeadlineExceeded", r.Err)
	}
	m = feed(t, m, r)
	if got := m.registryBrowser.pendingSearches; got != 1 {
		t.Errorf("pendingSearches = %d, want 1", got)
	}
	if n := len(m.registryBrowser.searchTable.Visible()); n != 0 {
		t.Errorf("%d rows after a timeout, want none", n)
	}
}

// IS-18 c.3: closing the browser cancels the context the Searcher received.
func TestClosingTheBrowserCancelsTheSearchContext(t *testing.T) {
	fake := newBlockingSearcher()
	withImageSearcher(t, fake)

	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	ch := runCmds(t, cmd)
	ctx := <-fake.started
	if ctx.Err() != nil {
		t.Fatalf("the context ended before the browser closed: %v", ctx.Err())
	}
	m = feed(t, m, RegistryBrowserCloseMsg{})
	if m.registryBrowser != nil {
		t.Fatal("the browser is still open")
	}
	if r := recvResult(t, ch); !errors.Is(r.Err, context.Canceled) {
		t.Errorf("Err = %v, want context.Canceled", r.Err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v, want Canceled", ctx.Err())
	}
}

// A failed registry counts as answered, adds nothing, and is logged without
// any secret.
func TestAFailedSearchIsLoggedWithoutSecretsAndAddsNoRow(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	logs := captureLog(t)

	m, _ := submitRaw(t, browsingModel(t), "nginx")
	err := errors.New("GET https://bob:s3cretpw@registry.example.com/v2/?password=hunter2 failed")
	m = feed(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "prod", Err: err})

	if got := m.registryBrowser.pendingSearches; got != 1 {
		t.Errorf("pendingSearches = %d, want 1", got)
	}
	if n := len(m.registryBrowser.searchTable.Visible()); n != 0 {
		t.Errorf("%d rows from a failed search", n)
	}
	out := logs.String()
	if !strings.Contains(out, "image search") {
		t.Errorf("no journal entry for the failure: %q", out)
	}
	for _, secret := range []string{"s3cretpw", "hunter2"} {
		if strings.Contains(out, secret) {
			t.Errorf("the journal holds %q: %q", secret, out)
		}
	}
}

// Two members of different groups with the same alias must not look alike in
// the Registry column, as in the tag flow (entryAlias).
func TestTwoGroupMembersWithTheSameAliasAreToldApart(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})

	m, _ := submitRaw(t, browsingModel(t), "nginx")
	b := m.registryBrowser
	b.entries = []browserRegistryEntry{
		{URL: "a.example.com", Alias: "dhi", ParentAlias: "nexus-a", key: "a/dhi"},
		{URL: "b.example.com", Alias: "dhi", ParentAlias: "nexus-b", key: "b/dhi"},
	}
	m = feed(t, m,
		ImageSearchResultMsg{Gen: b.gen, EntryKey: "a/dhi", Images: []imagesearch.Image{{Name: "nginx"}}},
		ImageSearchResultMsg{Gen: b.gen, EntryKey: "b/dhi", Images: []imagesearch.Image{{Name: "nginx"}}},
	)
	rows := m.registryBrowser.searchTable.Visible()
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	if rows[0].label == rows[1].label {
		t.Fatalf("both rows read %q, want the group told apart", rows[0].label)
	}
	got := []string{rows[0].label, rows[1].label}
	want := []string{"nexus-a/dhi", "nexus-b/dhi"}
	if !sameStrings(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

// esc at level 1 drops the lot: its context is cancelled, the generation
// changes and a late answer of that lot is ignored.
func TestEscAtLevelOneCancelsTheLotAndIgnoresItsLateAnswers(t *testing.T) {
	fake := newBlockingSearcher()
	withImageSearcher(t, fake)

	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	ch := runCmds(t, cmd)
	ctx := <-fake.started
	oldGen := m.registryBrowser.gen

	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser == nil || m.registryBrowser.state != browserStateInput {
		t.Fatal("esc did not bring the form back")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v, want Canceled", ctx.Err())
	}
	if m.registryBrowser.gen == oldGen {
		t.Error("gen did not change")
	}
	late := recvResult(t, ch)
	late.Images = []imagesearch.Image{{Name: "late"}}
	late.Err = nil
	m = feed(t, m, late, ImageSearchResultMsg{Gen: oldGen, EntryKey: "hub", Images: []imagesearch.Image{{Name: "late"}}})
	if got := m.registryBrowser.pendingSearches; got != 0 {
		t.Errorf("pendingSearches = %d, want 0", got)
	}
	if n := len(m.registryBrowser.searchTable.Visible()); n != 0 {
		t.Errorf("%d rows from a cancelled lot", n)
	}
	if m.registryBrowser.state != browserStateInput {
		t.Errorf("state = %d, a late answer moved the browser", m.registryBrowser.state)
	}
}
