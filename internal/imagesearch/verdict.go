package imagesearch

import (
	"errors"

	"github.com/anthnel/devdesk/internal/oci"
)

// Verdict is one registry's answer to a search. The zero value is not a
// verdict: it means no answer yet, and Classify never returns it.
type Verdict int

const (
	// VerdictOK means rows were found.
	VerdictOK Verdict = iota + 1
	// VerdictNoResult means the registry answered and nothing matches.
	VerdictNoResult
	// VerdictRefused means unsupported or refused.
	VerdictRefused
	// VerdictFailed means network, deadline, 5xx, 429 or unreadable body.
	VerdictFailed
)

// String is the verdict as shown to the user.
func (v Verdict) String() string {
	switch v {
	case VerdictOK:
		return "ok"
	case VerdictNoResult:
		return "no result"
	case VerdictRefused:
		return "unsupported or refused"
	case VerdictFailed:
		return "failed"
	}
	return "pending"
}

// Outcome is a verdict and its marks.
type Outcome struct {
	Verdict Verdict
	Partial bool
	Skipped bool
}

// ErrQueryTooShort: the catalog was not walked, the query is too short.
var ErrQueryTooShort = errors.New("query too short to walk a catalog")

// ErrPrefixNotInCatalog: no catalog name carries the entry's prefix.
var ErrPrefixNotInCatalog = errors.New("no catalog name carries the entry's prefix")

// Classify turns a search answer (error, row count, partial flag) into one
// Outcome. An error always wins over n and partial.
func Classify(err error, n int, partial bool) Outcome {
	switch {
	case err == nil && n == 0:
		return Outcome{Verdict: VerdictNoResult}
	case err == nil:
		return Outcome{Verdict: VerdictOK, Partial: partial}
	case errors.Is(err, ErrQueryTooShort):
		return Outcome{Verdict: VerdictNoResult, Skipped: true}
	case errors.Is(err, ErrUnsupported), errors.Is(err, ErrPrefixNotInCatalog):
		return Outcome{Verdict: VerdictRefused}
	}
	var se *oci.StatusError
	if errors.As(err, &se) && refusedStatus(se) {
		return Outcome{Verdict: VerdictRefused}
	}
	return Outcome{Verdict: VerdictFailed}
}

// refusedStatus tells a refusal from a failure: the registry refusing the
// request (401, 403, 404, 405) or the token endpoint refusing the credentials
// (401, 403). Any other status, 429 and 5xx included, is a failure.
func refusedStatus(se *oci.StatusError) bool {
	switch se.Code {
	case 401, 403:
		return true
	case 404, 405:
		return se.Source != oci.SourceToken
	}
	return false
}
