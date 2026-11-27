package docker

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// The support file of the engine contract tests (OC-09a, DEVDESK-806): a contract
// is a TestContract* function of a *_contract_test.go file of this package that
// calls the functions of the package against a real docker or podman, outside CI,
// in the test VMs of tools/engine-vms (mise run engine-contract). Everything that
// is not a contract lives here, because a *_contract_test.go file declares nothing
// but TestContract* functions (guard G5, internal/engineguard).
//
// The engine-free core (the gate, the run id, the label, the version check, the
// banner) is internal/testutil/enginecontract, shared with the other packages
// that will have contracts. What speaks to the engine stays in this package, in
// _test.go files, so the guards on engine launches, destructive verbs and pulls
// (G2, G3, G8) have nothing to report in the shared package.

// contract is what a contract test holds: its test, the session the run opened,
// the recorder every call of the contract goes through, and the resources the run
// owns (OC-09a2, DEVDESK-943).
type contract struct {
	t testing.TB
	s *enginecontract.Session
	// rec is the recording runner of the contract; nil until startContract (or
	// sibling) installs it.
	rec *recordingRunner
	// ours maps the id and the name of each resource of this run to its role. It
	// is what the argv rule calls "ours": a destructive argv may only name these.
	ours map[string]string
	// tried is set the moment a creation is attempted, before the engine answers:
	// a call that failed may still have made the resource, so the cleanup looks.
	tried bool
	// sleep is the pause waitHealthy takes between two reads: time.Sleep, which a test
	// replaces so it does not wait.
	sleep func(time.Duration)
	// publicIDs are the ids of 64 hexadecimal characters a fixture may keep (public).
	publicIDs []string
	// imageUsers maps the id of an image usedImage returned to the containers of this
	// run made from it: expectRmiRefusal sends an rmi only for an image listed here.
	imageUsers map[string][]string
}

// requireEngine is the first statement of every contract test (guard G5). It
// applies the gate, so without DEVDESK_ENGINE_CONTRACT=1 the test is skipped and
// with CI set it fails, both before the engine is reached; probes the engine the
// run is about; then starts the contract: the engine is pinned, every call goes
// through the recorder and its argv rule, and the cleanup of the run is registered.
func requireEngine(t *testing.T) *contract {
	s := enginecontract.Require(t, probeEngine)
	return startContract(t, s)
}

// newContract builds a contract for a session with no engine pinned, no runner
// swapped and nothing registered: the unit tests of the helpers use it.
func newContract(t testing.TB, s *enginecontract.Session) *contract {
	return &contract{t: t, s: s, ours: map[string]string{}, sleep: time.Sleep, imageUsers: map[string][]string{}}
}

// startContract is what requireEngine does once Require accepted the run. It pins
// the engine of the session, installs the recorder of the contract as the runner,
// and registers on t, in this order, "put the runner and the engine back" and then
// the cleanup of the run. t.Cleanup is last in, first out: the cleanup runs first,
// whether the test passed, failed or panicked, and still has the recorder.
func startContract(t testing.TB, s *enginecontract.Session) *contract {
	previousRunner, previousEngine := runner, engine.Current()
	engine.SetCurrent(engine.ShapeFor(string(s.Engine.Tool)))
	c := newContract(t, s)
	c.rec = newRecordingRunner(t, contractArgvRule(s.RunID, c.isOurs))
	runner = c.rec
	t.Cleanup(func() {
		runner = previousRunner
		engine.SetCurrent(previousEngine)
	})
	t.Cleanup(c.cleanup)
	return c
}

func (c *contract) isOurs(ref string) bool {
	_, ok := c.ours[ref]
	return ok
}

// recording makes the recorder of the contract the runner until the returned
// function is called. For the contract the test started this changes nothing; a
// sibling, which has a recorder and a rule of its own, takes the runner for the
// length of one call and gives it back.
func (c *contract) recording() func() {
	if c.rec == nil {
		return func() {}
	}
	previous := runner
	runner = c.rec
	return func() { runner = previous }
}

// requireLab ends the test unless the run is in a test VM (see enginecontract.CheckLab).
// Whatever creates or removes a resource asks it first; a read does not.
func (c *contract) requireLab() {
	c.t.Helper()
	c.s.RequireLab(c.t)
}

// sibling is a second contract for the run id "<run>-<suffix>", with its own
// recorder, its own rule and its own resources. Only the self-check of the cleanup
// uses it, to own a resource that carries the label of another run. It registers
// its own cleanup.
func (c *contract) sibling(suffix string) *contract {
	s := *c.s
	s.RunID = c.s.RunID + "-" + suffix
	sib := newContract(c.t, &s)
	sib.rec = newRecordingRunner(c.t, contractArgvRule(s.RunID, sib.isOurs))
	c.t.Cleanup(sib.cleanup)
	return sib
}

// container starts a container of this run, `sleep 600` unless cmd says otherwise,
// from the pinned image of DEVDESK_ENGINE_CONTRACT_IMAGE (never pulled here), and
// returns its id.
func (c *contract) container(role string, cmd ...string) string {
	c.t.Helper()
	return c.containerWith(role, nil, cmd...)
}

// containerWith is container with options between the name and the image (a volume,
// a network, a healthcheck): the rule judges them like any other.
func (c *contract) containerWith(role string, options []string, cmd ...string) string {
	c.t.Helper()
	c.requireLab()
	image := c.pinnedImage()
	if len(cmd) == 0 {
		cmd = []string{"sleep", "600"}
	}
	name := enginecontract.ResourceName(c.s.RunID, role)
	args := []string{"run", "-d", "--pull=never", "--label", enginecontract.Label(c.s.RunID), "--name", name}
	args = append(args, options...)
	args = append(args, image)
	return c.create("container", role, name, IsContainerID, append(args, cmd...)...)
}

// pinnedImage is the image of the contracts, read from DEVDESK_ENGINE_CONTRACT_IMAGE:
// a repository pinned by digest, or the contract ends.
func (c *contract) pinnedImage() string {
	c.t.Helper()
	image := os.Getenv(enginecontract.ImageEnv)
	if image == "" {
		c.t.Fatalf("%s is empty: contract.sh sets it from CONTRACT_IMAGE in tools/engine-vms/versions.env, "+
			"and a contract never pulls an image", enginecontract.ImageEnv)
	}
	if !enginecontract.ValidImage(image) {
		c.t.Fatalf("%s=%q is not a repository pinned by digest (repository@sha256:<64 hex>): a tag moves, and a contract never pulls",
			enginecontract.ImageEnv, image)
	}
	return image
}

// volume creates a volume of this run and returns its name.
func (c *contract) volume(role string) string {
	c.t.Helper()
	c.requireLab()
	name := enginecontract.ResourceName(c.s.RunID, role)
	return c.create("volume", role, name, IsVolumeName, "volume", "create", "--label", enginecontract.Label(c.s.RunID), name)
}

// network creates a network of this run and returns what the engine printed: an
// id for docker, the name for podman.
func (c *contract) network(role string) string {
	c.t.Helper()
	c.requireLab()
	name := enginecontract.ResourceName(c.s.RunID, role)
	return c.create("network", role, name, IsNetworkRef, "network", "create", "--label", enginecontract.Label(c.s.RunID), name)
}

// create runs one creation, registers what it made by the reference the engine
// printed and by name, and ends the test when the engine fails or prints something
// that is not a reference.
func (c *contract) create(kind, role, name string, valid func(string) bool, args ...string) string {
	c.t.Helper()
	return c.createHinted("", kind, role, name, valid, args...)
}

// createHinted is create with a sentence added to the report of a failure, for a
// creation whose failure has a likely cause the engine does not say (a port in use).
func (c *contract) createHinted(hint, kind, role, name string, valid func(string) bool, args ...string) string {
	c.t.Helper()
	defer c.recording()()
	c.tried = true
	out, err := dockerOutput(args...)
	ref := strings.TrimSpace(string(out))
	if err != nil {
		if hint != "" {
			hint = ": " + hint
		}
		c.t.Fatalf("creating the %s %s failed: %v: %s%s", kind, name, err, failureDetail(out, err), hint)
	}
	if !valid(ref) {
		c.t.Fatalf("creating the %s %s printed %q, want its reference", kind, name, ref)
	}
	c.ours[ref] = role
	c.ours[name] = role
	return ref
}

