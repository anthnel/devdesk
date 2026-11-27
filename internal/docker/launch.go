package docker

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ContainerLaunchOptions holds parameters for running a container
type ContainerLaunchOptions struct {
	Name        string   // container name (optional)
	Image       string   // image to run
	Entrypoint  string   // override default entrypoint (optional)
	Ports       []string // "hostPort:containerPort", "ip:hostPort:containerPort" or "containerPort", each with an optional "/proto"; LoopbackPort is applied to each
	Env         []string // "NAME=VALUE"; the value reaches the child's environment, never argv (OC-14)
	Volumes     []string // "name:/path" or "/host/path:/path[:ro]", each passed as -v
	Mounts      []string // the value of each --mount: "type=bind,source=/host/path,target=/path[,readonly]"
	Network     string   // network name
	User        string   // user to run as (--user UID:GID or username, optional)
	Detach      bool     // run in background (-d)
	Remove      bool     // automatically remove the container when it exits (--rm)
	Interactive bool     // keep STDIN open even if not attached (-i)
	TTY         bool     // allocate a pseudo-TTY (-t)
}

// noImplicitPull keeps `run` from fetching an image it does not have. Without
// it, launching a missing image is a pull no signature check saw (§3.82): an
// image reaches this machine through imagepull.Pull, or not at all. Measured on
// Docker and Podman: a missing image exits 125 with the engine's own message.
const noImplicitPull = "--pull=never"

// loopbackAddr is the host address a published port gets when its spec names none.
const loopbackAddr = "127.0.0.1"

// portAddress returns the host address a -p spec names, and whether the spec has
// an address field at all. The forms are [ip:]hostPort:containerPort[/proto] and
// containerPort[/proto]; an IPv6 address is in brackets, so a spec has an
// address field when it starts with "[" or holds two colons (the host port may be
// empty: "127.0.0.1::80"). The address itself is empty for ":8080:80".
func portAddress(spec string) (addr string, ok bool) {
	if strings.HasPrefix(spec, "[") {
		end := strings.Index(spec, "]")
		if end < 0 {
			return "", false // malformed: left to the engine to refuse
		}
		return spec[:end+1], true
	}
	if strings.Count(spec, ":") >= 2 {
		return spec[:strings.Index(spec, ":")], true
	}
	return "", false
}

// LoopbackPort returns spec with 127.0.0.1 as its host address when it names
// none, so a port DevDesk publishes is reachable from this machine only (OC-19).
// An address the user wrote is never rewritten, and applying it twice changes
// nothing. A spec the engine would refuse is left for the engine to refuse.
func LoopbackPort(spec string) string {
	if spec == "" {
		return spec
	}
	addr, ok := portAddress(spec)
	switch {
	case ok && addr != "":
		return spec
	case ok: // ":8080:80" or "::80": an empty address binds every interface
		return loopbackAddr + spec
	case strings.Contains(spec, ":"): // "8080:80"
		return loopbackAddr + ":" + spec
	default: // "80": the engine picks the host port
		return loopbackAddr + "::" + spec
	}
}

// PortExposedOnAllInterfaces reports whether spec itself names 0.0.0.0 or [::],
// that is, the user asked for the port to be reachable from the network. A spec
// with no address is not: LoopbackPort gives it 127.0.0.1.
func PortExposedOnAllInterfaces(spec string) bool {
	addr, ok := portAddress(spec)
	return ok && (addr == "0.0.0.0" || addr == "[::]")
}

// ErrInvalidLaunchEnv is returned, wrapped or not, when an Env entry has no "="
// or a name ValidEnvName refuses. A bare `-e NAME` would make the engine copy
// the variable of DevDesk's own environment into the container, so nothing is
// started or built. The text never holds the entry, nor even an equals sign: the
// entry may be a secret typed without its name, and the error reaches the log.
var ErrInvalidLaunchEnv = errors.New("invalid environment variable for the launch: each entry needs a valid name and a value")

