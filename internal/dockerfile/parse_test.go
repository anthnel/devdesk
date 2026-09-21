package dockerfile

import "testing"

// spelled returns the bytes a Span covers, which is what a rewrite would replace.
func spelled(t *testing.T, content string, s Span) string {
	t.Helper()
	if s.Start < 0 || s.End > len(content) || s.Start > s.End {
		t.Fatalf("span %+v is outside a %d-byte file", s, len(content))
	}
	return content[s.Start:s.End]
}

func TestASimpleFrom(t *testing.T) {
	src := "FROM alpine:3.18\nRUN echo hi\n"
	f := Parse([]byte(src))
	if len(f.Stages) != 1 {
		t.Fatalf("got %d stages, want 1", len(f.Stages))
	}
	st := f.Stages[0]
	if st.Image != "alpine:3.18" || st.Line != 1 || !st.Editable || st.Kind != KindImage {
		t.Errorf("stage = %+v", st)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q, want the image and nothing else", got)
	}
}

func TestAMultiStageFileHasOneStagePerFrom(t *testing.T) {
	src := `# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.22 AS build
RUN go build
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out /out
`
	f := Parse([]byte(src))
	if len(f.Stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(f.Stages))
	}
	b := f.Stages[0]
	if b.Name != "build" || b.Platform != "$BUILDPLATFORM" || b.Image != "golang:1.22" || b.Line != 2 {
		t.Errorf("build stage = %+v", b)
	}
	if got := spelled(t, src, b.Span); got != "golang:1.22" {
		t.Errorf("span covers %q", got)
	}
	if s := f.Stages[1]; s.Image != "gcr.io/distroless/static:nonroot" || s.Name != "" || s.Line != 4 {
		t.Errorf("final stage = %+v", s)
	}
}

func TestScratchAndStageReferencesAreNotImages(t *testing.T) {
	src := "FROM golang:1.22 AS build\nFROM scratch\nFROM build AS again\nFROM BUILD\n"
	f := Parse([]byte(src))
	want := []Kind{KindImage, KindScratch, KindStage, KindStage}
	if len(f.Stages) != len(want) {
		t.Fatalf("got %d stages, want %d", len(f.Stages), len(want))
	}
	for i, k := range want {
		if f.Stages[i].Kind != k {
			t.Errorf("stage %d kind = %v, want %v", i, f.Stages[i].Kind, k)
		}
		if k != KindImage && (f.Stages[i].Image != "" || f.Stages[i].Editable) {
			t.Errorf("stage %d carries an image or is editable: %+v", i, f.Stages[i])
		}
	}
}

func TestKeywordsAreCaseInsensitive(t *testing.T) {
	src := "from alpine:3.18 as base\narg X=1\n"
	f := Parse([]byte(src))
	if len(f.Stages) != 1 || f.Stages[0].Name != "base" || f.Stages[0].Image != "alpine:3.18" {
		t.Errorf("stages = %+v", f.Stages)
	}
}

func TestAnARGDefaultIsWhereTheImageIsWritten(t *testing.T) {
	src := "ARG BASE=alpine:3.18\nFROM ${BASE}\nRUN true\n"
	f := Parse([]byte(src))
	st := f.Stages[0]
	if st.Image != "alpine:3.18" || st.Written != "${BASE}" || !st.Editable {
		t.Fatalf("stage = %+v", st)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q, want the ARG default", got)
	}
}

func TestAQuotedARGDefaultIsSpannedWithoutItsQuotes(t *testing.T) {
	src := "ARG BASE=\"alpine:3.18\"\nFROM $BASE\n"
	st := Parse([]byte(src)).Stages[0]
	if st.Image != "alpine:3.18" {
		t.Fatalf("Image = %q", st.Image)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q, want the value inside the quotes", got)
	}
}

func TestTwoStagesSharingAnARGShareItsSpan(t *testing.T) {
	src := "ARG BASE=alpine:3.18\nFROM ${BASE} AS a\nFROM ${BASE} AS b\n"
	f := Parse([]byte(src))
	if f.Stages[0].Span != f.Stages[1].Span {
		t.Errorf("spans differ: %+v vs %+v", f.Stages[0].Span, f.Stages[1].Span)
	}
}

func TestAnAssembledReferenceResolvesButCannotBeEdited(t *testing.T) {
	src := "ARG NODE=20\nFROM node:${NODE}-alpine\n"
	st := Parse([]byte(src)).Stages[0]
	if st.Image != "node:20-alpine" {
		t.Errorf("Image = %q, want it resolved", st.Image)
	}
	if st.Editable || st.NoEditReason == "" {
		t.Errorf("Editable = %v, reason %q; an assembled reference has no one place to edit", st.Editable, st.NoEditReason)
	}
}

