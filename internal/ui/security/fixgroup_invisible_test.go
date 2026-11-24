package security

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-766: a name that holds a character with no visible form (a bidi
// override, a zero-width mark) is shown without it, so two names that differ only
// by that character looked identical. Each removed character is now shown as
// "?"; a name that needed no cleaning is untouched. The mark is display only: the
// key of a group keeps the raw name.

func TestVisibleText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"ordinary name", "Dockerfile", "Dockerfile"},
		{"ordinary path", "svc/app/Dockerfile", "svc/app/Dockerfile"},
		{"accents and CJK", "déploiement/配置.yaml", "déploiement/配置.yaml"},
		{"zero-width space", "Docker\u200bfile", "Docker?file"},
		{"right-to-left override", "Docker\u202efile", "Docker?file"},
		{"two characters, two marks", "a\u200b\u202eb", "a??b"},
		{"zero-width non-joiner stays", "a\u200cb", "a\u200cb"},
		{"zero-width joiner stays", "a\u200db", "a\u200db"},
		{"line separator", "a\u2028b", "a?b"},
		{"control character", "a\x07b", "a?b"},
		{"carriage return", "a\rb", "a?b"},
		{"escape sequence is one mark", "a\x1b[31mb", "a?b"},
		{"OSC sequence is one mark", "a\x1b]0;pwned\x07b", "a?b"},
		{"tab is not a removed character", "a\tb", "a    b"},
		{"line feed is not a removed character", "a\nb", "a b"},
		{"edges are trimmed, not marked", "  name  ", "name"},
		{"mark at the edge survives the trim", "\u200bname", "?name"},
		{"only an invisible character", "\u200b", "?"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := visibleText(tc.in); got != tc.want {
				t.Errorf("visibleText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The mark adds to the cleaning, it never changes it: without its "?" a visible
// text is what inlineText gives.
func TestVisibleTextOnlyAddsTheMark(t *testing.T) {
	for _, in := range []string{
		"Dockerfile", " a b ", "a\tb", "a\nb\nc", "\u200b", "a\u202eb", "x\x1b[0my", hostileText,
		"zl" + hostileText + "ib", "e\u0301", "a\u200db",
	} {
		if got, base := visibleText(in), inlineText(in); strings.ReplaceAll(got, "?", "") != strings.ReplaceAll(base, "?", "") {
			t.Errorf("visibleText(%q) = %q, inlineText = %q: they differ by more than the marks", in, got, base)
		}
	}
}

func subjectsOf(groups []fixGroup) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Subject
	}
	return out
}

func mgOne(id, file string) scan.Finding {
	return mgFinding(id, file, "msg "+file, scan.SeverityHigh)
}

func TestMisconfigGroupsMarkInvisibleCharactersInFileNames(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{"right-to-left override", "Docker\u202efile", "Docker?file"},
		{"zero-width space", "Docker\u200bfile", "Docker?file"},
		{"in a directory", "svc/Docker\u200bfile", "svc/Docker?file"},
		{"escape sequence", "Docker\x1b[31mfile", "Docker?file"},
	}
	for _, tc := range tests {
		t.Run(tc.name+" with no neighbor", func(t *testing.T) {
			groups := misconfigGroups([]scan.Finding{mgOne("DS002", tc.file)}, true)
			if got := subjectsOf(groups); !slices.Equal(got, []string{tc.want}) {
				t.Errorf("subjects = %q, want %q", got, []string{tc.want})
			}
		})
	}
}

func TestMisconfigGroupsNeverShowTwoFilesAlike(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{"zero-width space", []string{"Dockerfile", "Docker\u200bfile"}},
		{"right-to-left override", []string{"Dockerfile", "Docker\u202efile"}},
		{"both", []string{"Dockerfile", "Docker\u200bfile", "Docker\u202efile"}},
		{"a directory and a mark", []string{"svc/Dockerfile", "svc/\u200bDockerfile", "svc/Dockerfile\u202e"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var findings []scan.Finding
			for _, file := range tc.files {
				findings = append(findings, mgOne("DS002", file))
			}
			groups := misconfigGroups(findings, true)
			if len(groups) != len(tc.files) {
				t.Fatalf("got %d groups, want %d", len(groups), len(tc.files))
			}
			seen := map[string]bool{}
			for _, g := range groups {
				if seen[g.Subject] {
					t.Errorf("two groups show %q: %q", g.Subject, subjectsOf(groups))
				}
				seen[g.Subject] = true
				for _, c := range misconfigGroupColumns() {
					if cell := c.Cell(g); ansi.Strip(cell) != cell {
						t.Errorf("cell %s = %q holds an escape sequence (Rule 122)", c.Title, cell)
					}
				}
			}
		})
	}
}

