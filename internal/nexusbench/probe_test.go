package nexusbench

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The tests of the real Prober (DEVDESK-504, step 4, probe.go), against
// httptest servers on the loopback: no cluster, no external network.
//
// Hypotheses to settle with the code:
//   - the constructor is NewProber() Prober;
//   - Ready sends one unauthenticated GET to <baseURL>/service/rest/v1/status
//     and returns nil on a 200 and only then;
//   - a redirect is never followed (an error, the target is never reached);
//   - the error says the status code or the failure, never the body.

const probeStatusPath = "/service/rest/v1/status"

func TestProberReady(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		location   string // Location header, when set
		wantErr    bool
		wantStatus string // text the error must carry
	}{
		{name: "200 means ready", status: http.StatusOK, body: "{}"},
		{name: "503 while Nexus starts", status: http.StatusServiceUnavailable, body: "starting", wantErr: true, wantStatus: "503"},
		{name: "404 on a wrong port", status: http.StatusNotFound, wantErr: true, wantStatus: "404"},
		{name: "401 is not ready", status: http.StatusUnauthorized, wantErr: true, wantStatus: "401"},
		{name: "204 is not a 200", status: http.StatusNoContent, wantErr: true, wantStatus: "204"},
		{name: "the body of a failure stays out of the error", status: http.StatusInternalServerError, body: "boom " + fakeSecret, wantErr: true, wantStatus: "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var method, path, auth atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method.Store(r.Method)
				path.Store(r.URL.Path)
				auth.Store(r.Header.Get("Authorization"))
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			err := NewProber().Ready(context.Background(), srv.URL)
			if tt.wantErr != (err != nil) {
				t.Fatalf("Ready = %v, want error: %v", err, tt.wantErr)
			}
			if got := method.Load(); got != http.MethodGet {
				t.Errorf("method = %v, want GET", got)
			}
			if got := path.Load(); got != probeStatusPath {
				t.Errorf("path = %v, want %s", got, probeStatusPath)
			}
			if got := auth.Load(); got != "" {
				t.Errorf("the probe sent credentials: %v", got)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tt.wantStatus) {
					t.Errorf("error %q does not carry the status %s", err, tt.wantStatus)
				}
				if strings.Contains(err.Error(), fakeSecret) {
					t.Errorf("error %q carries the body of the response", err)
				}
			}
		})
	}
}

func TestProberDoesNotFollowRedirects(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+probeStatusPath, http.StatusFound)
	}))
	defer srv.Close()

	if err := NewProber().Ready(context.Background(), srv.URL); err == nil {
		t.Error("Ready followed a redirect to another origin and reported ready")
	}
	if reached.Load() != 0 {
		t.Error("the redirect target was contacted")
	}
}

func TestProberFailsWhenNothingListens(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	if err := NewProber().Ready(context.Background(), url); err == nil {
		t.Error("Ready succeeded against a closed server")
	}
}

func TestProberHonoursTheContext(t *testing.T) {
	t.Run("already cancelled", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := NewProber().Ready(ctx, srv.URL); !errors.Is(err, context.Canceled) {
			t.Errorf("Ready with a cancelled context = %v, want context.Canceled", err)
		}
	})

	t.Run("a server that never answers is cut by the deadline of the context", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := NewProber().Ready(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Ready on a silent server = %v, want context.DeadlineExceeded", err)
		}
	})
}
