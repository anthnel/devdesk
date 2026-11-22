package remediation

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-688 (S-03, criteria 4 and 6, and what criterion 5 needs from the
// catalog) — KSV-0030 and KSV-0104 are rules with a fix; the confirmation
// carries a warning; the finding alone greys KSV-0104 Unconfined; and a rule
// that clears a whole pod says so, so the re-scan looks at the pod.

func seccompRuleFinding(id string) scan.Finding {
	return scan.Finding{ID: id, Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes"}
}

func seccompRule(t *testing.T, id string) Rule {
	t.Helper()
	rule, ok := FixFor(seccompRuleFinding(id))
	if !ok {
		t.Fatalf("no rule for %q", id)
	}
	return rule
}

func TestKSV0030AndKSV0104HaveTheSameFixInTheCatalog(t *testing.T) {
	for _, id := range []string{"KSV-0030", "AVD-KSV-0030", "KSV030", "KSV-0104", "AVD-KSV-0104", "KSV104"} {
		t.Run(id, func(t *testing.T) {
			rule := seccompRule(t, id)
			if rule.Title != "Set seccompProfile.type: RuntimeDefault on the pod" {
				t.Errorf("Title = %q", rule.Title)
			}
			if rule.Fix == nil {
				t.Error("the rule has no Fix")
			}
		})
	}
}

// Criterion 4: the confirmation carries the risk the re-scan cannot see.
func TestTheSeccompFixWarnsThatTheWorkloadMayFailAtRunTime(t *testing.T) {
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			rule := seccompRule(t, id)
			w := strings.ToLower(rule.Warning)
			for _, want := range []string{"run time", "re-scan", "seccomp", "runtimedefault"} {
				if !strings.Contains(w, want) {
					t.Errorf("Warning = %q, want it to say %q", rule.Warning, want)
				}
			}
			if rule.Warning != seccompWarning {
				t.Errorf("Warning = %q, want %q", rule.Warning, seccompWarning)
			}
			if !strings.HasPrefix(rule.Warning, "A workload that ran without a seccomp filter may fail at run time under RuntimeDefault") {
				t.Errorf("the warning does not open with the risk: %q", rule.Warning)
			}
		})
	}
}

// Criterion 6, the part the finding alone settles: an explicit Unconfined is
// greyed without reading the file, with the S-04 text. Everything else is
// settled at the press, from the manifest (see the refusal tests).
func TestTheFindingAloneGreysAnExplicitUnconfined(t *testing.T) {
	rule := seccompRule(t, "KSV-0104")
	if rule.Declines == nil {
		t.Fatal("KSV-0104 has no Declines")
	}
	f := seccompRuleFinding("KSV-0104")
	f.SeccompOrigin = "unconfined"
	want, ok := MissingFor(f)
	if !ok {
		t.Fatal("MissingFor has no text for KSV-0104 Unconfined")
	}
	if want != "KSV-0104 (baseline): set seccompProfile.type: RuntimeDefault, not Unconfined (no built-in fix)" {
		t.Fatalf("the S-04 text changed: %q", want)
	}
	if got := rule.Declines(f); got != want {
		t.Errorf("Declines = %q, want the S-04 text %q", got, want)
	}
	for _, spelling := range []string{"AVD-KSV-0104", "KSV104"} {
		g := f
		g.ID = spelling
		if got := seccompRule(t, spelling).Declines(g); got != want {
			t.Errorf("%s: Declines = %q, want %q", spelling, got, want)
		}
	}
}

// Nothing else is greyed from the finding: not an absent profile, not an
// origin the scan could not read, not a KSV-0030 (it carries no origin, and
// even one that did would not be trusted for this).
func TestTheFindingAloneGreysNothingElse(t *testing.T) {
	tests := []struct {
		name, id, origin string
	}{
		{"KSV-0104 absent", "KSV-0104", "absent"},
		{"KSV-0104 with no origin", "KSV-0104", ""},
		{"KSV-0104 with an origin from a later version", "KSV-0104", "something-new"},
		{"KSV-0030", "KSV-0030", ""},
		{"KSV-0030 with a copied origin", "KSV-0030", "unconfined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := seccompRule(t, tt.id)
			if rule.Declines == nil {
				t.Fatal("the rule has no Declines")
			}
			f := seccompRuleFinding(tt.id)
			f.SeccompOrigin = tt.origin
			if got := rule.Declines(f); got != "" {
				t.Errorf("Declines = %q, want none", got)
			}
		})
	}
}

// seccompDeclines is the same function for both rules, and what it returns is
// never a reason of the file: those need the bytes.
func TestSeccompDeclinesOnlyEverSaysTheS04Text(t *testing.T) {
	for _, origin := range []string{"", "absent", "unconfined", "x"} {
		f := seccompRuleFinding("KSV-0104")
		f.SeccompOrigin = origin
		got := seccompDeclines(f)
		if origin == "unconfined" {
			if text, _ := MissingFor(f); got != text {
				t.Errorf("origin %q: got %q, want %q", origin, got, text)
			}
			continue
		}
		if got != "" {
			t.Errorf("origin %q: got %q, want none", origin, got)
		}
	}
}

