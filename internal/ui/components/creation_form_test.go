package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Field indices used by CreationForm.
const (
	fieldType       = 0
	fieldName       = 1
	fieldDesc       = 2
	fieldVisibility = 3
	fieldTemplate   = 4
)

func newGroupForm() *CreationForm {
	return NewCreationForm(0, "", "", "private", nil)
}

func newProjectForm(templates ...string) *CreationForm {
	return NewCreationForm(1, "parent/group", "42", "private", templates)
}

// feedForm applies messages in order.
func feedForm(f *CreationForm, msgs ...tea.Msg) *CreationForm {
	for _, m := range msgs {
		f, _ = f.Update(m)
	}
	return f
}

func TestNewCreationFormDefaults(t *testing.T) {
	f := newGroupForm()

	if f.focusedField != fieldType {
		t.Errorf("focusedField = %d on a new form, want %d (Type)", f.focusedField, fieldType)
	}
	if f.formType != FormTypeGroup {
		t.Errorf("formType = %v, want FormTypeGroup", f.formType)
	}
}

// The template list always gets a synthetic "none" entry at index 0, which is
// what makes templateIdx == 0 mean "no template" in submit().
func TestNewCreationFormPrependsNoneTemplate(t *testing.T) {
	f := newProjectForm("go", "python")

	if len(f.templates) != 3 {
		t.Fatalf("templates has %d entries, want 3 (none + 2)", len(f.templates))
	}
	if f.templates[0] != "none" {
		t.Errorf("templates[0] = %q, want %q", f.templates[0], "none")
	}
	if f.templateIdx != 0 {
		t.Errorf("templateIdx = %d, want 0", f.templateIdx)
	}
}

func TestNewCreationFormResolvesDefaultVisibility(t *testing.T) {
	tests := []struct {
		given string
		want  int
	}{
		{"private", 0},
		{"internal", 1},
		{"public", 2},
		{"nonsense", 0}, // unknown values fall back to the most restrictive
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.given, func(t *testing.T) {
			f := NewCreationForm(0, "", "", tt.given, nil)
			if f.visibility != tt.want {
				t.Errorf("visibility = %d for %q, want %d", f.visibility, tt.given, tt.want)
			}
		})
	}
}

// A group has no template field, so its submit button sits one index earlier
// than a project's.
func TestMaxFieldDependsOnResourceType(t *testing.T) {
	if got := newGroupForm().maxField(); got != 4 {
		t.Errorf("group maxField() = %d, want 4", got)
	}
	if got := newProjectForm("go").maxField(); got != 5 {
		t.Errorf("project maxField() = %d, want 5", got)
	}
	if got := newProjectForm().maxField(); got != 5 {
		t.Errorf("project with only the none template: maxField() = %d, want 5", got)
	}
}

func TestCreationFormEscapeCancels(t *testing.T) {
	f := newGroupForm()

	_, cmd := f.Update(testutil.Key("esc"))
	if _, ok := testutil.MsgOf[CreationFormCancelMsg](cmd); !ok {
		t.Errorf("esc did not cancel, got %T", testutil.Msg(cmd))
	}
}

func TestCreationFormArrowNavigationWraps(t *testing.T) {
	f := newGroupForm() // fields 0..4

	for want := 1; want <= 4; want++ {
		f, _ = f.Update(testutil.Key("down"))
		if f.focusedField != want {
			t.Fatalf("focusedField = %d after %d downs, want %d", f.focusedField, want, want)
		}
	}
	f, _ = f.Update(testutil.Key("down"))
	if f.focusedField != fieldType {
		t.Errorf("focusedField = %d after wrapping past the last field, want %d", f.focusedField, fieldType)
	}

	f, _ = f.Update(testutil.Key("up"))
	if f.focusedField != 4 {
		t.Errorf("focusedField = %d after up from the first field, want 4", f.focusedField)
	}
}

