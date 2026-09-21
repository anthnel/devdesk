package security

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

// ── Messages (Rule 109) ──────────────────────────────────────────────────────

// RemediationDiscoveredMsg is the answer to reading a repository's Dockerfiles:
// each base image with its candidates, and what the candidate cache already
// holds. Target says which repository it is about, so an answer that arrives
// after the user has opened another result is dropped rather than shown on it.
type RemediationDiscoveredMsg struct {
	Target    string
	Entries   []remediation.Entry
	Results   map[string]cache.RemediationEntry
	Truncated bool
	Err       error
}

// RemediationScanFinishedMsg reports one image scanned from its registry.
type RemediationScanFinishedMsg struct {
	Target string
	Ref    string
	Entry  cache.RemediationEntry
	Err    error
}

// ── Commands (Rule 110: they read and write disk and the network, never the model) ──

// discoverRemediationCmd reads the Dockerfiles under a repository and lists the
// tags each base image could move to.
//
// The track and the candidate cap are copied in: a Cmd runs concurrently with
// Update and must not read the model.
func discoverRemediationCmd(target string, track remediation.Track) tea.Cmd {
	return func() tea.Msg {
		entries, truncated, err := remediation.Discover(target, registryTagLister, track, remediationCandidates)
		if err != nil {
			return RemediationDiscoveredMsg{Target: target, Err: err}
		}
		// An unreadable cache is an empty one: nothing has been measured, which
		// is what the table then says.
		results, cacheErr := cache.ReadRemediationResults()
		if cacheErr != nil {
			log.Printf("ERROR [security/remediation] read candidate cache: %v", cacheErr)
			results = map[string]cache.RemediationEntry{}
		}
		return RemediationDiscoveredMsg{Target: target, Entries: entries, Results: results, Truncated: truncated}
	}
}

// registryTagLister lists a repository's tags with the credentials the engine
// holds for its registry, or anonymously when it holds none.
func registryTagLister(ref remediation.Ref) ([]string, error) {
	host := ref.Registry
	if host == "" {
		host = "docker.io"
	}
	user, pass, _ := docker.GetStoredCreds(host)
	return oci.ListRegistryTags(registryAPIBase(ref.Registry), ref.Repository, user, pass)
}

// registryAPIBase is the v2 API root of a registry named in a Dockerfile: Docker
// Hub has its own host, a local registry is reached over plain HTTP, and
// everything else over HTTPS.
func registryAPIBase(registry string) string {
	switch {
	case registry == "":
		return "https://registry-1.docker.io"
	case strings.HasPrefix(registry, "localhost") || strings.HasPrefix(registry, "127.0.0.1"):
		return "http://" + registry
	}
	return "https://" + registry
}

// scanRemediationCmds scans each image from its registry, one Cmd per image.
//
// They finish independently, which is why each reports itself: the table fills
// row by row rather than waiting for the slowest. Trivy runs one process at a
// time (scan.trivySem), so they queue there rather than racing for its cache.
// Each result is written to the candidate cache before its message is sent, so
// a scan that finishes after the user has left the view is not lost.
func scanRemediationCmds(target string, refs []string, opts scan.ScanOptions, timeout time.Duration) []tea.Cmd {
	var (
		once    sync.Once
		scanner *scan.Scanner
	)
	// Built on first use, on a Cmd's goroutine: detecting the tools runs
	// commands, and the model must not wait for that.
	shared := func() *scan.Scanner {
		once.Do(func() { scanner = scan.NewScanner(opts) })
		return scanner
	}

	cmds := make([]tea.Cmd, 0, len(refs))
	for _, ref := range refs {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			result, err := shared().ScanRemoteImage(ctx, ref)
			if err != nil {
				log.Printf("ERROR [security/remediation] scan %s: %v", ref, err)
				return RemediationScanFinishedMsg{Target: target, Ref: ref, Err: err}
			}
			entry := cache.RemediationEntry{
				Critical: result.Counts.Critical, High: result.Counts.High,
				Medium: result.Counts.Medium, Low: result.Counts.Low,
				ScannedAt: result.EndTime,
			}
			if err := cache.SetRemediationResult(ref, entry); err != nil {
				// The counts are still shown; only their persistence failed.
				log.Printf("ERROR [security/remediation] store result of %s: %v", ref, err)
			}
			return RemediationScanFinishedMsg{Target: target, Ref: ref, Entry: entry}
		})
	}
	return cmds
}

// readFile reads a Dockerfile, for the sources and Cmds that run off Update.
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

// prepareRemediationWriteCmd computes each file as it would become, and asks git
// about each. changes is copied in (Rule 110).
func prepareRemediationWriteCmd(target string, changes []remediationChange) tea.Cmd {
	return func() tea.Msg {
		order, byFile := groupByFile(changes)
		files := make([]preparedWrite, 0, len(order))
		for _, rel := range order {
			path := filepath.Join(target, filepath.FromSlash(rel))
			original, err := os.ReadFile(path)
			if err != nil {
				return RemediationWritePreparedMsg{Target: target, Err: err}
			}
			updated, err := patch.Rewrite(original, edits(byFile[rel]))
			if err != nil {
				return RemediationWritePreparedMsg{Target: target, Err: fmt.Errorf("%s: %w", rel, err)}
			}
			file := preparedWrite{File: rel, Path: path, Original: original, Updated: updated, Changes: byFile[rel]}
			if file.State, err = git.StateOf(path); err != nil {
				log.Printf("ERROR [security/remediation] git state of %s: %v", rel, err)
				file.StateErr = true
			}
			files = append(files, file)
		}
		return RemediationWritePreparedMsg{Target: target, Files: files}
	}
}

// writeRemediationCmd replaces each file, stopping at the first that cannot be.
// A file is replaced whole or not at all; the ones written before a failure stay
// written, and the message says which.
func writeRemediationCmd(target string, files []preparedWrite) tea.Cmd {
	return func() tea.Msg {
		var written []string
		for _, f := range files {
			if err := patch.WriteIfUnchanged(f.Path, f.Original, f.Updated); err != nil {
				return RemediationWrittenMsg{Target: target, Written: written, Failed: f.File, Err: err}
			}
			written = append(written, f.File)
		}
		return RemediationWrittenMsg{Target: target, Written: written}
	}
}
