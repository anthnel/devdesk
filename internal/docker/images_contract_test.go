package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// An image a container uses cannot be removed without force (and a contract never forces:
// the pinned image is the one every contract starts). Both engines refuse; docker says
// the image is being used by a container, podman that it is in use. The exit code
// is what the engine says, so it is logged and not required. The id of the image is public
// (usedImage declared it), the ids of the containers are redacted.
// OC-09b1 (DEVDESK-807), scenario images/rmi-used.
func TestContractRemovingAnImageInUseIsRefused(t *testing.T) {
	c := requireEngine(t)
	_, imageID := c.usedImage("c1")

	var err error
	// The sentinel of contract.sh and the container of the run both use the image, and the engine names
	// whichever it finds first (docker 12 characters after "container", podman 64 after "used by").
	ex := c.expectRmiRefusalWith(enginecontract.SameUpToContainerIDs(), "images", "rmi-used", "rmi of an image a container uses is refused", imageID, func() {
		err = RemoveImage(imageID, false)
	})

	if err == nil {
		t.Fatalf("RemoveImage(%s, false) = nil on an image a container uses, want the engine to refuse it", imageID)
	}
	if ex.Exit == 0 {
		t.Errorf("exit code = 0 for a refused rmi, want non-zero")
	}
	if !saysAny(ex.Stderr, "in use", "being used", "used by") {
		t.Errorf("stderr = %q, want the engine to say that a container uses the image", strings.TrimSpace(string(ex.Stderr)))
	}
	t.Logf("%s %s %s: rmi of an image in use exits %d, stderr %q",
		c.s.Engine.Tool, c.s.Engine.Version, c.s.Engine.Mode, ex.Exit, strings.TrimSpace(string(ex.Stderr)))
}
