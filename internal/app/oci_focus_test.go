package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
)

// TestAnImageFocusRequestOpensTheOCIView covers containers' J: the router
// switches to the OCI view, builds it if needed, and hands it the request.
func TestAnImageFocusRequestOpensTheOCIView(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	a.currentView = command.ViewContainers

	_, cmd := a.Update(ociresources.FocusImageRequestMsg{Image: "nginx:latest"})

	if a.currentView != command.ViewOCIResources {
		t.Fatalf("currentView = %q, want oci-resources", a.currentView)
	}
	if _, ok := a.views[command.ViewOCIResources]; !ok {
		t.Error("the OCI view was not built")
	}
	if cmd == nil {
		t.Error("no command: the view was neither initialized nor asked to list its images")
	}
}
