package engine

import (
	"os"
	"path/filepath"
)

// dockerHostSocket is the daemon socket a containerised scanner mounts to
// inspect images the host holds. Docker Desktop and a rootful daemon both
// expose it at this path, on every platform the scanners run a container from.
const dockerHostSocket = "/var/run/docker.sock"

// dockerAuthPaths is where `docker login` keeps registry credentials.
//
// A single location: docker has always written ~/.docker/config.json, and
// DOCKER_CONFIG — which can move it — is not read here because nothing in this
// application sets it and honouring it halfway (reading but not writing) would
// be worse than not honouring it.
func dockerAuthPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".docker", "config.json")}
}

// podmanAuthPaths is where `podman login` keeps registry credentials, most
// preferred first.
//
// Podman writes the runtime path when $XDG_RUNTIME_DIR is set, which is the
// ordinary case under a session manager, and falls back to the config path
// otherwise. Both carry the same JSON as docker's file. The runtime one does
// not survive a reboot, which is podman's decision and not this application's
// to correct.
func podmanAuthPaths() []string {
	var paths []string
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		paths = append(paths, filepath.Join(runtime, "containers", "auth.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "containers", "auth.json"))
	}
	return paths
}

// podmanHostSocket is the API socket a containerised scanner would mount to
// reach images podman holds.
//
// Rootless podman has no socket at all unless `podman system service` is
// running, so this reports the path only when it is actually there. An empty
// answer is the fact the scan view reports — the one thing the engine change
// genuinely breaks rather than moves (§3.67).
func podmanHostSocket() string {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime != "" {
		if path := filepath.Join(runtime, "podman", "podman.sock"); exists(path) {
			return path
		}
	}
	// Rootful podman puts it where docker's would be for compatibility.
	if exists("/run/podman/podman.sock") {
		return "/run/podman/podman.sock"
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// AuthPath returns the credential file to use: the first declared one that
// exists, else the first declared one — which is where a login would write.
// Empty when the home directory could not be resolved.
func (s Shape) AuthPath() string {
	for _, path := range s.AuthPaths {
		if exists(path) {
			return path
		}
	}
	if len(s.AuthPaths) == 0 {
		return ""
	}
	return s.AuthPaths[0]
}
