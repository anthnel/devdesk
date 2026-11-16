package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
)

// DEVDESK-412 step 2: the shared guards DEVDESK-410 introduced are exported so
// the other registry HTTP clients can use them, the redirect cap no longer
// carries the last target URL, and the bounded-read and URL-redaction helpers
// exist.

// A registry that redirects forever to a signed URL must end in
// ErrTooManyRedirects, with the cap in the text and nothing of the last
// target (its path and signed query) in the error.
func TestRegistryRedirectLimitCarriesNoTarget(t *testing.T) {
	var hops atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hops.Add(1)
		http.Redirect(w, r, fmt.Sprintf("/hop%d-SECRET?sig=SECRET", n), http.StatusFound)
	}))
	defer s.Close()

	_, err := ListRegistryTagsContext(context.Background(), s.URL, "x", "user", "pass")
	if err == nil {
		t.Fatal("err = nil, want the redirect cap")
	}
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Errorf("err = %v, want errors.Is(err, ErrTooManyRedirects)", err)
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("err = %q, want it to contain %q", err, "stopped after 10 redirects")
	}
	for _, leak := range []string{"SECRET", "sig=", "/hop"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q leaks %q from the last redirect target", err, leak)
		}
	}
	// Same count as 410: the original request plus nine followed redirects.
	if got := hops.Load(); got != 10 {
		t.Errorf("server saw %d requests, want 10", got)
	}
}

// runawayCap is the hard ceiling on what endlessReader hands out. A bounded
// reader takes limit+1 bytes at most; anything near this cap means the code
// under test reads without a limit, and the test must fail instead of filling
// the machine's memory.
const runawayCap = 1 << 20 // 1 MiB

// errRunaway is what endlessReader returns once runawayCap bytes were taken.
var errRunaway = errors.New("endless reader hit its safety cap")

// endlessReader yields 'a' until runawayCap bytes were taken, counts what was
// taken from it, then fails with errRunaway. It never yields more than
// runawayCap bytes in total, so a reader that ignores its limit cannot
// exhaust memory.
type endlessReader struct{ read int64 }

func (r *endlessReader) Read(p []byte) (int, error) {
	room := runawayCap - r.read
	if room <= 0 {
		return 0, errRunaway
	}
	if int64(len(p)) > room {
		p = p[:room]
	}
	for i := range p {
		p[i] = 'a'
	}
	r.read += int64(len(p))
	return len(p), nil
}

