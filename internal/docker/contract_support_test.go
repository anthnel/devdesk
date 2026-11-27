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
	if !enginecontract.ValidImage(image) {
		c.t.Fatalf("%s=%q is not a repository pinned by digest (repository@sha256:<64 hex>): a tag moves, and a contract never pulls",
			enginecontract.ImageEnv, image)
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
		c.s.Verify(c.t, campaign, scenario, raw, c.redaction(), nil)
	}
	return raw
}

// fullID is the id of a container or a network as the engine prints it whole.
var fullID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// redaction is what Redact must replace in this run's exchanges: the ids of the
// resources the run made, each with its role, the run id, and the home, the user
// and the host name of the machine that records (a lookup that fails leaves its
// field empty, and the leak check is the net for what that lets through).
func (c *contract) redaction() enginefixture.Redaction {
	r := enginefixture.Redaction{RunID: c.s.RunID, IDs: map[string]string{}}
	for ref, role := range c.ours {
		if fullID.MatchString(ref) {
			r.IDs[ref] = role
		}
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
//     never a path), --entrypoint, -u/--user and -w/--workdir. Everything else is
//     refused, and the ones that would reach the host name their reason:
//     --privileged, --replace, --mount, --cap-add, --device, --security-opt, --pid,
//     --ipc, --uts, --userns, --volumes-from, --env-file. volume create and network
//     create take only --label and one name that starts with ddc-<run>-. pod create is
//     refused: no cleanup of pods exists.
//   - Destruction and state changes (rm, kill, stop, start, restart, pause,
//     unpause, volume rm, network rm, network connect and disconnect) may name only
//     resources of this run, by exact reference, and take only the options of the
//     verb: rm -f, -v, -t; stop and kill -t and -s; restart -t; volume rm, network rm,
//     start, pause, unpause, connect and disconnect none (so no --force, --all,
//     --latest, --filter, --depend or --cidfile).
//   - Refused as a whole: every prune (a prune acts on the whole store; OC-09b may
//     allow one under a precondition), system, pull, push, load, import, build,
//     commit, tag, login, logout, exec, cp, save, export, and rmi (an image is never
//     made by a contract, contract.sh pulls the one it uses).
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
			"-v", "--volume", "--entrypoint", "-u", "--user", "-w", "--workdir"},
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
