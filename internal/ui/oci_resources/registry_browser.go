package ociresources

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
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

// InEditMode returns true when a text input is active (blocks command mode).
func (b *RegistryBrowser) InEditMode() bool {
	if b.state == browserStateResolving {
		return false
	}
	if b.state == browserStateInput && b.focusedField == brFieldRepo {
		return true
	}
	return b.state == browserStateTags && b.filterActive
}

// IsSearching returns true while registry tag queries are still in flight.
func (b *RegistryBrowser) IsSearching() bool {
	return b.pendingSearches > 0
}

// Update handles all messages for the browser.
func (b *RegistryBrowser) Update(msg tea.Msg) (*RegistryBrowser, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return b.handleKeyMsg(msg)
	case spinner.TickMsg:
		if b.state == browserStateStatus || b.state == browserStateResolving || len(b.scanningTags) > 0 || b.pendingSearches > 0 {
			var cmd tea.Cmd
			b.spinner, cmd = b.spinner.Update(msg)
			if b.state == browserStateTags && len(b.scanningTags) > 0 {
				b.tagScanSpinnerIdx++
				b.rebuildTagTable()
			}
			return b, cmd
		}
		return b, nil
	}
	return b.delegateUpdate(msg)
}

func (b *RegistryBrowser) delegateUpdate(msg tea.Msg) (*RegistryBrowser, tea.Cmd) {
	if b.state == browserStateInput && b.focusedField == brFieldRepo {
		var cmd tea.Cmd
		b.repoInput, cmd = b.repoInput.Update(msg)
		return b, cmd
	}
	if b.state == browserStateTags {
		if b.filterActive {
			var cmd tea.Cmd
			b.filterInput, cmd = b.filterInput.Update(msg)
			b.rebuildTagTable()
			return b, cmd
		}
		var cmd tea.Cmd
		b.tagTable, cmd = b.tagTable.Update(msg)
		return b, cmd
	}
	return b, nil
}

func (b *RegistryBrowser) handleKeyMsg(msg tea.KeyMsg) (*RegistryBrowser, tea.Cmd) {
	switch b.state {
	case browserStateResolving:
		return b, nil
	case browserStateInput:
		return b.handleInputKeyMsg(msg)
	case browserStateTags:
		return b.handleTagsKeyMsg(msg)
	case browserStateStatus:
		return b, nil
	}
	return b, nil
}

func (b *RegistryBrowser) handleInputKeyMsg(msg tea.KeyMsg) (*RegistryBrowser, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return b, func() tea.Msg { return RegistryBrowserCloseMsg{} }
	case "down":
		b.focusedField = min(b.focusedField+1, b.brFieldSubmit())
		b.updateFocus()
		return b, nil
	case "up":
		b.focusedField = max(b.focusedField-1, brFieldRepo)
		b.updateFocus()
		return b, nil
	case " ":
		if b.focusedField >= 1 && b.focusedField <= len(b.entries) {
			url := b.entries[b.focusedField-1].URL
			b.selectedRegs[url] = !b.selectedRegs[url]
			return b, nil
		}
	case "enter":
		if b.focusedField == brFieldRepo || b.focusedField == b.brFieldSubmit() {
			return b.submitSearch()
		}
		b.focusedField = min(b.focusedField+1, b.brFieldSubmit())
		b.updateFocus()
		return b, nil
	}
	return b.delegateUpdate(msg)
}

