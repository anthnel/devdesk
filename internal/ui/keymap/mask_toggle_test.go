package keymap

import (
	"slices"
	"testing"
)

// OC-13 (DEVDESK-811): `m` shows or hides the environment values of an inspect.
// It is a display toggle, local to the viewer, so the declaration comes with the
// binding (TestEveryLowercaseBindingIsDeclared). `m` already means "medium
// severity" in security: each surface declares its own, and the viewer's
// existing letters stay.
func TestViewerDeclaresTheMaskToggle(t *testing.T) {
	surface, ok := SurfaceFor("internal/ui/viewer/update.go")
	if !ok {
		t.Fatal("no surface covers internal/ui/viewer/")
	}
	if surface.Name != "viewer" {
		t.Fatalf("surface = %q, want viewer", surface.Name)
	}
	for _, key := range []string{"m", "f", "c", "w", "v", "t", "n", "g", "s"} {
		if !slices.Contains(surface.Keys, key) {
			t.Errorf("viewer does not declare %q; declares %v", key, surface.Keys)
		}
	}
}

// Declaring `m` for the viewer does not make it free on another surface, and
// the containers view, which opens the inspect, does not gain it.
func TestTheMaskToggleDoesNotWidenAnotherSurface(t *testing.T) {
	for _, path := range []string{"internal/ui/containers/update.go", "internal/ui/netdiag/ports_model.go", "internal/ui/workspaces/model.go"} {
		if surface, ok := SurfaceFor(path); ok && slices.Contains(surface.Keys, "m") {
			t.Errorf("%s declares m for %s", surface.Name, path)
		}
	}
}
