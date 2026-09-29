package filebrowser

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// headerPathWidth bounds the Path in the header, which shares its line with the
// other header fields.
const headerPathWidth = 48

// View renders the table, the creation form in its place (Rule 112), or the
// delete confirmation over it.
func (m Model) View() string {
	switch {
	case m.mode == modeCreating && m.form != nil:
		return m.form.View(m.currentPath)
	case m.mode == modeConfirmingDelete && m.confirm != nil:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.confirm.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}
	return m.table.View()
}

// GetFooterHeight is Rule 124's footer plus the filter bar when it shows.
func (m Model) GetFooterHeight() int {
	if m.mode == modeNormal {
		return 2 + m.table.FilterBar().ExtraHeight()
	}
	return 2
}

// RenderFooter renders the filter bar, a blank line and the info line. A
// picker's prompt is its status — a state for as long as it is lent, not an
// event with a timer (Rule 128).
func (m Model) RenderFooter(width int) string {
	var parts []string
	if bar := m.table.FilterBar(); m.mode == modeNormal && bar.IsVisible() {
		parts = append(parts, bar.View())
	}
	status := sharedcomponents.Status{}
	if m.pick != nil {
		status.Text = m.pick.Prompt
	}
	parts = append(parts, theme.EmptyLineBg(width), m.footer.View(width, status))
	return strings.Join(parts, "\n")
}

// GetShortcuts is state-aware (Rule 130): the form and the modal replace the
// list, the row greys what it cannot do.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	switch m.mode {
	case modeCreating:
		return []shortcut.Shortcut{
			{Key: "←→", Description: "Change type", Disabled: m.form == nil || m.form.focus != fieldKind},
			{Key: "enter", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	case modeConfirmingDelete:
		return []shortcut.Shortcut{
			{Key: "y/n", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	a := m.actions()
	enter := "View file"
	escape := "Go up"
	if m.pick != nil {
		enter = "Choose"
		escape = "Cancel"
	}
	return []shortcut.Shortcut{
		{Key: "←→", Description: "Up/Open", Disabled: !a.Right.Enabled()},
		{Key: "enter", Description: enter, Disabled: !a.Enter.Enabled()},
		{Key: "esc", Description: escape},
		{Key: keymap.New, Description: "New"},
		{Key: keymap.Copy, Description: "Copy path", Disabled: !a.Copy.Enabled()},
		{Key: keymap.Delete, Description: "Delete", Disabled: !a.Delete.Enabled()},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "/", Description: "Search"},
		{Key: "ctrl+p", Description: "Command"},
		{Key: "?", Description: "Help"},
	}
}

func (m Model) GetTitle() string {
	if m.pick != nil {
		return theme.IconDirectory + " Choose a " + m.pickNoun()
	}
	return theme.IconDirectory + " Files"
}

func (m Model) GetIcon() string { return "" }

func (m Model) pickNoun() string {
	if m.pick != nil && m.pick.Kind == PickFile {
		return "file"
	}
	return "directory"
}

// GetHeaderInfo carries where the table is and how many rows it has — what an
// empty table would otherwise say in its body (Rule 139).
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	path := m.currentPath
	if path == "" {
		path = m.requested
	}
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
		{Key: "Path", Value: theme.ShortPath(path, headerPathWidth), Style: theme.HeaderValueStyle},
		{Key: "Entries", Value: strconv.Itoa(len(m.table.Visible())), Style: theme.HeaderValueStyle},
	}
}

// GetHelpContent implements help.Provider (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Files",
		Description: "A browser over the whole filesystem. It opens in your home directory and ← goes up as far as the root. A form's path field — marked " + theme.IconBrowse + " after its label — opens it as a picker with enter.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑ / ↓", Description: "Move the selection"},
			{Key: "→", Description: "Enter the selected directory"},
			{Key: "←", Description: "Go to the parent directory, with the cursor on the one just left"},
			{Key: "enter", Description: "Open the selected file in the viewer — in a picker, choose the selected entry"},
			{Key: "Esc", Description: "Go to the parent directory — in a picker, close it without choosing"},
			{Key: keymap.New, Description: "Create a directory or an empty file in the directory being browsed"},
			{Key: keymap.Delete, Description: "Delete the selected entry, recursively for a directory, after a confirmation that defaults to No"},
			{Key: keymap.Copy, Description: "Copy the selected entry's absolute path to the system clipboard"},
			{Key: "ctrl+r", Description: "Reread the directory"},
			{Key: "/", Description: "Filter the listing by name"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Picker",
				Body:  "A path field in a form — the workspaces directory, the log file, a scanner's binary or rules file, a local template — carries " + theme.IconBrowse + " after its label. Press enter on it: this view opens where the field points, or at the nearest folder that exists. Enter chooses the selected row; a row of the wrong kind is greyed. When a directory is asked for, the first row, \".\", is the directory being browsed, so an empty one can be chosen too. N works here as well, to create the destination and then choose it. Esc goes back to the form without changing it.",
			},
			{
				Title: "Short paths",
				Body:  "A path that does not fit is shortened the way fish prints its prompt: the home directory becomes ~, then the folders between it and the name are reduced to their first letter, left to right, only as far as needed — ~/projects/work/devdesk, then ~/p/work/devdesk, then ~/p/w/devdesk. The last name is never shortened; only if even that does not fit is the start cut with an ellipsis.",
			},
			{
				Title: "Hidden files",
				Body:  "Entries whose name starts with a dot follow app.show_hidden_files, the same setting as the workspaces view (:config, app tab).",
			},
			{
				Title: "What is never deleted",
				Body:  "A filesystem root and your home directory: D is greyed on them whatever the confirmation would say.",
			},
		},
	}
}
