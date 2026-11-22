package remediation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-688 (S-03, criteria 1, 2 and 5) — the fix against what Trivy 0.76.2
// reported, recorded in testdata/seccompfix/ (see its README): the findings
// have the real lines and messages, the `after` files are what the fix must
// write, and the recorded re-scan of the `after` files is the proof that Trivy
// agrees. No Trivy is needed to run this.

// recordedFinding is one KSV-0030 or KSV-0104 of the recorded output.
type recordedFinding struct {
	id, message string
	line, end   int
}

// seccompRun is the recorded findings by target (file name), in report order.
type seccompRun map[string][]recordedFinding

func loadSeccompRun(t *testing.T, which string) seccompRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "seccompfix", "trivy-0.76.2-"+which+".json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var raw struct {
		Results []struct {
			Target            string
			Misconfigurations []struct {
				ID, Message, Status string
				CauseMetadata       struct{ StartLine, EndLine int }
			}
		}
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	run := seccompRun{}
	for _, r := range raw.Results {
		run[r.Target] = nil
		for _, m := range r.Misconfigurations {
			if m.Status == "FAIL" && (m.ID == "KSV-0030" || m.ID == "KSV-0104") {
				run[r.Target] = append(run[r.Target], recordedFinding{m.ID, m.Message, m.CauseMetadata.StartLine, m.CauseMetadata.EndLine})
			}
		}
	}
	return run
}

func readSeccompFixture(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "seccompfix", dir, name))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return string(data)
}

func (r recordedFinding) finding(target string) scan.Finding {
	f := scan.Finding{
		ID: r.id, Source: scan.SourceTrivyMisconfig, File: target, IaCType: "kubernetes",
		Line: r.line, EndLine: r.end, Message: r.message,
	}
	if r.id == "KSV-0104" {
		f.SeccompOrigin = "absent"
	}
	return f
}

// separatorLine is the line of the first "---" of src, or 0.
func separatorLine(src string) int {
	for i, l := range strings.Split(src, "\n") {
		if strings.TrimRight(l, "\r") == "---" {
			return i + 1
		}
	}
	return 0
}