func (b *RegistryBrowser) submitSearch() (*RegistryBrowser, tea.Cmd) {
	repo := strings.TrimSpace(b.repoInput.Value())
	if repo == "" {
		return b, nil
	}

	b.tags = nil
	b.registryFilter = ""
	b.pendingSearches = 0
	b.filterInput.SetValue("")
	b.filterActive = false
	b.tagSortCol = tagSortByName
	b.tagSortDesc = false

	var cmds []tea.Cmd
	for _, entry := range b.entries {
		if !b.selectedRegs[entry.URL] {
			continue
		}
		// Credentials come from the parent registry (or the entry itself for normal registries).
		credURL := entry.URL
		if entry.parentURL != "" {
			credURL = entry.parentURL
		}
		var username string
		for _, reg := range b.registries {
			if reg.URL == credURL {
				username = reg.Username
				break
			}
		}
		storedUser, storedPass, _ := docker.GetStoredCreds(credURL)
		if username == "" {
			username = storedUser
		}
		alias := entry.Alias
		if entry.ParentAlias != "" {
			alias = entry.ParentAlias + "/" + entry.Alias
		}
		normalizedRepo := normalizeRepoForRegistry(entry.URL, repo)
		apiURL := registryAPIURL(entry.URL)
		b.pendingSearches++
		cmds = append(cmds, searchRegistryTagsCmd(entry.URL, alias, apiURL, normalizedRepo, username, storedPass))
	}

	if len(cmds) == 0 {
		return b, nil
	}

	b.state = browserStateTags
	b.tagTable.Focus()
	b.rebuildTagTable()
	cmds = append(cmds, b.spinner.Tick)
	return b, tea.Batch(cmds...)
}

func (b *RegistryBrowser) handleTagsKeyMsg(msg tea.KeyMsg) (*RegistryBrowser, tea.Cmd) {
	if b.filterActive {
		switch msg.String() {
		case "esc", "enter":
			b.filterActive = false
			b.filterInput.Blur()
			b.rebuildTagTable()
			b.resizeTagTable()
			return b, nil
		}
		return b.delegateUpdate(msg)
	}

	switch msg.String() {
	case "esc":
		b.state = browserStateInput
		b.tagTable.Blur()
		b.updateFocus()
		return b, nil
	case "/":
		b.filterActive = true
		b.filterInput.Focus()
		b.resizeTagTable()
		return b, nil
	case ".":
		b.cycleSortTags()
		return b, nil
	case "r":
		b.cycleRegistryFilter()
		return b, nil
	case "up", "k":
		b.tagTable.MoveUp(1)
		return b, nil
	case "down", "j":
		b.tagTable.MoveDown(1)
		return b, nil
	case "g", "home":
		b.tagTable.GotoTop()
		return b, nil
	case "G", "end":
		b.tagTable.GotoBottom()
		return b, nil
	case "enter":
		return b.openTagScanDetails()
	case "p":
		return b.pullSelectedTag()
	case "ctrl+s":
		return b.requestDirectScan()
	}
	return b, nil
}

func (b *RegistryBrowser) cycleSortTags() {
	if b.tagSortCol == tagSortByName && !b.tagSortDesc {
		b.tagSortDesc = true
	} else if b.tagSortCol == tagSortByName && b.tagSortDesc {
		b.tagSortCol = tagSortByUpdated
		b.tagSortDesc = false
	} else if b.tagSortCol == tagSortByUpdated && !b.tagSortDesc {
		b.tagSortDesc = true
	} else {
		b.tagSortCol = tagSortByName
		b.tagSortDesc = false
	}
	b.rebuildTagTable()
}

func (b *RegistryBrowser) cycleRegistryFilter() {
	seen := make(map[string]bool)
	var urls []string
	for _, t := range b.tags {
		if !seen[t.RegistryURL] {
			seen[t.RegistryURL] = true
			urls = append(urls, t.RegistryURL)
		}
	}
	if len(urls) <= 1 {
		b.registryFilter = ""
		b.rebuildTagTable()
		return
	}
	if b.registryFilter == "" {
		b.registryFilter = urls[0]
	} else {
		found := false
		for i, u := range urls {
			if u == b.registryFilter {
				next := (i + 1) % (len(urls) + 1)
				if next == len(urls) {
					b.registryFilter = ""
				} else {
					b.registryFilter = urls[next]
				}
				found = true
				break
			}
		}
		if !found {
			b.registryFilter = ""
		}
	}
	b.rebuildTagTable()
}

// registryFilterLabel returns a short display label for the active registry filter.
func (b *RegistryBrowser) registryFilterLabel() string {
	for _, reg := range b.registries {
		if reg.URL == b.registryFilter {
			if reg.Alias != "" {
				return reg.Alias
			}
			return reg.URL
		}
	}
	return b.registryFilter
}

