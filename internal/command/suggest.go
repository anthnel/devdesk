package command

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// Stage says what the input is currently completing.
type Stage int

const (
	// StageCommand completes the command word.
	StageCommand Stage = iota
	// StageArgument completes the argument of a command that takes one.
	StageArgument
)

// ArgSuggestion is one candidate argument with an optional note.
type ArgSuggestion struct{ Value, Note string }

// ArgProvider supplies argument candidates for one command. The prefix is
// passed raw: case is preserved (the provider decides how to compare) and
// trailing whitespace is trimmed. ready is false while the provider is still
// loading its candidates.
type ArgProvider interface {
	SuggestArgs(prefix string) (args []ArgSuggestion, ready bool)
}

// Suggestions is the result of one Suggest call.
type Suggestions struct {
	Query   string
	Stage   Stage
	Entries []Entry
	Total   int
	Args    []ArgSuggestion
	// ArgsReady is false while the argument provider is still loading, so the
	// caller can tell "no candidates yet" from "no candidates".
	ArgsReady bool
}

// Ranks of a match, best first.
const (
	rankExact       = iota // the word is the name or an alias
	rankNamePrefix         // the word starts the canonical name
	rankAliasPrefix        // the word starts an alias
	rankNone
)

// Suggester ranks registry entries against the input.
type Suggester struct {
	entries   []Entry
	providers map[string]ArgProvider
}

// NewSuggester returns a Suggester over the registry.
func NewSuggester() *Suggester {
	return &Suggester{entries: Entries(), providers: map[string]ArgProvider{}}
}

// WithProvider registers the argument provider of a command and returns the
// suggester. The command is resolved like typed input, so an alias registers
// under the canonical name; an unknown command or a nil provider (including a
// typed nil pointer) is ignored.
func (s *Suggester) WithProvider(command string, p ArgProvider) *Suggester {
	if p == nil {
		return s
	}
	if v := reflect.ValueOf(p); v.Kind() == reflect.Pointer && v.IsNil() {
		return s
	}
	if e, ok := s.exact(normalize(command)); ok {
		s.providers[e.Name] = p
	}
	return s
}

// Suggest ranks the entries for the input. Only the first word is ranked; once
// it names an entry that takes an argument and is followed by a space, the
// result switches to the argument stage.
func (s *Suggester) Suggest(input string) Suggestions {
	raw := strings.TrimLeftFunc(strings.TrimPrefix(strings.TrimLeftFunc(input, unicode.IsSpace), ":"), unicode.IsSpace)
	head, rest, hasRest := raw, "", false
	if i := strings.IndexFunc(raw, unicode.IsSpace); i >= 0 {
		head, rest, hasRest = raw[:i], strings.TrimSpace(raw[i:]), true
	}
	word := normalize(head)
	out := Suggestions{Query: word, Total: len(s.entries)}

	if hasRest {
		if e, ok := s.exact(word); ok && e.TakesArg() {
			out.Stage = StageArgument
			out.Entries = []Entry{e}
			if p := s.providers[e.Name]; p != nil {
				out.Args, out.ArgsReady = p.SuggestArgs(rest)
			}
			return out
		}
	}
	out.Entries = s.rank(word)
	return out
}

// exact returns the entry whose name or alias is word.
func (s *Suggester) exact(word string) (Entry, bool) {
	for _, e := range s.entries {
		if matchRank(e, word) == rankExact {
			return e, true
		}
	}
	return Entry{}, false
}

// rank returns the entries matching word, best rank first then by name; an
// empty word returns the registry in its own order.
func (s *Suggester) rank(word string) []Entry {
	if word == "" {
		return slices.Clone(s.entries)
	}
	type ranked struct {
		e    Entry
		rank int
	}
	var hits []ranked
	for _, e := range s.entries {
		if r := matchRank(e, word); r != rankNone {
			hits = append(hits, ranked{e, r})
		}
	}
	slices.SortStableFunc(hits, func(a, b ranked) int {
		if c := cmp.Compare(a.rank, b.rank); c != 0 {
			return c
		}
		return cmp.Compare(a.e.Name, b.e.Name)
	})
	out := make([]Entry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

// matchRank is the best rank of word against the entry's spellings.
func matchRank(e Entry, word string) int {
	if e.Name == word || slices.Contains(e.Aliases, word) {
		return rankExact
	}
	if strings.HasPrefix(e.Name, word) {
		return rankNamePrefix
	}
	for _, a := range e.Aliases {
		if strings.HasPrefix(a, word) {
			return rankAliasPrefix
		}
	}
	return rankNone
}
