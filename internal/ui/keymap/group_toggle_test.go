package keymap

import (
	"slices"
	"testing"
)

// DEVDESK-754 (SC1): `g` is the display toggle of the :sec results, Group by fix.
// A lowercase letter is a filter or a toggle, local to its view, and only a letter
// declared in localToggles may be bound (TestEveryLowercaseBindingIsDeclared), so
// the declaration comes with the binding. The viewer, workspaces and the forge
// explorer keep their own `g` (go to, fuzzy find): each surface declares its own.
func TestSecurityFindingsDeclaresTheGroupByFixKey(t *testing.T) {
	surface, ok := SurfaceFor("internal/ui/security/findings.go")
	if !ok {
		t.Fatal("no surface covers internal/ui/security/")
	}
	if surface.Name != "security/findings" {
		t.Fatalf("surface = %q, want security/findings", surface.Name)
	}
	if !slices.Contains(surface.Keys, "g") {
		t.Errorf("%s does not declare \"g\"; declares %v", surface.Name, surface.Keys)
	}
	// The five it already had are still there.
	for _, key := range []string{"c", "h", "m", "l", "p"} {
		if !slices.Contains(surface.Keys, key) {
			t.Errorf("%s lost %q; declares %v", surface.Name, key, surface.Keys)
		}
	}
}

// The declaration stays on the security surface: it does not make `g` free
// elsewhere, and it does not retire the other meanings.
func TestGroupByFixDoesNotWidenAnotherSurface(t *testing.T) {
	for _, path := range []string{"internal/ui/containers/model.go", "internal/ui/netdiag/ports_model.go", "internal/ui/oci_resources/browser_model.go"} {
		if surface, ok := SurfaceFor(path); ok && slices.Contains(surface.Keys, "g") {
			t.Errorf("%s declares g for %s", surface.Name, path)
		}
	}
	for _, path := range []string{"internal/ui/viewer/model.go", "internal/ui/workspaces/model.go", "internal/ui/forge/explorer/model.go"} {
		if surface, ok := SurfaceFor(path); !ok || !slices.Contains(surface.Keys, "g") {
			t.Errorf("%s lost its own g", path)
		}
	}
}
