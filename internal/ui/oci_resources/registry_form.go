package ociresources

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

const registryFormMaxField = 6 // 0=URL, 1=Username, 2=Password, 3=Alias, 4=AuthEnabled, 5=NexusURL, 6=Submit

// RegistryFormSubmitMsg is sent when the registry form is submitted
type RegistryFormSubmitMsg struct {
	Index    int // -1 for new, >= 0 for edit
	Item     config.RegistryItem
	Password string // not stored in config; used for docker login
}

// RegistryFormCancelMsg is sent when the registry form is cancelled
type RegistryFormCancelMsg struct{}

// RegistryForm is a form for adding or editing a registry entry
type RegistryForm struct {
	index         int // -1 = new, >=0 = edit
	width         int
	urlInput      textinput.Model
	usernameInput textinput.Model
	passwordInput textinput.Model
	aliasInput    textinput.Model
	nexusURLInput textinput.Model
	authEnabled   bool
	focusedField  int
	err           string
}

// NewRegistryForm creates a form for adding a new registry
func NewRegistryForm(width int) *RegistryForm {
	return newRegistryForm(-1, config.RegistryItem{AuthEnabled: true}, width)
}

// NewRegistryEditForm creates a form for editing an existing registry
func NewRegistryEditForm(index int, item config.RegistryItem, width int) *RegistryForm {
	return newRegistryForm(index, item, width)
}

func newRegistryForm(index int, item config.RegistryItem, width int) *RegistryForm {
	urlInput := textinput.New()
	urlInput.CharLimit = 256
	urlInput.Placeholder = "registry.example.com"
	theme.StyleTextInput(&urlInput)
	urlInput.SetValue(item.URL)
	urlInput.Focus()

	usernameInput := textinput.New()
	usernameInput.CharLimit = 128
	usernameInput.Placeholder = "username"
	theme.StyleTextInput(&usernameInput)
	usernameInput.SetValue(item.Username)

	passwordInput := textinput.New()
	passwordInput.CharLimit = 256
	passwordInput.Placeholder = "password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '•'
	theme.StyleTextInput(&passwordInput)

	aliasInput := textinput.New()
	aliasInput.CharLimit = 32
	aliasInput.Placeholder = "gl"
	theme.StyleTextInput(&aliasInput)
	aliasInput.SetValue(item.Alias)

	nexusURLInput := textinput.New()
	nexusURLInput.CharLimit = 512
	nexusURLInput.Placeholder = "https://registry-mgr.example.com/path/to/group  (optional)"
	theme.StyleTextInput(&nexusURLInput)
	nexusURLInput.SetValue(item.ManagementURL)

	f := &RegistryForm{
		index:         index,
		width:         width,
		urlInput:      urlInput,
		usernameInput: usernameInput,
		passwordInput: passwordInput,
		aliasInput:    aliasInput,
		nexusURLInput: nexusURLInput,
		authEnabled:   item.AuthEnabled,
	}
	f.resizeInputs()
	return f
}

// SetWidth updates the form width and resizes inputs accordingly.
func (f *RegistryForm) SetWidth(w int) {
	f.width = w
	f.resizeInputs()
}

func (f *RegistryForm) resizeInputs() {
	// Label overhead: viewport border (2) + indent (2) + longest label "Nexus URL " + chevron + spaces (~16)
	const labelOverhead = 18
	w := max(f.width-labelOverhead, 20)
	f.urlInput.Width = w
	f.usernameInput.Width = w
	f.passwordInput.Width = w
	f.aliasInput.Width = w
	f.nexusURLInput.Width = w
}

// GetTitle returns the form title for the viewport breadcrumb
func (f *RegistryForm) GetTitle() string {
	if f.index < 0 {
		return "New Registry"
	}
	return "Edit Registry"
}

// Update handles messages for the registry form
func (f *RegistryForm) Update(msg tea.Msg) (*RegistryForm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return f.handleKeyMsg(msg)
	}
	return f.updateActiveInput(msg)
}

