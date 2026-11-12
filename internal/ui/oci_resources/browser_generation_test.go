package ociresources

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The tag flow's generation and cancellation (DEVDESK-393, IS-18 c.3 and c.4):
// every result message names the lot of requests it answers, the handler drops
// what a newer lot or a closed browser left behind, and a new lot (or closing)
// cancels the one in flight.

// ctxRecorder swaps the listRegistryTags seam for a stub that records the
// context of every call and answers at once.
type ctxRecorder struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func withTagLister(t *testing.T) *ctxRecorder {
	t.Helper()
	r := &ctxRecorder{}
	prev := listRegistryTags
	listRegistryTags = func(ctx context.Context, _, _, _, _ string) ([]string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.ctxs = append(r.ctxs, ctx)
		return []string{"v1"}, nil
	}
	t.Cleanup(func() { listRegistryTags = prev })
	return r
}

func (r *ctxRecorder) all() []context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]context.Context(nil), r.ctxs...)
}

// submit types the repository and presses enter, then runs the commands the
// validation returned (what the runtime does), returning the model and the
// messages they produced.
func submit(t *testing.T, m Model, repo string) (Model, []tea.Msg) {
	t.Helper()
	m = typeInto(t, m, repo)
	m, cmd := step(t, m, testutil.Key("enter"))
	if m.registryBrowser == nil || m.registryBrowser.state != browserStateTags {
		t.Fatal("the validation did not start a search")
	}
	if cmd == nil {
		return m, nil
	}
	return m, testutil.Msgs(cmd)
}

// resubmit goes back to the form (esc on the results) and validates again.
func resubmit(t *testing.T, m Model) (Model, []tea.Msg) {
	t.Helper()
	m = feed(t, m, testutil.Key("esc"))
	m, cmd := step(t, m, testutil.Key("enter"))
	if cmd == nil {
		return m, nil
	}
	return m, testutil.Msgs(cmd)
}

func tagsMsgs(msgs []tea.Msg) []MultiRegistryTagsLoadedMsg {
	var out []MultiRegistryTagsLoadedMsg
	for _, msg := range msgs {
		if l, ok := msg.(MultiRegistryTagsLoadedMsg); ok {
			out = append(out, l)
		}
	}
	return out
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(nopWriter{}) })
	return &buf
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// ── (a) the handler drops a stale generation ────────────────────────────────

func TestATagsMessageOfAnOlderGenerationIsIgnored(t *testing.T) {
	withTagLister(t)
	logs := captureLog(t)

	m, first := submit(t, browsingModel(t), "api")
	old := tagsMsgs(first)
	if len(old) != 2 {
		t.Fatalf("the first lot answered %d times, want one per registry (2)", len(old))
	}
	m, _ = resubmit(t, m)
	b := m.registryBrowser
	if b.pendingSearches != 2 {
		t.Fatalf("pendingSearches = %d after the second validation, want 2", b.pendingSearches)
	}
	if old[0].Gen == b.gen {
		t.Fatalf("both lots carry generation %d", b.gen)
	}
	logs.Reset()

	stale := old[0]
	stale.Err = errors.New("boom from the first lot")
	m, cmd := step(t, m, stale)

	if got := m.registryBrowser.pendingSearches; got != 2 {
		t.Errorf("pendingSearches = %d, want 2: a stale answer must not count", got)
	}
	if got := len(m.registryBrowser.tags); got != 0 {
		t.Errorf("tags = %d, want none from a stale answer", got)
	}
	if cmd != nil {
		t.Error("a stale answer started the metadata lookup")
	}
	if logs.Len() != 0 {
		t.Errorf("a stale answer was logged: %q", logs.String())
	}
}

func TestATagsMessageOfTheCurrentGenerationIsAccepted(t *testing.T) {
	withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	m, _ = resubmit(t, m)
	gen := m.registryBrowser.gen
	if gen == 0 || gen != m.RegistryBrowserGen() {
		t.Fatalf("gen = %d, RegistryBrowserGen() = %d, want one non-zero value", gen, m.RegistryBrowserGen())
	}

	m = feed(t, m, MultiRegistryTagsLoadedMsg{
		Gen: gen, EntryKey: "prod", RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1"},
	})

	if got := m.registryBrowser.pendingSearches; got != 1 {
		t.Errorf("pendingSearches = %d, want 1 after one of two answers", got)
	}
	if got := len(m.registryBrowser.tags); got != 1 {
		t.Errorf("tags = %d, want the accepted answer listed", got)
	}
}

