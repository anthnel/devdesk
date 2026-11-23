package security

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// DEVDESK-754 (SC1, step 2): the pure half of "Group by fix" on the CVE tab.

// cve builds a vulnerability finding.
func cve(id, pkg, eco, installed, fixed string, sev scan.SeverityLevel) scan.Finding {
	class := scan.ClassLangPackages
	if eco == "alpine" || eco == "debian" {
		class = scan.ClassOSPackages
	}
	return scan.Finding{
		ID: id, Title: id + " title", Source: scan.SourceTrivy, Severity: sev,
		PkgName: pkg, Ecosystem: eco, Class: class, Version: installed, FixedIn: fixed,
	}
}

func groupBySubject(t *testing.T, groups []fixGroup, subject string) fixGroup {
	t.Helper()
	for _, g := range groups {
		if g.Subject == subject {
			return g
		}
	}
	t.Fatalf("no group for %q in %+v", subject, groups)
	return fixGroup{}
}

func ids(fs []scan.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

func TestCVEGroups(t *testing.T) {
	const (
		crit = scan.SeverityCritical
		high = scan.SeverityHigh
		low  = scan.SeverityLow
	)
	cases := []struct {
		name     string
		findings []scan.Finding
		subject  string
		check    func(t *testing.T, g fixGroup)
	}{
		{
			name: "three CVEs of one package are one row with the delta and the count",
			findings: []scan.Finding{
				cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", high),
				cve("CVE-2", "openssl", "alpine", "1.1.1k", "1.1.1w", crit),
				cve("CVE-3", "openssl", "alpine", "1.1.1k", "1.1.1w", low),
			},
			subject: "openssl",
			check: func(t *testing.T, g fixGroup) {
				if g.Change != "1.1.1k -> 1.1.1w" {
					t.Errorf("Change = %q", g.Change)
				}
				if len(g.Members) != 3 {
					t.Errorf("Members = %v, want the 3 CVEs", ids(g.Members))
				}
				if g.Severity != crit {
					t.Errorf("Severity = %q, want the worst", g.Severity)
				}
				if g.Status.State != fixManual || g.Status.Label() != "Manual" {
					t.Errorf("Status = %+v (%q), want Manual", g.Status, g.Status.Label())
				}
				if g.Command != "apk upgrade openssl" {
					t.Errorf("Command = %q", g.Command)
				}
			},
		},
		{
			name:     "no fix published reads none and says so",
			findings: []scan.Finding{cve("CVE-1", "zlib", "alpine", "1.2", "", high), cve("CVE-2", "zlib", "alpine", "1.2", "", high)},
			subject:  "zlib",
			check: func(t *testing.T, g fixGroup) {
				if g.Change != "1.2 -> none" {
					t.Errorf("Change = %q", g.Change)
				}
				if g.Status.State != fixNoFixPublished || g.Status.Label() != "No fix published" {
					t.Errorf("Status = %+v (%q), want No fix published", g.Status, g.Status.Label())
				}
				if g.Command != "" {
					t.Errorf("Command = %q, want none", g.Command)
				}
				if !g.Key.Unfixed {
					t.Error("Key.Unfixed is false")
				}
			},
		},
		{
			name:     "an ambiguous fixed list lists the options and picks none",
			findings: []scan.Finding{cve("CVE-1", "libfoo", "npm", "1.0.0", "abc, def", high)},
			subject:  "libfoo",
			check: func(t *testing.T, g fixGroup) {
				if g.Change != "1.0.0 -> abc or def" {
					t.Errorf("Change = %q", g.Change)
				}
				if g.Command != "" {
					t.Errorf("Command = %q, want none: no version was chosen", g.Command)
				}
			},
		},
		{
			name: "versions that cannot be ordered are listed with or",
			findings: []scan.Finding{
				cve("CVE-1", "libfoo", "npm", "1.0.0", "1.0.1", high),
				cve("CVE-2", "libfoo", "npm", "1.0.0", "weird", high),
			},
			subject: "libfoo",
			check: func(t *testing.T, g fixGroup) {
				if g.Change != "1.0.0 -> 1.0.1 or weird" {
					t.Errorf("Change = %q", g.Change)
				}
			},
		},
		{
			name:     "a command that is not plain words is dropped, never shown",
			findings: []scan.Finding{cve("CVE-1", "~evil", "npm", "1.0.0", "1.0.1", high)},
			subject:  "~evil",
			check: func(t *testing.T, g fixGroup) {
				if g.Command != "" {
					t.Errorf("Command = %q: scan.PlainWords refuses a word that starts with ~", g.Command)
				}
				if g.Change != "1.0.0 -> 1.0.1" {
					t.Errorf("Change = %q: the delta does not depend on the command", g.Change)
				}
			},
		},
		{
			name:     "a Go module gets its go get command",
			findings: []scan.Finding{cve("CVE-1", "golang.org/x/net", "gomod", "0.10.0", "0.23.0", high)},
			subject:  "golang.org/x/net",
			check: func(t *testing.T, g fixGroup) {
				if g.Command != "go get golang.org/x/net@v0.23.0" {
					t.Errorf("Command = %q", g.Command)
				}
			},
		},
		{
			name: "a vulnerability with no package is its own group, named by its ID",
			findings: []scan.Finding{
				{ID: "CVE-9", Source: scan.SourceTrivy, Severity: high, Title: "t"},
				{ID: "CVE-8", Source: scan.SourceTrivy, Severity: high, Title: "t"},
			},
			subject: "CVE-9",
			check: func(t *testing.T, g fixGroup) {
				if len(g.Members) != 1 || g.Members[0].ID != "CVE-9" {
					t.Errorf("Members = %v, want just CVE-9", ids(g.Members))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			groups := cveGroups(tc.findings)
			tc.check(t, groupBySubject(t, groups, tc.subject))
		})
	}
}

func TestCVEGroupsIgnoreWhatIsNotAVulnerability(t *testing.T) {
	groups := cveGroups([]scan.Finding{
		{ID: "S1", Source: scan.SourceGitleaks, Severity: scan.SeverityHigh},
		{ID: "L1", Source: scan.SourceTrivyLicense, PkgName: "x", FixedIn: "1"},
		{ID: "M1", Source: scan.SourceTrivyMisconfig},
		cve("CVE-1", "libfoo", "npm", "1.0.0", "1.0.1", scan.SeverityHigh),
	})
	if len(groups) != 1 || groups[0].Subject != "libfoo" {
		t.Errorf("groups = %+v, want only libfoo", groups)
	}
	if got := cveGroups(nil); len(got) != 0 {
		t.Errorf("cveGroups(nil) = %+v", got)
	}
}

func TestCVEGroupsAreOrderedBySeverityThenCountThenSubjectThenChange(t *testing.T) {
	const (
		crit = scan.SeverityCritical
		high = scan.SeverityHigh
		low  = scan.SeverityLow
	)
	groups := cveGroups([]scan.Finding{
		cve("CVE-1", "zeta", "npm", "1.0.0", "1.0.1", high),
		cve("CVE-2", "alpha", "npm", "1.0.0", "1.0.1", high),
		cve("CVE-3", "mid", "npm", "1.0.0", "1.0.1", high),
		cve("CVE-4", "mid", "npm", "1.0.0", "1.0.1", high),
		cve("CVE-5", "low-one", "npm", "1.0.0", "1.0.1", low),
		cve("CVE-6", "crit-one", "npm", "1.0.0", "1.0.1", crit),
		cve("CVE-7", "tie", "npm", "2.0.0", "2.0.1", low),
		cve("CVE-8", "tie", "npm", "1.0.0", "1.0.1", low),
	})
	var got []string
	for _, g := range groups {
		got = append(got, g.Subject+" "+g.Change)
	}
	want := []string{
		"crit-one 1.0.0 -> 1.0.1",
		"mid 1.0.0 -> 1.0.1",
		"alpha 1.0.0 -> 1.0.1",
		"zeta 1.0.0 -> 1.0.1",
		"low-one 1.0.0 -> 1.0.1",
		"tie 1.0.0 -> 1.0.1",
		"tie 2.0.0 -> 2.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order =\n  %q\nwant\n  %q", got, want)
	}
}

// The key is what a re-scan finds a row again by: two installed versions of one
// package must not collide, and the key must not move when the target does.
func TestCVEGroupKeys(t *testing.T) {
	a := cve("CVE-1", "semver", "npm", "5.0.0", "5.7.2", scan.SeverityHigh)
	b := cve("CVE-1", "semver", "npm", "6.0.0", "6.3.1", scan.SeverityHigh)
	groups := cveGroups([]scan.Finding{a, b})
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want one per installed version", len(groups))
	}
	if groups[0].Key == groups[1].Key {
		t.Fatalf("two installed versions share the key %+v", groups[0].Key)
	}
	again := cveGroups([]scan.Finding{a, cve("CVE-2", "semver", "npm", "5.0.0", "5.9.0", scan.SeverityLow)})
	if groupBySubject(t, again, "semver").Key != groupBySubjectAndInstalled(t, groups, "5.0.0").Key {
		t.Error("the key of semver 5.0.0 changed when a CVE joined and the target moved")
	}
}