// failureDetail is what the engine said about a failure: its stdout when it wrote
// any, and its stderr when the error carries it.
func failureDetail(out []byte, err error) string {
	var parts []string
	if s := strings.TrimSpace(string(out)); s != "" {
		parts = append(parts, s)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if s := strings.TrimSpace(string(exit.Stderr)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ": ")
}

// exchange runs call, which must make exactly one engine exchange, and returns it
// as it happened (the contract asserts on the real ids). A fixture is one exchange:
// none (the call never reached the engine, or the rule refused it) and two or more
// are both a failure of the contract. The exchange is then redacted and, in a
// recording session, written as a fixture of campaign (Session.Record: the output
// directory of the VM, after the leak check); in a verifying one it is compared to
// the fixture recorded for the engine (Session.Verify), and with none recorded the
// contract goes on, so its own assertions after this call still run.
func (c *contract) exchange(campaign, scenario, text string, call func()) enginefixture.Exchange {
	c.t.Helper()
	return c.exchangeWith(nil, campaign, scenario, text, call)
}

// exchangeWith is exchange with a Comparer for the streams of the exchange when
// verifying: for an output that carries a value that moves with the clock (a
// duration, a timestamp, a health state). The argv and the exit code are compared
// exactly whatever the Comparer says; recording ignores it.
func (c *contract) exchangeWith(cmp enginecontract.Comparer, campaign, scenario, text string, call func()) enginefixture.Exchange {
	c.t.Helper()
	if c.rec == nil {
		c.t.Fatalf("%s/%s: the contract has no recorder: start it with requireEngine", campaign, scenario)
	}
	defer c.recording()()
	before := c.rec.count()
	call()
	window := c.rec.since(before)
	if len(window) != 1 {
		c.t.Fatalf("%s/%s (%s): %d engine exchanges in the window, want exactly 1: a fixture is one exchange",
			campaign, scenario, text, len(window))
	}
	e := window[0]
	c.t.Logf("%s/%s: %q exit %d", campaign, scenario, e.Argv, e.Exit)
	raw := enginefixture.Exchange{Argv: e.Argv, Exit: e.Exit, Stdout: e.Stdout, Stderr: e.Stderr}
	if c.s.Mode == enginecontract.Record {
		c.s.Record(c.t, campaign, scenario, text, raw, c.redaction())
	} else {
		c.s.Verify(c.t, campaign, scenario, raw, c.redaction(), cmp)
	}
	return raw
}

// fullID is the id of a container or a network as the engine prints it whole.
var fullID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// sentinelRole is the role the id of the sentinel container of contract.sh gets in a
// fixture: the one foreign resource a listing shows (its name, ddc-sentinel-<run>,
// loses the run id like any name).
const sentinelRole = "sentinel"

// redaction is what Redact must replace in this run's exchanges: the ids of the
// resources the run made, each with its role, the id of the sentinel (the
// SentinelEnv variable of contract.sh) as the role "sentinel", the run id, and the home, the user
// and the host name of the machine that records (a lookup that fails leaves its
// field empty, and the leak check is the net for what that lets through). The ids
// public() declared are kept as they are.
func (c *contract) redaction() enginefixture.Redaction {
	r := enginefixture.Redaction{RunID: c.s.RunID, IDs: map[string]string{}, Public: slices.Clone(c.publicIDs)}
	for ref, role := range c.ours {
		if fullID.MatchString(ref) {
			r.IDs[ref] = role
		}
	}
	id := os.Getenv(enginecontract.SentinelEnv)
	if id == "" && c.s.Mode == enginecontract.Record {
		c.t.Fatalf("recording without %s: contract.sh sets it from the sentinel, and a recorded list would hold a container "+
			"whose id nobody redacted (a recording is run by `mise run engine-contract:record` only)", enginecontract.SentinelEnv)
	}
	if id != "" {
		if !fullID.MatchString(id) {
			c.t.Fatalf("%s=%q is not an id of 64 lower-case hexadecimal characters: contract.sh sets it from the sentinel it started",
				enginecontract.SentinelEnv, id)
		}
		r.IDs[id] = sentinelRole
	}
	r.Home, _ = os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		r.User = u.Username
	}
	r.Host, _ = os.Hostname()
	return r
}

// contractKind is one kind of resource the cleanup handles: how to list the ones
// that carry a label, how to read the label of one, how to remove it.
type contractKind struct {
	name    string
	list    []string
	inspect func(ref string) []string
	remove  func(ref string) error
}

// contractKinds are the kinds in the order of removal: a container goes before the
// network and the volume it used. The list goes through the filter of the engine,
// and the label of each candidate is read again, because a filter that is wrong or
// ignored must not decide what is removed.
func contractKinds(run string) []contractKind {
	filter := "label=" + enginecontract.Label(run)
	format := `{{index .Labels "` + enginecontract.LabelKey + `"}}`
	return []contractKind{
		{
			name: "container",
			list: []string{"ps", "-a", "-q", "--no-trunc", "--filter", filter},
			inspect: func(ref string) []string {
				return []string{"inspect", "--type", "container", "--format", `{{index .Config.Labels "` + enginecontract.LabelKey + `"}}`, ref}
			},
			remove: func(ref string) error { return RemoveContainer(ref, true) },
		},
		{
			name:    "network",
			list:    []string{"network", "ls", "-q", "--no-trunc", "--filter", filter},
			inspect: func(ref string) []string { return []string{"network", "inspect", "--format", format, ref} },
			remove:  RemoveNetwork,
		},
		{
			name:    "volume",
			list:    []string{"volume", "ls", "-q", "--filter", filter},
			inspect: func(ref string) []string { return []string{"volume", "inspect", "--format", format, ref} },
			remove:  RemoveVolume,
		},
	}
}

// cleanup is registered by startContract. It does nothing, and asks the engine
// nothing, unless a creation was attempted: a read-only contract leaves the engine
// as it found it, and needs no lab mark.
func (c *contract) cleanup() {
	if c.tried {
		c.cleanupNow()
	}
}

// cleanupNow removes what carries the label of this run, and only that, and returns
// the references it removed: containers, then networks, then volumes. For each kind
// it lists by the filter of the engine, reads the label of every candidate, removes
// the ones whose label is exactly the run id through the Remove* functions (which
// validate the identifier, guard G3), then lists again if it removed anything. A
// candidate with another or no label is reported and left alone; a listing or an
// inspect that fails removes nothing, is reported, and the cleanup goes on.
func (c *contract) cleanupNow() []string {
	c.t.Helper()
	c.requireLab()
	if !enginecontract.ValidRunID(c.s.RunID) {
		c.t.Errorf("cleanup refused to list: %q is not a valid run id, and a filter built from it could match other runs", c.s.RunID)
		return nil
	}
	defer c.recording()()
	var removed []string
	for _, k := range contractKinds(c.s.RunID) {
		removed = append(removed, c.cleanupKind(k)...)
	}
	return removed
}

func (c *contract) cleanupKind(k contractKind) []string {
	c.t.Helper()
	candidates, ok := c.listRefs(k)
	if !ok {
		return nil
	}
	var verified []string
	for _, ref := range candidates {
		out, err := dockerOutput(k.inspect(ref)...)
		switch label := labelValue(string(out)); {
		case err != nil:
			c.t.Errorf("cleanup: cannot read the label of the %s %s, left alone: %v: %s", k.name, ref, err, failureDetail(out, err))
		case label == "":
			c.t.Errorf("cleanup: the %s %s was listed under %s but has no label, left alone", k.name, ref, enginecontract.Label(c.s.RunID))
		case label != c.s.RunID:
			c.t.Errorf("cleanup: the %s %s was listed under %s but carries the label %q, left alone",
				k.name, ref, enginecontract.Label(c.s.RunID), label)
		default:
			verified = append(verified, ref)
		}
	}
	var removed []string
	for _, ref := range verified {
		if _, known := c.ours[ref]; !known {
			c.ours[ref] = "found"
		}
		if err := k.remove(ref); err != nil {
			c.t.Errorf("cleanup: removing the %s %s: %v", k.name, ref, err)
			continue
		}
		removed = append(removed, ref)
	}
	if len(verified) > 0 {
		left, _ := c.listRefs(k)
		for _, ref := range left {
			if slices.Contains(verified, ref) {
				c.t.Errorf("cleanup: the %s %s is still there after its removal", k.name, ref)
			}
		}
	}
	return removed
}

