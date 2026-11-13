package imagesearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/testutil/loopback"
)

// verdictOf runs HubSearcher against endpoint and classifies the answer.
func verdictOf(ctx context.Context, endpoint string) (Outcome, error) {
	imgs, partial, err := HubSearcher{Endpoint: endpoint}.Search(ctx, Target{}, Query{Text: "nginx"})
	return Classify(err, len(imgs), partial), err
}

func TestVerdictHTTP(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   Verdict
	}{
		{"404", http.StatusNotFound, "", VerdictRefused},
		{"405", http.StatusMethodNotAllowed, "", VerdictRefused},
		{"401", http.StatusUnauthorized, "", VerdictRefused},
		{"403", http.StatusForbidden, "", VerdictRefused},
		{"429", http.StatusTooManyRequests, "", VerdictFailed},
		{"500", http.StatusInternalServerError, "", VerdictFailed},
		{"invalid body", http.StatusOK, "<html>not json", VerdictFailed},
		{"empty body", http.StatusOK, "", VerdictFailed},
		{"empty response", http.StatusOK, `{"results":[]}`, VerdictNoResult},
		{"results", http.StatusOK, `{"results":[{"name":"nginx","is_official":true}]}`, VerdictOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			got, err := verdictOf(context.Background(), srv.URL)
			if got.Verdict != tc.want {
				t.Errorf("verdict = %v, want %v (err = %v)", got.Verdict, tc.want, err)
			}
			if got.Partial || got.Skipped {
				t.Errorf("marks = %+v, want none", got)
			}
		})
	}
}

func TestVerdictHTTPDeadlineExceeded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := verdictOf(ctx, srv.URL)
	if err == nil {
		t.Fatal("err = nil, want a deadline error")
	}
	if got.Verdict != VerdictFailed {
		t.Errorf("verdict = %v, want %v (err = %v)", got.Verdict, VerdictFailed, err)
	}
}

func TestVerdictHTTPNetworkError(t *testing.T) {
	// The port is held bound but never listening, so connect() is refused
	// deterministically and no bind(0) can be handed it.
	url := fmt.Sprintf("http://127.0.0.1:%d", loopback.ReservePort(t))

	got, err := verdictOf(context.Background(), url)
	if err == nil {
		t.Fatal("err = nil, want a connection error")
	}
	if got.Verdict != VerdictFailed {
		t.Errorf("verdict = %v, want %v (err = %v)", got.Verdict, VerdictFailed, err)
	}
}
