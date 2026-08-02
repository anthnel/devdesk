package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// FilterToken is a named toggle filter that can be active or inactive.
// Active tokens are shown as highlighted labels in the filter bar.
type FilterToken struct {
	Label  string
	Active bool
}

// FilterBar is a single-line bar rendered at the bottom of a table viewport.
// It is hidden when no filter is active and appears automatically when:
//   - A toggle filter token is activated
//   - The user presses "/" to enter search mode
//
// Usage:
//
//	fb := components.NewFilterBar()
//	// or with toggle tokens:
//	fb := components.NewFilterBarWithTokens([]components.FilterToken{
//	    {Label: "tcp"}, {Label: "udp"},
//	})
//
// In resize: tableHeight := height - fb.ExtraHeight()
// In view:   append fb.View() as the last line after the table
// In update: forward keys to fb.Update(msg) when fb.InEditMode()
type FilterBar struct {
	searching   bool
	searchInput textinput.Model
	searchQuery string
	tokens      []FilterToken
	width       int
}

// NewFilterBar creates a FilterBar with no toggle tokens (text search only).
func NewFilterBar() FilterBar {
	return newFilterBar(nil)
}

// NewFilterBarWithTokens creates a FilterBar with the given toggle tokens.
func NewFilterBarWithTokens(tokens []FilterToken) FilterBar {
	return newFilterBar(tokens)
}

func newFilterBar(tokens []FilterToken) FilterBar {
	si := textinput.New()
	theme.StyleTextInput(&si)
	si.CharLimit = 100

	fb := FilterBar{
		searchInput: si,
	}
	if len(tokens) > 0 {
		fb.tokens = make([]FilterToken, len(tokens))
		copy(fb.tokens, tokens)
	}
	return fb
}

// IsVisible returns true when the bar should be rendered (searching, query set, or any token active).
func (fb *FilterBar) IsVisible() bool {
	if fb.searching || fb.searchQuery != "" {
		return true
	}
	for _, t := range fb.tokens {
		if t.Active {
			return true
		}
	}
	return false
}

// ExtraHeight returns 2 when the bar is visible (1 separator + 1 bar), 0 otherwise.
// Subtract this from the table height so the bar doesn't overlap table rows.
func (fb *FilterBar) ExtraHeight() int {
	if fb.IsVisible() {
		return 2
	}
	return 0
}

// Resize updates the display width used when rendering.
func (fb *FilterBar) Resize(width int) {
	fb.width = width
}

// InEditMode returns true when the search input is focused.
// Views must propagate this via their own InEditMode() to block command mode.
func (fb *FilterBar) InEditMode() bool {
	return fb.searching
}

// SearchQuery returns the current confirmed search string.
func (fb *FilterBar) SearchQuery() string {
	return fb.searchQuery
}

// ActivateSearch focuses the search input and returns the blink command.
func (fb *FilterBar) ActivateSearch() tea.Cmd {
	fb.searching = true
	fb.searchInput.Focus()
	return textinput.Blink
}

// ClearSearch clears the search query and blurs the input.
func (fb *FilterBar) ClearSearch() {
	fb.searching = false
	fb.searchQuery = ""
	fb.searchInput.SetValue("")
	fb.searchInput.Blur()
}

// IsTokenActive returns true if the token with the given label is active.
func (fb *FilterBar) IsTokenActive(label string) bool {
	for _, t := range fb.tokens {
		if t.Label == label {
			return t.Active
		}
	}
	return false
}

// SetTokenActive sets the active state of the token with the given label.
func (fb *FilterBar) SetTokenActive(label string, active bool) {
	for i, t := range fb.tokens {
		if t.Label == label {
			fb.tokens[i].Active = active
			return
		}
	}
}

// SetTokens replaces the token list. Active state is preserved for tokens
// whose label matches an existing active token.
func (fb *FilterBar) SetTokens(tokens []FilterToken) {
	activeLabels := make(map[string]bool)
	for _, t := range fb.tokens {
		if t.Active {
			activeLabels[t.Label] = true
		}
	}
	fb.tokens = make([]FilterToken, len(tokens))
	copy(fb.tokens, tokens)
	for i, t := range fb.tokens {
		if activeLabels[t.Label] {
			fb.tokens[i].Active = true
		}
	}
}

// Update handles key messages when the bar is in search mode.
// It returns the updated FilterBar and a Bubble Tea command.
// Call this from the view's Update() when fb.InEditMode() is true.
func (fb *FilterBar) Update(msg tea.Msg) (FilterBar, tea.Cmd) {
	if !fb.searching {
		return *fb, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return *fb, nil
	}
	switch keyMsg.String() {
	case "esc":
		fb.searching = false
		fb.searchQuery = ""
		fb.searchInput.SetValue("")
		fb.searchInput.Blur()
		return *fb, nil
	case "enter":
		fb.searching = false
		fb.searchQuery = fb.searchInput.Value()
		fb.searchInput.Blur()
		return *fb, nil
	}
	var cmd tea.Cmd
	fb.searchInput, cmd = fb.searchInput.Update(msg)
	fb.searchQuery = fb.searchInput.Value()
	return *fb, cmd
}

// View renders the filter bar as two lines forming a closed rectangle.
// The viewport's bottom border (└──────┘) serves as the top of the rectangle.
//
// Line 1 (content): │  / cursor...   [tcp] · [LISTEN]  │
// Line 2 (bottom):  └──────────────────────────────────┘
func (fb *FilterBar) View() string {
	w := fb.width
	if w == 0 {
		w = 80
	}

	borderStyle := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)

	var right string
	if len(fb.tokens) > 0 {
		var activeTokens []string
		for _, t := range fb.tokens {
			if t.Active {
				activeTokens = append(activeTokens, theme.KeyStyle.Render(t.Label))
			}
		}
		if len(activeTokens) > 0 {
			right = "  " + strings.Join(activeTokens, theme.DimStyle.Render("  ·  "))
		}
	}

	var left string
	if fb.searching {
		left = theme.KeyStyle.Render("/") + theme.Bg(" ") + fb.searchInput.View()
	} else {
		if fb.searchQuery != "" {
			left = theme.DimStyle.Render("/ ") + theme.KeyStyle.Render(fb.searchQuery)
		} else {
			left = theme.DimStyle.Render("/ search...")
		}
	}

	inner := theme.Bg(" ") + left + theme.Bg(right)
	// w-2 to leave room for │ on each side
	paddedInner := theme.PadWithBg(inner, w-2)
	contentLine := borderStyle.Render("│") + paddedInner + borderStyle.Render("│")
	bottomBorder := borderStyle.Render("└" + strings.Repeat("─", w-2) + "┘")
	return contentLine + "\n" + bottomBorder
}
