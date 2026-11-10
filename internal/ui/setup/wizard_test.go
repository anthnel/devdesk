package setup

import "testing"

// The wizard runs as a standalone program: with main.go and Run() no longer
// passing tea.WithAltScreen(), the wizard asks for the alternate screen in its
// own View (DEVDESK-201 step C, Bubble Tea v2). Written against the v2 API; red
// as a compile error until View() returns tea.View.

func TestSetupWizardUsesTheAltScreen(t *testing.T) {
	m := New()

	v := m.View()

	if !v.AltScreen {
		t.Error("the wizard does not ask for the alternate screen")
	}
	if v.Content != m.Render() {
		t.Error("View().Content differs from Render()")
	}
}

func TestSetupWizardKeepsTheAltScreenOnEveryStage(t *testing.T) {
	for name, stage := range map[string]stage{
		"confirm overwrite":   stageConfirmOverwrite,
		"confirm set current": stageConfirmSetCurrent,
		"done":                stageDone,
	} {
		t.Run(name, func(t *testing.T) {
			m := New()
			m.stage = stage
			if stage != stageDone {
				m.confirmModal = newConfirmPrompt("Confirm", "Proceed?")
			}
			if !m.View().AltScreen {
				t.Errorf("stage %q drops the alternate screen", name)
			}
		})
	}
}
