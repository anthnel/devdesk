package explorer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-292 step 4b: a refused creation shows its message in the footer
// (FooterMessage level Error), not as an "Error: " line in the form body.

// refusedCreate opens the creation form, presses Create with an empty name
// (up from Type wraps to the button) and returns the model.
func refusedCreate(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))
	if m.mode != ModeCreatingProject || m.creationForm == nil {
		t.Fatalf("mode = %v, form = %v, want the creation form open", m.mode, m.creationForm)
	}
	return feed(t, m, testutil.Key("up"), testutil.Key("enter"))
}

func wantNameRequired(m Model) string {
	return "Name is required — type a name for the " + strings.ToLower(m.vocab().Namespace)
}

func TestCreationFormRefusalReachesTheExplorerFooter(t *testing.T) {
	m := refusedCreate(t)
	want := wantNameRequired(m)

	st := m.status()
	if st.Text != want || st.Level != components.LevelError {
		t.Errorf("status() = %+v, want %q at LevelError", st, want)
	}
	if footer := ansi.Strip(m.RenderFooter(160)); !strings.Contains(footer, want) {
		t.Errorf("the footer does not carry the message:\n%s", footer)
	}
}

func TestCreationFormRefusalIsNotRepeatedInTheBody(t *testing.T) {
	m := refusedCreate(t)
	if body := ansi.Strip(m.Render()); strings.Contains(body, "Error: ") {
		t.Errorf("the form body still says Error:\n%s", body)
	}
}

func TestCreationFormFooterIsEmptyBeforeASubmit(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))
	if footer := ansi.Strip(m.RenderFooter(160)); strings.Contains(footer, "is required") {
		t.Errorf("an untouched form already has a footer message:\n%s", footer)
	}
	if strings.Contains(m.creationForm.View(), "4:3") {
		t.Error("an untouched form underlines a field")
	}
}

func TestCreationFormFooterFollowsTheCorrection(t *testing.T) {
	m := refusedCreate(t)
	m = feed(t, m, testutil.Key("down"), testutil.Key("down")) // Type, then Name
	m = feed(t, m, testutil.Type("svc")...)

	if footer := ansi.Strip(m.RenderFooter(160)); strings.Contains(footer, "is required") {
		t.Errorf("the message outlives the correction:\n%s", footer)
	}
	if st := m.status(); st.Level == components.LevelError && strings.Contains(st.Text, "required") {
		t.Errorf("status() = %+v after a correction", st)
	}
}

// The footer line is derived at every render, so it never expires the way a
// timed FooterMessage does.
func TestCreationFormFooterMessageStaysWhileTheFieldIsInvalid(t *testing.T) {
	m := refusedCreate(t)
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if footer := ansi.Strip(m.RenderFooter(160)); !strings.Contains(footer, wantNameRequired(m)) {
		t.Errorf("the message vanished while the field is still invalid:\n%s", footer)
	}
}
