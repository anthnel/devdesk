package docker

import (
	"os"
	"path"
	"runtime"
	"slices"
	"strings"
)

// OC-20 (DEVDESK-818, S4): a bind mount can hand the container the whole host,
// its keys or the engine that runs it. SensitiveMounts classifies what a launch
// asks to mount so the form can ask first; it is a reader and decides nothing:
// LaunchContainer and BuildLaunchCmd launch what they are given.

// MountCategory names why a bind mount is sensitive.
type MountCategory string

const (
	MountRoot         MountCategory = "root"          // "/"
	MountSystemConfig MountCategory = "system-config" // "/etc" and what is under it
	MountHome         MountCategory = "home"          // $HOME itself
	MountSSH          MountCategory = "ssh"           // ~/.ssh and what is under it
	MountDevDesk      MountCategory = "devdesk"       // ~/.devdesk and what is under it
	MountEngineSocket MountCategory = "engine-socket" // the socket of a container engine
)

// SensitiveMount is one bind mount of a launch that SensitiveMounts flags.
type SensitiveMount struct {
	Spec         string        // the whole entry as typed: "~/.ssh:/root/.ssh:ro"
	Source       string        // its host side as typed: "~/.ssh"
	Path         string        // the host path after expansion, normalization and symlink resolution
	Category     MountCategory // why it is sensitive
	Reason       string        // one sentence, fixed per category, never built from what was typed
	EngineSocket bool          // true exactly for MountEngineSocket
}

// mountReasons holds the sentence of each category. They are fixed on purpose:
// the typed path may hold a secret, and the modal and the log print the reason.
var mountReasons = map[MountCategory]string{
	MountRoot:         "The whole file system of this host is handed to the container.",
	MountSystemConfig: "The system configuration of this host is exposed.",
	MountHome:         "Your whole home directory, keys and tokens included, is exposed.",
	MountSSH:          "Your SSH keys and configuration are exposed.",
	MountDevDesk:      "The configuration and cache of DevDesk are exposed.",
	MountEngineSocket: "The container gets control of the engine, and so of this host.",
}

// maxLinkFollows bounds the symbolic links followed for one path, as the kernel
// does (ELOOP at 40), so a loop of links ends.
const maxLinkFollows = 40

// SensitiveMounts returns the bind mounts of opts that expose something
// sensitive of the host, the -v entries first and then the --mount values, each
// in the order given; nil when there is none. A bind source is a path (it starts
// with "/", "./", "../" or "~", or is "." or ".."); a named volume, an anonymous
// volume, a tmpfs and the container side of a mount are never checked. A source
// is flagged when it is equal to or under a sensitive path: an ancestor (/home)
// is not. A leading "~" is read as HOME although the engine does not expand it,
// and a relative source is taken from the working directory of DevDesk.
//
// The path is resolved before it is compared: links are followed component by
// component, and a ".." after a link applies to the link target, as the kernel
// does. A path that cannot be resolved (a missing component, a loop) is compared
// as written after normalization, and, when part of it resolves, as resolved so
// far. It reads HOME, XDG_RUNTIME_DIR, DOCKER_HOST, CONTAINER_HOST and the
// working directory at the time of the call, and the file system for links; it
// starts nothing, creates nothing and never fails. Windows is not covered: it
// returns nil there (the paths and the engine sockets differ), and macOS is
// read with the Linux layout.
func SensitiveMounts(opts ContainerLaunchOptions) []SensitiveMount {
	if runtime.GOOS == "windows" {
		return nil
	}
	h := newMountHost()
	var found []SensitiveMount
	for _, v := range opts.Volumes {
		spec := strings.TrimSpace(v)
		if source, ok := volumeSource(spec); ok {
			if m, flagged := h.check(spec, source); flagged {
				found = append(found, m)
			}
		}
	}
	for _, v := range opts.Mounts {
		spec := strings.TrimSpace(v)
		if source, ok := mountSource(spec); ok {
			if m, flagged := h.check(spec, source); flagged {
				found = append(found, m)
			}
		}
	}
	return found
}

// volumeSource returns the host side of a -v entry ("source:target[:options]"),
// and whether it is a host path. An entry with no colon is an anonymous volume
// whose one path is the container's.
func volumeSource(spec string) (string, bool) {
	source, _, ok := strings.Cut(spec, ":")
	if !ok {
		return "", false
	}
	source = strings.TrimSpace(source)
	return source, isHostPath(source)
}

// mountSource returns the source of a --mount value of type bind
// ("type=bind,source=...,target=...", also src), and whether it is a host path.
// Any order of the keys; the target (target, dst, destination) is the container's.
func mountSource(spec string) (string, bool) {
	var typ, source string
	for tok := range strings.SplitSeq(spec, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(tok), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "type":
			typ = strings.TrimSpace(value)
		case "source", "src":
			source = strings.TrimSpace(value)
		}
	}
	return source, typ == "bind" && isHostPath(source)
}

// isHostPath reports whether a bind source names a host path rather than a
// volume: a name never starts with "/", "." or "~".
func isHostPath(source string) bool {
	return strings.HasPrefix(source, "/") || strings.HasPrefix(source, "./") ||
		strings.HasPrefix(source, "../") || strings.HasPrefix(source, "~") ||
		source == "." || source == ".."
}

// mountHost is what a classification reads from the machine, once per call.
type mountHost struct {
	home    string          // HOME as set, "" when unset or not absolute
	homes   []string        // HOME and its resolved form
	cwd     string          // working directory, "" when unreadable
	etc     []string        // "/etc" and its resolved form
	sockets map[string]bool // engine socket paths, as written and resolved
}

