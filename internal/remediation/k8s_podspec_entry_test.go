package remediation

import (
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-688 (S-03, step 2) — podSpecFlag was cut in two so the seccomp fix
// can ask for a key the pod spec may not have: locatePodSpec finds the pod spec,
// podSpecEntry finds a key of it and says nothing when it is absent. The
// host-namespace tests (k8s_host_test.go, k8s_host_stale_test.go) are the net
// for the behavior of podSpecFlag, which is unchanged: an absent field there
// is still ReasonHostFieldNotTrue.

func podSpecEntryOf(t *testing.T, src string, line int, field string) (found bool, reason string) {
	t.Helper()
	f := scan.Finding{ID: "KSV-0030", Source: scan.SourceTrivyMisconfig, IaCType: "kubernetes", Line: line, EndLine: line}
	doc, ps, reason := locatePodSpec([]byte(src), f)
	if reason != "" {
		t.Fatalf("locatePodSpec: %s", reason)
	}
	key, value, reason := podSpecEntry(doc, ps.Spec, field)
	if (key == nil) != (value == nil) {
		t.Errorf("key = %v, value = %v: one without the other", key, value)
	}
	return value != nil, reason
}

func TestAnAbsentKeyIsNotARefusal(t *testing.T) {
	const src = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  hostNetwork: false\n  containers:\n    - name: a\n      image: a\n"
	found, reason := podSpecEntryOf(t, src, 8, "securityContext")
	if found || reason != "" {
		t.Errorf("found = %v, reason = %q; want nil, nil and no reason for a key the pod spec does not set", found, reason)
	}
}

func TestAPresentKeyIsFound(t *testing.T) {
	const src = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  securityContext:\n    runAsUser: 1\n  containers:\n    - name: a\n      image: a\n"
	found, reason := podSpecEntryOf(t, src, 9, "securityContext")
	if !found || reason != "" {
		t.Errorf("found = %v, reason = %q; want the key", found, reason)
	}
}

func TestAnAbsentKeyBesideAMergeKeyIsAmbiguous(t *testing.T) {
	const src = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nx-base: &base\n  dnsPolicy: ClusterFirst\nspec:\n  <<: *base\n  containers:\n    - name: a\n      image: a\n"
	_, reason := podSpecEntryOf(t, src, 10, "securityContext")
	if reason != ReasonAmbiguousField {
		t.Errorf("reason = %q, want %q", reason, ReasonAmbiguousField)
	}
}

func TestLocatePodSpecRefusesWhatIsNotOneWorkload(t *testing.T) {
	f := scan.Finding{ID: "KSV-0030", Source: scan.SourceTrivyMisconfig, IaCType: "kubernetes", Line: 1, EndLine: 3}
	if _, _, reason := locatePodSpec([]byte("apiVersion: v1\nkind: ConfigMap\ndata: {}\n"), f); reason != ReasonUnreadableSpan {
		t.Errorf("a ConfigMap: reason = %q, want %q", reason, ReasonUnreadableSpan)
	}
	f.IaCType = "helm"
	if _, _, reason := locatePodSpec([]byte("kind: Pod\n"), f); reason != ReasonNotAManifest {
		t.Errorf("a Helm template: reason = %q, want %q", reason, ReasonNotAManifest)
	}
}
