package app

import (
	"strings"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// HeaderView is the interface each view must implement to provide the header data
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

// FramelessView is implemented by a view that draws its own frames and wants
// none around it. The default — framed — is the safe behavior: a view that
// does not implement this is framed like all the others, unlike HeaderView
// whose missing half silently renders an empty title.
//
// Only one view implements it (the dashboard), and that is deliberate: every
// other view frames a single object — a table, a form.
type FramelessView interface {
	Frameless() bool
}

// headerMinHeight is the fixed header height (without the command line).
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
			if a.commandMode {
				shortcuts = a.commandLineShortcuts()
			}
			info := headerView.GetHeaderInfo(a.currentContext)

			header := renderHeaderContent(info, shortcuts, a.width)

			pad := theme.Bg(" ")
			var cmdLine string
			if a.commandMode {
				cmdLine = pad + theme.CommandLineStyle.Width(a.width-2).Render(a.commandInput.View()) + pad
			} else {
				cmdLine = pad + theme.CommandLineInactiveStyle.Width(a.width-2).Render("❯") + pad
			}

			// A blank line under the command line keeps it from touching the title rule.
			return header + "\n" + theme.EmptyLineBg(a.width) + "\n" + cmdLine + "\n" + theme.EmptyLineBg(a.width)
		}
	}

	return ""
}

// renderHeaderContent generates the header block with a fixed height (headerMinHeight).
// Layout: 3 columns — info (25%), shortcuts (flexible), logo (fixed width).
func renderHeaderContent(info []shortcut.HeaderInfo, shortcuts shortcut.Shortcuts, width int) string {
	// Two padding cells are the minimum a row can hold; below that the header
	// is blank rather than wider than the window.
	if width < 2 {
		return strings.Join(blankHeaderRows(width), "\n")
	}
	innerWidth := width - 2 // 1 padding each side

	// Column widths
	col1Width := innerWidth / 4
	col3Width := logoWidth
	// The logo column is fixed-width: drop it when it cannot sit next to the
	// info column, rather than pushing every row past the window (Rule 108).
	if innerWidth < col1Width+logoWidth {
		col3Width = 0
	}
	col2Width := innerWidth - col1Width - col3Width
	if col2Width < 0 {
		col2Width = 0
	}

	// Build column 1: key-value info lines
	col1Lines := buildInfoLines(info, col1Width)

	// Build column 2: shortcuts
	col2Lines := buildShortcutLines(shortcuts, col2Width)

	// Build column 3: logo (centered vertically — 1 empty line on top since 6 logo lines / 7 header lines)
	var col3Lines []string
	if col3Width > 0 {
		col3Lines = buildLogoLines(col3Width)
	}

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

// blankHeaderRows returns headerMinHeight background-filled rows of the given width.
func blankHeaderRows(width int) []string {
	rows := make([]string, headerMinHeight)
	for i := range rows {
		rows[i] = theme.EmptyLineBg(width)
	}
	return rows
}

// buildInfoLines builds the key-value info lines for column 1, aligning values on the longest key.
// Lines wider than col1Width are clipped, so a narrow column never overflows into the next one.
func buildInfoLines(info []shortcut.HeaderInfo, col1Width int) []string {
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
			if col1Width > 0 {
				// MaxWidth truncates on rune boundaries and keeps escape sequences intact.
				lines[i] = lipgloss.NewStyle().MaxWidth(col1Width).Render(lines[i])
			} else {
				// No room at all: show nothing rather than overflow.
				lines[i] = ""
			}
		} else {
			lines[i] = ""
		}
	}
	return lines
}

// buildShortcutLines builds the shortcut lines for column 2
func buildShortcutLines(shortcuts shortcut.Shortcuts, col2Width int) []string {
	// A column with no width shows no shortcut: PadWithBg returns a long line
	// untouched, so anything rendered here would spill past the window.
	if len(shortcuts) == 0 || col2Width <= 0 {
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
		// Clip to the column. PadWithBg cannot do this — it pads a short line and
		// returns a long one untouched — and a plain slice would cut an escape
		// sequence in half, which is the hazard Rule 122 is about. MaxWidth
		// truncates on rune boundaries and leaves the sequences intact.
		if col2Width > 0 {
			line = lipgloss.NewStyle().MaxWidth(col2Width).Render(line)
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

// commandLineShortcuts is the header's vocabulary while the command line owns
// the keyboard. It replaces the view's own: tab and enter stay in place and
// grey out when no row is highlighted (Rule 130).
func (a *App) commandLineShortcuts() shortcut.Shortcuts {
	_, runnable := a.runnableText()
	return shortcut.Shortcuts{
		{Key: "↑↓", Description: "Choose"},
		{Key: "tab", Description: "Complete", Disabled: !a.canComplete()},
		{Key: "enter", Description: "Open", Disabled: !runnable},
		{Key: "esc", Description: "Close"},
	}
}
