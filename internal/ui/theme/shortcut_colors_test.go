package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// ColorShortcutDisabled is an alias assigned in ApplyTheme, like the footer and
// syntax colours: it must follow whatever palette is applied rather than keep
// the value it had when the package was loaded.
func TestTheDisabledShortcutColorFollowsAThemeChange(t *testing.T) {
	th := DefaultTheme()
	th.Name = "dimmed"
	th.ColorDim = "#123456"
	withTheme(t, th)

	if got, want := ColorShortcutDisabled, ColorDim; got != want {
		t.Errorf("ColorShortcutDisabled = %q, want ColorDim %q", got, want)
	}
	if ColorShortcutDisabled != lipgloss.Color("#123456") {
		t.Errorf("ColorShortcutDisabled = %q; it did not follow the applied theme", ColorShortcutDisabled)
	}
}

// The style is rebuilt by RefreshStyles, which ApplyTheme calls last. A key
// keeping its weight would read as an emphasis rather than as an absence, so
// the two properties are asserted together.
func TestTheDisabledShortcutKeyStyleDropsColourAndWeight(t *testing.T) {
	withTheme(t, DefaultTheme())

	if got := ShortcutKeyDisabledStyle.GetForeground(); got != lipgloss.TerminalColor(ColorShortcutDisabled) {
		t.Errorf("ShortcutKeyDisabledStyle foreground = %v, want %v", got, ColorShortcutDisabled)
	}
	if ShortcutKeyDisabledStyle.GetBold() {
		t.Error("ShortcutKeyDisabledStyle is bold; a grey bold key reads as an emphasis")
	}
	if got := ShortcutKeyDisabledStyle.GetBackground(); got != lipgloss.TerminalColor(ColorBackground) {
		t.Errorf("ShortcutKeyDisabledStyle has no app background (Rule 115): %v", got)
	}
	if ShortcutKeyDisabledStyle.GetForeground() == ShortcutKeyStyle.GetForeground() {
		t.Error("a disabled key is painted like an available one; nothing tells them apart")
	}
}
