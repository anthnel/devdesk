package ociresources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestRegistryFailureReason(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"the sentinel", oci.ErrTokenRealmOffOrigin, "token realm on another host"},
		{"the sentinel, wrapped", offOriginErr(), "token realm on another host"},
		{"a 404 from the registry", &oci.StatusError{Code: 404, Source: oci.SourceRegistry}, ""},
		{"a blocked upstream", blockedUpstreamErr(t), "the proxy's upstream is temporarily blocked"},
		{"a blocked upstream, wrapped", fmt.Errorf("registry tags: %w", blockedUpstreamErr(t)), "the proxy's upstream is temporarily blocked"},
		{"a 404 that is not blocked", notBlocked404Err(t), ""},
		{"a plain error", errors.New("boom"), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := registryFailureReason(tt.err); got != tt.want {
				t.Errorf("registryFailureReason(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
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

// Tags opened from the form (the full name flow): silent today, the footer is added
// for this one failure only; any other failure stays silent.
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
		m, _ = step(t, m, failedTags(m, offOriginErr()))
		if m.footer.Level() != components.LevelError || m.footer.Text() != "token realm on another host" {
			t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), "token realm on another host")
		}
	})

	t.Run("any other failure stays silent", func(t *testing.T) {
		m := setup(t)
		m, _ = step(t, m, failedTags(m, errors.New("boom")))
		if m.footer.IsSet() {
			t.Errorf("footer = %q, want none", m.footer.Text())
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