func groupBySubjectAndInstalled(t *testing.T, groups []fixGroup, installed string) fixGroup {
	t.Helper()
	for _, g := range groups {
		if strings.HasPrefix(g.Change, installed+" ") {
			return g
		}
	}
	t.Fatalf("no group installed at %q in %+v", installed, groups)
	return fixGroup{}
}

func TestVisibleGroups(t *testing.T) {
	high := scan.SeverityHigh
	a1 := cve("CVE-1", "liba", "npm", "1.0.0", "1.0.1", high)
	a2 := cve("CVE-2", "liba", "npm", "1.0.0", "1.0.2", high)
	b1 := cve("CVE-3", "libb", "npm", "1.0.0", "1.0.1", high)
	c1 := cve("CVE-4", "libc", "npm", "1.0.0", "", high)
	all := cveGroups([]scan.Finding{a1, a2, b1, c1})

	subjects := func(gs []fixGroup) []string {
		out := []string{}
		for _, g := range gs {
			out = append(out, g.Subject)
		}
		return out
	}
	cases := []struct {
		name    string
		all     []fixGroup
		visible []scan.Finding
		want    []string
	}{
		{"everything visible keeps every group", all, []scan.Finding{a1, a2, b1, c1}, []string{"liba", "libb", "libc"}},
		{"one visible member keeps its group", all, []scan.Finding{a2}, []string{"liba"}},
		{"a filter that hides every member of a group drops it", all, []scan.Finding{b1, c1}, []string{"libb", "libc"}},
		{"a filter that hides everything leaves nothing", all, nil, []string{}},
		{"no groups, nothing to show", nil, []scan.Finding{a1}, []string{}},
		{"a visible finding that belongs to no group changes nothing", all, []scan.Finding{cve("CVE-77", "other", "npm", "1.0.0", "1.0.1", high)}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := visibleGroups(tc.all, tc.visible)
			if !reflect.DeepEqual(subjects(got), tc.want) {
				t.Errorf("groups = %v, want %v", subjects(got), tc.want)
			}
		})
	}

	t.Run("a kept group keeps its hidden members: the count is what one move closes", func(t *testing.T) {
		got := visibleGroups(all, []scan.Finding{a1})
		if len(got) != 1 || len(got[0].Members) != 2 {
			t.Errorf("groups = %+v, want liba with both members", got)
		}
	})
	t.Run("the order of the input is kept", func(t *testing.T) {
		got := visibleGroups(all, []scan.Finding{c1, a1})
		want := []string{}
		for _, g := range all {
			if g.Subject == "liba" || g.Subject == "libc" {
				want = append(want, g.Subject)
			}
		}
		if !reflect.DeepEqual(subjects(got), want) {
			t.Errorf("groups = %v, want %v", subjects(got), want)
		}
	})
	t.Run("the input is not modified", func(t *testing.T) {
		before := len(all)
		_ = visibleGroups(all, nil)
		if len(all) != before {
			t.Errorf("len(all) went from %d to %d", before, len(all))
		}
	})
}

