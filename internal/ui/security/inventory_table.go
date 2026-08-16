package security

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

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
	Kind   targetKind
	Name   string // the cache key: "repo:tag", or the absolute repository path
	Counts scan.SeverityCounts
	// Sensitive is the secret verdict as the cache holds it: nil quand aucune
	// étape n'a cherché, ce qui n'est pas la même chose que n'avoir rien trouvé.
	Sensitive *bool
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
//
// C'est un indice, donc il suit l'ordre des colonnes : Secrets s'est intercalée
// entre Target et CRIT. Une valeur périmée ne trie pas mal, elle ne trie plus du
// tout — `datatable` laisse tomber une direction qui ne désigne aucune colonne
// triable, et Secrets n'en est pas une.
const inventoryColumnCritical = 2

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

// secrets is the row's verdict. Une ligne purgée par ctrl+a n'a plus de verdict
// non plus : ses compteurs affichent `-`, et l'icône dit la même chose.
func (t scanTarget) secrets() theme.SecretsState {
	return theme.SecretsVerdict(t.Sensitive, t.Scanned)
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

// countColumnWidth holds the four severity columns to one width. datatable
// already reserves room for the sort arrow, which would give CRIT and HIGH six
// cells and MED and LOW five — four adjacent columns of the same kind, ragged.
const countColumnWidth = 6

// countColumn builds one of the four severity columns. A purged row prints "-"
// rather than "0": nothing was found and nothing is known are different answers,
// and zero is the one a user reads as "clean".
//
// severity names the column's own level, for the colour. It is passed rather
// than derived from the title so that renaming a header cannot silently repaint
// a column — "CRIT" is an abbreviation, "CRITICAL" is the scanners' vocabulary.
func countColumn(title, severity string, get func(scan.SeverityCounts) int) datatable.Column[scanTarget] {
	return datatable.Column[scanTarget]{
		Title: title, MinWidth: countColumnWidth,
		Cell: func(t scanTarget) string {
			if !t.Scanned {
				return "-"
			}
			return strconv.Itoa(get(t.Counts))
		},
		// Only a count that found something is coloured. Four columns of
		// severity-coloured zeroes would be the whole table shouting at once,
		// which says no more than a table with no colour at all.
		Style: func(t scanTarget) lipgloss.Style {
			if !t.Scanned || get(t.Counts) == 0 {
				return theme.DimStyle
			}
			return theme.SeverityTextStyle(severity)
		},
		Less: func(a, b scanTarget) bool { return get(a.Counts) < get(b.Counts) },
	}
}

// inventoryScannedStyle colours the scan state: a failure is the one thing in
// this column worth interrupting for, and a target never scanned is dim rather
// than absent.
func inventoryScannedStyle(t scanTarget) lipgloss.Style {
	switch {
	case t.Failed:
		return theme.StatusErrorStyle
	case t.Scanning, !t.Scanned:
		return theme.DimStyle
	}
	// Aucune opinion : c'est la table qui pose la couleur de texte du thème.
	return lipgloss.NewStyle()
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

// secretsColumnWidth is the Secrets column, held to the width the workspaces
// list gives it. Elle ne trie pas, et c'est délibéré alors que toutes ses
// voisines trient : `datatable` réserve `largeur(titre) + 2` à une colonne
// triable pour sa flèche, donc un tri coûterait neuf cellules à une colonne qui
// n'affiche qu'un glyphe — dans la table la plus serrée de l'application, où
// `Target` les paierait à 80 colonnes.
const secretsColumnWidth = 7

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
		{
			Title: "Secrets", MinWidth: secretsColumnWidth,
			Cell:  func(t scanTarget) string { return theme.SecretsIcon(t.secrets()) },
			Style: func(t scanTarget) lipgloss.Style { return theme.SecretsStyle(t.secrets()) },
		},
		countColumn("CRIT", "CRITICAL", func(c scan.SeverityCounts) int { return c.Critical }),
		countColumn("HIGH", "HIGH", func(c scan.SeverityCounts) int { return c.High }),
		countColumn("MED", "MEDIUM", func(c scan.SeverityCounts) int { return c.Medium }),
		countColumn("LOW", "LOW", func(c scan.SeverityCounts) int { return c.Low }),
		{
			Title: "Scanned", MinWidth: 14,
			Cell:  inventoryScannedCell,
			Style: inventoryScannedStyle,
			Less:  func(a, b scanTarget) bool { return a.ScannedAt.Before(b.ScannedAt) },
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
