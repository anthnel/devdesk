package security

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/viewer"
)

// pipelineSource is the CI configuration the forge resolves for a repository —
// every include and every component expanded, as the pipeline would run.
//
// It lives here rather than in internal/viewer for sources.go's reason: it
// knows about a forge and a credential store, and the viewer parses documents.
//
// **Why it exists at all.** plumber does not grade the repository's
// `.gitlab-ci.yml`; it grades what the server derives from it. Measured on one
// repository whose file declares a single `include`: fifteen includes resolved,
// twenty-one jobs, nine images, none of them written down anywhere the user can
// open. A finding is therefore about a job the repository does not contain, and
// its own file link points at the `include:` entry that brought it in.
type pipelineSource struct {
	// RepoPath is the working copy on disk. The remote and the branch are read
	// from it at Load time rather than captured: this source is built in
	// Update, and a git call there would be I/O on the render path.
	RepoPath string
	Label    string
	Forge    config.ForgeConfig
	Secrets  credentials.Storage
	// Findings are this scan's CI findings, written into the document as YAML
	// comments on the jobs they are about. The document is opened from the CI
	// tab, so showing where those findings sit is what it is opened for.
	Findings []scan.Finding
}

func (s pipelineSource) Name() string { return "pipeline · " + s.Label }

func (s pipelineSource) Kind() viewer.Kind { return viewer.KindYAML }

// Load resolves the document. Every refusal names what could not be done: this
// runs behind a keystroke with nothing else on screen to explain a blank pane.
func (s pipelineSource) Load() ([]byte, error) {
	if !forge.ShapeFor(s.Forge.Type).MergedCIConfig {
		return nil, fmt.Errorf("%s does not resolve a pipeline server-side", s.Forge.Type)
	}

	remote, err := git.RemoteURL(s.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("read the repository's remote: %w", err)
	}
	// §3.17's rule, asked a third time: a context holds one token, for one
	// host. A repository of any other forge is not fetched with it.
	if !git.SameHost(remote, s.Forge.URL) {
		return nil, errors.New("this repository's remote is not this context's forge")
	}

	project := projectPathOf(remote)
	if project == "" {
		return nil, fmt.Errorf("no project path in %q", remote)
	}

	token, err := s.loadToken()
	if err != nil {
		return nil, err
	}
	backend, err := session.Backend(s.Forge.Type, s.Forge.URL, token)
	if err != nil {
		return nil, fmt.Errorf("open a %s session: %w", s.Forge.Type, err)
	}

	merged, err := backend.MergedCIConfig(context.Background(), project, branchOf(s.RepoPath))
	if err != nil {
		return nil, err
	}
	return []byte(annotatePipeline(merged, s.Findings)), nil
}

// loadToken reads the context's forge token. The document is the repository's
// own CI configuration, so an anonymous read answers 404 on anything private:
// no token is a refusal rather than an attempt.
func (s pipelineSource) loadToken() (string, error) {
	if s.Secrets == nil {
		return "", errors.New("no credential store for this context")
	}
	token, err := s.Secrets.Load(s.Forge.URL)
	if err != nil || token == "" {
		return "", fmt.Errorf("no %s token stored — sign in with :%s", s.Forge.Type, "git-auth")
	}
	return token, nil
}

// projectPathOf is the forge-side path of a normalised remote: everything after
// the host, without the leading slash.
//
// It is not git.HostOf's sibling by accident — the two split one URL between
// them, and this half is what addresses a project with the API.
func projectPathOf(remoteURL string) string {
	normalized := git.NormalizeRemoteURL(remoteURL)
	idx := strings.Index(normalized, "://")
	if idx == -1 {
		return ""
	}
	rest := normalized[idx+3:]
	slash := strings.Index(rest, "/")
	if slash == -1 {
		return ""
	}
	return strings.Trim(rest[slash+1:], "/")
}

// branchOf is the branch the document should describe: the one checked out, so
// a user reading a finding sees the configuration it was graded against. An
// unreadable HEAD yields "", which the backend reads as the default branch.
func branchOf(repoPath string) string {
	status, err := git.ReadStatus(repoPath)
	if err != nil {
		return ""
	}
	return status.Branch
}
