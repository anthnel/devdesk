package ociresources

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-667 (S-04b), criteria a to e: a failed Docker Hub search or tag listing says why at
// the footer, level Error, in the three words the user can act on (the network, the proxy,
// a rate limit), and a failure that has no word of its own changes nothing it did before.
// No test reaches a real Hub: the answers are built by hand, or come from a local server
// or a RoundTripper that fails.

// hubAnonymous and hubWithLogin are the one entry of a context, "hub" at docker.io, reached
// without and with credentials.
func hubAnonymous() config.RegistryItem { return anonymousRegistry("docker.io", "hub") }

func hubWithLogin() config.RegistryItem {
	return config.RegistryItem{URL: "docker.io", Alias: "hub", Slug: "hub", Username: "alice", AuthMode: config.AuthCredentials}
}

func otherRegistry() config.RegistryItem { return anonymousRegistry("registry.example.com", "prod") }

// isolateDockerConfig keeps the credential lookup of a credentialed entry off the host.
func isolateDockerConfig(t *testing.T) {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
}

// failureOf is one way a request fails, as the HTTP client reports it.
type failureOf struct {
	name string
	err  error
	// reason is what the footer adds after its prefix, "" for a failure with no word of its own.
	reason func(a registryAccess) string
}

func constant(s string) func(registryAccess) string { return func(registryAccess) string { return s } }

func rateLimitReason(a registryAccess) string {
	switch a {
	case accessHubAnonymous:
		return reasonHubAnonymousRateLimit
	case accessHubLogin:
		return reasonHubLoginRateLimit
	}
	return reasonOtherRateLimit
}

func failures() []failureOf {
	refused := errors.New("connect: connection refused")
	return []failureOf{
		{"a 429 from the registry", &oci.StatusError{Code: 429, Source: oci.SourceRegistry}, rateLimitReason},
		{"a 429 from the token endpoint", &oci.StatusError{Code: 429, Source: oci.SourceToken}, rateLimitReason},
		{"a refused proxy", netFailure(opError("proxyconnect", refused)), constant("could not reach the proxy")},
		{"a refused connection", netFailure(opError("dial", refused)), constant("could not reach the registry")},
		{"an unresolvable host", netFailure(opError("dial", &net.DNSError{Err: "no such host", Name: "index.docker.io", IsNotFound: true})), constant("could not resolve the registry host")},
		{"no answer", &oci.NoAnswerError{After: oci.RequestTimeout}, constant(noAnswerText())},
		{"a 500", &oci.StatusError{Code: 500, Source: oci.SourceRegistry}, constant("")},
		{"a 503", &oci.StatusError{Code: 503, Source: oci.SourceRegistry}, constant("")},
		{"an unknown error", errors.New("boom"), constant("")},
	}
}

// wantNoRequestDetail checks the footer text carries nothing of the request.
func wantNoRequestDetail(t *testing.T, text string) {
	t.Helper()
	for _, leak := range []string{"index.docker.io", "registry-1.docker.io", "registry.example.com", "127.0.0.1", "s3cretpw", "alice", "nginx", "q=", "http", "boom"} {
		if strings.Contains(text, leak) {
			t.Errorf("footer = %q carries %q", text, leak)
		}
	}
}

// failedTagsOn is the failed answer of the tag listing of entry (key, URL, alias).
func failedTagsOn(m Model, key, regURL, alias string, err error) MultiRegistryTagsLoadedMsg {
	return MultiRegistryTagsLoadedMsg{Gen: m.registryBrowser.gen, EntryKey: key, RegistryURL: regURL, Alias: alias, Repo: "library/nginx", Err: err}
}

// formTagsModel is the browser on one entry, the name typed and Open tags pressed: the full
// name flow, which is the one G2 found silent.
func formTagsModel(t *testing.T, item config.RegistryItem) Model {
	t.Helper()
	isolateDockerConfig(t)
	captureLog(t)
	withFullTagRecorder(t)
	withImageSearcher(t, &fakeSearcher{})
	m := typeInto(t, registryModel(t, item), "library/nginx")
	m.registryBrowser.submit(actionOpenTags)
	if m.registryBrowser.state != browserStateTags || m.registryBrowser.tagsFromSearch {
		t.Fatalf("state %d, tagsFromSearch %v: not the full name flow", m.registryBrowser.state, m.registryBrowser.tagsFromSearch)
	}
	return m
}

