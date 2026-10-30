package command

import (
	"reflect"
	"slices"
	"testing"
	"unicode/utf8"
)

// wantEntries is the expected registry: views A-Z, then actions A-Z, aliases
// in declaration order.
var wantEntries = []struct {
	name    string
	aliases []string
	kind    EntryKind
	arg     string
}{
	{"about", []string{"version"}, KindView, ""},
	{"agent-usage", []string{"au"}, KindView, ""},
	{"configuration", []string{"config", "cfg"}, KindView, ""},
	{"containers", []string{"cont", "ct"}, KindView, ""},
	{"dashboard", []string{"dash", "d"}, KindView, ""},
	{"files", []string{"fs"}, KindView, ""},
	{"git-auth", []string{"ga"}, KindView, ""},
	{"git-explorer", []string{"ge"}, KindView, ""},
	{"jobs", []string{"j"}, KindView, ""},
	{"netdiag", []string{"net"}, KindView, ""},
	{"oci-resources", []string{"oci"}, KindView, ""},
	{"security", []string{"sec"}, KindView, ""},
	{"status", []string{"s"}, KindView, ""},
	{"templates", []string{"tpl"}, KindView, ""},
	{"workspaces", []string{"ws", "w"}, KindView, ""},
	{"context", []string{"ctx", "c"}, KindAction, "<name>"},
	{"quit", []string{"q"}, KindAction, ""},
}

func TestEntriesMatchExpectedRegistry(t *testing.T) {
	got := Entries()
	if len(got) != len(wantEntries) {
		t.Fatalf("len(Entries()) = %d, want %d", len(got), len(wantEntries))
	}
	for i, w := range wantEntries {
		e := got[i]
		if e.Name != w.name {
			t.Errorf("entry %d Name = %q, want %q", i, e.Name, w.name)
		}
		if !slices.Equal(e.Aliases, w.aliases) {
			t.Errorf("%s Aliases = %v, want %v", w.name, e.Aliases, w.aliases)
		}
		if e.Kind != w.kind {
			t.Errorf("%s Kind = %v, want %v", w.name, e.Kind, w.kind)
		}
		if e.Arg != w.arg {
			t.Errorf("%s Arg = %q, want %q", w.name, e.Arg, w.arg)
		}
	}
}

func TestEntriesOrderViewsThenActionsAlphabetical(t *testing.T) {
	var views, actions []string
	seenAction := false
	for _, e := range Entries() {
		if e.Kind == KindAction {
			seenAction = true
			actions = append(actions, e.Name)
			continue
		}
		if seenAction {
			t.Errorf("view %q after an action", e.Name)
		}
		views = append(views, e.Name)
	}
	if len(views) == 0 || len(actions) == 0 {
		t.Fatalf("views=%d actions=%d, want both non-empty", len(views), len(actions))
	}
	if !slices.IsSorted(views) {
		t.Errorf("views not sorted: %v", views)
	}
	if !slices.IsSorted(actions) {
		t.Errorf("actions not sorted: %v", actions)
	}
}

func entryNames() []string {
	var names []string
	for _, e := range Entries() {
		names = append(names, e.Name)
	}
	return names
}

func TestEntryNamesEqualFullNames(t *testing.T) {
	got, want := entryNames(), FullNames()
	if len(got) == 0 || !slices.Equal(got, want) {
		t.Errorf("Entries names = %v, FullNames() = %v", got, want)
	}
}

func TestFullNamesIsDeterministic(t *testing.T) {
	first := FullNames()
	for i := 0; i < 20; i++ {
		if next := FullNames(); !slices.Equal(first, next) {
			t.Fatalf("FullNames() order changed: %v vs %v", first, next)
		}
	}
}

func TestEveryAliasIsInExactlyOneEntryOfItsTarget(t *testing.T) {
	entries := Entries()
	if len(entries) == 0 {
		t.Fatal("Entries() is empty")
	}
	for alias, target := range GetAliases() {
		var owners []string
		for _, e := range entries {
			if slices.Contains(e.Aliases, alias) {
				owners = append(owners, e.Name)
			}
		}
		if len(owners) != 1 || owners[0] != target {
			t.Errorf("alias %q (target %q) found in entries %v", alias, target, owners)
		}
	}
	total := 0
	for _, e := range entries {
		total += len(e.Aliases)
	}
	if total != len(GetAliases()) {
		t.Errorf("entries carry %d aliases, GetAliases() has %d", total, len(GetAliases()))
	}
}

func TestNoWordIsUsedByTwoEntries(t *testing.T) {
	owner := map[string]string{}
	for _, e := range Entries() {
		for _, w := range append([]string{e.Name}, e.Aliases...) {
			if prev, dup := owner[w]; dup {
				t.Errorf("word %q is used by both %q and %q", w, prev, e.Name)
			}
			owner[w] = e.Name
		}
	}
}

func TestEntriesAreStableAcrossCalls(t *testing.T) {
	first := Entries()
	if len(first) == 0 {
		t.Fatal("Entries() is empty")
	}
	for i := 0; i < 20; i++ {
		if next := Entries(); !reflect.DeepEqual(first, next) {
			t.Fatalf("Entries() changed between calls")
		}
	}
}

func TestEveryDescriptionIsNonEmptyAndShort(t *testing.T) {
	entries := Entries()
	if len(entries) == 0 {
		t.Fatal("Entries() is empty")
	}
	for _, e := range entries {
		n := utf8.RuneCountInString(e.Description)
		if n == 0 {
			t.Errorf("%s has an empty description", e.Name)
		}
		if n > MaxDescriptionLen {
			t.Errorf("%s description is %d runes, max %d", e.Name, n, MaxDescriptionLen)
		}
	}
}

func TestContextTakesAnArgument(t *testing.T) {
	var ctx *Entry
	for _, e := range Entries() {
		if e.Name == "context" {
			ctx = &e
		}
	}
	if ctx == nil {
		t.Fatal("no context entry")
	}
	if !ctx.TakesArg() {
		t.Error("context.TakesArg() = false, want true")
	}
	if got := ctx.Label(); got != "context <name>" {
		t.Errorf("Label() = %q, want %q", got, "context <name>")
	}
}

func TestEntriesWithoutArgUseNameAsLabel(t *testing.T) {
	entries := Entries()
	if len(entries) == 0 {
		t.Fatal("Entries() is empty")
	}
	for _, e := range entries {
		if e.Name == "context" {
			continue
		}
		if e.TakesArg() {
			t.Errorf("%s.TakesArg() = true, want false", e.Name)
		}
		if e.Label() != e.Name {
			t.Errorf("%s.Label() = %q, want %q", e.Name, e.Label(), e.Name)
		}
	}
}

func TestEntriesReturnsACopy(t *testing.T) {
	a := Entries()
	if len(a) == 0 {
		t.Fatal("Entries() is empty")
	}
	wantName, wantAlias := a[0].Name, ""
	if len(a[0].Aliases) > 0 {
		wantAlias = a[0].Aliases[0]
	}
	a[0].Name = "mutated"
	if len(a[0].Aliases) > 0 {
		a[0].Aliases[0] = "mutated"
	}
	a[0], a[len(a)-1] = a[len(a)-1], a[0]
	b := Entries()
	if b[0].Name != wantName {
		t.Errorf("mutation leaked: first Name = %q, want %q", b[0].Name, wantName)
	}
	if wantAlias != "" && b[0].Aliases[0] != wantAlias {
		t.Errorf("alias mutation leaked: %q, want %q", b[0].Aliases[0], wantAlias)
	}
}
