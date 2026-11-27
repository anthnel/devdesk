package enginefixture_test

import (
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b, decision of the team lead on the review points of the tester: a dot is a
// word boundary for the three name rules (lab-identity, local-identity and
// exact-term). A fully qualified name hides nothing: the short name of a machine is
// seen at the start, in the middle and at the end of "<host>.<domain>", and before
// the dot that ends a sentence. A longer word still is not the name ("zedboxes").

func TestADotIsAWordBoundaryForTheNameRules(t *testing.T) {
	tests := []leakCase{
		{name: "lab/a fully qualified name", mutate: inStdout("jakku.corp.example"), planted: "jakku.corp.example", rules: []string{rLab}},
		{name: "lab/in the middle of a name", mutate: inStdout("srv.jakku.example"), planted: "srv.jakku.example", rules: []string{rLab}},
		{name: "lab/at the end of a name", mutate: inStdout("srv.example.dd-podman"), planted: "srv.example.dd-podman", rules: []string{rLab}},
		{name: "lab/before the dot of a sentence", mutate: inStdout("logged in on jakku."), planted: "logged in on jakku.", rules: []string{rLab}},
		{name: "local/a fully qualified name", mutate: inStdout("zedbox.corp.example"), planted: "zedbox.corp.example", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/in the middle of a name", mutate: inStdout("srv.zedbox.example"), planted: "srv.zedbox.example", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/in a URL", mutate: inStdout("http://127.0.0.1/zedbox.example/x"), planted: "zedbox.example", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "local/in a path with an extension", mutate: inStdout("/srv/zedbox.conf"), planted: "zedbox.conf", rules: []string{rLocal}, local: []string{"zedbox"}},
		{name: "exact/a fully qualified name", mutate: inStdout("zz-denied-term.example.org"), planted: "zz-denied-term.example.org", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
		{name: "exact/at the end of a name", mutate: inStdout("srv.zz-denied-term"), planted: "srv.zz-denied-term", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
		{name: "exact/before a digit after the dot", mutate: inStdout("zz-denied-term.2"), planted: "zz-denied-term.2", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
		{name: "exact/in a base64 stream", mutate: inBinaryStdout("srv.zz-denied-term.example.org"), planted: "zz-denied-term", at: "# stdout:", rules: []string{rExact}, exact: []string{"zz-denied-term"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assertPlanted(t, tc) })
	}
}

func TestADotDoesNotMakeALongerWordTheName(t *testing.T) {
	tests := []cleanCase{
		{name: "lab/a longer word before a dot", rule: rLab, m: inStdout("jakkus.example.org")},
		{name: "lab/glued before a dot", rule: rLab, m: inStdout("add.example.org")},
		{name: "lab/ddm before a dot", rule: rLab, m: inStdout("ddm-run.example.org")},
		{name: "local/a longer word before a dot", rule: rLocal, m: inStdout("zedboxes.example.org"), local: []string{"zedbox"}},
		{name: "local/glued before a dot", rule: rLocal, m: inStdout("myzedbox.example.org"), local: []string{"zedbox"}},
		{name: "local/glued after a dot", rule: rLocal, m: inStdout("srv.myzedbox"), local: []string{"zedbox"}},
		{name: "exact/glued before a dot", rule: rExact, m: inStdout("xzz-denied-term.example.org"), exact: []string{"zz-denied-term"}},
		{name: "exact/glued after a dot", rule: rExact, m: inStdout("srv.zz-denied-termx"), exact: []string{"zz-denied-term"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := plantInto(t, tc.m)
			if leaks := enginefixture.CheckLeaksAs("dots--docker", data, tc.exact, tc.local); len(leaks) != 0 {
				t.Errorf("CheckLeaksAs = %v, want none for:\n%s", leaks, data)
			}
		})
	}
}
