package dockerfile

import (
	"errors"
	"testing"

	"github.com/anthnel/devdesk/internal/patch"
)

// These are the seam between this package and internal/patch: a span this
// parser reports, handed to patch.Rewrite, must change the image and nothing
// else. They are here rather than in internal/patch because what they are
// really checking is the *parser* — that Span points at the one place the image
// is written, including when that place is an ARG default on another line.
//
// internal/patch's own tests build their spans by hand and never parse.

// editFor builds the edit that moves a parsed stage's image to another.
func editFor(t *testing.T, src string, stage int, to string) patch.Edit {
	t.Helper()
	st := Parse([]byte(src)).Stages[stage]
	if !st.Editable {
		t.Fatalf("stage %d is not editable: %+v", stage, st)
	}
	return patch.Edit{Span: st.Span, Old: src[st.Span.Start:st.Span.End], New: to}
}

func TestAParsedSpanReplacesTheImageAndNothingElse(t *testing.T) {
	src := "# a comment\nFROM alpine:3.18 AS base\nRUN echo alpine:3.18\n"
	got, err := patch.Rewrite([]byte(src), []patch.Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	want := "# a comment\nFROM alpine:3.21 AS base\nRUN echo alpine:3.18\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The image is written in the ARG default, not in the FROM. The span has to
// point there or the edit would replace `${BASE}` with a literal reference.
func TestAStageOnAnARGEditsTheARGDefault(t *testing.T) {
	src := "ARG BASE=\"alpine:3.18\"\nFROM ${BASE}\n"
	got, err := patch.Rewrite([]byte(src), []patch.Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if want := "ARG BASE=\"alpine:3.21\"\nFROM ${BASE}\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A FROM spread over continuation lines still spans exactly the image token.
func TestASpanAcrossAContinuation(t *testing.T) {
	src := "FROM \\\n  alpine:3.18 \\\n  AS base\n"
	got, err := patch.Rewrite([]byte(src), []patch.Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if want := "FROM \\\n  alpine:3.21 \\\n  AS base\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Two stages sharing one ARG report the same span, so moving both to the same
// image is one edit — and moving them to two different ones is a conflict the
// caller is told about rather than a pick made on its behalf.
func TestTwoStagesOnOneARGShareTheirSpan(t *testing.T) {
	src := "ARG BASE=alpine:3.18\nFROM ${BASE} AS a\nFROM ${BASE} AS b\n"

	same := []patch.Edit{editFor(t, src, 0, "alpine:3.21"), editFor(t, src, 1, "alpine:3.21")}
	got, err := patch.Rewrite([]byte(src), same)
	if err != nil {
		t.Fatalf("the same choice twice: %v", err)
	}
	if want := "ARG BASE=alpine:3.21\nFROM ${BASE} AS a\nFROM ${BASE} AS b\n"; string(got) != want {
		t.Errorf("got %q", got)
	}

	different := []patch.Edit{editFor(t, src, 0, "alpine:3.21"), editFor(t, src, 1, "alpine:3.20")}
	if _, err := patch.Rewrite([]byte(src), different); !errors.Is(err, patch.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}
