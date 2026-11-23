package security

import (
	"strings"
	"testing"
	"unicode"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-754, review: the grouped surfaces show text a scanner wrote, so none of
// it may carry an escape sequence, a control character or a bidi mark.

// hostileText holds an OSC title change, a CSI colour, a bidi override, a zero
// width mark, a carriage return and a newline.
const hostileText = "\x1b]0;pwned\x07\x1b[31m\u202e\u200b\r\n"

func requireTerminalSafe(t *testing.T, what, s string) {
	t.Helper()
	for _, r := range s {
		if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '‌' && r != '‍') {
			t.Errorf("%s = %q holds the unsafe rune %U", what, s, r)
			return
		}
	}
}

func TestCVEGroupsShowNoControlOrBidiTextFromTheScan(t *testing.T) {
	tests := []struct {
		name string
		f    scan.Finding
	}{
		{"fixed package", cve("CVE-1", "zl"+hostileText+"ib", "npm", "1.0"+hostileText, "1.1"+hostileText, scan.SeverityHigh)},
		{"unfixed package", cve("CVE-2", "zl"+hostileText+"ib", "npm", "1.0"+hostileText, "", scan.SeverityHigh)},
		{"candidates that cannot be ordered", cve("CVE-3", "libfoo", "npm", "1.0.0", "abc"+hostileText+", def"+hostileText, scan.SeverityHigh)},
		{"a vulnerability that names no package", cve("CVE-4"+hostileText, "", "npm", "1.0"+hostileText, "1.1", scan.SeverityHigh)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			groups := cveGroups([]scan.Finding{tc.f})
			if len(groups) != 1 {
				t.Fatalf("got %d groups, want 1", len(groups))
			}
			g := groups[0]
			requireTerminalSafe(t, "Subject", g.Subject)
			requireTerminalSafe(t, "Change", g.Change)
			requireTerminalSafe(t, "Target", g.Target)
			requireTerminalSafe(t, "title", groupTitle(g))
			requireTerminalSafe(t, "footer sentence", groupSentence(g))
			for _, c := range cveGroupColumns() {
				requireTerminalSafe(t, "cell "+c.Title, c.Cell(g))
			}
			if strings.Contains(g.Subject+g.Change, "\n") {
				t.Errorf("a line feed survives in %q / %q", g.Subject, g.Change)
			}
		})
	}
}

// Two vulnerabilities that name no package and share an ID, an ecosystem and a
// version are two rows, and each is found again by its own key.
func TestGroupKeysTellApartVulnerabilitiesWithNoPackage(t *testing.T) {
	a := scan.Finding{ID: "CVE-9", Source: scan.SourceTrivy, Ecosystem: "npm", Version: "1.0", Severity: scan.SeverityHigh, File: "a/lock.json"}
	b := a
	b.File = "b/lock.json"
	named := cve("CVE-1", "CVE-9", "npm", "1.0", "", scan.SeverityHigh)
	groups := cveGroups([]scan.Finding{a, b, named})
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	for _, g := range groups {
		got, ok := groupByKey(groups, g.Key)
		if !ok || len(got.Members) != 1 || got.Members[0].File != g.Members[0].File || got.Members[0].PkgName != g.Members[0].PkgName {
			t.Errorf("key %+v finds %+v, want the group it came from", g.Key, got)
		}
	}
}
