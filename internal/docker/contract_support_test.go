package docker

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

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
	return &contract{t: t, s: s, ours: map[string]string{}}
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
	c.requireLab()
	image := os.Getenv(enginecontract.ImageEnv)
	if image == "" {
		c.t.Fatalf("%s is empty: contract.sh sets it from CONTRACT_IMAGE in tools/engine-vms/versions.env, "+
			"and a contract never pulls an image", enginecontract.ImageEnv)
	}
	if len(cmd) == 0 {
		cmd = []string{"sleep", "600"}
	}
	name := enginecontract.ResourceName(c.s.RunID, role)
	args := []string{"run", "-d", "--pull=never", "--label", enginecontract.Label(c.s.RunID), "--name", name, image}
	return c.create("container", role, name, IsContainerID, append(args, cmd...)...)
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
	defer c.recording()()
	c.tried = true
	out, err := dockerOutput(args...)
	ref := strings.TrimSpace(string(out))
	if err != nil {
		c.t.Fatalf("creating the %s %s failed: %v: %s", kind, name, err, failureDetail(out, err))
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

// exchange runs call, which must make exactly one engine exchange, and returns it.
// A fixture is one exchange: none (the call never reached the engine, or the rule
// refused it) and two or more are both a failure of the contract. Recording and
// verifying against a fixture join here in OC-09a3; for now the exchange is
// returned as it happened and its argv and exit code are logged.
func (c *contract) exchange(campaign, scenario, text string, call func()) enginefixture.Exchange {
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
	return enginefixture.Exchange{Argv: e.Argv, Exit: e.Exit, Stdout: e.Stdout, Stderr: e.Stderr}
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
		switch label := strings.TrimSpace(string(out)); {
		case err != nil:
			c.t.Errorf("cleanup: cannot read the label of the %s %s, left alone: %v: %s", k.name, ref, err, failureDetail(out, err))
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
// verified) are all it knows. A verb it does not know is refused, so a new one is
// added here, with its reason, rather than worked around.
//
//   - Reads are allowed on anything: the cleanup inspects candidates it does not
//     own yet, to find out whose they are.
//   - A creation (run, create, volume create, network create, pod create) must
//     carry the exact label of the run; run and create must also say --pull=never.
//   - Destruction and state changes (rm, kill, stop, start, restart, pause,
//     unpause, volume rm, network rm, network connect and disconnect) may name only
//     resources of this run, by exact reference. -a, --all, --latest, --filter and
//     --cidfile are refused: they pick targets the rule cannot see.
//   - Refused as a whole: every prune (a prune acts on the whole store; OC-09b may
//     allow one under a precondition), system, pull, push, load, import, build,
//     commit, tag, login, logout, exec, cp, save, export, and rmi (an image is never
//     made by a contract, contract.sh pulls the one it uses).
//
// Limit: a --label that sits after the image of a `run` is seen as the label of
// the run, though the engine would pass it to the container's command.
func contractArgvRule(runID string, ours func(ref string) bool) func(argv []string) error {
	return func(argv []string) error {
		if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
			return errUnknownVerb(argv)
		}
		verb, rest := argv[0], argv[1:]
		switch verb {
		case "container", "image", "volume", "network", "pod", "system":
			return checkGroupArgv(runID, ours, verb, rest)
		}
		return checkVerbArgv(runID, ours, verb, rest)
	}
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
			return checkCreateArgv(runID, group+" "+sub, args, false)
		case "rm":
			return checkTargetsArgv(ours, group+" "+sub, args, false)
		}
	case "network":
		switch sub {
		case "ls", "inspect":
			return nil
		case "create":
			return checkCreateArgv(runID, group+" "+sub, args, false)
		case "rm":
			return checkTargetsArgv(ours, group+" "+sub, args, false)
		case "connect", "disconnect":
			return checkTargetsArgv(ours, group+" "+sub, args, true)
		}
	case "pod":
		if sub == "create" {
			return checkCreateArgv(runID, group+" "+sub, args, false)
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
		return checkCreateArgv(runID, verb, args, true)
	case "rm", "kill", "stop", "start", "restart", "pause", "unpause":
		return checkTargetsArgv(ours, verb, args, false)
	case "rmi":
		return errImagesAreNeverOurs(args)
	}
	return errUnknownVerb(append([]string{verb}, args...))
}

func errImagesAreNeverOurs(args []string) error {
	return fmt.Errorf("rmi %s is refused: a contract never makes an image, contract.sh pulls the one it uses", strings.Join(args, " "))
}

// checkCreateArgv requires the label of the run on a creation and, when needPull,
// --pull=never on every --pull the argv carries (and at least one).
func checkCreateArgv(runID, verb string, args []string, needPull bool) error {
	want := enginecontract.Label(runID)
	if !hasLabelArg(args, want) {
		return fmt.Errorf("%s without the label %s: every resource of a contract carries the label of its run", verb, want)
	}
	if needPull && !pullsNever(args) {
		return fmt.Errorf("%s without --pull=never: a contract never pulls, contract.sh pulls the image", verb)
	}
	return nil
}

// hasLabelArg reports whether args hold the exact label, as `--label X`,
// `--label=X` or `-l X`.
func hasLabelArg(args []string, want string) bool {
	for i, a := range args {
		switch {
		case a == "--label="+want:
			return true
		case (a == "--label" || a == "-l") && i+1 < len(args) && args[i+1] == want:
			return true
		}
	}
	return false
}

// pullsNever reports whether every --pull of args says never, and there is one.
func pullsNever(args []string) bool {
	found := false
	for i, a := range args {
		var value string
		switch {
		case strings.HasPrefix(a, "--pull="):
			value = strings.TrimPrefix(a, "--pull=")
		case a == "--pull" && i+1 < len(args):
			value = args[i+1]
		default:
			continue
		}
		if value != "never" {
			return false
		}
		found = true
	}
	return found
}

// checkTargetsArgv requires that every target of a destructive verb be a resource
// of the run. With lastOnly (network connect and disconnect) only the last
// positional, the container, is checked: the network may be a built-in one.
func checkTargetsArgv(ours func(string) bool, verb string, args []string, lastOnly bool) error {
	var targets []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--time" || a == "-s" || a == "--signal":
			i++ // its value is not a target
		case a == "--all" || a == "--latest" || strings.HasPrefix(a, "--filter") || strings.HasPrefix(a, "--cidfile"):
			return fmt.Errorf("%s %s is refused: it picks its targets where the rule cannot see them", verb, a)
		case strings.HasPrefix(a, "--"):
			// another long option: --force, --volumes, --time=5
		case strings.HasPrefix(a, "-"):
			if strings.ContainsAny(a[1:], "al") {
				return fmt.Errorf("%s %s is refused: -a and -l pick their targets where the rule cannot see them", verb, a)
			}
		default:
			targets = append(targets, a)
		}
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
