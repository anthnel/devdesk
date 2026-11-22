package remediation

import (
	"strings"
	"testing"
)

// DEVDESK-689 review: wording that the footer's one line can cut without losing
// the need, and details that are exact.

func TestMissingTextPutsTheRuleAndTheNeedFirst(t *testing.T) {
	for _, tc := range missingCases() {
		text, _ := MissingFor(tc.f)
		prefix := tc.f.ID + " (" + string(tc.profile) + "): "
		if !strings.HasPrefix(text, prefix) {
			t.Errorf("%s: %q does not start with %q", tc.name, text, prefix)
		}
		if i := strings.Index(text, "no built-in fix"); i != -1 && i <= len(prefix) {
			t.Errorf("%s: the secondary precision comes before the need: %q", tc.name, text)
		}
	}
}

// selinux_custom_options_set.rego has a second rule: a role or a user key fails.
func TestKSV0025AlsoSaysNotToSetRoleOrUser(t *testing.T) {
	text, _ := MissingFor(pssFinding("KSV-0025"))
	for _, want := range []string{"role", "user"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text lacks %q: %s", want, text)
		}
	}
}

// KSV-0012 is met by the pod, or by every container.
func TestKSV0012SaysOnThePodOrOnEveryContainer(t *testing.T) {
	text, _ := MissingFor(pssFinding("KSV-0012"))
	if !strings.Contains(text, "on the pod, or on every container") {
		t.Errorf("the text is not exact about where: %s", text)
	}
}

// The disallowed list of non_core_volume_types.rego holds nfs.
func TestKSV0028NamesARefusedType(t *testing.T) {
	text, _ := MissingFor(pssFinding("KSV-0028"))
	for _, want := range []string{"emptyDir", "nfs"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text lacks %q: %s", want, text)
		}
	}
}
