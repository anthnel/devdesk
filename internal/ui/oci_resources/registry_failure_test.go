package ociresources

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-629 (F1, H1): a refused token realm shows at the footer, level Error, as
// "token realm on another host", in the three interactive flows of the registry
// browser. The realm's host stays in the journal: the footer carries the reason only.

// offOriginErr is what the client returns: the sentinel, wrapped, with the host.
func offOriginErr() error {
	return fmt.Errorf("registry tags: %w", fmt.Errorf("%w (auth.example)", oci.ErrTokenRealmOffOrigin))
}

// The labels of a rate limit, one per way the request identified itself.
const (
	reasonHubAnonymousRateLimit = "Docker Hub anonymous rate limit reached, try again later"
	reasonHubLoginRateLimit     = "Docker Hub rate limit reached, try again later"
	reasonOtherRateLimit        = "rate limit reached (HTTP 429), try again later"
)

// netFailure builds what the HTTP client returns for a failed connection: a *url.Error that
// holds the full request URL (secrets and typed query included) around the *net.OpError.
func netFailure(inner error) error {
	return &url.Error{
		Op: "Get", URL: "https://bob:s3cretpw@index.docker.io/v1/search?n=50&q=nginx",
		Err: inner,
	}
}

func opError(op string, inner error) *net.OpError {
	return &net.OpError{Op: op, Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, Err: inner}
}

// everyAccess runs check for the three ways a request can identify itself.
var everyAccess = []struct {
	name   string
	access registryAccess
}{
	{"other registry", accessOther},
	{"hub anonymous", accessHubAnonymous},
	{"hub login", accessHubLogin},
}

func TestRegistryFailureReason(t *testing.T) {
	refused := errors.New("connect: connection refused")
	noAnswer := &oci.NoAnswerError{After: oci.RequestTimeout}
	for _, tt := range []struct {
		name string
		err  error
		want string // the same for every access
	}{
		{"nil", nil, ""},
		{"the sentinel", oci.ErrTokenRealmOffOrigin, "token realm on another host"},
		{"the sentinel, wrapped", offOriginErr(), "token realm on another host"},
		{"a 404 from the registry", &oci.StatusError{Code: 404, Source: oci.SourceRegistry}, ""},
		{"a 401 from the registry", &oci.StatusError{Code: 401, Source: oci.SourceRegistry}, ""},
		{"a 403 from the token endpoint", &oci.StatusError{Code: 403, Source: oci.SourceToken}, ""},
		{"a 500 from the registry", &oci.StatusError{Code: 500, Source: oci.SourceRegistry}, ""},
		{"a 503 from the registry", &oci.StatusError{Code: 503, Source: oci.SourceRegistry}, ""},
		{"a blocked upstream", blockedUpstreamErr(t), "the proxy's upstream is temporarily blocked"},
		{"a blocked upstream, wrapped", fmt.Errorf("registry tags: %w", blockedUpstreamErr(t)), "the proxy's upstream is temporarily blocked"},
		{"a 404 that is not blocked", notBlocked404Err(t), ""},
		{"a plain error", errors.New("boom"), ""},
		{"a bare deadline", fmt.Errorf("hub search: %w", context.DeadlineExceeded), ""},
		{"no answer", noAnswer, noAnswer.Error()},
		{"no answer, wrapped", fmt.Errorf("hub search: %w", noAnswer), noAnswer.Error()},
		// A connection that timed out is also a *net.OpError: the no-answer reading wins.
		{"no answer that is also a net error", fmt.Errorf("%w: %w", noAnswer, opError("dial", refused)), noAnswer.Error()},
		{"a bare cancellation", context.Canceled, ""},
		{"a cancellation in a url error", netFailure(context.Canceled), ""},
		{"a cancellation, wrapped", fmt.Errorf("hub search: %w", netFailure(context.Canceled)), ""},
		// A cancellation inside a net error is still the user leaving.
		{"a cancellation in a net error", netFailure(opError("dial", context.Canceled)), ""},
		{"a refused proxy", netFailure(opError("proxyconnect", refused)), "could not reach the proxy"},
		{"a refused proxy, wrapped", fmt.Errorf("hub search: %w", netFailure(opError("proxyconnect", refused))), "could not reach the proxy"},
		{"a refused connection", netFailure(opError("dial", refused)), "could not reach the registry"},
		{"a reset connection", netFailure(opError("read", errors.New("connection reset by peer"))), "could not reach the registry"},
		{"a refused connection, wrapped", fmt.Errorf("registry tags: %w", netFailure(opError("dial", refused))), "could not reach the registry"},
		{"a net error with no url error", opError("dial", refused), "could not reach the registry"},
		// What the dialer really returns for an unknown host: an *OpError around the *DNSError.
		{"an unresolvable host", netFailure(opError("dial", &net.DNSError{Err: "no such host", Name: "index.docker.io", IsNotFound: true})), "could not resolve the registry host"},
		{"an unresolvable host, bare", &net.DNSError{Err: "no such host", Name: "index.docker.io", IsNotFound: true}, "could not resolve the registry host"},
		{"a DNS timeout", netFailure(opError("dial", &net.DNSError{Err: "i/o timeout", Name: "index.docker.io", IsTimeout: true})), "could not resolve the registry host"},
	} {
		for _, acc := range everyAccess {
			t.Run(tt.name+"/"+acc.name, func(t *testing.T) {
				got := registryFailureReason(tt.err, acc.access)
				if got != tt.want {
					t.Errorf("registryFailureReason(%v, %s) = %q, want %q", tt.err, acc.name, got, tt.want)
				}
				// The reason is a constant: nothing of the request or the host comes with it.
				for _, leak := range []string{"index.docker.io", "127.0.0.1", "s3cretpw", "bob", "nginx", "q=", "http"} {
					if strings.Contains(got, leak) {
						t.Errorf("reason %q carries %q", got, leak)
					}
				}
			})
		}
	}
}

