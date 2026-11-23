package command

import (
	"slices"
	"strings"
	"testing"
)

// The palette list shows one alias per entry (DEVDESK-747); typing and
// filtering must keep accepting every alias. These tests do not call
// ListAlias: they pin what must not change.

// allSpellings is every alias of the five entries whose list line narrows to
// one alias, with the view or command it names.
var allSpellings = []struct {
	word string
	view ViewType
}{
	{"configuration", ViewConfiguration}, {"config", ViewConfiguration}, {"cfg", ViewConfiguration},
	{"containers", ViewContainers}, {"cont", ViewContainers}, {"ct", ViewContainers},
	{"dashboard", ViewDashboard}, {"dash", ViewDashboard}, {"d", ViewDashboard},
	{"workspaces", ViewWorkspaces}, {"ws", ViewWorkspaces}, {"w", ViewWorkspaces},
}

func TestEveryAliasIsStillTypedAndParsed(t *testing.T) {
	for _, s := range allSpellings {
		if got, err := Parse(s.word); err != nil || got != s.view {
			t.Errorf("Parse(%q) = %q, %v; want %q", s.word, got, err, s.view)
		}
		for _, in := range []string{s.word, ":" + s.word, strings.ToUpper(s.word), "  " + s.word + " "} {
			if cmd := ParseCommand(in); cmd.Type != CommandView || cmd.View != s.view {
				t.Errorf("ParseCommand(%q) = %+v, want view %q", in, cmd, s.view)
			}
		}
	}
	for _, word := range []string{"context", "ctx", "c"} {
		cmd := ParseCommand(word + " work")
		if cmd.Type != CommandContext || !slices.Equal(cmd.Args, []string{"work"}) {
			t.Errorf("ParseCommand(%q) = %+v, want the context command with arg work", word+" work", cmd)
		}
	}
}

func TestGetAliasesAndFullNamesKeepEveryAlias(t *testing.T) {
	aliases := GetAliases()
	want := map[string]string{
		"config": "configuration", "cfg": "configuration",
		"cont": "containers", "ct": "containers",
		"dash": "dashboard", "d": "dashboard",
		"ws": "workspaces", "w": "workspaces",
		"ctx": "context", "c": "context",
	}
	for alias, target := range want {
		if got, ok := aliases[alias]; !ok || got != target {
			t.Errorf("GetAliases()[%q] = %q, %v; want %q", alias, got, ok, target)
		}
	}
	names := FullNames()
	for _, name := range []string{"configuration", "containers", "dashboard", "workspaces", "context"} {
		if !slices.Contains(names, name) {
			t.Errorf("FullNames() lacks %q", name)
		}
	}
	for alias := range want {
		if slices.Contains(names, alias) {
			t.Errorf("FullNames() lists the alias %q", alias)
		}
	}
	if len(names) != 17 {
		t.Errorf("FullNames() has %d names, want 17", len(names))
	}
}

func TestEntriesStillCarryEveryAlias(t *testing.T) {
	want := map[string][]string{
		"configuration": {"config", "cfg"},
		"containers":    {"cont", "ct"},
		"dashboard":     {"dash", "d"},
		"workspaces":    {"ws", "w"},
		"context":       {"ctx", "c"},
	}
	for _, e := range Entries() {
		if w, ok := want[e.Name]; ok && !slices.Equal(e.Aliases, w) {
			t.Errorf("%s Aliases = %q, want %q", e.Name, e.Aliases, w)
		}
	}
}

// hiddenAliases are the spellings the palette list no longer shows, with the
// entry each one finds.
var hiddenAliases = []struct{ alias, entry string }{
	{"config", "configuration"},
	{"cont", "containers"},
	{"d", "dashboard"},
	{"w", "workspaces"},
	{"c", "context"},
}

// A hidden alias still finds its entry, ranked first (it is an exact match),
// and so does every prefix of it. Each of those is also a prefix of the
// canonical name, which is what keeps the displayed line explainable.
func TestSuggestStillFindsAnEntryByAHiddenAlias(t *testing.T) {
	s := NewSuggester()
	for _, h := range hiddenAliases {
		got := s.Suggest(h.alias)
		if len(got.Entries) == 0 || got.Entries[0].Name != h.entry {
			t.Errorf("Suggest(%q) first = %v, want %q first", h.alias, names(got.Entries), h.entry)
		}
		for n := 1; n <= len(h.alias); n++ {
			prefix := h.alias[:n]
			if !slices.Contains(names(s.Suggest(prefix).Entries), h.entry) {
				t.Errorf("Suggest(%q) does not find %q", prefix, h.entry)
			}
			if !strings.HasPrefix(h.entry, prefix) {
				t.Errorf("hidden alias %q: prefix %q does not start the name %q", h.alias, prefix, h.entry)
			}
		}
	}
	for _, in := range []string{"dash", "cfg", "ct", "ws", "ctx"} {
		if got := s.Suggest(in); len(got.Entries) == 0 {
			t.Errorf("Suggest(%q) finds nothing", in)
		}
	}
}
