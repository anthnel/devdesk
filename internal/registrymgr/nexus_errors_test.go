package registrymgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-509 (NX-06): the Nexus REST client reports a refused status as the
// typed oci.StatusError of the registry clients, keeps context errors as context
// errors, and treats a reply that is not a repository document as a failed
// discovery rather than as "not a group".

// nexusServer starts a Nexus-like server and returns the registry the detector
// is asked about, plus a counter of the requests the server received. The
// handler is the server's, so a test decides what each endpoint answers.
func nexusServer(t *testing.T, h http.HandlerFunc) (RegistryInfo, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return RegistryInfo{
		Provider: ProviderNexus,
		URL:      srv.URL + "/repository/docker-group",
		Username: nexusTestUser,
		Password: nexusTestPass,
	}, &hits
}

// blockUntilGone holds a handler until the client hangs up or the test ends, so
// a test server never sleeps for an unbounded time (the server's Close waits for
// its handlers). release is closed by a cleanup registered after the server's,
// which therefore runs before the server's Close.
func blockUntilGone(t *testing.T, r *http.Request) {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	select {
	case <-r.Context().Done():
	case <-release:
	}
}

// wantContextError fails unless err is the given context error and is not a
// status error: a cancelled or expired call is not something Nexus answered.
func wantContextError(t *testing.T, err, want error) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want errors.Is %v", want)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is %v", err, want)
	}
	var se *oci.StatusError
	if errors.As(err, &se) {
		t.Errorf("err = %v, is a *oci.StatusError (code %d), want a context error only", err, se.Code)
	}
}

// 3. A status other than 200 is a *oci.StatusError from the management API,
// carrying the code and nothing else: no status line, no body, no credential.
func TestNexusStatusIsATypedManagementAPIError(t *testing.T) {
	const bodyMarker = "BODY-MARKER-FROM-THE-SERVER"
	for _, status := range []int{
		http.StatusUnauthorized,        // 401
		http.StatusForbidden,           // 403
		http.StatusNotFound,            // 404
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
	} {
		t.Run(fmt.Sprintf("%d", status), func(t *testing.T) {
			info, hits := nexusServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, bodyMarker)
			})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			if len(members) != 0 {
				t.Errorf("members = %v, want none", members)
			}
			if err == nil {
				t.Fatal("err = nil, want a status error")
			}
			var se *oci.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want errors.As *oci.StatusError", err, err)
			}
			if se.Code != status {
				t.Errorf("StatusError.Code = %d, want %d", se.Code, status)
			}
			if se.Source != oci.SourceManagementAPI {
				t.Errorf("StatusError.Source = %v, want oci.SourceManagementAPI", se.Source)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err = %v, a status error is not a context error", err)
			}
			msg := err.Error()
			wantNoSecret(t, "error", msg)
			for _, leak := range []string{nexusTestUser, bodyMarker, http.StatusText(status)} {
				if strings.Contains(msg, leak) {
					t.Errorf("error leaks %q: %s", leak, msg)
				}
			}
			if !strings.Contains(msg, fmt.Sprintf("%d", status)) {
				t.Errorf("error = %q, want the status code in it", msg)
			}
			if got := hits.Load(); got != 1 {
				t.Errorf("the server received %d requests, want 1 (the generic endpoint only)", got)
			}
		})
	}
}

// 3. A password written in the registry URL goes out of nobody's mouth: the
// error names the endpoint without the userinfo.
func TestNexusPasswordInTheURLIsNotInTheStatusError(t *testing.T) {
	logs := nexusLog(t)
	info, _ := nexusServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	const user, pass = "alice", "S3CRETPW"
	info.Username, info.Password = "", ""
	info.URL = strings.Replace(info.URL, "http://", "http://"+user+":"+pass+"@", 1)

	_, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err == nil {
		t.Fatal("err = nil, want a status error")
	}
	var se *oci.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusUnauthorized || se.Source != oci.SourceManagementAPI {
		t.Fatalf("err = %v, want a *oci.StatusError{401, SourceManagementAPI}", err)
	}
	for what, s := range map[string]string{"error": err.Error(), "log": logs.String()} {
		for _, leak := range []string{user, pass} {
			if strings.Contains(s, leak) {
				t.Errorf("%s leaks %q: %s", what, leak, s)
			}
		}
	}
}

// 4. A context cancelled before the call never reaches the server and is not a
// status error.
func TestNexusCancelledContextIsNotAStatusError(t *testing.T) {
	info, hits := nexusServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, nexusGroupNoMembers)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	members, err := (&NexusDetector{}).DetectGroup(ctx, info)

	wantContextError(t, err, context.Canceled)
	if len(members) != 0 {
		t.Errorf("members = %v, want none", members)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("the server received %d requests, want 0", got)
	}
}