func TestReadAllLimited(t *testing.T) {
	const limit = 8
	boom := errors.New("connection reset")

	tests := []struct {
		name    string
		r       io.Reader
		limit   int64
		want    string
		wantErr error  // matched with errors.Is when non-nil
		wantMsg string // substring of the error text
	}{
		{name: "empty", r: strings.NewReader(""), limit: limit, want: ""},
		{name: "limit minus one", r: strings.NewReader(strings.Repeat("a", limit-1)), limit: limit, want: strings.Repeat("a", limit-1)},
		{name: "exactly the limit is accepted", r: strings.NewReader(strings.Repeat("a", limit)), limit: limit, want: strings.Repeat("a", limit)},
		{
			name: "limit plus one is refused", r: strings.NewReader(strings.Repeat("a", limit+1)), limit: limit,
			wantErr: ErrBodyTooLarge, wantMsg: "response body over 8 bytes",
		},
		{
			name: "far over the limit is refused", r: strings.NewReader(strings.Repeat("a", 10*limit)), limit: limit,
			wantErr: ErrBodyTooLarge, wantMsg: "response body over 8 bytes",
		},
		{name: "limit zero accepts an empty body", r: strings.NewReader(""), limit: 0, want: ""},
		{name: "limit zero refuses one byte", r: strings.NewReader("a"), limit: 0, wantErr: ErrBodyTooLarge, wantMsg: "response body over 0 bytes"},
		{name: "negative limit is refused", r: strings.NewReader(""), limit: -1, wantErr: ErrBodyTooLarge},
		{name: "MaxInt64 limit does not overflow", r: strings.NewReader("abc"), limit: math.MaxInt64, want: "abc"},
		{
			name: "reader error is returned, not mistaken for size", r: iotest.ErrReader(boom), limit: limit,
			wantErr: boom,
		},
		{
			name: "reader error after some bytes", r: io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(boom)), limit: limit,
			wantErr: boom,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadAllLimited(tc.r, tc.limit)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want errors.Is %v", err, tc.wantErr)
				}
				if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
					t.Errorf("err = %q, want it to contain %q", err, tc.wantMsg)
				}
				if !errors.Is(tc.wantErr, ErrBodyTooLarge) && errors.Is(err, ErrBodyTooLarge) {
					t.Errorf("err = %v, a reader error must not read as ErrBodyTooLarge", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("an endless reader stops one byte past the limit", func(t *testing.T) {
		r := &endlessReader{}
		_, err := ReadAllLimited(r, limit)
		if errors.Is(err, errRunaway) || r.read >= runawayCap {
			t.Fatalf("reader was drained past %d bytes (limit %d): ReadAllLimited does not cap its read", r.read, limit)
		}
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("err = %v, want ErrBodyTooLarge", err)
		}
		if r.read > limit+1 {
			t.Errorf("took %d bytes from the reader, want at most %d", r.read, limit+1)
		}
	})
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"password is masked", "https://user:S3CRETPW@registry.example.com/v2/", "https://user:xxxxx@registry.example.com/v2/"},
		{"no userinfo is unchanged", "https://registry.example.com/v2/_catalog?n=100", "https://registry.example.com/v2/_catalog?n=100"},
		{"user only is unchanged", "https://user@registry.example.com/v2/", "https://user@registry.example.com/v2/"},
		{"unparsable escape", "%zz", "<invalid URL>"},
		{"unparsable URL with a password is not echoed", "http://user:S3CRETPW@host/%zz", "<invalid URL>"},
		{"empty", "", ""},
		{"no scheme, userinfo and host", "user:S3CRETPW@host", "<invalid URL>"},
		{"no host", "/v2/_catalog", "<invalid URL>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactURL(tc.in)
			if got != tc.want {
				t.Errorf("RedactURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "S3CRETPW") {
				t.Errorf("RedactURL(%q) = %q still holds the password", tc.in, got)
			}
		})
	}
}

func TestStripRedirectTarget(t *testing.T) {
	const target = "https://cdn.example.com/blob?sig=SECRET"
	wrap := func(err error) error { return &url.Error{Op: "Get", URL: target, Err: err} }

	sentinels := []struct {
		name string
		err  error
	}{
		{"off-origin refusal", fmt.Errorf("%w (%s)", ErrOffOriginRedirect, "cdn.example.com")},
		{"redirect cap", ErrTooManyRedirects},
		{"https to http downgrade", fmt.Errorf("%w (%s)", errInsecureRedirect, "cdn.example.com")},
	}
	for _, tc := range sentinels {
		t.Run(tc.name+" is unwrapped", func(t *testing.T) {
			got := StripRedirectTarget(wrap(tc.err))
			var ue *url.Error
			if errors.As(got, &ue) {
				t.Errorf("got %v, still a *url.Error", got)
			}
			if got.Error() != tc.err.Error() {
				t.Errorf("got %q, want %q", got, tc.err)
			}
			for _, leak := range []string{"SECRET", "sig=", "/blob"} {
				if strings.Contains(got.Error(), leak) {
					t.Errorf("got %q, leaks %q", got, leak)
				}
			}
			// The sentinel, found through the stripped error, stays the one the
			// caller can match.
			sentinel := errors.Unwrap(tc.err)
			if sentinel == nil {
				sentinel = tc.err
			}
			if !errors.Is(got, sentinel) {
				t.Errorf("errors.Is(%v, %v) = false", got, sentinel)
			}
		})
	}

	t.Run("another *url.Error is unchanged", func(t *testing.T) {
		in := wrap(errors.New("connection refused"))
		if got := StripRedirectTarget(in); got != in {
			t.Errorf("got %v, want the same error back", got)
		}
	})
	t.Run("a plain error is unchanged", func(t *testing.T) {
		in := errors.New("boom")
		if got := StripRedirectTarget(in); got != in {
			t.Errorf("got %v, want the same error back", got)
		}
	})
	t.Run("nil stays nil", func(t *testing.T) {
		if got := StripRedirectTarget(nil); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}
