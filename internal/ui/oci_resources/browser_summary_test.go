package ociresources

import (
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-305 (IS-13), steps 2 and 3: fitNames abbreviates a list of registry
// names to a width; the level 1 summary is derived from the lot's keys and the
// verdicts.

// ── fitNames ─────────────────────────────────────────────────────────────────

func TestFitNames(t *testing.T) {
	tests := []struct {
		name   string
		names  []string
		suffix string
		width  int
		want   string
	}{
		{"everything fits", []string{"dhi.io", "nexus-prod"}, "", 18, "dhi.io, nexus-prod"},
		{"one name and a count", []string{"dhi.io", "nexus-prod"}, "", 10, "dhi.io, +1"},
		{"only a count", []string{"dhi.io", "nexus-prod"}, "", 9, "+2"},
		{"keeps as many names as fit", []string{"a", "b", "cccccc"}, "", 8, "a, b, +1"},
		{"one fewer name", []string{"a", "b", "cccccc"}, "", 7, "a, +2"},
		{"suffix fits", []string{"nexus-prod"}, skippedHint, 40, "nexus-prod (type 2+ characters)"},
		{"suffix dropped before the name (V3)", []string{"nexus-prod"}, skippedHint, 20, "nexus-prod"},
		{"count and suffix", []string{"p1", "p2"}, skippedHint, 25, "+2 (type 2+ characters)"},
		{"count alone when nothing else fits", []string{"nexus-prod"}, skippedHint, 5, "+1"},
		{"width 1 overflows with the count", []string{"a", "b"}, "", 1, "+2"},
		{"width 0 overflows with the count", []string{"a", "b", "c"}, "", 0, "+3"},
		{"no width limit shows all", []string{"a", "b", "c"}, "", noWidthLimit, "a, b, c"},
		{"no width limit keeps the suffix", []string{"a"}, skippedHint, noWidthLimit, "a (type 2+ characters)"},
		{"nil names", nil, "", 40, ""},
		{"nil names with a suffix", nil, skippedHint, 40, ""},
		{"wide characters count for 2 cells", []string{"日本"}, "", 4, "日本"},
		{"wide characters do not fit in 3", []string{"日本"}, "", 3, "+1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fitNames(tt.names, tt.suffix, tt.width); got != tt.want {
				t.Errorf("fitNames(%v, %q, %d) = %q, want %q", tt.names, tt.suffix, tt.width, got, tt.want)
			}
		})
	}
}

func TestSkippedHintReadsTheCatalogMinimum(t *testing.T) {
	want := " (type " + strconv.Itoa(imagesearch.CatalogMinQueryRunes) + "+ characters)"
	if skippedHint != want {
		t.Errorf("skippedHint = %q, want %q", skippedHint, want)
	}
}

// Property: an abbreviation always says how many names it hides, and it fits
// whenever a bare "+N" does.
func TestFitNamesNeverHidesACount(t *testing.T) {
	pool := []string{"hub", "prod", "dhi.io", "nexus-prod", "third", "日本語"}
	for _, suffix := range []string{"", skippedHint} {
		for n := 1; n <= len(pool); n++ {
			names := pool[:n]
			for width := 2; width <= 60; width++ {
				out := fitNames(names, suffix, width)
				if ansi.StringWidth(out) > width {
					t.Errorf("fitNames(%v, %q, %d) = %q is %d cells wide", names, suffix, width, out, ansi.StringWidth(out))
				}
				list := strings.TrimSuffix(out, suffix)
				parts := strings.Split(list, ", ")
				shown, hidden := len(parts), 0
				if last := parts[len(parts)-1]; strings.HasPrefix(last, "+") {
					c, err := strconv.Atoi(strings.TrimPrefix(last, "+"))
					if err != nil {
						t.Errorf("fitNames(%v, %q, %d) = %q: bad count %q", names, suffix, width, out, last)
						continue
					}
					shown, hidden = len(parts)-1, c
				}
				if shown+hidden != n {
					t.Errorf("fitNames(%v, %q, %d) = %q shows %d and hides %d, want %d in all",
						names, suffix, width, out, shown, hidden, n)
				}
			}
		}
	}
}

// ── summarizeSearch ──────────────────────────────────────────────────────────

