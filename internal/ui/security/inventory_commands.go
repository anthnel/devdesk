package security

import (
	"context"
	"errors"
	"log"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
)

// InventoryLoadedMsg carries the scanned targets read from the two caches.
type InventoryLoadedMsg struct {
	Targets []scanTarget
}

// InventoryResultLoadedMsg carries a full scan result read back for one target,
// or the error that says there is no longer a file to read.
type InventoryResultLoadedMsg struct {
	Name   string
	Result *scan.Result
	Err    error
}

// InventoryScanFinishedMsg reports one rescan launched from the inventory.
// ScannedAt is the scan's own end time, the value written to the cache — so the
// row and a later reload of that cache report the same age.
type InventoryScanFinishedMsg struct {
	Name   string
	Counts scan.SeverityCounts
	// Sensitive is the secret verdict, carried for the same reason as the
	// counts: sans lui la ligne rescannée garderait l'icône de son scan
	// précédent jusqu'au prochain ctrl+r, en affichant par ailleurs des
	// compteurs tout frais.
	Sensitive *bool
	ScannedAt time.Time
	Err       error
}

// inventoryScanJob is one target to rescan.
type inventoryScanJob struct {
	Kind targetKind
	Name string
}

// loadInventoryCmd reads both scan caches for the current context.
//
// Only this context's entries are visible: the caches are bound to one at
// construction, which is what stops the inventory listing what another context
// scanned (§0b).
func loadInventoryCmd() tea.Cmd {
	return func() tea.Msg {
		contextName := config.CurrentContextName()
		targets := make([]scanTarget, 0)

		if c, err := cache.NewImageScanCache(contextName); err != nil {
			log.Printf("ERROR [security/inventory] open image scan cache: %v", err)
		} else {
			for name, entry := range c.GetAll() {
				targets = append(targets, scanTarget{
					Kind: kindImage, Name: name, Scanned: true,
					Counts: scan.SeverityCounts{
						Critical: entry.Critical, High: entry.High,
						Medium: entry.Medium, Low: entry.Low,
					},
					Sensitive: entry.Sensitive,
					ScannedAt: entry.ScannedAt,
				})
			}
		}

		if c, err := cache.NewWorkspaceScanCache(contextName); err != nil {
			log.Printf("ERROR [security/inventory] open workspace scan cache: %v", err)
		} else {
			for path, entry := range c.GetAll() {
				targets = append(targets, scanTarget{
					Kind: kindRepo, Name: path, Scanned: true,
					Counts: scan.SeverityCounts{
						Critical: entry.Critical, High: entry.High,
						Medium: entry.Medium, Low: entry.Low,
					},
					Sensitive: entry.Sensitive,
					ScannedAt: entry.ScannedAt,
				})
			}
		}

		// Both caches are maps, so their range order is deliberately random.
		// Settling one here means the table's stable sort has something stable
		// underneath it, and two rows equal on the sort column keep their order
		// between refreshes instead of swapping.
		sort.Slice(targets, func(i, j int) bool {
			return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name)
		})
		return InventoryLoadedMsg{Targets: targets}
	}
}

// loadInventoryResultCmd reads back the full result stored for one target
// (Rule 126: opening a scanned row reads the cache, it never scans).
func loadInventoryResultCmd(target scanTarget) tea.Cmd {
	kind, name := target.Kind, target.Name
	return func() tea.Msg {
		var result *scan.Result
		var err error
		if kind == kindImage {
			result, err = cache.LoadImageScanResult(name)
		} else {
			result, err = cache.LoadWorkspaceScanResult(name)
		}
		if err != nil {
			log.Printf("ERROR [security/inventory] load result %s: %v", name, err)
		}
		return InventoryResultLoadedMsg{Name: name, Result: result, Err: err}
	}
}