// 4. A deadline that expires while the server is silent is a deadline error,
// not a status error.
func TestNexusDeadlineIsNotAStatusError(t *testing.T) {
	info, _ := nexusServer(t, func(_ http.ResponseWriter, r *http.Request) {
		blockUntilGone(t, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	members, err := (&NexusDetector{}).DetectGroup(ctx, info)

	wantContextError(t, err, context.DeadlineExceeded)
	if len(members) != 0 {
		t.Errorf("members = %v, want none", members)
	}
}

// 4. A cancellation that arrives while the body is being read is a context
// error, whatever net/http makes of a body cut short.
func TestNexusCancelDuringTheBodyIsAContextError(t *testing.T) {
	started := make(chan struct{}, 1)
	info, _ := nexusServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Promise more than is sent: the headers and 10 bytes, then silence.
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"g`)
		w.(http.Flusher).Flush()
		select {
		case started <- struct{}{}:
		default:
		}
		blockUntilGone(t, r)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-started:
			cancel()
		case <-time.After(5 * time.Second):
			cancel() // the server never answered: let the call fail rather than hang
		}
	}()

	members, err := (&NexusDetector{}).DetectGroup(ctx, info)

	wantContextError(t, err, context.Canceled)
	if len(members) != 0 {
		t.Errorf("members = %v, want none", members)
	}
}

// 6. A 200 whose body is not a repository document is an error, never a "not a
// group": the caller caches the second and must not cache the first (D23), or a
// proxy login page would erase the members last known. No partial decoding: a
// document that fails halfway yields no members at all.
func TestNexusUnreadableReplyIsAnError(t *testing.T) {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	cases := []struct {
		name string
		body string
		// want is the sentinel the error must wrap, or nil.
		want error
		// as is a pointer to the json error type errors.As must find, or nil.
		as any
	}{
		{"empty body", "", errEmptyReply, nil},
		{"whitespace only", "  \n", errEmptyReply, nil},
		{"truncated JSON", "{", nil, &syntaxErr},
		{"HTML login page", "<html>login</html>", nil, &syntaxErr},
		{"trailing garbage", `{"type":"group"} trailing`, nil, &syntaxErr},
		{"group cut before its end", `{"type":"group","group":{"memberNames":["a-proxy"]} trailing`, nil, &syntaxErr},
		{"type is not a string", `{"type":5}`, nil, &typeErr},
		{"members are not a list", `{"type":"group","format":"docker","group":{"memberNames":5}}`, nil, &typeErr},
		{"empty object", `{}`, errNoRepositoryType, nil},
		{"null", `null`, errNoRepositoryType, nil},
		{"object with no type", `{"format":"docker","group":{"memberNames":["a-proxy"]}}`, errNoRepositoryType, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, hits := nexusServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			if err == nil {
				t.Fatalf("err = nil with members %v, want an error (an unreadable reply is not \"not a group\")", members)
			}
			if len(members) != 0 {
				t.Errorf("members = %v, want none (no partial decoding)", members)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want errors.Is %v", err, tc.want)
			}
			if tc.as != nil && !errors.As(err, tc.as) {
				t.Errorf("err = %v (%T), want errors.As %T", err, err, tc.as)
			}
			var se *oci.StatusError
			if errors.As(err, &se) {
				t.Errorf("err = %v, is a *oci.StatusError, want a reading error (the status was 200)", err)
			}
			wantNoSecret(t, "error", err.Error())
			if got := hits.Load(); got != 1 {
				t.Errorf("the server received %d requests, want 1 (an unreadable reply stops the detection)", got)
			}
		})
	}
}

// 6. The format endpoint keeps swallowing its errors (NX-07): an unreadable
// reply there only means no members, and the detection does not fail or panic.
func TestNexusUnreadableFormatReplyGivesNoMembers(t *testing.T) {
	for _, body := range []string{"", "  \n", "{", "<html>login</html>", "null", `{"group":{"memberNames":5}}`} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			var formatHits atomic.Int32
			info, _ := nexusServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/group/") {
					formatHits.Add(1)
					_, _ = io.WriteString(w, body)
					return
				}
				_, _ = io.WriteString(w, nexusGroupNoMembers)
			})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			if err != nil {
				t.Errorf("err = %v, want nil (the format endpoint's errors are swallowed)", err)
			}
			if len(members) != 0 {
				t.Errorf("members = %v, want none", members)
			}
			if got := formatHits.Load(); got < 1 {
				t.Errorf("the format endpoint was never asked, the test checks nothing")
			}
		})
	}
}

// 5. A repository that Nexus describes and that is not a group is a settled
// answer, still read as "not a group" (nil, nil), and so still cacheable.
func TestNexusNonGroupStillReadsAsNotAGroup(t *testing.T) {
	for _, body := range []string{
		`{"type":"hosted","format":"docker"}`,
		`{"type":"proxy","format":"docker"}`,
	} {
		t.Run(body, func(t *testing.T) {
			var formatHits atomic.Int32
			info, hits := nexusServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/group/") {
					formatHits.Add(1)
				}
				_, _ = io.WriteString(w, body)
			})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			if err != nil {
				t.Fatalf("err = %v, want nil: a real non-group is an answer, not a failure", err)
			}
			if members != nil {
				t.Errorf("members = %v, want nil", members)
			}
			if got := hits.Load(); got != 1 || formatHits.Load() != 0 {
				t.Errorf("requests = %d (format endpoint %d), want 1 and 0", got, formatHits.Load())
			}
		})
	}
}