func TestGroupIndexOf(t *testing.T) {
	high := scan.SeverityHigh
	v5 := cve("CVE-1", "semver", "npm", "5.0.0", "5.7.2", high)
	v6 := cve("CVE-1", "semver", "npm", "6.0.0", "6.3.1", high)
	b := cve("CVE-2", "libb", "npm", "1.0.0", "1.0.1", high)
	groups := cveGroups([]scan.Finding{v5, v6, b})

	indexOfSubject := func(subject, installed string) int {
		for i, g := range groups {
			if g.Subject == subject && strings.HasPrefix(g.Change, installed+" ") {
				return i
			}
		}
		t.Fatalf("no group %s %s", subject, installed)
		return -1
	}
	cases := []struct {
		name   string
		groups []fixGroup
		f      scan.Finding
		want   int
	}{
		{"a member is found in its group", groups, b, indexOfSubject("libb", "1.0.0")},
		{"the same CVE in two installed versions lands in the right group", groups, v6, indexOfSubject("semver", "6.0.0")},
		{"and in the other one", groups, v5, indexOfSubject("semver", "5.0.0")},
		{"a finding of no group is -1", groups, cve("CVE-99", "nope", "npm", "1.0.0", "1.0.1", high), -1},
		{"no groups is -1", nil, b, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupIndexOf(tc.groups, tc.f); got != tc.want {
				t.Errorf("groupIndexOf = %d, want %d", got, tc.want)
			}
		})
	}

	t.Run("the line does not matter: a fix moves it", func(t *testing.T) {
		moved := b
		moved.Line = 42
		if got := groupIndexOf(groups, moved); got != indexOfSubject("libb", "1.0.0") {
			t.Errorf("groupIndexOf = %d", got)
		}
	})
}