func (b *RegistryBrowser) pullSelectedTag() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	b.imageName = name
	b.operation = "pull"
	b.state = browserStateStatus
	return b, tea.Batch(
		b.spinner.Tick,
		pullRegistryImageCmd(name),
	)
}

// requestDirectScan emits RegistryTagDirectScanMsg for a remote Trivy scan (no pull).
func (b *RegistryBrowser) requestDirectScan() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	return b, func() tea.Msg { return RegistryTagDirectScanMsg{ImageName: name} }
}

// openTagScanDetails emits ScanDetailsRequestMsg when the selected tag has cached results.
func (b *RegistryBrowser) openTagScanDetails() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	if _, ok := b.scanCache[name]; !ok {
		return b, nil
	}
	return b, func() tea.Msg { return ScanDetailsRequestMsg{ImageName: name} }
}

// AddRegistryTags incorporates results from one registry into the combined tag list.
// Always decrements pendingSearches, even on error (tags will be nil).
func (b *RegistryBrowser) AddRegistryTags(registryURL, alias, repo string, tags []string) {
	// Floored: a duplicate or late response would otherwise drive the counter
	// negative, and the next search would start from that base — IsSearching()
	// would stay false while requests were genuinely in flight, so the spinner
	// never showed.
	if b.pendingSearches > 0 {
		b.pendingSearches--
	}
	for _, tag := range tags {
		b.tags = append(b.tags, MultiRegistryTag{
			RegistryURL: registryURL,
			Alias:       alias,
			Repo:        repo,
			Tag:         tag,
		})
	}
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetMultiTagsMeta stores last-updated metadata for tags belonging to one registry.
func (b *RegistryBrowser) SetMultiTagsMeta(registryURL string, meta map[string]time.Time) {
	for i := range b.tags {
		if b.tags[i].RegistryURL != registryURL {
			continue
		}
		if t, ok := meta[b.tags[i].Tag]; ok {
			b.tags[i].UpdatedAt = t
		}
	}
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetTagScanning marks or clears the in-progress scan state for an image tag.
func (b *RegistryBrowser) SetTagScanning(imageName string, scanning bool) {
	if scanning {
		b.scanningTags[imageName] = true
	} else {
		delete(b.scanningTags, imageName)
	}
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// HasSelectedTagScanResults returns true when the selected tag has cached scan results.
func (b *RegistryBrowser) HasSelectedTagScanResults() bool {
	name := b.selectedImageName()
	if name == "" {
		return false
	}
	_, ok := b.scanCache[name]
	return ok
}

// SetScanCache stores the model's scan cache for CVE column display.
func (b *RegistryBrowser) SetScanCache(sc map[string]cache.ImageScanEntry) {
	b.scanCache = sc
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetOperationError returns to tags state (error shown in model footer).
func (b *RegistryBrowser) SetOperationError(_ string) {
	b.state = browserStateTags
	b.imageName = ""
	b.operation = ""
}

// SetOperationSuccess clears the operation and returns to the tags screen.
func (b *RegistryBrowser) SetOperationSuccess() {
	b.state = browserStateTags
	b.imageName = ""
	b.operation = ""
}

// OperationImageName returns the image currently being operated on.
func (b *RegistryBrowser) OperationImageName() string {
	return b.imageName
}

// selectedImageName returns the full image reference for the focused table row.
func (b *RegistryBrowser) selectedImageName() string {
	t := b.getSelectedMultiTag()
	if t == nil {
		return ""
	}
	return multiImageName(t.RegistryURL, t.Repo, t.Tag)
}

// getSelectedMultiTag returns the MultiRegistryTag under the cursor, or nil.
func (b *RegistryBrowser) getSelectedMultiTag() *MultiRegistryTag {
	displayed := b.filteredSortedMultiTags()
	cursor := b.tagTable.Cursor()
	if cursor < 0 || cursor >= len(displayed) {
		return nil
	}
	return &displayed[cursor]
}

// normalizeRepoForRegistry prepends "library/" for bare names on Docker Hub.
func normalizeRepoForRegistry(registryURL, repo string) string {
	if strings.Contains(repo, "/") {
		return repo
	}
	base := strings.ToLower(strings.TrimSuffix(registryURL, "/"))
	for _, alias := range []string{"docker.io", "registry-1.docker.io"} {
		if base == alias {
			return "library/" + repo
		}
	}
	return repo
}

// multiImageName constructs the full image reference for pull/scan.
func multiImageName(registryURL, repo, tag string) string {
	base := strings.TrimSuffix(registryURL, "/")
	for _, alias := range []string{"docker.io", "registry-1.docker.io"} {
		if strings.EqualFold(base, alias) {
			return repo + ":" + tag
		}
	}
	return base + "/" + repo + ":" + tag
}

func (b *RegistryBrowser) updateFocus() {
	b.repoInput.Blur()
	if b.focusedField == brFieldRepo {
		b.repoInput.Focus()
	}
}

// filteredSortedMultiTags returns tags after applying registry filter, text filter, and sort.
func (b *RegistryBrowser) filteredSortedMultiTags() []MultiRegistryTag {
	query := strings.ToLower(b.filterInput.Value())
	var result []MultiRegistryTag
	for _, t := range b.tags {
		if b.registryFilter != "" && t.RegistryURL != b.registryFilter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Tag), query) &&
			!strings.Contains(strings.ToLower(t.Repo), query) {
			continue
		}
		result = append(result, t)
	}

	sortCol := b.tagSortCol
	sortDesc := b.tagSortDesc
	sort.SliceStable(result, func(i, j int) bool {
		var less bool
		switch sortCol {
		case tagSortByUpdated:
			less = result[i].UpdatedAt.After(result[j].UpdatedAt)
		default:
			less = strings.ToLower(result[i].Tag) < strings.ToLower(result[j].Tag)
		}
		if sortDesc {
			return !less
		}
		return less
	})
	return result
}

// rebuildTagTable rebuilds the results table rows with current filter/sort/CVE data.
func (b *RegistryBrowser) rebuildTagTable() {
	displayed := b.filteredSortedMultiTags()
	rows := make([]table.Row, len(displayed))
	for i, t := range displayed {
		imageName := multiImageName(t.RegistryURL, t.Repo, t.Tag)

		registryLabel := t.Alias
		if registryLabel == "" {
			registryLabel = t.RegistryURL
		}

		updated := "-"
		if !t.UpdatedAt.IsZero() {
			updated = theme.TimeAgo(t.UpdatedAt)
		}

		crit, high, med, low := "-", "-", "-", "-"
		if b.scanningTags[imageName] {
			frame := spinner.Dot.Frames[b.tagScanSpinnerIdx%len(spinner.Dot.Frames)]
			crit, high, med, low = frame, frame, frame, frame
		} else if entry, ok := b.scanCache[imageName]; ok {
			crit = fmt.Sprintf("%d", entry.Critical)
			high = fmt.Sprintf("%d", entry.High)
			med = fmt.Sprintf("%d", entry.Medium)
			low = fmt.Sprintf("%d", entry.Low)
		}

		rows[i] = table.Row{registryLabel, t.Tag, updated, crit, high, med, low}
	}

	cols := b.tagTable.Columns()
	if len(cols) == 7 {
		cols[1].Title = "Tag"
		cols[2].Title = "Updated"
		switch b.tagSortCol {
		case tagSortByName:
			arrow := " ▲"
			if b.tagSortDesc {
				arrow = " ▼"
			}
			cols[1].Title = "Tag" + arrow
		case tagSortByUpdated:
			arrow := " ▲"
			if b.tagSortDesc {
				arrow = " ▼"
			}
			cols[2].Title = "Updated" + arrow
		}
		b.tagTable.SetColumns(cols)
	}

	b.tagTable.SetRows(rows)
	b.resizeTagTable()
}

// View renders the browser for the current state.
func (b *RegistryBrowser) View() string {
	switch b.state {
	case browserStateResolving:
		return b.viewResolving()
	case browserStateTags:
		return b.viewTags()
	case browserStateStatus:
		return b.viewStatus()
	default:
		return b.viewInput()
	}
}

func (b *RegistryBrowser) viewResolving() string {
	return theme.EmptyLineBg(b.width) + "\n" +
		theme.SpinnerMessage(b.spinner.View(), "Checking registries...")
}

func (b *RegistryBrowser) viewInput() string {
	var sb strings.Builder

	sb.WriteString(theme.EmptyLineBg(b.width) + "\n")
	sb.WriteString(b.renderInputField("Repository", b.repoInput.View(), brFieldRepo))
	sb.WriteString("\n\n")

	if len(b.entries) > 0 {
		sb.WriteString(theme.Bg("  ") + theme.DimStyle.Render("Registries:") + "\n")
		var currentGroup string
		for i, entry := range b.entries {
			if entry.ParentAlias != "" && entry.ParentAlias != currentGroup {
				if currentGroup != "" {
					sb.WriteString("\n")
				}
				currentGroup = entry.ParentAlias
				sb.WriteString(theme.Bg("  ") + theme.DimStyle.Render(currentGroup+":") + "\n")
			} else if entry.ParentAlias == "" && currentGroup != "" {
				currentGroup = ""
				sb.WriteString("\n")
			}
			label := entry.Alias
			if entry.ParentAlias != "" {
				label = "  " + entry.Alias // visual indent for group members
			}
			checked := b.selectedRegs[entry.URL]
			focused := b.focusedField == b.brFieldReg(i)
			sb.WriteString(theme.RenderCheckbox(checked, label, focused))
			if i < len(b.entries)-1 {
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n\n")
	}

	sb.WriteString("  ")
	sb.WriteString(theme.RenderButton("Browse Tags", b.focusedField == b.brFieldSubmit(), "primary"))

	return sb.String()
}

func (b *RegistryBrowser) renderInputField(label, value string, fieldIdx int) string {
	var labelStr string
	if b.focusedField == fieldIdx {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  " + label + " " + theme.IconChevronRight)
	}
	return labelStr + "\n  " + value
}

func (b *RegistryBrowser) viewTags() string {
	displayed := b.filteredSortedMultiTags()
	if len(displayed) == 0 {
		if b.pendingSearches > 0 {
			return theme.EmptyLineBg(b.width) + "\n" +
				theme.SpinnerMessage(b.spinner.View(), "Searching registries...")
		}
		return theme.DimStyle.Render("  No tags found")
	}
	return b.tagTable.View()
}

// FilterIsVisible returns true when the tag filter bar should be shown in the footer.
func (b *RegistryBrowser) FilterIsVisible() bool {
	return b.state == browserStateTags && (b.filterActive || b.filterInput.Value() != "" || b.registryFilter != "")
}

// FilterBarView renders the filter bar (2 lines) for display in the footer.
func (b *RegistryBrowser) FilterBarView(width int) string {
	borderStyle := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)

	var left string
	if b.filterActive {
		left = theme.KeyStyle.Render("/") + theme.Bg(" ") + b.filterInput.View()
	} else if b.filterInput.Value() != "" {
		left = theme.DimStyle.Render("/ ") + theme.KeyStyle.Render(b.filterInput.Value())
	} else {
		left = theme.DimStyle.Render("/ filter…")
	}

	if b.registryFilter != "" {
		left += theme.Bg("  ") + theme.KeyStyle.Render("["+b.registryFilterLabel()+"]")
	}

	inner := theme.Bg(" ") + left
	paddedInner := theme.PadWithBg(inner, width-2)
	contentLine := borderStyle.Render("│") + paddedInner + borderStyle.Render("│")
	bottomBorder := borderStyle.Render("└" + strings.Repeat("─", width-2) + "┘")
	return contentLine + "\n" + bottomBorder
}

func (b *RegistryBrowser) viewStatus() string {
	var label string
	switch b.operation {
	case "pull":
		label = "Pulling " + b.imageName + "..."
	default:
		label = "Working..."
	}
	return theme.EmptyLineBg(b.width) + "\n" +
		theme.SpinnerMessage(b.spinner.View(), label)
}
