package remediation

import (
	"strings"
	"testing"
)

// The inserted lines take the line ending of the line they go under, not of
// the first CRLF found anywhere: one stray "\r\n" in an LF file must not mix
// endings (DEVDESK-688).
func TestAStrayCRLFInAnLFFileKeepsTheInsertionInLF(t *testing.T) {
	const lf = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  containers:\n    - name: app\n      image: app:1.0\n"
	src := strings.Replace(lf, "name: w\n", "name: w\r\n", 1)
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			out, reason := seccompApply(t, src, seccompFinding(t, id, src, "Pod", "w", "app", 0, 0))
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			want := strings.Replace(spliceAfterLine(t, lf, 5, "  securityContext:\n    seccompProfile:\n      type: RuntimeDefault\n"), "name: w\n", "name: w\r\n", 1)
			if out != want {
				t.Errorf("got %q\nwant %q", out, want)
			}
		})
	}
}
