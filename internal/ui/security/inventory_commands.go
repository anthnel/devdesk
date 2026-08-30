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
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/git"
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
	// CIScore is the pipeline grade, carried for Sensitive's reason exactly: a
	// rescanned row would otherwise keep the letter of its previous scan until
	// the next ctrl+r, beside counters that are brand new.
	CIScore   *string
	ScannedAt time.Time
	Err       error
}

// InventoryScanStartingMsg reports that one rescan has a worker and is under
// way. It did not exist: rescanCmd emitted only a finished message, so a target
// went from queued straight to done and there was nothing to say which of the
// batch was actually running (D10).
type InventoryScanStartingMsg struct {
	Name string

	// Cancel stops the scan, and is what makes `K` on this row mean anything
	// (D7). It rides on the starting message because the context is created
	// inside the Cmd: the registry stores it in the same Update that marks the
	// item running, so there is no window where the row is running and cannot
	// be stopped.
	Cancel context.CancelFunc
}

// inventoryScanJob is one target to rescan.
type inventoryScanJob struct {
	Kind targetKind
	Name string
}

// loadInventoryCmd reads both scan caches for the current context, and drops
// what no longer exists.
//
// Only this context's entries are visible: the caches are bound to one at
// construction, which is what stops the inventory listing what another context
// scanned (§0b).
//
// The reconciliation is here rather than on each delete because a delete is not
// the only way a target goes away: `P` prunes an unknown set of images at once,
// and a `docker rmi` or an `rm -rf` outside DevDesk is not observed at all. One
// rule at the load covers the four, where four cascades would each have to be
// remembered.
// forgeURL is this context's forge, and it is empty when the CI column is off:
// resolving gradeability costs one `git remote get-url` per repository row, and
// a column nobody is showing must not pay for it.
func loadInventoryCmd(forgeURL string) tea.Cmd {
	return func() tea.Msg {
		contextName := config.CurrentContextName()
		targets := make([]scanTarget, 0)
		images, imagesKnown := localImages()

		if c, err := cache.NewImageScanCache(); err != nil {
			log.Printf("ERROR [security/inventory] open image scan cache: %v", err)
		} else {
			for name, entry := range c.GetAll() {
				if !stillPulled(name, images, imagesKnown) {
					continue
				}
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
				if cache.RepositoryGone(path) {
					continue
				}
				targets = append(targets, scanTarget{
					Kind: kindRepo, Name: path, Scanned: true,
					Counts: scan.SeverityCounts{
						Critical: entry.Critical, High: entry.High,
						Medium: entry.Medium, Low: entry.Low,
					},
					Sensitive:   entry.Sensitive,
					CIScore:     entry.CIScore,
					CIGradeable: gradeable(path, forgeURL),
					ScannedAt:   entry.ScannedAt,
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

// gradeable says whether plumber would grade this repository from this context.
//
// It is §3.42's rule, asked of a cache entry rather than of a workspaces row: a
// context targets one forge and holds one token, so a repository of any other
// host is not ungraded but ungradeable — an empty cell, never a dash. The
// distinction has to survive the trip through the cache, which stores only the
// letter.
//
// A remote that cannot be read is not gradeable. That is the honest answer and
// not a fallback: a repository with no origin is one plumber could not resolve
// a project for either.
func gradeable(repoPath, forgeURL string) bool {
	if forgeURL == "" {
		return false
	}
	remote, err := git.RemoteURL(repoPath)
	if err != nil {
		return false
	}
	return git.SameHost(remote, forgeURL)
}

// listImages is docker.ListImages, indirected for the tests and for nothing
// else — production never reassigns it.
//
// The seam exists because the reconciliation below makes the loader depend on
// what the daemon holds, and a test cannot pull an image to satisfy it. The
// round trips through the real cache files would otherwise be reduced to
// asserting that a fixture is absent, which they would pass for the wrong
// reason.
var listImages = docker.ListImages

// localImages names the images present on this machine, and says whether it
// could find out.
//
// The second return is the whole point. "Docker is not running" and "the image
// is gone" are the same silence from a caller's side, and reading the first as
// the second would empty the inventory of every image the moment the daemon
// stops. A failed enumeration keeps every entry instead: showing a target that
// no longer exists is a stale row, hiding one that does is a lie.
func localImages() (map[string]struct{}, bool) {
	list, err := listImages()
	if err != nil {
		log.Printf("INFO [security/inventory] image list unavailable, keeping every cached image: %v", err)
		return nil, false
	}
	names := make(map[string]struct{}, len(list))
	for _, img := range list {
		names[img.Name()] = struct{}{}
	}
	return names, true
}

// stillPulled says whether a cached image entry still has an image behind it.
// It is a function of its own so a test can put the two answers to it without a
// daemon: an enumeration that failed keeps everything, one that succeeded keeps
// what it listed.
func stillPulled(name string, images map[string]struct{}, known bool) bool {
	if !known {
		return true
	}
	_, ok := images[name]
	return ok
}

// isGone moved to cache.RepositoryGone: the dashboard's posture reads the same
// cache and has to reach the same answer, and it is in another package.
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
func purgeInventoryCmd(jobs []inventoryScanJob, contextName string) tea.Cmd {
	if len(jobs) == 0 {
		return nil
	}
	return func() tea.Msg {
		imageCache, imageErr := cache.NewImageScanCache()
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
//
// contextName is the one the run was stamped with at launch, carried down to
// storeRescan (D68). A rescan of twenty targets outlives a context switch
// easily, and a repository's counts belong to the context that asked for them.
func rescanCmd(jobs []inventoryScanJob, opts scan.ScanOptions, contextName string) tea.Cmd {
	sem := make(chan struct{}, max(runtime.NumCPU()/2, 1))
	cmds := make([]tea.Cmd, len(jobs))
	for i, job := range jobs {
		cmds[i] = rescanOneCmd(job, opts, contextName, sem)
	}
	return tea.Batch(cmds...)
}

// rescanOneCmd rescans a single target and writes the result through to both
// the counts cache and the stored result, so the row and `enter` agree.
func rescanOneCmd(job inventoryScanJob, opts scan.ScanOptions, contextName string, sem chan struct{}) tea.Cmd {
	// The context the rescan runs under, so `K` can stop it (D7). It travels on
	// the starting message, which is the same Update that marks the item
	// running — two steps would leave a window where the row is running and
	// cannot be stopped.
	ctx, cancel := context.WithCancel(context.Background())

	return tea.Sequence(
		// The starting message waits for its turn in the pool, which is what
		// moves the target from queued to running. Emitted before the wait,
		// every target in a batch would report itself running the instant the
		// batch was dispatched.
		func() tea.Msg {
			sem <- struct{}{}
			return InventoryScanStartingMsg{Name: job.Name, Cancel: cancel}
		},
		rescanOneBodyCmd(ctx, cancel, job, opts, contextName, sem),
	)
}

func rescanOneBodyCmd(ctx context.Context, cancel context.CancelFunc, job inventoryScanJob, opts scan.ScanOptions, contextName string, sem chan struct{}) tea.Cmd {
	return func() tea.Msg {
		defer func() { <-sem }()
		// Releases the context whether the scan was cancelled or ran to the
		// end; the registry drops its copy when the item settles.
		defer cancel()

		targetType := scan.TargetImage
		if job.Kind == kindRepo {
			targetType = scan.TargetDirectory
		}
		result, err := scan.NewScanner(opts).Scan(ctx, job.Name, targetType)
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

		storeRescan(job, result, contextName)
		return InventoryScanFinishedMsg{
			Name: job.Name, Counts: result.Counts,
			Sensitive: result.SecretVerdict(), CIScore: result.CIVerdict(),
			ScannedAt: result.EndTime,
		}
	}
}

// storeRescan writes a finished rescan to the cache its kind belongs to.
//
// contextName is passed rather than read here: an image cache is not scoped at
// all (§3.39 — its key is a local Docker reference, which answers for the
// machine), so only the repository branch uses it, and that branch must reach
// the context the scan was launched in.
func storeRescan(job inventoryScanJob, result *scan.Result, contextName string) {
	if job.Kind == kindImage {
		if c, err := cache.NewImageScanCache(); err == nil {
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
			CIScore:   result.CIVerdict(),
			ScannedAt: result.EndTime,
		})
	}
	if err := cache.SaveWorkspaceScanResult(job.Name, result); err != nil {
		log.Printf("ERROR [security/inventory] save workspace result %s: %v", job.Name, err)
	}
}