// ValidEnvName reports whether name is acceptable as the name of an environment
// variable of a launch: ASCII only, matching ^[A-Za-z0-9_][A-Za-z0-9_.-]*$. A dot
// or a dash inside the name and a leading digit pass; a leading dash does not, so
// a name can never read as an engine flag. A wildcard (`*`, `?`) is refused too:
// Podman expands `-e PREFIX*` to every variable of DevDesk's environment starting
// with PREFIX, which would copy host secrets into the container. Anything outside
// ASCII is refused (invisible and bidirectional characters, accents). The reserved
// names (`PATH`, `DOCKER_HOST`, ...) are a second rule, ReservedEnvName. The launch
// form calls both functions too, so the form and the launcher share one rule.
func ValidEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
		case (c == '.' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// launchEnvName returns the name of an Env entry, and whether the entry is
// well formed: a valid name, then "=". An empty value is accepted (`EMPTY=`); it
// is the form that refuses it. Whether the name is reserved is a separate
// question (ReservedEnvName), asked by validateLaunchEnv.
func launchEnvName(entry string) (string, bool) {
	name, _, ok := strings.Cut(entry, "=")
	return name, ok && ValidEnvName(name)
}

// validateLaunchEnv refuses the whole launch when one entry is not launchable:
// ErrInvalidLaunchEnv for a malformed one, ErrReservedLaunchEnv for a reserved
// name. Blank entries are skipped, as buildLaunchArgs skips them.
func validateLaunchEnv(env []string) error {
	for _, e := range env {
		if e == "" {
			continue
		}
		name, ok := launchEnvName(e)
		if !ok {
			return ErrInvalidLaunchEnv
		}
		if ReservedEnvName(name) {
			return ErrReservedLaunchEnv
		}
	}
	return nil
}

// launchEnviron returns the environment of the child: DevDesk's own, then
// exactly the variables typed, so the last entry of a name wins (os/exec keeps
// the last). A reserved name is never appended, even though the callers refuse
// the launch first (validateLaunchEnv): the child is the engine CLI, which such
// a name would drive. It is nil when nothing was typed, which leaves the child with
// DevDesk's environment as before. The explicit slice is what lets `-e NAME`
// find its value without the value ever being in argv; the model is
// containerRun.cmd (internal/scan).
func launchEnviron(env []string) []string {
	var typed []string
	for _, e := range env {
		if name, _, _ := strings.Cut(e, "="); e != "" && !ReservedEnvName(name) {
			typed = append(typed, e)
		}
	}
	if typed == nil {
		return nil
	}
	return append(os.Environ(), typed...)
}

// buildLaunchArgs constructs the docker run argument list for the given options.
// An Env entry is passed as `-e NAME`: its value travels in the environment of the
// child (launchEnviron), never in argv, where the process list would show it. An
// entry that is not launchable, or whose name is reserved, is left out; the
// callers refuse the launch first (validateLaunchEnv).
func buildLaunchArgs(opts ContainerLaunchOptions) []string {
	args := []string{"run", noImplicitPull}
	if opts.Remove {
		args = append(args, "--rm")
	}
	if opts.Detach {
		args = append(args, "-d")
	}
	if opts.Interactive {
		args = append(args, "-i")
	}
	if opts.TTY {
		args = append(args, "-t")
	}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Entrypoint != "" {
		args = append(args, "--entrypoint", opts.Entrypoint)
	}
	for _, p := range opts.Ports {
		if p != "" {
			args = append(args, "-p", LoopbackPort(p))
		}
	}
	for _, e := range opts.Env {
		if name, ok := launchEnvName(e); ok && !ReservedEnvName(name) {
			args = append(args, "-e", name)
		}
	}
	for _, v := range opts.Volumes {
		if v != "" {
			args = append(args, "-v", v)
		}
	}
	for _, m := range opts.Mounts {
		if m != "" {
			args = append(args, "--mount", m)
		}
	}
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}
	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}
	args = append(args, opts.Image)
	return args
}

// BuildLaunchCmd returns a ready-to-run exec.Cmd for the given options without executing it.
// Use this when you need to hand off the process to a terminal (e.g. tea.ExecProcess for -it mode).
// Its Env is set as soon as a variable is typed (launchEnviron). A launch with an
// entry ValidEnvName or the "=" refuses returns ErrInvalidLaunchEnv, and one with a
// reserved name ErrReservedLaunchEnv, and no command either way.
func BuildLaunchCmd(opts ContainerLaunchOptions) (*exec.Cmd, error) {
	if err := validateLaunchEnv(opts.Env); err != nil {
		return nil, err
	}
	if err := engineAvailable(); err != nil {
		return nil, err
	}
	cmd := runner.Build(buildLaunchArgs(opts)...)
	cmd.Env = launchEnviron(opts.Env)
	return cmd, nil
}

// LaunchError is a `run` the engine refused. Its text keeps the engine's own
// diagnostic for the caller that shows it, but that diagnostic can echo what was
// typed (an environment value), so a log line takes Summary instead.
type LaunchError struct {
	label  string
	output string
	err    error
}

func (e *LaunchError) Error() string { return e.label + " failed: " + e.output }

func (e *LaunchError) Unwrap() error { return e.err }

// Summary is the part of the error that is safe to log: the engine and the exit
// code, none of its output. A process that did not start has no exit code; the
// summary says so with a fixed phrase rather than the error text, which may name
// an argument.
func (e *LaunchError) Summary() string {
	var exit *exec.ExitError
	if errors.As(e.err, &exit) {
		return e.label + " failed (exit code " + strconv.Itoa(exit.ExitCode()) + ")"
	}
	return e.label + " failed (could not start the process)"
}

// LaunchLogText returns what a log line may say about the error of a launch:
// the summary of a *LaunchError, the engine's own output left out, and any other
// error as it is (none of them holds an entry or its value).
func LaunchLogText(err error) string {
	var le *LaunchError
	if errors.As(err, &le) {
		return le.Summary()
	}
	return err.Error()
}

// LaunchContainer runs a new container with the given options and returns its ID/output.
// Do NOT use this for interactive (-it) containers — use BuildLaunchCmd + tea.ExecProcess instead.
func LaunchContainer(opts ContainerLaunchOptions) (string, error) {
	if err := validateLaunchEnv(opts.Env); err != nil {
		return "", err
	}
	if err := engineAvailable(); err != nil {
		return "", err
	}
	output, err := runner.Run(dockerCmd{Args: buildLaunchArgs(opts), Combined: true, Env: launchEnviron(opts.Env)})
	if err != nil {
		return "", &LaunchError{label: cmdLabel("run"), output: strings.TrimSpace(string(output)), err: err}
	}
	return strings.TrimSpace(string(output)), nil
}

// VerifyEntrypoint checks if the given binary is available inside an image
// by running a short-lived throwaway container with /bin/sh -c "command -v <binary>".
// Returns true if the binary is found, false otherwise.
func VerifyEntrypoint(image, entrypoint string) (bool, error) {
	if err := engineAvailable(); err != nil {
		return false, err
	}
	_, err := dockerCombined("run", noImplicitPull, "--rm", "--entrypoint", "/bin/sh",
		image, "-c", "command -v "+entrypoint)
	return err == nil, nil
}