// listRefs lists the references of a kind under the label filter of the run; ok is
// false, after a report, when the listing failed.
func (c *contract) listRefs(k contractKind) ([]string, bool) {
	out, err := dockerOutput(k.list...)
	if err != nil {
		c.t.Errorf("cleanup: listing the %ss of run %s failed: %v: %s", k.name, c.s.RunID, err, failureDetail(out, err))
		return nil, false
	}
	return splitLines(out), true
}

// errNotContracted is what the recorder returns for a call the contract may not
// make; the refusal is also reported to the test, so it cannot go unnoticed.
var errNotContracted = errors.New("call refused: not allowed in a contract")

// exchangeLog is one exchange the recorder saw: what was asked, how it was run,
// and what the engine answered, stdout and stderr apart.
type exchangeLog struct {
	Argv     []string
	Combined bool
	Exit     int
	Stdout   []byte
	Stderr   []byte
}

// recordingRunner is the dockerRunner of a contract. It asks the argv rule before
// anything starts, refuses what a contract cannot record (a credential helper,
// standard input, a context, an interactive command), runs the rest through
// cliRunner, the one runner that reaches the engine (guard G1), and keeps what the
// engine answered. Limit: with Combined, stdout then stderr are returned, not the
// interleaving a terminal would show; the refusals measured write on stderr only.
type recordingRunner struct {
	t     testing.TB
	allow func(argv []string) error
	mu    sync.Mutex
	log   []exchangeLog
}

func newRecordingRunner(t testing.TB, allow func(argv []string) error) *recordingRunner {
	return &recordingRunner{t: t, allow: allow}
}

func (r *recordingRunner) LookPath() error { return cliRunner{}.LookPath() }

// Build is for commands handed to a terminal; a contract has none.
func (r *recordingRunner) Build(args ...string) *exec.Cmd {
	r.t.Helper()
	r.t.Fatalf("a contract never builds an interactive command: %q", args)
	return nil
}

func (r *recordingRunner) Run(dc dockerCmd) ([]byte, error) {
	r.t.Helper()
	if err := r.refusal(dc); err != nil {
		r.t.Errorf("%v", err)
		return nil, err
	}
	cmd := cliRunner{}.Build(dc.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var exit *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exit) {
		return nil, runErr // the process never ran: not an exchange
	}
	entry := exchangeLog{Argv: slices.Clone(dc.Args), Combined: dc.Combined, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if exit != nil {
		entry.Exit = exit.ExitCode()
	}
	r.mu.Lock()
	r.log = append(r.log, entry)
	r.mu.Unlock()
	if dc.Combined {
		// Like CombinedOutput: both streams in the bytes, nothing in ExitError.Stderr.
		return append(slices.Clone(entry.Stdout), entry.Stderr...), runErr
	}
	if exit != nil {
		exit.Stderr = entry.Stderr // like Output
	}
	return entry.Stdout, runErr
}

// refusal says why a call cannot be made in a contract, wrapping errNotContracted.
func (r *recordingRunner) refusal(dc dockerCmd) error {
	switch {
	case dc.Helper != "":
		return fmt.Errorf("%w: a credential helper (%q) is not contracted", errNotContracted, dc.Helper)
	case dc.Stdin != "":
		return fmt.Errorf("%w: a call with stdin is not contracted", errNotContracted)
	case dc.Ctx != nil:
		return fmt.Errorf("%w: a call with a context is not contracted", errNotContracted)
	}
	if err := r.allow(dc.Args); err != nil {
		return fmt.Errorf("%w: %q: %w", errNotContracted, dc.Args, err)
	}
	return nil
}

// count is the number of exchanges journaled so far; since returns those after n.
func (r *recordingRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log)
}

func (r *recordingRunner) since(n int) []exchangeLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log[min(n, len(r.log)):])
}

// probeEngine asks the engine of tool what it is. It pins that engine only for
// its own length, so the functions under contract and the probe agree on the
// binary, and it asks two questions of the engine itself rather than infer
// anything from the binary's name or path. Only stdout is parsed: a warning on
// stderr must not change the answer.
//
// The templates are those of plan OC-09a, section 3.4; they are still to be
// confirmed on the test VMs, for N and N-1 of both engines. A template an
// engine does not accept is an error here, never a guess.
func probeEngine(tool enginefixture.Tool) (enginecontract.Engine, error) {
	previous := engine.Current()
	defer engine.SetCurrent(previous)
	engine.SetCurrent(engine.ShapeFor(string(tool)))

	versionArgs, modeArgs := probeArgs(tool)
	out, err := dockerOutput(versionArgs...)
	if err != nil {
		return enginecontract.Engine{}, probeFailure(tool, "version", err)
	}
	version, err := parseEngineVersion(tool, string(out))
	if err != nil {
		return enginecontract.Engine{}, err
	}
	out, err = dockerOutput(modeArgs...)
	if err != nil {
		return enginecontract.Engine{}, probeFailure(tool, "info", err)
	}
	mode, err := parseEngineMode(tool, string(out))
	if err != nil {
		return enginecontract.Engine{}, err
	}
	return enginecontract.Engine{Tool: tool, Version: version, Mode: mode}, nil
}

// probeArgs are the two questions the probe asks: the version (docker: client
// and server, which must agree; podman: the client's) and whether the engine
// runs rootless.
func probeArgs(tool enginefixture.Tool) (version, mode []string) {
	if tool == enginefixture.Docker {
		return []string{"version", "--format", "{{.Client.Version}} {{.Server.Version}}"},
			[]string{"info", "--format", "{{json .SecurityOptions}}"}
	}
	return []string{"version", "--format", "{{.Client.Version}}"},
		[]string{"info", "--format", "{{.Host.Security.Rootless}}"}
}

// probeFailure names the engine, the question and, when the engine said why,
// its own diagnostic.
func probeFailure(tool enginefixture.Tool, question string, err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if detail := strings.TrimSpace(string(exit.Stderr)); detail != "" {
			return fmt.Errorf("%s %s: %w: %s", tool, question, err, detail)
		}
	}
	return fmt.Errorf("%s %s: %w", tool, question, err)
}

// parseEngineVersion reads the answer to the version question: docker's client
// and server versions, which must be equal, or podman's single version.
func parseEngineVersion(tool enginefixture.Tool, out string) (string, error) {
	fields := strings.Fields(out)
	if tool == enginefixture.Docker {
		if len(fields) != 2 {
			return "", fmt.Errorf("docker version output %q: want the client and the server version", out)
		}
		if fields[0] != fields[1] {
			return "", fmt.Errorf("docker client and server differ: %s and %s", fields[0], fields[1])
		}
		return fields[0], nil
	}
	if len(fields) != 1 {
		return "", fmt.Errorf("%s version output %q: want one version", tool, out)
	}
	return fields[0], nil
}

// parseEngineMode reads the answer to the mode question: docker lists
// name=rootless among its security options when it runs rootless; podman says
// true or false.
func parseEngineMode(tool enginefixture.Tool, out string) (enginefixture.Mode, error) {
	out = strings.TrimSpace(out)
	if tool == enginefixture.Docker {
		if strings.Contains(out, "name=rootless") {
			return enginefixture.Rootless, nil
		}
		return enginefixture.Rootful, nil
	}
	switch out {
	case "true":
		return enginefixture.Rootless, nil
	case "false":
		return enginefixture.Rootful, nil
	}
	return "", fmt.Errorf("%s info output %q: want true or false", tool, out)
}

