package ociresources

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// browserState tracks which screen the browser is on.
type browserState int

// browserStateResolving is gone. The browser used to open into it and ignore
// every key — esc included — while up to one 8-second detection per configured
// registry ran (D13). Members now come from config plus the group cache, so
// there is nothing to resolve and the form renders at once, offline included.
const (
	browserStateInput  browserState = iota // search form
	browserStateTags                       // results table
	browserStateStatus                     // spinner during pull
)

// browserRegistryEntry is one selectable registry in the browser form.
// Group members have ParentSlug set; standalone registries have it empty.
type browserRegistryEntry struct {
	URL   string
	Alias string
	// Slug identifies the entry: its own for a standalone registry, its group's
	// for a member. It is what the result filter resolves labels through, and
	// what replaced matching on URL — two registries configured with the same
	// URL used to collide.
	Slug        string
	ParentAlias string // non-empty = member of a group
	ParentSlug  string
	parentURL   string // URL of the parent RegistryItem (for credential lookup)
	// authMode is already resolved: a member carries what its group settled on,
	// since `inherit` is the only thing the shared credential entry can mean.
	authMode string
}

// resultFilter narrows the tag table to one group or one registry. Both empty
// means everything.
type resultFilter struct {
	groupSlug string
	url       string
}

func (f resultFilter) isEmpty() bool { return f.groupSlug == "" && f.url == "" }

// matches reports whether a result from url passes the filter. A group matches
// every member's URL, which is what the group level is for.
func (f resultFilter) matches(url string, entry *browserRegistryEntry) bool {
	switch {
	case f.isEmpty():
		return true
	case f.groupSlug != "":
		return entry != nil && entry.ParentSlug == f.groupSlug
	default:
		return url == f.url
	}
}

// pickerRow is one line of the registry picker. A group is a row of its own
// whose checkbox covers its members, which is the only way to select eight
// proxies without eight keystrokes.
type pickerRow struct {
	// groupSlug is set on a group header; entry is -1 there.
	groupSlug string
	label     string
	entry     int // index into b.entries, or -1
}

const brFieldRepo = 0

// brFieldReg returns the form field index for the i-th picker row.
func (b *RegistryBrowser) brFieldReg(i int) int { return 1 + i }

// brFieldSubmit returns the form field index for the Browse Tags button.
func (b *RegistryBrowser) brFieldSubmit() int { return 1 + len(b.rows) }

// RegistryBrowserCloseMsg is sent when the user presses ESC on the browser input screen.
type RegistryBrowserCloseMsg struct{}

// tagSortField selects which column to sort the tag list by.
type tagSortField int

const (
	tagSortByName    tagSortField = iota
	tagSortByUpdated              // newest first
)

// RegistryBrowser searches image tags across multiple configured registries.
type RegistryBrowser struct {
	registries    []config.RegistryItem
	width, height int

	// Entries, built once from config plus the group cache.
	entries []browserRegistryEntry
	rows    []pickerRow

	// Form state
	repoInput    textinput.Model
	selectedRegs map[string]bool // entry URL → checked
	focusedField int

	// Results state
	tags              []MultiRegistryTag
	pendingSearches   int
	registryFilter    resultFilter
	tagTable          table.Model
	scanCache         map[string]cache.ImageScanEntry
	scanningTags      map[string]bool
	tagScanSpinnerIdx int

	// Tag filter/sort in results view
	filterInput  textinput.Model
	filterActive bool
	tagSortCol   tagSortField
	tagSortDesc  bool

	// Status screen (pull operation)
	spinner   spinner.Model
	operation string
	imageName string

	state browserState
}