// purgeInventoryCmd removes the cached entries and stored results for the given
// targets. Rule 126: ctrl+a purges before rescanning, where ctrl+s overwrites.
func purgeInventoryCmd(jobs []inventoryScanJob) tea.Cmd {
	if len(jobs) == 0 {
		return nil
	}
	return func() tea.Msg {
		contextName := config.CurrentContextName()
		imageCache, imageErr := cache.NewImageScanCache(contextName)
		repoCache, repoErr := cache.NewWorkspaceScanCache(contextName)
		for _, job := range jobs {
			if job.Kind == kindImage {
				if imageErr == nil {
					_ = imageCache.Delete(job.Name)
				}
				continue
			}
			if repoErr == nil {
				_ = repoCache.Delete(job.Name)
			}
			_ = cache.DeleteWorkspaceScanResult(job.Name)
		}
		return nil
	}
}

// rescanCmd scans every job through a worker pool of NumCPU/2, the size the
// images and workspaces lists already use.
func rescanCmd(jobs []inventoryScanJob, opts scan.ScanOptions) tea.Cmd {
	sem := make(chan struct{}, max(runtime.NumCPU()/2, 1))
	cmds := make([]tea.Cmd, len(jobs))
	for i, job := range jobs {
		cmds[i] = rescanOneCmd(job, opts, sem)
	}
	return tea.Batch(cmds...)
}

// rescanOneCmd rescans a single target and writes the result through to both
// the counts cache and the stored result, so the row and `enter` agree.
func rescanOneCmd(job inventoryScanJob, opts scan.ScanOptions, sem chan struct{}) tea.Cmd {
	return func() tea.Msg {
		sem <- struct{}{}
		defer func() { <-sem }()

		targetType := scan.TargetImage
		if job.Kind == kindRepo {
			targetType = scan.TargetDirectory
		}
		result, err := scan.NewScanner(opts).Scan(context.Background(), job.Name, targetType)
		if err != nil {
			log.Printf("ERROR [security/inventory] rescan %s: %v", job.Name, err)
			return InventoryScanFinishedMsg{Name: job.Name, Err: err}
		}
		// Scan() reports scanner failures in result.Errors rather than as an
		// error. Every scanner having failed is not an empty inventory row, it
		// is a row that could not be produced.
		if len(result.Errors) > 0 && result.TotalFindings() == 0 {
			combined := strings.Join(result.Errors, "; ")
			log.Printf("ERROR [security/inventory] rescan errors for %s: %s", job.Name, combined)
			return InventoryScanFinishedMsg{Name: job.Name, Err: errors.New(combined)}
		}

		storeRescan(job, result)
		return InventoryScanFinishedMsg{
			Name: job.Name, Counts: result.Counts,
			Sensitive: result.SecretVerdict(), ScannedAt: result.EndTime,
		}
	}
}

// storeRescan writes a finished rescan to the cache its kind belongs to.
func storeRescan(job inventoryScanJob, result *scan.Result) {
	contextName := config.CurrentContextName()
	if job.Kind == kindImage {
		if c, err := cache.NewImageScanCache(contextName); err == nil {
			_ = c.Set(job.Name, cache.ImageScanEntry{
				Critical: result.Counts.Critical, High: result.Counts.High,
				Medium: result.Counts.Medium, Low: result.Counts.Low,
				Sensitive: result.SecretVerdict(),
				ScannedAt: result.EndTime,
			})
		}
		if err := cache.SaveImageScanResult(job.Name, result); err != nil {
			log.Printf("ERROR [security/inventory] save image result %s: %v", job.Name, err)
		}
		return
	}
	if c, err := cache.NewWorkspaceScanCache(contextName); err == nil {
		_ = c.Set(job.Name, cache.WorkspaceScanEntry{
			RepoPath: job.Name,
			Critical: result.Counts.Critical, High: result.Counts.High,
			Medium: result.Counts.Medium, Low: result.Counts.Low,
			Sensitive: result.SecretVerdict(),
			ScannedAt: result.EndTime,
		})
	}
	if err := cache.SaveWorkspaceScanResult(job.Name, result); err != nil {
		log.Printf("ERROR [security/inventory] save workspace result %s: %v", job.Name, err)
	}
}
