package docker

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
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
	MountUnverified   MountCategory = "unverified"    // a path DevDesk could not read, so could not prove harmless
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
	MountUnverified:   "DevDesk could not check where this path leads, so it may expose this host.",
}

// maxLinkFollows bounds the symbolic links followed for one path, as the kernel
// does (ELOOP at 40), so a loop of links ends.
const maxLinkFollows = 40

// SensitiveMounts returns the bind mounts of opts that expose something
// sensitive of the host, the -v entries first and then the --mount values, each
// in the order given; nil when there is none. A bind source is a path (it starts
// with "/", "." or "~", as the Docker CLI reads it); a named volume, an anonymous
// volume, a tmpfs and the container side of a mount are never checked. A --mount
// reads its keys and its type in any case (the engine lowercases them), counts
// type=glob as a bind, and reads volume-opt=device=<path> of a volume as a source.
//
// A source is flagged when it is equal to or under a sensitive path, or when it
// is an ancestor of one (/run holds the engine's socket, /home holds HOME), with
// the category of what it holds. A leading "~" is read as HOME although the
// engine does not expand it, and a relative source is taken from the working
// directory of DevDesk. Any source that is a socket file is read as an engine
// socket, wherever it is and whatever it is called.
//
// The path is resolved before it is compared, and read both ways a ".." after a
// link can be read: with the link followed first, as the kernel does, and with
// the path cleaned first, as the Docker CLI does; either may be the sensitive
// one. A path that cannot be resolved because it does not exist is compared as
// written after normalization and as resolved so far; one that cannot be read
// for another reason (permission denied) is flagged MountUnverified unless it is
// sensitive anyway. /proc/<pid>/root and /proc/<pid>/cwd are the host root.
// It reads HOME (the user database when it is unset or relative, for the home
// categories only), XDG_RUNTIME_DIR, DOCKER_HOST, CONTAINER_HOST and the working
// directory at the time of the call, and the file system for links and sockets;
// it starts nothing, creates nothing and never fails. Windows is not covered: it
// returns nil there, and macOS is read with the Linux layout.
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
		for _, source := range mountSources(spec) {
			if m, flagged := h.check(spec, source); flagged {
				found = append(found, m)
				break
			}
		}
	}
	return found
}

func volumeSource(spec string) (string, bool) {
	source, _, ok := strings.Cut(spec, ":")
	if !ok {
		return "", false
	}
	source = strings.TrimSpace(source)
	return source, isHostPath(source)
}

// mountSources returns the host paths a --mount value binds. Keys and the value
// of type are read in any case. type=bind (and Podman's type=glob) binds its
// source (or src); type=volume, or no type, binds each volume-opt=device=<path>
// (the local driver's bind in disguise). The target (target, dst, destination)
// is the container's and never read.
func mountSources(spec string) []string {
	var typ, source string
	var devices []string
	for tok := range strings.SplitSeq(spec, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(tok), "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "type":
			typ = strings.ToLower(value)
		case "source", "src":
			source = value
		case "volume-opt":
			if name, device, ok := strings.Cut(value, "="); ok && strings.EqualFold(strings.TrimSpace(name), "device") {
				devices = append(devices, strings.TrimSpace(device))
			}
		}
	}
	var out []string
	switch typ {
	case "bind", "glob":
		if isHostPath(source) {
			out = append(out, source)
		}
	case "", "volume":
		for _, d := range devices {
			if isHostPath(d) {
				out = append(out, d)
			}
		}
	}
	return out
}

// isHostPath reports whether a bind source names a host path rather than a
// volume: a volume name starts with a letter or a digit, and the Docker CLI makes
// any source that starts with "." absolute.
func isHostPath(source string) bool {
	return strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~")
}

// procHostRoot matches /proc/<pid>/root and /proc/<pid>/cwd (also self and
// thread-self) and what is under them: the root and the working directory of a
// process are reached through its /proc entry, and the root of PID 1 is the
// host's whatever the permissions say.
var procHostRoot = regexp.MustCompile(`^/proc/(?:[0-9]+|self|thread-self)/(?:root|cwd)(?:/|$)`)

