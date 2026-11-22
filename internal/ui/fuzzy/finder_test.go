package fuzzy

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// cands builds candidates whose Key is "/k/<label>", in the given order.
func cands(labels ...string) []Candidate {
	out := make([]Candidate, len(labels))
	for i, l := range labels {
		out[i] = Candidate{Key: "/k/" + l, Label: l}
	}
	return out
}

// typed returns a prompt loaded with labels and the query typed into it.
func typed(t *testing.T, query string, labels ...string) *Finder {
	t.Helper()
	f := New("Loading")
	f.SetCandidates(cands(labels...), 0)
	for _, k := range testutil.Type(query) {
		f, _ = f.Update(k)
	}
	return f
}

// labels reads the ranked results back as labels.
func labels(f *Finder) []string {
	var out []string
	for _, c := range f.results.Items() {
		out = append(out, c.Label)
	}
	return out
}

// press sends one key and fails the test when it produced a Confirm or Cancel.
func press(t *testing.T, f *Finder, name string) *Finder {
	t.Helper()
	f, cmd := f.Update(testutil.Key(name))
	for _, msg := range testutil.Msgs(cmd) {
		switch msg.(type) {
		case ConfirmMsg, CancelMsg:
			t.Errorf("%q produced %T", name, msg)
		}
	}
	return f
}

func TestModeString(t *testing.T) {
	if got := ModeFuzzy.String(); got != "fuzzy" {
		t.Errorf("ModeFuzzy.String() = %q, want %q", got, "fuzzy")
	}
	if got := ModeExact.String(); got != "exact" {
		t.Errorf("ModeExact.String() = %q, want %q", got, "exact")
	}
	var zero Mode
	if zero != ModeFuzzy {
		t.Error("the zero Mode must be ModeFuzzy")
	}
}

// Criterion 1: a freshly built prompt is in fuzzy mode, every time.
func TestNewStartsInFuzzyMode(t *testing.T) {
	f := New("Loading")
	if f.Mode() != ModeFuzzy {
		t.Errorf("Mode() = %v on a new prompt, want %v", f.Mode(), ModeFuzzy)
	}
	_ = press(t, f, "tab")
	if g := New("Loading"); g.Mode() != ModeFuzzy {
		t.Errorf("Mode() = %v on a second new prompt, want %v (no state shared)", g.Mode(), ModeFuzzy)
	}
}

// Criterion 2: tab flips the mode and back, leaves the query alone, and is
// never a confirm or a cancel.
func TestTabTogglesTheMode(t *testing.T) {
	f := typed(t, "dev", "docs/data-env", "workspaces/devdesk")
	want := []Mode{ModeExact, ModeFuzzy, ModeExact}
	for i, m := range want {
		f = press(t, f, "tab")
		if f.Mode() != m {
			t.Fatalf("after %d tab(s) Mode() = %v, want %v", i+1, f.Mode(), m)
		}
		if got := f.query.Value(); got != "dev" {
			t.Errorf("after %d tab(s) the query is %q, want it untouched", i+1, got)
		}
	}
}

// Criteria 2, 3 and 4: the list is recomputed on tab, without retyping.
func TestTabRematchesAtOnce(t *testing.T) {
	tests := []struct {
		name  string
		query string
		cands []string
		fuzzy []string // results before tab
		exact []string // results after tab
	}{
		{
			name:  "substring only",
			query: "dev",
			cands: []string{"docs/data-env", "workspaces/devdesk"},
			fuzzy: []string{"workspaces/devdesk", "docs/data-env"},
			exact: []string{"workspaces/devdesk"},
		},
		{
			name:  "subsequence only",
			query: "wsd",
			cands: []string{"docs/data-env", "workspaces/devdesk"},
			fuzzy: []string{"workspaces/devdesk"},
			exact: nil,
		},
		{
			name:  "case is folded",
			query: "DEV",
			cands: []string{"docs/data-env", "Workspaces/DevDesk"},
			fuzzy: []string{"Workspaces/DevDesk", "docs/data-env"},
			exact: []string{"Workspaces/DevDesk"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := typed(t, tc.query, tc.cands...)
			if got := labels(f); !slices.Equal(got, tc.fuzzy) {
				t.Fatalf("fuzzy results = %q, want %q", got, tc.fuzzy)
			}
			f = press(t, f, "tab")
			if got := labels(f); !slices.Equal(got, tc.exact) {
				t.Errorf("exact results = %q, want %q", got, tc.exact)
			}
			if tc.name == "substring only" {
				if got, want := f.MatchHint(), "1 match(es)"; got != want {
					t.Errorf("MatchHint() = %q, want %q", got, want)
				}
			}
			f = press(t, f, "tab")
			if got := labels(f); !slices.Equal(got, tc.fuzzy) {
				t.Errorf("results after tab twice = %q, want the fuzzy ones %q", got, tc.fuzzy)
			}
		})
	}
}

