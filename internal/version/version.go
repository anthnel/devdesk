// Package version reports which build of DevDesk is running.
//
// Trois sources répondent à la question, et elles ne se valent pas :
//
//  1. les `-ldflags` posés au build — ce que goreleaser fait en release et ce
//     que `mise run build` fait en local ;
//  2. les métadonnées VCS que le compilateur embarque tout seul
//     (`-buildvcs=auto`), qui donnent le commit mais jamais un numéro de
//     version ;
//  3. la version de module d'un `go install pkg@version`, que Go inscrit dans
//     `Main.Version` et que rien d'autre ne connaît.
//
// Get les consulte dans cet ordre. Un binaire construit hors de tout dépôt et
// sans drapeau ne répond donc pas « v0.0.0 » — il répond "dev", parce que ne
// pas savoir et prétendre à une version sont deux choses différentes.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Posés par -ldflags -X. Les laisser vides plutôt que de leur donner une fausse
// valeur est ce qui permet à Get de savoir qu'il doit chercher ailleurs.
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

// fillFromBuildInfo complète ce que les ldflags n'ont pas dit.
//
// Rien n'y écrase une valeur posée au build : un `-X` est une affirmation
// délibérée, les métadonnées VCS sont un défaut. C'est aussi ce qui rend la
// fonction sûre là où le VCS ne répond pas — un worktree créé côté sandbox, où
// `-buildvcs=auto` se dégrade en silence plutôt que d'échouer.
func (i *Info) fillFromBuildInfo(build *debug.BuildInfo) {
	// `go install pkg@v0.2.0` inscrit la version du module ici, et c'est le
	// seul cas où elle existe sans ldflags. "(devel)" est ce que Go rend pour
	// un build local, ce qui ne nomme rien de plus que DevVersion.
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

// shortSHA abrège un SHA complet à sept caractères, la longueur que git rend.
func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
