package security

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/registryalias"
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
	// CIScore and CIGradeable are the pipeline grade as the cache holds it, and
	// whether this context could grade the target at all. They are stored
	// rather than the finished verdict for Sensitive's reason: the verdict is
	// one calculation, in theme, and a row that carried it would be a second
	// place able to disagree with the workspaces list.
	//
	// CIGradeable is decided at load, not per frame: it needs the repository's
	// remote, which is a git call. An image is never gradeable — it has no
	// pipeline — so its zero value is already the truth.
	CIScore     *string
	CIGradeable bool
	ScannedAt   time.Time
	// Scanned is false between a ctrl+a purge and the scan that replaces it.
	// The target is still known — it is the counts that are not.
	Scanned  bool
	Scanning bool
	Failed   bool
	// Display is the image reference with its registry prefix replaced by the
	// configured alias, stamped by setInventory the way SpinnerFrame is — the
	// rows arrive from a Cmd, which cannot read the configuration off the model
	// (Rule 110), and a column function is built once and cannot either.
	//
	// Empty means nobody stamped it, and displayName falls back to Name: a row
	// built by hand must stay readable rather than render an empty cell.
	Display      string
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
const inventoryColumnCritical = 3

// kindIcon is what the glyph column shows: which of the two caches the row came
// from. It is a column of its own rather than a prefix inside Target, the way
// the workspaces and containers tables do it — an icon glued into a text cell
// spends the identifying column's width on something that is not the name, and
// a column that measures the name has to measure the glyph too.
func (t scanTarget) kindIcon() string {
	if t.Kind == kindImage {
		return theme.IconDocker
	}
	return theme.IconWorkspace
}

// shortName is what the Target column shows: the image reference with its
// registry prefix folded to the configured alias, or the repository path with
// the home directory folded back to "~". It is also what the title and the
// footer messages name the target.
//
// Both foldings answer the same pressure — this is the narrowest table in the
// application, and what a `nexus.example.com/docker-hosted/` prefix pushes out
// of the cell is the part that identifies the image. And both obey the same
// rule: the cache key stays the value everything else uses, so folding here
// cannot reach a loader, a scanner, or the directory a .gitleaksignore is
// written into.
func (t scanTarget) shortName() string {
	if t.Kind == kindRepo {
		return shortenHome(t.Name)
	}
	if t.Display != "" {
		return t.Display
	}
	return t.Name
}

// secrets is the row's verdict. Une ligne purgée par ctrl+a n'a plus de verdict
// non plus : ses compteurs affichent `-`, et l'icône dit la même chose.
func (t scanTarget) secrets() theme.SecretsState {
	return theme.SecretsVerdict(t.Sensitive, t.Scanned)
}

// ci is the row's grade verdict, computed the way the workspaces list computes
// it — same function, same three absences. A purged row has no grade either:
// its counters print "-" and so does this column.
func (t scanTarget) ci() theme.CIScoreState {
	return theme.CIScoreVerdict(t.CIScore, t.Scanned, t.CIGradeable)
}

// labelFor names a target the way the table does, for the messages that have
// only a cache key to go on.
//
// It resolves the row rather than folding the string blind: an absolute
// repository path can legitimately begin with a configured registry URL, and
// only the row knows which of the two caches the name came from. A name with no
// row — nothing has been loaded yet — is returned untouched, which is worse
// than the alias but never wrong.
func (m Model) labelFor(name string) string {
	for _, t := range m.inventory.Items() {
		if t.Name == name {
			return t.shortName()
		}
	}
	return name
}

