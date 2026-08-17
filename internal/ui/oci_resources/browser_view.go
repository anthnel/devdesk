package ociresources

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

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
	frame := spinner.Dot.Frames[b.tagScanSpinnerIdx%len(spinner.Dot.Frames)]

	rows := make([]tagRow, len(tags))
	for i, t := range tags {
		imageName := multiImageName(t.RegistryURL, t.Repo, t.Tag)
		label := t.Alias
		if label == "" {
			label = t.RegistryURL
		}
		entry, scanned := b.scanCache[imageName]
		rows[i] = tagRow{
			tag:      t,
			label:    label,
			entry:    entry,
			scanned:  scanned,
			scanning: b.scanningTags[imageName],
			frame:    frame,
		}
	}

	b.tagTable.SetItems(rows)
	b.resizeTagTable()
}

// View renders the browser for the current state.
func (b *RegistryBrowser) View() string {
	switch b.state {
	case browserStateTags:
		return b.viewTags()
	case browserStateStatus:
		return b.viewStatus()
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

// viewTags renders the tag table. The search says so in the footer, with a
// spinner (LoadingLabel), so the table keeps its place here and "No tags found"
// waits until there is nothing left in flight to find them.
func (b *RegistryBrowser) viewTags() string {
	if len(b.tagTable.Visible()) == 0 && b.pendingSearches == 0 {
		return theme.DimStyle.Render("  No tags found")
	}
	return b.tagTable.View()
}

// LoadingLabel names what the browser is fetching, for the OCI view's footer.
// The second result is false when nothing is in flight.
func (b *RegistryBrowser) LoadingLabel() (string, bool) {
	if b.state == browserStateTags && b.pendingSearches > 0 {
		return "Searching registries...", true
	}
	return "", false
}

// FilterIsVisible returns true when the tag filter bar should be shown in the footer.
func (b *RegistryBrowser) FilterIsVisible() bool {
	return b.state == browserStateTags && (b.filterActive || b.filterInput.Value() != "" || !b.registryFilter.isEmpty())
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

	if !b.registryFilter.isEmpty() {
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
