package ociresources

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/datatable"
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
	// repoPrefix goes in front of the repository name. With URL it forms the
	// entry's address, and both the browse path and the pull reference derive
	// from that one pair — which is what stops them meaning two different
	// repositories (D39, §3.18).
	repoPrefix string
	// Slug identifies the entry: its own for a standalone registry, its group's
	// for a member. It is what the result filter resolves labels through, and
	// what replaced matching on URL — two registries configured with the same
	// URL used to collide.
	Slug        string
	ParentAlias string // non-empty = member of a group
	ParentSlug  string
	parentURL   string // URL of the parent RegistryItem (for credential lookup)
	// key identifies the entry among all the others, and is what the checkbox,
	// the remembered exclusions and every result are keyed on (D40). It is not
	// the URL: two registries may be declared on one host — the form enforces
	// slug uniqueness, not URL uniqueness — and §3.18 makes that the ordinary
	// case, since a member is now a bare host plus a prefix. Nor is it Slug,
	// which holds the *group's* slug for a member and would give a whole group
	// one checkbox. So: the slug for a standalone registry, and the group's slug
	// plus the member's address for a member — the address, not the alias:
	// cleanMemberAlias strips `-proxy` and `-hosted`, so `docker-io-proxy` and
	// `docker-io-hosted` both display as `docker-io`.
	key string
	// authMode is already resolved: a member carries what its group settled on,
	// since `inherit` is the only thing the shared credential entry can mean.
	authMode string
}

// resultFilter narrows the tag table to one group or one registry. Both empty
// means everything.
//
// The single-registry level is an entry key rather than a URL: several entries
// share one host once a member is a host plus a prefix, so a URL would select
// every proxy on that instance at once (§3.18, and D40 one screen along).
type resultFilter struct {
	groupSlug string
	entryKey  string
}

func (f resultFilter) isEmpty() bool { return f.groupSlug == "" && f.entryKey == "" }

// matches reports whether a result from entryKey passes the filter. A group
// matches every one of its members, which is what the group level is for.
func (f resultFilter) matches(entryKey string, entry *browserRegistryEntry) bool {
	switch {
	case f.isEmpty():
		return true
	case f.groupSlug != "":
		return entry != nil && entry.ParentSlug == f.groupSlug
	default:
		return entryKey == f.entryKey
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

// tagRow is one line of the tag table: the tag, plus what the row says about it
// that does not live on the tag — the registry's display label, the cached scan
// counts and whether a scan is running.
//
// The columns are built once, in newRegistryBrowser, so their Cell functions
// cannot reach back into the browser for any of that. It is the imageRow
// pattern, and it exists for the same reason: a column sorts by the value it
// prints.
type tagRow struct {
	tag      MultiRegistryTag
	label    string // registry alias, or its URL when it has none
	entry    cache.ImageScanEntry
	scanned  bool
	scanning bool
	// frame is the spinner's current character, plain text because a table cell
	// carries no escape sequence (Rule 122).
	frame string
}

// tagColumns describes the tag results table.
func tagColumns() []datatable.Column[tagRow] {
	return []datatable.Column[tagRow]{
		{
			Title: "Registry", Sizing: datatable.SizingContent, MinWidth: 20,
			Cell: func(r tagRow) string { return r.label },
		},
		{
			Title: "Tag", Sizing: datatable.SizingContent, MinWidth: 30, Flex: 1,
			Cell: func(r tagRow) string { return r.tag.Tag },
			Less: func(a, b tagRow) bool {
				return strings.ToLower(a.tag.Tag) < strings.ToLower(b.tag.Tag)
			},
		},
		{
			Title: "Updated", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 16,
			Cell: func(r tagRow) string {
				if r.tag.UpdatedAt.IsZero() {
					return "-"
				}
				return theme.TimeAgo(r.tag.UpdatedAt)
			},
			// Newest first is this column's ascending order: "most recently
			// updated" is what anyone sorting by it is after.
			Less: func(a, b tagRow) bool { return a.tag.UpdatedAt.After(b.tag.UpdatedAt) },
		},
		tagCVEColumn("C", func(e cache.ImageScanEntry) int { return e.Critical }),
		tagCVEColumn("H", func(e cache.ImageScanEntry) int { return e.High }),
		tagCVEColumn("M", func(e cache.ImageScanEntry) int { return e.Medium }),
		tagCVEColumn("L", func(e cache.ImageScanEntry) int { return e.Low }),
	}
}

// tagCVEColumn builds one of the four severity count columns: the spinner while
// a scan runs, the count once one has, and a dash for a tag never scanned —
// nought findings and never looked at are different answers.
func tagCVEColumn(title string, get func(cache.ImageScanEntry) int) datatable.Column[tagRow] {
	return datatable.Column[tagRow]{
		Title: title, Sizing: datatable.SizingFixed, MinWidth: 4,
		Cell: func(r tagRow) string {
			switch {
			case r.scanning:
				return r.frame
			case r.scanned:
				return strconv.Itoa(get(r.entry))
			default:
				return "-"
			}
		},
	}
}

const brFieldRepo = 0

// brFieldReg returns the form field index for the i-th picker row.
func (b *RegistryBrowser) brFieldReg(i int) int { return 1 + i }

// brFieldSubmit returns the form field index for the Browse Tags button.
func (b *RegistryBrowser) brFieldSubmit() int { return 1 + len(b.rows) }

// RegistryBrowserCloseMsg is sent when the user presses ESC on the browser input screen.
type RegistryBrowserCloseMsg struct{}

// The two sortable columns of the tag table. `.` cycles Tag ascending, Tag
// descending, Updated, Updated descending and back — which is what
// datatable.CycleSort does over exactly these two, so the cycle is no longer
// written out.
const (
	tagColumnTag     = 1
	tagColumnUpdated = 2
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
	selectedRegs map[string]bool // entry key → checked
	focusedField int

	// Results state
	tags              []MultiRegistryTag
	pendingSearches   int
	registryFilter    resultFilter
	tagTable          datatable.Model[tagRow]
	scanCache         map[string]cache.ImageScanEntry
	scanningTags      map[string]bool
	tagScanSpinnerIdx int

	// Tag filter in the results view. The sort belongs to the table now; this
	// filter stays here because it narrows the tags before the table sees them,
	// alongside registryFilter, and because it is drawn in the OCI view's own
	// footer rather than the table's bar.
	filterInput  textinput.Model
	filterActive bool

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
		tagTable: datatable.New(datatable.Config[tagRow]{
			Columns:    tagColumns(),
			SortColumn: tagColumnTag,
		}),
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
				repoPrefix: reg.RepoPrefix,
				key:        reg.Slug,
			})
			continue
		}

		b.rows = append(b.rows, pickerRow{groupSlug: reg.Slug, label: alias, entry: -1})
		for _, m := range members {
			b.rows = append(b.rows, pickerRow{label: "  " + m.Alias, entry: len(b.entries)})
			b.entries = append(b.entries, browserRegistryEntry{
				URL:         m.URL,
				Alias:       m.Alias,
				repoPrefix:  m.RepoPrefix,
				Slug:        reg.Slug,
				ParentAlias: alias,
				ParentSlug:  reg.Slug,
				parentURL:   reg.URL,
				authMode:    mode,
				key:         memberKey(reg.Slug, m.URL, m.RepoPrefix),
			})
		}
	}

	b.selectedRegs = make(map[string]bool, len(b.entries))
	for _, e := range b.entries {
		b.selectedRegs[e.key] = !deselected[e.key]
	}
}