// contractArgvRule is the allow list of what a contract may ask the engine. It is
// pure: runID and the ours callback (what the contract created, or listed and
// verified) are all it knows. A verb or an option it does not know is refused, so a
// new one is added here, with its reason, rather than worked around.
//
//   - Reads are allowed on anything: the cleanup inspects candidates it does not
//     own yet, to find out whose they are.
//   - A creation is checked option by option against an allow list, up to the image
//     (the words after it are the container's command and are not read):
//     run and create take -d/--detach, --rm, --name (which must start with
//     ddc-<run>-), --label (one must be exactly the label of the run), --pull
//     (every one never, at least one), -e/--env KEY=value, --network (the default
//     bridge, none, or a network of this run), -v/--volume (a named volume of this run,
//     never a path), --entrypoint, -u/--user, -w/--workdir and --health-cmd,
//     --health-interval, --health-timeout and --health-retries (a command run inside
//     the container, nothing of the host). Everything else is
//     refused, and the ones that would reach the host name their reason:
//     --privileged, --replace, --mount, --cap-add, --device, --security-opt, --pid,
//     --ipc, --uts, --userns, --volumes-from, --env-file, and -p/--publish (a port of
//     the host is published only inside a launch window, see launchWindowRule).
//     volume create and network create take only --label and one name that starts with
//     ddc-<run>-. pod create is refused: no cleanup of pods exists.
//   - Destruction and state changes (rm, kill, stop, start, restart, pause,
//     unpause, volume rm, network rm, network connect and disconnect) may name only
//     resources of this run, by exact reference, and take only the options of the
//     verb: rm -f, -v, -t; stop and kill -t and -s; restart -t; volume rm, network rm,
//     start, pause, unpause, connect and disconnect none (so no --force, --all,
//     --latest, --filter, --depend or --cidfile). healthcheck run <container of the
//     run> (podman: its health state does not move by itself) takes no option either.
//   - Refused as a whole: every prune (a prune acts on the whole store), system,
//     pull, push, load, import, build, commit, tag, login, logout, exec, cp, save,
//     export, and rmi (an image is never made by a contract, contract.sh pulls the
//     one it uses). The windows below are the only exceptions, each for one argv, one
//     call, and a condition a reader can check: pruneWindowRule (expectPrune),
//     launchWindowRule (expectRefusal, portHolder) and rmiWindowRule
//     (expectRmiRefusal). A window never widens this rule: it only names the argv it
//     grants, and the rule keeps judging everything else.
//   - The options that point the CLI at another engine or store (--url, --connection,
//     --remote, --root, --runroot, --storage-driver, --host, -H, --context) are
//     refused at any position of the argv.
//
// Limit: the options of the other verbs (a read) are not checked, and an option
// value that is a secret is not recognised: a contract passes none.
func contractArgvRule(runID string, ours func(ref string) bool) func(argv []string) error {
	return func(argv []string) error {
		if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
			return errUnknownVerb(argv)
		}
		for _, a := range argv[1:] {
			if g := globalOption(a); g != "" {
				return fmt.Errorf("%s is refused at any position: it would send the call to another engine or store; "+
					"contractArgvRule never allows it", a)
			}
		}
		verb, rest := argv[0], argv[1:]
		switch verb {
		case "container", "image", "volume", "network", "pod", "system":
			return checkGroupArgv(runID, ours, verb, rest)
		}
		return checkVerbArgv(runID, ours, verb, rest)
	}
}

// globalEngineOptions point the CLI at another engine, connection, store or root.
var globalEngineOptions = []string{"--url", "--connection", "--remote", "--root", "--runroot", "--storage-driver", "--host", "-H", "--context"}

// globalOption returns the option of globalEngineOptions that arg is, as `--host
// x`, `--host=x` or `-Hx`, or "".
func globalOption(arg string) string {
	for _, g := range globalEngineOptions {
		if arg == g || strings.HasPrefix(arg, g+"=") || (g == "-H" && strings.HasPrefix(arg, g)) {
			return g
		}
	}
	return ""
}

// errUnknownVerb is the refusal of an argv the rule has no entry for.
func errUnknownVerb(argv []string) error {
	return fmt.Errorf("unknown verb in %q: contractArgvRule (internal/docker/contract_support_test.go) is an allow list, "+
		"add the verb there with the reason it is safe on the resources of a run; a global option before the verb "+
		"(--host, -H, --context, --connection) is refused too, it would send the call to another engine", argv)
}

// refusedVerbs are refused wherever they appear, at the top or after a group.
var refusedVerbs = map[string]string{
	"prune":  "a prune acts on the whole store, not on the resources of a run",
	"pull":   "a contract never pulls: contract.sh pulls the one image it uses, by digest",
	"push":   "a contract never pushes an image",
	"load":   "a contract never loads an image",
	"import": "a contract never imports an image",
	"save":   "a contract never saves an image",
	"export": "a contract never exports a container",
	"build":  "a contract never builds an image",
	"commit": "a contract never makes an image",
	"tag":    "a contract never tags an image",
	"login":  "a contract never logs in to a registry",
	"logout": "a contract never logs out of a registry",
	"exec":   "a contract never runs a command in a container",
	"cp":     "a contract never copies files in or out of a container",
}

// containerVerbs are the verbs allowed after `container`, which also stand alone.
var containerVerbs = []string{"inspect", "logs", "port", "top", "run", "create",
	"rm", "kill", "stop", "start", "restart", "pause", "unpause"}

func refusedVerb(words ...string) error {
	return fmt.Errorf("%s is refused: %s", strings.Join(words, " "), refusedVerbs[words[len(words)-1]])
}

// checkGroupArgv handles `<group> <subcommand> ...`.
func checkGroupArgv(runID string, ours func(string) bool, group string, rest []string) error {
	if group == "system" {
		return fmt.Errorf("system %s is refused: a system verb acts on the whole engine", strings.Join(rest, " "))
	}
	if len(rest) == 0 {
		return errUnknownVerb([]string{group})
	}
	sub, args := rest[0], rest[1:]
	if _, ok := refusedVerbs[sub]; ok {
		return refusedVerb(group, sub)
	}
	switch group {
	case "container":
		if sub == "ls" {
			return nil
		}
		if slices.Contains(containerVerbs, sub) {
			return checkVerbArgv(runID, ours, sub, args)
		}
	case "image":
		switch sub {
		case "ls", "inspect":
			return nil
		case "rm", "rmi":
			return errImagesAreNeverOurs(args)
		}
	case "volume":
		switch sub {
		case "ls", "inspect":
			return nil
		case "create":
			return checkNamedCreateArgv(runID, "volume create", args)
		case "rm":
			return checkTargetsArgv(ours, "volume rm", args, false)
		}
	case "network":
		switch sub {
		case "ls", "inspect":
			return nil
		case "create":
			return checkNamedCreateArgv(runID, "network create", args)
		case "rm", "connect", "disconnect":
			return checkTargetsArgv(ours, "network "+sub, args, sub != "rm")
		}
	case "pod":
		if sub == "create" {
			return errors.New("pod create is refused: no cleanup of pods exists, so a pod a contract made would stay in the store")
		}
	}
	return errUnknownVerb(append([]string{group, sub}, args...))
}

// checkVerbArgv handles a verb that stands alone.
func checkVerbArgv(runID string, ours func(string) bool, verb string, args []string) error {
	if _, ok := refusedVerbs[verb]; ok {
		return refusedVerb(verb)
	}
	switch verb {
	case "version", "info", "ps", "inspect", "logs", "port", "top", "images":
		return nil
	case "stats":
		if !slices.Contains(args, "--no-stream") {
			return errors.New("stats without --no-stream never ends")
		}
		return nil
	case "run", "create":
		return checkRunArgv(runID, ours, verb, args)
	case "rm", "kill", "stop", "start", "restart", "pause", "unpause":
		return checkTargetsArgv(ours, verb, args, false)
	case "rmi":
		return errImagesAreNeverOurs(args)
	case "healthcheck":
		// podman only: runs the check of a container once, which moves its health state.
		if len(args) > 0 && args[0] == "run" {
			return checkTargetsArgv(ours, "healthcheck run", args[1:], false)
		}
	}
	return errUnknownVerb(append([]string{verb}, args...))
}

func errImagesAreNeverOurs(args []string) error {
	return fmt.Errorf("rmi %s is refused: a contract never makes an image, contract.sh pulls the one it uses", strings.Join(args, " "))
}

// optSpec is the options a verb accepts: the ones without a value and the ones
// with one, spelled as they appear (`-f`, `--force`).
type optSpec struct {
	bools  []string
	valued []string
}

// option is one parsed option; value is "" for an option without one.
type option struct{ name, value string }

// dangerousOptions are refused with their reason; any other option that is not in
// the spec is refused as not listed.
var dangerousOptions = map[string]string{
	"--privileged":   "it gives the container the capabilities and devices of the host",
	"--replace":      "it removes an existing container of the same name, which may not be of this run",
	"--mount":        "it can bind a path of the host, the engine socket included",
	"--cap-add":      "it widens the capabilities of the container",
	"--device":       "it gives the container a device of the host",
	"--security-opt": "it relaxes the confinement of the container",
	"--pid":          "it can join a namespace of the host",
	"--ipc":          "it can join a namespace of the host",
	"--uts":          "it can join a namespace of the host",
	"--userns":       "it can join a namespace of the host",
	"--volumes-from": "it mounts what another container mounts",
	"--env-file":     "it reads a file of the host",
	"-p":             "it publishes a port of the host, which only a launch window may do, for one fixed loopback port",
	"--publish":      "it publishes a port of the host, which only a launch window may do, for one fixed loopback port",
	"--all":          "it picks its targets where the rule cannot see them",
	"--latest":       "it picks its targets where the rule cannot see them",
	"--filter":       "it picks its targets where the rule cannot see them",
	"--cidfile":      "it reads its targets from a file",
	"--depend":       "it removes what depends on the target",
	"--force":        "",
	"-a":             "it picks its targets where the rule cannot see them",
	"-l":             "it picks its targets where the rule cannot see them",
}

