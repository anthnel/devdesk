package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Field indices used by ComponentForm. Index 4 is the submit button for
// http/https/ssl and the type-specific input for icmp/dns, which is why the
// submit index is derived from the type rather than fixed.
const (
	fieldName    = 0
	fieldTarget  = 1
	fieldTimeout = 2
	fieldType    = 3
	fieldExtra   = 4
)

// feedForm applies messages in order, stopping early if the form closes itself
// (esc returns a nil form).
func feedForm(f *ComponentForm, msgs ...tea.Msg) *ComponentForm {
	for _, msg := range msgs {
		f, _ = f.Update(msg)
		if f == nil {
			return nil
		}
	}
	return f
}

// typeInto sends s one keypress at a time, as a textinput expects.
func typeInto(f *ComponentForm, s string) *ComponentForm {
	return feedForm(f, testutil.Type(s)...)
}

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewComponentFormDefaults(t *testing.T) {
	f := NewComponentForm(nil)

	if f.focusIndex != fieldName {
		t.Errorf("focusIndex = %d on a new form, want %d (Name)", f.focusIndex, fieldName)
	}
	if f.compType != "https" {
		t.Errorf("compType = %q on a new form, want \"https\"", f.compType)
	}
	if f.editing {
		t.Error("a form built from nil reports editing = true")
	}
	if f.original != nil {
		t.Error("a form built from nil carries an original component")
	}
	if !f.nameInput.Focused() {
		t.Error("the Name input is not focused on a new form")
	}
}