func TestGroupIndexByKey(t *testing.T) {
	high := scan.SeverityHigh
	groups := cveGroups([]scan.Finding{
		cve("CVE-1", "aaa", "npm", "1.0.0", "1.0.1", scan.SeverityCritical),
		cve("CVE-2", "bbb", "npm", "1.0.0", "1.0.1", high),
		cve("CVE-3", "ccc", "npm", "1.0.0", "1.0.1", scan.SeverityLow),
	})
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	absent := cveGroups([]scan.Finding{cve("CVE-9", "zzz", "npm", "1.0.0", "1.0.1", high)})
	if len(absent) != 1 {
		t.Fatalf("got %d groups, want 1", len(absent))
	}
	gone := absent[0].Key

	cases := []struct {
		name   string
		groups []fixGroup
		key    groupKey
		prev   int
		want   int
	}{
		{"the key wins over the previous index", groups, groups[2].Key, 0, 2},
		{"the key is found where the list moved", groups[1:], groups[2].Key, 2, 1},
		{"a group gone after a re-scan keeps the previous index", groups, gone, 1, 1},
		{"a previous index past the end is clamped", groups, gone, 7, 2},
		{"a negative previous index goes to the top", groups, gone, -3, 0},
		{"an empty list is -1", nil, gone, 0, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupIndexByKey(tc.groups, tc.key, tc.prev); got != tc.want {
				t.Errorf("groupIndexByKey = %d, want %d", got, tc.want)
			}
		})
	}

	t.Run("two installed versions of one package are told apart", func(t *testing.T) {
		both := cveGroups([]scan.Finding{
			cve("CVE-1", "semver", "npm", "5.0.0", "5.7.2", high),
			cve("CVE-1", "semver", "npm", "6.0.0", "6.3.1", high),
		})
		for i := range both {
			if got := groupIndexByKey(both, both[i].Key, 1-i); got != i {
				t.Errorf("groupIndexByKey(both[%d].Key) = %d", i, got)
			}
		}
	})
}

