package scan

import (
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/git"
)

// Which repositories plumber grades, decided in one place.
//
// A context targets one forge and one only (§3.6), so it has one session and
// one token: a GitHub context grades its GitHub repositories, a GitLab context
// its GitLab ones, and a repository of any other host is **not scannable**
// rather than scanned without credentials. That is §3.17's rule — a personal
// access token authenticates one host, and workspaces_dir holds whatever the
// user has cloned — asked here for a second reason.
//
// It lives in this package rather than in the two views that start scans,
// because two copies of a rule is what D26 was: only the form set IgnoreEOL, so
// the option applied from one screen and silently did not from the others.

// ciOptions decides whether plumber runs on this target, and with what.
//
// It is resolved **per target, not per batch**: one batch holds repositories
// with different remotes and different branches, so a single set of options for
// all of them would grade the wrong thing — or grade a repository of another
// forge, which is the whole point of the rule.
//
// The remote is read here rather than passed in because this is the only place
// that needs it and the two views that start scans would otherwise each carry
// the rule. Reading it costs one `git` call per repository, and only when the
// setting is on.
func (s *Scanner) ciOptions(target string, targetType TargetType) (PlumberOptions, bool) {
	// An image has no pipeline, so there is nothing to grade.
	if !s.options.EnableCIScore || targetType != TargetDirectory || !s.deps.PlumberAvailable {
		return PlumberOptions{}, false
	}
	status, err := git.ReadStatus(target)
	if err != nil {
		// Not a repository: not a failure of the scan, just nothing to grade.
		return PlumberOptions{}, false
	}
	return ciOptionsFor(s.options, status.Remote, status.Branch, s.options.LoadForgeToken)
}

// ciOptionsFor is the rule itself, separated from the git read so a test can
// state it without a repository on disk.
func ciOptionsFor(o ScanOptions, remoteURL, branch string, loadToken func() string) (PlumberOptions, bool) {
	if !git.SameHost(remoteURL, o.Forge.URL) {
		return PlumberOptions{}, false
	}

	opts := PlumberOptions{
		Provider:   plumberProvider(o.Forge.Type),
		Branch:     branch,
		ConfigPath: o.PlumberConfig,
	}
	if loadToken != nil {
		opts.Token = loadToken()
	}
	// Only the GitLab path takes an instance URL. Passing one on the GitHub
	// path is what makes plumber choose the GitLab path — the flag is part of
	// how it decides — so it would send a GitHub context to a GitLab it has no
	// token for.
	if opts.Provider == providerGitLab {
		opts.GitLabURL = o.Forge.URL
	}
	return opts, true
}

// plumberProvider maps the configured forge onto --provider.
//
// Declared, never sniffed: the context says which forge it targets, so plumber
// is told rather than left to guess from the remote. An unrecognised type falls
// back to GitLab for applyDefaults' reason — it has already normalised the
// value, and the one backend that has always existed beats an error nothing can
// act on.
func plumberProvider(forgeType string) string {
	if forgeType == config.ForgeGitHub {
		return providerGitHub
	}
	return providerGitLab
}