// j/k are letters, and a letter typed into a text input is text. This used to
// need a guard — isOnTextField() — because the form also answered j/k as
// navigation; a navigation key that has to ask whether you are typing is a key
// that should not be a letter, and §3.26 removed both the aliases and the guard.
func TestCreationFormLettersTypeIntoTextFields(t *testing.T) {
	f := newGroupForm()
	f = feedForm(f, testutil.Key("down")) // focus name

	f = feedForm(f, testutil.Key("j"), testutil.Key("k"))

	if f.focusedField != fieldName {
		t.Errorf("focusedField = %d after j/k on the name field, want %d", f.focusedField, fieldName)
	}
	if got := f.nameInput.Value(); got != "jk" {
		t.Errorf("nameInput = %q, want %q — j/k navigated instead of typing", got, "jk")
	}
}

func TestCreationFormVimKeysDoNotNavigate(t *testing.T) {
	for _, key := range []string{"j", "k"} {
		f := newGroupForm() // focus on Type, which is not a text input

		f = feedForm(f, testutil.Key(key))
		if f.focusedField != fieldType {
			t.Errorf("%q moved the focus to %d; ↑/↓ are the only field navigation (Rule 135)",
				key, f.focusedField)
		}
	}
}

func TestCreationFormCyclesResourceType(t *testing.T) {
	f := newGroupForm() // focus on Type

	f, _ = f.Update(testutil.Key("right"))
	if f.resourceType != 1 || f.formType != FormTypeProject {
		t.Errorf("right on Type gave resourceType=%d formType=%v, want 1/FormTypeProject", f.resourceType, f.formType)
	}

	f, _ = f.Update(testutil.Key("right"))
	if f.resourceType != 0 || f.formType != FormTypeGroup {
		t.Errorf("right wrapped to resourceType=%d formType=%v, want 0/FormTypeGroup", f.resourceType, f.formType)
	}

	f, _ = f.Update(testutil.Key("left"))
	if f.resourceType != 1 {
		t.Errorf("left on Type gave resourceType=%d, want 1", f.resourceType)
	}
}

// Cycling the type changes which fields exist, but the focus sits on the Type
// field itself and maxField() never drops below it, so focus cannot be stranded.
func TestCreationFormCyclingResourceTypeKeepsFocusOnType(t *testing.T) {
	f := newProjectForm("go") // maxField() == 5, template field present

	for _, key := range []string{"right", "left", "left"} {
		f, _ = f.Update(testutil.Key(key))
		if f.focusedField != fieldType {
			t.Fatalf("focusedField = %d after %q, want %d (Type)", f.focusedField, key, fieldType)
		}
		if f.focusedField > f.maxField() {
			t.Fatalf("focusedField = %d exceeds maxField() = %d after %q", f.focusedField, f.maxField(), key)
		}
	}
}

func TestCreationFormCyclesVisibility(t *testing.T) {
	f := newGroupForm()
	f.focusedField = fieldVisibility

	f, _ = f.Update(testutil.Key("right"))
	if f.visibilities[f.visibility] != "internal" {
		t.Errorf("visibility = %q after right, want internal", f.visibilities[f.visibility])
	}
	f, _ = f.Update(testutil.Key("right"))
	if f.visibilities[f.visibility] != "public" {
		t.Errorf("visibility = %q after two rights, want public", f.visibilities[f.visibility])
	}
	f, _ = f.Update(testutil.Key("right"))
	if f.visibilities[f.visibility] != "private" {
		t.Errorf("visibility = %q after wrapping, want private", f.visibilities[f.visibility])
	}
	f, _ = f.Update(testutil.Key("left"))
	if f.visibilities[f.visibility] != "public" {
		t.Errorf("visibility = %q after left from private, want public", f.visibilities[f.visibility])
	}
}

// Rule 132 reserves left/right for closed-set fields; on any other field they
// must do nothing rather than move focus.
func TestCreationFormHorizontalKeysInertElsewhere(t *testing.T) {
	f := newGroupForm()
	f.focusedField = fieldName

	f, _ = f.Update(testutil.Key("right"))

	if f.focusedField != fieldName {
		t.Errorf("focusedField = %d after right on the name field, want it unchanged", f.focusedField)
	}
}