// memberKey names one member of a group. Scoping it by the group's slug is what
// keeps two groups fronting the same repository apart, and what keeps a member
// from colliding with a standalone registry keyed on its own slug.
//
// The URL alone will not do: every member of a path-routed group answers to the
// same host now, and the prefix is the whole of what tells them apart (§3.18).
func memberKey(groupSlug, memberURL, repoPrefix string) string {
	return groupSlug + "/" + memberURL + "/" + repoPrefix
}

// browserAlias returns what a registry is labelled with in the browser.
//
// With no alias it falls back to the entry's *address* rather than its URL: one
// line per proxy is one host repeated, so the URL alone would label every proxy
// of an instance identically (§3.18).
func browserAlias(reg config.RegistryItem) string {
	if reg.Alias != "" {
		return reg.Alias
	}
	return registryRef(reg.URL, reg.RepoPrefix)
}

// selected reports whether an entry is checked.
func (b *RegistryBrowser) selected(e browserRegistryEntry) bool { return b.selectedRegs[e.key] }

// groupState reports whether all, none or some of a group's members are checked.
func (b *RegistryBrowser) groupState(slug string) theme.CheckState {
	checked, total := 0, 0
	for _, e := range b.entries {
		if e.ParentSlug != slug {
			continue
		}
		total++
		if b.selected(e) {
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
			b.selectedRegs[e.key] = want
		}
	}
}

// Deselected returns the keys of the entries the user unchecked, which is what
// is remembered between visits.
func (b *RegistryBrowser) Deselected() []string {
	var out []string
	for _, e := range b.entries {
		if !b.selected(e) {
			out = append(out, e.key)
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

// resizeTagTable hands the table the viewport width.
//
// b.width is already the content width — the caller subtracted the borders —
// and datatable.Resize subtracts them itself, so they are added back here. The
// column solving, the Rule 116 remainder and the selected row's width are all
// its business now.
func (b *RegistryBrowser) resizeTagTable() {
	b.tagTable.Resize(b.width+2, max(b.height, 1))
}
