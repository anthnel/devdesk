package ociresources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-629 step 4 (F5, DEVDESK-633): a registry that does not answer before the delay
// is a failed verdict whose cause reads "no answer within N s", in the journal and at the
// footer, with N the delay of the client. The delay is unchanged (15 s).
//
// No test waits for it: imageSearchDeadline is shrunk to 20 ms, or the error comes from the
// real client against an already expired context. This file names no symbol of the step; the
// ones that do are in browser_noanswer_symbols_test.go.

// productionSearchDeadline is imageSearchDeadline as shipped, read before any test shrinks it.
var productionSearchDeadline = imageSearchDeadline

// noAnswerText is the text of a no-answer error: the delay as shipped, in whole seconds.
func noAnswerText() string {
	return "no answer within " + strconv.Itoa(int(productionSearchDeadline/time.Second)) + " s"
}

// silentRegistry accepts every request and answers none until the client gives up.
func silentRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// searchSilentRegistry runs the production search on one registry that never answers and
// returns the model after its (failed) answer.
func searchSilentRegistry(t *testing.T) (Model, *silentSearch) {
	t.Helper()
	withSearchDeadline(t, 20*time.Millisecond)
	hub, _ := fakeHub(t)
	srv := silentRegistry(t)
	withImageSearcher(t, wiredLikeProduction(hub))
	logs := captureLog(t)

	m := registryModel(t, anonymousRegistry(srv.URL, "private"))
	m, cmd := submitRaw(t, m, "nginx")
	res, ok := cmd().(ImageSearchResultMsg)
	if !ok {
		t.Fatal("the search did not answer with an ImageSearchResultMsg")
	}
	m, _ = step(t, m, res)
	return m, &silentSearch{url: srv.URL, logs: logs.String(), err: res.Err}
}

type silentSearch struct {
	url, logs string
	err       error
}

func TestImageSearchNoAnswerLogsTheConstantMessage(t *testing.T) {
	m, s := searchSilentRegistry(t)

	wantVerdict(t, m, "private", imagesearch.VerdictFailed)
	want := "ERROR [oci_resources] image search on private: " + noAnswerText()
	var line string
	for _, l := range strings.Split(s.logs, "\n") {
		if strings.Contains(l, "image search on private") {
			line = l
		}
	}
	if !strings.HasSuffix(line, want) {
		t.Errorf("journal line = %q, want one ending with %q", line, want)
	}
	if strings.Contains(s.logs, s.url) || strings.Contains(s.logs, "127.0.0.1") {
		t.Errorf("the journal carries the registry's URL: %q", s.logs)
	}
}

func TestImageSearchNoAnswerShowsAtTheFooter(t *testing.T) {
	m, _ := searchSilentRegistry(t)

	want := "Image search on private: " + noAnswerText()
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
}

// A 502 received in time stays a failed verdict with the text it has today: no
// "no answer" anywhere, and no claim about what is upstream.
func TestImageSearchTimelyBadGatewayIsNotNoAnswer(t *testing.T) {
	hub, _ := fakeHub(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	withImageSearcher(t, wiredLikeProduction(hub))
	logs := captureLog(t)

	m := registryModel(t, anonymousRegistry(srv.URL, "private"))
	m, cmd := submitRaw(t, m, "nginx")
	res, ok := cmd().(ImageSearchResultMsg)
	if !ok {
		t.Fatal("the search did not answer with an ImageSearchResultMsg")
	}
	m, _ = step(t, m, res)

	wantVerdict(t, m, "private", imagesearch.VerdictFailed)
	if strings.Contains(logs.String(), "no answer") {
		t.Errorf("a 502 received in time is logged as no answer: %q", logs.String())
	}
	if m.footer.IsSet() {
		t.Errorf("footer = %q, want none: a 502 adds no reason", m.footer.Text())
	}
}

// expiredTagsErr is what the real client returns for a tag listing past its deadline.
func expiredTagsErr(t *testing.T) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	_, err := oci.ListRegistryTagsContext(ctx, srv.URL, "library/nginx", "", "")
	if err == nil {
		t.Fatal("an expired context listed tags")
	}
	return err
}

// Tags opened from level 1 (right): the generic text gains the reason, built from the delay.
func TestTagsFooterSaysNoAnswer(t *testing.T) {
	m := afterRight(t)
	logs := captureLog(t)
	err := expiredTagsErr(t)

	m, cmd := step(t, m, failedTags(m, err))

	want := tagsLoadFailedText + ": " + noAnswerText()
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), want)
	}
	if cmd == nil {
		t.Error("no Cmd: the footer message has no timer")
	}
	if wantLog := "registry tags on hub: " + noAnswerText(); !strings.Contains(logs.String(), wantLog) {
		t.Errorf("journal = %q, want it to contain %q", logs.String(), wantLog)
	}
	if strings.Contains(logs.String(), "127.0.0.1") {
		t.Errorf("the journal carries the registry's URL: %q", logs.String())
	}
}

// A 502 received in time keeps the generic tags text, with no reason.
func TestTagsFooterKeepsItsGenericTextOnATimely502(t *testing.T) {
	m := afterRight(t)
	err := &oci.StatusError{Code: http.StatusBadGateway, Source: oci.SourceRegistry}

	m, _ = step(t, m, failedTags(m, err))

	if strings.Contains(m.footer.Text(), "no answer") {
		t.Errorf("footer = %q, a 502 received in time is not a lack of answer", m.footer.Text())
	}
}

// Tags opened from the form stay silent for a failure with no reason today; no answer is
// one that has a reason, so it shows, like the refused realm.
func TestTagsFromTheFormSayNoAnswer(t *testing.T) {
	captureLog(t)
	withFullTagRecorder(t)
	withImageSearcher(t, &fakeSearcher{})
	m := typeInto(t, browsingModel(t), "library/nginx")
	m.registryBrowser.submit(actionOpenTags)

	m, _ = step(t, m, failedTags(m, expiredTagsErr(t)))

	if m.footer.Level() != components.LevelError || m.footer.Text() != noAnswerText() {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), noAnswerText())
	}
}
