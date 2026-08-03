package ociresources

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// browserState tracks which screen the browser is on.
type browserState int

const (
	browserStateInput     browserState = iota // search form
	browserStateTags                          // results table
	browserStateStatus                        // spinner during pull
	browserStateResolving                     // detecting group members
)

// browserRegistryEntry is one selectable registry in the browser form.
// Group members have ParentAlias set; normal registries have it empty.
type browserRegistryEntry struct {
	URL         string
	Alias       string
	ParentAlias string // non-empty = member of a group
	parentURL   string // URL of the parent RegistryItem (for credential lookup)
}

const brFieldRepo = 0

// brFieldReg returns the form field index for the i-th registry checkbox.
func (b *RegistryBrowser) brFieldReg(i int) int { return 1 + i }

// brFieldSubmit returns the form field index for the Browse Tags button.
func (b *RegistryBrowser) brFieldSubmit() int { return 1 + len(b.entries) }

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

	// Group detection state
	entryGroups       [][]browserRegistryEntry // one slot per registry, filled as detections complete
	entries           []browserRegistryEntry   // flattened from entryGroups after all detections
	pendingDetections int

	// Form state
	repoInput    textinput.Model
	selectedRegs map[string]bool // entry URL → checked
	focusedField int

	// Results state
	tags              []MultiRegistryTag
	pendingSearches   int
	registryFilter    string // "" = all, otherwise filter by RegistryURL
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

// newRegistryBrowser creates a RegistryBrowser and fires group-detection commands for all
// configured registries. The browser starts in browserStateResolving until all detections
// complete; it then transitions to browserStateInput with the resolved entry list.
func newRegistryBrowser(registries []config.RegistryItem, width, height int) (*RegistryBrowser, tea.Cmd) {
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
		registries:        registries,
		entryGroups:       make([][]browserRegistryEntry, len(registries)),
		pendingDetections: len(registries),
		width:             width,
		height:            height,
		repoInput:         repoInput,
		selectedRegs:      make(map[string]bool),
		focusedField:      brFieldRepo,
		filterInput:       fi,
		tagSortCol:        tagSortByName,
		tagSortDesc:       false,
		tagTable:          tt,
		spinner:           sp,
		scanCache:         make(map[string]cache.ImageScanEntry),
		scanningTags:      make(map[string]bool),
		state:             browserStateResolving,
	}
	b.resizeInputs()
	b.resizeTagTable()

	if len(registries) == 0 {
		b.state = browserStateInput
		return b, nil
	}

	cmds := make([]tea.Cmd, 0, len(registries)+1)
	for _, reg := range registries {
		cmds = append(cmds, detectRegistryGroupCmd(reg, ""))
	}
	cmds = append(cmds, sp.Tick)
	return b, tea.Batch(cmds...)
}

// SetSize updates dimensions and resizes internal components.
func (b *RegistryBrowser) SetSize(width, height int) {
	b.width = width
	b.height = height
	b.resizeInputs()
	b.resizeTagTable()
}

// HandleGroupDetected incorporates one detection result into the ordered entry list.
// When all detections have completed it finalizes entries and switches to input state.
func (b *RegistryBrowser) HandleGroupDetected(msg RegistryGroupDetectedMsg) (*RegistryBrowser, tea.Cmd) {
	for i, reg := range b.registries {
		if reg.URL != msg.RegistryURL {
			continue
		}
		alias := reg.Alias
		if alias == "" {
			alias = reg.URL
		}
		if len(msg.Members) > 0 {
			group := make([]browserRegistryEntry, len(msg.Members))
			for j, m := range msg.Members {
				group[j] = browserRegistryEntry{
					URL:         m.URL,
					Alias:       m.Alias,
					ParentAlias: alias,
					parentURL:   reg.URL,
				}
			}
			b.entryGroups[i] = group
		} else {
			b.entryGroups[i] = []browserRegistryEntry{{URL: reg.URL, Alias: alias}}
		}
		break
	}
	b.pendingDetections--
	if b.pendingDetections <= 0 {
		b.finalizeEntries()
	}
	return b, nil
}

// finalizeEntries flattens entryGroups into entries, initialises selectedRegs (all checked),
// and transitions the browser to the input form state.
func (b *RegistryBrowser) finalizeEntries() {
	b.entries = nil
	for _, group := range b.entryGroups {
		b.entries = append(b.entries, group...)
	}
	b.selectedRegs = make(map[string]bool, len(b.entries))
	for _, e := range b.entries {
		b.selectedRegs[e.URL] = true
	}
	b.state = browserStateInput
	b.updateFocus()
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