func TestAVariableWithNoDefaultIsUnresolved(t *testing.T) {
	tests := []struct{ name, src string }{
		{"undeclared", "FROM ${BASE}\n"},
		{"declared without a default", "ARG BASE\nFROM ${BASE}\n"},
		{"declared after the FROM", "FROM ${BASE}\nARG BASE=alpine\n"},
	}
	for _, tt := range tests {
		st := Parse([]byte(tt.src)).Stages[0]
		if st.Unresolved == "" || st.Image != "" || st.Editable {
			t.Errorf("%s: stage = %+v, want unresolved with a reason", tt.name, st)
		}
	}
}

func TestAnInlineDefaultResolvesTheReference(t *testing.T) {
	st := Parse([]byte("FROM ${BASE:-alpine:3.18}\n")).Stages[0]
	if st.Image != "alpine:3.18" {
		t.Errorf("Image = %q", st.Image)
	}
	if st.Editable {
		t.Error("an inline default is not an ARG line to edit")
	}
}

func TestAnARGRedeclaredBeforeTheFromWins(t *testing.T) {
	src := "ARG BASE=alpine:3.17\nARG BASE=alpine:3.18\nFROM ${BASE}\n"
	st := Parse([]byte(src)).Stages[0]
	if st.Image != "alpine:3.18" {
		t.Errorf("Image = %q, want the later declaration", st.Image)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q", got)
	}
}

func TestAnARGInsideAStageDoesNotReachALaterFrom(t *testing.T) {
	src := "FROM alpine:3.18\nARG BASE=busybox\nFROM ${BASE}\n"
	if st := Parse([]byte(src)).Stages[1]; st.Unresolved == "" {
		t.Errorf("stage = %+v; only ARGs before the first FROM are visible to FROM", st)
	}
}

func TestAContinuationCarriesTheImageToTheNextLine(t *testing.T) {
	src := "FROM \\\n  alpine:3.18 \\\n  AS base\nRUN true\n"
	st := Parse([]byte(src)).Stages[0]
	if st.Image != "alpine:3.18" || st.Name != "base" {
		t.Fatalf("stage = %+v", st)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q", got)
	}
}

func TestAContinuationSkipsCommentsAndBlankLines(t *testing.T) {
	src := "FROM \\\n# a comment\n\n  alpine:3.18\nRUN true\n"
	st := Parse([]byte(src)).Stages[0]
	if st.Image != "alpine:3.18" {
		t.Errorf("Image = %q", st.Image)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q", got)
	}
}

func TestTheEscapeDirectiveChangesTheContinuationCharacter(t *testing.T) {
	src := "# escape=`\nFROM `\nalpine:3.18\n"
	st := Parse([]byte(src)).Stages[0]
	if st.Image != "alpine:3.18" {
		t.Errorf("Image = %q", st.Image)
	}
	if got := spelled(t, src, st.Span); got != "alpine:3.18" {
		t.Errorf("span covers %q", got)
	}
}

func TestSpansAreAbsoluteWithCRLFAndNoFinalNewline(t *testing.T) {
	src := "# c\r\nARG BASE=alpine:3.18\r\nFROM ${BASE} AS a\r\nFROM debian:12"
	f := Parse([]byte(src))
	if len(f.Stages) != 2 {
		t.Fatalf("got %d stages", len(f.Stages))
	}
	if got := spelled(t, src, f.Stages[0].Span); got != "alpine:3.18" {
		t.Errorf("first span covers %q", got)
	}
	if got := spelled(t, src, f.Stages[1].Span); got != "debian:12" {
		t.Errorf("last span covers %q", got)
	}
	if f.Stages[1].Line != 4 {
		t.Errorf("Line = %d, want 4", f.Stages[1].Line)
	}
}

func TestADigestPinnedReferenceIsKeptWhole(t *testing.T) {
	src := "FROM alpine:3.18@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"
	st := Parse([]byte(src)).Stages[0]
	if got := spelled(t, src, st.Span); got != st.Image || st.Image == "" {
		t.Errorf("Image %q, span covers %q", st.Image, got)
	}
}

func TestSomethingThatIsNotAFromIsIgnored(t *testing.T) {
	src := "RUN echo FROM alpine\n# FROM busybox\nCOPY --from=x a b\n"
	if f := Parse([]byte(src)); len(f.Stages) != 0 {
		t.Errorf("got %d stages from a file with no FROM instruction", len(f.Stages))
	}
}

func TestAFromWithNoImageAndAnEmptyFile(t *testing.T) {
	if st := Parse([]byte("FROM\n")).Stages; len(st) != 1 || st[0].Unresolved == "" {
		t.Errorf("stages = %+v, want one unresolved", st)
	}
	if f := Parse(nil); len(f.Stages) != 0 {
		t.Errorf("an empty file has %d stages", len(f.Stages))
	}
}
