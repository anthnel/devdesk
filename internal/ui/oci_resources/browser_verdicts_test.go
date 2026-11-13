package ociresources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-303 (IS-12): each checked registry gets exactly one verdict (ok, no
// result, unsupported or refused, failed), kept in the browser for IS-13 and
// logged; nothing new is drawn (Q2 = A), and no registry's problem hides the
// others' rows.

// registryModel opens the browser on a model configured with items only.
func registryModel(t *testing.T, items ...config.RegistryItem) Model {
	t.Helper()
	cfg := testConfig()
	cfg.Registry.Registries = items
	m := feed(t, New(cfg), testutil.Resize(180, 30),
		ImagesListMsg{Images: imageFixtures()},
		ScanCacheLoadedMsg{Entries: scanCacheFixture()},
		NetworksListMsg{Networks: networkFixtures()},
		VolumesListMsg{Volumes: volumeFixtures()},
	)
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	return m
}

func anonymousRegistry(url, alias string) config.RegistryItem {
	return config.RegistryItem{URL: url, Alias: alias, Slug: alias, AuthMode: config.AuthAnonymous}
}

// threeRegistryModel has the registries "prod", "hub" and "third".
func threeRegistryModel(t *testing.T) Model {
	t.Helper()
	return registryModel(t,
		anonymousRegistry("prod.example.com", "prod"),
		anonymousRegistry("docker.io", "hub"),
		anonymousRegistry("third.example.com", "third"),
	)
}

// searchLot submits a short name, so one search per checked registry starts.
func searchLot(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := submitRaw(t, m, "nginx")
	if cmd == nil || m.registryBrowser.state != browserStateImages {
		t.Fatalf("the search did not start (state %d)", m.registryBrowser.state)
	}
	return m
}

// answerFor feeds one registry's answer, stamped with the open lot.
func answerFor(t *testing.T, m Model, key string, images []imagesearch.Image, partial bool, err error) Model {
	t.Helper()
	return feed(t, m, ImageSearchResultMsg{
		Gen: m.registryBrowser.gen, EntryKey: key, Images: images, Partial: partial, Err: err,
	})
}

func oneImage(name string) []imagesearch.Image {
	return []imagesearch.Image{{Name: name, Repo: "library/" + name}}
}

func verdictOf(t *testing.T, m Model, key string) (imagesearch.Outcome, bool) {
	t.Helper()
	return m.registryBrowser.Outcome(key)
}

func wantVerdict(t *testing.T, m Model, key string, want imagesearch.Verdict) {
	t.Helper()
	o, ok := verdictOf(t, m, key)
	if !ok {
		t.Errorf("%s has no verdict, want %q", key, want)
		return
	}
	if o.Verdict != want {
		t.Errorf("%s verdict = %q, want %q", key, o.Verdict, want)
	}
}

func rowsOf(m Model) []string {
	var out []string
	for _, r := range m.registryBrowser.searchTable.Visible() {
		out = append(out, r.img.EntryKey+":"+r.img.Repo)
	}
	return out
}

// hubServer answers every search with one result named name.
func hubServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"results":[{"name":%q,"description":"d"}]}`, name)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// c.1: a registry that never answers does not hold the others back; its
// verdict becomes failed at the deadline and the other rows do not move.
func TestASlowRegistryNeverHoldsBackTheOthers(t *testing.T) {
	withSearchDeadline(t, 150*time.Millisecond)
	fast1, fast2 := hubServer(t, "alpha"), hubServer(t, "beta")
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) }) // runs first: frees the handler before Close

	endpoints := map[string]*httptest.Server{"prod": fast1, "hub": fast2, "third": slow}
	withImageSearcher(t, imagesearch.SearcherFunc(func(ctx context.Context, tg imagesearch.Target, q imagesearch.Query) ([]imagesearch.Image, bool, error) {
		srv := endpoints[tg.EntryKey]
		return imagesearch.HubSearcher{Endpoint: srv.URL, Client: srv.Client()}.Search(ctx, tg, q)
	}))

	m, cmd := submitRaw(t, threeRegistryModel(t), "nginx")
	m = feed(t, m, testutil.Resize(200, 40))
	if m.registryBrowser.pendingSearches != 3 {
		t.Fatalf("pendingSearches = %d, want 3", m.registryBrowser.pendingSearches)
	}
	results := runCmds(t, cmd)

	first, second := recvResult(t, results), recvResult(t, results)
	if first.EntryKey == "third" || second.EntryKey == "third" {
		t.Fatalf("the slow registry answered before the fast ones: %q, %q", first.EntryKey, second.EntryKey)
	}
	m = feed(t, m, first, second)
	if got := len(rowsOf(m)); got != 2 {
		t.Fatalf("rows = %v, want the two fast registries' rows", rowsOf(m))
	}
	wantVerdict(t, m, "prod", imagesearch.VerdictOK)
	wantVerdict(t, m, "hub", imagesearch.VerdictOK)
	if _, ok := verdictOf(t, m, "third"); ok {
		t.Error("the slow registry has a verdict before it answered")
	}
	if got := m.registryBrowser.pendingSearches; got != 1 {
		t.Errorf("pendingSearches = %d, want 1", got)
	}
	before := rowsOf(m)
	selBefore, _ := m.registryBrowser.searchTable.Selected()

	third := recvResult(t, results)
	if third.EntryKey != "third" || !errors.Is(third.Err, context.DeadlineExceeded) {
		t.Fatalf("third = %q / %v, want third / deadline exceeded", third.EntryKey, third.Err)
	}
	m = feed(t, m, third)
	wantVerdict(t, m, "third", imagesearch.VerdictFailed)
	if got := rowsOf(m); !equal(got, before) {
		t.Errorf("rows moved after the deadline: %v, was %v", got, before)
	}
	if sel, _ := m.registryBrowser.searchTable.Selected(); sel.img != selBefore.img {
		t.Errorf("cursor moved: %+v, was %+v", sel.img, selBefore.img)
	}
	if got := m.registryBrowser.pendingSearches; got != 0 {
		t.Errorf("pendingSearches = %d, want 0", got)
	}
}

// c.2: an empty answer is no result: no row, no log.
func TestARegistryThatFindsNothingIsNoResult(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	logs := captureLog(t)

	m := searchLot(t, threeRegistryModel(t))
	m = answerFor(t, m, "prod", nil, false, nil)

	wantVerdict(t, m, "prod", imagesearch.VerdictNoResult)
	if o, _ := verdictOf(t, m, "prod"); o.Partial || o.Skipped {
		t.Errorf("no result carries a mark: %+v", o)
	}
	if got := rowsOf(m); len(got) != 0 {
		t.Errorf("rows = %v, want none", got)
	}
	if got := logs.String(); got != "" {
		t.Errorf("a no-result answer logged %q", got)
	}
}

// c.3: unsupported or refused: no row of that registry, the others stay, and
// the journal says it at INFO, never ERROR.
func TestRefusedRegistriesAddNoRowAndLogAtInfo(t *testing.T) {
	errs := map[string]error{
		"unsupported": imagesearch.ErrUnsupported,
		"401":         &oci.StatusError{Code: 401, Source: oci.SourceRegistry},
		"403":         &oci.StatusError{Code: 403, Source: oci.SourceRegistry},
		"404":         &oci.StatusError{Code: 404, Source: oci.SourceRegistry},
		"405":         &oci.StatusError{Code: 405, Source: oci.SourceRegistry},
		"wrapped 403": fmt.Errorf("hub search: %w", &oci.StatusError{Code: 403, Source: oci.SourceRegistry}),
	}
	for name, cause := range errs {
		t.Run(name, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			logs := captureLog(t)

			m := searchLot(t, threeRegistryModel(t))
			m = answerFor(t, m, "hub", oneImage("alpha"), false, nil)
			m = answerFor(t, m, "prod", nil, false, cause)

			wantVerdict(t, m, "prod", imagesearch.VerdictRefused)
			wantVerdict(t, m, "hub", imagesearch.VerdictOK)
			if got := rowsOf(m); !equal(got, []string{"hub:library/alpha"}) {
				t.Errorf("rows = %v, want only the other registry's", got)
			}
			out := logs.String()
			if !strings.Contains(out, "INFO [oci_resources] image search on prod: unsupported or refused: ") {
				t.Errorf("no INFO line for the refusal: %q", out)
			}
			if strings.Contains(out, "ERROR") {
				t.Errorf("a refusal was logged as an error: %q", out)
			}
		})
	}
}

// c.3: dhi.io is refused without any request, and a name with a "/" still
// opens its tags there.
func TestDHIEntryIsRefusedWithoutARequest(t *testing.T) {
	var hubCalls, otherCalls atomic.Int32
	count := func(n *atomic.Int32) imagesearch.Searcher {
		return imagesearch.SearcherFunc(func(context.Context, imagesearch.Target, imagesearch.Query) ([]imagesearch.Image, bool, error) {
			n.Add(1)
			return oneImage("x"), false, nil
		})
	}
	withImageSearcher(t, imagesearch.ByRegistry(count(&hubCalls), count(&otherCalls)))
	tags := withTagRecorder(t)
	dhi := anonymousRegistry("dhi.io", "dhi")

	m, cmd := submitRaw(t, registryModel(t, dhi), "nginx")
	// A batch of one is the Cmd itself, not a BatchMsg.
	res, ok := cmd().(ImageSearchResultMsg)
	if !ok {
		t.Fatal("the dhi.io search did not answer with an ImageSearchResultMsg")
	}
	m = feed(t, m, res)
	wantVerdict(t, m, "dhi", imagesearch.VerdictRefused)
	if hubCalls.Load() != 0 || otherCalls.Load() != 0 {
		t.Errorf("dhi.io was asked: hub %d, other %d", hubCalls.Load(), otherCalls.Load())
	}

	m2, cmd2 := submitRaw(t, registryModel(t, dhi), "dhi/nginx")
	if m2.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want browserStateTags", m2.registryBrowser.state)
	}
	testutil.Msgs(cmd2)
	if got := tags.sorted(); len(got) != 1 {
		t.Errorf("tag listings = %v, want one on dhi.io", got)
	}
}

// c.4: a failure adds no row, is logged at ERROR with the cause only, and the
// journal never holds a password, a token, an Authorization header or the
// text the user typed.
func TestFailedRegistriesAreLoggedWithoutSecrets(t *testing.T) {
	leaky := &url.Error{
		Op:  "Get",
		URL: "https://alice:s3cr3t-pw@reg.example.com/v1/search?q=nginx&token=tok-123",
		Err: errors.New("Authorization: Bearer tok-456"),
	}
	causes := map[string]error{
		"deadline":  context.DeadlineExceeded,
		"429":       &oci.StatusError{Code: 429, Source: oci.SourceRegistry},
		"500":       &oci.StatusError{Code: 500, Source: oci.SourceRegistry},
		"503":       &oci.StatusError{Code: 503, Source: oci.SourceRegistry},
		"bad body":  fmt.Errorf("hub search: decode: %w", errors.New("invalid character 'n'")),
		"url error": leaky,
	}
	for name, cause := range causes {
		t.Run(name, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			logs := captureLog(t)

			m := searchLot(t, threeRegistryModel(t))
			m = answerFor(t, m, "prod", nil, false, cause)

			wantVerdict(t, m, "prod", imagesearch.VerdictFailed)
			if got := rowsOf(m); len(got) != 0 {
				t.Errorf("rows = %v, want none", got)
			}
			out := logs.String()
			if !strings.Contains(out, "ERROR [oci_resources] image search on prod:") {
				t.Errorf("no ERROR line for the failure: %q", out)
			}
			for _, secret := range []string{"s3cr3t-pw", "tok-123", "tok-456", "alice:", "q=nginx"} {
				if strings.Contains(out, secret) {
					t.Errorf("the journal holds %q: %q", secret, out)
				}
			}
		})
	}
}

// c.5: every answer gets exactly one verdict; partial only marks an ok, and an
// error wins over partial.
func TestEachAnswerGetsExactlyOneVerdict(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, threeRegistryModel(t))
	m = answerFor(t, m, "prod", oneImage("alpha"), true, nil)
	m = answerFor(t, m, "hub", nil, false, nil)
	m = answerFor(t, m, "third", nil, true, errors.New("boom"))

	got := m.RegistryBrowserOutcomes()
	if len(got) != 3 {
		t.Fatalf("outcomes = %+v, want 3 entries", got)
	}
	want := map[string]imagesearch.Outcome{
		"prod":  {Verdict: imagesearch.VerdictOK, Partial: true},
		"hub":   {Verdict: imagesearch.VerdictNoResult},
		"third": {Verdict: imagesearch.VerdictFailed},
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s outcome = %+v, want %+v", key, got[key], w)
		}
	}
	// A copy: writing to it changes nothing in the browser.
	got["prod"] = imagesearch.Outcome{Verdict: imagesearch.VerdictFailed}
	if o, _ := verdictOf(t, m, "prod"); o.Verdict != imagesearch.VerdictOK {
		t.Error("RegistryBrowserOutcomes returned the live map, not a copy")
	}
}

func TestOutcomesAreNilWhenTheBrowserIsClosed(t *testing.T) {
	if got := loadedModel(t).RegistryBrowserOutcomes(); got != nil {
		t.Errorf("outcomes with no browser = %+v, want nil", got)
	}
}

// c.6: no err.Error() text reaches the screen: not the body, not the footer,
// not the header, whatever the verdict; the body stays the table (Rule 139)
// and no footer message is set.
func TestNoErrorTextReachesTheScreen(t *testing.T) {
	const marker = "MARKER-7f3a"
	causes := map[string]error{
		"refused":     fmt.Errorf("%s: %w", marker, imagesearch.ErrUnsupported),
		"refused 403": fmt.Errorf("%s: %w", marker, &oci.StatusError{Code: 403}),
		"failed":      errors.New(marker + " connection reset"),
		"deadline":    fmt.Errorf("%s: %w", marker, context.DeadlineExceeded),
	}
	for name, cause := range causes {
		t.Run(name, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			captureLog(t)

			m := searchLot(t, threeRegistryModel(t))
			m = feed(t, m, testutil.Resize(160, 40))
			m = answerFor(t, m, "hub", oneImage("alpha"), false, nil)
			m, cmd := step(t, m, ImageSearchResultMsg{
				Gen: m.registryBrowser.gen, EntryKey: "prod", Err: cause,
			})
			if cmd != nil {
				t.Errorf("a %s answer returned a Cmd: no footer message is wanted", name)
			}
			wantVerdict(t, m, "prod", map[bool]imagesearch.Verdict{
				true: imagesearch.VerdictRefused, false: imagesearch.VerdictFailed,
			}[strings.HasPrefix(name, "refused")])

			screen := ansi.Strip(m.Render())
			if !strings.Contains(screen, "Name") {
				t.Errorf("the body is not the table any more: %q", screen)
			}
			texts := []string{screen, ansi.Strip(m.RenderFooter(120))}
			for _, info := range m.GetHeaderInfo("") {
				texts = append(texts, info.Key, info.Value)
			}
			for _, s := range texts {
				if strings.Contains(s, marker) {
					t.Errorf("error text on screen: %q", s)
				}
			}
		})
	}
}

// A stale answer (another lot) records no verdict and logs nothing.
func TestAStaleAnswerRecordsNoVerdict(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	logs := captureLog(t)

	m := searchLot(t, threeRegistryModel(t))
	old := m.registryBrowser.gen
	m = feed(t, m, ImageSearchResultMsg{Gen: old + 1, EntryKey: "prod", Images: oneImage("alpha")})
	m = feed(t, m, ImageSearchResultMsg{Gen: old - 1, EntryKey: "hub", Err: errors.New("late")})

	if got := m.RegistryBrowserOutcomes(); len(got) != 0 {
		t.Errorf("stale answers left verdicts: %+v", got)
	}
	if got := rowsOf(m); len(got) != 0 {
		t.Errorf("stale answers left rows: %v", got)
	}
	if got := logs.String(); got != "" {
		t.Errorf("stale answers logged %q", got)
	}
}

// H7: a new search starts from no verdict at all.
func TestANewSearchClearsTheVerdicts(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, threeRegistryModel(t))
	m = answerFor(t, m, "prod", oneImage("alpha"), false, nil)
	m = answerFor(t, m, "hub", nil, false, errors.New("boom"))
	wantVerdict(t, m, "prod", imagesearch.VerdictOK)
	wantVerdict(t, m, "hub", imagesearch.VerdictFailed)

	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateInput {
		t.Fatalf("esc did not bring the form back (state %d)", m.registryBrowser.state)
	}
	m, _ = step(t, m, testutil.Key("enter"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("the second search did not start (state %d)", m.registryBrowser.state)
	}
	if got := m.RegistryBrowserOutcomes(); len(got) != 0 {
		t.Errorf("verdicts of the previous search survive: %+v", got)
	}
	m = answerFor(t, m, "third", nil, false, nil)
	if got := m.RegistryBrowserOutcomes(); len(got) != 1 {
		t.Errorf("outcomes = %+v, want only the new answer", got)
	}
	wantVerdict(t, m, "third", imagesearch.VerdictNoResult)
}
