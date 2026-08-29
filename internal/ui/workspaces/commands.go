package workspaces

import (
	"context"
	"log"
	"runtime"
	"strings"
	"sync"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/scan"
)

// checkDepsCmd resolves the scanners once, off the Update goroutine.
//
// scan.CheckDependencies runs exec.LookPath, a --version per tool and a
// docker images -q; none of that belongs in New or View (Rule 110). The
// dashboard's detectTools is the same shape for the same reason.
func checkDepsCmd(cfg config.ScanConfig) tea.Cmd {
	return func() tea.Msg {
		return DepsCheckedMsg{Deps: scan.CheckDependencies(cfg)}
	}
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
			loaded, err := session.NewAuth(storage).LoadCredentials(gitlabURL)
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
		// The starting message waits for its turn in the pool, and that is the
		// point: it is what moves the item from queued to running. Emitted
		// before the wait — as it was — every repository in the batch reported
		// itself running the instant the batch was dispatched, so a twelve-repo
		// sync showed twelve spinners for four workers (D6).
		func() tea.Msg {
			sem <- struct{}{}
			return WorkspaceSyncStartingMsg{RepoPath: repoPath}
		},
		func() tea.Msg {
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

// batchSyncCmd syncs every path through a pool of forge.pull.parallel_jobs.
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
//
// contextName is the one the run was stamped with, not the one current when
// this runs: the purge and the rescan that replaces it must reach the same
// cache, and there is no reason for a purge to be the one command in the batch
// that reads the name again.
func deleteScanCacheCmd(paths []string, contextName string) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewWorkspaceScanCache(contextName)
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
//
// contextName travels with the command rather than being read at the end of the
// scan (D68). A batch of twelve repositories runs for minutes; the context can
// change twice in that time, and the result of a repository belongs to the
// context it was launched in whatever the user is looking at when it lands.
func scanOneRepoCmd(repoPath string, opts scan.ScanOptions, contextName string, sem chan struct{}) tea.Cmd {
	return tea.Sequence(
		// Queued until a worker is free, and only then running — see the note
		// in syncOneRepoCmd.
		func() tea.Msg {
			sem <- struct{}{}
			return WorkspaceScanStartingMsg{RepoPath: repoPath}
		},
		func() tea.Msg {
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
				CIScore:   result.CIVerdict(),
				ScannedAt: result.EndTime,
			}

			storeWorkspaceScan(contextName, repoPath, entry)

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
				CIScore:   entry.CIScore,
				ScannedAt: entry.ScannedAt,
			}
		},
	)
}

// storeWorkspaceScan writes one finished scan into the counts cache of the
// context the run was launched in.
//
// The full result beside it is not scoped at all — it is addressed by path, and
// a scan of /repos/devdesk is the same scan whichever context asked for it. The
// counts are scoped because two contexts legitimately point at different
// workspace roots (see internal/cache/scan_context_test.go).
func storeWorkspaceScan(contextName, repoPath string, entry cache.WorkspaceScanEntry) {
	wc, err := cache.NewWorkspaceScanCache(contextName)
	if err != nil {
		log.Printf("ERROR [workspaces] open cache: %v", err)
		return
	}
	if err := wc.Set(repoPath, entry); err != nil {
		log.Printf("ERROR [workspaces] cache set %s: %v", repoPath, err)
	}
}

// batchScanCmd scans all provided repo paths in parallel using a worker pool
// of runtime.NumCPU()/2 workers (minimum 1).
func batchScanCmd(repoPaths []string, opts scan.ScanOptions, contextName string) tea.Cmd {
	numWorkers := runtime.NumCPU() / 2
	if numWorkers < 1 {
		numWorkers = 1
	}
	sem := make(chan struct{}, numWorkers)

	cmds := make([]tea.Cmd, len(repoPaths))
	for i, path := range repoPaths {
		cmds[i] = scanOneRepoCmd(path, opts, contextName, sem)
	}
	return tea.Batch(cmds...)
}

// copyPathCmd writes a path to the system clipboard (Rule 110: the I/O happens
// in the Cmd, and the outcome comes back as a message).
func copyPathCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return PathCopiedMsg{Path: path, Error: clipboard.WriteAll(path)}
	}
}

// scanOptions is the one place this view assembles scan options, so the token
// loader cannot be forgotten at one of the four call sites that start a scan.
//
// The loader is shared across the whole batch and reads the store once
// (tokenLoader is a sync.Once closure); which repositories actually receive the
// token is decided per target, by the forge-host rule in internal/scan.
func (m Model) scanOptions() scan.ScanOptions {
	opts := scan.OptionsFromConfig(m.config)
	opts.LoadForgeToken = tokenLoader(m.secrets, m.config.Forge.URL)
	return opts
}
