package scan

import (
	"fmt"
	"net/url"
	"strings"
)

// Building a Trivy invocation is pure: the same inputs produce the command that
// is run and the command that is shown. Keeping the two derived from one
// builder is what stops them drifting apart (§1.3 D19).

// containerScanPath is where a scanned directory is mounted inside the tool's
// container, so the argument passed to the tool is never the host path.
const containerScanPath = "/scan"

// dockerSocketMount lets a containerised Trivy inspect images held by the host
// daemon. It is a broad grant, so it is only made when there is no Trivy server
// to do the work instead.
const dockerSocketMount = "/var/run/docker.sock:/var/run/docker.sock:ro"

// serverAddr normalises the configured Trivy server address, refusing one Trivy
// cannot use.
//
// Trivy parses this as a URL and fails the entire scan when it cannot. A single
// stray ":" in the field — which is easy to get, since a bare ":" typed while
// the field has focus is a character rather than the command line (§3.7) —
// produces
//
//	FATAL flag error: unable to convert flags to options:
//	invalid server address format: parse ":": missing protocol scheme
//
// a message naming neither DevDesk nor the setting it came from. Worse, the
// field is persisted on every option toggle, so one keystroke breaks every
// later scan from every view until it is found in the config file.
//
// Surrounding space is not an address, so it trims to nothing and client-server
// mode is simply off. Anything else that is not an absolute URL is refused
// here, where the message can say where to fix it.
// ValidateTrivyServer reports whether a Trivy server address can be used, so a
// form can refuse it while the user is still looking at the field rather than
// letting the failure surface as a scan warning minutes later.
func ValidateTrivyServer(server string) error {
	_, err := serverAddr(server)
	return err
}

func serverAddr(server string) (string, error) {
	trimmed := strings.TrimSpace(server)
	if trimmed == "" {
		return "", nil
	}
	// Hostname() rather than Host: "http://:" parses with a Host of ":" and no
	// hostname at all, which is not somewhere a request can be sent.
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf(
			"trivy server address %q is not a URL (want http://host:port) — fix or clear scan.trivy_server", trimmed)
	}
	return trimmed, nil
}

// trivyArgs builds a vulnerability or license scan.
func trivyArgs(target string, targetType TargetType, licenseMode bool, tool ToolSpec,
	server string, ignoreUnfixed, ignoreEOL bool) (toolCmd, error) {
	server, err := serverAddr(server)
	if err != nil {
		return toolCmd{}, err
	}

	var args []string

	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "json"}
		if licenseMode {
			args = append(args, "--scanners", "license")
		} else {
			args = append(args, "--scanners", "vuln")
		}
	case TargetImage:
		// --scanners vuln explicitly: Trivy's default for an image is
		// "vuln,secret", so this stage was running the secret scanner on every
		// image scan and throwing its output away. Secrets are the secret
		// stage's job, and asking for them twice would report each one twice.
		args = []string{"image", "--format", "json", "--scanners", "vuln"}
	default:
		return toolCmd{}, fmt.Errorf("unsupported target type: %s", targetType)
	}

	if server != "" {
		args = append(args, "--server", server)
	}
	if ignoreUnfixed {
		args = append(args, "--ignore-unfixed")
	}
	if ignoreEOL {
		args = append(args, "--ignore-status", "end_of_life")
	}

	return wrapTrivy(args, target, targetType, tool, server), nil
}

// trivySecretArgs builds a secret scan. Like misconfiguration, it reads content
// rather than a package manifest, so it applies to both target types — which is
// what gives an image scan a secret stage at all, Gitleaks being directory-only.
func trivySecretArgs(target string, targetType TargetType, tool ToolSpec, server string) (toolCmd, error) {
	server, err := serverAddr(server)
	if err != nil {
		return toolCmd{}, err
	}

	var args []string

	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "json", "--scanners", "secret"}
	case TargetImage:
		args = []string{"image", "--format", "json", "--scanners", "secret"}
	default:
		return toolCmd{}, fmt.Errorf("unsupported target type: %s", targetType)
	}

	if server != "" {
		args = append(args, "--server", server)
	}

	// No --ignore-status: end_of_life describes a package's support window and
	// says nothing about a secret sitting in a file.
	return wrapTrivy(args, target, targetType, tool, server), nil
}

// trivyMisconfigArgs builds a misconfiguration scan. It reads configuration
// files rather than a package manifest, so it applies to both target types.
func trivyMisconfigArgs(target string, targetType TargetType, tool ToolSpec,
	server string, ignoreEOL bool) (toolCmd, error) {
	server, err := serverAddr(server)
	if err != nil {
		return toolCmd{}, err
	}

	var args []string

	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "json", "--scanners", "misconfig"}
	case TargetImage:
		args = []string{"image", "--format", "json", "--scanners", "misconfig"}
	default:
		return toolCmd{}, fmt.Errorf("unsupported target type: %s", targetType)
	}

	if server != "" {
		args = append(args, "--server", server)
	}
	if ignoreEOL {
		args = append(args, "--ignore-status", "end_of_life")
	}

	return wrapTrivy(args, target, targetType, tool, server), nil
}

// wrapTrivy turns tool arguments into the invocation to run: either trivy
// directly, or docker run with the target mounted and the tool arguments
// appended after the image.
func wrapTrivy(args []string, target string, targetType TargetType, tool ToolSpec, server string) toolCmd {
	if tool.Source != ToolSourceDocker {
		return toolCmd{Name: trivyBinary(tool), Args: append(args, target)}
	}

	dockerArgs := []string{"run", "--rm"}
	if targetType == TargetDirectory {
		dockerArgs = append(dockerArgs, "-v", target+":"+containerScanPath+":ro")
		args = append(args, containerScanPath)
	} else {
		if server == "" {
			dockerArgs = append(dockerArgs, "-v", dockerSocketMount)
		}
		args = append(args, target)
	}
	dockerArgs = append(dockerArgs, trivyImage(tool.Image))
	return toolCmd{Name: "docker", Args: append(dockerArgs, args...)}
}

// trivyBinary is what to exec for a binary-source scan: the resolved path when
// detection found one, else the plain name. The fallback keeps the pure arg
// builders usable from tests that never ran detection.
func trivyBinary(tool ToolSpec) string {
	if tool.Binary != "" {
		return tool.Binary
	}
	return "trivy"
}

func gitleaksBinary(tool ToolSpec) string {
	if tool.Binary != "" {
		return tool.Binary
	}
	return "gitleaks"
}

func trivyImage(image string) string {
	if image == "" {
		return DefaultTrivyImage
	}
	return image
}

func gitleaksImage(image string) string {
	if image == "" {
		return DefaultGitleaksImage
	}
	return image
}
