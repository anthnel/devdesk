package ociresources

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Form fields, in the order they are rendered. Kind comes first because it is
// what decides whether the two group-only fields exist at all.
const (
	regFieldKind = iota
	regFieldURL
	regFieldSlug
	regFieldAlias
	regFieldUsername
	regFieldPassword
	regFieldAuth
	regFieldMgmtURL  // group only
	regFieldProvider // group only
	regFieldSubmit
)

const registryFormMaxField = regFieldSubmit

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
	slugInput     textinput.Model
	usernameInput textinput.Model
	passwordInput textinput.Model
	aliasInput    textinput.Model
	nexusURLInput textinput.Model
	authEnabled   bool
	kindIdx       int
	providerIdx   int
	// existing is every configured entry, so that a slug already in use can be
	// refused here rather than at the next load (§3.8, decision 1).
	existing     []config.RegistryItem
	focusedField int
	err          string
}

// NewRegistryForm creates a form for adding a new registry
func NewRegistryForm(existing []config.RegistryItem, width int) *RegistryForm {
	return newRegistryForm(-1, config.RegistryItem{AuthEnabled: true}, existing, width)
}

// NewRegistryEditForm creates a form for editing an existing registry
func NewRegistryEditForm(index int, item config.RegistryItem, existing []config.RegistryItem, width int) *RegistryForm {
	return newRegistryForm(index, item, existing, width)
}

func newRegistryForm(index int, item config.RegistryItem, existing []config.RegistryItem, width int) *RegistryForm {
	urlInput := textinput.New()
	urlInput.CharLimit = 256
	urlInput.Placeholder = "registry.example.com"
	theme.StyleTextInput(&urlInput)
	urlInput.SetValue(item.URL)
	urlInput.Focus()

	slugInput := textinput.New()
	slugInput.CharLimit = 64
	slugInput.Placeholder = "derived from the alias or the host  (optional)"
	theme.StyleTextInput(&slugInput)
	slugInput.SetValue(item.Slug)

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
		slugInput:     slugInput,
		usernameInput: usernameInput,
		passwordInput: passwordInput,
		aliasInput:    aliasInput,
		nexusURLInput: nexusURLInput,
		authEnabled:   item.AuthEnabled,
		kindIdx:       indexOf(config.Kinds(), item.Kind),
		providerIdx:   indexOf(config.Providers(), item.Provider),
		existing:      existing,
		focusedField:  regFieldURL,
	}
	f.resizeInputs()
	return f
}

// indexOf returns the position of value in options, or 0 when it is absent —
// an unset kind is a plain registry and an unset provider is generic, and both
// are first in their list.
func indexOf(options []string, value string) int {
	for i, o := range options {
		if o == value {
			return i
		}
	}
	return 0
}

// SetWidth updates the form width and resizes inputs accordingly.
func (f *RegistryForm) SetWidth(w int) {
	f.width = w
	f.resizeInputs()
}

func (f *RegistryForm) resizeInputs() {
	// Label overhead: viewport border (2) + indent (2) + longest label
	// "Management URL " + chevron + spaces (~18)
	const labelOverhead = 20
	w := max(f.width-labelOverhead, 20)
	f.urlInput.Width = w
	f.slugInput.Width = w
	f.usernameInput.Width = w
	f.passwordInput.Width = w
	f.aliasInput.Width = w
	f.nexusURLInput.Width = w
}

// isGroup reports whether the entry being edited is a repository-manager group.
func (f *RegistryForm) isGroup() bool {
	return config.Kinds()[f.kindIdx] == config.KindGroup
}

// isFieldSkipped reports whether a field is absent for the current kind. The
// management URL and the provider describe where a group's members come from,
// so they mean nothing on a plain registry.
func (f *RegistryForm) isFieldSkipped(idx int) bool {
	return !f.isGroup() && (idx == regFieldMgmtURL || idx == regFieldProvider)
}

func (f *RegistryForm) nextField() int {
	next := (f.focusedField + 1) % (registryFormMaxField + 1)
	for f.isFieldSkipped(next) {
		next = (next + 1) % (registryFormMaxField + 1)
	}
	return next
}

func (f *RegistryForm) prevField() int {
	prev := (f.focusedField - 1 + registryFormMaxField + 1) % (registryFormMaxField + 1)
	for f.isFieldSkipped(prev) {
		prev = (prev - 1 + registryFormMaxField + 1) % (registryFormMaxField + 1)
	}
	return prev
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
		f.focusedField = f.nextField()
		f.updateFocus()
		return f, nil
	case "up":
		f.focusedField = f.prevField()
		f.updateFocus()
		return f, nil
	case "left":
		return f.cycle(-1)
	case "right":
		return f.cycle(1)
	case "enter":
		if f.focusedField == regFieldSubmit {
			return f.submit()
		}
		f.focusedField = f.nextField()
		f.updateFocus()
		return f, nil
	}
	return f.updateActiveInput(msg)
}

// cycle moves a closed-list field by one step (Rule 132).
func (f *RegistryForm) cycle(step int) (*RegistryForm, tea.Cmd) {
	switch f.focusedField {
	case regFieldKind:
		f.kindIdx = wrap(f.kindIdx+step, len(config.Kinds()))
		// Leaving a group blurs the fields that just disappeared.
		f.updateFocus()
	case regFieldProvider:
		f.providerIdx = wrap(f.providerIdx+step, len(config.Providers()))
	case regFieldAuth:
		f.authEnabled = !f.authEnabled
	}
	return f, nil
}