func targetsOf(run seccompRun) []string {
	var names []string
	for name := range run {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestTheSeccompFixturesAreWhatTheTestsAssume(t *testing.T) {
	before, after := loadSeccompRun(t, "before"), loadSeccompRun(t, "after")
	if !slices.Equal(targetsOf(before), targetsOf(after)) {
		t.Fatalf("the two recordings are not about the same files: %v and %v", targetsOf(before), targetsOf(after))
	}
	if len(before) < 6 {
		t.Fatalf("%d files recorded, want the six of the README", len(before))
	}
	for name, findings := range before {
		if len(findings) < 2 {
			t.Errorf("%s: %d KSV-0030 / KSV-0104 recorded before the fix, want at least one of each", name, len(findings))
		}
	}
	// The repository normalizes text to LF: the CRLF files keep theirs only
	// through the .gitattributes of the fixture.
	for _, dir := range []string{"before", "after"} {
		src := readSeccompFixture(t, dir, "pod-crlf.yaml")
		if strings.Count(src, "\n") == 0 || strings.Count(src, "\n") != strings.Count(src, "\r\n") {
			t.Errorf("%s/pod-crlf.yaml lost its CRLF line endings", dir)
		}
	}
}

// Every recorded finding, KSV-0030 and KSV-0104, in every file: the fix gives
// the bytes of `after/` for a finding of the workload it fixed, and only that
// workload's. For the file of two Pods, a finding of the second Pod fixes the
// second Pod and leaves the first one's bytes alone.
func TestTheFixWritesTheRecordedAfterFileForEveryRecordedFinding(t *testing.T) {
	before := loadSeccompRun(t, "before")
	for _, name := range targetsOf(before) {
		src := readSeccompFixture(t, "before", name)
		want := readSeccompFixture(t, "after", name)
		sep := separatorLine(src)
		for _, rec := range before[name] {
			t.Run(name+" "+rec.id+" line "+strconv.Itoa(rec.line), func(t *testing.T) {
				f := rec.finding(name)
				out, reason := seccompApply(t, src, f)
				if reason != "" {
					t.Fatalf("declined: %s (message %q, lines %d-%d)", reason, rec.message, rec.line, rec.end)
				}
				if sep != 0 && rec.line > sep {
					// The second document: the first one's lines are as they were.
					head := strings.Join(strings.SplitAfter(src, "\n")[:sep], "")
					if !strings.HasPrefix(out, head) {
						t.Errorf("the fix of the second document changed the first one:\n%s", out)
					}
					if out == want {
						t.Error("a finding of the second document fixed the first one")
					}
					if got := strings.Count(out, "RuntimeDefault"); got != 1 {
						t.Errorf("%d RuntimeDefault in the output, want exactly the second pod's", got)
					}
					return
				}
				if out != want {
					t.Errorf("got:\n%s\nwant the recorded after file:\n%s", out, want)
				}
			})
		}
	}
}

// The recorded re-scan of the fixed files: nothing left in the workload that
// was fixed; for the file of two Pods, the second one keeps its findings.
func TestTheRecordedRescanClearsTheFixedWorkloadAndNothingElse(t *testing.T) {
	after := loadSeccompRun(t, "after")
	for _, name := range targetsOf(after) {
		findings := after[name]
		if !strings.Contains(name, "two-pods") {
			if len(findings) != 0 {
				t.Errorf("%s: Trivy still reports %d KSV-0030 / KSV-0104 after the fix: %+v", name, len(findings), findings)
			}
			continue
		}
		sep := separatorLine(readSeccompFixture(t, "after", name))
		if len(findings) == 0 {
			t.Errorf("%s: the second Pod lost its findings; the fix must leave it alone", name)
		}
		for _, rec := range findings {
			if rec.line <= sep {
				t.Errorf("%s: %s still reported in the fixed first Pod, line %d", name, rec.id, rec.line)
			}
			if rec.id == "KSV-0104" && !strings.Contains(rec.message, `pod "second"`) {
				t.Errorf("%s: a KSV-0104 of another pod than the second: %q", name, rec.message)
			}
		}
	}
}

// seccompStillReported is the judgement of criterion 5 as the plan states it
// (the view's own copy lives in internal/ui/security): the ids of the rules of
// the scope that a re-scan still reports in the scope's lines.
func seccompStillReported(findings []recordedFinding, s Scope) []string {
	var still []string
	for _, rec := range findings {
		inRules := slices.ContainsFunc(s.Rules, func(id string) bool { return RuleKey(id) == RuleKey(rec.id) })
		inLines := (s.Line == 0 && s.EndLine == 0) || (rec.line >= s.Line && rec.line <= s.EndLine)
		if inRules && inLines && !slices.Contains(still, rec.id) {
			still = append(still, rec.id)
		}
	}
	return still
}

// The scope the rule gives for the recorded fix, applied to the recorded
// re-scans: the `after` run is cleared (the second Pod's findings are outside
// the scope, the three containers of a Deployment are inside it), the `before`
// run is still reported.
func TestTheScopeOfTheRecordedFixJudgesTheRecordedRescans(t *testing.T) {
	before, after := loadSeccompRun(t, "before"), loadSeccompRun(t, "after")
	for _, name := range targetsOf(before) {
		t.Run(name, func(t *testing.T) {
			src := readSeccompFixture(t, "before", name)
			// Trivy does not report in file order (the second Pod comes first
			// in two-pods.yaml): the first workload is the one at the lowest line.
			first := recordedFinding{line: 1 << 30}
			for _, rec := range before[name] {
				if rec.id == "KSV-0030" && rec.line < first.line {
					first = rec
				}
			}
			f := first.finding(name)
			rule, ok := FixFor(f)
			if !ok {
				t.Fatalf("no rule for %s", f.ID)
			}
			edits, reason := rule.Fix([]byte(src), f)
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			updated := rewritten(t, src, edits)
			if updated != readSeccompFixture(t, "after", name) {
				t.Fatalf("the fix of the first KSV-0030 is not the recorded after file:\n%s", updated)
			}

			scope := rule.ScopeOf([]byte(updated), edits)
			if still := seccompStillReported(after[name], scope); len(still) != 0 {
				t.Errorf("the recorded re-scan of the fixed file is judged still reported: %v (scope %+v)", still, scope)
			}
			if still := seccompStillReported(before[name], scope); len(still) == 0 {
				t.Errorf("the recorded scan before the fix is judged cleared (scope %+v)", scope)
			}
		})
	}
}
