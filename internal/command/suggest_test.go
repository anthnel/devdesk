package command

import (
	"slices"
	"testing"
)

type fakeProvider struct {
	got     []string
	args    []ArgSuggestion
	loading bool
}

func (f *fakeProvider) SuggestArgs(prefix string) ([]ArgSuggestion, bool) {
	f.got = append(f.got, prefix)
	return f.args, !f.loading
}

func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

func TestSuggestEmptyInputListsRegistryInOrder(t *testing.T) {
	got := NewSuggester().Suggest("")
	if got.Total != 17 || len(got.Entries) != 17 {
		t.Fatalf("Total=%d entries=%d, want 17/17", got.Total, len(got.Entries))
	}
	if !slices.Equal(names(got.Entries), names(Entries())) {
		t.Errorf("order differs from Entries(): %v", names(got.Entries))
	}
	if got.Stage != StageCommand {
		t.Errorf("Stage = %v, want StageCommand", got.Stage)
	}
}

func TestSuggestRanking(t *testing.T) {
	tests := []struct {
		input string
		want  []string // exact list when full, else leading entries
		full  bool
	}{
		{"co", []string{"configuration", "containers", "context"}, true},
		{"cont", []string{"containers"}, false},
		{"s", []string{"status"}, false},
		{"w", []string{"workspaces"}, false},
		{"cfg", []string{"configuration"}, true},
		{"version", []string{"about"}, true},
		{"status foo", []string{"status"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := names(NewSuggester().Suggest(tt.input).Entries)
			if tt.full {
				if !slices.Equal(got, tt.want) {
					t.Errorf("got %v, want %v", got, tt.want)
				}
				return
			}
			if len(got) < len(tt.want) || !slices.Equal(got[:len(tt.want)], tt.want) {
				t.Errorf("got %v, want leading %v", got, tt.want)
			}
		})
	}
}

func TestSuggestNormalizesInput(t *testing.T) {
	base := NewSuggester().Suggest("co")
	if len(base.Entries) == 0 {
		t.Fatal("co yields nothing")
	}
	for _, in := range []string{"  CO ", ":co", ": co"} {
		got := NewSuggester().Suggest(in)
		if !slices.Equal(names(got.Entries), names(base.Entries)) || got.Total != base.Total {
			t.Errorf("%q = %v, want %v", in, names(got.Entries), names(base.Entries))
		}
	}
}

func TestSuggestNoMatch(t *testing.T) {
	got := NewSuggester().Suggest("zz")
	if len(got.Entries) != 0 || got.Total != 17 {
		t.Errorf("entries=%d total=%d, want 0/17", len(got.Entries), got.Total)
	}
}

func TestSuggestArgumentStage(t *testing.T) {
	p := &fakeProvider{args: []ArgSuggestion{{Value: "work", Note: "n"}}}
	got := NewSuggester().WithProvider("context", p).Suggest("ctx wo")
	if got.Stage != StageArgument {
		t.Fatalf("Stage = %v, want StageArgument", got.Stage)
	}
	if !slices.Equal(names(got.Entries), []string{"context"}) {
		t.Errorf("entries = %v, want [context]", names(got.Entries))
	}
	if !slices.Equal(p.got, []string{"wo"}) {
		t.Errorf("provider prefixes = %v, want [wo]", p.got)
	}
	if len(got.Args) != 1 || got.Args[0].Value != "work" {
		t.Errorf("Args = %v", got.Args)
	}
}

func TestSuggestArgumentStageWithoutProvider(t *testing.T) {
	got := NewSuggester().Suggest("ctx wo")
	if got.Stage != StageArgument {
		t.Errorf("Stage = %v, want StageArgument", got.Stage)
	}
	if got.Args != nil {
		t.Errorf("Args = %v, want nil", got.Args)
	}
}

func TestSuggestIsDeterministic(t *testing.T) {
	for _, in := range []string{"", "c", "co", "s"} {
		a := names(NewSuggester().Suggest(in).Entries)
		b := names(NewSuggester().Suggest(in).Entries)
		if !slices.Equal(a, b) {
			t.Errorf("%q: %v vs %v", in, a, b)
		}
	}
}

func TestSuggestTargetsAppearOnce(t *testing.T) {
	for _, in := range []string{"", "c", "co", "s", "w", "d", "g"} {
		seen := map[string]bool{}
		for _, n := range names(NewSuggester().Suggest(in).Entries) {
			if seen[n] {
				t.Errorf("%q: %s appears twice", in, n)
			}
			seen[n] = true
		}
	}
}

func TestSuggestEverySpellingRanksItsTargetFirst(t *testing.T) {
	for _, e := range Entries() {
		for _, spelling := range append([]string{e.Name}, e.Aliases...) {
			got := NewSuggester().Suggest(spelling)
			if len(got.Entries) == 0 || got.Entries[0].Name != e.Name {
				t.Errorf("%q: head = %v, want %s", spelling, names(got.Entries), e.Name)
			}
		}
	}
}

func TestSuggestArgumentStageEmptyPrefix(t *testing.T) {
	p := &fakeProvider{}
	NewSuggester().WithProvider("context", p).Suggest("ctx ")
	if !slices.Equal(p.got, []string{""}) {
		t.Errorf("provider prefixes = %v, want [\"\"]", p.got)
	}
}

func TestSuggestArgumentPrefixKeepsCaseAndDropsTrailingSpace(t *testing.T) {
	p := &fakeProvider{}
	NewSuggester().WithProvider("context", p).Suggest("ctx  Wo  ")
	if !slices.Equal(p.got, []string{"Wo"}) {
		t.Errorf("provider prefixes = %q, want [Wo]", p.got)
	}
}

func TestSuggestArgsReady(t *testing.T) {
	for _, loading := range []bool{false, true} {
		p := &fakeProvider{loading: loading}
		got := NewSuggester().WithProvider("context", p).Suggest("ctx w")
		if got.ArgsReady == loading {
			t.Errorf("loading=%v: ArgsReady = %v", loading, got.ArgsReady)
		}
	}
}

func TestWithProviderResolvesAliasAndIgnoresUnknown(t *testing.T) {
	p := &fakeProvider{}
	s := NewSuggester().WithProvider("ctx", p).WithProvider("nope", &fakeProvider{})
	if s.providers["context"] != ArgProvider(p) {
		t.Errorf("alias not registered under the canonical name: %v", s.providers)
	}
	if len(s.providers) != 1 {
		t.Errorf("unknown command registered: %v", s.providers)
	}
}

func TestWithProviderIgnoresTypedNil(t *testing.T) {
	var p *fakeProvider
	s := NewSuggester().WithProvider("context", p)
	if len(s.providers) != 0 {
		t.Errorf("typed nil registered: %v", s.providers)
	}
	if got := s.Suggest("ctx w"); got.Stage != StageArgument {
		t.Errorf("Stage = %v", got.Stage)
	}
}

func TestSuggestRankOrderExactPrefixAlias(t *testing.T) {
	s := &Suggester{entries: []Entry{
		{Name: "zeta", Aliases: []string{"ab"}},
		{Name: "abc"},
		{Name: "alpha", Aliases: []string{"x"}},
		{Name: "ab"},
		{Name: "beta", Aliases: []string{"abz"}},
	}}
	got := names(s.Suggest("ab").Entries)
	want := []string{"ab", "zeta", "abc", "beta"} // exact (name asc), name prefix, alias prefix
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
