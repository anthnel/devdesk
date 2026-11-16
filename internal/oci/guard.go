package oci

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MaxRedirects is net/http's default limit, kept now that CheckRedirect is set.
const MaxRedirects = 10

// RefuseOffOriginRedirect keeps credentials on the origin they were meant for.
// It compares with the initial request (via[0]), not only the previous hop, so
// a chain that leaves the origin and comes back is refused too. The redirect is
// refused rather than followed without Authorization: a registry that moves
// content to another host is misconfigured for this client, and an explicit
// error is safer than a silent anonymous retry.
func RefuseOffOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= MaxRedirects {
		return ErrTooManyRedirects
	}
	if !sameOrigin(via[0].URL.String(), req.URL.String()) {
		return fmt.Errorf("%w (%s)", ErrOffOriginRedirect, req.URL.Host)
	}
	return nil
}

// ErrOffOriginRedirect is the refusal RefuseOffOriginRedirect returns.
var ErrOffOriginRedirect = errors.New("refusing redirect to another origin")

// ErrTooManyRedirects is returned once a request was redirected MaxRedirects
// times. net/http wraps it in a *url.Error carrying the last target URL, so
// callers pass the error through StripRedirectTarget.
var ErrTooManyRedirects = errors.New("stopped after " + strconv.Itoa(MaxRedirects) + " redirects")

// errInsecureRedirect is the refusal of a redirect from https to http, so a
// credential or a signed URL does not leave in clear.
var errInsecureRedirect = errors.New("refusing redirect from https to http")

// StripRedirectTarget strips the target URL from a refused redirect. net/http
// wraps the refusal in a *url.Error carrying the full target URL, which may hold
// a signed query; only the meaning and the host (kept by the refusal itself)
// remain. Other errors pass through unchanged.
func StripRedirectTarget(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	for _, sentinel := range []error{ErrOffOriginRedirect, ErrTooManyRedirects, errInsecureRedirect, errBlobRedirectLocal} {
		if errors.Is(ue.Err, sentinel) {
			return ue.Err
		}
	}
	return err
}

// RedactRegistryURL is RedactURL for an address as the registry configuration
// holds it, with or without a scheme ("r.example:5000" as well as
// "https://r.example"), so a log still says which registry it is about. The
// password of its userinfo is masked, and an address that cannot be read is not
// echoed. An empty address stays empty.
func RedactRegistryURL(raw string) string {
	if raw == "" || strings.Contains(raw, "://") {
		return RedactURL(raw)
	}
	out := RedactURL("https://" + raw)
	return strings.TrimPrefix(out, "https://")
}

// ErrBodyTooLarge is returned by ReadAllLimited when the body is over the limit.
var ErrBodyTooLarge = errors.New("size limit exceeded")

// ReadAllLimited reads r to its end, accepting at most limit bytes (limit >= 0;
// a negative limit is refused with ErrBodyTooLarge without reading). It takes
// limit+1 bytes at most from r: a body over the limit is an ErrBodyTooLarge, not
// a silent cut, and a read error is returned as is, never mistaken for a size.
// With limit = math.MaxInt64 the extra byte cannot be asked for, so the read is
// capped at limit and an overflow cannot be told apart.
func ReadAllLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("response body over %d bytes: %w", limit, ErrBodyTooLarge)
	}
	n := limit
	if n < math.MaxInt64 {
		n++
	}
	body, err := io.ReadAll(io.LimitReader(r, n))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body over %d bytes: %w", limit, ErrBodyTooLarge)
	}
	return body, nil
}

// RedactURL returns raw with the password of its userinfo masked, safe to put in
// an error or a log. raw must be an absolute URL: one that does not parse, or
// that has no host (such as "user:pass@host", read as a scheme and an opaque
// part), is not echoed, since it may still hold a password. An empty raw stays
// empty.
func RedactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<invalid URL>"
	}
	return u.Redacted()
}