// Criterion 5: boundary first, then the shortest label, then the order the
// view gave.
func TestExactRanking(t *testing.T) {
	tests := []struct {
		name  string
		query string
		cands []string
		want  []string
	}{
		{
			name:  "boundary, then shortest",
			query: "dev",
			cands: []string{"a/zdevz", "b/devops", "devdesk", "xdev/dev"},
			want:  []string{"devdesk", "b/devops", "xdev/dev", "a/zdevz"},
		},
		{
			name:  "same length keeps the input order",
			query: "dev",
			cands: []string{"b/devx", "a/devy", "c/devz"},
			want:  []string{"b/devx", "a/devy", "c/devz"},
		},
		{
			name:  "same length keeps the input order, reversed input",
			query: "dev",
			cands: []string{"c/devz", "a/devy", "b/devx"},
			want:  []string{"c/devz", "a/devy", "b/devx"},
		},
		{
			name:  "a boundary beats a shorter mid-word match",
			query: "dev",
			cands: []string{"zdev", "a/developer"},
			want:  []string{"a/developer", "zdev"},
		},
		{
			name:  "length counts runes, not bytes",
			query: "été",
			cands: []string{"ab/été-xx", "é/été"},
			want:  []string{"é/été", "ab/été-xx"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := typed(t, tc.query, tc.cands...)
			f = press(t, f, "tab")
			if got := labels(f); !slices.Equal(got, tc.want) {
				t.Errorf("exact order = %q, want %q", got, tc.want)
			}
		})
	}
}

// A tab puts the cursor back on the best match, like a new query does.
func TestTabMovesTheCursorToTheTop(t *testing.T) {
	f := typed(t, "dev", "a/zdevz", "b/devops", "devdesk", "xdev/dev")
	f = press(t, f, "down")
	f = press(t, f, "down")
	f = press(t, f, "tab")
	if got := f.results.Cursor(); got != 0 {
		t.Errorf("cursor = %d after tab, want 0", got)
	}
	// enter then picks the best exact match.
	_, cmd := f.Update(testutil.Key("enter"))
	var confirmed *ConfirmMsg
	for _, msg := range testutil.Msgs(cmd) {
		if c, ok := msg.(ConfirmMsg); ok {
			confirmed = &c
		}
	}
	if confirmed == nil || confirmed.Key != "/k/devdesk" {
		t.Errorf("enter after tab confirmed %+v, want /k/devdesk", confirmed)
	}
}

// Criterion 7: the threshold is the same in exact mode, and the mode still
// flips below it.
func TestTabBelowTheThreshold(t *testing.T) {
	for _, query := range []string{"", "d", "de"} {
		t.Run("query "+query, func(t *testing.T) {
			f := typed(t, query, "devdesk", "dev/x")
			f = press(t, f, "tab")
			if f.Mode() != ModeExact {
				t.Errorf("Mode() = %v, want %v", f.Mode(), ModeExact)
			}
			if got := labels(f); len(got) != 0 {
				t.Errorf("results = %q below MinQueryLen, want none", got)
			}
			if got := f.MatchHint(); got != "" {
				t.Errorf("MatchHint() = %q below MinQueryLen, want empty", got)
			}
			if got := f.StatusText(); !strings.Contains(got, "at least") {
				t.Errorf("StatusText() = %q, want the minimum-length hint", got)
			}
			// Crossing the threshold applies the exact rule.
			for _, k := range testutil.Type(strings.Repeat("e", MinQueryLen-len(query))) {
				f, _ = f.Update(k)
			}
			if got := labels(f); len(got) != 0 {
				t.Errorf("results for %q = %q, want none (no such substring)", f.query.Value(), got)
			}
		})
	}
}