// labelForResult folds a result's target the same way, for the one caller that
// has no inventory to resolve against: a view constructed straight onto a
// stored result. The result carries its own TargetType, which is the same fact
// targetKind holds.
func labelForResult(cfg *config.Config, result *scan.Result) string {
	if result == nil {
		return ""
	}
	if result.TargetType != scan.TargetImage {
		return shortenHome(result.Target)
	}
	return docker.ApplyAliases(result.Target, registryalias.From(cfg.Registry.Registries))
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
		Title: title, Sizing: datatable.SizingFixed, MinWidth: countColumnWidth,
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

// ciColumnWidth is four cells for one letter: the title is what costs, not the
// value. Same width as the workspaces list gives it.
const ciColumnWidth = 4

// ciColumn is the pipeline grade, and it exists only when scan.enable_ci_score
// is on — off, it would be four cells of nothing on every row.
//
// It does not declare Optional, for the reason the workspaces column does not:
// its states already include two absences that mean different things, and a
// column that vanished on a narrow terminal would add a third that looks like
// them.
//
// It does not sort either. datatable reserves width(title)+2 for a sortable
// column's arrow, which would cost this one six cells instead of four — in the
// narrowest table in the application, where Target pays for it at 80 columns.
func ciColumn() datatable.Column[scanTarget] {
	return datatable.Column[scanTarget]{
		Title: "CI", Sizing: datatable.SizingFixed, MinWidth: ciColumnWidth,
		Cell:  func(t scanTarget) string { return theme.CIScoreCell(t.ci(), t.CIScore) },
		Style: func(t scanTarget) lipgloss.Style { return theme.CIScoreStyle(t.ci(), t.CIScore) },
	}
}

// inventoryColumns describes the inventory table.
func inventoryColumns(withCI bool) []datatable.Column[scanTarget] {
	cols := []datatable.Column[scanTarget]{
		{
			// No title: the column carries a glyph, and a header over it would
			// name something read at a glance anyway. It declares neither Less
			// nor Search — it adds no text anyone could type, so the filter
			// stays on the target's two names.
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell: func(t scanTarget) string { return t.kindIcon() },
		},
		{
			// Two cells narrower than before on both bounds: the glyph and its
			// space left the cell, so what is measured here is the name alone.
			Title: "Target", Sizing: datatable.SizingContent,
			MinWidth: 22, MaxWidth: 58, Flex: 1, TruncateHead: true,
			Cell: func(t scanTarget) string { return t.shortName() },
			// The cache key, not the alias: an alias is a display name the user
			// can rename, and sorting by it would move every row of a registry
			// the day they do.
			Less: func(a, b scanTarget) bool {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			},
			// Both names. A query for the full path of a repository has to match
			// the row that folds it to "~", and a query for the alias the row
			// actually shows has to match it too — a column saying one name while
			// the search wants the other reads as a bug.
			Search: func(t scanTarget) string { return t.Name + " " + t.Display },
		},
		{
			Title: "Secrets", Sizing: datatable.SizingFixed, MinWidth: secretsColumnWidth,
			Cell:  func(t scanTarget) string { return theme.SecretsIcon(t.secrets()) },
			Style: func(t scanTarget) lipgloss.Style { return theme.SecretsStyle(t.secrets()) },
		},
		countColumn("CRIT", "CRITICAL", func(c scan.SeverityCounts) int { return c.Critical }),
		countColumn("HIGH", "HIGH", func(c scan.SeverityCounts) int { return c.High }),
		countColumn("MED", "MEDIUM", func(c scan.SeverityCounts) int { return c.Medium }),
		countColumn("LOW", "LOW", func(c scan.SeverityCounts) int { return c.Low }),
	}
	if withCI {
		// Beside the four severity counters, before Scanned — where a reader
		// looks for what a scan concluded, and the placement the workspaces
		// list uses. Appending here also leaves CRIT at inventoryColumnCritical,
		// which is what the table opens sorted by.
		cols = append(cols, ciColumn())
	}
	return append(cols, datatable.Column[scanTarget]{
		Title: "Scanned", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 14,
		Cell:  inventoryScannedCell,
		Style: inventoryScannedStyle,
		Less:  func(a, b scanTarget) bool { return a.ScannedAt.Before(b.ScannedAt) },
	})
}

// newInventoryTable builds the inventory table.
func newInventoryTable(withCI bool) datatable.Model[scanTarget] {
	return datatable.New(datatable.Config[scanTarget]{
		Columns:    inventoryColumns(withCI),
		SortColumn: inventoryColumnCritical,
		SortDesc:   true,
	})
}