func TestEveryResultMessageCarriesTheGenerationOfItsLot(t *testing.T) {
	withTagLister(t)

	m, msgs := submit(t, browsingModel(t), "api")
	got := tagsMsgs(msgs)
	if len(got) != 2 {
		t.Fatalf("%d answers, want 2", len(got))
	}
	for _, l := range got {
		if l.Gen == 0 || l.Gen != m.RegistryBrowserGen() {
			t.Errorf("answer for %s carries Gen %d, want the browser's %d", l.RegistryURL, l.Gen, m.RegistryBrowserGen())
		}
	}
}

func TestAMetaMessageOfAnOlderGenerationDatesNothing(t *testing.T) {
	withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	m, _ = resubmit(t, m)
	gen := m.registryBrowser.gen
	m = feed(t, m, MultiRegistryTagsLoadedMsg{
		Gen: gen, EntryKey: "prod", RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1"},
	})

	m = feed(t, m, MultiRegistryTagsMetaMsg{
		Gen: gen - 1, EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api",
		Meta: map[string]time.Time{"v1": at(1)},
	})
	if got := m.registryBrowser.tagTable.Table().Rows()[0][3]; got != "-" {
		t.Errorf("Updated = %q, a stale metadata answer dated the new search's tag", got)
	}

	m = feed(t, m, MultiRegistryTagsMetaMsg{
		Gen: gen, EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api",
		Meta: map[string]time.Time{"v1": time.Now().Add(-2 * time.Hour)},
	})
	if got := m.registryBrowser.tagTable.Table().Rows()[0][3]; got == "-" {
		t.Error("the current generation's metadata was dropped")
	}
}

// ── (b) the counter is global ───────────────────────────────────────────────

func TestAMessageOfAClosedBrowserIsNotAcceptedByTheNextOne(t *testing.T) {
	withTagLister(t)

	m, first := submit(t, browsingModel(t), "api")
	old := tagsMsgs(first)
	if len(old) == 0 {
		t.Fatal("no answer from the first browser")
	}
	m = feed(t, m, RegistryBrowserCloseMsg{})
	if m.registryBrowser != nil {
		t.Fatal("the browser did not close")
	}
	if got := m.RegistryBrowserGen(); got != 0 {
		t.Errorf("RegistryBrowserGen() = %d with the browser closed, want 0", got)
	}

	m = feed(t, m, testutil.Key(keymap.Browser))
	m, _ = submit(t, m, "api")
	b := m.registryBrowser
	if b.gen <= old[0].Gen {
		t.Fatalf("the reopened browser holds gen %d, not past the old %d: the counter is not global", b.gen, old[0].Gen)
	}

	m = feed(t, m, old[0])

	if got := m.registryBrowser.pendingSearches; got != 2 {
		t.Errorf("pendingSearches = %d, want 2: the old instance's answer must not count", got)
	}
	if len(m.registryBrowser.tags) != 0 {
		t.Error("the old instance's answer was listed")
	}
}

// ── (c) cancellation ────────────────────────────────────────────────────────

func TestANewValidationCancelsTheRequestsInFlight(t *testing.T) {
	rec := withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	first := rec.all()
	if len(first) != 2 {
		t.Fatalf("the first lot made %d calls, want 2", len(first))
	}
	for i, ctx := range first {
		if ctx.Err() != nil {
			t.Fatalf("call %d: context already %v before any new validation", i, ctx.Err())
		}
	}

	resubmit(t, m)

	for i, ctx := range first {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("call %d: ctx.Err() = %v, want context.Canceled", i, ctx.Err())
		}
	}
	for i, ctx := range rec.all()[len(first):] {
		if ctx.Err() != nil {
			t.Errorf("new call %d: context cancelled, want it live", i)
		}
	}
}

func TestClosingTheBrowserCancelsTheRequestsInFlight(t *testing.T) {
	rec := withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	m, _ = step(t, m, testutil.Key("esc"))
	_, cmd := step(t, m, testutil.Key("esc"))
	msg, ok := testutil.MsgOf[RegistryBrowserCloseMsg](cmd)
	if !ok {
		t.Fatal("esc on the form did not ask to close")
	}
	feed(t, m, msg)

	for i, ctx := range rec.all() {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("call %d: ctx.Err() = %v, want context.Canceled after closing", i, ctx.Err())
		}
	}
}