func TestNewComponentFormPrefillsFromComponent(t *testing.T) {
	original := &config.ComponentConfig{
		Name:       "dns-primary",
		Type:       "dns",
		Target:     "example.com",
		Timeout:    30,
		Count:      7,
		Nameserver: "8.8.8.8",
	}

	f := NewComponentForm(original)

	if !f.editing {
		t.Error("a form built from a component reports editing = false")
	}
	if f.original != original {
		t.Error("the form did not keep the original component for the submit message")
	}

	fields := []struct {
		name string
		got  string
		want string
	}{
		{"name", f.nameInput.Value(), "dns-primary"},
		{"target", f.targetInput.Value(), "example.com"},
		{"timeout", f.timeoutInput.Value(), "30"},
		{"count", f.countInput.Value(), "7"},
		{"nameserver", f.nsInput.Value(), "8.8.8.8"},
	}
	for _, tc := range fields {
		if tc.got != tc.want {
			t.Errorf("%s input = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if f.compType != "dns" {
		t.Errorf("compType = %q, want \"dns\"", f.compType)
	}
}

// Zero values are absent settings, not literal zeros: prefilling "0" would make
// the form save a zero timeout the user never asked for.
func TestNewComponentFormLeavesZeroValuesEmpty(t *testing.T) {
	f := NewComponentForm(&config.ComponentConfig{Name: "bare", Target: "example.com"})

	if f.timeoutInput.Value() != "" {
		t.Errorf("timeout input = %q for a zero Timeout, want empty", f.timeoutInput.Value())
	}
	if f.countInput.Value() != "" {
		t.Errorf("count input = %q for a zero Count, want empty", f.countInput.Value())
	}
	if f.nsInput.Value() != "" {
		t.Errorf("nameserver input = %q for an empty Nameserver, want empty", f.nsInput.Value())
	}
	// An empty Type must not blank the default.
	if f.compType != "https" {
		t.Errorf("compType = %q for an empty Type, want the \"https\" default", f.compType)
	}
}

func TestComponentFormGetTitle(t *testing.T) {
	if got := NewComponentForm(nil).GetTitle(); got != "Add New Monitor" {
		t.Errorf("GetTitle() = %q on a creation form, want \"Add New Monitor\"", got)
	}
	if got := NewComponentForm(&config.ComponentConfig{Name: "x"}).GetTitle(); got != "Edit Monitor" {
		t.Errorf("GetTitle() = %q on an edit form, want \"Edit Monitor\"", got)
	}
}

// ── Navigation ───────────────────────────────────────────────────────────────

// http/https/ssl have no type-specific input, so field 4 is the submit button;
// icmp and dns insert one, pushing submit to 5.
func TestComponentFormMaxFieldIndexDependsOnType(t *testing.T) {
	tests := []struct {
		compType string
		want     int
	}{
		{"http", 4},
		{"https", 4},
		{"ssl", 4},
		{"icmp", 5},
		{"dns", 5},
	}

	for _, tc := range tests {
		t.Run(tc.compType, func(t *testing.T) {
			f := NewComponentForm(&config.ComponentConfig{Type: tc.compType})
			if got := f.maxFieldIndex(); got != tc.want {
				t.Errorf("maxFieldIndex() = %d for %q, want %d", got, tc.compType, tc.want)
			}
		})
	}
}

func TestComponentFormVerticalNavigationWraps(t *testing.T) {
	t.Run("down wraps past the last field", func(t *testing.T) {
		f := NewComponentForm(nil) // https, max index 4

		for i := 0; i <= 4; i++ {
			f = feedForm(f, testutil.Key("down"))
		}
		if f.focusIndex != fieldName {
			t.Errorf("focusIndex = %d after wrapping past the submit button, want %d", f.focusIndex, fieldName)
		}
	})

	t.Run("up from the first field lands on the last", func(t *testing.T) {
		f := NewComponentForm(nil)

		f = feedForm(f, testutil.Key("up"))
		if f.focusIndex != 4 {
			t.Errorf("focusIndex = %d after up from Name, want 4 (submit)", f.focusIndex)
		}
	})

	t.Run("dns reaches its extra field", func(t *testing.T) {
		f := NewComponentForm(&config.ComponentConfig{Type: "dns"})

		f = feedForm(f, testutil.Key("up"))
		if f.focusIndex != 5 {
			t.Errorf("focusIndex = %d after up from Name on a dns form, want 5", f.focusIndex)
		}
	})
}

func TestComponentFormFocusFollowsNavigation(t *testing.T) {
	f := NewComponentForm(&config.ComponentConfig{Type: "icmp"})

	f = feedForm(f, testutil.Key("down"))
	if !f.targetInput.Focused() || f.nameInput.Focused() {
		t.Error("focus did not move from Name to Target")
	}

	f = feedForm(f, testutil.Key("down"))
	if !f.timeoutInput.Focused() {
		t.Error("focus did not move to Timeout")
	}

	// Field 3 is the type selector: no input should hold focus.
	f = feedForm(f, testutil.Key("down"))
	if f.nameInput.Focused() || f.targetInput.Focused() || f.timeoutInput.Focused() ||
		f.countInput.Focused() || f.nsInput.Focused() {
		t.Error("an input kept focus while the type selector was active")
	}

	f = feedForm(f, testutil.Key("down"))
	if !f.countInput.Focused() {
		t.Error("focus did not move to the ping count on an icmp form")
	}
}

func TestComponentFormTypingReachesTheFocusedInput(t *testing.T) {
	f := typeInto(NewComponentForm(nil), "api")
	if f.nameInput.Value() != "api" {
		t.Errorf("name input = %q after typing on field 0, want \"api\"", f.nameInput.Value())
	}

	f = feedForm(f, testutil.Key("down"))
	f = typeInto(f, "example.com")
	if f.targetInput.Value() != "example.com" {
		t.Errorf("target input = %q after typing on field 1, want \"example.com\"", f.targetInput.Value())
	}
	if f.nameInput.Value() != "api" {
		t.Errorf("name input = %q after typing into Target, want it unchanged", f.nameInput.Value())
	}
}

// ── Type selector (Rule 132) ─────────────────────────────────────────────────

func TestComponentFormCyclesTypeOnFieldThreeOnly(t *testing.T) {
	t.Run("right advances through the list", func(t *testing.T) {
		f := NewComponentForm(nil) // https
		f.focusIndex = fieldType

		want := []string{"icmp", "dns", "ssl", "http", "https"}
		for _, expected := range want {
			f = feedForm(f, testutil.Key("right"))
			if f.compType != expected {
				t.Fatalf("compType = %q after right, want %q", f.compType, expected)
			}
		}
	})

	t.Run("left walks back and wraps", func(t *testing.T) {
		f := NewComponentForm(&config.ComponentConfig{Type: "http"})
		f.focusIndex = fieldType

		f = feedForm(f, testutil.Key("left"))
		if f.compType != "ssl" {
			t.Errorf("compType = %q after left from http, want \"ssl\" (wrapped)", f.compType)
		}
	})

	t.Run("horizontal keys are inert on other fields", func(t *testing.T) {
		f := NewComponentForm(nil)
		f.focusIndex = fieldTarget

		f = feedForm(f, testutil.Key("right"), testutil.Key("left"))
		if f.compType != "https" {
			t.Errorf("compType = %q after left/right on Target, want it unchanged", f.compType)
		}
	})
}

func TestComponentFormCycleTypeUpdatesTargetPlaceholder(t *testing.T) {
	f := NewComponentForm(nil)
	f.focusIndex = fieldType

	f = feedForm(f, testutil.Key("right")) // icmp
	if !strings.Contains(f.targetInput.Placeholder, "192.168.1.1") {
		t.Errorf("icmp placeholder = %q, want it to suggest an address", f.targetInput.Placeholder)
	}

	f = feedForm(f, testutil.Key("right"), testutil.Key("right")) // dns then ssl
	if !strings.Contains(f.targetInput.Placeholder, ":443") {
		t.Errorf("ssl placeholder = %q, want it to suggest a port", f.targetInput.Placeholder)
	}
}

// ── Submission ───────────────────────────────────────────────────────────────

// fillValid puts the form one key away from a valid submission.
func fillValid(t *testing.T, compType string) *ComponentForm {
	t.Helper()
	f := NewComponentForm(&config.ComponentConfig{Type: compType})
	f = typeInto(f, "api")
	f = feedForm(f, testutil.Key("down"))
	f = typeInto(f, "example.com")
	return f
}

func TestComponentFormEnterSubmitsOnlyFromTheLastField(t *testing.T) {
	f := fillValid(t, "https")
	f.focusIndex = fieldTimeout

	_, cmd := f.Update(testutil.Key("enter"))
	if cmd != nil {
		t.Errorf("enter on Timeout emitted a command, got %T", testutil.Msg(cmd))
	}

	f.focusIndex = f.maxFieldIndex()
	_, cmd = f.Update(testutil.Key("enter"))
	if _, ok := testutil.MsgOf[ComponentFormSubmitMsg](cmd); !ok {
		t.Errorf("enter on the submit button did not emit ComponentFormSubmitMsg, got %T", testutil.Msg(cmd))
	}
}

func TestComponentFormRejectsIncompleteSubmission(t *testing.T) {
	tests := []struct {
		name   string
		name_  string
		target string
	}{
		{"no name", "", "example.com"},
		{"no target", "api", ""},
		{"neither", "", ""},
		{"whitespace only", "   ", "   "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NewComponentForm(nil)
			f.nameInput.SetValue(tc.name_)
			f.targetInput.SetValue(tc.target)
			f.focusIndex = f.maxFieldIndex()

			_, cmd := f.Update(testutil.Key("enter"))
			if cmd != nil {
				t.Errorf("the form submitted with name=%q target=%q", tc.name_, tc.target)
			}
		})
	}
}

func TestComponentFormSubmitTrimsAndAppliesDefaults(t *testing.T) {
	f := NewComponentForm(nil)
	f.nameInput.SetValue("  api  ")
	f.targetInput.SetValue("  example.com  ")
	f.focusIndex = f.maxFieldIndex()

	_, cmd := f.Update(testutil.Key("enter"))
	msg, ok := testutil.MsgOf[ComponentFormSubmitMsg](cmd)
	if !ok {
		t.Fatalf("submit did not emit ComponentFormSubmitMsg, got %T", testutil.Msg(cmd))
	}

	if msg.Component.Name != "api" || msg.Component.Target != "example.com" {
		t.Errorf("submitted name=%q target=%q, want them trimmed", msg.Component.Name, msg.Component.Target)
	}
	if msg.Component.Timeout != 10 {
		t.Errorf("Timeout = %d with an empty input, want the default 10", msg.Component.Timeout)
	}
	if msg.Original != nil {
		t.Error("a creation form submitted a non-nil Original")
	}
}

func TestComponentFormSubmitParsesNumericInputs(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		count       string
		wantTimeout int
		wantCount   int
	}{
		{"parsed", "45", "9", 45, 9},
		{"non-numeric falls back", "abc", "xyz", 10, 4},
		{"empty falls back", "", "", 10, 4},
		{"whitespace falls back", "  ", "  ", 10, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := fillValid(t, "icmp")
			f.timeoutInput.SetValue(tc.timeout)
			f.countInput.SetValue(tc.count)
			f.focusIndex = f.maxFieldIndex()

			_, cmd := f.Update(testutil.Key("enter"))
			msg, ok := testutil.MsgOf[ComponentFormSubmitMsg](cmd)
			if !ok {
				t.Fatalf("submit did not emit ComponentFormSubmitMsg, got %T", testutil.Msg(cmd))
			}
			if msg.Component.Timeout != tc.wantTimeout {
				t.Errorf("Timeout = %d, want %d", msg.Component.Timeout, tc.wantTimeout)
			}
			if msg.Component.Count != tc.wantCount {
				t.Errorf("Count = %d, want %d", msg.Component.Count, tc.wantCount)
			}
		})
	}
}