// A 429 is a rate limit, worded by who was asked and how they were identified: the
// registry's answer and the token endpoint's are the same limit.
func TestRegistryFailureReasonRecognisesARateLimit(t *testing.T) {
	want := map[string]string{
		"other registry": reasonOtherRateLimit,
		"hub anonymous":  reasonHubAnonymousRateLimit,
		"hub login":      reasonHubLoginRateLimit,
	}
	for _, src := range []struct {
		name   string
		source oci.StatusSource
	}{
		{"the registry", oci.SourceRegistry},
		{"the token endpoint", oci.SourceToken},
	} {
		for _, wrapped := range []bool{false, true} {
			for _, acc := range everyAccess {
				name := src.name + "/" + acc.name
				if wrapped {
					name += "/wrapped"
				}
				t.Run(name, func(t *testing.T) {
					var err error = &oci.StatusError{Code: 429, Source: src.source}
					if wrapped {
						err = fmt.Errorf("hub search: %w", err)
					}
					if got := registryFailureReason(err, acc.access); got != want[acc.name] {
						t.Errorf("registryFailureReason(429 from %s, %s) = %q, want %q", src.name, acc.name, got, want[acc.name])
					}
				})
			}
		}
	}
}

// The reasons that do not depend on the access are the same under each of them.
func TestRegistryFailureReasonSentinelsIgnoreTheAccess(t *testing.T) {
	for _, acc := range everyAccess {
		if got := registryFailureReason(offOriginErr(), acc.access); got != "token realm on another host" {
			t.Errorf("%s: realm reason = %q", acc.name, got)
		}
		if got := registryFailureReason(blockedUpstreamErr(t), acc.access); got != "the proxy's upstream is temporarily blocked" {
			t.Errorf("%s: blocked upstream reason = %q", acc.name, got)
		}
	}
}

// Tags opened from level 1 (→): the generic text gains the reason.
func TestTagsFooterNamesAnOffOriginTokenRealmFromSearch(t *testing.T) {
	m := afterRight(t)
	if !m.registryBrowser.tagsFromSearch {
		t.Fatal("tagsFromSearch is false after →")
	}

	m, cmd := step(t, m, failedTags(m, offOriginErr()))

	want := tagsLoadFailedText + ": token realm on another host"
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	if cmd == nil {
		t.Error("no Cmd: the footer message has no timer")
	}
}