// newRegistryBrowser creates a RegistryBrowser showing its form immediately.
//
// A group's members come from the cache the Registries tab refreshes, so
// opening the browser reads a file rather than the network — which is what
// removed the resolving state D13 is about, rather than papering over it.
func newRegistryBrowser(
	registries []config.RegistryItem,
	groups map[string]cache.RegistryGroupEntry,
	deselected map[string]bool,
	width, height int,
) *RegistryBrowser {
	repoInput := textinput.New()
	repoInput.Placeholder = "nginx  (or library/nginx)"
	repoInput.CharLimit = 256
	theme.StyleTextInput(&repoInput)
	repoInput.Focus()

	fi := textinput.New()
	fi.Placeholder = "filter tags…"
	fi.CharLimit = 128
	theme.StyleTextInput(&fi)

	tagCols := []table.Column{
		{Title: "Registry", Width: 20},
		{Title: "Tag", Width: 30},
		{Title: "Updated", Width: 16},
		{Title: "C", Width: 4},
		{Title: "H", Width: 4},
		{Title: "M", Width: 4},
		{Title: "L", Width: 4},
	}
	tt := table.New(
		table.WithColumns(tagCols),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	tt.SetStyles(theme.DefaultTableStyles())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	b := &RegistryBrowser{
		registries:   registries,
		width:        width,
		height:       height,
		repoInput:    repoInput,
		selectedRegs: make(map[string]bool),
		focusedField: brFieldRepo,
		filterInput:  fi,
		tagSortCol:   tagSortByName,
		tagSortDesc:  false,
		tagTable:     tt,
		spinner:      sp,
		scanCache:    make(map[string]cache.ImageScanEntry),
		scanningTags: make(map[string]bool),
		state:        browserStateInput,
	}
	b.buildEntries(groups, deselected)
	b.resizeInputs()
	b.resizeTagTable()
	b.updateFocus()
	return b
}

// buildEntries expands every configured registry into the lines the picker
// shows: a group into its cached members under a header, anything else into
// itself.
//
// A selection is remembered as the entries that were *un*checked, so a member
// discovered since the last visit arrives checked like every other new one.
func (b *RegistryBrowser) buildEntries(groups map[string]cache.RegistryGroupEntry, deselected map[string]bool) {
	b.entries = nil
	b.rows = nil
	for _, reg := range b.registries {
		alias := browserAlias(reg)
		// Members share their group's host and therefore its single credential
		// entry, so they share the decision the group made about using it.
		mode := config.ResolveAuthMode(reg, nil)

		members := groups[reg.Slug].Members
		if reg.Kind != config.KindGroup || len(members) == 0 {
			// A group nobody has discovered yet is offered as itself: it is a
			// pullable registry too, and hiding it would be worse than listing
			// it without the members it may have.
			b.rows = append(b.rows, pickerRow{label: alias, entry: len(b.entries)})
			b.entries = append(b.entries, browserRegistryEntry{
				URL: reg.URL, Alias: alias, Slug: reg.Slug, authMode: mode,
			})
			continue
		}

		b.rows = append(b.rows, pickerRow{groupSlug: reg.Slug, label: alias, entry: -1})
		for _, m := range members {
			b.rows = append(b.rows, pickerRow{label: "  " + m.Alias, entry: len(b.entries)})
			b.entries = append(b.entries, browserRegistryEntry{
				URL:         m.URL,
				Alias:       m.Alias,
				Slug:        reg.Slug,
				ParentAlias: alias,
				ParentSlug:  reg.Slug,
				parentURL:   reg.URL,
				authMode:    mode,
			})
		}
	}

	b.selectedRegs = make(map[string]bool, len(b.entries))
	for _, e := range b.entries {
		b.selectedRegs[e.URL] = !deselected[e.URL]
	}
}

// browserAlias returns what a registry is labelled with in the browser.
func browserAlias(reg config.RegistryItem) string {
	if reg.Alias != "" {
		return reg.Alias
	}
	return reg.URL
}

// groupState reports whether all, none or some of a group's members are checked.
func (b *RegistryBrowser) groupState(slug string) theme.CheckState {
	checked, total := 0, 0
	for _, e := range b.entries {
		if e.ParentSlug != slug {
			continue
		}
		total++
		if b.selectedRegs[e.URL] {
			checked++
		}
	}
	switch {
	case total == 0 || checked == 0:
		return theme.CheckNone
	case checked == total:
		return theme.CheckAll
	default:
		return theme.CheckSome
	}
}

// toggleGroup checks every member of a group, or unchecks them when they were
// all already checked. A partial selection resolves upwards: the user is more
// likely to be completing it than discarding it.
func (b *RegistryBrowser) toggleGroup(slug string) {
	want := b.groupState(slug) != theme.CheckAll
	for _, e := range b.entries {
		if e.ParentSlug == slug {
			b.selectedRegs[e.URL] = want
		}
	}
}

// Deselected returns the entries the user unchecked, which is what is
// remembered between visits.
func (b *RegistryBrowser) Deselected() []string {
	var out []string
	for _, e := range b.entries {
		if !b.selectedRegs[e.URL] {
			out = append(out, e.URL)
		}
	}
	return out
}

// SetSize updates dimensions and resizes internal components.
func (b *RegistryBrowser) SetSize(width, height int) {
	b.width = width
	b.height = height
	b.resizeInputs()
	b.resizeTagTable()
}

func (b *RegistryBrowser) resizeInputs() {
	w := max(b.width-22, 20)
	b.repoInput.Width = w
	b.filterInput.Width = max(b.width-6, 10)
}

func (b *RegistryBrowser) resizeTagTable() {
	b.tagTable.SetHeight(max(b.height, 1))

	// b.width is already the viewport content width (full width - 2 borders).
	// Subtract only the cell padding (numCols × 2) so column widths sum exactly to `available`.
	const numCols = 7
	available := max(b.width-numCols*2, 10)
	fixedReg := 20
	fixedUpdated := 16
	fixedC, fixedH, fixedM := 4, 4, 4
	fixedSum := fixedReg + fixedUpdated + fixedC + fixedH + fixedM
	flexTag := max(available-fixedSum-4, 8) // reserve 4 for last col minimum

	cols := b.tagTable.Columns()
	if len(cols) == numCols {
		cols[0].Width = fixedReg
		cols[1].Width = flexTag
		cols[2].Width = fixedUpdated
		cols[3].Width = fixedC
		cols[4].Width = fixedH
		cols[5].Width = fixedM
		// Last column absorbs leftover so sum(col_widths) == available exactly.
		cols[6].Width = available - fixedReg - flexTag - fixedUpdated - fixedC - fixedH - fixedM
		b.tagTable.SetColumns(cols)
	}

	// Force the Selected row style to span the viewport content width so the selected
	// background extends to the right border, even when row content visually differs
	// from the calculated width (e.g. Nerd Font icon width discrepancies). See workspaces fix.
	styles := theme.DefaultTableStyles()
	styles.Selected = styles.Selected.Width(b.width)
	b.tagTable.SetStyles(styles)
}
