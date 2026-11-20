package ociresources

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-579 (NX-12, DEVDESK-623), step 6, UX conditions on the label of a merged
// group unit ("<alias> (group)", groupUnitSuffix):
//
//  1. the `?` help says what the suffix means: those images were found through the
//     group, and Nexus does not say which member holds each one;
//  2. the suffix is never cut. A long alias ("nexus-production (group)") must never
//     render as "nexus-production…", which reads as the group that was not
//     discovered. When the Registry column is too narrow, the alias gives way
//     (in the middle or at the end), the suffix stays whole. The same goes for
//     the header lines that name a registry and for the `r` filter token.
//
// The tests read the screen (Render) and the header values, never a column width,
// so they hold whichever way the production code keeps the suffix.

const (
	longGroupAlias = "nexus-production"
	longGroupSlug  = "nxp"
	longGroupHost  = "nexus.example.com"
)

// aliasThenSuffix matches a Registry cell that keeps its suffix whole, behind an
// alias that may have been cut anywhere but in front of the suffix: "nexus-production (group)",
// "nexus-pro… (group)", "nexus…tion (group)". "nexus-production… (group)" matches
// too, which is fine: the suffix is there, the alias is plainly shortened.
var aliasThenSuffix = regexp.MustCompile(`nexus\S* ` + regexp.QuoteMeta(strings.TrimSpace(groupUnitSuffix)))

// narrowWidths are terminal widths from the narrowest the tables still draw their
// Registry column in (60: dot, Name, Registry, see
// TestSearchTableAt60ColumnsKeepsActivityNameRegistry) up to a wide one.
var narrowWidths = []int{60, 64, 70, 80, 90, 100, 120, 160, 200}

// everyWidth is every terminal width from 60 to 200: the column solver shares
// the room between Name, Registry and the optional columns in steps, so a cut
// shows up at some widths only (a few samples miss it).
var everyWidth = func() []int {
	var ws []int
	for w := 60; w <= 200; w++ {
		ws = append(ws, w)
	}
	return ws
}()

// longAliasGroup is a group with a long alias whose two discovered members have no
// address of their own (the group's URL, no prefix), so a search merges them into
// one unit labelled longGroupAlias + groupUnitSuffix, keyed by the group's slug.
func longAliasGroup(provider string) config.RegistryItem {
	g := nexusGroup()
	g.Slug, g.Alias, g.URL, g.Provider = longGroupSlug, longGroupAlias, longGroupHost, provider
	g.AuthMode = config.AuthAnonymous
	return g
}

func longAliasMembers() map[string]cache.RegistryGroupEntry {
	return map[string]cache.RegistryGroupEntry{longGroupSlug: {Members: []cache.RegistryGroupMember{
		{Alias: "docker-hosted", URL: longGroupHost},
		{Alias: "docker-proxy", URL: longGroupHost},
	}}}
}

// groupUnitLot opens the browser on the long-alias group (and on items besides),
// at the given width, and starts the search of the lot. No answer is in yet.
func groupUnitLot(t *testing.T, width int, provider string, others ...config.RegistryItem) Model {
	t.Helper()
	withImageSearcher(t, &fakeSearcher{})
	items := append([]config.RegistryItem{longAliasGroup(provider)}, others...)
	m := modelWithGroups(t, items, longAliasMembers())
	m = feed(t, m, testutil.Resize(width, 40))
	m = searchLot(t, m)
	if m.registryBrowser.entryFor(longGroupSlug) == nil {
		t.Fatal("the lot made no unit for the group: the test does not set up what it claims to")
	}
	return m
}

// lineWith returns the first line of the screen (viewport and footer, where the
// filter bar lives) containing needle.
func lineWith(t *testing.T, m Model, needle string) string {
	t.Helper()
	screen := ansi.Strip(m.Render() + "\n" + m.RenderFooter(m.width))
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line of the screen contains %q:\n%s", needle, screen)
	return ""
}

// wantSuffixKept fails when the line shows the alias without its whole suffix.
func wantSuffixKept(t *testing.T, where, line string) {
	t.Helper()
	if !aliasThenSuffix.MatchString(line) {
		t.Errorf("%s: the Registry cell lost the suffix %q:\n%s", where, groupUnitSuffix, line)
	}
}

// ---- (a) the help ----------------------------------------------------------

