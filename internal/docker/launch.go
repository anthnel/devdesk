package docker

import (
	"os/exec"
	"strings"
)

// ContainerLaunchOptions holds parameters for running a container
type ContainerLaunchOptions struct {
	Name        string   // container name (optional)
	Image       string   // image to run
	Entrypoint  string   // override default entrypoint (optional)
	Ports       []string // "hostPort:containerPort", "ip:hostPort:containerPort" or "containerPort", each with an optional "/proto"; LoopbackPort is applied to each
	Env         []string // "KEY=VALUE"
	Volumes     []string // "name:/path"
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

// buildLaunchArgs constructs the docker run argument list for the given options.
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
		if e != "" {
			args = append(args, "-e", e)
		}
	}
	for _, v := range opts.Volumes {
		if v != "" {
			args = append(args, "-v", v)
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
func BuildLaunchCmd(opts ContainerLaunchOptions) (*exec.Cmd, error) {
	if err := requireEngine(); err != nil {
		return nil, err
	}
	return runner.Build(buildLaunchArgs(opts)...), nil
}

// LaunchContainer runs a new container with the given options and returns its ID/output.
// Do NOT use this for interactive (-it) containers — use BuildLaunchCmd + tea.ExecProcess instead.
func LaunchContainer(opts ContainerLaunchOptions) (string, error) {
	if err := requireEngine(); err != nil {
		return "", err
	}
	output, err := dockerCombined(buildLaunchArgs(opts)...)
	if err != nil {
		return "", errWithOutput(cmdLabel("run"), output)
	}
	return strings.TrimSpace(string(output)), nil
}

// VerifyEntrypoint checks if the given binary is available inside an image
// by running a short-lived throwaway container with /bin/sh -c "command -v <binary>".
// Returns true if the binary is found, false otherwise.
func VerifyEntrypoint(image, entrypoint string) (bool, error) {
	if err := requireEngine(); err != nil {
		return false, err
	}
	_, err := dockerCombined("run", noImplicitPull, "--rm", "--entrypoint", "/bin/sh",
		image, "-c", "command -v "+entrypoint)
	return err == nil, nil
}
