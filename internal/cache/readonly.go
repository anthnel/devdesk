package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthnel/devdesk/internal/scan"
)

// The read-only reads (§3.38).
//
// ~/.devdesk/ has no lock, and nothing warns when two writers cross. A server
// that writes nothing removes the question — it can run while the TUI runs
// without anyone having to think about it — so the MCP server reads the scan
// caches through these functions and never opens a *ScanCache.
//
// They return maps rather than a cache value on purpose. A read-only cache with
// a Set that silently does nothing, or returns an error nobody checks, is a trap
// laid for the next caller; with no cache there is no Set, and the write is
// unexpressible rather than forbidden by review. It is Rule 122's shape one
// layer up: make the wrong thing impossible to write, not merely against the
// rules.
//
// Three writes are avoided, and each is a real one:
//
//   - the MkdirAll both constructors do — a read has no business creating the
//     cache directory;
//   - the pre-context upgrade readScanCacheFile performs, which *attributes*
//     legacy entries to the opening context;
//   - the fold §3.39 writes back on the first open of an image cache. That one
//     decides nothing harmful — it is deterministic, newest wins — but it is
//     still a second process writing a file with no lock over it.
//
// The stored results need the same treatment for a smaller reason:
// LoadImageScanResult and LoadWorkspaceScanResult resolve their path through a
// helper that creates the results directory, so reading a result that is not
// there would create the directory it is not in.

// scanCacheDir is ~/.devdesk/cache, and does not create it. NewImageScanCache
// and NewWorkspaceScanCache create it because they are about to write.
func scanCacheDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".devdesk", "cache"), nil
}

// ReadImageScanEntries returns every cached image scan, without writing.
//
// The image cache is not scoped to a context (§3.39): its key is a local Docker
// reference, and `docker image ls` answers for the machine.
func ReadImageScanEntries() (map[string]ImageScanEntry, error) {
	dir, err := scanCacheDir()
	if err != nil {
		return nil, err
	}
	return readImageScanEntriesAt(filepath.Join(dir, "image-scans.json"))
}

// readImageScanEntriesAt is ReadImageScanEntries without the home-directory
// lookup, so tests can point at a temp file.
func readImageScanEntriesAt(path string) (map[string]ImageScanEntry, error) {
	// The fold is computed and deliberately not written back.
	entries, _, err := readImageScanCacheFile(path)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// ReadWorkspaceScanEntries returns one context's cached repository scans,
// without writing.
//
// Entries from a file that predates contexts are served for whichever context
// is asked, because nobody has decided whose they are — and this read is not
// where that gets decided. The TUI still attributes them on its first open,
// which is where somebody is present to see it happen.
func ReadWorkspaceScanEntries(context string) (map[string]WorkspaceScanEntry, error) {
	dir, err := scanCacheDir()
	if err != nil {
		return nil, err
	}
	return readWorkspaceScanEntriesAt(filepath.Join(dir, "workspace-scans.json"), context)
}

// readWorkspaceScanEntriesAt is ReadWorkspaceScanEntries without the
// home-directory lookup, so tests can point at a temp file.
func readWorkspaceScanEntriesAt(path, context string) (map[string]WorkspaceScanEntry, error) {
	contexts, legacy, err := parseScanCacheFile[WorkspaceScanEntry](path)
	if err != nil {
		return nil, err
	}
	if len(legacy) > 0 {
		return legacy, nil
	}
	own := contexts[context]
	if own == nil {
		own = map[string]WorkspaceScanEntry{}
	}
	return own, nil
}

// ReadImageScanResult loads one image's stored findings without creating the
// results directory on the way.
func ReadImageScanResult(imageName string) (*scan.Result, error) {
	dir, err := scanCacheDir()
	if err != nil {
		return nil, err
	}
	return readScanResult(filepath.Join(dir, "image-results", hashedResultName(imageName)))
}

// ReadWorkspaceScanResult loads one repository's stored findings without
// creating the results directory on the way.
func ReadWorkspaceScanResult(repoPath string) (*scan.Result, error) {
	dir, err := scanCacheDir()
	if err != nil {
		return nil, err
	}
	return readScanResult(filepath.Join(dir, "workspace-results", hashedResultName(repoPath)))
}

// hashedResultName is how both result stores name a file: the SHA256 of the
// target, because a Docker reference and an absolute path both carry characters
// a filename cannot. It is written here as well as in the two Save paths, and
// TestTheReadOnlyResultPathMatchesTheWrittenOne holds the two in step — the
// alternative was exporting a path helper from each, which is three names for
// one rule.
func hashedResultName(target string) string {
	return fmt.Sprintf("%x.json", sha256.Sum256([]byte(target)))
}

func readScanResult(path string) (*scan.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result scan.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
