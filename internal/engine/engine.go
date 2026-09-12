// Package engine names the container engine DevDesk drives — docker or podman —
// and carries everything about it that is not the same between the two.
//
// It exists as its own package rather than as a field of internal/docker
// because internal/scan needs the same answer and does not import
// internal/docker. That is the internal/forge arrangement read one level down:
// the thing every caller shares does not live inside one of its callers.
//
// What a Shape carries is what measurement, not taste, put there (§3.67):
//
//   - the binary, which is the cheap part — internal/docker already funnels
//     every invocation through one runner;
//   - the credential helper prefix and the auth file, because that file is read
//     *and written* directly rather than through the CLI;
//   - the host socket, which is the one path that breaks rather than moves:
//     rootless podman has no /var/run/docker.sock, and its own socket exists
//     only while `podman system service` runs;
//   - the output templates, which are the silent risk. See Templates.
//
// Nothing here sniffs. A shape is declared, the way a forge's is and the way a
// registry's `provider` is (§3.8): a thing is treated as what it says it is.
package engine

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Shape is one container engine: what to run, where it keeps its credentials,
// and how to ask it for machine-readable output.
type Shape struct {
	// Name identifies the engine for a config discriminator, a log line, and
	// the word the views show — "docker", "podman".
	Name string

	// Binary is what gets executed. It is the engine's own name for the two
	// declared shapes, and an explicit path when the configuration names one.
	Binary string

	// HelperPrefix is what a credential helper's name is prefixed with:
	// `docker-credential-osxkeychain`, `podman-credential-…`. The protocol is
	// shared, the naming is not.
	HelperPrefix string

	// Templates are the --format strings for every output this application
	// parses.
	Templates Templates
}

// A Shape carries no resolved filesystem path, and that is deliberate: the
// credential file and the socket are looked up through AuthPaths(), AuthPath()
// and HostSocket() at the moment they are needed.
//
// Storing them on the struct was tried and reverted. The shape is built once,
// at package initialisation, so a $HOME or $XDG_RUNTIME_DIR read there is
// frozen before anything else runs — and the socket is worse, since whether it
// exists is the entire question and it can appear (`podman system service`) or
// vanish while the application is up. A cached answer to "where is the file"
// is a cached answer to "does it exist", which is not a thing a shape can know
// once and for all.

// Templates holds one Go template per parsed output.
//
// They are a field of the shape rather than constants at the call sites for a
// reason that has not been measured away: podman implements --format, but
// equality of the *fields* is guaranteed nowhere, and {{.Ports}} and
// {{.CreatedAt}} are the likeliest to diverge. A divergence raises no error —
// it produces wrong rows, which is the defect class this repository keeps
// cataloguing (D24, D25, D57).
//
// Both shapes therefore declare the same templates today, and that is the
// honest state: no measurement has been taken against a real podman. What this
// indirection buys is that a divergence found later is one line here rather
// than a refactor. Do not collapse it back into constants without that
// measurement (§3.67).
//
// The twelfth parsed output has no template and is not here: `system df -v` is
// a fixed-width table scraped by column offsets (internal/docker/images.go),
// because no --format exposes per-image unique size. See ParsesSystemDFVerbose.
type Templates struct {
	// PS lists containers: id, name, image, state, status, created, ports.
	PS string
	// PSState asks for nothing but the state column, to count containers.
	PSState string
	// Stats is the live resource sample, taken with --no-stream.
	Stats string
	// Info is the host's capacity: CPU count and total memory.
	Info string
	// ImageLS lists images: id, repository, tag, size, created.
	ImageLS string
	// ImageInspect reads the sizes of several images in one call.
	ImageInspect string
	// ImageExposedPorts reads an image's EXPOSE declarations as JSON.
	ImageExposedPorts string
	// NetworkLS lists networks: id, name, driver, scope, created.
	NetworkLS string
	// VolumeLS lists volumes: name, driver, mountpoint.
	VolumeLS string
	// SystemDF is the disk report, one row per resource family.
	SystemDF string
	// Version is the client version, for the dashboard's tools table.
	Version string
}

// ParsesSystemDFVerbose reports whether `<engine> system df -v` prints the
// fixed-width "Images space usage:" table internal/docker scrapes for per-image
// unique size and container count.
//
// Only docker is known to. Under podman the columns are left empty rather than
// filled from offsets taken on a header that may not be the same one: a blank
// column reads as an absence, a value sliced out of the wrong column does not.
func (s Shape) ParsesSystemDFVerbose() bool { return s.Name == Docker }

