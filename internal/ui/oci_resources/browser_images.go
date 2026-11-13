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
		},
		{
			Title: "Registry", Sizing: datatable.SizingContent, MinWidth: 16,
			Cell: func(r searchRow) string { return r.label },
		},
		{
			Title: "Stars", Sizing: datatable.SizingFixed, Optional: true, DropFirst: true, MinWidth: 6,
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
			label = entryAlias(*e)
			if label == "" {
				label = e.URL
			}
			ref = searchRowReference(e.URL, img)
		}
		rows[i] = searchRow{img: img, label: label, ref: ref}
	}
	sortSearchRows(b.searchQuery, rows)
	// The cursor follows the selected row by (EntryKey, Repo), not by index:
	// rows of another registry land before or after it. Nothing selected (an
	// empty table, so also a new search) goes to the first row, because an
	// empty table keeps its old cursor index; a vanished row stays put.
	prev, hadSel := b.searchTable.Selected()
	b.searchTable.SetItems(rows)
	if !hadSel {
		b.searchTable.GotoTop()
	} else {
		for i, r := range b.searchTable.Visible() {
			if r.img.EntryKey == prev.img.EntryKey && r.img.Repo == prev.img.Repo {
				b.searchTable.SetCursor(i)
				break
			}
		}
	}
	b.resizeTables()
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
	if msg.Err == nil {
		for _, img := range msg.Images {
			img.EntryKey = msg.EntryKey
			b.images = append(b.images, img)
		}
	}
	b.rebuildSearchTable()
	return true
}

// handleImagesKeyMsg handles the keys of the level 1 table. esc acts in
// order: the filter bar while it is open, then a confirmed query, then it
// cancels the lot and goes back to the form (which keeps the typed name and
// the selection). The lot's late answers are no longer wanted and are dropped
// (TestEscAtLevelOneCancelsTheLotAndIgnoresItsLateAnswers). enter does
// nothing: a row has no action yet.
func (b *RegistryBrowser) handleImagesKeyMsg(msg tea.KeyPressMsg) (*RegistryBrowser, tea.Cmd) {
	if b.searchTable.InEditMode() {
		return b, b.searchTable.Update(msg)
	}
	switch msg.String() {
	case "esc":
		if bar := b.searchTable.FilterBar(); bar.SearchQuery() != "" {
			bar.ClearSearch()
			b.searchTable.SetItems(b.searchTable.Items())
			return b, nil
		}
		b.cancelInFlight()
		b.gen = nextBrowserGen()
		b.pendingSearches = 0
		b.state = browserStateInput
		b.searchTable.Blur()
		b.updateFocus()
		return b, nil
	case "enter":
		return b, nil
	}
	return b, b.searchTable.Update(msg)
}

// handleImageSearchResult records one registry's answer; a stale one is ignored.
func (m Model) handleImageSearchResult(msg ImageSearchResultMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil || !m.registryBrowser.AddImageResults(msg) {
		return m, nil
	}
	if msg.Err != nil {
		alias := msg.EntryKey
		if e := m.registryBrowser.entryFor(msg.EntryKey); e != nil {
			alias = entryAlias(*e)
		}
		if errors.Is(msg.Err, imagesearch.ErrUnsupported) {
			log.Printf("INFO [oci_resources] image search on %s: %s", alias, msg.Err)
			return m, nil
		}
		log.Printf("ERROR [oci_resources] image search on %s: %s", alias, searchErrorCause(msg.Err))
	}
	return m, nil
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
