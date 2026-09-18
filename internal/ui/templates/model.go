// Package templates is the `:templates` screen: the catalog of repository
// templates a new repository can be filled from.
//
// An entry is a reference — a git repository, a local checkout, an OCI
// artifact — and the content is fetched where it lives when it is needed, so
// nothing here can drift from the original. The model and the fetching are
// internal/template; this package is the screen over them.
package templates

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// row is one line of the table.
type row struct {
	Entry template.Entry
}

// Model is the templates view.
type Model struct {
	config  *config.Config
	secrets credentials.Storage

	// path is the catalog file. It is a field so a test can point the view at a
	// temporary one; New sets it to ~/.devdesk/templates.yaml.
	path string

	// store is nil until the catalog has been opened, and stays nil when it
	// could not be read — storeErr says why.
	store    *template.Store
	storeErr error

	declared []template.Entry

	table datatable.Model[row]

	// form and confirmModal are modes: each replaces the list while it is open
	// (Rule 130).
	form         *entryForm
	confirmModal *sharedcomponents.ConfirmModal
	// pendingDelete is the slug the modal is asking about.
	pendingDelete string

	// selecting marks a view lent to another to pick one template: it answers
	// with TemplateSelectedMsg or SelectionCancelledMsg and edits nothing.
	// selectionMessage is what the footer says meanwhile.
	selecting        bool
	selectionMessage string

	footer sharedcomponents.FooterMessage

	width, height int
}

// Column indexes. Only columnName is referred to — the table opens sorted by
// it — and the block is what keeps that true when a column is inserted above.
const (
	columnIcon = iota
	columnName
	columnTags
	columnSource
	columnRef
)

// New builds the view. secrets resolves the credentials a registry or a forge
// host needs; it may be nil, in which case everything is fetched anonymously.
func New(cfg *config.Config, secrets credentials.Storage) Model {
	path, _ := template.DefaultPath()
	return NewWithPath(cfg, secrets, path)
}

// NewWithPath is New with the catalog file named.
func NewWithPath(cfg *config.Config, secrets credentials.Storage, path string) Model {
	return Model{
		config:  cfg,
		secrets: secrets,
		path:    path,
		table: datatable.New(datatable.Config[row]{
			Columns:    columns(),
			SortColumn: columnName,
		}),
	}
}

// NewForSelection builds the view in selection mode, to be lent to another view
// that needs a template chosen — the repository-creation form.
//
// It is the whole catalog, filterable and sortable, with a preview: the choice
// is between things a reader has to be able to tell apart, which a one-line
// dropdown cannot show. It changes nothing: N, E and D are absent, not greyed
// (Rule 130 — a mode replaces the list).
func NewForSelection(cfg *config.Config, secrets credentials.Storage, message string) Model {
	m := New(cfg, secrets)
	m.selecting = true
	m.selectionMessage = message
	return m
}

// columns describes the table.
//
// The first is the source's kind, as a glyph in a column of its own (Rule
// 125). Its colour comes from a role rather than a hue: a git repository is a
// repository and a checkout on disk is a directory in `ws`, `:sec` and the
// explorer, and a template's source reads the same here — the roles are shared,
// not duplicated.
func columns() []datatable.Column[row] {
	return []datatable.Column[row]{
		{
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(r row) string { return kindGlyph(r.Entry.Source.Kind) },
			Style: func(r row) lipgloss.Style { return theme.IconStyle(kindRole(r.Entry.Source.Kind)) },
		},
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: 16, MaxWidth: 40, Flex: 2,
			Cell: func(r row) string { return r.Entry.Name },
			Less: func(a, b row) bool { return strings.ToLower(a.Entry.Name) < strings.ToLower(b.Entry.Name) },
			// The description is searchable without a column of its own: it is
			// long, and the width is worth more to the source.
			Search: func(r row) string { return r.Entry.Name + " " + r.Entry.Description },
		},
		{
			Title: "Tags", Sizing: datatable.SizingContent, MinWidth: 8, MaxWidth: 30, Optional: true,
			Cell:   func(r row) string { return dashIfEmpty(strings.Join(r.Entry.Tags, ", ")) },
			Style:  func(r row) lipgloss.Style { return dimIfEmpty(len(r.Entry.Tags) == 0) },
			Less:   func(a, b row) bool { return strings.Join(a.Entry.Tags, ",") < strings.Join(b.Entry.Tags, ",") },
			Search: func(r row) string { return strings.Join(r.Entry.Tags, " ") },
		},
		{
			Title: "Source", Sizing: datatable.SizingContent,
			MinWidth: 16, MaxWidth: 60, Flex: 3, TruncateHead: true, Optional: true,
			Cell:   func(r row) string { return sourceSummary(r.Entry.Source) },
			Less:   func(a, b row) bool { return sourceSummary(a.Entry.Source) < sourceSummary(b.Entry.Source) },
			Search: func(r row) string { return sourceSummary(r.Entry.Source) },
		},
		{
			Title: "Ref", Sizing: datatable.SizingContent, MinWidth: 6, MaxWidth: 16, Optional: true,
			Cell:  func(r row) string { return dashIfEmpty(r.Entry.Source.Ref) },
			Style: func(r row) lipgloss.Style { return dimIfEmpty(r.Entry.Source.Ref == "") },
			Less:  func(a, b row) bool { return a.Entry.Source.Ref < b.Entry.Source.Ref },
		},
	}
}

// kindGlyph, kindRole: what a source's kind looks like. A Nerd Font glyph, so
// the column is left-aligned and untitled (Rule 125).
func kindGlyph(k template.Kind) string {
	switch k {
	case template.KindLocal:
		return theme.IconDirectory
	case template.KindOCI:
		return theme.IconDocker
	default:
		return theme.IconRepository
	}
}

func kindRole(k template.Kind) theme.IconRole {
	switch k {
	case template.KindLocal:
		return theme.IconRoleDirectory
	case template.KindOCI:
		return theme.IconRoleImage
	default:
		return theme.IconRoleRepository
	}
}

// sourceSummary is where the content lives, in one cell.
func sourceSummary(s template.Source) string {
	switch s.Kind {
	case template.KindLocal:
		return s.Path
	case template.KindOCI:
		return strings.TrimRight(s.URL, "/") + "/" + s.Path
	default:
		if s.Path != "" {
			return s.URL + " · " + s.Path
		}
		return s.URL
	}
}

// dashIfEmpty is the placeholder for a cell with nothing in it: a dash rather
// than a blank, so the column still reads as a column (Rule 122).
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// dimIfEmpty dims an absence, which is Rule 122's rule for a zero: the value
// that is there is what deserves to be read.
func dimIfEmpty(empty bool) lipgloss.Style {
	if empty {
		return theme.DimStyle
	}
	return lipgloss.NewStyle()
}

// rebuild recomputes the rows from the catalog.
func (m *Model) rebuild() {
	rows := make([]row, len(m.declared))
	for i, e := range m.declared {
		rows[i] = row{Entry: e}
	}
	m.table.SetItems(rows)
}

// selectedEntry is the entry under the cursor.
func (m *Model) selectedEntry() (template.Entry, bool) {
	r, ok := m.table.Selected()
	return r.Entry, ok
}