// wantTagTableKept is Rule 139: the tag table stays a table, its header and no rows.
func wantTagTableKept(t *testing.T, m Model) {
	t.Helper()
	if got := len(m.registryBrowser.tagTable.Items()); got != 0 {
		t.Errorf("tag rows = %d, want 0", got)
	}
	view := ansi.Strip(m.registryBrowser.View())
	for _, col := range []string{"Registry", "Tag"} {
		if !strings.Contains(view, col) {
			t.Errorf("the table lost its %q header (Rule 139):\n%s", col, view)
		}
	}
}

// Criteria b and c: a listing opened from the form that fails says why, with the wording the
// way the entry identified itself gives it; no cause of its own keeps the generic text.
func TestTagsFooterFromTheFormNamesTheCause(t *testing.T) {
	for _, entry := range []struct {
		name   string
		item   config.RegistryItem
		access registryAccess
	}{
		{"hub anonymous", hubAnonymous(), accessHubAnonymous},
		{"hub with login", hubWithLogin(), accessHubLogin},
		{"other registry", otherRegistry(), accessOther},
	} {
		for _, f := range failures() {
			t.Run(entry.name+"/"+f.name, func(t *testing.T) {
				m := formTagsModel(t, entry.item)
				key := entry.item.Slug

				m, cmd := step(t, m, failedTagsOn(m, key, entry.item.URL, entry.item.Alias, f.err))

				want := tagsLoadFailedText
				if reason := f.reason(entry.access); reason != "" {
					want += ": " + reason
				}
				if m.footer.Level() != components.LevelError || m.footer.Text() != want {
					t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
				}
				if cmd == nil {
					t.Error("no Cmd: the footer message has no timer")
				}
				wantNoRequestDetail(t, m.footer.Text())
				wantTagTableKept(t, m)
				if m.registryBrowser.state != browserStateTags {
					t.Errorf("state = %d, want browserStateTags", m.registryBrowser.state)
				}
			})
		}
	}
}

// Criterion b, from level 1 (→): the same wording, on the same accesses.
func TestTagsFooterFromRightNamesARateLimit(t *testing.T) {
	m := afterRight(t)

	m, cmd := step(t, m, failedTags(m, &oci.StatusError{Code: 429, Source: oci.SourceRegistry}))

	want := tagsLoadFailedText + ": " + reasonHubAnonymousRateLimit
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	if cmd == nil {
		t.Error("no Cmd: the footer message has no timer")
	}
	wantTagTableKept(t, m)
}

func TestTagsFooterFromRightNamesTheNetwork(t *testing.T) {
	m := afterRight(t)

	m, _ = step(t, m, failedTags(m, netFailure(opError("dial", errors.New("connect: connection refused")))))

	if want := tagsLoadFailedText + ": could not reach the registry"; m.footer.Text() != want {
		t.Errorf("footer = %q, want %q", m.footer.Text(), want)
	}
	wantNoRequestDetail(t, m.footer.Text())
}

