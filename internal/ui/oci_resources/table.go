package ociresources

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// imageRow is one line of the Images tab: the image, plus everything the row
// says about it that does not live on the image — the cached scan counts,
// whether a scan is running or the last one failed, and the alias-substituted
// name.
//
// The columns are built once, in New, so their Cell functions cannot reach back
// into the model for any of that. Carrying it on the row instead is what keeps
// the sort honest: the C column orders by the same number it prints, where the
// old comparator looked the entry up a second time.
type imageRow struct {
	Image        docker.Image
	DisplayName  string
	RawName      string
	Entry        cache.ImageScanEntry
	Scanned      bool
	Scanning     bool
	Failed       bool
	SpinnerFrame string
}

// imageColumnName is the column the Images tab opens sorted by.
const imageColumnName = 1

// secretsColumnWidth is the Secrets column, at the width the workspaces list
// gives it. Elle ne trie pas, comme là-bas : `datatable` réserve deux cellules
// de plus à une colonne triable pour sa flèche, ce qui est cher pour un glyphe.
const secretsColumnWidth = 7

// secrets is the row's verdict — inconnu tant que l'image n'a pas été scannée,
// et inconnu aussi pour un scan qui n'a pas eu d'étape secrets.
func (r imageRow) secrets() theme.SecretsState {
	return theme.SecretsVerdict(r.Entry.Sensitive, r.Scanned)
}

// cveColumn builds one of the four severity count columns.
//
// severity names the column's level for the colour, spelled as the scanners
// spell it — the title here is a single letter, which is no basis for deciding
// what colour a count is.
func cveColumn(title, severity string, get func(cache.ImageScanEntry) int) datatable.Column[imageRow] {
	return datatable.Column[imageRow]{
		Title: title, MinWidth: 4,
		Cell: func(r imageRow) string { return formatCVECount(get(r.Entry), r.Scanned) },
		// A zero is dim, like an unscanned image: four coloured zeroes on a
		// clean image would read as four findings.
		Style: func(r imageRow) lipgloss.Style {
			if !r.Scanned || get(r.Entry) == 0 {
				return theme.DimStyle
			}
			return theme.SeverityTextStyle(severity)
		},
		Less: func(a, b imageRow) bool { return get(a.Entry) < get(b.Entry) },
	}
}

// scannedStyle colours the scan state, a failure being the one value in the
// column that asks for anything.
func scannedStyle(r imageRow) lipgloss.Style {
	switch {
	case r.Failed:
		return theme.StatusErrorStyle
	case r.Scanning, !r.Scanned:
		return theme.DimStyle
	}
	// Aucune opinion : c'est la table qui pose la couleur de texte du thème.
	return lipgloss.NewStyle()
}

// scannedCell reports the scan state: the spinner while one runs, an error icon
// when the last attempt failed, otherwise how long ago it succeeded.
func scannedCell(r imageRow) string {
	switch {
	case r.Scanning:
		return r.SpinnerFrame + "scanning"
	case r.Failed:
		return theme.IconError + " error"
	case r.Scanned:
		return timeAgo(r.Entry.ScannedAt)
	}
	return "-"
}

// imageColumns describes the Images tab. ID is the only column that neither
// sorts nor searches — twelve hex characters are not something anyone orders or
// looks for.
func imageColumns() []datatable.Column[imageRow] {
	return []datatable.Column[imageRow]{
		{
			Title: "ID", MinWidth: 14,
			Cell: func(r imageRow) string { return shortID(r.Image.ID) },
		},
		{
			Title: "Name", MinWidth: 20, Flex: 1,
			Cell: func(r imageRow) string { return r.DisplayName },
			Less: func(a, b imageRow) bool { return strings.ToLower(a.RawName) < strings.ToLower(b.RawName) },
			// Both names. The filter has always matched the repository and tag
			// docker reports; the alias the row actually shows was not
			// searchable, which is a small thing that reads as a bug when the
			// column says one name and the query wants the other.
			Search: func(r imageRow) string { return r.DisplayName + " " + r.RawName },
		},
		{
			Title: "Disk Usage", MinWidth: 12,
			Cell: func(r imageRow) string { return formatBytes(r.Image.UniqueSize) },
			Less: func(a, b imageRow) bool { return a.Image.UniqueSize < b.Image.UniqueSize },
		},
		{
			Title: "Content Size", MinWidth: 14,
			Cell: func(r imageRow) string { return formatBytes(r.Image.Size) },
			Less: func(a, b imageRow) bool { return a.Image.Size < b.Image.Size },
		},
		{
			Title: "Secrets", MinWidth: secretsColumnWidth,
			Cell:  func(r imageRow) string { return theme.SecretsIcon(r.secrets()) },
			Style: func(r imageRow) lipgloss.Style { return theme.SecretsStyle(r.secrets()) },
		},
		cveColumn("C", "CRITICAL", func(e cache.ImageScanEntry) int { return e.Critical }),
		cveColumn("H", "HIGH", func(e cache.ImageScanEntry) int { return e.High }),
		cveColumn("M", "MEDIUM", func(e cache.ImageScanEntry) int { return e.Medium }),
		cveColumn("L", "LOW", func(e cache.ImageScanEntry) int { return e.Low }),
		{
			Title: "Scanned", MinWidth: 14,
			Cell:  scannedCell,
			Style: scannedStyle,
			Less:  func(a, b imageRow) bool { return a.Entry.ScannedAt.Before(b.Entry.ScannedAt) },
		},
	}
}

