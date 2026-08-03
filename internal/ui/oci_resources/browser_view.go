package ociresources

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

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
