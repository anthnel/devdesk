package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// descWrapWidth is the number of runes per visual line for the description field.
const descWrapWidth = 80

// FormType represents the type of creation form
type FormType int

const (
	FormTypeGroup FormType = iota
	FormTypeProject
)

// resourceTypes lists the available resource types in cycle order.
var resourceTypes = []string{"Group", "Project"}

// CreationForm is a form for creating GitLab groups or projects
type CreationForm struct {
	resourceType    int      // 0=Group, 1=Project
	formType        FormType // kept in sync with resourceType
	parentName      string   // Name of parent group (for display)
	parentID        int64    // ID of parent group
	nameInput       textinput.Model
	descInput       WrappedInput
	visibility      int      // 0=private, 1=internal, 2=public
	visibilities    []string // visibility options
	templates       []string // for projects: available templates
	templateIdx     int      // selected template index
	templateScroll  int      // scroll offset for template dropdown
	templateWarning string   // warning message if template loading failed
	// focusedField: 0=resourceType, 1=name, 2=desc, 3=visibility, 4=template (project only), 4or5=submit
	focusedField int
	width        int
	height       int
	err          string
}

// CreationFormSubmitMsg is sent when form is submitted
type CreationFormSubmitMsg struct {
	FormType    FormType
	Name        string
	Description string
	Visibility  string
	Template    string // for projects
	ParentID    int64
}

// CreationFormCancelMsg is sent when form is cancelled
type CreationFormCancelMsg struct{}

// Description character limit (GitLab limit is 250)
const descriptionCharLimit = 250

// maxVisibleTemplates is the maximum number of templates visible in the dropdown
const maxVisibleTemplates = 7

// formTypeFromResourceType converts a resource type index to FormType.
func formTypeFromResourceType(rt int) FormType {
	if rt == 1 {
		return FormTypeProject
	}
	return FormTypeGroup
}

// NewCreationForm creates the unified group/project creation form.
// defaultResourceType: 0=Group, 1=Project.
// focusedField starts at 0 (Type) so the user can immediately cycle the resource type.
func NewCreationForm(defaultResourceType int, parentName string, parentID int64, defaultVisibility string, templates []string) *CreationForm {
	nameInput := textinput.New()
	nameInput.Placeholder = "name"
	nameInput.CharLimit = 100
	nameInput.Width = 50
	theme.StyleTextInput(&nameInput)

	descInput := NewWrappedInput(descWrapWidth, descriptionCharLimit, "description (optional)")

	visibilities := []string{"private", "internal", "public"}
	visIdx := 0
	for i, v := range visibilities {
		if v == defaultVisibility {
			visIdx = i
			break
		}
	}

	// Add "none" template option at the beginning
	allTemplates := append([]string{"none"}, templates...)

	return &CreationForm{
		resourceType: defaultResourceType,
		formType:     formTypeFromResourceType(defaultResourceType),
		parentName:   parentName,
		parentID:     parentID,
		nameInput:    nameInput,
		descInput:    descInput,
		visibility:   visIdx,
		visibilities: visibilities,
		templates:    allTemplates,
		templateIdx:  0,
		focusedField: 0, // start on Type
	}
}

// SetTemplateWarning sets a warning message displayed near the template field
func (f *CreationForm) SetTemplateWarning(msg string) {
	f.templateWarning = msg
}

// Update handles messages
func (f *CreationForm) Update(msg tea.Msg) (*CreationForm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.height = msg.Height
		f.descInput.SetDisplayWidth(msg.Width)

	case tea.KeyMsg:
		return f.handleKeyMsg(msg)
	}

	// Update focused text input
	var cmd tea.Cmd
	switch f.focusedField {
	case 1:
		f.nameInput, cmd = f.nameInput.Update(msg)
	case 2:
		f.descInput, cmd = f.descInput.Update(msg)
	}

	return f, cmd
}

// maxField returns the index of the submit button (last field).
func (f *CreationForm) maxField() int {
	if f.resourceType == 1 && len(f.templates) > 0 {
		return 5
	}
	return 4
}