func TestCreationFormEnterAdvancesThenSubmits(t *testing.T) {
	f := newGroupForm()
	f = feedForm(f, testutil.Key("down"))
	f = feedForm(f, testutil.Type("my-group")...)

	f, cmd := f.Update(testutil.Key("enter")) // on name -> advances
	if f.focusedField != fieldDesc {
		t.Errorf("focusedField = %d after enter on name, want %d", f.focusedField, fieldDesc)
	}
	if cmd != nil {
		t.Error("enter on a non-submit field produced a command")
	}

	f.focusedField = f.maxField()
	_, cmd = f.Update(testutil.Key("enter"))
	if _, ok := testutil.MsgOf[CreationFormSubmitMsg](cmd); !ok {
		t.Errorf("enter on the submit button did not submit, got %T", testutil.Msg(cmd))
	}
}

func TestCreationFormSubmitRequiresName(t *testing.T) {
	f := newGroupForm()
	f.focusedField = f.maxField()

	f, cmd := f.Update(testutil.Key("enter"))

	if cmd != nil {
		t.Error("submitting without a name produced a command")
	}
	if f.err != "Name is required" {
		t.Errorf("err = %q, want %q", f.err, "Name is required")
	}
}

func TestCreationFormSubmitRejectsWhitespaceOnlyName(t *testing.T) {
	f := newGroupForm()
	f = feedForm(f, testutil.Key("down"))
	f = feedForm(f, testutil.Type("   ")...)
	f.focusedField = f.maxField()

	f, cmd := f.Update(testutil.Key("enter"))

	if cmd != nil {
		t.Error("a whitespace-only name was accepted")
	}
	if f.err == "" {
		t.Error("a whitespace-only name produced no error message")
	}
}

func TestCreationFormSubmitPayload(t *testing.T) {
	f := newProjectForm("go", "python")
	f.focusedField = fieldName
	f.updateFocus()
	f = feedForm(f, testutil.Type("  my-project  ")...)
	f.visibility = 2 // public
	f.templateIdx = 2

	f.focusedField = f.maxField()
	_, cmd := f.Update(testutil.Key("enter"))

	msg, ok := testutil.MsgOf[CreationFormSubmitMsg](cmd)
	if !ok {
		t.Fatalf("submit produced %T, want CreationFormSubmitMsg", testutil.Msg(cmd))
	}
	if msg.Name != "my-project" {
		t.Errorf("Name = %q, want %q (trimmed)", msg.Name, "my-project")
	}
	if msg.FormType != FormTypeProject {
		t.Errorf("FormType = %v, want FormTypeProject", msg.FormType)
	}
	if msg.Visibility != "public" {
		t.Errorf("Visibility = %q, want public", msg.Visibility)
	}
	if msg.Template != "python" {
		t.Errorf("Template = %q, want python", msg.Template)
	}
	if msg.ParentID != "42" {
		t.Errorf("ParentID = %q, want 42", msg.ParentID)
	}
}

// templateIdx 0 is the synthetic "none" entry and must submit as an empty
// template, not the literal string "none".
func TestCreationFormNoneTemplateSubmitsEmpty(t *testing.T) {
	f := newProjectForm("go")
	f.focusedField = fieldName
	f.updateFocus()
	f = feedForm(f, testutil.Type("proj")...)
	f.templateIdx = 0

	f.focusedField = f.maxField()
	_, cmd := f.Update(testutil.Key("enter"))

	msg, _ := testutil.MsgOf[CreationFormSubmitMsg](cmd)
	if msg.Template != "" {
		t.Errorf("Template = %q for the none entry, want empty", msg.Template)
	}
}

func TestCreationFormGroupNeverSubmitsATemplate(t *testing.T) {
	f := NewCreationForm(0, "", "7", "private", []string{"go"})
	f.focusedField = fieldName
	f.updateFocus()
	f = feedForm(f, testutil.Type("grp")...)
	f.templateIdx = 1

	f.focusedField = f.maxField()
	_, cmd := f.Update(testutil.Key("enter"))

	msg, _ := testutil.MsgOf[CreationFormSubmitMsg](cmd)
	if msg.Template != "" {
		t.Errorf("Template = %q on a group, want empty", msg.Template)
	}
}

