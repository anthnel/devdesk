package status

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-292 step 4a: the footer of the status view carries the form's error.

func submitEmptyForm(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))
	for i := 0; i < 4; i++ {
		m = feed(t, m, testutil.Key("down"))
	}
	m, cmd := step(t, m, testutil.Key("enter"))
	if cmd != nil {
		t.Fatal("submitting an empty form produced a command")
	}
	return m
}

func TestFooterNamesTheInvalidFieldAfterARefusedSubmit(t *testing.T) {
	m := submitEmptyForm(t)

	footer := m.RenderFooter(120)
	if !strings.Contains(ansi.Strip(footer), "Name is required") {
		t.Fatalf("footer does not name the Name field:\n%s", ansi.Strip(footer))
	}
	sgr := strings.SplitN(theme.FooterErrorStyle.Render("x"), "x", 2)[0]
	if !strings.Contains(footer, sgr) {
		t.Error("the message is not rendered with the footer error style")
	}
	if !strings.Contains(m.Render(), "4:3") {
		t.Error("the form body shows no underline")
	}
}

func TestFooterIsSilentBeforeTheFirstSubmit(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))

	if strings.Contains(ansi.Strip(m.RenderFooter(120)), "is required") {
		t.Error("footer shows a validation message before any submit")
	}
	if strings.Contains(m.Render(), "4:3") {
		t.Error("underline before any submit")
	}
}

func TestFooterFollowsTheCorrectionAndOutlivesTheFooterTimer(t *testing.T) {
	m := submitEmptyForm(t)
	for i := 0; i < 4; i++ {
		m = feed(t, m, testutil.Key("up"))
	}
	m = feed(t, m, testutil.Type("api")...)

	got := ansi.Strip(m.RenderFooter(120))
	if strings.Contains(got, "Name is required") || !strings.Contains(got, "Target is required") {
		t.Errorf("footer after typing a name:\n%s", got)
	}
	if h := strings.Count(m.RenderFooter(120), "\n") + 1; h != m.GetFooterHeight() {
		t.Errorf("RenderFooter emitted %d lines, GetFooterHeight() promised %d", h, m.GetFooterHeight())
	}
}