func (f *CreationForm) handleKeyMsg(msg tea.KeyMsg) (*CreationForm, tea.Cmd) {
	max := f.maxField()

	switch msg.String() {
	case "esc":
		return f, func() tea.Msg { return CreationFormCancelMsg{} }

	case "down":
		if f.isOnTemplateField() && f.templateIdx < len(f.templates)-1 {
			f.templateDown()
			return f, nil
		}
		f.focusedField = (f.focusedField + 1) % (max + 1)
		f.updateFocus()
		return f, nil

	case "up":
		if f.isOnTemplateField() && f.templateIdx > 0 {
			f.templateUp()
			return f, nil
		}
		f.focusedField = (f.focusedField - 1 + max + 1) % (max + 1)
		f.updateFocus()
		return f, nil

	case "enter":
		if f.focusedField == max { // Submit button
			return f.submit()
		}
		// On other fields, move to next
		f.focusedField = (f.focusedField + 1) % (max + 1)
		f.updateFocus()
		return f, nil

	case "left", "right":
		// Field 0: cycle resource type (Group ↔ Project)
		if f.focusedField == 0 {
			if msg.String() == "left" {
				f.resourceType = (f.resourceType - 1 + len(resourceTypes)) % len(resourceTypes)
			} else {
				f.resourceType = (f.resourceType + 1) % len(resourceTypes)
			}
			f.formType = formTypeFromResourceType(f.resourceType)
			// Pas de clamp du focus ici : cette branche n'est atteinte qu'avec
			// focusedField == 0, et maxField() ne descend jamais sous 4.
			return f, nil
		}
		// Field 3: cycle visibility
		if f.focusedField == 3 {
			if msg.String() == "left" {
				f.visibility = (f.visibility - 1 + len(f.visibilities)) % len(f.visibilities)
			} else {
				f.visibility = (f.visibility + 1) % len(f.visibilities)
			}
		}
		return f, nil
	}

	// Update text inputs
	var cmd tea.Cmd
	switch f.focusedField {
	case 1:
		f.nameInput, cmd = f.nameInput.Update(msg)
	case 2:
		f.descInput, cmd = f.descInput.Update(msg)
	}

	return f, cmd
}

// isOnTemplateField returns true if the template dropdown has focus
func (f *CreationForm) isOnTemplateField() bool {
	return f.focusedField == 4 && f.resourceType == 1 && len(f.templates) > 0
}

// templateDown moves template selection down by one
func (f *CreationForm) templateDown() {
	if f.templateIdx < len(f.templates)-1 {
		f.templateIdx++
		f.adjustTemplateScroll()
	}
}

// templateUp moves template selection up by one
func (f *CreationForm) templateUp() {
	if f.templateIdx > 0 {
		f.templateIdx--
		f.adjustTemplateScroll()
	}
}

// adjustTemplateScroll ensures the selected template is visible in the dropdown
func (f *CreationForm) adjustTemplateScroll() {
	// Scroll down if selected is below visible area
	if f.templateIdx >= f.templateScroll+maxVisibleTemplates {
		f.templateScroll = f.templateIdx - maxVisibleTemplates + 1
	}
	// Scroll up if selected is above visible area
	if f.templateIdx < f.templateScroll {
		f.templateScroll = f.templateIdx
	}
}

func (f *CreationForm) updateFocus() {
	f.nameInput.Blur()
	f.descInput.Blur()

	switch f.focusedField {
	case 1:
		f.nameInput.Focus()
	case 2:
		f.descInput.Focus()
	}
}

// GetTitle returns the form title for use in the viewport breadcrumb.
// Includes the parent name when applicable: "New Project (parent: group/sub)".
func (f *CreationForm) GetTitle() string {
	base := "New " + resourceTypes[f.resourceType]
	if f.parentName != "" {
		parent := lipgloss.NewStyle().Foreground(theme.ColorSecondary).Background(theme.ColorBackground).Render("(parent: " + f.parentName + ")")
		return base + " " + parent
	}
	return base
}

// InEditMode returns true when the form is being edited (for preventing command mode)
func (f *CreationForm) InEditMode() bool {
	return true
}

func (f *CreationForm) submit() (*CreationForm, tea.Cmd) {
	name := strings.TrimSpace(f.nameInput.Value())
	if name == "" {
		f.err = "Name is required"
		return f, nil
	}

	template := ""
	if f.formType == FormTypeProject && len(f.templates) > 0 && f.templateIdx > 0 {
		template = f.templates[f.templateIdx]
	}

	return f, func() tea.Msg {
		return CreationFormSubmitMsg{
			FormType:    f.formType,
			Name:        name,
			Description: strings.TrimSpace(f.descInput.Value()),
			Visibility:  f.visibilities[f.visibility],
			Template:    template,
			ParentID:    f.parentID,
		}
	}
}