// mountTarget is a sensitive path and the category of what it holds.
type mountTarget struct {
	path     string
	category MountCategory
}

// mountHost is what a classification reads from the machine, once per call.
type mountHost struct {
	home    string          // HOME as set, for the expansion of "~"; "" when unset or not absolute
	homes   []string        // the home of the user and its resolved form: HOME, or the user database when HOME is unusable
	cwd     string          // working directory, "" when unreadable
	etc     []string        // "/etc" and its resolved form
	ssh     []string        // ~/.ssh as written and as resolved (it is often a link)
	devdesk []string        // ~/.devdesk as written and as resolved
	sockets map[string]bool // engine socket paths, as written and resolved
	targets []mountTarget   // the sensitive paths an ancestor is flagged for, in order of precedence
}

func newMountHost() mountHost {
	h := mountHost{sockets: map[string]bool{}}
	if home := os.Getenv("HOME"); path.IsAbs(home) {
		h.home = path.Clean(home)
	}
	base := h.home
	if base == "" {
		base = fallbackHome()
	}
	if base != "" {
		h.homes = withResolved(base)
		for _, d := range h.homes {
			h.ssh = appendMissing(h.ssh, withResolved(path.Join(d, ".ssh"))...)
			h.devdesk = appendMissing(h.devdesk, withResolved(path.Join(d, ".devdesk"))...)
		}
	}
	if cwd, err := os.Getwd(); err == nil && path.IsAbs(cwd) {
		h.cwd = cwd
	}
	h.etc = withResolved("/etc")
	for _, sock := range engineSockets() {
		for _, p := range withResolved(sock) {
			h.sockets[p] = true
			h.targets = append(h.targets, mountTarget{p, MountEngineSocket})
		}
	}
	for _, group := range []struct {
		paths    []string
		category MountCategory
	}{
		{h.etc, MountSystemConfig}, {h.homes, MountHome}, {h.ssh, MountSSH}, {h.devdesk, MountDevDesk},
	} {
		for _, p := range group.paths {
			h.targets = append(h.targets, mountTarget{p, group.category})
		}
	}
	return h
}

// fallbackHome is the home of the user when HOME says nothing usable: the home
// directory of the environment, then the user database. It serves the home
// categories only; "~" is never expanded with it.
func fallbackHome() string {
	if d, err := os.UserHomeDir(); err == nil && path.IsAbs(d) {
		return path.Clean(d)
	}
	if u, err := user.Current(); err == nil && path.IsAbs(u.HomeDir) {
		return path.Clean(u.HomeDir)
	}
	return ""
}

// appendMissing appends each of add that dst does not hold yet.
func appendMissing(dst []string, add ...string) []string {
	for _, a := range add {
		if !slices.Contains(dst, a) {
			dst = append(dst, a)
		}
	}
	return dst
}

// withResolved returns p cleaned and, when different, as far as its links resolve.
func withResolved(p string) []string {
	out := []string{path.Clean(p)}
	if r := resolvePath(p); r.path != out[0] {
		out = append(out, r.path)
	}
	return out
}

