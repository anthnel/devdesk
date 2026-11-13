package ociresources

import (
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// searchRow is one line of the level 1 table: an image, and the label of the
// registry that returned it.
type searchRow struct {
	img   imagesearch.Image
	label string
	// ref is the full reference (host/repo), what the footer describes.
	ref string
}

// searchRowReference returns the full reference of an image found in the
// registry at registryURL: the host, then the final repository path. Docker
// Hub is always written docker.io, whichever spelling it is configured under.
func searchRowReference(registryURL string, img imagesearch.Image) string {
	host := oci.RegistryHost(registryURL)
	if host == "" {
		return img.Repo
	}
	if oci.IsDockerHub(registryURL) {
		host = "docker.io"
	}
	return host + "/" + img.Repo
}

// Column indexes of the level 1 table that can be sorted by (DEVDESK-309).
const (
	searchColName     = 1
	searchColRegistry = 2
	searchColStars    = 3
)

// searchColumns describes the level 1 table. Stars, Official and Description
// leave whole when the terminal narrows, in that reverse order (Description,
// Stars, Official), through Optional and DropFirst: drop() takes the rightmost
// DropFirst column first, then the rightmost Optional one.
func searchColumns() []datatable.Column[searchRow] {
	return []datatable.Column[searchRow]{
		datatable.IndicatorColumn(
			func(searchRow) indicator.State { return indicator.Idle },
			func(searchRow) string { return theme.IconDocker },
			func(searchRow) lipgloss.Style { return theme.IconStyle(theme.IconRoleImage) },
		),
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: 24, Flex: 1,
			Cell:   func(r searchRow) string { return r.img.Name },
			Search: func(r searchRow) string { return r.img.Name },
			Less:   lessSearchName,
		},
		{
			Title: "Registry", Sizing: datatable.SizingContent, MinWidth: 16,
			Cell: func(r searchRow) string { return r.label },
			Less: lessSearchRegistry,
		},
		{
			Title: "Stars", Sizing: datatable.SizingFixed, Optional: true, DropFirst: true, MinWidth: 7,
			Less: lessSearchStars, DescFirst: true,
			Cell: func(r searchRow) string {
				if !r.img.HasStars {
					return ""
				}
				return strconv.Itoa(r.img.Stars)
			},
		},
		{
			Title: "Official", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 10,
			Cell: func(r searchRow) string {
				if r.img.Official {
					return "official"
				}
				return ""
			},
		},
		{
			Title: "Description", Sizing: datatable.SizingContent, Optional: true, DropFirst: true,
			MinWidth: 20, Flex: 2,
			Cell:   func(r searchRow) string { return r.img.Description },
			Search: func(r searchRow) string { return r.img.Description },
		},
	}
}

// newSearchTable builds the level 1 table; rebuildSearchTable orders its rows.
func newSearchTable() *datatable.Model[searchRow] {
	t := datatable.New(datatable.Config[searchRow]{
		Columns:    searchColumns(),
		SortColumn: -1,
	})
	return &t
}

// rebuildSearchTable hands the collected images to the table.
func (b *RegistryBrowser) rebuildSearchTable() {
	rows := make([]searchRow, len(b.images))
	for i, img := range b.images {
		label, ref := "", img.Repo
		if e := b.entryFor(img.EntryKey); e != nil {
			label = b.entryLabel(img.EntryKey)
			ref = searchRowReference(e.URL, img)
		}
		rows[i] = searchRow{img: img, label: label, ref: ref}
	}
	sortSearchRows(b.searchQuery, rows)
	// Nothing selected (an empty table, so also a new search) goes to the
	// first row, because an empty table keeps its old cursor index.
	prev, hadSel := b.searchTable.Selected()
	b.searchTable.SetItems(rows)
	b.reselect(prev, hadSel)
	b.resizeTables()
}

// reselect puts the cursor back on the row prev, by (EntryKey, Repo) and not by
// index: rows of another registry land before or after it, and a sort moves it.
// Without a previous selection the cursor goes to the first row; a vanished row
// leaves the cursor where it is.
func (b *RegistryBrowser) reselect(prev searchRow, had bool) {
	if !had {
		b.searchTable.GotoTop()
		return
	}
	for i, r := range b.searchTable.Visible() {
		if r.img.EntryKey == prev.img.EntryKey && r.img.Repo == prev.img.Repo {
			b.searchTable.SetCursor(i)
			return
		}
	}
}

// cycleSearchSort advances the level 1 sort (`.`); the cursor follows the row
// it was on (DEVDESK-309).
func (b *RegistryBrowser) cycleSearchSort() {
	prev, had := b.searchTable.Selected()
	b.searchTable.CycleSort()
	b.reselect(prev, had)
}

// AddImageResults takes one registry's answer. It returns false, touching
// nothing, when the answer belongs to another lot. pendingSearches never goes
// below zero.
func (b *RegistryBrowser) AddImageResults(msg ImageSearchResultMsg) bool {
	if msg.Gen != b.gen {
		return false
	}
	if b.pendingSearches > 0 {
		b.pendingSearches--
	}
	oc := imagesearch.Classify(msg.Err, len(msg.Images), msg.Partial)
	if b.outcomes == nil {
		b.outcomes = map[string]imagesearch.Outcome{}
	}
	b.outcomes[msg.EntryKey] = oc
	if oc.Verdict == imagesearch.VerdictOK {
		for _, img := range msg.Images {
			img.EntryKey = msg.EntryKey
			b.images = append(b.images, img)
		}
	}
	b.rebuildSearchTable()
	return true
}

