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
	maxNoIDEntries = 100000
	// maxToolEntries caps the table of distinct tool calls. Past it a new call
	// is counted without being remembered, so a repeat of it would count again:
	// the total can then overshoot, and never undershoot.
	maxToolEntries    = 100000
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

// modelTally accumulates per-model tokens in first-appearance order. At most
// maxModelsPerAgent named models get a slot; what the cap refuses is folded
// into the unnamed ("") bucket, so the sum stays exact.
type modelTally struct {
	list []ModelUsage
	idx  map[string]int
}

// add folds one message's tokens and its 1 h cache writes into model's slot.
func (m *modelTally) add(model string, t Tokens, write1h int64) {
	i, ok := m.idx[model]
	// A model already capped in a spill arrives here as "" and passes through
	// untouched; a named one that finds the cap reached is folded into "".
	if !ok && model != "" && len(m.list) >= maxModelsPerAgent {
		model = ""
		i, ok = m.idx[model]
	}
	if !ok {
		if m.idx == nil {
			m.idx = map[string]int{}
		}
		i = len(m.list)
		m.idx[model] = i
		m.list = append(m.list, ModelUsage{Model: model})
	}
	u := &m.list[i]
	u.Tokens = u.Tokens.Add(t)
	u.CacheWrite1h = satAdd(u.CacheWrite1h, write1h)
}

// merge folds o into m.
func (m *modelTally) merge(o *modelTally) {
	for _, u := range o.list {
		m.add(u.Model, u.Tokens, u.CacheWrite1h)
	}
}

// result returns the tally with the unnamed bucket last; nil when empty.
func (m *modelTally) result() []ModelUsage {
	if len(m.list) == 0 {
		return nil
	}
	out := make([]ModelUsage, 0, len(m.list))
	var unnamed *ModelUsage // there is at most one: add keys it by name
	for i, u := range m.list {
		if u.Model == "" {
			unnamed = &m.list[i]
			continue
		}
		out = append(out, u)
	}
	if unnamed != nil {
		out = append(out, *unnamed)
	}
	return out
}