// The re-scan judges the whole pod for these two rules, and for nothing else:
// every other rule of the catalog keeps judging its own occurrence.
func TestOnlyTheSeccompRulesDeclareAScopeAndAFindingRefusal(t *testing.T) {
	for key, rule := range catalog {
		if key == RuleKey("KSV-0030") || key == RuleKey("KSV-0104") {
			continue
		}
		if rule.Declines != nil {
			t.Errorf("%s has a Declines it did not have before DEVDESK-688", rule.AVDID)
		}
		if rule.WorkloadRules != nil {
			t.Errorf("%s has WorkloadRules it did not have before DEVDESK-688", rule.AVDID)
		}
	}
	for _, id := range seccompIDs {
		got := slices.Clone(seccompRule(t, id).WorkloadRules)
		slices.Sort(got)
		if !slices.Equal(got, []string{"KSV-0030", "KSV-0104"}) {
			t.Errorf("%s: WorkloadRules = %v, want KSV-0030 and KSV-0104 (the fix closes both)", id, got)
		}
	}
}

// ── ScopeOf ──────────────────────────────────────────────────────────────────

func rewritten(t *testing.T, src string, edits []patch.Edit) string {
	t.Helper()
	out, err := patch.Rewrite([]byte(src), edits)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	return string(out)
}

func sortedRules(s Scope) []string {
	rules := slices.Clone(s.Rules)
	slices.Sort(rules)
	return rules
}

// Fixing the first of two Pods in a file: the scope is the lines of the first
// document, read in the updated content, so the second Pod's findings are not
// the first one's business (F14 of the plan).
func TestTheScopeIsTheDocumentOfTheEditInTheUpdatedContent(t *testing.T) {
	const src = `apiVersion: v1
kind: Pod
metadata:
  name: first
spec:
  containers:
    - name: a
      image: a:1
---
apiVersion: v1
kind: Pod
metadata:
  name: second
spec:
  containers:
    - name: b
      image: b:1
`
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			rule := seccompRule(t, id)
			f := seccompFinding(t, id, src, "Pod", "first", "a", 0, 0)
			edits, reason := rule.Fix([]byte(src), f)
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			updated := rewritten(t, src, edits)

			scope := rule.ScopeOf([]byte(updated), edits)
			if got := sortedRules(scope); !slices.Equal(got, []string{"KSV-0030", "KSV-0104"}) {
				t.Errorf("Rules = %v, want KSV-0030 and KSV-0104", got)
			}
			// The first document grew by the three inserted lines: 1..8 became 1..11.
			if scope.Line != 1 || scope.EndLine != 11 {
				t.Errorf("lines = %d..%d, want 1..11 (the first document of the updated file)", scope.Line, scope.EndLine)
			}
		})
	}
}

// A rule without WorkloadRules has no scope: the verification looks at its own
// occurrence, as it always did.
func TestARuleWithoutWorkloadRulesHasTheZeroScope(t *testing.T) {
	rule := catalog[RuleKey("KSV-0009")]
	if rule.WorkloadRules != nil {
		t.Fatal("the test needs a rule without WorkloadRules")
	}
	edit := patch.Edit{Span: patch.Span{Start: 0, End: 0}, New: "x\n"}
	if got := rule.ScopeOf([]byte("x\nkind: Pod\n"), []patch.Edit{edit}); got.Rules != nil || got.Line != 0 || got.EndLine != 0 {
		t.Errorf("ScopeOf = %+v, want the zero Scope", got)
	}
}

// An edit that lands in no document (a header comment): the rules are still
// the rule's, and the lines are unknown, which the judge reads as the whole
// file: the safe side, a verdict of "still reported" at worst.
func TestAnEditOutsideAnyDocumentScopesTheWholeFile(t *testing.T) {
	const src = "# header\napiVersion: v1\nkind: Pod\nmetadata:\n  name: a\n"
	edit := patch.Edit{Span: patch.Span{Start: 0, End: 0}, New: "# new\n"}
	updated := rewritten(t, src, []patch.Edit{edit})

	scope := seccompRule(t, "KSV-0030").ScopeOf([]byte(updated), []patch.Edit{edit})
	if got := sortedRules(scope); !slices.Equal(got, []string{"KSV-0030", "KSV-0104"}) {
		t.Errorf("Rules = %v, want KSV-0030 and KSV-0104", got)
	}
	if scope.Line != 0 || scope.EndLine != 0 {
		t.Errorf("lines = %d..%d, want 0..0 (the whole file)", scope.Line, scope.EndLine)
	}
}