// imageRows decorates the image list with the scan state the table shows.
func (m *Model) imageRows() []imageRow {
	aliases := make([]docker.RegistryAlias, 0, len(m.registries))
	for _, reg := range m.registries {
		if reg.Alias != "" {
			aliases = append(aliases, docker.RegistryAlias{URL: reg.URL, Alias: reg.Alias})
		}
	}
	frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]

	rows := make([]imageRow, 0, len(m.images))
	for _, img := range m.images {
		raw := img.Name()
		entry, scanned := m.scanCache[raw]
		rows = append(rows, imageRow{
			Image:        img,
			DisplayName:  docker.ApplyAliases(raw, aliases),
			RawName:      raw,
			Entry:        entry,
			Scanned:      scanned,
			Scanning:     m.scanningImages[raw],
			Failed:       m.failedScans[raw],
			SpinnerFrame: frame,
		})
	}
	return rows
}

// formatBytes formats bytes into human-readable string
func formatBytes(b int64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.1f kB", float64(b)/1e3)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatCVECount formats a CVE count for display, returns "-" if not scanned
func formatCVECount(count int, scanned bool) string {
	if !scanned {
		return "-"
	}
	return fmt.Sprintf("%d", count)
}

// updateImageTable rebuilds the image table rows.
//
// Every caller reaches here after changing something the row shows — a scan
// started, finished, or the list came back — so the decoration is recomputed
// wholesale rather than patched in place.
func (m *Model) updateImageTable() {
	m.imageTable.SetItems(m.imageRows())
	m.imageTable.SetHeight(m.tableHeight())
}

// networkColumns describes the Networks tab. Neither this table nor the volumes
// one sorts or filters today, so no Less and no Search: `/` and `.` stay inert
// rather than being wired to a bar this view's footer does not render.
func networkColumns() []datatable.Column[docker.Network] {
	return []datatable.Column[docker.Network]{
		{Title: "ID", MinWidth: 14, Cell: func(n docker.Network) string { return shortID(n.ID) }},
		{Title: "Name", MinWidth: 20, Flex: 1, Cell: func(n docker.Network) string { return n.Name }},
		{Title: "Driver", MinWidth: 12, Cell: func(n docker.Network) string { return n.Driver }},
		{Title: "Scope", MinWidth: 10, Cell: func(n docker.Network) string { return n.Scope }},
	}
}

// volumeColumns describes the Volumes tab.
func volumeColumns() []datatable.Column[docker.Volume] {
	return []datatable.Column[docker.Volume]{
		{Title: "Name", MinWidth: 30, Cell: func(v docker.Volume) string { return v.Name }},
		{Title: "Driver", MinWidth: 12, Cell: func(v docker.Volume) string { return v.Driver }},
		{Title: "Mountpoint", MinWidth: 20, Flex: 1, Cell: func(v docker.Volume) string { return v.Mountpoint }},
	}
}

// shortID truncates a Docker ID to the twelve characters the CLI shows.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// registryRow is one line of the Registries tab.
//
// The tab shows two populations in one table — the configured entries at the
// top level, and one group's discovered members after `→` — and datatable is
// generic over a single T, so the row type has to carry both. explorerRow
// exists for the same reason.
//
// config is the index into Model.registries the row stands for, and -1 for a
// discovered member: a member is not a config entry, has no alias of its own to
// edit and no credentials but its group's, so every action on the tab has to be
// able to tell the two apart. Carrying it on the row is also what lets the
// cursor be resolved without indexing m.registries by row number, which is
// correct today only because this table neither sorts nor filters.
type registryRow struct {
	alias   string
	url     string
	kind    string
	auth    string
	logged  string
	members string
	config  int
}

// registryColumns describes the Registries tab. Nothing sorts or searches: the
// list is the order the config declares, which is the order the user wrote.
func registryColumns() []datatable.Column[registryRow] {
	return []datatable.Column[registryRow]{
		{Title: "Alias", MinWidth: 16, Cell: func(r registryRow) string { return r.alias }},
		{Title: "URL", MinWidth: 20, Flex: 1, Cell: func(r registryRow) string { return r.url }},
		{Title: "Kind", MinWidth: 10, Cell: func(r registryRow) string { return r.kind }},
		{Title: "Auth", MinWidth: 12, Cell: func(r registryRow) string { return r.auth }}, // holds "credentials"
		{Title: "Logged", MinWidth: 8, Cell: func(r registryRow) string { return r.logged }},
		{Title: "Members", MinWidth: 16, Cell: func(r registryRow) string { return r.members }}, // holds "12 · 30 days ago"
	}
}

// membersCell reports what the last discovery for a group found, and when.
//
// The "when" is not decoration: a cache with no visible age is worse than the
// re-detection it replaced, because it looks current whatever it holds
// (§3.8, decision 3). Plain text only — Rule 122.
func (m *Model) membersCell(reg config.RegistryItem) string {
	if reg.Kind != config.KindGroup {
		return ""
	}
	if m.refreshingGroups[reg.Slug] {
		// The spinner's frame, not its View(): the latter renders through a
		// style, and a table cell carries no escape sequence.
		return spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)] + " refreshing"
	}
	entry, ok := m.groupCache[reg.Slug]
	if !ok {
		return "never" // declared a group, never asked
	}
	return fmt.Sprintf("%d · %s", len(entry.Members), timeAgo(entry.DiscoveredAt))
}