// The `?` help explains what "<alias> (group)" means, in the Registry Browser
// section, where the image search is described. Rule 114: the section follows the
// form's mode, and without a Searcher there is no search, so no group unit and
// nothing to explain.
func TestTheHelpExplainsTheGroupUnitLabel(t *testing.T) {
	suffix := strings.TrimSpace(groupUnitSuffix)
	wants := []string{suffix, "found through the group", "Nexus does not say which member holds each one"}

	t.Run("with a Searcher", func(t *testing.T) {
		body := registryBrowserHelp(t, lvl1ImagesModel(t))
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("the Registry Browser help lacks %q", want)
			}
		}
	})
	t.Run("without a Searcher", func(t *testing.T) {
		m := lvl1ImagesModel(t)
		m.registryBrowser.searcher = nil
		if body := registryBrowserHelp(t, m); strings.Contains(body, suffix) {
			t.Errorf("the help explains %q although there is no search to produce it", suffix)
		}
	})
}

// The new sentence is the only change to the help's shape: every key binding still
// has a key and a capitalized description, and no section is empty.
func TestTheHelpStaysWellFormedWithTheGroupUnitSentence(t *testing.T) {
	c := lvl1ImagesModel(t).GetHelpContent()
	for _, kb := range c.KeyBindings {
		if kb.Key == "" || kb.Description == "" {
			t.Errorf("key binding %+v has an empty key or description", kb)
			continue
		}
		if first := kb.Description[:1]; first != strings.ToUpper(first) {
			t.Errorf("description of %q does not start with a capital: %q", kb.Key, kb.Description)
		}
	}
	for _, s := range c.Sections {
		if s.Title == "" || strings.TrimSpace(s.Body) == "" {
			t.Errorf("section %q is empty", s.Title)
		}
	}
}

// ---- (b) the suffix is never cut -------------------------------------------

// Level 1, the Registry column (SizingContent, MinWidth 16): at every width the row
// of the unit keeps " (group)" whole, even when the alias plus the suffix (24 cells)
// exceeds the column.
func TestTheSearchTableNeverCutsTheGroupSuffix(t *testing.T) {
	for _, w := range everyWidth {
		t.Run(fmt.Sprintf("width %d", w), func(t *testing.T) {
			m := groupUnitLot(t, w, config.ProviderNexus)
			m = answerFor(t, m, longGroupSlug, oneImage("webapp"), false, nil)

			line := lineWith(t, m, "webapp")
			wantSuffixKept(t, fmt.Sprintf("level 1 at %d columns", w), line)
			if strings.Contains(line, longGroupAlias+"…") && !strings.Contains(line, longGroupAlias+"… "+strings.TrimSpace(groupUnitSuffix)) {
				t.Errorf("level 1 at %d columns reads as the undiscovered group %q:\n%s", w, longGroupAlias+"…", line)
			}
		})
	}
}

// Level 2, the Registry column of the tag table (MinWidth 20), reached with → on a
// row of the unit.
func TestTheTagTableNeverCutsTheGroupSuffix(t *testing.T) {
	for _, w := range everyWidth {
		t.Run(fmt.Sprintf("width %d", w), func(t *testing.T) {
			withTagRecorder(t)
			m := groupUnitLot(t, w, config.ProviderNexus)
			m = answerFor(t, m, longGroupSlug, oneImage("webapp"), false, nil)
			m = selectLevelOneRow(t, m, longGroupSlug, "library/webapp")
			m, _, loaded := pressRight(t, m)

			if m.registryBrowser.state != browserStateTags || len(loaded) == 0 {
				t.Fatalf("→ did not open the tags of the unit (state %d, %d answers)", m.registryBrowser.state, len(loaded))
			}
			rows := m.registryBrowser.tagTable.Visible()
			if len(rows) == 0 || rows[0].label != longGroupAlias+groupUnitSuffix {
				t.Fatalf("the tag rows are %+v, want the unit's label %q", rows, longGroupAlias+groupUnitSuffix)
			}
			wantSuffixKept(t, fmt.Sprintf("level 2 at %d columns", w), lineWith(t, m, "v1"))
		})
	}
}

