// Package version reports which build of DevDesk is running.
//
// Three sources answer the question, and they are not equally trustworthy:
//
//  1. the `-ldflags` set at build time — what goreleaser does on release and
//     what `mise run build` does locally;
//  2. the VCS metadata the compiler embeds on its own (`-buildvcs=auto`),
//     which gives the commit but never a version number;
//  3. the module version of a `go install pkg@version`, which Go records in
//     `Main.Version` and which nothing else knows about.
//
// Get consults them in that order. A binary built outside any repo and
// without a flag therefore does not answer "v0.0.0" — it answers "dev",
// because not knowing and claiming a version are two different things.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Set by -ldflags -X. Leaving them empty rather than giving them a false
// value is what lets Get know it must look elsewhere.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// Unknown is what a field reads when no source could answer it.
const Unknown = "unknown"

// DevVersion is the version of a build that carries no version number.
const DevVersion = "dev"

// Info is one build's identity.
type Info struct {
	Version  string // "v0.2.0", or DevVersion
	Commit   string // short SHA, or Unknown
	Date     string // RFC 3339 build date, or Unknown
	Go       string // toolchain that compiled it
	Platform string // GOOS/GOARCH
	Dirty    bool   // built from a tree with uncommitted changes
}

// Released reports whether this build carries a version number rather than
// being a working-tree build. It is the one distinction that changes how the
// rest should be read: on a dev build the commit is the only reliable field.
func (i Info) Released() bool { return i.Version != DevVersion }

// Short is the one-line form for a header — "v0.2.0", or "dev+a1b2c3d" when
// there is no version but a commit to name instead.
func (i Info) Short() string {
	if i.Released() {
		return i.Version
	}
	if i.Commit != Unknown {
		return i.Version + "+" + i.Commit
	}
	return i.Version
}

// Get resolves the running build's identity.
func Get() Info {
	info := Info{
		Version:  version,
		Commit:   commit,
		Date:     date,
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}

	if build, ok := debug.ReadBuildInfo(); ok {
		info.fillFromBuildInfo(build)
	}

	if info.Commit == "" {
		info.Commit = Unknown
	}
	if info.Date == "" {
		info.Date = Unknown
	}
	return info
}

// fillFromBuildInfo fills in what the ldflags did not say.
//
// Nothing here overwrites a value set at build time: a `-X` is a deliberate
// assertion, the VCS metadata is a fallback. That is also what makes the
// function safe where the VCS does not answer — a worktree created on the
// sandbox side, where `-buildvcs=auto` degrades silently instead of failing.
func (i *Info) fillFromBuildInfo(build *debug.BuildInfo) {
	// `go install pkg@v0.2.0` records the module version here, and it is the
	// only case where it exists without ldflags. "(devel)" is what Go returns
	// for a local build, which names nothing more than DevVersion.
	if i.Version == DevVersion && build.Main.Version != "" && build.Main.Version != "(devel)" {
		i.Version = build.Main.Version
	}

	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			if i.Commit == "" {
				i.Commit = shortSHA(setting.Value)
			}
		case "vcs.time":
			if i.Date == "" {
				i.Date = setting.Value
			}
		case "vcs.modified":
			i.Dirty = setting.Value == "true"
		}
	}
}

// shortSHA shortens a full SHA to seven characters, the length git renders.
func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