// Engine names, shared with the configuration. They are duplicated in
// internal/config rather than imported from it so that config does not depend
// on this package; TestEveryConfiguredEngineHasAShape holds the two in step,
// the same arrangement the forge names have (§3.6).
const (
	// Docker is what `auto` resolves to when both engines are installed, and
	// what an unrecognised name falls back to.
	Docker = "docker"
	Podman = "podman"
)

// Auto asks for whichever engine is installed. It is the default, so that
// nobody who installed exactly one of the two is asked which one they meant —
// app.secret_backend's precedent (§3.9).
const Auto = "auto"

// dockerTemplates is the set every call in this application was written
// against. podmanTemplates is a copy, and stays a copy until the measurement
// described on Templates has been made.
var dockerTemplates = Templates{
	PS:                "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Ports}}",
	PSState:           "{{.State}}",
	Stats:             "{{.ID}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}",
	Info:              "{{.NCPU}}\t{{.MemTotal}}",
	ImageLS:           "{{.ID}}\t{{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}",
	ImageInspect:      "{{.ID}}\t{{.Size}}",
	ImageExposedPorts: "{{json .Config.ExposedPorts}}",
	NetworkLS:         "{{.ID}}\t{{.Name}}\t{{.Driver}}\t{{.Scope}}\t{{.CreatedAt}}",
	VolumeLS:          "{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}",
	SystemDF:          "{{.Type}}\t{{.TotalCount}}\t{{.Size}}\t{{.Reclaimable}}",
	Version:           "{{.Client.Version}}",
}

var podmanTemplates = dockerTemplates

// current is the resolved engine. It starts as docker so that every caller has
// a usable answer before the router has read a configuration — which is what
// the application did before this package existed, and what a test that never
// resolves anything should keep seeing.
var (
	currentMu sync.RWMutex
	current   = shapeFor(Docker, Docker)
)

// Current returns the engine in use.
func Current() Shape {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}

// SetCurrent installs the resolved engine. The router calls it once at startup
// and once per context switch, from Update — never from a Cmd (Rule 110).
func SetCurrent(s Shape) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = s
}

// Names lists the engines a shape is declared for, in the order the
// configuration view cycles them.
func Names() []string { return []string{Docker, Podman} }

// Resolve turns an app.container_engine value into the engine to drive.
//
// The four cases:
//
//	""/"auto"  docker if it is on PATH, else podman, else an error naming both
//	"docker"   pinned; absent is an error
//	"podman"   pinned; absent is an error
//	anything   taken as a path to a binary
//
// A pinned engine that is not installed is an error rather than a fall back to
// the other one. That is credentials.Select's rule (§3.9), and for the same
// reason: someone who asked for podman should not silently get docker, because
// the two do not hold the same containers.
func Resolve(preference string) (Shape, error) {
	switch preference {
	case "", Auto:
		for _, name := range Names() {
			if _, err := exec.LookPath(name); err == nil {
				return shapeFor(name, name), nil
			}
		}
		return Shape{}, fmt.Errorf("no container engine found: neither %s nor %s is on PATH", Docker, Podman)

	case Docker, Podman:
		if _, err := exec.LookPath(preference); err != nil {
			return Shape{}, fmt.Errorf("%s not found: %w", preference, err)
		}
		return shapeFor(preference, preference), nil
	}

	// Anything else is a path. The engine it belongs to is read from the file
	// name — the one place this package guesses, and it guesses about a value
	// the user typed rather than about the world. Getting it wrong costs the
	// auth file location, not the invocation.
	path := preference
	if _, err := exec.LookPath(path); err != nil {
		return Shape{}, fmt.Errorf("container engine %q not found: %w", path, err)
	}
	return shapeFor(engineOfPath(path), path), nil
}

// engineOfPath reads an engine name out of a configured path. Everything that
// is not recognisably podman is treated as docker, which is what an unknown
// docker-compatible CLI (nerdctl, a wrapper script) behaves like.
func engineOfPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	base = strings.TrimSuffix(base, ".exe")
	if strings.HasPrefix(base, Podman) {
		return Podman
	}
	return Docker
}

// shapeFor builds the shape for one engine, running the given binary.
func shapeFor(name, binary string) Shape {
	if name == Podman {
		return Shape{
			Name:         Podman,
			Binary:       binary,
			HelperPrefix: "podman-credential-",
			Templates:    podmanTemplates,
		}
	}
	return Shape{
		Name:         Docker,
		Binary:       binary,
		HelperPrefix: "docker-credential-",
		Templates:    dockerTemplates,
	}
}

// ShapeFor returns the declared shape for an engine name, running that name as
// its binary. It answers the configuration view, which needs the shapes before
// anything has been resolved — internal/forge's reason for holding its shapes
// in a table rather than on its backends.
func ShapeFor(name string) Shape { return shapeFor(name, name) }
