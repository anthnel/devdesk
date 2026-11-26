package components

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// Resize updates the display width used when rendering and fits the search
// field to it.
func (fb *FilterBar) Resize(width int) {
	fb.width = width
	fb.fitField()
}

// barChrome is what the bar spends on the frame and the lead-in around the
// query: "│" and "│" (2), the leading space (1) and "/ " (2).
const barChrome = 5

// tokensText returns the right-hand part of the bar: two spaces and the active
// token labels joined by a dim dot. It is empty when no token is active.
func (fb *FilterBar) tokensText() string {
	var active []string
	for _, t := range fb.tokens {
		if t.Active {
			active = append(active, theme.KeyStyle.Render(t.Label))
		}
	}
	if len(active) == 0 {
		return ""
	}
	return "  " + strings.Join(active, theme.DimStyle.Render("  ·  "))
}

// queryRoom is how many cells the query may use on the content line: what the
// frame, the lead-in and the active tokens leave. A bar still at width 0 is
// drawn 80 cells wide by BarFrame, so the same default applies here. A bar
// narrower than its own tokens gets 1: its line may then overrun the width,
// which is out of scope (BarFrame itself panics below a width of 2).
func (fb *FilterBar) queryRoom() int {
	width := fb.width
	if width == 0 {
		width = 80
	}
	return max(width-barChrome-ansi.StringWidth(fb.tokensText()), 1)
}

// fieldWidth is the width of the search field: the room of the query minus the
// cell the cursor takes after the last character, with a floor of 1.
func (fb *FilterBar) fieldWidth() int {
	return max(fb.queryRoom()-1, 1)
}

// fitField hands the field its width. The scroll window is re-seated only when
// the width changed, so a resize that keeps the field's size does not scroll a
// line that did not need it (components.ReseatInput).
func (fb *FilterBar) fitField() {
	before := fb.searchInput.Width()
	fb.searchInput.SetWidth(fb.fieldWidth())
	if fb.searchInput.Width() != before {
		ReseatInput(&fb.searchInput)
	}
}

// cutQuery shortens query to at most room cells, from its end, with an ellipsis
// in the last cell. It walks grapheme clusters, so a wide glyph that does not
// fit is dropped whole (the line is then one cell short and the frame pads it).
func cutQuery(query string, room int) string {
	if ansi.StringWidth(query) <= room {
		return query
	}
	var b strings.Builder
	used := 0
	for rest := query; rest != ""; {
		cluster, w := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if cluster == "" || used+w > room-1 {
			break
		}
		b.WriteString(cluster)
		used += w
		rest = rest[len(cluster):]
	}
	return b.String() + "…"
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
	fb.fitField()
	fb.searchInput.Focus()
	return textinput.Blink
}

// EndSearch leaves the search input without touching the query: the text typed
// so far stays applied, as if enter had been pressed. A view calls it when
// something else must take the keyboard.
func (fb *FilterBar) EndSearch() {
	fb.searching = false
	fb.searchQuery = fb.searchInput.Value()
	fb.searchInput.Blur()
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
			fb.fitField()
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
	fb.fitField()
}

// Update handles key messages when the bar is in search mode.
// It returns the updated FilterBar and a Bubble Tea command.
// Call this from the view's Update() when fb.InEditMode() is true.
func (fb *FilterBar) Update(msg tea.Msg) (FilterBar, tea.Cmd) {
	if !fb.searching {
		return *fb, nil
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		// A paste is text for the search input, never an action key. The
		// input turns newlines into spaces itself.
		var cmd tea.Cmd
		fb.searchInput, cmd = fb.searchInput.Update(paste)
		fb.searchQuery = fb.searchInput.Value()
		return *fb, cmd
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
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
	right := fb.tokensText()

	var left string
	if fb.searching {
		// The field is drawn from a copy without a width: with one, the
		// textinput pads its value up to it and the tokens would jump while
		// the user types. The cursor cell stays; BarFrame fills the line.
		field := fb.searchInput
		field.SetWidth(0)
		left = theme.KeyStyle.Render("/") + theme.Bg(" ") + field.View()
	} else {
		if fb.searchQuery != "" {
			left = theme.DimStyle.Render("/ ") + theme.KeyStyle.Render(cutQuery(fb.searchQuery, fb.queryRoom()))
		} else {
			left = theme.DimStyle.Render("/ search...")
		}
	}

	return BarFrame(fb.width, theme.Bg(" ")+left+theme.Bg(right))
}
