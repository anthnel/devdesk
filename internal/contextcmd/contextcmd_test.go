package contextcmd

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func snap(current, previous string, names ...string) Snapshot {
	return Snapshot{Names: names, Current: current, Previous: previous, Ready: true}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		highlighted string
		snap        Snapshot
		want        Action
	}{
		{"no args lists", nil, "", snap("work", "", "web", "work"), Action{Kind: List}},
		{"list alias", []string{"list"}, "", snap("work", "", "web", "work"), Action{Kind: List}},
		{"already current", []string{"work"}, "", snap("work", "", "web", "work"),
			Action{Kind: Refuse, Level: Info, Text: `Already on context "work"`}},
		{"existing switches", []string{"work"}, "", snap("web", "", "web", "work"),
			Action{Kind: Switch, Name: "work"}},
		{"ambiguous prefix", []string{"w"}, "", snap("web", "", "web", "work"),
			Action{Kind: KeepOpen, Level: Warn, Text: `"w" matches web, work — type more letters or press tab`}},
		{"ambiguous but chosen with tab", []string{"w"}, "work", snap("web", "", "web", "work"),
			Action{Kind: Switch, Name: "work"}},
		{"single prefix match", []string{"wo"}, "", snap("web", "", "web", "work"),
			Action{Kind: Switch, Name: "work"}},
		{"unknown no close", []string{"zzz"}, "", snap("web", "", "web", "work"),
			Action{Kind: Refuse, Level: Error, Text: `Context "zzz" not found — use :ctx new zzz to create it`}},
		{"unknown no close, wrok alone", []string{"wrok"}, "", snap("a", "", "a", "b"),
			Action{Kind: Refuse, Level: Error, Text: `Context "wrok" not found — use :ctx new wrok to create it`}},
		{"unknown with close", []string{"wrok"}, "", snap("web", "", "web", "work"),
			Action{Kind: Refuse, Level: Error, Text: `Context "wrok" not found — did you mean "work"? Use :ctx new wrok to create it`}},
		{"uppercase invalid even if lowercase exists", []string{"Work"}, "", snap("web", "", "web", "work"),
			Action{Kind: Refuse, Level: Error, Text: `Invalid context name "Work" — use lowercase letters, digits and hyphens`}},
		{"previous empty", []string{"-"}, "", snap("web", "", "web", "work"),
			Action{Kind: Refuse, Level: Warn, Text: "No previous context in this session"}},
		{"previous set", []string{"-"}, "", snap("web", "a", "a", "web"),
			Action{Kind: Switch, Name: "a"}},
		{"new without name", []string{"new"}, "", snap("web", "", "web"),
			Action{Kind: KeepOpen, Level: Warn, Text: "Usage: :ctx new <name>"}},
		{"new creates", []string{"new", "staging"}, "", snap("web", "", "web"),
			Action{Kind: Create, Name: "staging"}},
		{"new existing", []string{"new", "staging"}, "", snap("web", "", "web", "staging"),
			Action{Kind: Refuse, Level: Warn, Text: `Context "staging" already exists — use :ctx staging to switch`}},
		{"new reserved new", []string{"new", "new"}, "", snap("web", "", "web"),
			Action{Kind: Refuse, Level: Error, Text: `"new" is reserved — choose another name`}},
		{"new reserved dash", []string{"new", "-"}, "", snap("web", "", "web"),
			Action{Kind: Refuse, Level: Error, Text: `"-" is reserved — choose another name`}},
		{"new reserved list", []string{"new", "list"}, "", snap("web", "", "web"),
			Action{Kind: Refuse, Level: Error, Text: `"list" is reserved — choose another name`}},
		{"new uppercase invalid", []string{"new", "Staging"}, "", snap("web", "", "web"),
			Action{Kind: Refuse, Level: Error, Text: `Invalid context name "Staging" — use lowercase letters, digits and hyphens`}},
		{"new underscore invalid", []string{"new", "my_ctx"}, "", snap("web", "", "web"),
			Action{Kind: Refuse, Level: Error, Text: `Invalid context name "my_ctx" — use lowercase letters, digits and hyphens`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.args, tt.highlighted, tt.snap)
			if got != tt.want {
				t.Errorf("Resolve(%q, %q) = %+v, want %+v", tt.args, tt.highlighted, got, tt.want)
			}
		})
	}
}

