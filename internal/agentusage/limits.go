package agentusage

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bounds that keep memory and output size independent of what a transcript
// contains.
const (
	// maxFileIssuesPerFile caps the unreadable/oversized FileIssue entries
	// kept per file; the counters keep counting past it.
	maxFileIssuesPerFile = 100
	// maxNoIDEntries caps the per-message table for lines without a message
	// id. Past it a line still counts in NoMessageID, Messages and tokens,
	// but gets no table entry.
	maxNoIDEntries    = 100000
	maxModelsPerAgent = 64
	maxVersions       = 64
	// maxLabelLen is the longest label (model, version, id, file name) kept, in bytes.
	maxLabelLen = 128
)

// sanitizeLabel cleans a short label read from disk; see sanitize.
func sanitizeLabel(s string) string { return sanitize(s, maxLabelLen) }

// sanitize replaces with '?' every invalid UTF-8 byte, every control character
// (C0, DEL and C1, so a raw or encoded 0x9B CSI included) and every format
// character (category Cf: bidi overrides and isolates, zero-width marks), then
// truncates the result to maxLen bytes, without splitting a UTF-8 sequence
// when it can be avoided. Text read from disk reaches a terminal, where an
// escape sequence would be interpreted and a bidi override would reorder it.
func sanitize(s string, maxLen int) string {
	if len(s) > maxLen {
		cut := maxLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	var b strings.Builder
	for _, r := range s { // an invalid byte ranges as utf8.RuneError
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// modelSet is an insertion-ordered set of at most maxModelsPerAgent models.
type modelSet struct {
	list []string
	seen map[string]bool
}

// add keeps model unless it is empty, already listed, or the set is full.
func (s *modelSet) add(model string) {
	if model == "" || len(s.list) >= maxModelsPerAgent || s.seen[model] {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[model] = true
	s.list = append(s.list, model)
}
