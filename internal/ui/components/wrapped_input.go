package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// WrappedInput is a multi-line text field that word-wraps content at a configurable column.
// It uses textinput.Model internally (correct background) and renders each visual line
// with theme.PadWithBg, ensuring a uniform app background on every line.
//
// Usage:
//
//	field := NewWrappedInput(80, 250, "description (optional)")
//	field.SetDisplayWidth(terminalWidth)
//
//	// In Update():
//	field, cmd = field.Update(msg)
//
//	// In View():
//	return labelStr + "\n" + field.View()
type WrappedInput struct {
	input        textinput.Model
	wrapWidth    int
	displayWidth int
	focused      bool
}

// NewWrappedInput creates a WrappedInput.
// wrapWidth: number of runes per visual line before wrapping (e.g. 80).
// charLimit: maximum total characters allowed (0 = unlimited).
// placeholder: text shown when empty and unfocused.
func NewWrappedInput(wrapWidth, charLimit int, placeholder string) WrappedInput {
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = charLimit
	input.Width = 4096 // never scrolls internally; display is word-wrapped manually
	theme.StyleTextInput(&input)
	return WrappedInput{
		input:     input,
		wrapWidth: wrapWidth,
	}
}

// SetDisplayWidth sets the full terminal width used for background padding.
// Call this whenever a tea.WindowSizeMsg is received.
func (w *WrappedInput) SetDisplayWidth(width int) {
	w.displayWidth = width
}

// Focus gives keyboard focus to the input and returns the blink command.
func (w *WrappedInput) Focus() tea.Cmd {
	w.focused = true
	return w.input.Focus()
}

// Blur removes keyboard focus from the input.
func (w *WrappedInput) Blur() {
	w.focused = false
	w.input.Blur()
}

// Value returns the current text value.
func (w WrappedInput) Value() string {
	return w.input.Value()
}

// Update handles Bubble Tea messages and returns the updated model.
func (w WrappedInput) Update(msg tea.Msg) (WrappedInput, tea.Cmd) {
	var cmd tea.Cmd
	w.input, cmd = w.input.Update(msg)
	return w, cmd
}

// View renders the word-wrapped content block padded to displayWidth.
// Each line is prefixed with a 2-char indent. When focused, a block cursor
// is drawn at the current rune position.
func (w WrappedInput) View() string {
	value := w.input.Value()
	lines := wrapInputLines(value, w.wrapWidth)
	cursorPos := w.input.Position()

	if len(lines) == 0 {
		if w.focused {
			cur := lipgloss.NewStyle().Background(theme.ColorText).Foreground(theme.ColorBackground).Render(" ")
			return theme.PadWithBg("  "+cur, w.displayWidth)
		}
		return theme.PadWithBg(theme.DimStyle.Render("  "+w.input.Placeholder), w.displayWidth)
	}

	cursorStyle := lipgloss.NewStyle().Background(theme.ColorText).Foreground(theme.ColorBackground)

	var b strings.Builder
	for i, wl := range lines {
		runes := []rune(wl.text)
		lineEnd := wl.startIndex + len(runes)

		var displayLine string
		if w.focused && cursorPos >= wl.startIndex && cursorPos <= lineEnd {
			col := cursorPos - wl.startIndex
			before := string(runes[:col])
			var cur, after string
			if col < len(runes) {
				cur = cursorStyle.Render(string(runes[col]))
				after = string(runes[col+1:])
			} else {
				cur = cursorStyle.Render(" ")
			}
			displayLine = "  " + before + cur + after
		} else {
			displayLine = "  " + wl.text
		}

		b.WriteString(theme.PadWithBg(displayLine, w.displayWidth))
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// wrappedLine holds one visual line and its start offset in the original rune slice.
type wrappedLine struct {
	text       string
	startIndex int
}

// wrapInputLines splits text into visual lines of at most wrapWidth runes,
// breaking at word boundaries where possible, falling back to a hard break.
func wrapInputLines(text string, wrapWidth int) []wrappedLine {
	if text == "" {
		return nil
	}
	// A non-positive width makes the loop below unable to advance: the break
	// point collapses onto start and the line slice grows without bound. No
	// caller does this today (Rule 133 fixes the width at 60/80/100), so refuse
	// to wrap rather than inventing a fallback width.
	if wrapWidth < 1 {
		return []wrappedLine{{text, 0}}
	}
	runes := []rune(text)
	var lines []wrappedLine
	start := 0

	for start < len(runes) {
		end := start + wrapWidth
		if end >= len(runes) {
			lines = append(lines, wrappedLine{string(runes[start:]), start})
			break
		}
		// Walk back to the last space within the allowed width.
		breakAt := end
		for breakAt > start && runes[breakAt] != ' ' {
			breakAt--
		}
		if breakAt == start {
			breakAt = end // no space found: hard break at wrapWidth
		}
		lines = append(lines, wrappedLine{string(runes[start:breakAt]), start})
		start = breakAt
		if start < len(runes) && runes[start] == ' ' {
			start++ // consume the breaking space
		}
	}
	return lines
}