// A mark is not a suffix: the plain name keeps its look, the other one says "?".
func TestMisconfigGroupsKeepTheOrdinaryNameAsItWas(t *testing.T) {
	groups := misconfigGroups([]scan.Finding{mgOne("DS002", "Dockerfile"), mgOne("DS002", "Docker\u200bfile")}, true)
	want := []string{"Docker?file", "Dockerfile"}
	got := subjectsOf(groups)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("subjects = %q, want %q", got, want)
	}
}

func TestMisconfigGroupsOrdinaryNamesAreUnchanged(t *testing.T) {
	for _, onDisk := range []bool{true, false} {
		groups := misconfigGroups(mgFindings(), onDisk)
		for _, g := range groups {
			if g.Subject != g.Key.File && g.Key.File != "" {
				t.Errorf("onDisk=%v: Subject %q differs from the plain name %q", onDisk, g.Subject, g.Key.File)
			}
		}
	}
	groups := misconfigGroups(mgFindings(), true)
	want := []string{"app.yaml", "Dockerfile", "svc/Dockerfile", "deploy.yaml", "pod.yaml", "pod.yaml"}
	if got := subjectsOf(groups); !slices.Equal(got, want) {
		t.Errorf("subjects = %q, want %q", got, want)
	}
}

// Two files folded to one name by shortPaths (the container mount) have the same
// Subject with no invisible character at all; they are told apart by a suffix, and
// only then.
func TestMisconfigGroupsNumberTwoFilesThatFoldToOneName(t *testing.T) {
	findings := []scan.Finding{mgOne("DS002", "Dockerfile"), mgOne("DS002", "/scan/Dockerfile")}
	groups := misconfigGroups(findings, true)
	got := subjectsOf(groups)
	slices.Sort(got)
	if want := []string{"Dockerfile", "Dockerfile (2)"}; !slices.Equal(got, want) {
		t.Fatalf("subjects = %q, want %q", got, want)
	}
	// The suffix follows the raw name, not the order of the scan.
	reversed := misconfigGroups([]scan.Finding{findings[1], findings[0]}, true)
	for _, g := range groups {
		other, ok := groupByKey(reversed, g.Key)
		if !ok || other.Subject != g.Subject {
			t.Errorf("key %+v: subject %q, then %q after reversing the scan", g.Key, g.Subject, other.Subject)
		}
	}
}

// Invisible characters that fold to the same mark still make two groups: the
// second is numbered.
func TestMisconfigGroupsNumberFilesWhoseMarksAreAlike(t *testing.T) {
	groups := misconfigGroups([]scan.Finding{mgOne("DS002", "a\u200bb"), mgOne("DS002", "a\u202eb")}, true)
	got := subjectsOf(groups)
	slices.Sort(got)
	if want := []string{"a?b", "a?b (2)"}; !slices.Equal(got, want) {
		t.Errorf("subjects = %q, want %q", got, want)
	}
}

// Several rules in one file share its name: they are one file, not a duplicate.
func TestMisconfigGroupsDoNotNumberOneFileTwice(t *testing.T) {
	groups := misconfigGroups([]scan.Finding{mgOne("DS002", "Dockerfile"), mgOne("DS026", "Dockerfile"), mgOne("DS002", "/scan/Dockerfile")}, true)
	got := subjectsOf(groups)
	slices.Sort(got)
	if want := []string{"Dockerfile", "Dockerfile", "Dockerfile (2)"}; !slices.Equal(got, want) {
		t.Errorf("subjects = %q, want %q", got, want)
	}
	// The file that needed no folding keeps the plain name.
	for _, g := range groups {
		if g.Key.File == "/scan/Dockerfile" && g.Subject != "Dockerfile (2)" {
			t.Errorf("the folded file shows %q, want Dockerfile (2)", g.Subject)
		}
	}
	// Same file, two rules: the two rows do differ, by the Rule column.
	for i, a := range groups {
		for _, b := range groups[i+1:] {
			if a.Subject == b.Subject && a.Rule == b.Rule {
				t.Errorf("two groups look alike: %q %q", a.Subject, a.Rule)
			}
		}
	}
}