// The 429 the real client builds, from a local server, is the one the footer words.
func TestTagsFooterFromTheFormNamesARealRateLimit(t *testing.T) {
	var tokenLimited atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.WriteHeader(http.StatusTooManyRequests)
		case tokenLimited.Load():
			w.Header().Set("Www-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="reg"`)
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	t.Cleanup(srv.Close)

	for _, from := range []string{"the registry", "the token endpoint"} {
		t.Run(from, func(t *testing.T) {
			tokenLimited.Store(from == "the token endpoint")
			_, err := oci.ListRegistryTagsContext(context.Background(), srv.URL, "library/nginx", "", "")
			if err == nil {
				t.Fatal("the rate limited registry listed tags")
			}
			m := formTagsModel(t, hubAnonymous())

			m, _ = step(t, m, failedTagsOn(m, "hub", "docker.io", "hub", err))

			if want := tagsLoadFailedText + ": " + reasonHubAnonymousRateLimit; m.footer.Text() != want {
				t.Errorf("footer = %q, want %q", m.footer.Text(), want)
			}
			wantNoRequestDetail(t, m.footer.Text())
		})
	}
}

// Criterion d: the footer message expires like any other, and a failure retries nothing:
// the only message its Cmd carries is the one that clears the footer.
func TestAFailedFormListingRetriesNothingAndExpires(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m := formTagsModel(t, hubAnonymous())
	rec := withFullTagRecorder(t)

	m, cmd := step(t, m, failedTagsOn(m, "hub", "docker.io", "hub", &oci.StatusError{Code: 429, Source: oci.SourceRegistry}))

	msgs := testutil.Msgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("the failure returned %d messages (%T...), want the footer's one", len(msgs), msgs)
	}
	clear, ok := msgs[0].(components.ClearFooterMsg)
	if !ok {
		t.Fatalf("the failure's Cmd answered %T, want components.ClearFooterMsg", msgs[0])
	}
	if got := len(rec.sorted()); got != 0 {
		t.Errorf("%d tag requests after the failure, want none (no retry)", got)
	}
	m = feed(t, m, clear)
	if m.footer.IsSet() {
		t.Errorf("footer still shows %q", m.footer.Text())
	}
}

// searchedHubModel is the browser on one entry with a search started, ready for its answer.
func searchedHubModel(t *testing.T, item config.RegistryItem) Model {
	t.Helper()
	isolateDockerConfig(t)
	captureLog(t)
	withImageSearcher(t, &fakeSearcher{})
	m, _ := submitRaw(t, registryModel(t, item), "nginx")
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want browserStateImages", m.registryBrowser.state)
	}
	return m
}

// wantLevelOneTableKept is Rule 139 at level 1: a header and no row.
func wantLevelOneTableKept(t *testing.T, m Model) {
	t.Helper()
	if got := len(m.registryBrowser.searchTable.Visible()); got != 0 {
		t.Errorf("level 1 rows = %d, want 0", got)
	}
	if screen := ansi.Strip(m.Render()); !strings.Contains(screen, "Name") {
		t.Errorf("the body is not the table any more: %q", screen)
	}
}

// Criteria a and c at level 1: a failed search says why at the footer, level Error. Docker
// Hub's search is anonymous whatever the entry's credentials, so its rate limit is the
// anonymous one even for an entry with a login.
func TestImageSearchFooterNamesTheCause(t *testing.T) {
	for _, entry := range []struct {
		name   string
		item   config.RegistryItem
		access registryAccess
	}{
		{"hub anonymous", hubAnonymous(), accessHubAnonymous},
		{"hub with login", hubWithLogin(), accessHubAnonymous},
		{"other registry", otherRegistry(), accessOther},
	} {
		for _, f := range failures() {
			t.Run(entry.name+"/"+f.name, func(t *testing.T) {
				m := searchedHubModel(t, entry.item)
				key := entry.item.Slug

				m, cmd := step(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: key, Err: f.err})

				wantVerdict(t, m, key, imagesearch.VerdictFailed)
				wantLevelOneTableKept(t, m)
				reason := f.reason(entry.access)
				if reason == "" {
					// Unchanged: a failure with no word of its own adds nothing at level 1.
					if m.footer.IsSet() {
						t.Errorf("footer = %q, want none", m.footer.Text())
					}
					return
				}
				want := "Image search on " + entry.item.Alias + ": " + reason
				if m.footer.Level() != components.LevelError || m.footer.Text() != want {
					t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
				}
				if cmd == nil {
					t.Error("no Cmd: the footer message has no timer")
				}
				wantNoRequestDetail(t, m.footer.Text())
			})
		}
	}
}

