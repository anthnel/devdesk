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
	Ports       []string // "hostPort:containerPort"
	Env         []string // "KEY=VALUE"
	Volumes     []string // "name:/path"
	Network     string   // network name
	User        string   // user to run as (--user UID:GID or username, optional)
	Detach      bool     // run in background (-d)
	Remove      bool     // automatically remove the container when it exits (--rm)
	Interactive bool     // keep STDIN open even if not attached (-i)
	TTY         bool     // allocate a pseudo-TTY (-t)
}

// buildLaunchArgs constructs the docker run argument list for the given options.
func buildLaunchArgs(opts ContainerLaunchOptions) []string {
	args := []string{"run"}
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
			args = append(args, "-p", p)
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
	_, err := dockerCombined("run", "--rm", "--entrypoint", "/bin/sh",
		image, "-c", "command -v "+entrypoint)
	return err == nil, nil
}
