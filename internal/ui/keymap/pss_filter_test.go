package keymap

import (
	"slices"
	"testing"
)

// DEVDESK-686 (S-01): `p` is the Pod Security filter of the security findings
// table. A lowercase letter is a filter, local to its view, and only a letter
// declared in localToggles may be bound (TestEveryLowercaseBindingIsDeclared).
// The binding is written next to the declaration, so the declaration must name
// it before the view binds it.
func TestSecurityFindingsDeclaresThePodSecurityFilterKey(t *testing.T) {
	surface, ok := SurfaceFor("internal/ui/security/pss.go")
	if !ok {
		t.Fatal("no surface covers internal/ui/security/")
	}
	if surface.Name != "security/findings" {
		t.Fatalf("surface = %q, want security/findings", surface.Name)
	}
	for _, key := range []string{"c", "h", "m", "l", "p"} {
		if !slices.Contains(surface.Keys, key) {
			t.Errorf("%s does not declare %q; declares %v", surface.Name, key, surface.Keys)
		}
	}
}
