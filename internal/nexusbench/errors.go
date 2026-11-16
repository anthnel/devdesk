package nexusbench

import "strings"

// ErrKind says which of the failures of the bench (D-F) a BenchError is. The
// zero value is Unclassified on purpose: an error nobody classified must not
// pass for the first named kind.
type ErrKind int

// The kinds of D-F, in the order the bench meets them.
const (
	// Unclassified is a failure none of the other kinds describes.
	Unclassified ErrKind = iota
	KubectlMissing
	ContextNotFound
	ClusterUnreachable
	Forbidden
	NamespaceForeign
	NamespaceMissing
	NodePortTaken
	AdmissionDenied
	PodPending
	ImagePull
	Crash
	Deadline
	RESTUnreachable
	ProvisionFailed
)

// BenchError is the one error type of the bench: a probable cause and what to
// check, both in English and both free of secrets. Err is the underlying error,
// kept for errors.Is and errors.As; Error never prints it, because the text of
// a failed sub-process can carry a credential.
type BenchError struct {
	Kind  ErrKind
	Cause string
	Check string
	Err   error
}

// Error returns "<Cause>. Check: <Check>".
func (e *BenchError) Error() string {
	return e.Cause + ". Check: " + e.Check
}

// Unwrap returns the underlying error, or nil when there is none.
func (e *BenchError) Unwrap() error { return e.Err }

// classifyKubectl maps the stderr of a failed kubectl call to a kind. The text
// of kubectl changes between versions, so this is a fallback for the cases the
// structured calls (exit status, JSON) cannot tell apart; a text it does not
// know is Unclassified.
func classifyKubectl(stderr string) ErrKind {
	s := strings.ToLower(stderr)
	switch {
	// The more specific reason wins: a webhook refusal is reported by the API
	// server as Forbidden too.
	case strings.Contains(s, "admission webhook") && strings.Contains(s, "denied"):
		return AdmissionDenied
	case strings.Contains(s, "provided port is already allocated"),
		strings.Contains(s, "provided port is not in the valid range"):
		return NodePortTaken
	case strings.Contains(s, "forbidden"):
		return Forbidden
	case strings.Contains(s, "unable to connect to the server"),
		strings.Contains(s, "connection refused"),
		strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "tls handshake timeout"):
		return ClusterUnreachable
	}
	return Unclassified
}

// isNotFound reports whether stderr is the kubectl answer for an absent object.
func isNotFound(stderr string) bool {
	return strings.Contains(stderr, "(NotFound)")
}
