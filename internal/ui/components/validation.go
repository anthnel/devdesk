package components

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// FieldError is one field that failed validation. Key names the field in the
// form's own vocabulary; Text is the sentence shown in the footer.
type FieldError struct{ Key, Text string }

// Validation is what a form's submit found, kept live once a submit was
// refused (DEVDESK-292). Nothing is invalid before the first refused submit;
// after it every change revalidates the whole form. The zero value is ready
// and says nothing.
type Validation struct {
	attempted bool
	errs      []FieldError
}

// Submit records the errors a submit found, in display order, and reports
// whether the form may proceed. A refused submit arms revalidation; an
// accepted one clears every error.
func (v *Validation) Submit(errs []FieldError) bool {
	v.errs = append([]FieldError(nil), errs...)
	v.attempted = len(errs) > 0
	return len(errs) == 0
}

// Revalidate replaces the errors after a change. It does nothing until a
// submit has been refused, and stays live afterwards even when the form goes
// clean, so a later error shows at once.
func (v *Validation) Revalidate(errs []FieldError) {
	if !v.attempted {
		return
	}
	v.errs = errs
}

// Invalid reports whether the field named key currently fails validation.
func (v Validation) Invalid(key string) bool {
	for _, e := range v.errs {
		if e.Key == key {
			return true
		}
	}
	return false
}

// Status is the footer line for the current errors: the first one, followed by
// "(+N more)" when others remain. It is derived on every call, at LevelError,
// so it never expires like a timed FooterMessage. The zero Status means no
// error.
func (v Validation) Status() Status {
	if len(v.errs) == 0 {
		return Status{}
	}
	text := v.errs[0].Text
	if n := len(v.errs) - 1; n > 0 {
		text = fmt.Sprintf("%s (+%d more)", text, n)
	}
	return Status{Text: text, Level: LevelError}
}

// InvalidMarks remembers which text inputs currently carry the invalid style,
// so a form restyles an input only when its state changes. Restyling resets the
// input's cursor blink, so doing it on every message (the blink ticks included)
// would freeze the cursor. The zero value is ready; every input starts valid.
type InvalidMarks struct{ applied map[string]bool }

// Sync applies the invalid style to in when invalid differs from what was last
// applied under key, and reports whether it did. Call it from Update() only
// (Rule 110).
func (m *InvalidMarks) Sync(key string, in *textinput.Model, invalid bool) bool {
	if m.applied[key] == invalid {
		return false
	}
	if m.applied == nil {
		m.applied = map[string]bool{}
	}
	theme.SetTextInputInvalid(in, invalid)
	m.applied[key] = invalid
	return true
}