// errOption is the refusal of an option a verb does not allow.
func errOption(verb, arg string) error {
	name, _, _ := strings.Cut(arg, "=")
	if reason := dangerousOptions[name]; reason != "" {
		return fmt.Errorf("%s %s is refused: %s (contractArgvRule)", verb, arg, reason)
	}
	return fmt.Errorf("%s option %s is not in the allow list of contractArgvRule "+
		"(internal/docker/contract_support_test.go): add it there with its reason", verb, arg)
}

// parseOptions splits args into the options spec allows and the positional words.
// With stopAtFirst, the first positional word ends the options and everything after
// it is returned as positional (run and create: the image, then its command).
func parseOptions(verb string, args []string, spec optSpec, stopAtFirst bool) ([]option, []string, error) {
	var opts []option
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			if stopAtFirst {
				return opts, args[i:], nil
			}
			positional = append(positional, a)
			continue
		}
		name, value, hasValue := a, "", false
		if strings.HasPrefix(a, "--") {
			name, value, hasValue = strings.Cut(a, "=")
		}
		switch {
		case slices.Contains(spec.valued, name):
			if !hasValue {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("%s option %s has no value", verb, a)
				}
				i++
				value = args[i]
			}
			opts = append(opts, option{name, value})
		case slices.Contains(spec.bools, name) && !hasValue:
			opts = append(opts, option{name: name})
		case !strings.HasPrefix(a, "--") && len(a) > 2 && groupOf(a, spec.bools):
			for _, c := range a[1:] {
				opts = append(opts, option{name: "-" + string(c)})
			}
		default:
			return nil, nil, errOption(verb, a)
		}
	}
	return opts, positional, nil
}

// groupOf reports whether arg is a group of short options without values, `-fv`.
func groupOf(arg string, bools []string) bool {
	for _, c := range arg[1:] {
		if !slices.Contains(bools, "-"+string(c)) {
			return false
		}
	}
	return true
}

var (
	runSpec = optSpec{
		bools: []string{"-d", "--detach", "--rm"},
		valued: []string{"--name", "--label", "-l", "--pull", "-e", "--env", "--network", "--net",
			"-v", "--volume", "--entrypoint", "-u", "--user", "-w", "--workdir",
			"--health-cmd", "--health-interval", "--health-timeout", "--health-retries"},
	}
	namedCreateSpec = optSpec{valued: []string{"--label"}}
	// destructiveSpecs are the options of each destructive verb. Nothing else: a
	// --force, --all or --depend changes what the verb reaches.
	destructiveSpecs = map[string]optSpec{
		"rm":                 {bools: []string{"-f", "--force", "-v", "--volumes"}, valued: []string{"-t", "--time"}},
		"stop":               {valued: []string{"-t", "--time", "-s", "--signal"}},
		"kill":               {valued: []string{"-t", "--time", "-s", "--signal"}},
		"restart":            {valued: []string{"-t", "--time"}},
		"start":              {},
		"pause":              {},
		"unpause":            {},
		"volume rm":          {},
		"network rm":         {},
		"network connect":    {},
		"network disconnect": {},
		"healthcheck run":    {},
	}
	// defaultNetworks are the networks of the engines a contract may join: no
	// network of the host.
	defaultNetworks = []string{"bridge", "none", "podman"}
)

// checkRunArgv checks a run or create: the options (see contractArgvRule) up to
// the image, and the image itself.
func checkRunArgv(runID string, ours func(string) bool, verb string, args []string) error {
	opts, positional, err := parseOptions(verb, args, runSpec, true)
	if err != nil {
		return err
	}
	prefix := enginecontract.NamePrefix + runID + "-"
	label, pulls, never := false, 0, 0
	for _, o := range opts {
		switch o.name {
		case "--name":
			if !strings.HasPrefix(o.value, prefix) {
				return fmt.Errorf("%s --name %s: a name must start with %s, so a name of another run cannot be reused", verb, o.value, prefix)
			}
		case "--label", "-l":
			label = label || o.value == enginecontract.Label(runID)
		case "--pull":
			pulls++
			if o.value == "never" {
				never++
			}
		case "-e", "--env":
			if key, _, ok := strings.Cut(o.value, "="); !ok || key == "" {
				return fmt.Errorf("%s %s %s: an environment variable needs an explicit KEY=value, a bare name would pass the host's", verb, o.name, o.value)
			}
		case "--network", "--net":
			if !slices.Contains(defaultNetworks, o.value) && !ours(o.value) {
				return fmt.Errorf("%s %s %s: a contract joins a default network or a network of this run, not the host's or another's", verb, o.name, o.value)
			}
		case "-v", "--volume":
			if err := checkVolumeArg(verb, o.value, ours); err != nil {
				return err
			}
		}
	}
	if !label {
		return fmt.Errorf("%s without the label %s: every resource of a contract carries the label of its run", verb, enginecontract.Label(runID))
	}
	if pulls == 0 || never != pulls {
		return fmt.Errorf("%s without --pull=never: a contract never pulls, contract.sh pulls the image", verb)
	}
	if len(positional) == 0 {
		return fmt.Errorf("%s names no image", verb)
	}
	return nil
}

// checkVolumeArg accepts `-v name:/path[:options]` where name is a volume of this
// run; a path (a bind mount, the engine socket included) and an anonymous volume
// are refused.
func checkVolumeArg(verb, value string, ours func(string) bool) error {
	source, _, ok := strings.Cut(value, ":")
	if !ok || !IsVolumeName(source) || !ours(source) {
		return fmt.Errorf("%s -v %s: a contract mounts only a named volume of its run, never a path of the host", verb, value)
	}
	return nil
}

// checkNamedCreateArgv checks a volume create or a network create: the label of the
// run, one name that starts with ddc-<run>-, and no other option.
func checkNamedCreateArgv(runID, verb string, args []string) error {
	opts, positional, err := parseOptions(verb, args, namedCreateSpec, false)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(opts, func(o option) bool { return o.value == enginecontract.Label(runID) }) {
		return fmt.Errorf("%s without the label %s: every resource of a contract carries the label of its run", verb, enginecontract.Label(runID))
	}
	prefix := enginecontract.NamePrefix + runID + "-"
	if len(positional) != 1 || !strings.HasPrefix(positional[0], prefix) {
		return fmt.Errorf("%s %q: want exactly one name, starting with %s", verb, positional, prefix)
	}
	return nil
}

// checkTargetsArgv requires that every target of a destructive verb be a resource
// of the run, and that its options be the ones of the verb. With lastOnly (network
// connect and disconnect) only the last positional, the container, is checked: the
// network may be a built-in one.
func checkTargetsArgv(ours func(string) bool, verb string, args []string, lastOnly bool) error {
	_, targets, err := parseOptions(verb, args, destructiveSpecs[verb], false)
	if err != nil {
		return err
	}
	if lastOnly && len(targets) > 0 {
		targets = targets[len(targets)-1:]
	}
	if len(targets) == 0 {
		return fmt.Errorf("%s names no target: a destructive verb must name resources of this run", verb)
	}
	for _, target := range targets {
		if !ours(target) {
			return fmt.Errorf("%s: %s is not a resource of this run, so a contract may not touch it", verb, target)
		}
	}
	return nil
}

// A window is the one way a contract sends an argv that contractArgvRule refuses, and
// it is narrow on every side (OC-09b1, DEVDESK-807). It names the argv it grants, it
// grants it once, it exists for the length of one call (withWindow puts the standard
// rule back when the call returns), and what the call does besides is judged by the
// standard rule as ever. The rule of the recorder is the allow list it always was: a
// window never makes it wider for the other calls of the contract.

