package workspaces

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Focus indices for the input form
const (
	focusInput = iota
	focusCreate
	focusCancel
)

// WorkspaceInput handles the input form for creating or renaming a workspace
type WorkspaceInput struct {
	input      textinput.Model
	focusIndex int
	title      string
	isRename   bool
}

// NewWorkspaceInput creates a new workspace input form for directory creation
func NewWorkspaceInput() *WorkspaceInput {
	ti := textinput.New()
	ti.Placeholder = "directory-name"
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40
	theme.StyleTextInput(&ti)

	return &WorkspaceInput{
		input:      ti,
		focusIndex: focusInput,
		title:      "New Directory",
		isRename:   false,
	}
}

// NewRenameInput creates a new workspace input form for renaming, pre-filled with the current name
func NewRenameInput(currentName string) *WorkspaceInput {
	ti := textinput.New()
	ti.Placeholder = "new-name"
	ti.SetValue(currentName)
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40
	theme.StyleTextInput(&ti)

	return &WorkspaceInput{
		input:      ti,
		focusIndex: focusInput,
		title:      "Rename",
		isRename:   true,
	}
}

// RenameInputSubmitMsg is sent when the rename form is submitted
type RenameInputSubmitMsg struct {
	Name string
}

// WorkspaceInputSubmitMsg is sent when the user submits the input
type WorkspaceInputSubmitMsg struct {
	Name string
}

// WorkspaceInputCancelMsg is sent when the user cancels the input
type WorkspaceInputCancelMsg struct{}

// Update handles input events
func (w *WorkspaceInput) Update(msg tea.Msg) (*WorkspaceInput, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down":
			w.nextFocus()
			return w, nil

		case "up":
			w.prevFocus()
			return w, nil

		case "enter":
			return w.handleEnter()

		case "esc":
			return w, func() tea.Msg {
				return WorkspaceInputCancelMsg{}
			}
		}
	}

	// Only update text input if it's focused
	if w.focusIndex == focusInput {
		w.input, cmd = w.input.Update(msg)
	}

	return w, cmd
}

// nextFocus moves focus to the next element
func (w *WorkspaceInput) nextFocus() {
	w.focusIndex++
	if w.focusIndex > focusCancel {
		w.focusIndex = focusInput
	}
	w.updateInputFocus()
}

// prevFocus moves focus to the previous element
func (w *WorkspaceInput) prevFocus() {
	w.focusIndex--
	if w.focusIndex < focusInput {
		w.focusIndex = focusCancel
	}
	w.updateInputFocus()
}

// updateInputFocus updates the text input focus state
func (w *WorkspaceInput) updateInputFocus() {
	if w.focusIndex == focusInput {
		w.input.Focus()
	} else {
		w.input.Blur()
	}
}

// handleEnter processes enter key based on current focus
func (w *WorkspaceInput) handleEnter() (*WorkspaceInput, tea.Cmd) {
	switch w.focusIndex {
	case focusInput, focusCreate:
		name := strings.TrimSpace(w.input.Value())
		if name != "" {
			if w.isRename {
				return w, func() tea.Msg {
					return RenameInputSubmitMsg{Name: name}
				}
			}
			return w, func() tea.Msg {
				return WorkspaceInputSubmitMsg{Name: name}
			}
		}
		return w, nil

	case focusCancel:
		return w, func() tea.Msg {
			return WorkspaceInputCancelMsg{}
		}
	}

	return w, nil
}

// View renders the input form
func (w *WorkspaceInput) View() string {
	var b strings.Builder

	// Title
	b.WriteString(theme.TitleStyle.Render(w.title))
	b.WriteString("\n\n")

	// Input field with focus indicator
	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if w.focusIndex == focusInput {
		b.WriteString(highlightStyle.Render(theme.IconCircleSmall + " Name " + theme.IconChevronRight + " "))
	} else {
		b.WriteString(theme.Bg("  Name " + theme.IconChevronRight + " "))
	}
	b.WriteString(w.input.View())
	b.WriteString("\n\n")

	// Buttons with focus state
	submitLabel := "Create"
	if w.isRename {
		submitLabel = "Rename"
	}
	createFocused := w.focusIndex == focusCreate
	cancelFocused := w.focusIndex == focusCancel

	submit := theme.RenderButton(submitLabel, createFocused, "primary")
	cancel := theme.RenderButton("Cancel", cancelFocused, "secondary")
	buttons := submit + theme.Bg("  ") + cancel
	b.WriteString(buttons)
	b.WriteString("\n\n")

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorPrimary).
		BorderBackground(theme.ColorBackground).
		Padding(1, 2).
		Render(b.String())
}

// Focus sets focus on the input
func (w *WorkspaceInput) Focus() {
	w.focusIndex = focusInput
	w.input.Focus()
}

// Blur removes focus from the input
func (w *WorkspaceInput) Blur() {
	w.input.Blur()
}
