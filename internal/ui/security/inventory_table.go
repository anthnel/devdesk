package security

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// targetKind tells the two caches apart. It decides which loader `enter` uses
// and which TargetType a rescan runs with, so it is carried rather than
// re-derived from the name — an absolute path and a "repo:tag" are not reliably
// distinguishable, and guessing wrong scans a directory as an image.
type targetKind int

const (
	kindImage targetKind = iota
	kindRepo
)

// scanTarget is one row of the inventory: something that has been scanned, what
// the scan found, and when.
//
// Kind, the display name and whether a rescan is running now live on none of
// the cache entries, which is why the decoration goes on the row rather than
// being looked up from the model — the same reason imageRow exists. The columns
// are built once, in New, so their Cell functions have nothing to reach back
// into, and each count column sorts by the number it prints.
type scanTarget struct {
	Kind      targetKind
	Name      string // the cache key: "repo:tag", or the absolute repository path
	Counts    scan.SeverityCounts
	ScannedAt time.Time
	// Scanned is false between a ctrl+a purge and the scan that replaces it.
	// The target is still known — it is the counts that are not.
	Scanned      bool
	Scanning     bool
	Failed       bool
	SpinnerFrame string
}

// inventoryColumnCritical is the column the inventory opens sorted by,
// descending: the target with the most critical findings is the one the view
// exists to surface.
const inventoryColumnCritical = 1

// displayName is what the Target column shows: the image reference as it is
// cached, or the repository path with the home directory folded back to "~".
// The cache key stays the value everything else uses, so folding here cannot
// reach a loader or a scanner.
func (t scanTarget) displayName() string {
	if t.Kind == kindImage {
		return theme.IconDocker + " " + t.Name
	}
	return theme.IconWorkspace + " " + shortenHome(t.Name)
}

// shortenHome replaces the home directory prefix with "~". It is the inverse of
// the expansion the workspaces view does on the configured root.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

// countColumn builds one of the four severity columns. A purged row prints "-"
// rather than "0": nothing was found and nothing is known are different answers,
// and zero is the one a user reads as "clean".
func countColumn(title string, get func(scan.SeverityCounts) int) datatable.Column[scanTarget] {
	return datatable.Column[scanTarget]{
		Title: title, MinWidth: 5,
		Cell: func(t scanTarget) string {
			if !t.Scanned {
				return "-"
			}
			return strconv.Itoa(get(t.Counts))
		},
		Less: func(a, b scanTarget) bool { return get(a.Counts) < get(b.Counts) },
	}
}

// inventoryScannedCell reports the scan state: the spinner while one runs, an
// error icon when the last attempt failed, otherwise how long ago it succeeded
// (Rule 127). Plain text throughout — Rule 122.
func inventoryScannedCell(t scanTarget) string {
	switch {
	case t.Scanning:
		return t.SpinnerFrame + "scanning"
	case t.Failed:
		return theme.IconError + " error"
	case t.Scanned:
		return theme.TimeAgo(t.ScannedAt)
	}
	return "-"
}

// inventoryColumns describes the inventory table.
func inventoryColumns() []datatable.Column[scanTarget] {
	return []datatable.Column[scanTarget]{
		{
			Title: "Target", MinWidth: 24, Flex: 1,
			Cell: func(t scanTarget) string { return t.displayName() },
			Less: func(a, b scanTarget) bool {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			},
			// The cache key, not the displayed name: a query for the full path of
			// a repository has to match the row that folds it to "~".
			Search: func(t scanTarget) string { return t.Name },
		},
		countColumn("CRIT", func(c scan.SeverityCounts) int { return c.Critical }),
		countColumn("HIGH", func(c scan.SeverityCounts) int { return c.High }),
		countColumn("MED", func(c scan.SeverityCounts) int { return c.Medium }),
		countColumn("LOW", func(c scan.SeverityCounts) int { return c.Low }),
		{
			Title: "Scanned", MinWidth: 14,
			Cell: inventoryScannedCell,
			Less: func(a, b scanTarget) bool { return a.ScannedAt.Before(b.ScannedAt) },
		},
	}
}

// newInventoryTable builds the inventory table.
func newInventoryTable() datatable.Model[scanTarget] {
	return datatable.New(datatable.Config[scanTarget]{
		Columns:    inventoryColumns(),
		SortColumn: inventoryColumnCritical,
		SortDesc:   true,
	})
}