func TestNearestFinding(t *testing.T) {
	high := scan.SeverityHigh
	f := func(id string) scan.Finding { return cve(id, "lib"+id, "npm", "1.0.0", "1.0.1", high) }
	f1, f2, f3, f4, f5 := f("CVE-1"), f("CVE-2"), f("CVE-3"), f("CVE-4"), f("CVE-5")
	visible := []scan.Finding{f1, f2, f3, f4}
	group := func(members ...scan.Finding) *fixGroup { return &fixGroup{Members: members} }

	cases := []struct {
		name       string
		visible    []scan.Finding
		g          *fixGroup
		prev       *scan.Finding
		prevCursor int
		want       int
	}{
		{"the first visible member of the group, in the order of the table", visible, group(f4, f3), nil, 0, 2},
		{"a hidden member is skipped", visible, group(f5, f2), nil, 0, 1},
		{"the group wins over the previous selection", visible, group(f3), &f1, 0, 2},
		{"a group with no visible member falls back to the previous selection", visible, group(f5), &f4, 0, 3},
		{"no group: the previous selection if it is still visible", visible, nil, &f2, 3, 1},
		{"the previous selection is gone: the cursor, kept", visible, nil, &f5, 2, 2},
		{"the cursor is clamped past the end", visible, nil, &f5, 9, 3},
		{"a negative cursor goes to the top", visible, nil, nil, -1, 0},
		{"nothing visible is -1, whatever the group says", nil, group(f1), &f1, 0, -1},
		{"an empty group is not a match", visible, group(), nil, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nearestFinding(tc.visible, tc.g, tc.prev, tc.prevCursor); got != tc.want {
				t.Errorf("nearestFinding = %d, want %d", got, tc.want)
			}
		})
	}
}

// ---- columns (Rule 116, Rule 122, AC6) ----

func columnTitles(cols []datatable.Column[fixGroup]) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Title)
	}
	return out
}

func sampleGroups() []fixGroup {
	return cveGroups([]scan.Finding{
		cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityCritical),
		cve("CVE-2", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-3", "golang.org/x/net/http2/h2c/a-rather-long-module-path", "gomod", "0.10.0", "0.23.0", scan.SeverityHigh),
		cve("CVE-4", "zlib", "alpine", "1.2", "", scan.SeverityLow),
		cve("CVE-5", "libfoo", "npm", "1.0.0", "abc, def", scan.SeverityMedium),
	})
}