// wrap returns i modulo n, for negative i as well.
func wrap(i, n int) int {
	return (i%n + n) % n
}

func (f *RegistryForm) updateFocus() {
	f.urlInput.Blur()
	f.slugInput.Blur()
	f.usernameInput.Blur()
	f.passwordInput.Blur()
	f.aliasInput.Blur()
	f.nexusURLInput.Blur()
	switch f.focusedField {
	case regFieldURL:
		f.urlInput.Focus()
	case regFieldSlug:
		f.slugInput.Focus()
	case regFieldAlias:
		f.aliasInput.Focus()
	case regFieldUsername:
		f.usernameInput.Focus()
	case regFieldPassword:
		f.passwordInput.Focus()
	case regFieldMgmtURL:
		f.nexusURLInput.Focus()
	}
}

func (f *RegistryForm) updateActiveInput(msg tea.Msg) (*RegistryForm, tea.Cmd) {
	var cmd tea.Cmd
	switch f.focusedField {
	case regFieldURL:
		f.urlInput, cmd = f.urlInput.Update(msg)
	case regFieldSlug:
		f.slugInput, cmd = f.slugInput.Update(msg)
	case regFieldAlias:
		f.aliasInput, cmd = f.aliasInput.Update(msg)
	case regFieldUsername:
		f.usernameInput, cmd = f.usernameInput.Update(msg)
	case regFieldPassword:
		f.passwordInput, cmd = f.passwordInput.Update(msg)
	case regFieldMgmtURL:
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
		Kind:        config.Kinds()[f.kindIdx],
		URL:         url,
		Username:    strings.TrimSpace(f.usernameInput.Value()),
		Alias:       strings.TrimSpace(f.aliasInput.Value()),
		AuthEnabled: f.authEnabled,
	}
	if f.isGroup() {
		item.Provider = config.Providers()[f.providerIdx]
		item.ManagementURL = strings.TrimSpace(f.nexusURLInput.Value())
	}

	slug, err := f.resolveSlug(item)
	if err != nil {
		f.err = err.Error()
		return f, nil
	}
	item.Slug = slug

	pass := f.passwordInput.Value()
	idx := f.index
	return f, func() tea.Msg {
		return RegistryFormSubmitMsg{Index: idx, Item: item, Password: pass}
	}
}

// resolveSlug settles the entry's slug: what was typed, if it is a slug and not
// already taken, or the one derived from the alias or the host. A typed value is
// refused rather than corrected — a slug is a link target, and quietly changing
// it is how a group loses its members.
func (f *RegistryForm) resolveSlug(item config.RegistryItem) (string, error) {
	typed := strings.TrimSpace(f.slugInput.Value())
	if typed == "" {
		return f.freeSlug(config.ProposeSlug(item)), nil
	}
	if config.Slugify(typed) != typed {
		return "", fmt.Errorf("slug %q: use lowercase letters, digits and dashes", typed)
	}
	if f.slugTaken(typed) {
		return "", fmt.Errorf("slug %q is already used by another registry", typed)
	}
	return typed, nil
}

// freeSlug returns base, or base-2, base-3… until one is not in use.
func (f *RegistryForm) freeSlug(base string) string {
	slug := base
	for n := 2; f.slugTaken(slug); n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}

// slugTaken reports whether another entry — not the one being edited — holds it.
func (f *RegistryForm) slugTaken(slug string) bool {
	for i, existing := range f.existing {
		if i != f.index && existing.Slug == slug {
			return true
		}
	}
	return false
}

// View renders the registry form (Rule 131: starts with empty line)
func (f *RegistryForm) View() string {
	var b strings.Builder

	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	if f.err != "" {
		b.WriteString(theme.StatusErrorStyle.Render("  " + f.err))
		b.WriteString("\n\n")
	}

	b.WriteString(f.renderCycleField("Kind", config.Kinds()[f.kindIdx], regFieldKind))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("URL", f.urlInput.View(), regFieldURL))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Slug", f.slugInput.View(), regFieldSlug))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Alias", f.aliasInput.View(), regFieldAlias))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Username", f.usernameInput.View(), regFieldUsername))
	b.WriteString("\n\n")
	b.WriteString(f.renderField("Password", f.passwordInput.View(), regFieldPassword))
	b.WriteString("\n\n")
	b.WriteString(f.renderCycleField("Auth Enabled", yesNo(f.authEnabled), regFieldAuth))
	b.WriteString("\n\n")
	if f.isGroup() {
		b.WriteString(f.renderField("Management URL", f.nexusURLInput.View(), regFieldMgmtURL))
		b.WriteString("\n\n")
		b.WriteString(f.renderCycleField("Provider", config.Providers()[f.providerIdx], regFieldProvider))
		b.WriteString("\n\n")
	}
	b.WriteString("  ")
	b.WriteString(theme.RenderButton("Save", f.focusedField == regFieldSubmit, "primary"))

	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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

// renderCycleField renders a closed-list field (Rule 132).
func (f *RegistryForm) renderCycleField(label, value string, fieldIdx int) string {
	selectLabel := label + " " + theme.IconSelect + " "
	if f.focusedField == fieldIdx {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+selectLabel+theme.IconChevronRight+" ") + theme.Bg(value)
	}
	return theme.Bg("  "+selectLabel+theme.IconChevronRight+" ") + theme.Bg(value)
}