// withWindow runs f with window as a second judge of the recorder: an argv the
// window grants goes through, anything else is judged by the standard rule alone. A
// refusal names both, so a report says why neither accepted the argv.
func (c *contract) withWindow(window func(argv []string) error, f func()) {
	c.t.Helper()
	if c.rec == nil {
		c.t.Fatalf("the contract has no recorder: start it with requireEngine")
	}
	standard := c.rec.allow
	c.rec.allow = func(argv []string) error {
		windowErr := window(argv)
		if windowErr == nil {
			return nil
		}
		standardErr := standard(argv)
		if standardErr == nil {
			return nil
		}
		return fmt.Errorf("%w (the window of this call refused it too: %v)", standardErr, windowErr)
	}
	defer func() { c.rec.allow = standard }()
	f()
}

// builtinNetworks are the networks the engines make themselves: a store "of this run"
// is allowed to hold them.
var builtinNetworks = []string{"bridge", "host", "none", "podman"}

// pruneListArgv is the read of the whole store of a kind, with no filter, that
// expectPrune makes right before a prune; nil for a kind that has no prune window.
// Only container, network and volume have one: an image prune and a system prune
// reach what no contract made, and no function of DevDesk prunes with a filter.
func pruneListArgv(kind string) []string {
	switch kind {
	case "container":
		return []string{"ps", "-a", "-q", "--no-trunc"}
	case "network":
		return []string{"network", "ls", "--format", "{{.Name}}"}
	case "volume":
		return []string{"volume", "ls", "-q"}
	}
	return nil
}

// pruneWindowRule grants the one argv `<kind> prune -f`, once: what PruneContainers,
// PruneNetworks and PruneVolumes send. Never -a or --all, never a filter, never
// `system prune` or an image prune, never a kind it does not know. An argv it refuses
// does not use its attempt.
func pruneWindowRule(kind string) func(argv []string) error {
	granted := false
	return func(argv []string) error {
		if pruneListArgv(kind) == nil {
			return fmt.Errorf("there is no prune window for %q: only container, network and volume have one", kind)
		}
		if granted {
			return errors.New("the prune window grants one attempt and it was used")
		}
		if !slices.Equal(argv, []string{kind, "prune", "-f"}) {
			return fmt.Errorf("the prune window grants %q and nothing else, not %q", kind+" prune -f", argv)
		}
		granted = true
		return nil
	}
}

// expectPrune runs call, which must make exactly one prune of kind, in a window that
// allows `<kind> prune -f` once, and returns the exchange as exchange does. A prune
// acts on the whole store, so it is allowed only when the whole store of that kind
// holds nothing but resources of this run, read right before the call: a foreign
// resource, a listing that fails or a kind with no window ends the contract before
// the engine is asked to prune, and says which resource it saw. A lab VM after a
// reset holds the sentinel container, so a container prune never passes there; a
// network prune is allowed the networks the engines make themselves.
func (c *contract) expectPrune(campaign, scenario, text, kind string, call func()) enginefixture.Exchange {
	c.t.Helper()
	c.requireLab()
	list := pruneListArgv(kind)
	if list == nil {
		c.t.Fatalf("%s/%s: no prune window for %q: only container, network and volume have one", campaign, scenario, kind)
	}
	c.requireOnlyThisRun(kind, list)
	var ex enginefixture.Exchange
	c.withWindow(pruneWindowRule(kind), func() { ex = c.exchange(campaign, scenario, text, call) })
	return ex
}

// requireOnlyThisRun ends the contract unless list, the read of a whole store, names
// only resources of this run (and the networks the engines make).
func (c *contract) requireOnlyThisRun(kind string, list []string) {
	c.t.Helper()
	refs, out, err := c.readStore(list)
	if err != nil {
		c.t.Fatalf("cannot read the %s store before a prune, so nothing is pruned: %v: %s", kind, err, failureDetail(out, err))
	}
	if foreign := c.foreignRefs(kind, refs, false); len(foreign) > 0 {
		c.t.Fatalf("the %s store holds %s, which is not a resource of this run: a prune would remove it, so nothing is pruned", kind, foreign[0])
	}
}

// readStore reads a whole store (a list with no filter) through the recorder.
func (c *contract) readStore(list []string) (refs []string, out []byte, err error) {
	c.t.Helper()
	defer c.recording()()
	out, err = dockerOutput(list...)
	if err != nil {
		return nil, out, err
	}
	return splitLines(out), out, nil
}

// foreignRefs are the references of a store that are not resources of this run: not the
// networks the engines make, and, with allowSentinel, not the sentinel of contract.sh.
func (c *contract) foreignRefs(kind string, refs []string, allowSentinel bool) []string {
	sentinel := os.Getenv(enginecontract.SentinelEnv)
	var foreign []string
	for _, ref := range refs {
		switch {
		case kind == "network" && slices.Contains(builtinNetworks, ref):
		case allowSentinel && sentinel != "" && ref == sentinel:
		case c.isOurs(ref):
		default:
			foreign = append(foreign, ref)
		}
	}
	return foreign
}

// exchangeListing is exchangeWith for a call whose output lists a whole store (a `ps`, a
// `volume ls`): a fixture of a list must hold this run and the sentinel and nothing a killed
// run left behind. Recording reads the store first (the same read as a prune) and ends the
// contract, before anything is recorded, when it cannot or when it holds another resource;
// verifying only logs what it found, since a VM that was not reset may hold more.
func (c *contract) exchangeListing(kind string, cmp enginecontract.Comparer, campaign, scenario, text string, call func()) enginefixture.Exchange {
	c.t.Helper()
	list := pruneListArgv(kind)
	if list == nil {
		c.t.Fatalf("%s/%s: %q is not a kind of store a listing is checked for", campaign, scenario, kind)
	}
	recording := c.s.Mode == enginecontract.Record
	refs, out, err := c.readStore(list)
	switch foreign := c.foreignRefs(kind, refs, true); {
	case err != nil && recording:
		c.t.Fatalf("%s/%s: cannot read the %s store before recording a list of it: %v: %s", campaign, scenario, kind, err, failureDetail(out, err))
	case err != nil:
		c.t.Logf("%s/%s: cannot read the %s store: %v", campaign, scenario, kind, err)
	case len(foreign) > 0 && recording:
		c.t.Fatalf("%s/%s: the %s store holds %s, which is neither of this run nor the sentinel: a fixture of a list must not hold "+
			"what a killed run left behind (reset the VM)", campaign, scenario, kind, foreign[0])
	case len(foreign) > 0:
		c.t.Logf("%s/%s: the %s store holds %d resource(s) that are neither of this run nor the sentinel: %q", campaign, scenario, kind, len(foreign), foreign)
	}
	return c.exchangeWith(cmp, campaign, scenario, text, call)
}

// findContainer is the row of a listing whose (short) id starts id. A row with no id is
// never a match: an empty string is a prefix of every id.
func findContainer(list []Container, id string) (Container, bool) {
	i := slices.IndexFunc(list, func(ctr Container) bool { return ctr.ID != "" && strings.HasPrefix(id, ctr.ID) })
	if i < 0 {
		return Container{}, false
	}
	return list[i], true
}

// volumePruneAgrees checks that what a volume prune reported agrees with what is left: a
// report that names the volume (as a word, docker under "Deleted Volumes:", podman one name
// a line) means it is gone, and one that does not means it is still listed. Engines and
// versions differ on whether an unused named volume is removed (podman 5.8.11 removes it,
// docker 28 and 29 and podman 6.2.1 do not), so neither outcome is required.
func volumePruneAgrees(report, volume string, stillListed bool) error {
	reported := slices.Contains(strings.Fields(report), volume)
	switch {
	case reported && stillListed:
		return fmt.Errorf("the prune reports having removed %s, which is still listed", volume)
	case !reported && !stillListed:
		return fmt.Errorf("%s is no longer listed, but the prune does not report having removed it (%q)", volume, strings.TrimSpace(report))
	}
	return nil
}

// launchWindow is what a launch the engine is expected to refuse may name: the name of
// the container (a name of this run), and Port, "" or the one published port
// "127.0.0.1:<port>:<port>" on loopback.
type launchWindow struct{ Name, Port string }

