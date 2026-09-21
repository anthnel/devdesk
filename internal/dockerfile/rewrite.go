package dockerfile

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
)

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
func Diff(name string, content []byte, edits []Edit) (string, error) {
	rewritten, err := Rewrite(content, edits)
	if err != nil {
		return "", err
	}
	before, after := splitDiffLines(content), splitDiffLines(rewritten)

	var b strings.Builder
	b.WriteString("--- " + name + "\n+++ " + name + "\n")
	for i := 0; i < len(before) && i < len(after); i++ {
		if before[i] == after[i] {
			continue
		}
		fmt.Fprintf(&b, "@@ line %d @@\n-%s\n+%s\n", i+1, before[i], after[i])
	}
	return b.String(), nil
}

func splitDiffLines(b []byte) []string {
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}