// handleImagesKeyMsg handles the keys of the level 1 table. left goes straight
// back to the form; esc acts in order: the filter bar while it is open, then a
// confirmed query, then it cancels the lot and goes back to the form (which
// keeps the typed name and the selection). The lot's late answers are no
// longer wanted and are dropped
// (TestEscAtLevelOneCancelsTheLotAndIgnoresItsLateAnswers). enter does
// nothing: a row has no action yet; → opens the row's tags.
func (b *RegistryBrowser) handleImagesKeyMsg(msg tea.KeyPressMsg) (*RegistryBrowser, tea.Cmd) {
	if b.searchTable.InEditMode() {
		return b, b.searchTable.Update(msg)
	}
	switch msg.String() {
	case ".":
		b.cycleSearchSort()
		return b, nil
	case "right":
		return b.openSelectedImageTags()
	case "esc":
		if bar := b.searchTable.FilterBar(); bar.SearchQuery() != "" {
			// The cursor stays on the selected row once the filter is gone.
			prev, had := b.searchTable.Selected()
			bar.ClearSearch()
			b.searchTable.SetItems(b.searchTable.Items())
			b.reselect(prev, had)
			return b, nil
		}
		return b.backToForm()
	case "left":
		return b.backToForm()
	case "enter":
		return b, nil
	}
	return b, b.searchTable.Update(msg)
}

// backToForm cancels the lot and returns to the form, which keeps the typed
// name and the selection. Late answers of the lot are dropped.
func (b *RegistryBrowser) backToForm() (*RegistryBrowser, tea.Cmd) {
	b.abandonLot()
	b.state = browserStateInput
	b.searchTable.Blur()
	b.updateFocus()
	return b, nil
}

// handleImageSearchResult records one registry's answer; a stale one is ignored.
func (m Model) handleImageSearchResult(msg ImageSearchResultMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil || !m.registryBrowser.AddImageResults(msg) {
		return m, nil
	}
	oc, _ := m.registryBrowser.Outcome(msg.EntryKey)
	alias := msg.EntryKey
	if e := m.registryBrowser.entryFor(msg.EntryKey); e != nil {
		alias = entryAlias(*e)
	}
	logSearchOutcome(alias, oc, msg.Err)
	return m, nil
}

// logSearchOutcome logs a registry's verdict: failed as an error, refused as
// info, nothing for ok or no result. The cause goes through searchErrorCause,
// so no secret or typed text reaches the log.
func logSearchOutcome(alias string, oc imagesearch.Outcome, err error) {
	switch oc.Verdict {
	case imagesearch.VerdictFailed:
		log.Printf("ERROR [oci_resources] image search on %s: %s", alias, searchErrorCause(err))
	case imagesearch.VerdictRefused:
		log.Printf("INFO [oci_resources] image search on %s: unsupported or refused: %s", alias, searchErrorCause(err))
	}
}

// searchErrorCause is the loggable text of a search error. A *url.Error holds
// the full request URL, which carries the text the user typed, so only its Op
// and Err are kept.
func searchErrorCause(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return jobs.SafeCause(errors.New(ue.Op + ": " + jobs.SafeCause(ue.Err)))
	}
	return jobs.SafeCause(err)
}

// handleRegistryBrowserWarn shows the browser's warning in the footer.
func (m Model) handleRegistryBrowserWarn(msg RegistryBrowserWarnMsg) (tea.Model, tea.Cmd) {
	return m, m.footer.Warn(strings.TrimSpace(msg.Text))
}

// tagsLoadFailedText is the footer text of a failed tag listing opened by →.
const tagsLoadFailedText = "Could not load tags"

// selectedImage returns the level 1 row under the cursor with its registry
// entry; false when there is no row or the entry is no longer known.
func (b *RegistryBrowser) selectedImage() (searchRow, *browserRegistryEntry, bool) {
	row, ok := b.searchTable.Selected()
	if !ok {
		return searchRow{}, nil, false
	}
	entry := b.entryFor(row.img.EntryKey)
	return row, entry, entry != nil
}

// imageDrillIn reports whether → has an image to open: a row under the cursor
// whose registry entry is still known. One computation for the header and the
// handler (Rule 130).
func (b *RegistryBrowser) imageDrillIn() shortcut.Availability {
	if _, _, ok := b.selectedImage(); !ok {
		return shortcut.Unavailable(reasonNoImage)
	}
	return shortcut.Availability{}
}

// openSelectedImageTags lists the tags of the selected level 1 row, on that
// row's registry only, with the row's final repository as is (no prefix, no
// library/ added again). Without a row it changes nothing and warns. A level 1
// search still running is cancelled: the browser has one lot at a time.
func (b *RegistryBrowser) openSelectedImageTags() (*RegistryBrowser, tea.Cmd) {
	if av := b.imageDrillIn(); !av.Enabled() {
		reason := av.Reason
		return b, func() tea.Msg { return RegistryBrowserWarnMsg{Text: reason} }
	}
	row, entry, _ := b.selectedImage()
	b, cmd := b.startTagRequests([]tagRequest{b.newTagRequest(*entry, row.img.Repo)}, true)
	b.tagsImage = row.img.Name
	return b, cmd
}

// Outcome returns the verdict of entryKey in the current lot; false while
// pending.
func (b *RegistryBrowser) Outcome(entryKey string) (imagesearch.Outcome, bool) {
	oc, ok := b.outcomes[entryKey]
	return oc, ok
}
