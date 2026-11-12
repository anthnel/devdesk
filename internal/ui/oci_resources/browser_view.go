package ociresources

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// rebuildTagTable re-decorates the rows and hands them to the table.
//
// The decoration — the scan counts, whether a scan is running, the registry's
// label — is not on the tag, and the columns are built once so they cannot
// reach the browser for it. So it is recomputed wholesale on every change,
// exactly as the images tab does with imageRow.
//
// The filters are applied here because they narrow the tags before the table
// sees them; the sort and the header arrows are the table's.
func (b *RegistryBrowser) rebuildTagTable() {
	tags := b.filteredMultiTags()

	rows := make([]tagRow, len(tags))
	for i, t := range tags {
		imageName := multiImageName(t.RegistryURL, t.Repo, t.Tag)
		label := t.Alias
		if label == "" {
			label = t.RegistryURL
		}
		entry, scanned := b.scanCache[imageName]
		rows[i] = tagRow{
			tag:         t,
			label:       label,
			entry:       entry,
			scanned:     scanned,
			Activity:    b.tagActivity(imageName),
			ScanRunning: b.scanRunning(imageName),
		}
	}

	b.tagTable.SetItems(rows)
	b.resizeTables()
}

// tagActivity resolves what one tag row says about its activity: a scan or a
// pull, whoever started it, or a remembered failure of either. Both are read
// against the context the snapshot carries, with the raw reference as identity.
func (b *RegistryBrowser) tagActivity(imageName string) components.RowActivity {
	return components.DescribeRow(components.RowQuery{
		Runs: b.jobs, Failures: b.failures, Context: b.contextName,
		Target: jobs.TargetKey(imageName), Kinds: ociKinds,
	})
}

// scanRunning reports whether a scan of the tag runs now. A queued scan does
// not: it keeps its counts.
func (b *RegistryBrowser) scanRunning(imageName string) bool {
	for _, a := range jobs.ActiveOn(b.jobs, b.contextName, imageName, jobs.KindScan) {
		if a.Item.State == jobs.ItemRunning {
			return true
		}
	}
	return false
}

// View renders the browser for the current state.
func (b *RegistryBrowser) View() string {
	switch b.state {
	case browserStateTags:
		return b.viewTags()
	case browserStateImages:
		return b.searchTable.View()
	default:
		return b.viewInput()
	}
}

func (b *RegistryBrowser) viewInput() string {
	var sb strings.Builder

	sb.WriteString(theme.EmptyLineBg(b.width) + "\n")
	sb.WriteString(b.renderInputField("Repository", b.repoInput.View(), brFieldRepo))
	sb.WriteString("\n\n")

	if len(b.rows) > 0 {
		sb.WriteString(theme.Bg("  ") + theme.DimStyle.Render("Registries:") + "\n")
		for i, row := range b.rows {
			focused := b.focusedField == b.brFieldReg(i)
			if row.groupSlug != "" {
				// One checkbox for the whole group, with a third state for a
				// partial selection — eight proxies are not eight keystrokes.
				sb.WriteString(theme.RenderCheckboxTri(b.groupState(row.groupSlug), row.label, focused))
			} else {
				sb.WriteString(theme.RenderCheckbox(b.selected(b.entries[row.entry]), row.label, focused))
			}
			if i < len(b.rows)-1 {
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n\n")
	}

	sb.WriteString(b.renderFormButtons())

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

// viewTags renders the tag table. The search says so in the footer, with the
// shared activity dot (LoadingLabel), and an empty table stays a table (Rule 139) — the
// count lives in GetHeaderInfo's "Tags" field.
func (b *RegistryBrowser) viewTags() string {
	return b.tagTable.View()
}

// LoadingLabel names what the browser is fetching, for the OCI view's footer.
// The second result is false when nothing is in flight.
func (b *RegistryBrowser) LoadingLabel() (string, bool) {
	if (b.state == browserStateTags || b.state == browserStateImages) && b.pendingSearches > 0 {
		return "Searching registries...", true
	}
	return "", false
}

// FilterIsVisible returns true when the tag filter bar should be shown in the footer.
func (b *RegistryBrowser) FilterIsVisible() bool {
	if b.state == browserStateImages {
		return b.searchTable.FilterBar().IsVisible()
	}
	return b.state == browserStateTags && (b.filterActive || b.filterInput.Value() != "" || !b.registryFilter.isEmpty())
}

// FilterBarView renders the filter bar (2 lines) for display in the footer.
func (b *RegistryBrowser) FilterBarView(width int) string {
	if b.state == browserStateImages {
		return b.searchTable.FilterBar().View()
	}
	borderStyle := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)

	var left string
	if b.filterActive {
		left = theme.KeyStyle.Render("/") + theme.Bg(" ") + b.filterInput.View()
	} else if b.filterInput.Value() != "" {
		left = theme.DimStyle.Render("/ ") + theme.KeyStyle.Render(b.filterInput.Value())
	} else {
		left = theme.DimStyle.Render("/ filter…")
	}

	if !b.registryFilter.isEmpty() {
		left += theme.Bg("  ") + theme.KeyStyle.Render("["+b.registryFilterLabel()+"]")
	}

	inner := theme.Bg(" ") + left
	paddedInner := theme.PadWithBg(inner, width-2)
	contentLine := borderStyle.Render("│") + paddedInner + borderStyle.Render("│")
	bottomBorder := borderStyle.Render("└" + strings.Repeat("─", width-2) + "┘")
	return contentLine + "\n" + bottomBorder
}

// viewStatus is gone with browserStateStatus (§3.60). It was a
// loading message filling the panel — the shape Rule 139 keeps out of a
// table body — and what it announced is now a row in the Images tab.
