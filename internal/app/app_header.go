package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/shortcut"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// HeaderView est l'interface que chaque vue doit implémenter pour fournir les données du header
type HeaderView interface {
	GetShortcuts() shortcut.Shortcuts
	GetTitle() string
	GetIcon() string
	GetHeaderInfo(context string) []shortcut.HeaderInfo
}

// FooterView is implemented by views that have footer content rendered below the viewport border (Rule 124).
// The footer can contain a tab bar (1 line) and optionally an empty line + info line (2 lines).
type FooterView interface {
	GetFooterHeight() int          // current footer height in lines (0 if no footer)
	RenderFooter(width int) string // rendered footer content
}

// headerMinHeight est la hauteur fixe du header (sans la ligne de commande).
const headerMinHeight = 7

// ASCII logo for the header
var logoLines = []string{
	`    ____            ____            __  `,
	`   / __ \___ _   __/ __ \___  _____/ /__`,
	`  / / / / _ \ | / / / / / _ \/ ___/ //_/`,
	` / /_/ /  __/ |/ / /_/ /  __(__  ) ,<   `,
	`/_____/\___/|___/_____/\___/____/_/|_|  `,
}

const logoWidth = 40 // max width of a logo line

// renderHeader generates the header (fixed height) followed by the command line (1 line).
func (a *App) renderHeader() string {
	if view, ok := a.views[a.currentView]; ok {
		if headerView, implementsHeaderView := view.(HeaderView); implementsHeaderView {
			shortcuts := headerView.GetShortcuts()
			info := headerView.GetHeaderInfo(a.currentContext)

			header := renderHeaderContent(info, shortcuts, a.width)

			pad := theme.Bg(" ")
			var cmdLine string
			if a.commandMode {
				a.commandInput.Focus()
				cmdLine = pad + theme.CommandLineStyle.Width(a.width-2).Render(a.renderCommandLineWithCompletion()) + pad
			} else {
				cmdLine = pad + theme.CommandLineInactiveStyle.Width(a.width-2).Render(":") + pad
			}

			return header + "\n" + theme.EmptyLineBg(a.width) + "\n" + cmdLine
		}
	}

	return ""
}

// renderHeaderContent generates the header block with a fixed height (headerMinHeight).
// Layout: 3 columns — info (25%), shortcuts (flexible), logo (fixed width).
func renderHeaderContent(info []shortcut.HeaderInfo, shortcuts shortcut.Shortcuts, width int) string {
	innerWidth := width - 2 // 1 padding each side

	// Column widths
	col1Width := innerWidth / 4
	col3Width := logoWidth
	col2Width := innerWidth - col1Width - col3Width
	if col2Width < 0 {
		col2Width = 0
	}

	// Build column 1: key-value info lines
	col1Lines := buildInfoLines(info, col1Width)

	// Build column 2: shortcuts
	col2Lines := buildShortcutLines(shortcuts, col2Width)

	// Build column 3: logo (centered vertically — 1 empty line on top since 6 logo lines / 7 header lines)
	col3Lines := buildLogoLines(col3Width)

	// Assemble rows
	rows := make([]string, headerMinHeight)
	for i := 0; i < headerMinHeight; i++ {
		c1 := ""
		if i < len(col1Lines) {
			c1 = col1Lines[i]
		}
		c2 := ""
		if i < len(col2Lines) {
			c2 = col2Lines[i]
		}
		c3 := ""
		if i < len(col3Lines) {
			c3 = col3Lines[i]
		}

		pad := theme.Bg(" ")
		row := pad + theme.PadWithBg(c1, col1Width) + theme.PadWithBg(c2, col2Width) + theme.PadWithBg(c3, col3Width) + pad
		rows[i] = row
	}

	return strings.Join(rows, "\n")
}

// buildInfoLines builds the key-value info lines for column 1, aligning values on the longest key
func buildInfoLines(info []shortcut.HeaderInfo, _ int) []string {
	// Find max key length for alignment
	maxKeyLen := 0
	for _, h := range info {
		if len(h.Key) > maxKeyLen {
			maxKeyLen = len(h.Key)
		}
	}

	lines := make([]string, headerMinHeight)
	for i := 0; i < headerMinHeight; i++ {
		if i < len(info) {
			// Pad key to maxKeyLen so all ": " align
			paddedKey := info[i].Key + strings.Repeat(" ", maxKeyLen-len(info[i].Key))
			label := theme.HeaderKeyStyle.Render(paddedKey + ": ")

			value := info[i].Value
			if info[i].Style.GetForeground() != lipgloss.Color("") {
				value = info[i].Style.Background(theme.ColorBackground).Render(value)
			} else {
				value = lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(value)
			}

			lines[i] = label + value
		} else {
			lines[i] = ""
		}
	}
	return lines
}