func TestMisconfigGroupsMarkTheRuleToo(t *testing.T) {
	groups := misconfigGroups([]scan.Finding{mgOne("DS002", "Dockerfile"), mgOne("DS002\u200b", "Dockerfile")}, true)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	if groups[0].Rule == groups[1].Rule {
		t.Errorf("both groups show the rule %q", groups[0].Rule)
	}
}

// The mark is not in the key: a group is found again, and opened, by the raw name.
func TestMisconfigGroupKeysKeepTheRawName(t *testing.T) {
	const raw = "Docker\u202efile"
	findings := []scan.Finding{mgOne("DS002", raw), mgOne("DS002", "Dockerfile")}
	groups := misconfigGroups(findings, true)
	g, ok := groupByKey(groups, groupKey{File: raw, Rule: "DS002", Fix: groups[0].Key.Fix})
	if !ok {
		// Fix state is not the point here: look the group up by File alone.
		for _, c := range groups {
			if c.Key.File == raw {
				g, ok = c, true
			}
		}
	}
	if !ok {
		t.Fatalf("no group keyed by the raw name %q in %+v", raw, groups)
	}
	if g.Subject != "Docker?file" {
		t.Errorf("Subject = %q, want %q", g.Subject, "Docker?file")
	}
	if len(g.Members) != 1 || g.Members[0].File != raw {
		t.Errorf("members = %+v, want the finding of the raw file", g.Members)
	}
}

// The order reads the key, not the scan: a reversed scan lists the same rows, with
// the same text.
func TestMisconfigGroupsStayDeterministicWithMarks(t *testing.T) {
	files := []string{"Dockerfile", "Docker\u200bfile", "Docker\u202efile", "/scan/Dockerfile", "a\u200bb", "a\u202eb", "app.yaml"}
	var findings []scan.Finding
	for _, f := range files {
		findings = append(findings, mgOne("DS002", f))
	}
	want := misconfigGroups(findings, true)
	slices.Reverse(findings)
	got := misconfigGroups(findings, true)
	if len(got) != len(want) {
		t.Fatalf("got %d groups, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Key != want[i].Key || got[i].Subject != want[i].Subject {
			t.Errorf("row %d: %q (%+v), then %q (%+v) after reversing the scan", i, want[i].Subject, want[i].Key, got[i].Subject, got[i].Key)
		}
	}
}

// DEVDESK-766, the CVE list: the package, the versions and the target are cleaned
// the same way, so two packages that differ by an invisible character are two rows
// that say so.
func TestCVEGroupsMarkInvisibleCharactersInPackageNames(t *testing.T) {
	groups := cveGroups([]scan.Finding{
		cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-2", "open\u200bssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-3", "open\u202essl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-4", "zlib\u200b", "alpine", "1.2.11", "1.2.13", scan.SeverityHigh),
	})
	got := subjectsOf(groups)
	slices.Sort(got)
	want := []string{"open?ssl", "open?ssl", "openssl", "zlib?"}
	if !slices.Equal(got, want) {
		t.Fatalf("subjects = %q, want %q", got, want)
	}
}

func TestCVEGroupsMarkInvisibleCharactersInVersions(t *testing.T) {
	groups := cveGroups([]scan.Finding{
		cve("CVE-1", "openssl", "alpine", "1.1.1\u200bk", "1.1.1w", scan.SeverityHigh),
	})
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if want := "1.1.1?k -> 1.1.1w"; groups[0].Change != want {
		t.Errorf("Change = %q, want %q", groups[0].Change, want)
	}
}

func TestCVEGroupsOrdinaryNamesAreUnchanged(t *testing.T) {
	groups := cveGroups([]scan.Finding{cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh)})
	if len(groups) != 1 || groups[0].Subject != "openssl" || groups[0].Change != "1.1.1k -> 1.1.1w" {
		t.Errorf("got %+v, want openssl 1.1.1k -> 1.1.1w", groups)
	}
}

func TestCVEGroupKeysKeepTheRawName(t *testing.T) {
	groups := cveGroups([]scan.Finding{cve("CVE-2", "open\u200bssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh)})
	if len(groups) != 1 || groups[0].Key.Subject != "open\u200bssl" {
		t.Errorf("key = %+v, want the raw package name", groups)
	}
}