func TestCreationFormTemplateNavigation(t *testing.T) {
	f := newProjectForm("a", "b", "c")
	f.focusedField = fieldTemplate

	f, _ = f.Update(testutil.Key("down"))
	if f.templateIdx != 1 {
		t.Errorf("templateIdx = %d after down, want 1", f.templateIdx)
	}
	if f.focusedField != fieldTemplate {
		t.Error("down moved focus off the template field instead of changing the selection")
	}

	f, _ = f.Update(testutil.Key("up"))
	if f.templateIdx != 0 {
		t.Errorf("templateIdx = %d after up, want 0", f.templateIdx)
	}
}

// At the ends of the template list the arrow keys fall through to normal field
// navigation, which is how the user escapes the dropdown.
func TestCreationFormTemplateNavigationFallsThroughAtEnds(t *testing.T) {
	f := newProjectForm("a")
	f.focusedField = fieldTemplate
	f.templateIdx = len(f.templates) - 1

	f, _ = f.Update(testutil.Key("down"))

	if f.focusedField == fieldTemplate {
		t.Error("down at the end of the template list did not move focus onward")
	}
}

func TestAdjustTemplateScrollKeepsSelectionVisible(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = string(rune('a' + i))
	}
	f := newProjectForm(names...)
	f.focusedField = fieldTemplate

	for i := 0; i < 12; i++ {
		f, _ = f.Update(testutil.Key("down"))
	}

	if f.templateIdx < f.templateScroll || f.templateIdx >= f.templateScroll+maxVisibleTemplates {
		t.Errorf("selection %d is outside the visible window [%d, %d)",
			f.templateIdx, f.templateScroll, f.templateScroll+maxVisibleTemplates)
	}

	for i := 0; i < 12; i++ {
		f, _ = f.Update(testutil.Key("up"))
	}
	if f.templateScroll != 0 {
		t.Errorf("templateScroll = %d after scrolling back to the top, want 0", f.templateScroll)
	}
}

func TestCreationFormFocusMovesTextInputFocus(t *testing.T) {
	f := newGroupForm()

	f = feedForm(f, testutil.Key("down")) // name
	if !f.nameInput.Focused() {
		t.Error("name input is not focused while the name field is selected")
	}

	f = feedForm(f, testutil.Key("down")) // description
	if f.nameInput.Focused() {
		t.Error("name input kept focus after moving to the description")
	}
	if !f.descInput.focused {
		t.Error("description input is not focused while the description field is selected")
	}
}

func TestCreationFormWindowSizePropagatesToDescription(t *testing.T) {
	f := newGroupForm()

	f, _ = f.Update(testutil.Resize(140, 40))

	if f.width != 140 || f.height != 40 {
		t.Errorf("form size = %dx%d, want 140x40", f.width, f.height)
	}
	if f.descInput.displayWidth != 140 {
		t.Errorf("description displayWidth = %d, want 140", f.descInput.displayWidth)
	}
}

func TestCreationFormGetTitle(t *testing.T) {
	if got := newGroupForm().GetTitle(); got != "New Group" {
		t.Errorf("GetTitle() = %q, want %q", got, "New Group")
	}

	title := newProjectForm().GetTitle()
	if !strings.Contains(title, "New Project") || !strings.Contains(title, "parent/group") {
		t.Errorf("GetTitle() = %q, want it to mention both the type and the parent", title)
	}
}

// The form always blocks command mode while open (Rule: FormView).
func TestCreationFormAlwaysInEditMode(t *testing.T) {
	if !newGroupForm().InEditMode() {
		t.Error("InEditMode() = false; an open form must block command mode")
	}
}

func TestSetTemplateWarning(t *testing.T) {
	f := newProjectForm("go")
	f.SetTemplateWarning("Templates unavailable")

	if f.templateWarning != "Templates unavailable" {
		t.Errorf("templateWarning = %q, want %q", f.templateWarning, "Templates unavailable")
	}
}
