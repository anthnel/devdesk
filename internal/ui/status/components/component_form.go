package components

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ComponentForm est un formulaire pour ajouter/éditer un composant
type ComponentForm struct {
	// Inputs
	nameInput    textinput.Model
	targetInput  textinput.Model
	timeoutInput textinput.Model
	countInput   textinput.Model // Pour ICMP
	nsInput      textinput.Model // Pour DNS nameserver

	// État
	focusIndex int
	compType   string // http, https, icmp, dns
	editing    bool   // true si édition, false si création
	original   *config.ComponentConfig

	// Dimensions
	width  int
	height int
}

// NewComponentForm crée un nouveau formulaire de composant
func NewComponentForm(component *config.ComponentConfig) *ComponentForm {
	nameInput := textinput.New()
	nameInput.Placeholder = "my-service"
	nameInput.CharLimit = 50
	nameInput.Width = 40
	nameInput.Focus()

	targetInput := textinput.New()
	targetInput.Placeholder = "example.com or 192.168.1.1"
	targetInput.CharLimit = 200
	targetInput.Width = 40

	timeoutInput := textinput.New()
	timeoutInput.Placeholder = "10"
	timeoutInput.CharLimit = 3
	timeoutInput.Width = 10

	countInput := textinput.New()
	countInput.Placeholder = "4"
	countInput.CharLimit = 2
	countInput.Width = 10

	nsInput := textinput.New()
	nsInput.Placeholder = "8.8.8.8 (optional)"
	nsInput.CharLimit = 50
	nsInput.Width = 40

	// Apply theme background to all textinputs
	theme.StyleTextInput(&nameInput)
	theme.StyleTextInput(&targetInput)
	theme.StyleTextInput(&timeoutInput)
	theme.StyleTextInput(&countInput)
	theme.StyleTextInput(&nsInput)

	form := &ComponentForm{
		nameInput:    nameInput,
		targetInput:  targetInput,
		timeoutInput: timeoutInput,
		countInput:   countInput,
		nsInput:      nsInput,
		focusIndex:   0,
		compType:     "https",
		editing:      component != nil,
	}

	// Si édition, pré-remplir les champs
	if component != nil {
		form.original = component
		form.nameInput.SetValue(component.Name)
		form.targetInput.SetValue(component.Target)
		if component.Timeout > 0 {
			form.timeoutInput.SetValue(fmt.Sprintf("%d", component.Timeout))
		}
		if component.Count > 0 {
			form.countInput.SetValue(fmt.Sprintf("%d", component.Count))
		}
		if component.Nameserver != "" {
			form.nsInput.SetValue(component.Nameserver)
		}
		if component.Type != "" {
			form.compType = component.Type
		}
	}

	return form
}

// ComponentFormSubmitMsg est envoyé quand le formulaire est soumis
type ComponentFormSubmitMsg struct {
	Component config.ComponentConfig
	Original  *config.ComponentConfig // Non nil si édition
}

// Update met à jour le formulaire
func (f *ComponentForm) Update(msg tea.Msg) (*ComponentForm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.height = msg.Height
	case tea.KeyMsg:
		return f.handleKeyMsg(msg)
	}

	return f, f.updateActiveInput(msg)
}

// handleKeyMsg traite les entrées clavier du formulaire
func (f *ComponentForm) handleKeyMsg(msg tea.KeyMsg) (*ComponentForm, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "down":
		f.navigateField(1)
		return f, nil
	case "up":
		f.navigateField(-1)
		return f, nil
	case "left":
		if f.focusIndex == 3 {
			f.cycleType(-1)
			return f, nil
		}
	case "right":
		if f.focusIndex == 3 {
			f.cycleType(1)
			return f, nil
		}
	case "enter":
		return f.handleEnterKey()
	}
	return f, f.updateActiveInput(msg)
}

// maxFieldIndex returns the maximum field index based on component type
func (f *ComponentForm) maxFieldIndex() int {
	if f.compType == "http" || f.compType == "https" || f.compType == "ssl" {
		return 4
	}
	return 5
}

// navigateField moves focus by delta (+1 or -1) with wrapping
func (f *ComponentForm) navigateField(delta int) {
	maxIndex := f.maxFieldIndex()
	f.focusIndex += delta
	if f.focusIndex > maxIndex {
		f.focusIndex = 0
	}
	if f.focusIndex < 0 {
		f.focusIndex = maxIndex
	}
	f.updateFocus()
}

// handleEnterKey traite la touche Enter (submit ou avance au champ suivant)
func (f *ComponentForm) handleEnterKey() (*ComponentForm, tea.Cmd) {
	if f.focusIndex == f.maxFieldIndex() {
		return f.submitForm()
	}
	return f, nil
}

// submitForm valide et soumet le formulaire
func (f *ComponentForm) submitForm() (*ComponentForm, tea.Cmd) {
	name := strings.TrimSpace(f.nameInput.Value())
	target := strings.TrimSpace(f.targetInput.Value())
	if name == "" || target == "" {
		return f, nil
	}

	component := config.ComponentConfig{
		Name:    name,
		Type:    f.compType,
		Target:  target,
		Timeout: f.parseTimeout(),
	}

	if f.compType == "icmp" {
		component.Count = f.parseCount()
	}
	if f.compType == "dns" {
		component.Nameserver = strings.TrimSpace(f.nsInput.Value())
	}

	return f, func() tea.Msg {
		return ComponentFormSubmitMsg{
			Component: component,
			Original:  f.original,
		}
	}
}

