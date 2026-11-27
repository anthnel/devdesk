package docker

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09b1 (DEVDESK-807): the helpers of the tests of what the first recordings need
// from the contract support file, with no engine. Four things are added there
// (contract_support_test.go), and every one of them is a narrow, explicit exception
// to contractArgvRule, never a loosening of it:
//
//	// A window is one argv, allowed once, for the length of one call. The rule of the
//	// recorder is the allow list it always was; a window only adds the argv it names,
//	// and it closes when the call returns.
//
//	// (1) prune: allowed only when the whole store holds nothing but resources of
//	// the run, read right before the call. kind is "container", "network" or
//	// "volume"; the one argv is `<kind> prune -f`, what PruneContainers,
//	// PruneNetworks and PruneVolumes send. Never -a, never `system prune`, never an
//	// image prune.
//	func pruneWindowRule(kind string) func(argv []string) error // pure; true once, then refuses
//	func (c *contract) expectPrune(campaign, scenario, text, kind string, call func()) enginefixture.Exchange
//
//	// (2) a launch the engine is expected to refuse (name taken, port taken). The
//	// argv DevDesk builds has no label, so the standard rule refuses it; the window
//	// grants ONE `run` that names the exact name of the window, the pinned image,
//	// --pull=never, optionally the one loopback port, and passes the standard rule
//	// otherwise (a label is added to the argv it is judged on, nothing else is).
//	type launchWindow struct{ Name, Port string } // Port "" or "127.0.0.1:<port>:<port>"
//	func launchWindowRule(runID, image string, ours func(ref string) bool, w launchWindow) func(argv []string) error
//	func (c *contract) expectRefusal(campaign, scenario, text string, w launchWindow, call func()) enginefixture.Exchange
//
//	// (2b) rmi of an image a container of the run uses: the standard rule refuses
//	// every rmi (an image is never made by a contract). The window grants `rmi <id>`
//	// once, for an id usedImage returned, after a read has shown that a container of
//	// the run uses it.
//	func rmiWindowRule(imageID string) func(argv []string) error
//	func (c *contract) expectRmiRefusal(campaign, scenario, text, imageID string, call func()) enginefixture.Exchange
//
//	// (4) creation helpers, all of them label and name their resources like
//	// c.container and need the lab marks:
//	func (c *contract) healthContainer(role string) string
//	func (c *contract) waitHealthy(id string)
//	func (c *contract) portHolder(role string) (id, port string)
//	func (c *contract) usedVolume(volumeRole, containerRole string) (volume, container string)
//	func (c *contract) usedNetwork(networkRole, containerRole string) (network, container string)
//	func (c *contract) usedImage(role string) (container, imageID string)
//
//	// (5) ids a fixture may keep: Redaction.Public, filled from here.
//	func (c *contract) public(ids ...string)
//
// The contract struct gets `sleep func(time.Duration)` (time.Sleep from newContract;
// a test replaces it) and what it needs to remember (the public ids, the containers
// that use an image). The names of these tests do not start with "TestContract"
// (guard G5, R3).

// idImg stands for the id of the image the contracts use: 64 lower-case hex.
var idImg = strings.Repeat("d4", 32)

// ctrNameOf is the name c.container(role) and its siblings give to a resource.
func ctrNameOf(role string) string { return enginecontract.ResourceName(labRunID, role) }

// replyTo declares what the fake engine answers to one argv.
func replyTo(reply fakecli.Reply, argv ...string) fakecli.Exchange {
	return fakecli.Exchange{Args: argv, Reply: reply}
}

// startOnFake starts a contract of shape on a fakecli engine that answers steps and
// nothing else (an argv it does not know fails the test), with the lab marks and the
// pinned image set. Whatever the contract registered is cleaned when the test ends,
// before the runner and the engine are put back; the three listings of that cleanup
// are answered with an empty store.
func startOnFake(t *testing.T, shape engine.Shape, steps ...fakecli.Exchange) (*contract, *contractT, *fakecli.Fake) {
	t.Helper()
	openLab(t)
	t.Setenv(enginecontract.ImageEnv, contractImage)
	scenario := make(fakecli.Scenario, 0, len(steps)+3)
	for _, s := range steps {
		s.Tool = shape.Binary
		scenario = append(scenario, s)
	}
	for _, k := range resourceKinds(labRunID) {
		scenario = append(scenario, fakecli.Exchange{Tool: shape.Binary, Args: k.list})
	}
	fake := fakecli.Install(t, scenario, shape.Binary)
	ct := &contractT{TB: t}
	c := startContract(ct, sessionOf(shape, labRunID))
	t.Cleanup(ct.runCleanups)
	return c, ct, fake
}

// pinEngine pins the engine for the length of the test and puts the previous one back.
func pinEngine(t *testing.T, name string) {
	t.Helper()
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	useEngine(t, name)
}

// invokedArgs lists the argv the fake engine was started with, oldest first.
func invokedArgs(f *fakecli.Fake) [][]string {
	var out [][]string
	for _, inv := range f.Invocations() {
		out = append(out, inv.Args)
	}
	return out
}

// sameArgvs reports whether two lists of argv are equal, element by element.
func sameArgvs(got, want [][]string) bool {
	return slices.EqualFunc(got, want, func(a, b []string) bool { return slices.Equal(a, b) })
}

// mustReachTheEnd runs f and fails the test when it ended on a Fatal.
func mustReachTheEnd(t *testing.T, ct *contractT, f func()) {
	t.Helper()
	if ended := ct.run(f); ended {
		t.Fatalf("the contract ended on a Fatalf: fatals = %q, reports = %q", ct.fatals, ct.errs)
	}
}

// imageAnswer is what `image inspect --format {{.Id}}` prints for an image id: docker
// keeps the algorithm in front of it, podman does not.
func imageAnswer(shape engine.Shape, id string) string {
	if shape.Name == engine.Docker {
		return "sha256:" + id + "\n"
	}
	return id + "\n"
}

// networkAnswer is what `network create` prints: an id for docker, the name for podman.
func networkAnswer(shape engine.Shape, name string) string {
	if shape.Name == engine.Podman {
		return name + "\n"
	}
	return idB + "\n"
}

// imageInspectArgv is the read that gives the id of the pinned image.
func imageInspectArgv() []string {
	return []string{"image", "inspect", "--format", "{{.Id}}", contractImage}
}

// ancestorArgv is the read that lists the containers made from an image.
func ancestorArgv(imageID string) []string {
	return []string{"ps", "-a", "-q", "--no-trunc", "--filter", "ancestor=" + imageID}
}

// strayInspectArgv is the read that finds what a container of that name is: its id and
// the value of its devdesk.test label, which is empty for a launch that carries none.
func strayInspectArgv(name string) []string {
	return []string{"inspect", "--type", "container", "--format", `{{.Id}} {{index .Config.Labels "devdesk.test"}}`, name}
}

// launchArgv is what LaunchContainer sends for a detached launch by name, with the
// port when there is one.
func launchArgv(name, port string) []string {
	args := []string{"run", "--pull=never", "-d", "--name", name}
	if port != "" {
		args = append(args, "-p", port)
	}
	return append(args, contractImage)
}

// launchCall is the launch DevDesk makes, as a function a window can wrap.
func launchCall(name string, ports ...string) func() error {
	return func() error {
		_, err := LaunchContainer(ContainerLaunchOptions{Name: name, Image: contractImage, Ports: ports, Detach: true})
		return err
	}
}