func TestResolveNeverSwitchesToDash(t *testing.T) {
	for _, args := range [][]string{{"-"}, {"new", "-"}} {
		for _, prev := range []string{"", "a"} {
			got := Resolve(args, "", snap("web", prev, "a", "web"))
			if (got.Kind == Switch || got.Kind == Create) && got.Name == "-" {
				t.Errorf("Resolve(%q) targeted %q: %+v", args, "-", got)
			}
			if got.Kind == Create {
				t.Errorf("Resolve(%q) created: %+v", args, got)
			}
		}
	}
}

func TestResolveNotReady(t *testing.T) {
	s := Snapshot{Names: []string{"web", "work"}, Current: "web", Previous: "work"}
	for _, args := range [][]string{{"work"}, {"-"}, {"new", "staging"}, {"wo"}, {"new"}, {"wrok"}} {
		got := Resolve(args, "", s)
		if got.Kind != KeepOpen {
			t.Errorf("Resolve(%q) not ready: Kind = %v, want KeepOpen (%+v)", args, got.Kind, got)
		}
		if got.Name != "" {
			t.Errorf("Resolve(%q) not ready: Name = %q, want empty", args, got.Name)
		}
	}
}

func TestMatch(t *testing.T) {
	names := []string{"web", "work", "Wonder", "alpha"}
	tests := []struct {
		name   string
		prefix string
		names  []string
		want   []string
	}{
		{"empty returns all", "", names, names},
		{"case insensitive", "WO", names, []string{"work", "Wonder"}},
		{"exact first", "work", []string{"workshop", "work", "work2"}, []string{"work", "workshop", "work2"}},
		{"exact first case-insensitive", "WORK", []string{"workshop", "work"}, []string{"work", "workshop"}},
		{"stable order", "w", []string{"web", "work", "wiki"}, []string{"web", "work", "wiki"}},
		{"no match", "zz", names, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Match(tt.prefix, tt.names)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Match(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestDidYouMean(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		names []string
		want  string
		ok    bool
	}{
		{"transposition", "wrok", []string{"web", "work"}, "work", true},
		{"distance 3 with common start 3 is offered", "abcdef", []string{"abcxyz"}, "abcxyz", true},
		{"distance 3 with common start 2 is not", "abdefg", []string{"abxyzq"}, "", false},
		{"common prefix 3", "staging-eu-west", []string{"staging"}, "staging", true},
		{"prefix 2 not enough", "wxyzabc", []string{"web"}, "", false},
		{"none", "zzz", []string{"web", "work"}, "", false},
		{"empty names", "work", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DidYouMean(tt.in, tt.names)
			if got != tt.want || ok != tt.ok {
				t.Errorf("DidYouMean(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestResolvePreviousEdgeCases(t *testing.T) {
	t.Run("previous deleted never switches or creates", func(t *testing.T) {
		got := Resolve([]string{"-"}, "", snap("web", "gone", "web", "work"))
		if got.Kind == Switch || got.Kind == Create {
			t.Fatalf("Resolve(-) with a deleted previous = %+v, want no Switch/Create", got)
		}
		if got.Level != Error {
			t.Errorf("Level = %v, want Error (%+v)", got.Level, got)
		}
		if !strings.Contains(got.Text, `"gone"`) {
			t.Errorf("Text = %q, want it to name the missing context", got.Text)
		}
	})
	t.Run("previous equals current is already-on", func(t *testing.T) {
		got := Resolve([]string{"-"}, "", snap("web", "web", "web", "work"))
		want := Action{Kind: Refuse, Level: Info, Text: `Already on context "web"`}
		if got != want {
			t.Errorf("Resolve(-) = %+v, want %+v", got, want)
		}
	})
}

func TestResolveEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		highlighted string
		snap        Snapshot
		want        Action
	}{
		{"unique prefix of current is already-on", []string{"wo"}, "", snap("work", "", "web", "work"),
			Action{Kind: Refuse, Level: Info, Text: `Already on context "work"`}},
		{"two args is usage", []string{"a", "b"}, "", snap("web", "", "a", "b", "web"),
			Action{Kind: KeepOpen, Level: Warn, Text: "Usage: :ctx <name>"}},
		{"list before loading still lists", []string{"list"}, "", Snapshot{Names: []string{"web"}, Current: "web"},
			Action{Kind: List}},
		{"no args before loading still lists", nil, "", Snapshot{},
			Action{Kind: List}},
		{"highlight outside the matches is ignored", []string{"w"}, "zzz", snap("a", "", "web", "work"),
			Action{Kind: KeepOpen, Level: Warn, Text: `"w" matches web, work — type more letters or press tab`}},
		{"duplicate names are listed once", []string{"w"}, "", snap("a", "", "web", "work", "work"),
			Action{Kind: KeepOpen, Level: Warn, Text: `"w" matches web, work — type more letters or press tab`}},
		{"duplicate collapsing to one match switches", []string{"wo"}, "", snap("web", "", "web", "work", "work"),
			Action{Kind: Switch, Name: "work"}},
		{"empty name is never suggested", []string{"ab"}, "", snap("web", "", "", "xyz"),
			Action{Kind: Refuse, Level: Error, Text: `Context "ab" not found — use :ctx new ab to create it`}},
		{"short names get no suggestion", []string{"qa"}, "", snap("web", "", "ci", "web"),
			Action{Kind: Refuse, Level: Error, Text: `Context "qa" not found — use :ctx new qa to create it`}},
		{"close long name still suggested", []string{"workshop"}, "", snap("web", "", "web", "workspace"),
			Action{Kind: Refuse, Level: Error, Text: `Context "workshop" not found — did you mean "workspace"? Use :ctx new workshop to create it`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.args, tt.highlighted, tt.snap)
			if got != tt.want {
				t.Errorf("Resolve(%q, %q) = %+v, want %+v", tt.args, tt.highlighted, got, tt.want)
			}
		})
	}
}

func TestResolveNotReadyReportsLoading(t *testing.T) {
	got := Resolve([]string{"work"}, "", Snapshot{Names: []string{"web", "work"}})
	want := Action{Kind: KeepOpen, Level: Warn, Text: "Contexts are still loading — try again"}
	if got != want {
		t.Errorf("Resolve not ready = %+v, want %+v", got, want)
	}
}

func TestResolveAmbiguousIsCapped(t *testing.T) {
	var names []string
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("p%02d", i))
	}
	got := Resolve([]string{"p"}, "", snap("web", "", names...))
	if got.Kind != KeepOpen || got.Level != Warn {
		t.Fatalf("Resolve = %+v, want KeepOpen/Warn", got)
	}
	want := `"p" matches p01, p02, p03, p04, p05, p06, p07, p08, p09, p10, and 2 more — type more letters or press tab`
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
	// Exactly 10 matches are listed in full, with no "more" suffix.
	got = Resolve([]string{"p"}, "", snap("web", "", names[:10]...))
	if strings.Contains(got.Text, ", and ") {
		t.Errorf("10 matches should not be truncated: %q", got.Text)
	}
}

func TestDidYouMeanEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		names []string
		want  string
		ok    bool
	}{
		{"long shared start (5+) is offered", "workshop", []string{"workspace"}, "workspace", true},
		{"short names: distance capped near half the length", "qa", []string{"ci"}, "", false},
		{"one-char names get nothing", "a", []string{"b"}, "", false},
		{"empty name is never offered", "ab", []string{""}, "", false},
		{"empty name skipped, real one found", "wrok", []string{"", "work"}, "work", true},
		{"duplicates are harmless", "wrok", []string{"work", "work"}, "work", true},
		{"tie goes to the first in Names", "ab", []string{"ac", "ad"}, "ac", true},
		{"tie goes to the first in Names (reversed)", "ab", []string{"ad", "ac"}, "ad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DidYouMean(tt.in, tt.names)
			if got != tt.want || ok != tt.ok {
				t.Errorf("DidYouMean(%q, %q) = (%q, %v), want (%q, %v)", tt.in, tt.names, got, ok, tt.want, tt.ok)
			}
		})
	}
}
