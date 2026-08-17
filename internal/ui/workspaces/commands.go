package workspaces

import (
	"context"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/scan"
)

// clearSyncSummaryCmd drops a finished sync's summary from the footer after the
// same three seconds every other footer message gets (Rule 128).
func clearSyncSummaryCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearSyncSummaryMsg{}
	})
}

// syncSpec is what a batch of syncs needs from the model, copied out before the
// first command runs (Rule 110).
type syncSpec struct {
	gitlabURL string
	// token loads the configured GitLab token, at most once for the batch. It
	// is a closure rather than the token itself so the keyring read happens on
	// a command's goroutine — Update does no I/O — and so twelve repositories
	// do not produce twelve reads.
	token func() string
	jobs  int
}

// tokenLoader returns a function that loads the GitLab token once and then
// answers from memory. A missing token is not an error: a public repository
// fetches without one, and a private one fails with git's own reason.
func tokenLoader(storage credentials.Storage, gitlabURL string) func() string {
	if storage == nil || gitlabURL == "" {
		return func() string { return "" }
	}
	var once sync.Once
	var token string
	return func() string {
		once.Do(func() {
			loaded, err := gitlab.NewAuth(storage).LoadCredentials(gitlabURL)
			if err != nil {
				log.Printf("ERROR [workspaces] loading the sync token: %v", err)
				return
			}
			token = loaded
		})
		return token
	}
}

// syncOneRepoCmd syncs a single repository, bounded by the batch's semaphore.
func syncOneRepoCmd(repoPath string, spec syncSpec, sem chan struct{}) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg { return WorkspaceSyncStartingMsg{RepoPath: repoPath} },
		func() tea.Msg {
			sem <- struct{}{}
			defer func() { <-sem }()

			msg := WorkspaceSyncCompleteMsg{RepoPath: repoPath}
			remote, err := git.RemoteURL(repoPath)
			if err != nil {
				msg.Error = err
				return msg
			}

			result, err := git.Sync(repoPath, git.SyncOptions{
				Token: tokenForRemote(remote, spec.gitlabURL, spec.token),
			})
			msg.Error = err
			msg.Outcome = result.Outcome
			msg.Behind = result.Behind
			msg.Reason = result.Reason

			// Re-read the repository whatever happened. A refused sync fetched
			// all the same, so its counts are current now even though nothing
			// merged — which is the answer to D35 and the reason a refusal is
			// still worth something.
			msg.Status = readGitStatus(repoPath)
			return msg
		},
	)
}

// batchSyncCmd syncs every path through a pool of gitlab.pull.parallel_jobs.
//
// The setting is shared with the explorer's clone deliberately: both are "how
// many git network operations at once", and one number the user can reason
// about beats two they have to keep in step.
func batchSyncCmd(repoPaths []string, spec syncSpec) tea.Cmd {
	sem := make(chan struct{}, max(spec.jobs, 1))
	cmds := make([]tea.Cmd, len(repoPaths))
	for i, path := range repoPaths {
		cmds[i] = syncOneRepoCmd(path, spec, sem)
	}
	return tea.Batch(cmds...)
}

// readGitStatus re-reads one repository's git metadata.
func readGitStatus(repoPath string) Entry {
	entry := Entry{Path: repoPath, IsDir: true}
	detectGitStatus(&entry)
	return entry
}

// loadScanCacheCmd loads the workspace scan cache from disk asynchronously
func loadScanCacheCmd() tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewWorkspaceScanCache(config.CurrentContextName())
		if err != nil {
			return ScanCacheLoadedMsg{Cache: nil}
		}
		return ScanCacheLoadedMsg{Cache: c.GetAll()}
	}
}

// deleteScanCacheCmd removes the given repo paths from the disk cache (Rule 126: scan all purges cache)
func deleteScanCacheCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewWorkspaceScanCache(config.CurrentContextName())
		if err != nil {
			return nil
		}
		for _, path := range paths {
			_ = c.Delete(path)
		}
		return nil
	}
}

// scanOneRepoCmd scans a single git repo directory, using a semaphore to limit concurrency.
// It emits WorkspaceScanStartingMsg before scanning and WorkspaceScanCompleteMsg after.
func scanOneRepoCmd(repoPath string, opts scan.ScanOptions, sem chan struct{}) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return WorkspaceScanStartingMsg{RepoPath: repoPath}
		},
		func() tea.Msg {
			sem <- struct{}{}
			defer func() { <-sem }()

			scanner := scan.NewScanner(opts)
			result, err := scanner.Scan(context.Background(), repoPath, scan.TargetDirectory)
			if err != nil {
				log.Printf("ERROR [workspaces] scan %s: %v", repoPath, err)
				return WorkspaceScanCompleteMsg{
					RepoPath: repoPath,
					Error:    err,
				}
			}

			if len(result.Errors) > 0 && result.TotalFindings() == 0 {
				combined := strings.Join(result.Errors, "; ")
				log.Printf("ERROR [workspaces] scan errors for %s: %s", repoPath, combined)
			}

			// Le verdict vient du scan et de nulle part ailleurs : la boucle
			// « une finding dont Source vaut gitleaks » qui était ici ne voyait
			// pas les secrets trouvés par Trivy, et ne savait pas dire qu'aucune
			// étape n'avait cherché.
			sensitive := result.SecretVerdict()

			entry := cache.WorkspaceScanEntry{
				RepoPath:  repoPath,
				Critical:  result.Counts.Critical,
				High:      result.Counts.High,
				Medium:    result.Counts.Medium,
				Low:       result.Counts.Low,
				Sensitive: sensitive,
				ScannedAt: result.EndTime,
			}

			wc, cErr := cache.NewWorkspaceScanCache(config.CurrentContextName())
			if cErr != nil {
				log.Printf("ERROR [workspaces] open cache: %v", cErr)
			} else if sErr := wc.Set(repoPath, entry); sErr != nil {
				log.Printf("ERROR [workspaces] cache set %s: %v", repoPath, sErr)
			}

			if sErr := cache.SaveWorkspaceScanResult(repoPath, result); sErr != nil {
				log.Printf("ERROR [workspaces] save full result %s: %v", repoPath, sErr)
			}

			return WorkspaceScanCompleteMsg{
				RepoPath:  repoPath,
				Critical:  entry.Critical,
				High:      entry.High,
				Medium:    entry.Medium,
				Low:       entry.Low,
				Sensitive: sensitive,
				ScannedAt: entry.ScannedAt,
			}
		},
	)
}

// batchScanCmd scans all provided repo paths in parallel using a worker pool
// of runtime.NumCPU()/2 workers (minimum 1).
func batchScanCmd(repoPaths []string, opts scan.ScanOptions) tea.Cmd {
	numWorkers := runtime.NumCPU() / 2
	if numWorkers < 1 {
		numWorkers = 1
	}
	sem := make(chan struct{}, numWorkers)

	cmds := make([]tea.Cmd, len(repoPaths))
	for i, path := range repoPaths {
		cmds[i] = scanOneRepoCmd(path, opts, sem)
	}
	return tea.Batch(cmds...)
}
