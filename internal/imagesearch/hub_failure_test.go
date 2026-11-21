package imagesearch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-667 (S-04b, G1): the Docker Hub search reports a registry that does not answer as
// an *oci.NoAnswerError, the same typed error the catalog path gives, and keeps the network
// error of a failed connection reachable with errors.As, so the browser can word the cause.
// Nothing here reaches the network: a local server that stalls, or a RoundTripper that fails.

// stallingHub accepts every request and answers none until the client gives up. With
// afterHeader it sends the status line and the start of a body first.
func stallingHub(t *testing.T, afterHeader bool) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if afterHeader {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":[`))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	old := hubSearchURL
	hubSearchURL = srv.URL
	t.Cleanup(func() { hubSearchURL = old })
	return &n
}

// wantNoAnswerWithoutRequest checks err is a no-answer error whose text names neither the
// request's URL, nor its query, nor the host.
func wantNoAnswerWithoutRequest(t *testing.T, err error) {
	t.Helper()
	var na *oci.NoAnswerError
	if !errors.As(err, &na) {
		t.Fatalf("err = %v (%T), want it to wrap an *oci.NoAnswerError", err, err)
	}
	if na.After != oci.RequestTimeout {
		t.Errorf("After = %v, want oci.RequestTimeout (%v)", na.After, oci.RequestTimeout)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is(err, context.DeadlineExceeded) = false")
	}
	for _, leak := range []string{"http://", "127.0.0.1", "q=", "nginx", "Get "} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("err = %q carries %q", err.Error(), leak)
		}
	}
}

func TestHubSearchThatStallsIsANoAnswer(t *testing.T) {
	n := stallingHub(t, false)

	_, _, err := HubSearcher{Client: &http.Client{Timeout: 50 * time.Millisecond}}.
		Search(context.Background(), Target{}, Query{Text: "nginx"})

	wantNoAnswerWithoutRequest(t, err)
	if got := n.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (no retry)", got)
	}
}

func TestHubSearchCutByTheCallersDeadlineIsANoAnswer(t *testing.T) {
	stallingHub(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// No Client: the one built inside Search, with its own (longer) delay.
	_, _, err := HubSearcher{}.Search(ctx, Target{}, Query{Text: "nginx"})

	wantNoAnswerWithoutRequest(t, err)
}

func TestHubSearchWhoseBodyStallsIsANoAnswer(t *testing.T) {
	stallingHub(t, true)

	_, _, err := HubSearcher{Client: &http.Client{Timeout: 50 * time.Millisecond}}.
		Search(context.Background(), Target{}, Query{Text: "nginx"})

	wantNoAnswerWithoutRequest(t, err)
}

// A cancellation is the user leaving, not the registry's silence.
func TestHubSearchCancelledIsNotANoAnswer(t *testing.T) {
	stallingHub(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := HubSearcher{}.Search(ctx, Target{}, Query{Text: "nginx"})

	var na *oci.NoAnswerError
	if errors.As(err, &na) {
		t.Errorf("a cancellation became a no answer: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want errors.Is(context.Canceled)", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The network error survives the wrap: errors.As finds the *net.OpError (its Op decides the
// wording) and the failure is not mistaken for a no answer.
func TestHubSearchKeepsTheNetworkErrorReachable(t *testing.T) {
	for _, op := range []string{"proxyconnect", "dial"} {
		t.Run(op, func(t *testing.T) {
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, &net.OpError{Op: op, Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
					Err: errors.New("connection refused")}
			})
			s := HubSearcher{Endpoint: "http://hub.invalid/v1/search", Client: &http.Client{Transport: rt}}

			_, _, err := s.Search(context.Background(), Target{}, Query{Text: "nginx"})

			var oe *net.OpError
			if !errors.As(err, &oe) || oe.Op != op {
				t.Fatalf("err = %v (%T), want errors.As to find a *net.OpError with Op %q", err, err, op)
			}
			var na *oci.NoAnswerError
			if errors.As(err, &na) {
				t.Errorf("a refused connection became a no answer: %v", err)
			}
		})
	}
}

// 429 is a *StatusError from the registry, the shape the browser words as a rate limit.
// The search asks once.
func TestHubSearchRateLimitedIsAStatusError(t *testing.T) {
	_, n, _ := hubServer(t, http.StatusTooManyRequests, `{"message":"too many requests, rate limit for nginx"}`)

	_, _, err := search(t, Target{}, Query{Text: "nginx"})

	var se *oci.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusTooManyRequests || se.Source != oci.SourceRegistry {
		t.Fatalf("err = %v (%T), want a *oci.StatusError{429, SourceRegistry}", err, err)
	}
	if strings.Contains(err.Error(), "rate limit for nginx") {
		t.Errorf("err = %q carries the response body", err.Error())
	}
	if got := n.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (no retry)", got)
	}
}
