package scan

import (
	"os"
	"os/exec"

	"github.com/anthnel/devdesk/internal/config"
)

// ToolSpec says how one scanner is invoked: from a binary or from a Docker
// image, and which one.
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
	// Image is the Docker image when Source is ToolSourceDocker.
	Image string
}

// TrivySpec, GitleaksSpec and PlumberSpec are how a resolved DependencyStatus
// is handed to the command builders.
func (d DependencyStatus) TrivySpec() ToolSpec {
	return ToolSpec{Source: d.TrivySource, Binary: d.TrivyBinary, Image: d.TrivyImage}
}

func (d DependencyStatus) GitleaksSpec() ToolSpec {
	return ToolSpec{Source: d.GitleaksSource, Binary: d.GitleaksBinary, Image: d.GitleaksImage}
}

func (d DependencyStatus) PlumberSpec() ToolSpec {
	return ToolSpec{Source: d.PlumberSource, Binary: d.PlumberBinary, Image: d.PlumberImage}
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
// **not** fall back to Docker. That silent fallback is what kept D27 invisible
// — a configured path that was never read still produced working scans, run by
// something other than what was asked for. Asking for a binary and not having
// one is now an unavailable tool, which the dashboard reports.
func resolveTool(pref, configuredPath, name, image string, dockerAvailable bool, versionArgs ...string) toolResolution {
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
		if !dockerAvailable || !checkDockerImage(image) {
			return toolResolution{Source: ToolSourceNone}, false
		}
		return toolResolution{
			Available: true,
			Source:    ToolSourceDocker,
			Version:   dockerToolVersion(image, versionArgs...),
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

func dockerToolVersion(image string, args ...string) string {
	runArgs := append([]string{"run", "--rm", image}, args...)
	out, err := exec.Command("docker", runArgs...).Output()
	if err != nil {
		return "docker"
	}
	return "docker:" + string(out)
}