// windowPortValid reports whether spec is "127.0.0.1:<host port>:<container port>":
// loopback, both ports numbers of one to five digits and in range.
func windowPortValid(spec string) bool {
	parts := strings.Split(spec, ":")
	if len(parts) != 3 || parts[0] != "127.0.0.1" {
		return false
	}
	for _, p := range parts[1:] {
		n, err := strconv.Atoi(p)
		if err != nil || len(p) > 5 || n < 1 || n > 65535 || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// launchWindowRule grants one `run` that names exactly the name of w, the pinned
// image, optionally the one published port of w, and that the standard rule would
// grant once the label of the run is added to the argv it judges (the launch DevDesk
// builds has none): --pull=never, no host path, no host network, no privilege. The
// label is the only thing added; the argv is judged from its parsed options, so a
// word the parser read as a value cannot be read as an option by the rule. The
// words after the image are the container's command. An argv it refuses does not use
// its attempt.
func launchWindowRule(runID, image string, ours func(ref string) bool, w launchWindow) func(argv []string) error {
	standard := contractArgvRule(runID, ours)
	granted := false
	return func(argv []string) error {
		if granted {
			return errors.New("the launch window grants one launch and it was used")
		}
		if len(argv) == 0 || argv[0] != "run" {
			return fmt.Errorf("the launch window grants one `run`, not %q", argv)
		}
		if w.Port != "" && !windowPortValid(w.Port) {
			return fmt.Errorf("the port %q of the window is not 127.0.0.1:<port>:<port>", w.Port)
		}
		spec := runSpec
		spec.valued = append(slices.Clone(runSpec.valued), "-p")
		opts, positional, err := parseOptions("run", argv[1:], spec, true)
		if err != nil {
			return err
		}
		forwarded, err := windowOptions(runID, w, spec, opts)
		if err != nil {
			return err
		}
		if len(positional) == 0 || positional[0] != image {
			return fmt.Errorf("the image %q is not the pinned image %q: the window grants the launch of that one", positional, image)
		}
		if err := standard(append(forwarded, positional...)); err != nil {
			return err
		}
		granted = true
		return nil
	}
}

// windowOptions checks the options of a launch against the window (one --name, which
// is its name; at most one -p, which is its port; no label of another run) and
// returns the argv the standard rule judges: `run --label <run>`, then every option
// but -p.
func windowOptions(runID string, w launchWindow, spec optSpec, opts []option) ([]string, error) {
	forwarded := []string{"run", "--label", enginecontract.Label(runID)}
	names, ports := 0, 0
	for _, o := range opts {
		switch o.name {
		case "--name":
			names++
			if o.value != w.Name {
				return nil, fmt.Errorf("--name %s is not the name of the window, %s", o.value, w.Name)
			}
		case "-p":
			ports++
			if w.Port == "" || o.value != w.Port {
				return nil, fmt.Errorf("the published port %q is not the port of the window (%q): the window grants one loopback port or none", o.value, w.Port)
			}
			continue
		case "--label", "-l":
			if key, value, _ := strings.Cut(o.value, "="); key == enginecontract.LabelKey && value != runID {
				return nil, fmt.Errorf("the label %s is the label of another run", o.value)
			}
		}
		forwarded = append(forwarded, o.name)
		if slices.Contains(spec.valued, o.name) {
			forwarded = append(forwarded, o.value)
		}
	}
	if names != 1 {
		return nil, fmt.Errorf("the window grants a launch with exactly one --name (%s), this one has %d", w.Name, names)
	}
	if ports > 1 {
		return nil, errors.New("the window grants one published port at most")
	}
	return forwarded, nil
}

// windowRole matches what follows ddc-<run>- in the name of a window.
var windowRole = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// requireWindow ends the contract unless w names a resource of this run and, when it
// names a port, one fixed loopback port: the window is whole before anything runs.
func (c *contract) requireWindow(w launchWindow) {
	c.t.Helper()
	prefix := enginecontract.NamePrefix + c.s.RunID + "-"
	if role, ok := strings.CutPrefix(w.Name, prefix); !ok || !windowRole.MatchString(role) {
		c.t.Fatalf("the name %q of the window is not %s<role>, a name of this run", w.Name, prefix)
	}
	if w.Port != "" && !windowPortValid(w.Port) {
		c.t.Fatalf("the port %q of the window is not 127.0.0.1:<port>:<port>: a window publishes one fixed port on loopback or none", w.Port)
	}
}

// expectRefusal runs call, which must make exactly one launch the engine refuses (a
// name taken, a port taken), in a window that allows that one `run`: the launch
// DevDesk builds carries no label, so the standard rule refuses it. It returns the
// exchange as exchange does and reports a launch the engine accepted. Afterwards it
// looks at what carries the name of the window, because an engine may leave a
// container behind when it fails (docker creates the container of a port it cannot
// bind): one with no label is removed by its id, one of this run is left to the
// cleanup, one of another run is reported and left alone. The look also happens when
// the call ends the contract. Limit: a process killed in the window can leave a
// container with no label, which the cleanup by label does not see; reset.sh does.
func (c *contract) expectRefusal(campaign, scenario, text string, w launchWindow, call func()) enginefixture.Exchange {
	c.t.Helper()
	c.requireLab()
	image := c.pinnedImage()
	c.requireWindow(w)
	defer c.removeStray(w.Name)
	var ex enginefixture.Exchange
	c.withWindow(launchWindowRule(c.s.RunID, image, c.isOurs, w), func() { ex = c.exchange(campaign, scenario, text, call) })
	if ex.Exit == 0 {
		c.t.Errorf("%s/%s: the engine accepted the launch of %s, which it should refuse; the container is removed", campaign, scenario, w.Name)
	}
	return ex
}

// noLabelPrinted is what a template that indexes a missing label prints on docker (measured on
// 29.10.1 and 28.5.2); podman may print an empty string. Both mean "no label".
const noLabelPrinted = "<no value>"

// labelValue is the label a template printed, trimmed, with "no label" as "".
func labelValue(printed string) string {
	label := strings.TrimSpace(printed)
	if label == noLabelPrinted {
		return ""
	}
	return label
}

// removeStray looks for a container called name and deals with what it finds, see
// expectRefusal: the name is the window's, so no other launch can have given it. One with no
// label is the container of the refused run and is removed by its full id. A lookup that fails
// for another reason than "no such container" is reported.
func (c *contract) removeStray(name string) {
	c.t.Helper()
	defer c.recording()()
	format := `{{.Id}} {{index .Config.Labels "` + enginecontract.LabelKey + `"}}`
	out, err := dockerOutput("inspect", "--type", "container", "--format", format, name)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.Contains(strings.ToLower(string(exit.Stderr)), "no such") {
			return
		}
		c.t.Errorf("looking for a container called %s after the refusal: %v: %s", name, err, failureDetail(out, err))
		return
	}
	// The line is the id, a space and the label: cut at the first space (an id has none), the
	// rest is the label, whatever it holds. A container with no label prints "<no value>" on docker.
	id, rest, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !fullID.MatchString(id) {
		c.t.Errorf("looking for a container called %s after the refusal: the engine printed %q, want its id and its label", name, strings.TrimSpace(string(out)))
		return
	}
	label := labelValue(rest)
	switch {
	case c.isOurs(id):
		// A container the contract made: the name was taken by it.
	case label == c.s.RunID:
		c.tried = true // the cleanup finds it by its label
	case label != "":
		c.t.Errorf("the container %s called %s carries the label %q, which is not the one of this run: left alone", id, name, label)
	default:
		c.ours[id] = "stray"
		if err := RemoveContainer(id, true); err != nil {
			c.t.Errorf("removing the container %s the engine left behind under the name %s: %v", id, name, err)
		}
	}
}

// rmiWindowRule grants the one argv `rmi <imageID>`, once, for an id of 64
// hexadecimal characters: no -f (it would remove the image from under the container),
// no tag, no other image. An argv it refuses does not use its attempt.
func rmiWindowRule(imageID string) func(argv []string) error {
	granted := false
	return func(argv []string) error {
		if !fullID.MatchString(imageID) {
			return fmt.Errorf("%q is not an image id of 64 lower-case hexadecimal characters: the rmi window grants nothing", imageID)
		}
		if granted {
			return errors.New("the rmi window grants one attempt and it was used")
		}
		if !slices.Equal(argv, []string{"rmi", imageID}) {
			return fmt.Errorf("the rmi window grants %q and nothing else, not %q", "rmi "+imageID, argv)
		}
		granted = true
		return nil
	}
}

// expectRmiRefusal runs call, which must make exactly one `rmi <imageID>` of an image
// a container of this run uses, in a window that allows that one argv, and returns the
// exchange as exchange does. The standard rule refuses every rmi (an image is never
// made by a contract), and an rmi that succeeded would remove the image every later
// contract starts, so the use of the image is read again right before the call
// (`ps -a -q --no-trunc --filter ancestor=<id>`) and must list a container usedImage
// made: an id usedImage did not return, a listing that fails or one without such a
// container ends the contract before any rmi. An rmi the engine accepted is reported.
func (c *contract) expectRmiRefusal(campaign, scenario, text, imageID string, call func()) enginefixture.Exchange {
	c.t.Helper()
	c.requireLab()
	users := c.imageUsers[imageID]
	if len(users) == 0 {
		c.t.Fatalf("%s/%s: the image %s was not announced by usedImage: an rmi is sent only for an image a container of this run uses",
			campaign, scenario, imageID)
	}
	c.requireImageInUse(imageID, users)
	var ex enginefixture.Exchange
	c.withWindow(rmiWindowRule(imageID), func() { ex = c.exchange(campaign, scenario, text, call) })
	if ex.Exit == 0 {
		c.t.Errorf("%s/%s: the image %s was removed, so the pinned image is gone for the next contract: pull it again with contract.sh",
			campaign, scenario, imageID)
	}
	return ex
}

// requireImageInUse ends the contract unless the engine lists one of users among the
// containers made from the image.
func (c *contract) requireImageInUse(imageID string, users []string) {
	c.t.Helper()
	defer c.recording()()
	out, err := dockerOutput("ps", "-a", "-q", "--no-trunc", "--filter", "ancestor="+imageID)
	if err != nil {
		c.t.Fatalf("cannot read which containers use the image %s, so it is not removed: %v: %s", imageID, err, failureDetail(out, err))
	}
	listed := splitLines(out)
	if !slices.ContainsFunc(users, func(user string) bool { return slices.Contains(listed, user) }) {
		c.t.Fatalf("no container of this run uses the image %s according to the engine (ancestor filter), so an rmi would remove it: "+
			"on docker with the containerd image store the id and the filter may not agree", imageID)
	}
}

// healthContainer starts a container of this run whose healthcheck is `true` every
// second (one retry), and returns its id. The health state moves with time: waitHealthy
// waits for it.
func (c *contract) healthContainer(role string) string {
	c.t.Helper()
	return c.containerWith(role, []string{"--health-cmd", "true", "--health-interval", "1s", "--health-timeout", "2s", "--health-retries", "1"})
}

// healthReads is how many times waitHealthy reads the health state, one second apart.
const healthReads = 20

// waitHealthy reads the health state of a container of this run until it is healthy,
// at most healthReads times, a second apart. An unhealthy container, a read that
// fails and a state that never comes end the contract, naming what was read. On
// podman each round first runs the check once (`healthcheck run`): the health state
// of a container of the lab VM does not move by itself (measured 2026-11-27).
func (c *contract) waitHealthy(id string) {
	c.t.Helper()
	defer c.recording()()
	last := ""
	for i := range healthReads {
		if i > 0 {
			c.sleep(time.Second)
		}
		if c.s.Engine.Tool == enginefixture.Podman {
			_, _ = dockerOutput("healthcheck", "run", id) // a failing check is what the read below says
		}
		out, err := dockerOutput("inspect", "--type", "container", "--format", "{{.State.Health.Status}}", id)
		if err != nil {
			c.t.Fatalf("reading the health state of %s: %v: %s", id, err, failureDetail(out, err))
		}
		switch last = strings.TrimSpace(string(out)); last {
		case "healthy":
			return
		case "unhealthy":
			c.t.Fatalf("the container %s is unhealthy: its check `true` cannot fail", id)
		}
	}
	c.t.Fatalf("the container %s is still %q after %d reads, a second apart", id, last, healthReads)
}

// portHolderPort is the host port of portHolder: fixed, so the argv a fixture holds
// does not change from one recording to the next, and on loopback only. A lab VM has
// nothing else on it; anywhere else the holder fails and says so.
const portHolderPort = "18080"

// portHolder starts a container of this run that publishes 127.0.0.1:18080 (to its
// port 80) and returns its id and the host port. The standard rule refuses a published
// port, so the creation goes through a launch window for that one name and that one
// port, and the window closes when the call returns.
func (c *contract) portHolder(role string) (id, port string) {
	c.t.Helper()
	c.requireLab()
	image := c.pinnedImage()
	name := enginecontract.ResourceName(c.s.RunID, role)
	spec := "127.0.0.1:" + portHolderPort + ":80"
	hint := "port " + portHolderPort + " on 127.0.0.1 is taken by something else, or this is not a clean lab VM: the port holder needs it"
	window := launchWindowRule(c.s.RunID, image, c.isOurs, launchWindow{Name: name, Port: spec})
	c.withWindow(window, func() {
		id = c.createHinted(hint, "container", role, name, IsContainerID,
			"run", "-d", "--pull=never", "--label", enginecontract.Label(c.s.RunID), "--name", name, "-p", spec, image, "sleep", "600")
	})
	return id, portHolderPort
}

// usedVolume creates a volume of this run and a container of this run that mounts it,
// and returns the name of the volume and the id of the container.
func (c *contract) usedVolume(volumeRole, containerRole string) (volume, container string) {
	c.t.Helper()
	volume = c.volume(volumeRole)
	mount := enginecontract.ResourceName(c.s.RunID, volumeRole) + ":/data"
	container = c.containerWith(containerRole, []string{"-v", mount})
	return volume, container
}

// usedNetwork creates a network of this run and a container of this run that joins
// it, and returns what the engine printed for the network (an id for docker, the name
// for podman) and the id of the container.
func (c *contract) usedNetwork(networkRole, containerRole string) (network, container string) {
	c.t.Helper()
	network = c.network(networkRole)
	container = c.containerWith(containerRole, []string{"--network", enginecontract.ResourceName(c.s.RunID, networkRole)})
	return network, container
}

// usedImage starts a container of this run and returns it with the id of the image it
// was made from: 64 lower-case hexadecimal characters, read with `image inspect
// --format {{.Id}}` (docker keeps "sha256:" in front, podman does not) and declared
// public, since the scenarios print it. expectRmiRefusal checks the use of the image
// by the engine's own ancestor filter: with the containerd image store of recent
// docker the id and the filter may disagree, which only a lab VM shows.
func (c *contract) usedImage(role string) (container, imageID string) {
	c.t.Helper()
	container = c.container(role)
	imageID = c.publicImageID()
	c.imageUsers[imageID] = append(c.imageUsers[imageID], container)
	return container, imageID
}

// publicImageID reads the id of the pinned image, declares it public and returns it.
func (c *contract) publicImageID() string {
	c.t.Helper()
	image := c.pinnedImage()
	defer c.recording()()
	out, err := dockerOutput("image", "inspect", "--format", "{{.Id}}", image)
	id := strings.TrimPrefix(strings.TrimSpace(string(out)), "sha256:")
	if err != nil || !fullID.MatchString(id) {
		c.t.Fatalf("the id of the image %s: want 64 hexadecimal characters, the engine printed %q (%v)", image, strings.TrimSpace(string(out)), err)
	}
	c.public(id)
	return id
}

// public declares ids a fixture may keep (Redaction.Public): the id of the pinned
// image, which every scenario that prints it shares. Redact does not touch them, and
// a recording that holds any other id of 64 hexadecimal characters is refused. An id
// that is not 64 lower-case hexadecimal characters, or that is a resource of this
// run (it is redacted, never public), ends the contract and declares nothing.
func (c *contract) public(ids ...string) {
	c.t.Helper()
	for _, id := range ids {
		if !fullID.MatchString(id) {
			c.t.Fatalf("%q is not an id of 64 lower-case hexadecimal characters: no algorithm in front, no short form", id)
		}
		if c.isOurs(id) {
			c.t.Fatalf("%s is the id of a resource of this run: it is redacted and never public", id)
		}
	}
	for _, id := range ids {
		if !slices.Contains(c.publicIDs, id) {
			c.publicIDs = append(c.publicIDs, id)
		}
	}
}

// ownLaunch is the launch DevDesk makes for a container of the window: detached, from
// the pinned image, with the given name and published ports ("18080:80" becomes
// 127.0.0.1:18080:80, LoopbackPort).
func (c *contract) ownLaunch(name string, ports ...string) ContainerLaunchOptions {
	c.t.Helper()
	return ContainerLaunchOptions{Name: name, Image: c.pinnedImage(), Ports: ports, Detach: true}
}

// saysAny reports whether text holds one of words, whatever the case.
func saysAny(text []byte, words ...string) bool {
	lower := strings.ToLower(string(text))
	return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(lower, strings.ToLower(w)) })
}