// View renders the form
func (f *CreationForm) View() string {
	var b strings.Builder

	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	// Error message
	if f.err != "" {
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError).Render("Error: "+f.err) + "\n\n")
	}

	// Resource type selector (cycle ←→)
	b.WriteString(f.renderResourceTypeField())
	b.WriteString("\n\n")

	// Name field
	b.WriteString(f.renderField("Name", f.nameInput.View(), 1))
	b.WriteString("\n\n")

	// Description field (multiline)
	b.WriteString(f.renderDescriptionField())
	b.WriteString("\n\n")

	// Visibility selector
	b.WriteString(f.renderVisibilityField())
	b.WriteString("\n\n")

	// Template dropdown (for projects only)
	if f.resourceType == 1 && len(f.templates) > 0 {
		b.WriteString(f.renderTemplateList())
		b.WriteString("\n\n")
	}

	// Submit button
	b.WriteString("\n")
	b.WriteString("  " + theme.RenderButton("Create", f.focusedField == f.maxField(), "primary"))
	b.WriteString("\n\n")

	// Help is shown in header shortcuts

	// Return without modal border (Rule 112: forms in viewport)
	return b.String()
}

func (f *CreationForm) renderField(label, value string, fieldIdx int) string {
	var labelStr string
	if f.focusedField == fieldIdx {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  " + label + " " + theme.IconChevronRight)
	}
	return labelStr + "\n" + theme.Bg("  ") + value
}

// renderResourceTypeField renders the resource type cycle selector (Rule 132).
func (f *CreationForm) renderResourceTypeField() string {
	label := "Type " + theme.IconSelect + " "
	value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).
		Render(resourceTypes[f.resourceType])
	if f.focusedField == 0 {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}

func (f *CreationForm) renderDescriptionField() string {
	charCount := len([]rune(f.descInput.Value()))
	countStyle := theme.DimStyle
	if charCount > descriptionCharLimit-20 {
		countStyle = lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight)
	}
	if charCount >= descriptionCharLimit {
		countStyle = lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError)
	}
	countStr := countStyle.Render(fmt.Sprintf(" (%d/%d)", charCount, descriptionCharLimit))

	var labelStr string
	if f.focusedField == 2 {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " Description " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  Description " + theme.IconChevronRight)
	}

	return labelStr + countStr + "\n" + f.descInput.View()
}

// renderVisibilityField renders the visibility selector in the same single-line format as
// the "Target Type" field in the security view (Rule 115).
func (f *CreationForm) renderVisibilityField() string {
	label := "Visibility " + theme.IconSelect + " "
	value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(f.visibilities[f.visibility])
	if f.focusedField == 3 {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}

// renderTemplateList renders a vertical scrollable dropdown list for template selection
func (f *CreationForm) renderTemplateList() string {
	focused := f.focusedField == 4

	var labelStr string
	if focused {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " Template " + theme.IconSelect + " " + theme.IconChevronRight + " ")
	} else {
		labelStr = theme.Bg("  Template " + theme.IconSelect + " " + theme.IconChevronRight + " ")
	}

	// Show selected template name next to label when not focused. The warning
	// still goes out: it explains why the list is empty, and a user who never
	// lands on this field would otherwise never learn the registry failed.
	if !focused {
		selected := f.templates[f.templateIdx]
		return labelStr + theme.PrimaryColorStyle.Render(selected) + f.renderTemplateWarning()
	}

	// Focused: render vertical dropdown list
	var b strings.Builder
	b.WriteString(labelStr)
	b.WriteString("\n")

	visibleCount := maxVisibleTemplates
	if len(f.templates) < visibleCount {
		visibleCount = len(f.templates)
	}

	// Scroll indicator: items above
	if f.templateScroll > 0 {
		b.WriteString(theme.DimStyle.Render(fmt.Sprintf("    ↑ %d more", f.templateScroll)))
		b.WriteString("\n")
	}

	// Render visible items
	end := f.templateScroll + visibleCount
	if end > len(f.templates) {
		end = len(f.templates)
	}

	for i := f.templateScroll; i < end; i++ {
		tmpl := f.templates[i]
		if i == f.templateIdx {
			b.WriteString(theme.PrimaryColorStyle.Bold(true).Render("  " + theme.IconCircleSmall + " " + tmpl))
		} else {
			b.WriteString(theme.DimStyle.Render("    " + tmpl))
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	// Scroll indicator: items below
	remaining := len(f.templates) - end
	if remaining > 0 {
		b.WriteString("\n")
		b.WriteString(theme.DimStyle.Render(fmt.Sprintf("    ↓ %d more", remaining)))
	}

	b.WriteString(f.renderTemplateWarning())

	return b.String()
}

// renderTemplateWarning renders the registry failure on its own line, or an
// empty string when the templates loaded. It is appended in both the focused
// and unfocused branches of renderTemplateList: the warning explains why the
// list is empty, so hiding it until the field takes focus tells the user
// nothing at the moment they need it.
func (f *CreationForm) renderTemplateWarning() string {
	if f.templateWarning == "" {
		return ""
	}
	return "\n" + lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorError).
		Render("    "+theme.IconWarning+" "+f.templateWarning)
}