// The header lines that name a registry: whatever the width, a name that appears is
// the unit's whole label. fitNames may replace the list by "+N" when nothing
// fits (C4), it must never print the alias without its suffix. At a width where
// the label fits, it is there in full.
func TestTheHeaderLinesNeverCutTheGroupSuffix(t *testing.T) {
	label := longGroupAlias + groupUnitSuffix
	tests := []struct {
		name     string
		provider string
		key      string
		answer   func(t *testing.T, m Model) Model
	}{
		{"Cached only", config.ProviderNexus, "Cached only", func(t *testing.T, m Model) Model {
			return answerFor(t, m, longGroupSlug, oneImage("webapp"), false, nil)
		}},
		{"Failed", config.ProviderNexus, "Failed", func(t *testing.T, m Model) Model {
			return answerFor(t, m, longGroupSlug, nil, false, errors.New("boom"))
		}},
		{"Refused", config.ProviderNexus, "Refused", func(t *testing.T, m Model) Model {
			return answerFor(t, m, longGroupSlug, nil, false, imagesearch.ErrUnsupported)
		}},
		// A group that is not Nexus is not cache-only: its empty answer is "No result".
		{"No result", config.ProviderGeneric, "No result", func(t *testing.T, m Model) Model {
			return answerFor(t, m, longGroupSlug, nil, false, nil)
		}},
	}
	for _, tt := range tests {
		for _, w := range narrowWidths {
			t.Run(fmt.Sprintf("%s at %d", tt.name, w), func(t *testing.T) {
				m := tt.answer(t, groupUnitLot(t, w, tt.provider))
				_, vals := headerLines(m)
				v, ok := vals[tt.key]
				if !ok {
					t.Fatalf("no %q line in the header (%v)", tt.key, vals)
				}
				if strings.Contains(v, "nexus") && !strings.Contains(v, label) {
					t.Errorf("%s = %q names the group without its suffix, want %q or a bare count", tt.key, v, label)
				}
				if w >= 200 && !strings.Contains(v, label) {
					t.Errorf("%s = %q at %d columns, want the whole label %q (it fits)", tt.key, v, w, label)
				}
			})
		}
	}
}

// ---- (c) the `r` filter on the unit ----------------------------------------

// lotWithUnitAndHub is the group unit next to a plain registry, so the lot has
// something to filter down to. Both have one image.
func lotWithUnitAndHub(t *testing.T, width int) Model {
	t.Helper()
	m := groupUnitLot(t, width, config.ProviderNexus, anonymousRegistry("docker.io", "hub"))
	m = answerFor(t, m, longGroupSlug, oneImage("webapp"), false, nil)
	return answerFor(t, m, "hub", oneImage("nginx"), false, nil)
}

// `r` stops, in order: the group (its own label, the unit's rows), the unit (label
// with its suffix: a space and parentheses in a label change nothing, the filter
// is a key and not a parsed token), the plain registry, then off. Each stop keeps
// exactly its rows, and the token the bar shows is the stop's whole label.
func TestTheRegistryFilterSelectsTheGroupUnitExactly(t *testing.T) {
	unit := longGroupAlias + groupUnitSuffix
	stops := []struct {
		label string
		rows  []string
	}{
		{longGroupAlias, []string{longGroupSlug + ":library/webapp"}},
		{unit, []string{longGroupSlug + ":library/webapp"}},
		{"hub", []string{"hub:library/nginx"}},
	}

	m := lotWithUnitAndHub(t, 200)
	b := m.registryBrowser
	if got := repoKeys(m); !slices.Equal(got, sorted([]string{longGroupSlug + ":library/webapp", "hub:library/nginx"})) {
		t.Fatalf("rows before the filter = %v", got)
	}
	for i, stop := range stops {
		m = feed(t, m, testutil.Key("r"))
		if got := repoKeys(m); !slices.Equal(got, stop.rows) {
			t.Errorf("stop %d (%s): rows = %v, want %v", i, stop.label, got, stop.rows)
		}
		if got := b.filterLabel(b.searchFilter); got != stop.label {
			t.Errorf("stop %d: label = %q, want %q", i, got, stop.label)
		}
		// The token in the bar: the label as is, not split at the space, not cut at
		// the parenthesis.
		if line := lineWith(t, m, "/ search"); !strings.Contains(line, stop.label) {
			t.Errorf("stop %d: the filter bar does not show %q:\n%s", i, stop.label, line)
		}
	}
	m = feed(t, m, testutil.Key("r"))
	if !b.searchFilter.isEmpty() || len(rowsOf(m)) != 2 {
		t.Errorf("after the last stop: filter = %+v, rows = %v, want off and 2 rows", b.searchFilter, rowsOf(m))
	}
}

// The filter token, on a terminal as narrow as the tables allow, keeps the suffix
// when it is the unit's stop.
func TestTheFilterTokenNeverCutsTheGroupSuffix(t *testing.T) {
	for _, w := range narrowWidths {
		t.Run(fmt.Sprintf("width %d", w), func(t *testing.T) {
			m := lotWithUnitAndHub(t, w)
			m = feed(t, m, testutil.Key("r"), testutil.Key("r")) // the group, then the unit
			b := m.registryBrowser
			if got := b.filterLabel(b.searchFilter); got != longGroupAlias+groupUnitSuffix {
				t.Fatalf("second stop = %q, want the unit's label", got)
			}
			wantSuffixKept(t, fmt.Sprintf("filter bar at %d columns", w), lineWith(t, m, "/ search"))
		})
	}
}
