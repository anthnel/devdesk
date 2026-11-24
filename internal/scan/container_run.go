package scan

import (
	"os"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/netproxy"
)

// proxyForward is the proxy environment a container receives. Tests swap it so
// that a developer's own HTTPS_PROXY never reaches an exact-argv assertion.
var proxyForward = netproxy.ContainerForward

// containerRun is every `<engine> run --rm` DevDesk issues for a scanner. It is
// the one place that adds the proxy variables, so no launcher can forget them.
type containerRun struct {
	// Flags are the engine flags before the image: mounts, -w, -i, and the -e of
	// credentials.
	Flags []string
	Image string
	// Argv is the tool's own argv, after the image.
	Argv []string
	// Env is extra NAME=value for the child, the values of the -e NAME in Flags.
	// It never reaches argv.
	Env   []string
	Stdin []byte
	// Offline cuts the container's network (`--network none`) and forwards no
	// proxy variable: a renderer reads a repository that is not trusted
	// (DEVDESK-767).
	Offline bool
}

// cmd builds `run --rm <Flags> [-e NAME]... <Image> <Argv>`. The proxy variables
// are passed by name, right before the image: their values travel in the
// child's environment, never in argv, so neither the process list nor the
// command shown in the UI carries them. Env is nil when there is neither a proxy
// to forward nor an extra variable.
//
// An Offline run adds `--network none` after the flags and forwards no proxy at
// all, so Env is nil unless the caller has variables of its own.
func (r containerRun) cmd() toolCmd {
	fwd := proxyForward()
	if r.Offline {
		fwd = netproxy.Forward{}
	}
	args := append([]string{"run", "--rm"}, r.Flags...)
	for _, name := range fwd.Names() {
		args = append(args, "-e", name)
	}
	if r.Offline {
		args = append(args, "--network", "none")
	}
	args = append(append(args, r.Image), r.Argv...)

	cmd := toolCmd{Name: engine.Current().Binary, Args: args, Stdin: r.Stdin}
	if !fwd.Empty() || r.Env != nil {
		cmd.Env = append(fwd.Environ(os.Environ()), r.Env...)
	}
	return cmd
}