func newMountHost() mountHost {
	h := mountHost{etc: []string{"/etc"}, sockets: map[string]bool{}}
	if home := os.Getenv("HOME"); path.IsAbs(home) {
		h.home = path.Clean(home)
		h.homes = withResolved(h.home)
	}
	if cwd, err := os.Getwd(); err == nil && path.IsAbs(cwd) {
		h.cwd = cwd
	}
	h.etc = withResolved("/etc")
	for _, sock := range engineSockets() {
		for _, p := range withResolved(sock) {
			h.sockets[p] = true
		}
	}
	return h
}

// withResolved returns p cleaned and, when different, as far as its links resolve.
func withResolved(p string) []string {
	out := []string{path.Clean(p)}
	if resolved, _ := resolvePath(p); resolved != out[0] {
		out = append(out, resolved)
	}
	return out
}

// engineSockets lists where a container engine listens: Docker and Podman at
// their usual places, under XDG_RUNTIME_DIR for the rootless ones, and the unix
// socket of DOCKER_HOST or CONTAINER_HOST. A tcp:// or ssh:// host names no file.
func engineSockets() []string {
	socks := []string{
		"/var/run/docker.sock", "/run/docker.sock",
		"/var/run/podman/podman.sock", "/run/podman/podman.sock",
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); path.IsAbs(xdg) {
		socks = append(socks, path.Join(xdg, "docker.sock"), path.Join(xdg, "podman", "podman.sock"))
	}
	for _, name := range []string{"DOCKER_HOST", "CONTAINER_HOST"} {
		if p, ok := strings.CutPrefix(os.Getenv(name), "unix://"); ok && path.IsAbs(p) {
			socks = append(socks, p)
		}
	}
	return socks
}

// absolute returns the host path a source designates, before any resolution:
// "~" and "~/..." under HOME, a relative source under the working directory.
func (h mountHost) absolute(source string) (string, bool) {
	switch {
	case path.IsAbs(source):
		return source, true
	case source == "~" || strings.HasPrefix(source, "~/"):
		if h.home == "" {
			return "", false // nothing to expand it with: not read as anything
		}
		return h.home + strings.TrimPrefix(source, "~"), true
	case h.cwd == "":
		return "", false
	}
	return h.cwd + "/" + source, true
}

// check classifies one bind source. The candidates are the path as resolved
// when it resolves in full; otherwise the path as written, then as resolved so
// far (a link into /etc followed by a name that does not exist yet is /etc's).
func (h mountHost) check(spec, source string) (SensitiveMount, bool) {
	abs, ok := h.absolute(source)
	if !ok {
		return SensitiveMount{}, false
	}
	resolved, complete := resolvePath(abs)
	candidates := []string{resolved}
	if !complete {
		candidates = []string{path.Clean(abs), resolved}
	}
	for _, c := range candidates {
		if cat, flagged := h.classify(c); flagged {
			return SensitiveMount{
				Spec: spec, Source: source, Path: c, Category: cat,
				Reason: mountReasons[cat], EngineSocket: cat == MountEngineSocket,
			}, true
		}
	}
	return SensitiveMount{}, false
}

// classify returns the category of a resolved absolute path, equal-or-under.
func (h mountHost) classify(p string) (MountCategory, bool) {
	switch {
	case h.sockets[p]:
		return MountEngineSocket, true
	case p == "/":
		return MountRoot, true
	case underAny(p, h.etc, ""):
		return MountSystemConfig, true
	case underAny(p, h.homes, ".ssh"):
		return MountSSH, true
	case underAny(p, h.homes, ".devdesk"):
		return MountDevDesk, true
	case slices.Contains(h.homes, p):
		return MountHome, true
	}
	return "", false
}

// underAny reports whether p is equal to or under one of the bases, joined with
// sub when it is not empty. Whole components only: "/etcetera" is not under "/etc".
func underAny(p string, bases []string, sub string) bool {
	for _, base := range bases {
		if sub != "" {
			base = path.Join(base, sub)
		}
		if p == base || strings.HasPrefix(p, base+"/") {
			return true
		}
	}
	return false
}

// resolvePath resolves an absolute path component by component, following each
// link where it stands, so that a ".." after a link applies to the link's target
// as the kernel does (a Clean first would apply it to the link's name). The
// result reports whether every component exists. A component that does not exist
// or cannot be read is kept as written, and so is everything after it. Too many
// links (a loop) give the path cleaned as written and false.
func resolvePath(p string) (string, bool) {
	rest := pathComponents(p)
	var cur []string
	complete := true
	follows := 0
	for len(rest) > 0 {
		c := rest[0]
		rest = rest[1:]
		if c == ".." {
			if len(cur) > 0 {
				cur = cur[:len(cur)-1]
			}
			continue
		}
		if complete {
			next := "/" + strings.Join(append(slices.Clone(cur), c), "/")
			info, err := os.Lstat(next)
			switch {
			case err != nil:
				complete = false
			case info.Mode()&os.ModeSymlink != 0:
				follows++
				target, err := os.Readlink(next)
				if follows > maxLinkFollows {
					return path.Clean(p), false
				}
				if err != nil {
					complete = false
					break
				}
				if path.IsAbs(target) {
					cur = nil
				}
				rest = append(pathComponents(target), rest...)
				continue
			}
		}
		cur = append(cur, c)
	}
	return "/" + strings.Join(cur, "/"), complete
}

// pathComponents splits p on "/" and drops the empty and "." components.
func pathComponents(p string) []string {
	var out []string
	for c := range strings.SplitSeq(p, "/") {
		if c != "" && c != "." {
			out = append(out, c)
		}
	}
	return out
}
