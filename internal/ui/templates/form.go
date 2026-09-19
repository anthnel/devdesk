package templates

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// formField names one control of the form. The fields on screen depend on the
// source's kind, so focus is an index into fields(), not one of these.
type formField int

const (
	fieldName formField = iota
	fieldDescription
	fieldTags
	fieldKind
	fieldURL
	fieldPath
	fieldRef
	fieldSubmit
)

const (
	descriptionWrapWidth = 80
	descriptionCharLimit = 250
)

// entryForm creates or edits a catalog entry. It is shown in the viewport, not
// as a modal (Rule 112).
type entryForm struct {
	name, tags, url, path, ref textinput.Model
	description                sharedcomponents.WrappedInput

	kind  int // index into template.Kinds
	focus int // index into fields()

	// editing fixes the slug. Creating derives it from the name, so renaming a
	// template does not silently orphan whatever remembered its old slug.
	editing bool
	slug    string
	// taken reports whether a slug already names a declared entry.
	taken func(slug string) bool

	err   string
	width int
}

// newEntryForm opens the form empty, or prefilled from an entry to edit it.
func newEntryForm(entry *template.Entry, taken func(string) bool) *entryForm {
	f := &entryForm{
		name:        newInput("Spring Boot API", 100),
		tags:        newInput("java, spring-boot", 200),
		url:         newInput("", 300),
		path:        newInput("", 300),
		ref:         newInput("", 100),
		description: sharedcomponents.NewWrappedInput(descriptionWrapWidth, descriptionCharLimit, "description (optional)"),
		taken:       taken,
	}

	if entry != nil {
		f.editing = true
		f.slug = entry.Slug
		f.name.SetValue(entry.Name)
		f.description.SetValue(entry.Description)
		f.tags.SetValue(strings.Join(entry.Tags, ", "))
		f.url.SetValue(entry.Source.URL)
		f.path.SetValue(entry.Source.Path)
		f.ref.SetValue(entry.Source.Ref)
		for i, k := range template.Kinds {
			if k == entry.Source.Kind {
				f.kind = i
			}
		}
	}

	f.updateFocus()
	return f
}

func newInput(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	in.Width = 60
	theme.StyleTextInput(&in)
	return in
}

// SetWidth is called from the window-size handler.
func (f *entryForm) SetWidth(width int) {
	f.width = width
	f.description.SetDisplayWidth(width)
}

func (f *entryForm) currentKind() template.Kind { return template.Kinds[f.kind] }

// fields lists what is on screen, in order. A local template has no URL, so the
// field is absent rather than disabled: it is not that it cannot be filled, it
// is that it means nothing.
func (f *entryForm) fields() []formField {
	list := []formField{fieldName, fieldDescription, fieldTags, fieldKind}
	if f.currentKind() != template.KindLocal {
		list = append(list, fieldURL)
	}
	return append(list, fieldPath, fieldRef, fieldSubmit)
}

func (f *entryForm) focused() formField { return f.fields()[f.focus] }

// Cycling is only meaningful on the kind field; the header greys ←→ elsewhere.
func (f *entryForm) OnCycleField() bool { return f.focused() == fieldKind }

func (f *entryForm) updateFocus() {
	f.name.Blur()
	f.tags.Blur()
	f.url.Blur()
	f.path.Blur()
	f.ref.Blur()
	f.description.Blur()

	switch f.focused() {
	case fieldName:
		f.name.Focus()
	case fieldDescription:
		f.description.Focus()
	case fieldTags:
		f.tags.Focus()
	case fieldURL:
		f.url.Focus()
	case fieldPath:
		f.path.Focus()
	case fieldRef:
		f.ref.Focus()
	}
}

// InEditMode is always true while the form is open: `:` is a character in a URL.
func (f *entryForm) InEditMode() bool { return true }

// Update handles a key. Keys follow Rule 135: ↑↓ move between fields, ←→ cycle
// the kind, tab does nothing, enter advances or confirms.
func (f *entryForm) Update(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return func() tea.Msg { return FormCancelMsg{} }

	case "down":
		f.move(1)
		return nil

	case "up":
		f.move(-1)
		return nil

	case "tab", "shift+tab":
		return nil

	case "enter":
		if f.focused() == fieldSubmit {
			return f.submit()
		}
		f.move(1)
		return nil

	case "left", "right":
		if f.focused() == fieldKind {
			f.cycleKind(msg.String() == "right")
			return nil
		}
	}

	return f.forward(msg)
}

func (f *entryForm) move(delta int) {
	n := len(f.fields())
	f.focus = (f.focus + delta + n) % n
	f.updateFocus()
}

// cycleKind moves to the next or previous kind. The focus stays on the kind
// field, which sits above every field that changes.
func (f *entryForm) cycleKind(forward bool) {
	n := len(template.Kinds)
	if forward {
		f.kind = (f.kind + 1) % n
	} else {
		f.kind = (f.kind - 1 + n) % n
	}
	f.err = ""
}