// engineSockets lists where a container engine listens: Docker, Podman and
// containerd at their usual places, under XDG_RUNTIME_DIR (or /run/user/<uid>
// when it is unset) for the rootless ones, and the unix socket of DOCKER_HOST or
// CONTAINER_HOST. A tcp:// or ssh:// host names no file.
func engineSockets() []string {
	socks := []string{
		"/var/run/docker.sock", "/run/docker.sock",
		"/var/run/podman/podman.sock", "/run/podman/podman.sock",
		"/run/containerd/containerd.sock",
	}
	xdg := os.Getenv("XDG_RUNTIME_DIR")
	if !path.IsAbs(xdg) && os.Getuid() >= 0 {
		xdg = "/run/user/" + strconv.Itoa(os.Getuid())
	}
	if path.IsAbs(xdg) {
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

// check classifies one bind source. The candidates are the paths it can lead to:
// for each reading of a ".." after a link (the link followed first, as the kernel
// does, and the path cleaned first, as the Docker CLI does) the path as resolved
// when it resolves in full, otherwise as written and then as resolved so far (a
// link into /etc followed by a name that does not exist yet is /etc's). The
// first candidate that is equal to or under a sensitive path wins, then the
// first that holds one, then a path that could not be read.
func (h mountHost) check(spec, source string) (SensitiveMount, bool) {
	abs, ok := h.absolute(source)
	if !ok {
		return SensitiveMount{}, false
	}
	clean := path.Clean(abs)
	flag := func(p string, cat MountCategory) (SensitiveMount, bool) {
		return SensitiveMount{
			Spec: spec, Source: source, Path: p, Category: cat,
			Reason: mountReasons[cat], EngineSocket: cat == MountEngineSocket,
		}, true
	}
	if procHostRoot.MatchString(clean) {
		return flag(clean, MountRoot)
	}
	var candidates []string
	unverified := false
	for _, reading := range appendMissing(nil, abs, clean) {
		r := resolvePath(reading)
		unverified = unverified || r.unverified
		if r.complete {
			candidates = appendMissing(candidates, r.path)
		} else {
			candidates = appendMissing(candidates, path.Clean(reading), r.path)
		}
	}
	for _, c := range candidates {
		if cat, flagged := h.classify(c); flagged {
			return flag(c, cat)
		}
	}
	for _, c := range candidates {
		if cat, flagged := h.holds(c); flagged {
			return flag(c, cat)
		}
	}
	if unverified {
		return flag(clean, MountUnverified)
	}
	return SensitiveMount{}, false
}

// classify returns the category of a resolved absolute path that is equal to or
// under a sensitive one, or is a socket file (any socket may be an engine's).
func (h mountHost) classify(p string) (MountCategory, bool) {
	switch {
	case h.sockets[p]:
		return MountEngineSocket, true
	case p == "/":
		return MountRoot, true
	case underAny(p, h.etc):
		return MountSystemConfig, true
	case underAny(p, h.ssh):
		return MountSSH, true
	case underAny(p, h.devdesk):
		return MountDevDesk, true
	case slices.Contains(h.homes, p):
		return MountHome, true
	}
	if info, err := os.Stat(p); err == nil && info.Mode()&fs.ModeSocket != 0 {
		return MountEngineSocket, true
	}
	return "", false
}

// holds returns the category of the first sensitive path that p is an ancestor
// of: mounting /run hands over /run/docker.sock.
func (h mountHost) holds(p string) (MountCategory, bool) {
	for _, t := range h.targets {
		if strings.HasPrefix(t.path, p+"/") {
			return t.category, true
		}
	}
	return "", false
}

// underAny reports whether p is equal to or under one of the bases. Whole
// components only: "/etcetera" is not under "/etc".
func underAny(p string, bases []string) bool {
	for _, base := range bases {
		if p == base || strings.HasPrefix(p, base+"/") {
			return true
		}
	}
	return false
}

// resolution is what resolvePath found.
type resolution struct {
	path       string // the path with its links followed as far as they could be
	complete   bool   // every component exists
	unverified bool   // a component could not be read for a reason other than "does not exist"
}

// isMissing reports whether err says the path is not there (a component that
// does not exist, or a file where a directory was expected).
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// resolvePath resolves an absolute path component by component, following each
// link where it stands, so that a ".." after a link applies to the link's target
// as the kernel does (a Clean first would apply it to the link's name). A
// component that does not exist or cannot be read is kept as written, and so is
// everything after it; the result says which of the two it was. Too many links
// (a loop) give the path cleaned as written, incomplete and not unverified.
func resolvePath(p string) resolution {
	rest := pathComponents(p)
	var cur []string
	r := resolution{complete: true}
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
		if r.complete {
			next := "/" + strings.Join(append(slices.Clone(cur), c), "/")
			info, err := os.Lstat(next)
			switch {
			case err != nil:
				r.complete, r.unverified = false, !isMissing(err)
			case info.Mode()&os.ModeSymlink != 0:
				follows++
				if follows > maxLinkFollows {
					return resolution{path: path.Clean(p)}
				}
				target, err := os.Readlink(next)
				if err != nil {
					r.complete, r.unverified = false, !isMissing(err)
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
	r.path = "/" + strings.Join(cur, "/")
	return r
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
