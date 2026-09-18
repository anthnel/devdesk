package components

import (
	"fmt"
	"github.com/anthnel/devdesk/internal/forge"
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

// resourceTypeLabels are what the two kinds are called, in cycle order. They
// come from the forge's vocabulary rather than being written here: a GitLab
// user creates a Group or a Project, a GitHub user an Organization or a
// Repository, and this component knows neither (§3.6).
func resourceTypeLabels(v forge.Vocabulary) [2]string {
	return [2]string{v.Namespace, v.Repository}
}

// CreationForm is a form for creating GitLab groups or projects
type CreationForm struct {
	resourceType   int       // 0=namespace, 1=repository
	resourceLabels [2]string // the forge's words for the two, in cycle order
	formType       FormType  // kept in sync with resourceType
	parentName     string    // Name of parent group (for display)
	// parentID is the forge's opaque identifier for the parent namespace, empty
	// at the root. It was an int64 — GitLab's numeric id — which is exactly what
	// §3.6 made opaque: the form carries it and never reads it.
	parentID     string
	nameInput    textinput.Model
	descInput    WrappedInput
	visibility   int      // index into visibilities
	visibilities []string // visibility options, already narrowed by the caller
	// to whatever the parent (and the forge) allow — see
	// forge.Shape.VisibilitiesUnder. Never empty in practice: every known
	// forge declares at least one.
	visibilityNote string // why the choices above are narrower than the forge's own list, if they are
	// template is the catalog slug of the template chosen for a project, and
	// templateName what it is called. Both are empty for none, which is what a
	// repository gets unless one is picked: an empty repository.
	template     string
	templateName string
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
	Template    string // for projects: the catalog slug, empty for none
	ParentID    string
}

// CreationFormCancelMsg is sent when form is cancelled
type CreationFormCancelMsg struct{}

// CreationFormPickTemplateMsg asks the view to let the user choose a template.
// The form cannot: the catalog is another screen, and which one lends itself is
// the router's to decide. The answer comes back through SetTemplate.
type CreationFormPickTemplateMsg struct{}

// Description character limit (GitLab limit is 250)
const descriptionCharLimit = 250

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
//
// visibilities is the closed set the Visibility field cycles through — the
// caller's to narrow (forge.Shape.VisibilitiesUnder), not this component's:
// it has no notion of a parent or of which forge it is talking to. If
// defaultVisibility is not among them, the field opens on the first entry —
// visibilities is ordered most-private-first, so that is always the safe
// side to land on.
func NewCreationForm(defaultResourceType int, parentName string, parentID string, defaultVisibility string, visibilities []string, v forge.Vocabulary) *CreationForm {
	nameInput := textinput.New()
	nameInput.Placeholder = "name"
	nameInput.CharLimit = 100
	nameInput.Width = 50
	theme.StyleTextInput(&nameInput)

	descInput := NewWrappedInput(descWrapWidth, descriptionCharLimit, "description (optional)")

	visIdx := 0
	for i, visibility := range visibilities {
		if visibility == defaultVisibility {
			visIdx = i
			break
		}
	}

	return &CreationForm{
		resourceType:   defaultResourceType,
		resourceLabels: resourceTypeLabels(v),
		formType:       formTypeFromResourceType(defaultResourceType),
		parentName:     parentName,
		parentID:       parentID,
		nameInput:      nameInput,
		descInput:      descInput,
		visibility:     visIdx,
		visibilities:   visibilities,
		focusedField:   0, // start on Type
	}
}

// SetTemplate records the template the user chose. An empty slug is none.
func (f *CreationForm) SetTemplate(slug, name string) {
	f.template = slug
	f.templateName = name
}

// SetVisibilityNote sets a dim explanation displayed next to the Visibility
// field — why the choices are narrower than the forge's own list, when the
// caller has narrowed them (Shape.VisibilitiesUnder). Empty renders nothing:
// the common case, a root-level create, is not narrowed by anything.
func (f *CreationForm) SetVisibilityNote(msg string) {
	f.visibilityNote = msg
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
	if f.resourceType == 1 {
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
		f.focusedField = (f.focusedField + 1) % (max + 1)
		f.updateFocus()
		return f, nil

	case "up":
		f.focusedField = (f.focusedField - 1 + max + 1) % (max + 1)
		f.updateFocus()
		return f, nil

	case "enter":
		if f.focusedField == max { // Submit button
			return f.submit()
		}
		// On the template field enter opens the catalog rather than moving on:
		// the choice is made elsewhere, and the field only shows the result.
		if f.isOnTemplateField() {
			return f, func() tea.Msg { return CreationFormPickTemplateMsg{} }
		}
		// On other fields, move to next
		f.focusedField = (f.focusedField + 1) % (max + 1)
		f.updateFocus()
		return f, nil

	case "backspace", "delete":
		// Back to none. On any other field these edit the text, so the reset is
		// only taken here.
		if f.isOnTemplateField() {
			f.SetTemplate("", "")
			return f, nil
		}

	case "left", "right":
		// Field 0: cycle resource type (Group ↔ Project)
		if f.focusedField == 0 {
			if msg.String() == "left" {
				f.resourceType = (f.resourceType - 1 + len(f.resourceLabels)) % len(f.resourceLabels)
			} else {
				f.resourceType = (f.resourceType + 1) % len(f.resourceLabels)
			}
			f.formType = formTypeFromResourceType(f.resourceType)
			// No focus clamp here: this branch is only reached with
			// focusedField == 0, and maxField() never drops below 4.
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

// isOnTemplateField returns true if the template field has focus
func (f *CreationForm) isOnTemplateField() bool {
	return f.focusedField == 4 && f.resourceType == 1
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
	base := "New " + f.resourceLabels[f.resourceType]
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
	if f.formType == FormTypeProject {
		template = f.template
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

	// Template (for projects only)
	if f.resourceType == 1 {
		b.WriteString(f.renderTemplateField())
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
		Render(f.resourceLabels[f.resourceType])
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
//
// The note, when set, explains why ←→ does not reach the forge's full list —
// without it, a single-entry field (a project under a private group) reads as
// a control that is stuck rather than one that has nothing else to offer.
func (f *CreationForm) renderVisibilityField() string {
	label := "Visibility " + theme.IconSelect + " "
	value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(f.visibilities[f.visibility])
	line := theme.Bg("  " + label + theme.IconChevronRight + " ")
	if f.focusedField == 3 {
		line = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + theme.IconChevronRight + " ")
	}
	line += value
	if f.visibilityNote != "" {
		line += theme.Bg("  ") + theme.DimStyle.Render(f.visibilityNote)
	}
	return line
}

// renderTemplateField renders the chosen template, or "none". It is a single
// line (Rule 113): the choice is made on another screen, so there is nothing to
// list here — the field shows what was picked and enter goes to pick.
func (f *CreationForm) renderTemplateField() string {
	label := "Template " + theme.IconSelect + " "
	if f.focusedField == 4 {
		label = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + theme.IconChevronRight + " ")
	} else {
		label = theme.Bg("  " + label + theme.IconChevronRight + " ")
	}
	if f.template == "" {
		return label + theme.DimStyle.Render("none")
	}
	return label + theme.PrimaryColorStyle.Render(f.templateName)
}