// Criterion 2: a tab while the candidates are still loading changes the mode;
// they then land and are ranked the exact way.
func TestTabWhileLoading(t *testing.T) {
	f := New("Loading")
	for _, k := range testutil.Type("dev") {
		f, _ = f.Update(k)
	}
	f = press(t, f, "tab")
	if f.Mode() != ModeExact {
		t.Fatalf("Mode() = %v while loading, want %v", f.Mode(), ModeExact)
	}
	if !f.Loading() {
		t.Error("tab must not end the loading state")
	}
	if got := labels(f); len(got) != 0 {
		t.Errorf("results = %q while loading, want none", got)
	}

	f.SetCandidates(cands("a/zdevz", "b/devops", "devdesk", "xdev/dev", "docs/data-env"), 0)
	if f.Mode() != ModeExact {
		t.Errorf("SetCandidates reset the mode to %v", f.Mode())
	}
	want := []string{"devdesk", "b/devops", "xdev/dev", "a/zdevz"}
	if got := labels(f); !slices.Equal(got, want) {
		t.Errorf("results after SetCandidates = %q, want the exact ranking %q", got, want)
	}
}

// SetCandidates can be called again while the prompt is open (a fresher index
// lands): the mode, like the query, is kept.
func TestSetCandidatesKeepsTheMode(t *testing.T) {
	f := typed(t, "dev", "docs/data-env", "workspaces/devdesk")
	f = press(t, f, "tab")
	f.SetCandidates(cands("docs/data-env", "workspaces/devdesk", "x/dev"), 0)
	if f.Mode() != ModeExact {
		t.Errorf("Mode() = %v after a second SetCandidates, want %v", f.Mode(), ModeExact)
	}
	want := []string{"x/dev", "workspaces/devdesk"}
	if got := labels(f); !slices.Equal(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
}

// Criteria 1 and 2: the bar names the mode, before the typed query.
func TestBarShowsTheMode(t *testing.T) {
	f := typed(t, "dev", "docs/data-env", "workspaces/devdesk")
	tests := []struct {
		mode, other string
	}{{"fuzzy", "exact"}, {"exact", "fuzzy"}}
	for i, tc := range tests {
		if i == 1 {
			f = press(t, f, "tab")
		}
		bar := ansi.Strip(f.RenderBar(80))
		at := strings.Index(bar, tc.mode)
		if at < 0 {
			t.Fatalf("bar %q does not name the mode %q", bar, tc.mode)
		}
		if q := strings.Index(bar, "dev"); q < 0 || at > q {
			t.Errorf("bar %q: the mode label must come before the query", bar)
		}
		if strings.Contains(bar, tc.other) {
			t.Errorf("bar %q names %q while the mode is %q", bar, tc.other, tc.mode)
		}
		if !strings.Contains(bar, "Find") {
			t.Errorf("bar %q lost its Find label", bar)
		}
		if h := lipgloss.Height(f.RenderBar(80)); h != 2 {
			t.Errorf("bar is %d lines in %s mode, want 2", h, tc.mode)
		}
	}
	if FooterHeight != 4 {
		t.Errorf("FooterHeight = %d, want 4", FooterHeight)
	}
}

// Criterion 8: the shortcut offered to the header and the help names the mode
// tab would switch to.
func TestModeShortcut(t *testing.T) {
	f := New("Loading")
	want := []shortcut.Shortcut{
		{Key: "tab", Description: "Exact match"},
		{Key: "tab", Description: "Fuzzy match"},
		{Key: "tab", Description: "Exact match"},
	}
	for i, w := range want {
		if i > 0 {
			f = press(t, f, "tab")
		}
		got := f.ModeShortcut()
		if got.Key != w.Key || got.Description != w.Description {
			t.Errorf("after %d tab(s) ModeShortcut() = {%q %q}, want {%q %q}",
				i, got.Key, got.Description, w.Key, w.Description)
		}
		if got.Disabled {
			t.Errorf("after %d tab(s) the mode shortcut is greyed: it always applies", i)
		}
	}
}