// parseTimeout parses the timeout input value with default of 10
func (f *ComponentForm) parseTimeout() int {
	if t := strings.TrimSpace(f.timeoutInput.Value()); t != "" {
		if parsed, err := strconv.Atoi(t); err == nil {
			return parsed
		}
	}
	return 10
}

// parseCount parses the ping count input value with default of 4
func (f *ComponentForm) parseCount() int {
	if c := strings.TrimSpace(f.countInput.Value()); c != "" {
		if parsed, err := strconv.Atoi(c); err == nil {
			return parsed
		}
	}
	return 4
}

// updateActiveInput met à jour l'input qui a le focus
func (f *ComponentForm) updateActiveInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch f.focusIndex {
	case 0:
		f.nameInput, cmd = f.nameInput.Update(msg)
	case 1:
		f.targetInput, cmd = f.targetInput.Update(msg)
	case 2:
		f.timeoutInput, cmd = f.timeoutInput.Update(msg)
	case 4:
		switch f.compType {
		case "icmp":
			f.countInput, cmd = f.countInput.Update(msg)
		case "dns":
			f.nsInput, cmd = f.nsInput.Update(msg)
		}
	}
	return cmd
}

// updateFocus met à jour le focus des inputs
func (f *ComponentForm) updateFocus() {
	f.nameInput.Blur()
	f.targetInput.Blur()
	f.timeoutInput.Blur()
	f.countInput.Blur()
	f.nsInput.Blur()

	switch f.focusIndex {
	case 0:
		f.nameInput.Focus()
	case 1:
		f.targetInput.Focus()
	case 2:
		f.timeoutInput.Focus()
	case 4:
		switch f.compType {
		case "icmp":
			f.countInput.Focus()
		case "dns":
			f.nsInput.Focus()
		}
	}
}

// cycleType change le type de composant dans la direction donnée (+1 ou -1)
func (f *ComponentForm) cycleType(dir int) {
	types := []string{"http", "https", "icmp", "dns", "ssl"}
	n := len(types)
	for i, t := range types {
		if t == f.compType {
			f.compType = types[(i+dir+n)%n]
			f.updatePlaceholders()
			return
		}
	}
}

// updatePlaceholders updates placeholder text based on selected type
func (f *ComponentForm) updatePlaceholders() {
	switch f.compType {
	case "ssl":
		f.targetInput.Placeholder = "example.com:443 or example.com"
	case "http":
		f.targetInput.Placeholder = "example.com or 192.168.1.1"
	case "https":
		f.targetInput.Placeholder = "example.com or 192.168.1.1"
	case "icmp":
		f.targetInput.Placeholder = "192.168.1.1 or hostname"
	case "dns":
		f.targetInput.Placeholder = "example.com"
	}
}

// GetTitle returns the form title for use in the viewport breadcrumb
func (f *ComponentForm) GetTitle() string {
	if f.editing {
		return "Edit Monitor"
	}
	return "Add New Monitor"
}

// View affiche le formulaire
func (f *ComponentForm) View() string {
	var b strings.Builder

	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	// Champ Name
	if f.focusIndex == 0 {
		b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall+" Name "+theme.IconChevronRight) + "\n")
	} else {
		b.WriteString(theme.Bg("  Name "+theme.IconChevronRight) + "\n")
	}
	b.WriteString(theme.Bg("  ") + f.nameInput.View() + "\n\n")

	// Champ Target
	if f.focusIndex == 1 {
		b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall+" Target "+theme.IconChevronRight) + "\n")
	} else {
		b.WriteString(theme.Bg("  Target "+theme.IconChevronRight) + "\n")
	}
	b.WriteString(theme.Bg("  ") + f.targetInput.View() + "\n\n")

	// Champ Timeout
	if f.focusIndex == 2 {
		b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall+" Timeout (seconds) "+theme.IconChevronRight) + "\n")
	} else {
		b.WriteString(theme.Bg("  Timeout (seconds) "+theme.IconChevronRight) + "\n")
	}
	b.WriteString(theme.Bg("  ") + f.timeoutInput.View() + "\n\n")

	// Type selector — cycle field (Rule 132)
	typeLabel := "Type " + theme.IconSelect + " "
	typeValue := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(f.compType)
	if f.focusIndex == 3 {
		b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall+" "+typeLabel+theme.IconChevronRight+" ") + typeValue + "\n\n")
	} else {
		b.WriteString(theme.Bg("  "+typeLabel+theme.IconChevronRight+" ") + typeValue + "\n\n")
	}

	// Champs spécifiques au type
	switch f.compType {
	case "icmp":
		if f.focusIndex == 4 {
			b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall+" Ping count "+theme.IconChevronRight) + "\n")
		} else {
			b.WriteString(theme.Bg("  Ping count "+theme.IconChevronRight) + "\n")
		}
		b.WriteString(theme.Bg("  ") + f.countInput.View() + "\n\n")
	case "dns":
		if f.focusIndex == 4 {
			b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall+" Nameserver "+theme.IconChevronRight) + "\n")
		} else {
			b.WriteString(theme.Bg("  Nameserver "+theme.IconChevronRight) + "\n")
		}
		b.WriteString(theme.Bg("  ") + f.nsInput.View() + "\n\n")
	}

	submitIndex := 4
	if f.compType == "icmp" || f.compType == "dns" {
		submitIndex = 5
	}

	// Bouton Submit
	b.WriteString(theme.Bg(" ") + theme.RenderButton("Save Monitor", f.focusIndex == submitIndex, "primary"))

	return b.String()
}
