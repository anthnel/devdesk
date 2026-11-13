package imagesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

type classifyCase struct {
	name    string
	err     error
	n       int
	partial bool
	want    Outcome
}

func classifyCases() []classifyCase {
	status := func(code int) error { return &oci.StatusError{Code: code} }
	token := func(code int) error { return &oci.StatusError{Code: code, Source: oci.SourceToken} }
	refused, failed := Outcome{Verdict: VerdictRefused}, Outcome{Verdict: VerdictFailed}
	return []classifyCase{
		{"rows", nil, 3, false, Outcome{Verdict: VerdictOK}},
		{"rows at the cap", nil, 100, true, Outcome{Verdict: VerdictOK, Partial: true}},
		{"empty answer", nil, 0, false, Outcome{Verdict: VerdictNoResult}},
		// DEVDESK-306 Q1=A: a walk cut at the cap with no match is not a
		// definitive "nothing": the verdict stays no result, with the mark.
		{"empty partial answer", nil, 0, true, Outcome{Verdict: VerdictNoResult, Partial: true}},
		{"query too short", ErrQueryTooShort, 0, false, Outcome{Verdict: VerdictNoResult, Skipped: true}},
		{"query too short wrapped", fmt.Errorf("x: %w", ErrQueryTooShort), 0, false, Outcome{Verdict: VerdictNoResult, Skipped: true}},
		{"unsupported", ErrUnsupported, 0, false, refused},
		{"prefix not in catalog", ErrPrefixNotInCatalog, 0, false, refused},
		{"registry 401", status(401), 0, false, refused},
		{"registry 403", status(403), 0, false, refused},
		{"registry 404", status(404), 0, false, refused},
		{"registry 405", status(405), 0, false, refused},
		{"token 401", token(401), 0, false, refused},
		{"token 403", token(403), 0, false, refused},
		{"token 404", token(404), 0, false, failed},
		{"token 500", token(500), 0, false, failed},
		{"429", status(429), 0, false, failed},
		{"500", status(500), 0, false, failed},
		{"502", status(502), 0, false, failed},
		{"503", status(503), 0, false, failed},
		{"400", status(400), 0, false, failed},
		{"301", status(301), 0, false, failed},
		{"wrapped 403", fmt.Errorf("hub search: %w", status(403)), 0, false, refused},
		{"deadline", context.DeadlineExceeded, 0, false, failed},
		{"deadline in url error", &url.Error{Op: "Get", URL: "https://x", Err: context.DeadlineExceeded}, 0, false, failed},
		{"canceled", context.Canceled, 0, false, failed},
		{"unreadable body", fmt.Errorf("decode: %w", &json.SyntaxError{}), 0, false, failed},
		{"network error", errors.New("connection refused"), 0, false, failed},
		{"error wins over n and partial", ErrUnsupported, 5, true, refused},
		{"failure wins over partial", errors.New("boom"), 5, true, failed},
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range classifyCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err, tc.n, tc.partial); got != tc.want {
				t.Errorf("Classify(%v, %d, %v) = %+v, want %+v", tc.err, tc.n, tc.partial, got, tc.want)
			}
		})
	}
}

// One verdict per answer, and a mark only with its own verdict.
func TestClassifyMarksOnlyTheirVerdict(t *testing.T) {
	for _, tc := range classifyCases() {
		t.Run(tc.name, func(t *testing.T) {
			o := Classify(tc.err, tc.n, tc.partial)
			switch o.Verdict {
			case VerdictOK, VerdictNoResult, VerdictRefused, VerdictFailed:
			default:
				t.Fatalf("verdict = %d, want one of the four", o.Verdict)
			}
			if o.Partial && o.Verdict != VerdictOK && o.Verdict != VerdictNoResult {
				t.Errorf("Partial set with %v", o.Verdict)
			}
			if o.Partial && o.Skipped {
				t.Error("Partial and Skipped set together")
			}
			if o.Skipped && o.Verdict != VerdictNoResult {
				t.Errorf("Skipped set with %v", o.Verdict)
			}
		})
	}
}

func TestVerdictString(t *testing.T) {
	for v, want := range map[Verdict]string{
		VerdictOK:       "ok",
		VerdictNoResult: "no result",
		VerdictRefused:  "unsupported or refused",
		VerdictFailed:   "failed",
		Verdict(0):      "pending",
	} {
		if got := v.String(); got != want {
			t.Errorf("Verdict(%d).String() = %q, want %q", int(v), got, want)
		}
	}
}

func TestSentinelsAreDistinctErrorsWithAMessage(t *testing.T) {
	if ErrQueryTooShort.Error() == "" || ErrPrefixNotInCatalog.Error() == "" {
		t.Fatal("sentinels need a message")
	}
	if errors.Is(ErrQueryTooShort, ErrPrefixNotInCatalog) || errors.Is(ErrQueryTooShort, ErrUnsupported) {
		t.Error("sentinels must be distinct")
	}
}
