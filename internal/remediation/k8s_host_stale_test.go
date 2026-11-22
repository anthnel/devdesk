package remediation

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// A host namespace finding is about one named workload, and the manifest may
// have changed since the scan (DEVDESK-687): the line alone must not pick what
// gets edited. Trivy's message names the kind and the name ("Pod 'all' should
// not set 'spec.template.spec.hostIPC' to true", testdata/pss/trivy-0.76.2.json),
// and the fix compares them with the document it finds.

const twoPods = `apiVersion: v1
kind: Pod
metadata:
  name: first
spec:
  containers: [{name: a, image: a}]
---
apiVersion: v1
kind: Pod
metadata:
  name: second
spec:
  hostNetwork: true # second
  containers: [{name: b, image: b}]
`

// namedHostFinding is a KSV-0009 finding whose message names kind and name.
func namedHostFinding(kind, name string, line, endLine int) scan.Finding {
	return scan.Finding{
		ID: "KSV-0009", Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: line, EndLine: endLine,
		Message: kind + " '" + name + "' should not set 'spec.template.spec.hostNetwork' to true",
	}
}

// The finding was about 'first', the file changed, and the reported line now
// falls in 'second', which sets hostNetwork: the fix must not edit it.
func TestTheHostFixDeclinesAWorkloadThatIsNotTheOneNamed(t *testing.T) {
	out, reason := apply(t, twoPods, namedHostFinding("Pod", "first", 11, 14))
	if reason != ReasonWorkloadChanged {
		t.Errorf("reason = %q, want %q (output %q)", reason, ReasonWorkloadChanged, out)
	}
}

func TestTheHostFixDeclinesAKindThatIsNotTheOneNamed(t *testing.T) {
	out, reason := apply(t, twoPods, namedHostFinding("Deployment", "second", 11, 14))
	if reason != ReasonWorkloadChanged {
		t.Errorf("reason = %q, want %q (output %q)", reason, ReasonWorkloadChanged, out)
	}
}

// A message that does not name a workload gives nothing to compare, so nothing
// is invented: the fix declines.
func TestTheHostFixDeclinesAMessageThatNamesNoWorkload(t *testing.T) {
	f := namedHostFinding("Pod", "second", 11, 14)
	f.Message = "hostNetwork should not be true"
	out, reason := apply(t, twoPods, f)
	if reason != ReasonWorkloadChanged {
		t.Errorf("reason = %q, want %q (output %q)", reason, ReasonWorkloadChanged, out)
	}
}

// The pod spec has to be inside the lines the finding reported.
func TestTheHostFixDeclinesAPodSpecOutsideTheReportedLines(t *testing.T) {
	// The pod spec of 'second' starts on line 13 (its first key).
	for _, tt := range []struct {
		name          string
		line, endLine int
	}{
		{"ending before it", 11, 11},
		{"starting after it", 14, 14},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, reason := apply(t, twoPods, namedHostFinding("Pod", "second", tt.line, tt.endLine))
			if reason != ReasonWorkloadChanged {
				t.Errorf("reason = %q, want %q (output %q)", reason, ReasonWorkloadChanged, out)
			}
		})
	}
}

// Kind, name and lines agree: the fix is offered, on the second pod only.
func TestTheHostFixIsOfferedWhenTheWorkloadIsTheOneNamed(t *testing.T) {
	out, reason := apply(t, twoPods, namedHostFinding("Pod", "second", 11, 14))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	want := strings.Replace(twoPods, "hostNetwork: true # second", "hostNetwork: false # second", 1)
	if out != want {
		t.Errorf("got:\n%s\nwant only the second pod changed", out)
	}
}

// A key written as an alias may stand for the field the fix is looking for:
// which value is in force is not for this fix to decide.
func TestTheHostFixDeclinesAKeyWrittenAsAnAlias(t *testing.T) {
	const src = `apiVersion: v1
kind: Pod
metadata:
  name: w
x-key: &k hostNetwork
spec:
  *k : false
  hostNetwork: true
  containers: [{name: a, image: a}]
`
	out, reason := apply(t, src, namedHostFinding("Pod", "w", 6, 9))
	if reason != ReasonAmbiguousField {
		t.Errorf("reason = %q, want %q (output %q)", reason, ReasonAmbiguousField, out)
	}
}
