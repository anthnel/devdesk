package filebrowser

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// createForm is N: a kind and a name, created in the browsed directory. It is a
// form in the viewport (Rule 112), not a modal — the directory it writes into
// is the header's Path, and a modal would cover the listing it is adding to.

type formField int

const (
	fieldKind formField = iota
	fieldName
	fieldCreate
	fieldCancel
	fieldCount
)

// kinds is the closed set the Type field cycles through (Rule 132).
var kinds = []string{"directory", "file"}

const nameCharLimit = 255 // the common NAME_MAX

type createForm struct {
	focus   formField
	kindIdx int
	name    textinput.Model
	width   int
}

// createFormSubmitMsg and createFormCancelMsg are the form's two answers.
type createFormSubmitMsg struct {
	Name  string
	IsDir bool
}

type createFormCancelMsg struct{}

func newCreateForm(isDir bool, width int) *createForm {
	in := textinput.New()
	in.Placeholder = "name"
	in.CharLimit = nameCharLimit
	in.Width = 40
	theme.StyleTextInput(&in)
	f := &createForm{name: in, width: width, focus: fieldName}
	if !isDir {
		f.kindIdx = 1
	}
	f.name.Focus()
	return f
}

func (f *createForm) setWidth(w int) { f.width = w }

func (f *createForm) isDir() bool { return f.kindIdx == 0 }

// Update follows Rule 135: ↑↓ move between fields, ←→ cycle the Type, enter
// acts on a button or submits from the name, esc cancels.
func (f *createForm) Update(msg tea.KeyMsg) (*createForm, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return f, func() tea.Msg { return createFormCancelMsg{} }
	case "up":
		f.setFocus(max(f.focus-1, 0))
		return f, nil
	case "down":
		f.setFocus(min(f.focus+1, fieldCount-1))
		return f, nil
	case "left", "right":
		return f.cycle(msg.String())
	case "enter":
		return f, f.enter()
	case "tab", "shift+tab":
		return f, nil // no tabs here (Rule 135)
	}
	if f.focus == fieldName {
		var cmd tea.Cmd
		f.name, cmd = f.name.Update(msg)
		return f, cmd
	}
	return f, nil
}

// cycle turns the Type on its field; on the name it moves the caret, which is
// the textinput's own ←→.
func (f *createForm) cycle(key string) (*createForm, tea.Cmd) {
	switch f.focus {
	case fieldKind:
		step := 1
		if key == "left" {
			step = len(kinds) - 1
		}
		f.kindIdx = (f.kindIdx + step) % len(kinds)
	case fieldName:
		var cmd tea.Cmd
		f.name, cmd = f.name.Update(tea.KeyMsg{Type: keyTypeFor(key)})
		return f, cmd
	}
	return f, nil
}

func keyTypeFor(key string) tea.KeyType {
	if key == "left" {
		return tea.KeyLeft
	}
	return tea.KeyRight
}

func (f *createForm) enter() tea.Cmd {
	switch f.focus {
	case fieldCancel:
		return func() tea.Msg { return createFormCancelMsg{} }
	case fieldKind:
		f.setFocus(fieldName)
		return nil
	}
	name, isDir := f.name.Value(), f.isDir()
	return func() tea.Msg { return createFormSubmitMsg{Name: name, IsDir: isDir} }
}

func (f *createForm) setFocus(field formField) {
	f.focus = field
	if field == fieldName {
		f.name.Focus()
	} else {
		f.name.Blur()
	}
}

// View renders the form with Rule 131's single blank line on top.
func (f *createForm) View(dir string) string {
	lines := []string{
		theme.EmptyLineBg(f.width),
		theme.BgLine(theme.TitleStyle.Render("New entry in "+theme.ShortPath(dir, max(f.width-20, 10))), f.width),
		theme.EmptyLineBg(f.width),
		theme.PadWithBg(f.kindLine(), f.width),
		theme.PadWithBg(f.nameLine(), f.width),
		theme.EmptyLineBg(f.width),
		theme.PadWithBg(theme.Bg("  ")+
			theme.RenderButton("Create", f.focus == fieldCreate, "primary")+theme.Bg("  ")+
			theme.RenderButton("Cancel", f.focus == fieldCancel, "secondary"), f.width),
	}
	return strings.Join(lines, "\n")
}

// kindLine is Rule 132's cycle field.
func (f *createForm) kindLine() string {
	label := "Type " + theme.IconSelect + " "
	value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(kinds[f.kindIdx])
	if f.focus == fieldKind {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}

// nameLine pads its label to the Type's so the two values share a column.
func (f *createForm) nameLine() string {
	label := "Name" + strings.Repeat(" ", lipgloss.Width("Type "+theme.IconSelect)-lipgloss.Width("Name")) + " "
	if f.focus == fieldName {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + f.name.View()
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + f.name.View()
}