// loggedCell reports the docker login status for a registry URL.
func (m *Model) loggedCell(mode, url string) string {
	if !config.UsesCredentials(mode) {
		return "-"
	}
	if m.registryLoginStatus[url] {
		return theme.IconOK
	}
	return theme.IconError
}

// drilledGroup returns the group the tab has entered, or nil at the top level.
func (m *Model) drilledGroup() *config.RegistryItem {
	if m.registryGroupSlug == "" {
		return nil
	}
	for i := range m.registries {
		if m.registries[i].Slug == m.registryGroupSlug {
			return &m.registries[i]
		}
	}
	return nil
}

func (m *Model) updateRegistryTable() {
	if group := m.drilledGroup(); group != nil {
		m.updateGroupMemberTable(*group)
		return
	}
	rows := make([]registryRow, 0, len(m.registries))
	for i, reg := range m.registries {
		rows = append(rows, registryRow{
			alias:   reg.Alias,
			url:     reg.URL,
			kind:    reg.Kind,
			auth:    reg.AuthMode,
			logged:  m.loggedCell(reg.AuthMode, reg.URL),
			members: m.membersCell(reg),
			config:  i,
		})
	}
	m.setRegistryRows(rows)
}

// updateGroupMemberTable fills the table with one group's discovered members.
//
// A member is not a config entry: it has no alias of its own to edit, its
// credentials are the group's, and it is what the cache holds — so the columns
// say `inherit`, carry no member count, and the row points at no config entry.
func (m *Model) updateGroupMemberTable(group config.RegistryItem) {
	entry := m.groupCache[group.Slug]
	rows := make([]registryRow, 0, len(entry.Members))
	for _, member := range entry.Members {
		rows = append(rows, registryRow{
			alias:  member.Alias,
			url:    member.URL,
			kind:   "member",
			auth:   config.AuthInherit,
			logged: m.loggedCell(group.AuthMode, group.URL),
			config: -1,
		})
	}
	m.setRegistryRows(rows)
}

func (m *Model) setRegistryRows(rows []registryRow) {
	m.registryTable.SetItems(rows)
	m.registryTable.Resize(m.width, m.tableHeight())
}