// Type-specific fields must not leak onto types that do not use them: a stray
// Count on an https monitor would be written to the config file.
func TestComponentFormSubmitCarriesOnlyTypeRelevantFields(t *testing.T) {
	tests := []struct {
		compType       string
		wantCount      int
		wantNameserver string
	}{
		{"https", 0, ""},
		{"http", 0, ""},
		{"ssl", 0, ""},
		{"icmp", 4, ""},
		{"dns", 0, "8.8.8.8"},
	}

	for _, tc := range tests {
		t.Run(tc.compType, func(t *testing.T) {
			f := fillValid(t, tc.compType)
			f.nsInput.SetValue("8.8.8.8")
			f.focusIndex = f.maxFieldIndex()

			_, cmd := f.Update(testutil.Key("enter"))
			msg, ok := testutil.MsgOf[ComponentFormSubmitMsg](cmd)
			if !ok {
				t.Fatalf("submit did not emit ComponentFormSubmitMsg, got %T", testutil.Msg(cmd))
			}
			if msg.Component.Type != tc.compType {
				t.Errorf("Type = %q, want %q", msg.Component.Type, tc.compType)
			}
			if msg.Component.Count != tc.wantCount {
				t.Errorf("Count = %d for %q, want %d", msg.Component.Count, tc.compType, tc.wantCount)
			}
			if msg.Component.Nameserver != tc.wantNameserver {
				t.Errorf("Nameserver = %q for %q, want %q", msg.Component.Nameserver, tc.compType, tc.wantNameserver)
			}
		})
	}
}

