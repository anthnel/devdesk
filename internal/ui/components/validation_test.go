package components

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
)

var (
	errA = FieldError{Key: "a", Text: "A bad"}
	errB = FieldError{Key: "b", Text: "B bad"}
	errC = FieldError{Key: "c", Text: "C bad"}
)

func TestValidationZeroValueSaysNothing(t *testing.T) {
	var v Validation
	if v.Status() != (Status{}) {
		t.Errorf("zero Validation Status = %+v, want zero", v.Status())
	}
	if v.Invalid("x") {
		t.Error("zero Validation reports x invalid")
	}
}

// Nothing is validated before the first submit: no field is flagged while the
// user is still typing the form for the first time.
func TestValidationRevalidateBeforeSubmitIsInert(t *testing.T) {
	var v Validation
	v.Revalidate([]FieldError{errA})
	if v.Invalid("a") || v.Status() != (Status{}) {
		t.Errorf("Revalidate before Submit flagged a field: invalid=%v status=%+v", v.Invalid("a"), v.Status())
	}
}

func TestValidationSubmit(t *testing.T) {
	tests := []struct {
		name        string
		errs        []FieldError
		wantOK      bool
		wantText    string
		wantInvalid []string
		wantValid   []string
	}{
		{"no errors", nil, true, "", nil, []string{"a", "b"}},
		{"one error", []FieldError{errA}, false, "A bad", []string{"a"}, []string{"b"}},
		{"two errors", []FieldError{errA, errB}, false, "A bad (+1 more)", []string{"a", "b"}, nil},
		{"three errors", []FieldError{errA, errB, errC}, false, "A bad (+2 more)", []string{"a", "b", "c"}, nil},
		{"first in display order wins", []FieldError{errB, errA}, false, "B bad (+1 more)", []string{"a", "b"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v Validation
			if got := v.Submit(tt.errs); got != tt.wantOK {
				t.Errorf("Submit() = %v, want %v", got, tt.wantOK)
			}
			st := v.Status()
			if st.Text != tt.wantText {
				t.Errorf("Status().Text = %q, want %q", st.Text, tt.wantText)
			}
			if tt.wantText != "" && st.Level != LevelError {
				t.Errorf("Status().Level = %v, want LevelError", st.Level)
			}
			for _, k := range tt.wantInvalid {
				if !v.Invalid(k) {
					t.Errorf("Invalid(%q) = false, want true", k)
				}
			}
			for _, k := range tt.wantValid {
				if v.Invalid(k) {
					t.Errorf("Invalid(%q) = true, want false", k)
				}
			}
		})
	}
}

// After a refused submit every change revalidates the whole form: a corrected
// field is no longer invalid at the next render, a newly broken one is.
func TestValidationRevalidatesAfterARefusedSubmit(t *testing.T) {
	var v Validation
	v.Submit([]FieldError{errA, errB})

	v.Revalidate([]FieldError{errB})
	if v.Invalid("a") {
		t.Error("a was corrected but is still invalid")
	}
	if !v.Invalid("b") {
		t.Error("b is still wrong but is not invalid")
	}
	if got := v.Status().Text; got != "B bad" {
		t.Errorf("Status().Text = %q, want %q (no suffix once one error is left)", got, "B bad")
	}

	v.Revalidate([]FieldError{errC})
	if !v.Invalid("c") || v.Invalid("b") {
		t.Errorf("revalidation did not follow the new errors: c=%v b=%v", v.Invalid("c"), v.Invalid("b"))
	}

	v.Revalidate(nil)
	if v.Invalid("c") || v.Status() != (Status{}) {
		t.Errorf("Revalidate(nil) left state: invalid=%v status=%+v", v.Invalid("c"), v.Status())
	}
}

// A form that revalidates to clean stays attempted: a later error shows at once.
func TestValidationStaysLiveAfterItWentClean(t *testing.T) {
	var v Validation
	v.Submit([]FieldError{errA})
	v.Revalidate(nil)
	v.Revalidate([]FieldError{errA})
	if !v.Invalid("a") {
		t.Error("validation went inert after a clean revalidation")
	}
}

// A successful submit keeps nothing: a form refused earlier and then accepted
// has no error left.
func TestValidationSuccessfulSubmitClearsErrors(t *testing.T) {
	var v Validation
	v.Submit([]FieldError{errA})
	if !v.Submit(nil) {
		t.Fatal("Submit(nil) refused")
	}
	if v.Invalid("a") || v.Status() != (Status{}) {
		t.Error("errors survived an accepted submit")
	}
}

// The status is derived, not a timed message: it is recomputed on every call,
// is an Error, and the footer still shows it after the 3 s expiry has fired.
func TestValidationStatusIsDerivedAndNeverExpires(t *testing.T) {
	var v Validation
	v.Submit([]FieldError{errA})

	var f FooterMessage
	f.Error("some earlier failure")
	f.Handle(ClearFooterMsg{ID: f.ID()})
	if f.IsSet() {
		t.Fatal("test setup: the timed message should have expired")
	}

	out := ansi.Strip(f.View(80, v.Status()))
	if !strings.Contains(out, "A bad") {
		t.Fatalf("footer after expiry = %q, want it to still show %q", out, "A bad")
	}
	if got := v.Status(); got.Level != LevelError || got.Text != "A bad" {
		t.Errorf("Status() = %+v, want the same error on every call", got)
	}

	v.Revalidate([]FieldError{errB})
	out = ansi.Strip(f.View(80, v.Status()))
	if strings.Contains(out, "A bad") || !strings.Contains(out, "B bad") {
		t.Errorf("footer = %q, want it recomputed to %q", out, "B bad")
	}
}

func TestInvalidMarksRestylesOnlyOnAChange(t *testing.T) {
	var marks InvalidMarks
	in := textinput.New()
	marker := func() bool { return in.Styles().Focused.Placeholder.GetItalic() }
	setMarker := func() {
		s := in.Styles()
		s.Focused.Placeholder = s.Focused.Placeholder.Italic(true)
		in.SetStyles(s)
	}

	if marks.Sync("a", &in, false) {
		t.Error("a valid input that was never marked was restyled")
	}
	setMarker()
	marks.Sync("a", &in, false)
	if !marker() {
		t.Error("an unchanged state restyled the input")
	}
	if !marks.Sync("a", &in, true) || marker() {
		t.Error("becoming invalid did not restyle the input")
	}
	if marks.Sync("a", &in, true) {
		t.Error("an unchanged invalid state restyled the input")
	}
	if !marks.Sync("a", &in, false) {
		t.Error("becoming valid again did not restyle the input")
	}
	if !marks.Sync("b", &in, true) {
		t.Error("keys must be tracked independently")
	}
}
