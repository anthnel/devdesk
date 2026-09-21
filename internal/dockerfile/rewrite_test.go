package dockerfile

import (
	"errors"
	"strings"
	"testing"
)

// editFor builds the edit that moves a parsed stage's image to another.
func editFor(t *testing.T, src string, stage int, to string) Edit {
	t.Helper()
	st := Parse([]byte(src)).Stages[stage]
	if !st.Editable {
		t.Fatalf("stage %d is not editable: %+v", stage, st)
	}
	return Edit{Span: st.Span, Old: src[st.Span.Start:st.Span.End], New: to}
}

func TestRewriteReplacesTheImageAndNothingElse(t *testing.T) {
	src := "# a comment\nFROM alpine:3.18 AS base\nRUN echo alpine:3.18\n"
	got, err := Rewrite([]byte(src), []Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	want := "# a comment\nFROM alpine:3.21 AS base\nRUN echo alpine:3.18\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The reason to edit by byte range: everything else is byte-for-byte the same.
func TestRewriteKeepsCRLFAndAMissingFinalNewline(t *testing.T) {
	src := "FROM alpine:3.18\r\nRUN true\r\nFROM debian:12"
	got, err := Rewrite([]byte(src), []Edit{editFor(t, src, 0, "alpine:3.21"), editFor(t, src, 1, "debian:13")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if want := "FROM alpine:3.21\r\nRUN true\r\nFROM debian:13"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteEditsAnARGDefault(t *testing.T) {
	src := "ARG BASE=\"alpine:3.18\"\nFROM ${BASE}\n"
	got, err := Rewrite([]byte(src), []Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if want := "ARG BASE=\"alpine:3.21\"\nFROM ${BASE}\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteAcrossAContinuation(t *testing.T) {
	src := "FROM \\\n  alpine:3.18 \\\n  AS base\n"
	got, err := Rewrite([]byte(src), []Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if want := "FROM \\\n  alpine:3.21 \\\n  AS base\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Two stages sharing one ARG are one edit, and a choice of two different bases
// for it is a conflict — never a silent pick.
func TestTwoStagesOnOneARGAreOneEditOrAConflict(t *testing.T) {
	src := "ARG BASE=alpine:3.18\nFROM ${BASE} AS a\nFROM ${BASE} AS b\n"

	same := []Edit{editFor(t, src, 0, "alpine:3.21"), editFor(t, src, 1, "alpine:3.21")}
	got, err := Rewrite([]byte(src), same)
	if err != nil {
		t.Fatalf("the same choice twice: %v", err)
	}
	if want := "ARG BASE=alpine:3.21\nFROM ${BASE} AS a\nFROM ${BASE} AS b\n"; string(got) != want {
		t.Errorf("got %q", got)
	}

	different := []Edit{editFor(t, src, 0, "alpine:3.21"), editFor(t, src, 1, "alpine:3.20")}
	if _, err := Rewrite([]byte(src), different); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

func TestOverlappingEditsConflict(t *testing.T) {
	src := "FROM alpine:3.18\n"
	edits := []Edit{
		{Span: Span{5, 16}, Old: "alpine:3.18", New: "a"},
		{Span: Span{8, 12}, Old: "ine:", New: "b"},
	}
	if _, err := Rewrite([]byte(src), edits); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

// An offset belongs to the content it was measured on. A file that changed
// since is refused, not cut at a stale position.
func TestRewriteRefusesAFileThatChanged(t *testing.T) {
	src := "FROM alpine:3.18\n"
	edit := editFor(t, src, 0, "alpine:3.21")
	changed := "FROM debian:12abc\n"
	_, err := Rewrite([]byte(changed), []Edit{edit})
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("err = %v, want it to say the file changed", err)
	}
}

func TestRewriteRefusesASpanOutsideTheFile(t *testing.T) {
	for _, span := range []Span{{-1, 3}, {0, 99}, {5, 2}} {
		if _, err := Rewrite([]byte("FROM a\n"), []Edit{{Span: span}}); err == nil {
			t.Errorf("span %+v was accepted", span)
		}
	}
}

func TestRewriteWithNoEditsIsTheSameBytes(t *testing.T) {
	src := "FROM alpine\r\n"
	got, err := Rewrite([]byte(src), nil)
	if err != nil || string(got) != src {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestEditsAreAppliedInPositionOrderWhateverTheOrderGiven(t *testing.T) {
	src := "FROM alpine:3.18\nFROM debian:12\n"
	second, first := editFor(t, src, 1, "debian:13"), editFor(t, src, 0, "alpine:3.21")
	got, err := Rewrite([]byte(src), []Edit{second, first})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if want := "FROM alpine:3.21\nFROM debian:13\n"; string(got) != want {
		t.Errorf("got %q", got)
	}
}

func TestDiffShowsTheChangedLinesOnly(t *testing.T) {
	src := "# c\r\nFROM alpine:3.18 AS a\r\nRUN true\r\nFROM debian:12\r\n"
	got, err := Diff("svc/Dockerfile", []byte(src), []Edit{editFor(t, src, 0, "alpine:3.21")})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	want := "--- svc/Dockerfile\n+++ svc/Dockerfile\n@@ line 2 @@\n-FROM alpine:3.18 AS a\n+FROM alpine:3.21 AS a\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDiffOfTwoEditsOnDifferentLines(t *testing.T) {
	src := "FROM alpine:3.18\nRUN true\nFROM debian:12\n"
	got, err := Diff("Dockerfile", []byte(src), []Edit{editFor(t, src, 0, "alpine:3.21"), editFor(t, src, 1, "debian:13")})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, want := range []string{"@@ line 1 @@", "-FROM alpine:3.18", "+FROM alpine:3.21", "@@ line 3 @@", "-FROM debian:12", "+FROM debian:13"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "RUN true") {
		t.Errorf("diff shows an unchanged line:\n%s", got)
	}
}

func TestDiffPropagatesARefusal(t *testing.T) {
	if _, err := Diff("Dockerfile", []byte("FROM a\n"), []Edit{{Span: Span{0, 99}}}); err == nil {
		t.Error("a bad edit produced a diff")
	}
}
