package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// A launch with the name of a container that exists is refused, with the same exit code on
// both engines. The launch DevDesk builds carries no label, so it goes through a launch window
// (expectRefusal) for that one name.
// OC-09b1 (DEVDESK-807), scenario launch/name-taken.
func TestContractLaunchWithATakenNameIsRefused(t *testing.T) {
	c := requireEngine(t)
	c.container("c1")
	name := enginecontract.ResourceName(c.s.RunID, "c1")

	var err error
	ex := c.expectRefusal("launch", "name-taken", "a launch with the name of a container that exists is refused",
		launchWindow{Name: name}, func() {
			_, err = LaunchContainer(c.ownLaunch(name))
		})

	if err == nil {
		t.Fatalf("LaunchContainer(%s) = nil with a name that is taken, want the engine to refuse it", name)
	}
	if ex.Exit != 125 {
		t.Errorf("exit code = %d, want 125 on both engines", ex.Exit)
	}
	if !saysAny(ex.Stderr, "already in use") {
		t.Errorf("stderr = %q, want the engine to say that the name is already in use", strings.TrimSpace(string(ex.Stderr)))
	}
}

// A launch that publishes a host port another container holds is refused. The holder is a
// container of this run on 127.0.0.1:18080 (portHolder); the engines word it differently
// (docker: the port is already allocated, exit 125; podman: the address is already in use,
// exit 126), so the exit code is one of the two and the words one of three. Docker creates
// the container before it fails to bind: expectRefusal removes it, by its id, after the call.
// OC-09b1 (DEVDESK-807), scenario launch/port-taken.
func TestContractLaunchOnATakenPortIsRefused(t *testing.T) {
	c := requireEngine(t)
	_, port := c.portHolder("p1")
	name := enginecontract.ResourceName(c.s.RunID, "c2")
	// Docker's refusal names the network endpoint it set up for the container it made and then
	// could not start (64 hexadecimal characters, new at every run), and no resource of the run owns
	// it: the recording replaces up to two such ids (two words seen on docker 29.10.1) by stable
	// placeholders, and refuses a third.
	c.anonymous(2)

	var err error
	ex := c.expectRefusal("launch", "port-taken", "a launch that publishes a port another container holds is refused",
		launchWindow{Name: name, Port: "127.0.0.1:" + port + ":80"}, func() {
			_, err = LaunchContainer(c.ownLaunch(name, port+":80"))
		})

	if err == nil {
		t.Fatalf("LaunchContainer(%s) = nil with a port that is taken, want the engine to refuse it", name)
	}
	if ex.Exit != 125 && ex.Exit != 126 {
		t.Errorf("exit code = %d, want 125 (docker) or 126 (podman)", ex.Exit)
	}
	if !saysAny(ex.Stderr, "already allocated", "already in use", "address in use") {
		t.Errorf("stderr = %q, want the engine to say that the port is taken", strings.TrimSpace(string(ex.Stderr)))
	}
}