func (f *entryForm) forward(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	switch f.focused() {
	case fieldName:
		f.name, cmd = f.name.Update(msg)
	case fieldDescription:
		f.description, cmd = f.description.Update(msg)
	case fieldTags:
		f.tags, cmd = f.tags.Update(msg)
	case fieldURL:
		f.url, cmd = f.url.Update(msg)
	case fieldPath:
		f.path, cmd = f.path.Update(msg)
	case fieldRef:
		f.ref, cmd = f.ref.Update(msg)
	}
	return cmd
}

// entry is what the form currently describes.
func (f *entryForm) entry() (template.Entry, error) {
	slug := f.slug
	if !f.editing {
		slug = slugify(f.name.Value())
	}

	src := template.Source{Kind: f.currentKind(), Path: strings.TrimSpace(f.path.Value()), Ref: strings.TrimSpace(f.ref.Value())}
	if src.Kind != template.KindLocal {
		src.URL = strings.TrimSpace(f.url.Value())
	}

	e := template.Entry{
		Slug:        slug,
		Name:        strings.TrimSpace(f.name.Value()),
		Description: strings.TrimSpace(f.description.Value()),
		Tags:        template.NormalizeTags(strings.Split(f.tags.Value(), ",")),
		Source:      src,
	}
	if slug == "" {
		return e, fmt.Errorf("the name needs a letter or a digit")
	}
	if !f.editing && f.taken != nil && f.taken(slug) {
		return e, fmt.Errorf("a template named %q already exists", slug)
	}
	return e, e.Validate()
}

// submit validates in place: an invalid entry keeps the form open with the
// reason, rather than sending a message the view would only turn back.
func (f *entryForm) submit() tea.Cmd {
	e, err := f.entry()
	if err != nil {
		f.err = err.Error()
		return nil
	}
	f.err = ""
	return func() tea.Msg { return FormSubmitMsg{Entry: e} }
}

// slugify makes the identifier of a new entry from its name.
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}

// GetTitle is the form's breadcrumb.
func (f *entryForm) GetTitle() string {
	if f.editing {
		return "Edit template"
	}
	return "New template"
}

// View renders the form: one blank line of top padding (Rule 131), then the
// fields with the focus marker (Rule 120), each followed by a blank line
// (Rule 113) — the separator every other form in the app uses between
// fields, single-line ones included (RegistryForm, ResourceCreateForm,
// ComponentForm, CreationForm), and the one this form was missing.
func (f *entryForm) View() string {
	var b strings.Builder
	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	if f.err != "" {
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError).Render("Error: "+f.err) + "\n\n")
	}

	labels := f.labels()
	for _, field := range f.fields() {
		switch field {
		case fieldName:
			b.WriteString(f.line(field, "Name", f.name.View()) + "\n\n")
		case fieldDescription:
			b.WriteString(f.descriptionBlock() + "\n\n")
		case fieldTags:
			b.WriteString(f.line(field, "Tags", f.tags.View()) + "\n\n")
		case fieldKind:
			b.WriteString(f.kindLine() + "\n\n")
		case fieldURL:
			b.WriteString(f.line(field, labels.url, f.url.View()) + "\n\n")
		case fieldPath:
			b.WriteString(f.line(field, labels.path, f.path.View()) + "\n\n")
		case fieldRef:
			b.WriteString(f.line(field, labels.ref, f.ref.View()) + "\n\n")
		case fieldSubmit:
			label := "Create"
			if f.editing {
				label = "Save"
			}
			b.WriteString("  " + theme.RenderButton(label, true, "primary"))
		}
	}
	return b.String()
}

// kindLabels names the three source fields for the kind on screen. The same
// three inputs serve every kind — a value typed before cycling the kind is kept
// — and only what they are called changes.
type kindLabels struct{ url, path, ref string }

func (f *entryForm) labels() kindLabels {
	switch f.currentKind() {
	case template.KindLocal:
		return kindLabels{path: "Directory", ref: "Branch, tag or SHA"}
	case template.KindOCI:
		return kindLabels{url: "Registry URL", path: "Repository", ref: "Tag"}
	default:
		return kindLabels{url: "Clone URL", path: "Subdirectory", ref: "Branch, tag or SHA"}
	}
}

// line renders a single-line field: label and value on the same line (Rule 113).
func (f *entryForm) line(field formField, label, value string) string {
	if f.focused() == field {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+" "+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+" "+theme.IconChevronRight+" ") + value
}

// kindLine is the ←→ cycle field (Rule 132).
func (f *entryForm) kindLine() string {
	label := "Source " + theme.IconSelect + " "
	value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(string(f.currentKind()))
	if f.focused() == fieldKind {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}

// descriptionBlock is the multi-line field: label, then the wrapped input.
func (f *entryForm) descriptionBlock() string {
	label := theme.Bg("  Description " + theme.IconChevronRight)
	if f.focused() == fieldDescription {
		label = theme.KeyStyle.Render(theme.IconCircleSmall + " Description " + theme.IconChevronRight)
	}
	return label + "\n" + f.description.View()
}
