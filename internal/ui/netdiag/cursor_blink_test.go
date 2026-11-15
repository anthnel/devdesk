package netdiag

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-485: textinput.Focus() returns the Cmd that starts the cursor blink
// chain. Moving focus with up/down and coming back from the trace sub-level
// with esc must hand that Cmd to the runtime, or the cursor stays fixed. The
// Cmd is only checked for nil: a blink Cmd may wait on a tick, so it is never run.

func isTextField(field int) bool {
	return field == fieldTarget || field == fieldPort || field == fieldDNSServer
}

func TestFormVerticalMoveReturnsBlinkCmdForTextInputs(t *testing.T) {
	tests := []struct {
		name  string
		from  int
		key   string
		moves int // field the key lands on
	}{
		{"down Target to Port", fieldTarget, "down", fieldPort},
		{"down Port to DNS Server", fieldPort, "down", fieldDNSServer},
		{"down DNS Server to Run button", fieldDNSServer, "down", fieldRunButton},
		{"down Run button to Trace Path button", fieldRunButton, "down", fieldTraceButton},
		{"down Trace Path button to Target", fieldTraceButton, "down", fieldTarget},
		{"up Port to Target", fieldPort, "up", fieldTarget},
		{"up DNS Server to Port", fieldDNSServer, "up", fieldPort},
		{"up Run button to DNS Server", fieldRunButton, "up", fieldDNSServer},
		{"up Target to Trace Path button", fieldTarget, "up", fieldTraceButton},
		{"up Trace Path button to Run button", fieldTraceButton, "up", fieldRunButton},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := traceFormModel(t, true)
			m.state = StateInput
			m.focusedField = tt.from
			m, cmd := step(t, m, testutil.Key(tt.key))
			if m.focusedField != tt.moves {
				t.Fatalf("focusedField = %d, want %d", m.focusedField, tt.moves)
			}
			if isTextField(tt.moves) && cmd == nil {
				t.Errorf("landing on text field %d returned a nil Cmd: the cursor blink never starts", tt.moves)
			}
			if !isTextField(tt.moves) && cmd != nil {
				t.Errorf("landing on button %d returned a non-nil Cmd, no input is focused", tt.moves)
			}
		})
	}
}

func TestEscFromTraceReturnsBlinkCmdForTextInputs(t *testing.T) {
	tests := []struct {
		name  string
		field int
	}{
		{"Target", fieldTarget},
		{"Port", fieldPort},
		{"DNS Server", fieldDNSServer},
		{"Run button", fieldRunButton},
		{"Trace Path button", fieldTraceButton},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := traceFormModel(t, true)
			m.activeTab = tabDiagnostics
			m.state = StateTrace
			m.traceOrigin = StateInput
			m.focusedField = tt.field
			m, cmd := step(t, m, testutil.Key("esc"))
			if m.state != StateInput {
				t.Fatalf("state = %v, want StateInput", m.state)
			}
			if isTextField(tt.field) && cmd == nil {
				t.Errorf("esc back onto text field %d returned a nil Cmd: the cursor blink never starts", tt.field)
			}
			if !isTextField(tt.field) && cmd != nil {
				t.Errorf("esc back onto button %d returned a non-nil Cmd, no input is focused", tt.field)
			}
		})
	}
}

// Leaving the results for the form (esc, or New) goes through resetToForm,
// which refocuses the Target input: its blink Cmd must reach the runtime too.
func TestResultsBackToFormReturnsBlinkCmd(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"esc", "esc"},
		{"New", keymap.New},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := traceFormModel(t, true)
			m.activeTab = tabDiagnostics
			m.state = StateResults
			m.OriginView = ""
			m.focusedField = fieldRunButton
			m, cmd := step(t, m, testutil.Key(tt.key))
			if m.state != StateInput || m.focusedField != fieldTarget {
				t.Fatalf("state = %v, focusedField = %d, want StateInput on Target", m.state, m.focusedField)
			}
			if cmd == nil {
				t.Error("back to the form returned a nil Cmd: the Target cursor blink never starts")
			}
		})
	}
}
