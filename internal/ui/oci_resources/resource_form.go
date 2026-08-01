package ociresources

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// resourceFormKind distinguishes network vs volume creation
type resourceFormKind int

const (
	resourceFormNetwork resourceFormKind = iota
	resourceFormVolume
)

// networkDrivers lists the standard Docker network drivers
var networkDrivers = []string{"bridge", "host", "overlay", "macvlan", "ipvlan", "null"}

// ResourceFormSubmitMsg is sent when the resource form is submitted
type ResourceFormSubmitMsg struct {
	Kind   resourceFormKind
	Name   string
	Driver string
}

// ResourceFormCancelMsg is sent when the resource form is cancelled
type ResourceFormCancelMsg struct{}

// ResourceCreateForm is a simple form for creating a network or volume
type ResourceCreateForm struct {
	kind         resourceFormKind
	width        int
	nameInput    textinput.Model
	driverInput  textinput.Model // used for volumes
	driverIdx    int             // used for networks (listbox)
	focusedField int             // 0=name, 1=driver, 2=submit
	err          string
}

const resourceFormMaxField = 2

// NewResourceForm creates a new resource creation form
func NewResourceForm(kind resourceFormKind, width int) *ResourceCreateForm {
	nameInput := textinput.New()
	nameInput.CharLimit = 128
	nameInput.Placeholder = "resource-name"
	theme.StyleTextInput(&nameInput)
	nameInput.Focus()

	driverInput := textinput.New()
	driverInput.CharLimit = 64
	theme.StyleTextInput(&driverInput)
	driverInput.Placeholder = "local"
	driverInput.SetValue("local")

	f := &ResourceCreateForm{
		kind:        kind,
		width:       width,
		nameInput:   nameInput,
		driverInput: driverInput,
		driverIdx:   0, // "bridge" by default for networks
	}
	f.resizeInputs()
	return f
}

// SetWidth updates the form width and resizes inputs accordingly.
func (f *ResourceCreateForm) SetWidth(w int) {
	f.width = w
	f.resizeInputs()
}

func (f *ResourceCreateForm) resizeInputs() {
	// Label overhead: viewport border (2) + indent (2) + longest label "Driver " + chevron + spaces (~12)
	const labelOverhead = 16
	w := max(f.width-labelOverhead, 20)
	f.nameInput.Width = w
	f.driverInput.Width = w
}

// Update handles messages for the resource form
func (f *ResourceCreateForm) Update(msg tea.Msg) (*ResourceCreateForm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return f.handleKeyMsg(msg)
	}
	return f.updateActiveInput(msg)
}

func (f *ResourceCreateForm) handleKeyMsg(msg tea.KeyMsg) (*ResourceCreateForm, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return f, func() tea.Msg { return ResourceFormCancelMsg{} }
	case "down":
		f.focusedField = (f.focusedField + 1) % (resourceFormMaxField + 1)
		f.updateFocus()
		return f, nil
	case "up":
		f.focusedField = (f.focusedField - 1 + resourceFormMaxField + 1) % (resourceFormMaxField + 1)
		f.updateFocus()
		return f, nil
	case "left":
		if f.kind == resourceFormNetwork && f.focusedField == 1 {
			f.driverIdx = (f.driverIdx - 1 + len(networkDrivers)) % len(networkDrivers)
			return f, nil
		}
	case "right":
		if f.kind == resourceFormNetwork && f.focusedField == 1 {
			f.driverIdx = (f.driverIdx + 1) % len(networkDrivers)
			return f, nil
		}
	case "enter":
		if f.focusedField == resourceFormMaxField {
			return f.submit()
		}
		f.focusedField = (f.focusedField + 1) % (resourceFormMaxField + 1)
		f.updateFocus()
		return f, nil
	}
	return f.updateActiveInput(msg)
}

func (f *ResourceCreateForm) updateFocus() {
	f.nameInput.Blur()
	f.driverInput.Blur()
	switch f.focusedField {
	case 0:
		f.nameInput.Focus()
	case 1:
		if f.kind == resourceFormVolume {
			f.driverInput.Focus()
		}
	}
}

func (f *ResourceCreateForm) updateActiveInput(msg tea.Msg) (*ResourceCreateForm, tea.Cmd) {
	var cmd tea.Cmd
	switch f.focusedField {
	case 0:
		f.nameInput, cmd = f.nameInput.Update(msg)
	case 1:
		if f.kind == resourceFormVolume {
			f.driverInput, cmd = f.driverInput.Update(msg)
		}
	}
	return f, cmd
}

func (f *ResourceCreateForm) submit() (*ResourceCreateForm, tea.Cmd) {
	name := strings.TrimSpace(f.nameInput.Value())
	if name == "" {
		f.err = "Name is required"
		return f, nil
	}
	driver := strings.TrimSpace(f.driverInput.Value())
	if f.kind == resourceFormNetwork {
		driver = networkDrivers[f.driverIdx]
	}
	return f, func() tea.Msg {
		return ResourceFormSubmitMsg{Kind: f.kind, Name: name, Driver: driver}
	}
}

// GetTitle returns the form title for use in the viewport breadcrumb
func (f *ResourceCreateForm) GetTitle() string {
	if f.kind == resourceFormNetwork {
		return "New Network"
	}
	return "New Volume"
}

// View renders the resource creation form
func (f *ResourceCreateForm) View() string {
	var b strings.Builder

	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	if f.err != "" {
		b.WriteString(theme.StatusErrorStyle.Render("  " + f.err))
		b.WriteString("\n\n")
	}

	b.WriteString(f.renderField("Name", f.nameInput.View(), 0))
	b.WriteString("\n\n")

	if f.kind == resourceFormNetwork {
		b.WriteString(f.renderDriverSelector())
		b.WriteString("\n\n")
	} else {
		b.WriteString(f.renderField("Driver", f.driverInput.View(), 1))
		b.WriteString("\n\n")
	}

	b.WriteString("  ")
	b.WriteString(theme.RenderButton("Create", f.focusedField == resourceFormMaxField, "primary"))

	return b.String()
}

// renderDriverSelector renders the network driver selector in the same style as "Target Type" in the security view
func (f *ResourceCreateForm) renderDriverSelector() string {
	label := "Driver " + theme.IconSelect + " "
	textStyle := theme.Bg(networkDrivers[f.driverIdx])
	highlightStyle := theme.KeyStyle

	if f.focusedField == 1 {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + textStyle
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + textStyle
}

func (f *ResourceCreateForm) renderField(label, value string, fieldIdx int) string {
	var labelStr string
	if f.focusedField == fieldIdx {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  " + label + " " + theme.IconChevronRight)
	}
	return labelStr + "\n  " + value
}
