// Package patch edits a text file by byte range: it replaces what an edit
// covers and nothing else, so comments, line endings — CRLF included — and the
// absence of a final newline survive exactly.
//
// It knows nothing about what it is editing. It was written for the Dockerfiles
// of the base image remediation (§3.2), lived in internal/dockerfile, and never
// read a Dockerfile: it takes bytes and spans, which is why §3.78 moved it out
// rather than writing a second copy for misconfigurations.
//
// The three pieces are deliberately separate. Rewrite computes the new content,
// Diff is what a user is shown before agreeing to it, and WriteIfUnchanged puts
// it on disk only if the file still holds what the edit was computed from — so
// what gets written is always what was agreed to.
package patch

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Span is a range of bytes, End exclusive.
type Span struct{ Start, End int }

// Edit replaces the bytes Span covers, and says what it expects to find there.
type Edit struct {
	Span Span
	// Old is the text the span held when the file was read. Rewrite refuses an
	// edit whose span no longer holds it: an offset is only meaningful for the
	// content it was measured on, and applying it to another would cut
	// something else in half.
	Old string
	New string
}

// ErrConflict is returned when two edits ask for different text in the same
// place, or in places that overlap.
var ErrConflict = errors.New("conflicting edits")

// Rewrite applies edits to content and returns the result. It changes the bytes
// the edits cover and nothing else, so comments, line endings — CRLF included —
// and the absence of a final newline survive exactly.
//
// Two edits on the same span with the same text are one edit: two stages that
// share a base image through one ARG both point at its default. The same span
// with different text is a conflict, as is any overlap, and either is an error
// rather than a choice made on the caller's behalf.
func Rewrite(content []byte, edits []Edit) ([]byte, error) {
	edits, err := normalize(content, edits)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Grow(len(content))
	at := 0
	for _, e := range edits {
		out.Write(content[at:e.Span.Start])
		out.WriteString(e.New)
		at = e.Span.End
	}
	out.Write(content[at:])
	return out.Bytes(), nil
}

// normalize validates edits against content, drops exact duplicates and orders
// what is left by position.
func normalize(content []byte, edits []Edit) ([]Edit, error) {
	sorted := append([]Edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Span.Start < sorted[j].Span.Start })

	var out []Edit
	for _, e := range sorted {
		s := e.Span
		if s.Start < 0 || s.End > len(content) || s.Start > s.End {
			return nil, fmt.Errorf("edit at %d-%d is outside a %d-byte file", s.Start, s.End, len(content))
		}
		if got := string(content[s.Start:s.End]); got != e.Old {
			return nil, fmt.Errorf("the file changed: expected %q at %d-%d, found %q", e.Old, s.Start, s.End, got)
		}
		if n := len(out); n > 0 {
			prev := out[n-1]
			switch {
			case prev.Span == s && prev.New == e.New:
				continue
			case s.Start < prev.Span.End || prev.Span == s:
				return nil, fmt.Errorf("%w: %q and %q at %d-%d", ErrConflict, prev.New, e.New, s.Start, s.End)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// Diff shows what applying edits would change, as a unified diff of the lines
// they touch: the file name, and for each changed line its number, the line
// before and the line after. It is what the user reads before agreeing to a
// write, so it is computed from the same Rewrite that would perform it.
// It pairs the lines of each edit through that edit's own span, not by index
// across the whole file. Pairing by index only works while every edit replaces
// as many lines as it removes, which was true of §3.2's base image bumps and is
// false of any insertion: one added line shifts the rest, so every following
// line reads as changed and the tail past the shorter side is dropped
// altogether. A confirmation that shows two of the ten lines about to be written
// is worse than no preview, because it is read as the whole change.
func Diff(name string, content []byte, edits []Edit) (string, error) {
	normalized, err := normalize(content, edits)
	if err != nil {
		return "", err
	}
	rewritten, err := Rewrite(content, edits)
	if err != nil {
		return "", err
	}
	before, after := splitDiffLines(content), splitDiffLines(rewritten)

	var b strings.Builder
	b.WriteString("--- " + name + "\n+++ " + name + "\n")
	shift := 0
	for _, e := range normalized {
		first := countNewlines(content[:e.Span.Start])
		last := countNewlines(content[:e.Span.End])
		delta := strings.Count(e.New, "\n") - strings.Count(e.Old, "\n")

		old := slice(before, first, last)
		new := slice(after, first+shift, last+shift+delta)
		shift += delta

		// An edit that starts or ends mid-line shares that line with text it
		// does not touch, so the two sides hold identical lines at the edges.
		// Trimming them is what keeps a pure insertion from also reporting the
		// line it was inserted before as removed and re-added.
		at := first + 1
		for len(old) > 0 && len(new) > 0 && old[0] == new[0] {
			old, new, at = old[1:], new[1:], at+1
		}
		for len(old) > 0 && len(new) > 0 && old[len(old)-1] == new[len(new)-1] {
			old, new = old[:len(old)-1], new[:len(new)-1]
		}
		if len(old) == 0 && len(new) == 0 {
			continue
		}
		fmt.Fprintf(&b, "@@ line %d @@\n", at)
		for _, l := range old {
			b.WriteString("-" + l + "\n")
		}
		for _, l := range new {
			b.WriteString("+" + l + "\n")
		}
	}
	return b.String(), nil
}

// slice returns lines[from:to] inclusive, clamped to what the slice holds.
func slice(lines []string, from, to int) []string {
	if from < 0 {
		from = 0
	}
	if to >= len(lines) {
		to = len(lines) - 1
	}
	if from > to {
		return nil
	}
	return lines[from : to+1]
}

func countNewlines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

func splitDiffLines(b []byte) []string {
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}