// Tags opened from the form (the full name flow): the footer says it could not load them,
// with the reason when there is one, exactly as for → (DEVDESK-667, G2). It used to say
// nothing but for a refused token realm, and that one had no prefix.
func TestTagsFooterNamesAnOffOriginTokenRealmFromTheForm(t *testing.T) {
	setup := func(t *testing.T) Model {
		captureLog(t)
		withFullTagRecorder(t)
		withImageSearcher(t, &fakeSearcher{})
		m := typeInto(t, browsingModel(t), "library/nginx")
		m.registryBrowser.submit(actionOpenTags)
		if m.registryBrowser.tagsFromSearch {
			t.Fatal("the full name flow is marked as coming from level 1")
		}
		return m
	}

	t.Run("off-origin realm", func(t *testing.T) {
		m := setup(t)
		m, cmd := step(t, m, failedTags(m, offOriginErr()))
		want := tagsLoadFailedText + ": token realm on another host"
		if m.footer.Level() != components.LevelError || m.footer.Text() != want {
			t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
		}
		if cmd == nil {
			t.Error("no Cmd: the footer message has no timer")
		}
	})

	t.Run("an unknown cause still says tags could not load", func(t *testing.T) {
		m := setup(t)
		m, _ = step(t, m, failedTags(m, errors.New("boom")))
		if m.footer.Level() != components.LevelError || m.footer.Text() != tagsLoadFailedText {
			t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), tagsLoadFailedText)
		}
	})
}

// The footer carries the reason only: never the realm's host, a path or a secret.
func TestOffOriginFooterCarriesNoHost(t *testing.T) {
	m := afterRight(t)
	m, _ = step(t, m, failedTags(m, offOriginErr()))
	if strings.Contains(m.footer.Text(), "auth.example") {
		t.Errorf("footer = %q names the realm's host", m.footer.Text())
	}
}

// Image search: the registry's answer is failed, and the footer says why.
func TestImageSearchFooterNamesAnOffOriginTokenRealm(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	captureLog(t)
	m, _ := submitRaw(t, browsingModel(t), "nginx")

	m, cmd := step(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "prod", Err: offOriginErr()})

	wantVerdict(t, m, "prod", imagesearch.VerdictFailed)
	want := "Image search on prod: token realm on another host"
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	if cmd == nil {
		t.Error("no Cmd: the footer message has no timer")
	}
}

// Any other failed answer keeps today's silence at the footer.
func TestImageSearchFooterStaysSilentForOtherFailures(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	captureLog(t)
	m, _ := submitRaw(t, browsingModel(t), "nginx")

	m, _ = step(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "prod", Err: errors.New("connection reset")})

	if m.footer.IsSet() {
		t.Errorf("footer = %q, want none", m.footer.Text())
	}
}

// DEVDESK-629 (F3): a Nexus proxy whose upstream is auto-blocked answers 404 "Remote Auto
// Blocked". Only the real client can build that StatusError, so these tests read it from a
// registry that answers it.

func registryAnswering404(t *testing.T, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	_, err := oci.ListRegistryTagsContext(context.Background(), srv.URL, "library/nginx", "", "")
	return err
}

func blockedUpstreamErr(t *testing.T) error {
	t.Helper()
	return registryAnswering404(t, `{"errors":[{"code":"UNKNOWN","message":"Remote Auto Blocked until 2026-11-20T14:24:21.987Z"}]}`)
}

func notBlocked404Err(t *testing.T) error {
	t.Helper()
	return registryAnswering404(t, `{"errors":[{"message":"Repository not found"}]}`)
}

// Tags opened from level 1 (right): the footer says the upstream is temporarily blocked,
// with no date and no duration.
func TestTagsFooterSaysTheUpstreamIsTemporarilyBlocked(t *testing.T) {
	m := afterRight(t)

	m, cmd := step(t, m, failedTags(m, blockedUpstreamErr(t)))

	want := tagsLoadFailedText + ": the proxy's upstream is temporarily blocked"
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	for _, leak := range []string{"until", "2026", "Remote Auto Blocked"} {
		if strings.Contains(m.footer.Text(), leak) {
			t.Errorf("footer = %q carries %q from the body", m.footer.Text(), leak)
		}
	}
	if cmd == nil {
		t.Error("no Cmd: the footer message has no timer")
	}
}

// A 404 that is not the blocked message adds no reason at the footer.
func TestTagsFooterKeepsItsGenericTextOnAnOrdinary404(t *testing.T) {
	m := afterRight(t)

	m, _ = step(t, m, failedTags(m, notBlocked404Err(t)))

	if strings.Contains(m.footer.Text(), "blocked") {
		t.Errorf("footer = %q, want no blocked reason on an ordinary 404", m.footer.Text())
	}
}