func TestSummarizeSearch(t *testing.T) {
	ok := imagesearch.Outcome{Verdict: imagesearch.VerdictOK}
	okPartial := imagesearch.Outcome{Verdict: imagesearch.VerdictOK, Partial: true}
	none := imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult}
	nonePartial := imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult, Partial: true}
	skipped := imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult, Skipped: true}
	refused := imagesearch.Outcome{Verdict: imagesearch.VerdictRefused}
	failed := imagesearch.Outcome{Verdict: imagesearch.VerdictFailed}

	tests := []struct {
		name     string
		keys     []string
		outcomes map[string]imagesearch.Outcome
		live     bool
		cached   []string // keys the lot's cachedOnly predicate accepts
		want     searchSummary
	}{
		{"no keys", nil, nil, true, nil, searchSummary{}},
		{"ok without a mark appears nowhere", []string{"a"}, map[string]imagesearch.Outcome{"a": ok}, true, nil, searchSummary{}},
		{"no outcome in a live lot is pending", []string{"a", "b"}, nil, true, nil, searchSummary{Pending: 2}},
		{"no outcome in an abandoned lot is interrupted", []string{"a", "b"},
			map[string]imagesearch.Outcome{"a": ok}, false, nil, searchSummary{Interrupted: []string{"b"}}},
		{"failed", []string{"a"}, map[string]imagesearch.Outcome{"a": failed}, true, nil, searchSummary{Failed: []string{"a"}}},
		{"refused", []string{"a"}, map[string]imagesearch.Outcome{"a": refused}, true, nil, searchSummary{Refused: []string{"a"}}},
		{"ok partial", []string{"a"}, map[string]imagesearch.Outcome{"a": okPartial}, true, nil, searchSummary{Partial: []string{"a"}}},
		{"no result", []string{"a"}, map[string]imagesearch.Outcome{"a": none}, true, nil, searchSummary{NoResult: []string{"a"}}},
		{"no result and partial is both", []string{"a"}, map[string]imagesearch.Outcome{"a": nonePartial}, true, nil,
			searchSummary{NoResult: []string{"a"}, Partial: []string{"a"}}},
		{"skipped is only skipped", []string{"a"}, map[string]imagesearch.Outcome{"a": skipped}, true, nil,
			searchSummary{Skipped: []string{"a"}}},
		{"names follow the keys, not the map", []string{"z", "m", "a"},
			map[string]imagesearch.Outcome{"a": refused, "m": refused, "z": refused}, true, nil,
			searchSummary{Refused: []string{"z", "m", "a"}}},
		{"a mix", []string{"hub", "prod", "dev", "slow", "third"},
			map[string]imagesearch.Outcome{"hub": ok, "prod": refused, "dev": failed, "slow": okPartial}, true, nil,
			searchSummary{Pending: 1, Failed: []string{"dev"}, Refused: []string{"prod"}, Partial: []string{"slow"}}},
		// DEVDESK-308 (IS-14): a registry that only lists cached content gets the
		// Cached only line; a no-result there means "absent from the cache" and
		// leaves No result, which then means "absent from the registry".
		{"cached ok is listed", []string{"a"}, map[string]imagesearch.Outcome{"a": ok}, true, []string{"a"},
			searchSummary{CachedOnly: []string{"a"}}},
		{"cached no result is marked and leaves No result", []string{"a"}, map[string]imagesearch.Outcome{"a": none}, true, []string{"a"},
			searchSummary{CachedOnly: []string{"a" + cachedNoResultMark}}},
		{"cached ok partial is on both lines", []string{"a"}, map[string]imagesearch.Outcome{"a": okPartial}, true, []string{"a"},
			searchSummary{Partial: []string{"a"}, CachedOnly: []string{"a"}}},
		{"cached no result and partial is Partial and marked", []string{"a"}, map[string]imagesearch.Outcome{"a": nonePartial}, true, []string{"a"},
			searchSummary{Partial: []string{"a"}, CachedOnly: []string{"a" + cachedNoResultMark}}},
		{"cached failed keeps only Failed", []string{"a"}, map[string]imagesearch.Outcome{"a": failed}, true, []string{"a"},
			searchSummary{Failed: []string{"a"}}},
		{"cached refused keeps only Refused", []string{"a"}, map[string]imagesearch.Outcome{"a": refused}, true, []string{"a"},
			searchSummary{Refused: []string{"a"}}},
		{"cached skipped keeps only Skipped", []string{"a"}, map[string]imagesearch.Outcome{"a": skipped}, true, []string{"a"},
			searchSummary{Skipped: []string{"a"}}},
		{"cached pending stays pending", []string{"a"}, nil, true, []string{"a"}, searchSummary{Pending: 1}},
		{"cached interrupted stays interrupted", []string{"a"}, nil, false, []string{"a"}, searchSummary{Interrupted: []string{"a"}}},
		{"an unconcerned registry is unchanged", []string{"a", "b", "c"},
			map[string]imagesearch.Outcome{"a": ok, "b": none, "c": none}, true, []string{"a"},
			searchSummary{CachedOnly: []string{"a"}, NoResult: []string{"b", "c"}}},
		{"cached names follow the keys", []string{"z", "m", "a"},
			map[string]imagesearch.Outcome{"a": ok, "m": none, "z": ok}, true, []string{"a", "m", "z"},
			searchSummary{CachedOnly: []string{"z", "m" + cachedNoResultMark, "a"}}},
	}
	label := func(k string) string { return "L:" + k }
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerned := func(k string) bool { return slices.Contains(tt.cached, k) }
			got := summarizeSearch(tt.keys, tt.outcomes, tt.live, label, concerned)
			// Names carry the label, not the key.
			want := tt.want
			for _, p := range []*[]string{&want.Interrupted, &want.Failed, &want.Refused, &want.Partial, &want.CachedOnly, &want.NoResult, &want.Skipped} {
				for i, k := range *p {
					(*p)[i] = "L:" + k
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("summarizeSearch = %+v, want %+v", got, want)
			}
		})
	}
}

