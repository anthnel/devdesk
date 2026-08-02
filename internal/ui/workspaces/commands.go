package workspaces

import (
	"context"
	"log"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/scan"
)

// clearFooterInfoCmd returns a command that clears the footer info message after a delay.
func clearFooterInfoCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearFooterInfoMsg{}
	})
}

// loadScanCacheCmd loads the workspace scan cache from disk asynchronously
func loadScanCacheCmd() tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewWorkspaceScanCache()
		if err != nil {
			return ScanCacheLoadedMsg{Cache: nil}
		}
		return ScanCacheLoadedMsg{Cache: c.GetAll()}
	}
}

// deleteScanCacheCmd removes the given repo paths from the disk cache (Rule 126: scan all purges cache)
func deleteScanCacheCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewWorkspaceScanCache()
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

			sensitive := false
			for _, f := range result.Findings {
				if f.Source == "gitleaks" {
					sensitive = true
					break
				}
			}

			entry := cache.WorkspaceScanEntry{
				RepoPath:  repoPath,
				Critical:  result.Counts.Critical,
				High:      result.Counts.High,
				Medium:    result.Counts.Medium,
				Low:       result.Counts.Low,
				Sensitive: sensitive,
				ScannedAt: result.EndTime,
			}

			wc, cErr := cache.NewWorkspaceScanCache()
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
