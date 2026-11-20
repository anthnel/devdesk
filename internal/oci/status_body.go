package oci

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// maxStatusBody bounds the body read from a 404 to look for the upstream-blocked
// message. A hypothesis, like maxNexusErrorBody: the measured body is about 110 bytes.
const maxStatusBody = 4 << 10

// upstreamBlockedPrefix starts the message a Nexus proxy gives while its upstream is
// blocked (auto-block after failures). The match is exact: case-sensitive, no leading space.
const upstreamBlockedPrefix = "Remote Auto Blocked"

// registryStatusError is the *StatusError of a non-200 registry answer. Only for a 404
// does it read the body, at most maxStatusBody bytes, and it marks the error when the
// first errors[].message starts with upstreamBlockedPrefix. The body never reaches the
// error: neither its text, the date it carries nor anything else.
//
// A body read that ends on a deadline (the client's delay, the caller's context) is no
// answer: the result is the *NoAnswerError, not a 404. Any other read failure (a cut
// body) leaves a plain, unmarked 404.
func registryStatusError(resp *http.Response) error {
	se := &StatusError{Code: resp.StatusCode, Source: SourceRegistry}
	if resp.StatusCode != http.StatusNotFound {
		return se
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxStatusBody))
	if err != nil {
		if na := asNoAnswer(err); na != nil {
			return na
		}
		return se
	}
	var doc struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &doc) == nil && len(doc.Errors) > 0 {
		se.upstreamBlocked = strings.HasPrefix(doc.Errors[0].Message, upstreamBlockedPrefix)
	}
	return se
}

// UpstreamBlocked tells a 404 that says the proxy's upstream is temporarily blocked from
// a 404 that says the repository or tag is not there.
func (e *StatusError) UpstreamBlocked() bool { return e.upstreamBlocked }
