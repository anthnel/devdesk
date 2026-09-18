package templates

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

// GetTitle names the view, and the form when one is open.
func (m Model) GetTitle() string {
	base := theme.IconRepository + " Templates"
	if m.form != nil {
		return base + " " + theme.IconChevronRight + " " + m.form.GetTitle()
	}
	return base
}

// GetIcon returns the view icon. The title carries it, as everywhere else.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo says how many templates are on screen. The count is what says
// why the table is empty — no template, or a filter matching none — since the
// body is always the table (Rule 139).
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Templates", Value: strconv.Itoa(len(m.table.Visible())), Style: theme.HeaderValueStyle},
	}
}

// GetShortcuts returns the header's shortcut column (Rules 130, 137, 138).
//
// A mode replaces the list; a state inside the list only greys entries.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	if m.confirmModal != nil {
		return []shortcut.Shortcut{
			{Key: "←→", Description: "Choose"},
			{Key: "enter", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	if m.form != nil {
		return []shortcut.Shortcut{
			// Structural: ←→ only means something on the source field, and there
			// is nothing to explain about a lost arrow key (Rule 130).
			{Key: "←→", Description: "Change source", Disabled: !m.form.OnCycleField()},
			{Key: "enter", Description: "Next field"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	a := m.availability()
	return []shortcut.Shortcut{
		{Key: keymap.New, Description: "Create template", Disabled: !a.New.Enabled()},
		{Key: keymap.Edit, Description: "Edit template", Disabled: !a.Edit.Enabled()},
		{Key: keymap.Delete, Description: "Delete template", Disabled: !a.Delete.Enabled()},
		{Key: keymap.Pager, Description: "Preview files", Disabled: !a.Preview.Enabled()},
		{Key: ".", Description: "Sort"},
		{Key: "/", Description: "Filter"},
		{Key: "?", Description: "Help"},
	}
}

// InEditMode tells the router a field or the filter has the keyboard: `:` is a
// character in a URL (app.FormView).
func (m Model) InEditMode() bool {
	return m.form != nil || m.confirmModal != nil || m.table.InEditMode()
}

// FilterBarVisible closes the viewport rectangle around the bar
// (app.FilterBarView). It is not shown under a form or a modal.
func (m Model) FilterBarVisible() bool {
	return m.form == nil && m.confirmModal == nil && m.table.FilterBar().IsVisible()
}

// GetFooterHeight budgets the footer (Rules 124, 136).
func (m Model) GetFooterHeight() int {
	if m.form != nil || m.confirmModal != nil {
		return 2
	}
	return 2 + m.table.FilterBar().ExtraHeight()
}

// RenderFooter renders the lines below the viewport: filter bar, blank, message.
func (m Model) RenderFooter(width int) string {
	var parts []string
	if m.FilterBarVisible() {
		parts = append(parts, m.table.FilterBar().View())
	}
	parts = append(parts, theme.EmptyLineBg(width), m.footer.View(width, sharedcomponents.Status{}))
	return strings.Join(parts, "\n")
}

// View renders the form, the modal, or the table — and the table whatever it
// holds: while loading, empty, or filtered to nothing it is a header and no rows
// (Rule 139).
func (m Model) View() string {
	if m.form != nil {
		return m.form.View()
	}
	if m.confirmModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}
	return m.table.View()
}

// GetHelpContent returns the help for `?` (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Templates",
		Description: "The catalog of repository templates. A template is a reference to content that lives somewhere else — " +
			"a git repository, a directory on this machine, or an artifact in an OCI registry — and it is fetched from there when it is needed, " +
			"so the catalog cannot drift from the original.",
		KeyBindings: []help.KeyBinding{
			{Key: keymap.New, Description: "Add a template to the catalog"},
			{Key: keymap.Edit, Description: "Edit the selected template"},
			{Key: keymap.Delete, Description: "Remove the selected template from the catalog. The template's own content is never touched"},
			{Key: keymap.Pager, Description: "Preview the files the template would put in a new repository"},
			{Key: ".", Description: "Cycle the sort column. Each press toggles asc/desc, then moves to the next column"},
			{Key: "/", Description: "Filter by name, description, tags or source"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Sources",
				Body: "git: a remote repository. Give the clone URL, optionally a subdirectory to use as the template's root, and a branch, tag or commit.\n" +
					"local: a git repository on this machine. Only what is committed is used — no .git, nothing ignored, no uncommitted edit — so a template never depends on the state of a working tree.\n" +
					"oci: an artifact in a registry — the registry URL, the repository and the tag.\n\n" +
					"A URL may be https, ssh, git or the scp form (git@host:path). Nothing else is accepted: the catalog file can be shared, and a URL is not trusted.",
			},
			{
				Title: "Tags",
				Body:  "Free-form labels — java, spring-boot, ci-component — separated by commas. They are lowercased and de-duplicated, and searched by the filter as stored.",
			},
			{
				Title: "The catalog",
				Body: "Templates are kept in ~/.devdesk/templates.yaml and shared by every context. " +
					"When a repository is created, the Template field of the form offers this list, or none for an empty repository.",
			},
			{
				Title: "Credentials",
				Body: "A source is fetched anonymously unless it is on the same host as the context's forge (git) or registry (oci), in which case the stored token or password is used. " +
					"A token authenticates one host, so it is never offered to another: a private repository on any other host needs an SSH URL.",
			},
		},
	}
}
