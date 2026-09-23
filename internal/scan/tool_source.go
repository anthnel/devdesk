package scan

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
)

// ToolSpec says how one scanner is invoked: from a binary or from an image run
// by the configured container engine, and which one.
//
// It replaces the `source ToolSource, image string` pair that every command
// builder used to take. That pair had nowhere to carry the configured binary
// path, which is why `scan.trivy_path` was declared, defaulted, tilde-expanded
// on load — and read by nothing (D27). Carrying all three together means a
// builder cannot see the source without also seeing what to run.
type ToolSpec struct {
	Source ToolSource
	// Binary is what to exec when Source is ToolSourceBinary: the configured
	// path when there is one, else the plain name resolved on PATH.
	Binary string
	// Image is the OCI image when Source is ToolSourceContainer.
	Image string
	// HostSocket is the engine socket to bind-mount so a containerised Trivy
	// can inspect an image the host holds. Empty means there is none to mount,
	// which is rootless podman without `podman system service`; the builder
	// then makes no mount rather than inventing a path (§3.67).
	//
	// Only the Trivy image path reads it. A directory scan mounts the
	// directory and nothing else.
	HostSocket string
	// Config is Trivy's own rules file (trivy.yaml), absolute. Gitleaks and
	// plumber take theirs through their options, as they did before.
	Config string
	// Args are the user's extra arguments, placed after the subcommand and
	// before DevDesk's own flags and the target: kubeconform parses with Go's
	// flag package, which stops at the first positional argument, so a flag
	// placed after the target would be read as a file.
	Args []string
}

// afterSubcommand inserts the user's arguments after the subcommand args[0]
// and before everything DevDesk adds. The slice is always a new one, so a
// builder's literal is never written through.
func afterSubcommand(args, user []string) []string {
	if len(user) == 0 || len(args) == 0 {
		return args
	}
	out := make([]string, 0, len(args)+len(user))
	out = append(out, args[0])
	out = append(out, user...)
	return append(out, args[1:]...)
}

// checkRulesFile refuses a rules file that cannot be read, before anything is
// started — §3.50's guard, for §3.50's reason. `docker run -v` on a host path
// that does not exist does not fail: it creates a directory at that path and
// mounts it, once per scan.
func checkRulesFile(tool, path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec // the path is the user's own setting
	if err != nil {
		return fmt.Errorf("%s config: %w", tool, err)
	}
	return f.Close()
}

// toolResolution is what detection worked out for one scanner.
type toolResolution struct {
	Available bool
	Source    ToolSource
	Binary    string
	Version   string
}

// resolveTool decides where one scanner runs from.
//
// The three preferences differ in exactly one way that matters: `binary` does
// **not** fall back to a container. That silent fallback is what kept D27 invisible
// — a configured path that was never read still produced working scans, run by
// something other than what was asked for. Asking for a binary and not having
// one is now an unavailable tool, which the dashboard reports.
func resolveTool(pref, configuredPath, name, image string, engineAvailable bool, versionArgs ...string) toolResolution {
	useBinary := func() (toolResolution, bool) {
		path, ok := locateBinary(configuredPath, name)
		if !ok {
			return toolResolution{Source: ToolSourceNone}, false
		}
		return toolResolution{
			Available: true,
			Source:    ToolSourceBinary,
			Binary:    path,
			Version:   toolVersion(path, versionArgs...),
		}, true
	}

	useImage := func() (toolResolution, bool) {
		if !engineAvailable || !imageIsLocal(image) {
			return toolResolution{Source: ToolSourceNone}, false
		}
		return toolResolution{
			Available: true,
			Source:    ToolSourceContainer,
			Version:   containerToolVersion(image, versionArgs...),
		}, true
	}

	switch pref {
	case config.ToolSourceBinary:
		res, _ := useBinary()
		return res
	case config.ToolSourceImage:
		res, _ := useImage()
		return res
	default: // config.ToolSourceAuto, and an empty value from a config that predates the setting
		if res, ok := useBinary(); ok {
			return res
		}
		res, _ := useImage()
		return res
	}
}

// locateBinary returns the executable to run. A configured path is taken as
// given — it is the one thing the user stated explicitly — and only checked for
// existence; anything else is resolved on PATH.
func locateBinary(configuredPath, name string) (string, bool) {
	if configuredPath != "" {
		if info, err := os.Stat(configuredPath); err == nil && !info.IsDir() {
			return configuredPath, true
		}
		// A path that was set and does not exist resolves to nothing rather than
		// quietly falling through to PATH: the user named a file.
		return "", false
	}
	path, err := exec.LookPath(name)
	if err != nil || path == "" {
		return "", false
	}
	return path, true
}

// toolVersion asks the binary what it is. A tool that will not answer is still
// usable, so the version is best-effort.
func toolVersion(path string, args ...string) string {
	out, err := exec.Command(path, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// containerToolVersion asks the image what it is, through the configured
// engine. The engine's name is part of the answer — it is what the dashboard
// shows to say where the tool is coming from.
func containerToolVersion(image string, args ...string) string {
	name := engine.Current().Name
	runArgs := append([]string{"run", "--rm", image}, args...)
	out, err := exec.Command(engine.Current().Binary, runArgs...).Output() //nolint:gosec // the binary is a declared engine name or a configured path
	if err != nil {
		return name
	}
	return name + ":" + string(out)
}
