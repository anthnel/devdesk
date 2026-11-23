package command

import (
	"slices"
	"testing"
)

// TestListAliasRule pins the rule of the palette list (DEVDESK-747): one alias
// per entry, the first of 2 or 3 letters in declaration order, else the longest
// (the first declared on a tie), else nothing. It is red until ListAlias exists.
func TestListAliasRule(t *testing.T) {
	tests := []struct {
		name    string
		aliases []string
		want    string
	}{
		{"two aliases of 2-3 letters: the first declared", []string{"config", "cfg"}, "cfg"},
		{"2-3 letter alias declared second", []string{"ws", "w"}, "ws"},
		{"three letters before one letter", []string{"ctx", "c"}, "ctx"},
		{"two 2-3 letter aliases: declaration order, not length", []string{"abc", "xy"}, "abc"},
		{"two 2-3 letter aliases, the shorter first", []string{"xy", "abc"}, "xy"},
		{"a 2-3 letter alias wins over a longer one declared before it", []string{"abcd", "xy", "xyz"}, "xy"},
		{"a single alias of 2 letters", []string{"au"}, "au"},
		{"a single alias of 3 letters", []string{"sec"}, "sec"},
		{"no alias of 2-3 letters: the longest", []string{"dash", "d"}, "dash"},
		{"no alias of 2-3 letters: the longest, declared first", []string{"cont", "c"}, "cont"},
		{"no alias of 2-3 letters: the longest, declared last", []string{"d", "dash"}, "dash"},
		{"a single long alias", []string{"version"}, "version"},
		{"two long aliases of the same length: the first declared", []string{"abcd", "efgh"}, "abcd"},
		{"two long aliases of the same length, reversed", []string{"efgh", "abcd"}, "efgh"},
		{"the longest of three long aliases", []string{"abcd", "efghij", "klmno"}, "efghij"},
		{"a single one-letter alias", []string{"j"}, "j"},
		{"two one-letter aliases: the first declared", []string{"x", "y"}, "x"},
		{"wide characters count as letters, not bytes", []string{"別名一つ", "別名二つ"}, "別名一つ"},
		{"two wide letters are a 2-3 letter alias", []string{"別名一つ", "別名"}, "別名"},
		{"no alias at all", nil, ""},
		{"an empty alias list", []string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Entry{Name: "entry", Aliases: slices.Clone(tt.aliases)}
			if got := e.ListAlias(); got != tt.want {
				t.Errorf("ListAlias(%q) = %q, want %q", tt.aliases, got, tt.want)
			}
			if !slices.Equal(e.Aliases, tt.aliases) {
				t.Errorf("ListAlias changed Aliases: %q, want %q", e.Aliases, tt.aliases)
			}
		})
	}
}

// wantListAlias is the alias the palette list shows for each registry entry:
// five entries narrow to one alias, the twelve others have only one.
var wantListAlias = map[string]string{
	"about":         "version",
	"agent-usage":   "au",
	"configuration": "cfg",
	"containers":    "ct",
	"dashboard":     "dash",
	"files":         "fs",
	"git-auth":      "ga",
	"git-explorer":  "ge",
	"jobs":          "j",
	"netdiag":       "net",
	"oci-resources": "oci",
	"security":      "sec",
	"status":        "s",
	"templates":     "tpl",
	"workspaces":    "ws",
	"context":       "ctx",
	"quit":          "q",
}

func TestListAliasOfEveryRegistryEntry(t *testing.T) {
	entries := Entries()
	if len(entries) != len(wantListAlias) {
		t.Fatalf("registry has %d entries, the table pins %d", len(entries), len(wantListAlias))
	}
	for _, e := range entries {
		want, ok := wantListAlias[e.Name]
		if !ok {
			t.Errorf("entry %q is not in the table", e.Name)
			continue
		}
		if got := e.ListAlias(); got != want {
			t.Errorf("%s: ListAlias() = %q, want %q (aliases %q)", e.Name, got, want, e.Aliases)
		}
		if !slices.Contains(e.Aliases, e.ListAlias()) {
			t.Errorf("%s: ListAlias() %q is not one of its aliases %q", e.Name, e.ListAlias(), e.Aliases)
		}
	}
}