// buildShortcutLines builds the shortcut lines for column 2
func buildShortcutLines(shortcuts shortcut.Shortcuts, col2Width int) []string {
	if len(shortcuts) == 0 {
		lines := make([]string, headerMinHeight)
		for i := range lines {
			lines[i] = ""
		}
		return lines
	}

	// Split shortcuts into columns of maxLine items
	columns := splitArrayIntoChunks(shortcuts, headerMinHeight)

	// Get rendered lines for each column
	var allColumnLines [][]string
	for i, col := range columns {
		minH := 0
		if i == 0 {
			minH = headerMinHeight
		}
		allColumnLines = append(allColumnLines, renderShortcutsColumnLines(col, minH))
	}

	// Normalize heights
	for i := range allColumnLines {
		for len(allColumnLines[i]) < headerMinHeight {
			allColumnLines[i] = append(allColumnLines[i], "")
		}
	}

	// Calculate column widths
	colWidths := make([]int, len(allColumnLines))
	for ci, colLines := range allColumnLines {
		for _, line := range colLines {
			if w := lipgloss.Width(line); w > colWidths[ci] {
				colWidths[ci] = w
			}
		}
	}

	sep := "  "
	sepRendered := theme.Bg(sep)

	// Build each row
	result := make([]string, headerMinHeight)
	for row := 0; row < headerMinHeight; row++ {
		var line string
		for ci, colLines := range allColumnLines {
			if ci > 0 {
				line += sepRendered
			}
			if row < len(colLines) {
				line += theme.PadWithBg(colLines[row], colWidths[ci])
			} else {
				line += theme.EmptyLineBg(colWidths[ci])
			}
		}
		// Truncate if wider than col2Width
		if lipgloss.Width(line) > col2Width && col2Width > 0 {
			line = theme.PadWithBg(line, col2Width)
		}
		result[row] = line
	}

	return result
}

// buildLogoLines builds the logo lines for column 3, vertically centered
func buildLogoLines(col3Width int) []string {
	logoStyle := lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorPrimary).
		Bold(true)

	lines := make([]string, headerMinHeight)
	// 5 logo lines in 7 header lines → 1 empty line top and bottom
	offset := 1
	for i := 0; i < headerMinHeight; i++ {
		logoIdx := i - offset
		if logoIdx >= 0 && logoIdx < len(logoLines) {
			lines[i] = theme.PadWithBg(logoStyle.Render(logoLines[logoIdx]), col3Width)
		} else {
			lines[i] = theme.EmptyLineBg(col3Width)
		}
	}
	return lines
}

// renderShortcutsColumnLines renders a column of shortcuts as individual lines padded to the same width with background.
func renderShortcutsColumnLines(shortcuts shortcut.Shortcuts, minHeight int) []string {
	if len(shortcuts) == 0 {
		return nil
	}

	lines := shortcuts.ToStrings()

	// Find max width for uniform padding
	maxWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxWidth {
			maxWidth = w
		}
	}

	// Pad each line to the same width with background
	var result []string
	for _, line := range lines {
		result = append(result, theme.PadWithBg(line, maxWidth))
	}

	// Add empty lines if minHeight required
	for len(result) < minHeight {
		result = append(result, theme.EmptyLineBg(maxWidth))
	}

	return result
}

func splitArrayIntoChunks(arr []shortcut.Shortcut, chunkSize int) []shortcut.Shortcuts {
	var chunks []shortcut.Shortcuts
	for i := 0; i < len(arr); i += chunkSize {
		end := i + chunkSize
		if end > len(arr) {
			end = len(arr)
		}
		chunks = append(chunks, arr[i:end])
	}
	return chunks
}

// renderCommandLineWithCompletion generates input + preview + hint
func (a *App) renderCommandLineWithCompletion() string {
	inputView := a.commandInput.View()

	if len(a.completionSuggestions) == 0 {
		return inputView
	}

	current := a.completionSuggestions[a.completionIndex]
	currentInput := a.commandInput.Value()

	previewText := ""
	if strings.HasPrefix(strings.ToLower(current.Text), strings.ToLower(currentInput)) &&
		len(currentInput) > 0 {
		previewText = current.Text[len(currentInput):]
	}

	cmdBgStyle := lipgloss.NewStyle().Background(theme.ColorCmdLineBg)

	preview := cmdBgStyle.Foreground(theme.ColorDim).Render(previewText)

	hint := cmdBgStyle.Foreground(theme.ColorDim).Italic(true).
		Render(fmt.Sprintf(" %s [%d/%d]",
			current.Display,
			a.completionIndex+1,
			len(a.completionSuggestions)))

	contentWidth := lipgloss.Width(inputView) + lipgloss.Width(preview)
	hintWidth := lipgloss.Width(hint)
	// width - 2 (outer padding) - 2 (CommandLineStyle inner Padding(0,1,0,1))
	spacerWidth := (a.width - 4) - contentWidth - hintWidth
	spacer := ""
	if spacerWidth > 0 {
		spacer = cmdBgStyle.Render(strings.Repeat(" ", spacerWidth))
	}

	return inputView + preview + spacer + hint
}