func TestCVEGroupColumnsAreDeclaredLikeThePlanSays(t *testing.T) {
	cols := cveGroupColumns()
	want := []string{"Severity", "Package", "Change", "CVEs", "Fix"}
	if got := columnTitles(cols); !reflect.DeepEqual(got, want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for _, c := range cols {
		if c.Sizing == datatable.SizingUnset {
			t.Errorf("%s declares no Sizing", c.Title)
		}
		if c.Cell == nil {
			t.Errorf("%s has no Cell", c.Title)
		}
	}
	sev, pkg, change, count, fix := cols[0], cols[1], cols[2], cols[3], cols[4]
	if sev.Sizing != datatable.SizingFixed || sev.MinWidth != 10 {
		t.Errorf("Severity = %v/%d, want Fixed 10", sev.Sizing, sev.MinWidth)
	}
	if pkg.Sizing != datatable.SizingContent || pkg.MinWidth != 14 {
		t.Errorf("Package = %v/%d, want Content min 14", pkg.Sizing, pkg.MinWidth)
	}
	if change.Sizing != datatable.SizingContent || change.MinWidth != 16 || change.Flex < 1 {
		t.Errorf("Change = %v/%d flex %d, want Content min 16 Flex 1", change.Sizing, change.MinWidth, change.Flex)
	}
	if count.Sizing != datatable.SizingFixed || count.MinWidth != 5 {
		t.Errorf("CVEs = %v/%d, want Fixed 5: a count is never truncated", count.Sizing, count.MinWidth)
	}
	if fix.Sizing != datatable.SizingContent || fix.MinWidth != 16 || fix.MaxWidth != 24 || !fix.Optional {
		t.Errorf("Fix = %v/%d..%d optional=%v, want Content 16..24 Optional", fix.Sizing, fix.MinWidth, fix.MaxWidth, fix.Optional)
	}
}

func TestCVEGroupCellsArePlainTextAndOnlySeverityIsColoured(t *testing.T) {
	cols := cveGroupColumns()
	if len(cols) != 5 {
		t.Fatalf("columns = %v", columnTitles(cols))
	}
	g := groupBySubject(t, sampleGroups(), "openssl")

	want := []string{"CRITICAL", "openssl", "1.1.1k -> 1.1.1w", "2", "Manual"}
	for i, c := range cols {
		cell := c.Cell(g)
		if cell != ansi.Strip(cell) {
			t.Errorf("%s cell %q carries an escape sequence (Rule 122)", c.Title, cell)
		}
		if cell != want[i] {
			t.Errorf("%s cell = %q, want %q", c.Title, cell, want[i])
		}
		if i == 0 {
			continue
		}
		if c.Style != nil {
			t.Errorf("%s declares a Style: only Severity is coloured", c.Title)
		}
	}
	if cols[0].Style == nil {
		t.Fatal("Severity declares no Style")
	}
	if got := cols[0].Style(g).Render(cols[0].Cell(g)); got == cols[0].Cell(g) || ansi.Strip(got) != "CRITICAL" {
		t.Errorf("Severity renders %q: want the word, in colour", got)
	}
}

func TestCVEGroupFixCellSpeaksTheClosedVocabulary(t *testing.T) {
	cols := cveGroupColumns()
	if len(cols) != 5 {
		t.Fatalf("columns = %v", columnTitles(cols))
	}
	groups := sampleGroups()
	if got := cols[4].Cell(groupBySubject(t, groups, "zlib")); got != "No fix published" {
		t.Errorf("unfixed Fix cell = %q", got)
	}
	if got := cols[4].Cell(groupBySubject(t, groups, "openssl")); got != "Manual" {
		t.Errorf("fixable Fix cell = %q", got)
	}
}

func TestCVEGroupTableFillsTheViewportAndFitsAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := datatable.New(datatable.Config[fixGroup]{Columns: cveGroupColumns(), SortColumn: -1})
			m.SetItems(sampleGroups())
			m.Resize(width, 20)

			if got, want := m.RenderedWidth(), width-2; got != want {
				t.Errorf("RenderedWidth = %d, want %d (Rule 116)", got, want)
			}
			view := m.View()
			for i, line := range strings.Split(view, "\n") {
				if w := ansi.StringWidth(line); w > width-2 {
					t.Errorf("line %d is %d cells wide, the viewport holds %d: %q", i, w, width-2, ansi.Strip(line))
				}
			}
			header, _, _ := strings.Cut(ansi.Strip(view), "\n")
			for _, title := range []string{"Severity", "Package", "Change", "CVEs", "Fix"} {
				if !strings.Contains(header, title) {
					t.Errorf("at %d columns the header %q has lost %q", width, strings.TrimSpace(header), title)
				}
			}
		})
	}
}

func TestCVEGroupTableShowsEveryCellOfASampleRowAt80(t *testing.T) {
	m := datatable.New(datatable.Config[fixGroup]{Columns: cveGroupColumns(), SortColumn: -1})
	m.SetItems(sampleGroups())
	m.Resize(80, 20)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"CRITICAL", "openssl", "1.1.1k -> 1.1.1w", "Manual", "1.2 -> none", "No fix published"} {
		if !strings.Contains(plain, want) {
			t.Errorf("at 80 columns the table does not show %q:\n%s", want, plain)
		}
	}
}