// The status view matches the original by name+type+target to find the entry to
// replace, so the submit message must carry the pointer it was built with.
func TestComponentFormSubmitCarriesTheOriginalWhenEditing(t *testing.T) {
	original := &config.ComponentConfig{Name: "api", Type: "https", Target: "example.com"}
	f := NewComponentForm(original)
	f.focusIndex = f.maxFieldIndex()

	_, cmd := f.Update(testutil.Key("enter"))
	msg, ok := testutil.MsgOf[ComponentFormSubmitMsg](cmd)
	if !ok {
		t.Fatalf("submit did not emit ComponentFormSubmitMsg, got %T", testutil.Msg(cmd))
	}
	if msg.Original != original {
		t.Error("the submit message dropped the original component")
	}
}

// ── Cancellation and layout ──────────────────────────────────────────────────

// esc closes the form by returning nil rather than emitting a message; the
// caller stores the result, so a nil form is how the view learns to stop
// rendering it.
func TestComponentFormEscClosesTheForm(t *testing.T) {
	f, cmd := NewComponentForm(nil).Update(testutil.Key("esc"))

	if f != nil {
		t.Error("esc returned a live form")
	}
	if cmd != nil {
		t.Errorf("esc emitted a command, got %T", testutil.Msg(cmd))
	}
}

func TestComponentFormStoresWindowSize(t *testing.T) {
	f := feedForm(NewComponentForm(nil), testutil.Resize(120, 40))

	if f.width != 120 || f.height != 40 {
		t.Errorf("window size = %dx%d, want 120x40", f.width, f.height)
	}
}

func TestComponentFormViewRendersEveryType(t *testing.T) {
	for _, compType := range []string{"http", "https", "icmp", "dns", "ssl"} {
		t.Run(compType, func(t *testing.T) {
			f := feedForm(NewComponentForm(&config.ComponentConfig{Type: compType}), testutil.Resize(100, 30))

			out := f.View()
			for _, want := range []string{"Name", "Target", "Timeout", "Type", "Save Monitor"} {
				if !strings.Contains(out, want) {
					t.Errorf("View() for %q is missing %q", compType, want)
				}
			}
		})
	}
}

func TestComponentFormViewShowsTypeSpecificField(t *testing.T) {
	tests := []struct {
		compType string
		want     string
		absent   string
	}{
		{"icmp", "Ping count", "Nameserver"},
		{"dns", "Nameserver", "Ping count"},
	}

	for _, tc := range tests {
		t.Run(tc.compType, func(t *testing.T) {
			f := feedForm(NewComponentForm(&config.ComponentConfig{Type: tc.compType}), testutil.Resize(100, 30))

			out := f.View()
			if !strings.Contains(out, tc.want) {
				t.Errorf("View() for %q is missing %q", tc.compType, tc.want)
			}
			if strings.Contains(out, tc.absent) {
				t.Errorf("View() for %q shows %q, which belongs to the other type", tc.compType, tc.absent)
			}
		})
	}
}
