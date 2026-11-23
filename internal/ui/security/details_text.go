package security

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Caps on the Message section of a finding detail (DEVDESK-750): a scanner can
// hand back a whole stderr, and the detail shows the start of it.
const (
	maxMessageWidth = 110
	maxMessageLines = 8
)

// scanMount is where a scanned directory is mounted inside a scanner container
// (scan.containerScanPath, unexported there).
const scanMount = "/scan"

// pathToken finds an absolute path in a text: it starts at the beginning of the
// text or after a character that cannot be part of a path name, and stops at
// white space or punctuation that ends a path in a message.
var pathToken = regexp.MustCompile(`(?:^|[^\w./~-])/[^\s:'"<>()\[\],;|]*`)

// shortPaths rewrites absolute paths inside text for display: "<target>/" and
// the container mount "/scan/" are removed, a bare target becomes ".", and any
// other path under HOME is folded to "~/". target is used only when it is an
// absolute path (an image reference is ignored). Display only: the finding
// keeps its text.
func shortPaths(text, target string) string {
	if text == "" {
		return ""
	}
	root := ""
	if filepath.IsAbs(target) {
		if root = filepath.ToSlash(filepath.Clean(target)); root == "/" {
			root = ""
		}
	}
	return pathToken.ReplaceAllStringFunc(text, func(match string) string {
		at := strings.IndexByte(match, '/')
		path, tail := match[at:], ""
		// A sentence can end on a path: "cannot read /repo."
		if trimmed := strings.TrimRight(path, "."); trimmed != "" {
			path, tail = trimmed, path[len(trimmed):]
		}
		return match[:at] + shortPath(path, root) + tail
	})
}

// shortPath shortens one absolute path: relative to the target, relative to the
// container mount, or folded under HOME.
func shortPath(path, root string) string {
	for _, base := range []string{root, scanMount} {
		switch {
		case base == "":
		case path == base:
			return "."
		case strings.HasPrefix(path, base+"/"):
			if rest := strings.TrimLeft(path[len(base):], "/"); rest != "" {
				return rest
			}
			return "."
		}
	}
	return theme.FoldHome(path)
}

// oneLine is the first line of text, cut to width cells with theme.Ellipsis.
// It measures with x/ansi, as the viewport does, not with runewidth, after the
// tabs and carriage returns are gone: x/ansi counts a tab as no cell while the
// renderer lays it out as four spaces.
func oneLine(text string, width int) string {
	if width <= 0 {
		return ""
	}
	first, _, _ := strings.Cut(text, "\n")
	first = withoutControlSpace(first)
	if ansi.StringWidth(first) <= width {
		return first
	}
	return ansi.Truncate(first, width, theme.Ellipsis)
}

// cappedLines wraps text at min(width, maxMessageWidth), hard-breaking a token
// longer than that, and keeps the first maxLines lines. hidden counts the wrapped
// lines left out. A blank text gives nil, 0.
func cappedLines(text string, width, maxLines int) (lines []string, hidden int) {
	if strings.TrimSpace(text) == "" {
		return nil, 0
	}
	text = strings.TrimRight(text, " \t\r\n")
	lines = strings.Split(ansi.Wrap(text, max(min(width, maxMessageWidth), 1), ""), "\n")
	if len(lines) > maxLines {
		return lines[:maxLines], len(lines) - maxLines
	}
	return lines, 0
}

// moreLines is the marker after a capped text, "" when nothing was left out.
func moreLines(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "… 1 more line"
	}
	return fmt.Sprintf("… %d more lines", n)
}

// scrollPosition is "Lines a-b of N" when the content is taller than the
// viewport, "" otherwise.
func scrollPosition(offset, visible, total int) string {
	if visible <= 0 || total <= visible {
		return ""
	}
	offset = max(offset, 0)
	return fmt.Sprintf("Lines %d-%d of %d", offset+1, min(offset+visible, total), total)
}

// withoutControlSpace drops carriage returns and turns a tab into four spaces,
// which is how the renderer lays it out.
func withoutControlSpace(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\t", "    ")
}

// normalizeText makes a scanner's text safe to lay out: a tab or a carriage
// return has no width the viewport and the terminal agree on, trailing spaces
// are noise, and a run of blank lines is one blank line. dropBlank removes the
// blank lines altogether (a stderr, where they separate nothing).
func normalizeText(text string, dropBlank bool) string {
	text = withoutControlSpace(text)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " ")
		blank := line == ""
		switch {
		case blank && (dropBlank || len(out) == 0 || out[len(out)-1] == ""):
		default:
			out = append(out, line)
		}
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// dropBlankLines is normalizeText without any blank line.
func dropBlankLines(text string) string { return normalizeText(text, true) }

// detailsText lays text out for the finding detail: every line it returns is
// styled on the theme background and spans at most width cells, measured with
// x/ansi as the viewport does.
type detailsText struct {
	width int            // cells a line may span
	text  lipgloss.Style // ordinary text
	dim   lipgloss.Style // secondary text
}

func newDetailsText(width int) detailsText {
	return detailsText{
		width: width,
		text:  lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText),
		dim:   theme.DimStyle,
	}
}

// block wraps text at the full width, one styled line per entry.
func (t detailsText) block(text string, style lipgloss.Style) []string {
	return t.indented("", text, style)
}

// indented wraps text under a prefix of spaces.
func (t detailsText) indented(prefix, text string, style lipgloss.Style) []string {
	text = normalizeText(text, false)
	if text == "" {
		return nil
	}
	room := max(t.width-ansi.StringWidth(prefix), 1)
	var lines []string
	for _, line := range strings.Split(ansi.Wrap(text, room, ""), "\n") {
		line = strings.TrimRight(line, " ")
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, theme.Bg(prefix)+style.Render(line))
	}
	return lines
}

// field is a label and its value, the value wrapped under itself. When the label
// leaves too little room for the value, the value goes on the lines below.
func (t detailsText) field(label, value string, style lipgloss.Style) []string {
	value = normalizeText(value, false)
	if value == "" {
		return nil
	}
	labelWidth := ansi.StringWidth(label)
	if t.width-labelWidth < minFieldValueWidth {
		return append([]string{theme.SubTitleStyle.Render(strings.TrimRight(label, " "))}, t.block(value, style)...)
	}
	lines := strings.Split(ansi.Wrap(value, t.width-labelWidth, ""), "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		line = style.Render(strings.TrimRight(line, " "))
		if i == 0 {
			out = append(out, theme.SubTitleStyle.Render(label)+line)
		} else {
			out = append(out, theme.Bg(strings.Repeat(" ", labelWidth))+line)
		}
	}
	return out
}

// minFieldValueWidth is the least room a value needs beside its label.
const minFieldValueWidth = 12