// Criterion d: the statuses the browser already knew are unchanged, at both levels: a refusal
// stays silent at level 1 and the table is not emptied of its header.
func TestKnownStatusesStaySilentAtLevelOne(t *testing.T) {
	for _, code := range []int{401, 403, 404, 405} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			m := searchedHubModel(t, hubAnonymous())

			m, _ = step(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "hub",
				Err: &oci.StatusError{Code: code, Source: oci.SourceRegistry}})

			if m.footer.IsSet() {
				t.Errorf("footer = %q, want none", m.footer.Text())
			}
			wantLevelOneTableKept(t, m)
		})
	}
}

// searchOnHub runs the production Hub searcher, pointed at endpoint with client, on a context
// whose only entry is an anonymous docker.io, and returns the model after its answer.
func searchOnHub(t *testing.T, endpoint string, client *http.Client) (Model, ImageSearchResultMsg) {
	t.Helper()
	isolateDockerConfig(t)
	captureLog(t)
	withImageSearcher(t, imagesearch.ByRegistry(
		imagesearch.HubSearcher{Endpoint: endpoint, Client: client}, imagesearch.CatalogSearcher{}))
	m, cmd := submitRaw(t, registryModel(t, hubAnonymous()), "nginx")
	res, ok := cmd().(ImageSearchResultMsg)
	if !ok {
		t.Fatalf("the search answered %T, want an ImageSearchResultMsg", cmd())
	}
	m, _ = step(t, m, res)
	return m, res
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Criterion a, end to end (G1): a Hub that does not answer says so at the footer.
func TestAHubThatDoesNotAnswerIsNamedAtTheFooter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	m, _ := searchOnHub(t, srv.URL, &http.Client{Timeout: 50 * time.Millisecond})

	wantVerdict(t, m, "hub", imagesearch.VerdictFailed)
	want := "Image search on hub: " + noAnswerText()
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	wantNoRequestDetail(t, m.footer.Text())
	wantLevelOneTableKept(t, m)
	if got := hits.Load(); got != 1 {
		t.Errorf("Hub requests = %d, want 1 (no retry)", got)
	}
}

// Criterion a, end to end (G1): the machine's proxy refuses the connection.
func TestAHubBehindARefusingProxyIsNamedAtTheFooter(t *testing.T) {
	var hits atomic.Int32
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		hits.Add(1)
		return nil, opError("proxyconnect", errors.New("connect: connection refused"))
	})

	m, res := searchOnHub(t, "https://hub.invalid/v1/search", &http.Client{Transport: rt})

	var ue *url.Error
	if !errors.As(res.Err, &ue) {
		t.Fatalf("the answer's error is %T, want a url error inside it", res.Err)
	}
	wantVerdict(t, m, "hub", imagesearch.VerdictFailed)
	want := "Image search on hub: could not reach the proxy"
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	if strings.Contains(m.footer.Text(), "hub.invalid") {
		t.Errorf("footer = %q names the host", m.footer.Text())
	}
	wantNoRequestDetail(t, m.footer.Text())
	wantLevelOneTableKept(t, m)
	if got := hits.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry)", got)
	}
}

// Criterion c, end to end: a Hub that answers 429 is a rate limit, asked once.
func TestAHubThatAnswers429IsARateLimitAtTheFooter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	m, _ := searchOnHub(t, srv.URL, srv.Client())

	wantVerdict(t, m, "hub", imagesearch.VerdictFailed)
	want := "Image search on hub: " + reasonHubAnonymousRateLimit
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	wantNoRequestDetail(t, m.footer.Text())
	wantLevelOneTableKept(t, m)
	if got := hits.Load(); got != 1 {
		t.Errorf("Hub requests = %d, want 1 (no retry)", got)
	}
}

// Criterion d, end to end: a Hub that answers 500 changes nothing: no footer, a failed
// verdict, the table kept.
func TestAHubThatAnswers500StaysAsItWas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	m, _ := searchOnHub(t, srv.URL, srv.Client())

	wantVerdict(t, m, "hub", imagesearch.VerdictFailed)
	if m.footer.IsSet() {
		t.Errorf("footer = %q, want none", m.footer.Text())
	}
	wantLevelOneTableKept(t, m)
}