// ── the summary as the model derives it ──────────────────────────────────────

func TestThePendingCountFollowsTheAnswers(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, threeRegistryModel(t))
	if got := m.registryBrowser.searchSummary().Pending; got != 3 {
		t.Fatalf("Pending after submit = %d, want 3", got)
	}
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	if got := m.registryBrowser.searchSummary().Pending; got != 2 {
		t.Errorf("Pending after one answer = %d, want 2", got)
	}
	m = answerFor(t, m, "prod", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "third", nil, false, errors.New("boom"))
	s := m.registryBrowser.searchSummary()
	if s.Pending != 0 {
		t.Errorf("Pending after every answer = %d, want 0", s.Pending)
	}
	if !reflect.DeepEqual(s.Failed, []string{"third"}) {
		t.Errorf("Failed = %v, want [third]", s.Failed)
	}
}

// → cancels the lot (DEVDESK-304); back at level 1 the registries that had not
// answered are neither pending nor judged: they are interrupted.
func TestOpeningTagsMidSearchMarksTheRestInterrupted(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, threeRegistryModel(t))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	m = selectLevelOneRow(t, m, "hub", "library/nginx")
	m, _ = step(t, m, testutil.Key("right"))
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d after right, want the tags level", m.registryBrowser.state)
	}
	m, _ = step(t, m, testutil.Key("left"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d after left, want level 1", m.registryBrowser.state)
	}
	s := m.registryBrowser.searchSummary()
	if s.Pending != 0 {
		t.Errorf("Pending = %d, want 0: nothing runs any more", s.Pending)
	}
	if want := []string{"prod", "third"}; !reflect.DeepEqual(s.Interrupted, want) {
		t.Errorf("Interrupted = %v, want %v (form order)", s.Interrupted, want)
	}
}

func TestANewSearchStartsANewSummary(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, threeRegistryModel(t))
	m = answerFor(t, m, "prod", nil, false, imagesearch.ErrUnsupported)
	if got := m.registryBrowser.searchSummary().Refused; len(got) != 1 {
		t.Fatalf("Refused = %v, want one entry before going back", got)
	}
	m, _ = step(t, m, testutil.Key("left"))
	if m.registryBrowser.state != browserStateInput {
		t.Fatalf("state = %d after left, want the form", m.registryBrowser.state)
	}
	m, _ = step(t, m, testutil.Key("enter"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("the second search did not start (state %d)", m.registryBrowser.state)
	}
	s := m.registryBrowser.searchSummary()
	if len(s.Refused) != 0 {
		t.Errorf("Refused = %v after a new search, want none", s.Refused)
	}
	if want := len(checkedKeys(m)); s.Pending != want {
		t.Errorf("Pending = %d, want %d (every checked registry)", s.Pending, want)
	}
}

// V5: one label per registry, the Registry column's.
func TestTheRegistryColumnAndTheSummaryShareTheLabel(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	bare := anonymousRegistry("bare.example.com", "")
	bare.Slug = "bare"
	m := searchLot(t, registryModel(t, bare))
	m = answerFor(t, m, "bare", nil, false, errors.New("boom"))

	failed := m.registryBrowser.searchSummary().Failed
	if want := []string{"bare.example.com"}; !reflect.DeepEqual(failed, want) {
		t.Fatalf("Failed = %v, want %v (the URL, no alias)", failed, want)
	}
	if got := m.registryBrowser.entryLabel("bare"); got != failed[0] {
		t.Errorf("entryLabel = %q, summary says %q", got, failed[0])
	}

	// The same label sits in the Registry column of a row of that registry.
	m2 := answerFor(t, searchLot(t, registryModel(t, bare)), "bare", oneImage("nginx"), false, nil)
	rows := m2.registryBrowser.searchTable.Visible()
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if rows[0].label != m2.registryBrowser.entryLabel("bare") {
		t.Errorf("column label = %q, entryLabel = %q", rows[0].label, m2.registryBrowser.entryLabel("bare"))
	}
}
