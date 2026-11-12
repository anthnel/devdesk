package ociresources

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// formAction is what a button of the browser form starts.
type formAction int

const (
	actionSearch   formAction = iota // IS-01 routing: short name + Searcher -> level 1, else tags
	actionOpenTags                   // IS-11: always the tag lookup of the typed name
)

// submit validates the form and starts action. An empty name starts nothing;
// no checked registry warns. actionSearch sends a name with no "/" to the image
// search when a Searcher is injected (level 1), anything else lists tags;
// actionOpenTags always lists the tags of the typed name.
func (b *RegistryBrowser) submit(action formAction) (*RegistryBrowser, tea.Cmd) {
	repo := strings.TrimSpace(b.repoInput.Value())
	if repo == "" {
		return b, nil
	}
	var entries []browserRegistryEntry
	for _, entry := range b.entries {
		if b.selected(entry) {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return b, func() tea.Msg { return RegistryBrowserWarnMsg{Text: "No registry selected"} }
	}
	if action == actionSearch && b.searcher != nil && !strings.Contains(repo, "/") {
		return b.startImageSearch(repo, entries)
	}
	return b.startTagLookup(repo, entries)
}

// formButton is one button of the form: its label and what enter on it starts.
type formButton struct {
	label  string
	action formAction
}

// formButtons is the single source of the form's buttons, read by the keyboard,
// the rendering and the header. Without a Searcher the form keeps its one
// Browse Tags button; with one, Search then Open Tags.
func (b *RegistryBrowser) formButtons() []formButton {
	if b.searcher == nil {
		return []formButton{{label: "Browse Tags", action: actionSearch}}
	}
	return []formButton{
		{label: "Search", action: actionSearch},
		{label: "Open Tags", action: actionOpenTags},
	}
}

// brFieldButton returns the form field index of the i-th button: right after
// the Repository field and the picker rows.
func (b *RegistryBrowser) brFieldButton(i int) int { return 1 + len(b.rows) + i }

// brFieldLast returns the index of the last field, the last button.
func (b *RegistryBrowser) brFieldLast() int { return b.brFieldButton(len(b.formButtons()) - 1) }

// focusedButton returns the button the focus is on, and false when it is on
// the Repository field or a checkbox.
func (b *RegistryBrowser) focusedButton() (formButton, bool) {
	buttons := b.formButtons()
	i := b.focusedField - b.brFieldButton(0)
	if i < 0 || i >= len(buttons) {
		return formButton{}, false
	}
	return buttons[i], true
}

// renderFormButtons renders the buttons on one line.
func (b *RegistryBrowser) renderFormButtons() string {
	var sb strings.Builder
	sb.WriteString("  ")
	for i, btn := range b.formButtons() {
		if i > 0 {
			sb.WriteString(theme.Bg("  "))
		}
		sb.WriteString(theme.RenderButton(btn.label, b.focusedField == b.brFieldButton(i), "primary"))
	}
	return sb.String()
}

// FormEnter returns the label of the header's single enter entry and whether
// it is lit. The label follows the focus: "Search" on Repository and on the
// Search button, "Open tags" on the Open Tags button, "Browse tags" where no
// Searcher is wired. A checkbox only advances the focus, so enter is greyed
// there (it keeps the label of the Repository action). The keys never change.
func (b *RegistryBrowser) FormEnter() (label string, enabled bool) {
	repoLabel := "Browse tags"
	if b.searcher != nil {
		repoLabel = "Search"
	}
	if b.focusedField == brFieldRepo {
		return repoLabel, true
	}
	btn, ok := b.focusedButton()
	if !ok {
		return repoLabel, false
	}
	if btn.action == actionOpenTags {
		return "Open tags", true
	}
	return repoLabel, true
}
