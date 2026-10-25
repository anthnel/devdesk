package agentusage

import "unicode/utf8"

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

// sanitizeLabel replaces every control character (0x00-0x1f, 0x7f) with '?'
// and truncates the result to maxLabelLen bytes, without splitting a UTF-8
// sequence when it can be avoided. Text read from disk reaches a terminal,
// where an escape sequence would be interpreted.
func sanitizeLabel(s string) string {
	if len(s) > maxLabelLen {
		cut := maxLabelLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c == 0x7f {
			if b == nil {
				b = []byte(s)
			}
			b[i] = '?'
		}
	}
	if b == nil {
		return s
	}
	return string(b)
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