func (f *RegistryForm) handleKeyMsg(msg tea.KeyMsg) (*RegistryForm, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return f, func() tea.Msg { return RegistryFormCancelMsg{} }
	case "down":
		f.focusedField = (f.focusedField + 1) % (registryFormMaxField + 1)
		f.updateFocus()
		return f, nil
	case "up":
		f.focusedField = (f.focusedField - 1 + registryFormMaxField + 1) % (registryFormMaxField + 1)
		f.updateFocus()
		return f, nil
	case "left":
		if f.focusedField == 4 {
			f.authEnabled = !f.authEnabled
			return f, nil
		}
	case "right":
		if f.focusedField == 4 {
			f.authEnabled = !f.authEnabled
			return f, nil
		}
	case "enter":
		if f.focusedField == registryFormMaxField {
			return f.submit()
		}
		f.focusedField = (f.focusedField + 1) % (registryFormMaxField + 1)
		f.updateFocus()
		return f, nil
	}
	return f.updateActiveInput(msg)
}

func (f *RegistryForm) updateFocus() {
	f.urlInput.Blur()
	f.usernameInput.Blur()
	f.passwordInput.Blur()
	f.aliasInput.Blur()
	f.nexusURLInput.Blur()
	switch f.focusedField {
	case 0:
		f.urlInput.Focus()
	case 1:
		f.usernameInput.Focus()
	case 2:
		f.passwordInput.Focus()
	case 3:
		f.aliasInput.Focus()
	case 5:
		f.nexusURLInput.Focus()
	}
}

func (f *RegistryForm) updateActiveInput(msg tea.Msg) (*RegistryForm, tea.Cmd) {
	var cmd tea.Cmd
	switch f.focusedField {
	case 0:
		f.urlInput, cmd = f.urlInput.Update(msg)
	case 1:
		f.usernameInput, cmd = f.usernameInput.Update(msg)
	case 2:
		f.passwordInput, cmd = f.passwordInput.Update(msg)
	case 3:
		f.aliasInput, cmd = f.aliasInput.Update(msg)
	case 5:
		f.nexusURLInput, cmd = f.nexusURLInput.Update(msg)
	}
	return f, cmd
}

func (f *RegistryForm) submit() (*RegistryForm, tea.Cmd) {
	url := strings.TrimSpace(f.urlInput.Value())
	if url == "" {
		f.err = "URL is required"
		return f, nil
	}
	item := config.RegistryItem{
		URL:           url,
		Username:      strings.TrimSpace(f.usernameInput.Value()),
		Alias:         strings.TrimSpace(f.aliasInput.Value()),
		AuthEnabled:   f.authEnabled,
		ManagementURL: strings.TrimSpace(f.nexusURLInput.Value()),
	}
	pass := f.passwordInput.Value()
	idx := f.index
	return f, func() tea.Msg {
		return RegistryFormSubmitMsg{Index: idx, Item: item, Password: pass}
	}
}

// View renders the registry form (Rule 131: starts with empty line)
func (f *RegistryForm) View() string {
	var b strings.Builder

	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	if f.err != "" {
		b.WriteString(theme.StatusErrorStyle.Render("  " + f.err))
		b.WriteString("\n\n")
	}

	b.WriteString(f.renderField("URL", f.urlInput.View(), 0))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Username", f.usernameInput.View(), 1))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Password", f.passwordInput.View(), 2))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Alias", f.aliasInput.View(), 3))
	b.WriteString("\n\n")
	b.WriteString(f.renderAuthToggle())
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Management URL", f.nexusURLInput.View(), 5))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(theme.RenderButton("Save", f.focusedField == registryFormMaxField, "primary"))

	return b.String()
}

func (f *RegistryForm) renderField(label, value string, fieldIdx int) string {
	var labelStr string
	if f.focusedField == fieldIdx {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  " + label + " " + theme.IconChevronRight)
	}
	return labelStr + "\n  " + value
}

func (f *RegistryForm) renderAuthToggle() string {
	label := "Auth Enabled " + theme.IconSelect + " "
	var value string
	if f.authEnabled {
		value = theme.Bg("yes")
	} else {
		value = theme.Bg("no")
	}
	if f.focusedField == 4 {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}