// G closes the browser to start a pull; it goes through the same close.
func TestPullingFromTheBrowserCancelsTheRequestsInFlight(t *testing.T) {
	rec := withTagLister(t)

	m, msgs := submit(t, browsingModel(t), "api")
	m = feed(t, m, msgs...)
	m = pullRequested(t, m)
	if m.registryBrowser != nil {
		t.Fatal("the browser is still open after G")
	}

	calls := rec.all()
	if len(calls) == 0 {
		t.Fatal("no request was made")
	}
	for i, ctx := range calls {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("call %d: ctx.Err() = %v, want context.Canceled after G", i, ctx.Err())
		}
	}
}

func TestAnEmptyOrRefusedValidationLeavesTheLotInFlight(t *testing.T) {
	rec := withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	m = feed(t, m, testutil.Key("esc"))
	gen := m.registryBrowser.gen

	// Empty repository.
	for range 4 {
		m = feed(t, m, testutil.Key("backspace"))
	}
	m = feed(t, m, testutil.Key("enter"))
	if m.registryBrowser.gen != gen {
		t.Errorf("an empty validation changed gen %d -> %d", gen, m.registryBrowser.gen)
	}

	// No registry checked.
	m = typeInto(t, m, "api")
	b := m.registryBrowser
	for _, entry := range b.entries {
		b.selectedRegs[entry.key] = false
	}
	m = feed(t, m, testutil.Key("enter"))
	if m.registryBrowser.gen != gen {
		t.Errorf("a refused validation changed gen %d -> %d", gen, m.registryBrowser.gen)
	}

	for i, ctx := range rec.all() {
		if ctx.Err() != nil {
			t.Errorf("call %d: a validation that searched nothing cancelled it (%v)", i, ctx.Err())
		}
	}
}

func TestSearchRegistryTagsCmdReportsACancelWithItsGeneration(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan tea.Msg, 1)
	go func() {
		done <- searchRegistryTagsCmd(ctx, 42, "prod", "registry.example.com", "prod", srv.URL, "api", "", "")()
	}()

	<-started
	cancel()
	msg, ok := (<-done).(MultiRegistryTagsLoadedMsg)
	if !ok {
		t.Fatal("the command did not return a MultiRegistryTagsLoadedMsg")
	}
	if !errors.Is(msg.Err, context.Canceled) {
		t.Errorf("Err = %v, want context.Canceled", msg.Err)
	}
	if msg.Gen != 42 {
		t.Errorf("Gen = %d, want the 42 it was given", msg.Gen)
	}
}

// ── (d) a response with no valid Gen is ignored ─────────────────────────────

// The tag flow used to take any answer for the browser that was open. A message
// with no generation (the zero value) is not the open browser's once it has
// searched.
func TestAnAnswerWithNoGenerationIsIgnoredOnceASearchStarted(t *testing.T) {
	withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	if m.registryBrowser.gen == 0 {
		t.Fatal("a search left the browser on generation 0")
	}

	m = feed(t, m, MultiRegistryTagsLoadedMsg{
		EntryKey: "prod", RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1"},
	})

	if got := m.registryBrowser.pendingSearches; got != 2 {
		t.Errorf("pendingSearches = %d, want 2: an answer with no Gen must not count", got)
	}
	if len(m.registryBrowser.tags) != 0 {
		t.Error("an answer with no Gen was listed")
	}
}

// Reopening the browser while a lot is in flight replaces it: the old lot's
// context must be cancelled, or its requests keep running for nobody.
func TestReopeningTheBrowserCancelsTheLotInFlight(t *testing.T) {
	r := withTagLister(t)

	m, _ := submit(t, browsingModel(t), "api")
	calls := r.all()
	if len(calls) == 0 {
		t.Fatal("the first lot made no request")
	}
	for i, ctx := range calls {
		if ctx.Err() != nil {
			t.Fatalf("call %d: context already done before reopening: %v", i, ctx.Err())
		}
	}

	reopened, _ := m.openMultiRegistryBrowser()
	if reopened.(Model).registryBrowser == m.registryBrowser {
		t.Fatal("openMultiRegistryBrowser kept the old browser")
	}

	for i, ctx := range calls {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("call %d: ctx.Err() = %v, want context.Canceled after reopening", i, ctx.Err())
		}
	}
}
