package ociresources

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-16 (DEVDESK-814, S9): the command preview and the ctrl+y copy name each
// environment variable and never carry its value, whatever the variable is
// (there is no "sensitive" class). The value stays in the child's environment
// (OC-14); the preview shows `-e NAME`, which is what the launcher really puts
// in the argv.
//
// Pinned below:
//
//   - buildCommandLines (the two-column preview) and buildCommandString (what
//     ctrl+y copies) print `-e NAME` for a NAME=VALUE entry.
//   - An entry with no "=" prints its trimmed name when docker.ValidEnvName
//     accepts it. An entry whose name is refused (it may be a secret pasted into
//     the name position) is never repeated: the fixed marker `-e <invalid>`
//     stands for it, so a pasted command fails instead of silently dropping the
//     variable.
//   - ctrl+y writes that same string through the clipboard seam (writeClipboard)
//     and the footer then says, at level Info, that values are not copied.
//   - The launch form help says the same.

const previewCanary = "canary"

var errClipboardForTest = errors.New("no clipboard on this system")

// envPreviewForm builds the form for the preview tests with a given Environment.
func envPreviewForm(width int, env string) *LaunchForm {
	f := NewLaunchForm("api:v1", nil, nil, width)
	setEnv(f, env)
	return f
}

// withClipboard replaces the clipboard seam with a recorder for one test.
func withClipboard(t *testing.T) *[]string {
	t.Helper()
	var written []string
	prev := writeClipboard
	writeClipboard = func(text string) error {
		written = append(written, text)
		return nil
	}
	t.Cleanup(func() { writeClipboard = prev })
	return &written
}

func hasLine(lines []string, want string) bool {
	return slices.ContainsFunc(lines, func(l string) bool {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l), "\\")) == want
	})
}

// Criterion 1: NAME=VALUE is previewed as -e NAME, in the lines of the
// preview, in the copied string and in the rendered right column.
func TestThePreviewNamesTheVariableAndNeverItsValue(t *testing.T) {
	f := envPreviewForm(120, "API_TOKEN="+previewCanary)

	if !hasLine(f.buildCommandLines(), "-e API_TOKEN") {
		t.Errorf("the preview lacks -e API_TOKEN:\n%s", strings.Join(f.buildCommandLines(), "\n"))
	}
	if !strings.Contains(" "+f.buildCommandString()+" ", " -e API_TOKEN ") {
		t.Errorf("the copied command lacks -e API_TOKEN: %q", f.buildCommandString())
	}
	for name, text := range map[string]string{
		"lines":  strings.Join(f.buildCommandLines(), "\n"),
		"copy":   f.buildCommandString(),
		"column": ansi.Strip(strings.Join(f.commandPreviewLines(), "\n")),
	} {
		if strings.Contains(text, previewCanary) || strings.Contains(text, "API_TOKEN=") {
			t.Errorf("the preview (%s) holds the value: %q", name, text)
		}
	}
}

// The value is typed in the Environment field on the left: that is the only
// place it may still be drawn. The right column adds none.
func TestTheRenderedFormShowsTheValueOnlyWhereItIsTyped(t *testing.T) {
	f := envPreviewForm(120, "API_TOKEN="+previewCanary)

	wide := strings.Count(ansi.Strip(f.View()), previewCanary)
	left := strings.Count(ansi.Strip(f.renderFormContent()), previewCanary)

	if wide != left {
		t.Errorf("the canary is drawn %d times in the two-column view, %d in the form alone", wide, left)
	}
	if !strings.Contains(ansi.Strip(f.View()), "-e API_TOKEN") {
		t.Error("the two-column view does not show -e API_TOKEN")
	}
}

// The narrow form has no preview column, but the copy is the same command.
func TestTheNarrowFormCopiesTheSameCommandWithoutTheValue(t *testing.T) {
	f := envPreviewForm(80, "API_TOKEN="+previewCanary)

	if strings.Contains(ansi.Strip(f.View()), "docker run") {
		t.Error("the narrow form shows a preview column")
	}
	got := f.buildCommandString()
	if strings.Contains(got, previewCanary) || !strings.Contains(" "+got+" ", " -e API_TOKEN ") {
		t.Errorf("narrow copy = %q, want -e API_TOKEN and no value", got)
	}
	wide := envPreviewForm(120, "API_TOKEN="+previewCanary)
	if got != wide.buildCommandString() {
		t.Errorf("the narrow copy %q differs from the wide one %q", got, wide.buildCommandString())
	}
}

// Criterion 3: no distinction between a sensitive name and any other.
func TestAnOrdinaryVariableIsPreviewedWithoutItsValueToo(t *testing.T) {
	f := envPreviewForm(120, "LOG_LEVEL=info, API_TOKEN="+previewCanary)

	for _, want := range []string{"-e LOG_LEVEL", "-e API_TOKEN"} {
		if !hasLine(f.buildCommandLines(), want) || !strings.Contains(" "+f.buildCommandString()+" ", " "+want+" ") {
			t.Errorf("preview or copy lacks %q: %q", want, f.buildCommandString())
		}
	}
	got := f.buildCommandString()
	for _, leak := range []string{"info", previewCanary, "LOG_LEVEL=", "API_TOKEN="} {
		if strings.Contains(got, leak) {
			t.Errorf("copy %q holds %q", got, leak)
		}
	}
}

// The value is cut at the first "=": a value that holds one is never repeated.
func TestAValueWithAnEqualsSignIsNotRepeated(t *testing.T) {
	f := envPreviewForm(120, "DSN=postgres://u:"+previewCanary+"@h/db?sslmode=disable")

	got := f.buildCommandString()
	if !strings.Contains(" "+got+" ", " -e DSN ") || strings.Contains(got, previewCanary) || strings.Contains(got, "sslmode") {
		t.Errorf("copy = %q, want -e DSN and nothing of the value", got)
	}
}

// An entry with no "=" is never named, whatever it looks like: it may be a token
// pasted into the field, and the launch refuses it. Only NAME=VALUE is named.
func TestAnEntryWithNoEqualsSignIsNeverNamed(t *testing.T) {
	for _, env := range []string{" API_TOKEN ", "API_TOKEN", "ghp_abcdef123456", "glpat-xxxxxxxxxxxx"} {
		t.Run(env, func(t *testing.T) {
			f := envPreviewForm(120, env)

			if !hasLine(f.buildCommandLines(), "-e <invalid>") {
				t.Errorf("the preview lacks the marker:\n%s", strings.Join(f.buildCommandLines(), "\n"))
			}
			fragment := strings.TrimSpace(env)
			for _, text := range []string{strings.Join(f.buildCommandLines(), "\n"), f.buildCommandString()} {
				if strings.Contains(text, fragment) {
					t.Errorf("preview or copy repeats %q: %q", fragment, text)
				}
			}
		})
	}
}

// An entry with an empty value (or only "=" after the name) is refused by the
// form, so it is not named: a base64 token ends with "=" and its "name" would be
// the token itself.
func TestAnEntryWithAnEmptyValueIsNeverNamed(t *testing.T) {
	for _, env := range []string{"abcdef==", "abcdef=", "API_TOKEN=", "API_TOKEN=="} {
		t.Run(env, func(t *testing.T) {
			f := envPreviewForm(120, env)

			if !hasLine(f.buildCommandLines(), "-e <invalid>") {
				t.Errorf("the preview lacks the marker:\n%s", strings.Join(f.buildCommandLines(), "\n"))
			}
			if got := f.buildCommandString(); strings.Contains(got, "abcdef") || strings.Contains(got, "API_TOKEN") {
				t.Errorf("copy repeats the entry: %q", got)
			}
		})
	}
}

// The preview names a variable only if the launch would accept the entry: the
// name is not trimmed, as in the form's validation (one rule, envEntryName).
func TestThePreviewAndTheValidationAgreeOnWhichEntriesAreNamed(t *testing.T) {
	for _, entry := range []string{
		"API_TOKEN=canary", "LOG_LEVEL=info", "API_TOKEN =canary", " =canary", "A B=c",
		"API_TOKEN=", "API_TOKEN", "=x", "-A=x", "caf\u00e9=x", "A.B-C=x", "9A=x",
	} {
		t.Run(entry, func(t *testing.T) {
			named := previewEnvName(entry) != invalidEnvMarker
			if named != envEntryValid(entry) {
				t.Errorf("preview names %q = %v, validation accepts it = %v", entry, named, envEntryValid(entry))
			}
		})
	}
	f := envPreviewForm(120, "API_TOKEN ="+previewCanary)
	if strings.Contains(f.buildCommandString(), "-e API_TOKEN ") || strings.Contains(f.buildCommandString(), previewCanary) {
		t.Errorf("a refused entry is named: %q", f.buildCommandString())
	}
}

// Whatever is typed and is not NAME=VALUE leaves no fragment in the wide
// preview, the copy or the footer after ctrl+y.
func TestThePreviewRepeatsNothingOfEntriesTheFormHasNotValidated(t *testing.T) {
	tests := []struct{ env, fragment string }{
		{"ghp_abcdef123456", "ghp_abcdef"},
		{"glpat-xxxxxxxxxxxx", "glpat"},
		{"abcdef==", "abcdef"},
		{"API_TOKEN", "API_TOKEN"},
		{"API_TOKEN=", "API_TOKEN"},
		{"API TOKEN=c", "API TOKEN"},
		{"=x,,", "=x"},
	}
	withClipboard(t)
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			m := launchingModel(t)
			m.launchForm.SetWidth(120)
			setEnv(m.launchForm, tt.env)

			if m.launchForm.buildCommandString() == "" || len(m.launchForm.commandPreviewLines()) == 0 {
				t.Fatalf("no preview for Environment %q", tt.env)
			}
			m, cmd := step(t, m, testutil.Key("ctrl+y"))
			m, _ = step(t, m, run(t, cmd))

			texts := []string{
				strings.Join(m.launchForm.buildCommandLines(), "\n"),
				m.launchForm.buildCommandString(),
				ansi.Strip(strings.Join(m.launchForm.commandPreviewLines(), "\n")),
				m.footer.Text(),
			}
			for _, text := range texts {
				if strings.Contains(text, tt.fragment) {
					t.Errorf("%q repeats %q", text, tt.fragment)
				}
			}
		})
	}
}

func TestAnInvalidNameIsNeverRepeatedInThePreviewOrTheCopy(t *testing.T) {
	tests := []struct {
		name  string
		input string
		leak  string
	}{
		{"a secret pasted as the name", "sk-live" + previewCanary + "/x", previewCanary},
		{"a secret pasted as the name with a space", "my " + previewCanary + "=x", previewCanary},
		{"a secret pasted as the name with a value", "tok" + previewCanary + "*=x", previewCanary},
		{"an empty name", "=" + previewCanary, previewCanary},
		{"a leading dash", "-" + previewCanary + "=x", previewCanary},
		{"a non-ASCII name", "caf\u00e9" + previewCanary + "=x", previewCanary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := envPreviewForm(120, tt.input)

			texts := []string{
				strings.Join(f.buildCommandLines(), "\n"),
				f.buildCommandString(),
				ansi.Strip(strings.Join(f.commandPreviewLines(), "\n")),
			}
			for _, text := range texts {
				if strings.Contains(text, tt.leak) {
					t.Errorf("preview or copy repeats %q: %q", tt.leak, text)
				}
				if !strings.Contains(text, "-e <invalid>") {
					t.Errorf("preview or copy lacks the fixed marker: %q", text)
				}
			}
		})
	}
}

// Whatever the reserved names do at submit (OC-15), the preview names them and
// never repeats a value.
func TestAReservedNameIsNamedWithoutItsValue(t *testing.T) {
	f := envPreviewForm(120, "PATH="+previewCanary)

	got := f.buildCommandString()
	if !strings.Contains(" "+got+" ", " -e PATH ") || strings.Contains(got, previewCanary) {
		t.Errorf("copy = %q, want -e PATH and no value", got)
	}
}

// Criterion 2: ctrl+y writes the preview's own command through the clipboard.
func TestCtrlYCopiesThePreviewedCommandWithoutValues(t *testing.T) {
	written := withClipboard(t)
	m := launchingModel(t)
	m.launchForm.envInput.SetValue("API_TOKEN=" + previewCanary + ", LOG_LEVEL=info")
	want := m.launchForm.buildCommandString()

	m, cmd := step(t, m, testutil.Key("ctrl+y"))
	msg := run(t, cmd)

	copied, ok := msg.(ClipboardCopyMsg)
	if !ok || copied.Err != nil {
		t.Fatalf("ctrl+y produced %#v, want a successful ClipboardCopyMsg", msg)
	}
	if len(*written) != 1 || (*written)[0] != want {
		t.Fatalf("clipboard = %q, want the previewed command %q", *written, want)
	}
	for _, leak := range []string{previewCanary, "info", "API_TOKEN=", "LOG_LEVEL="} {
		if strings.Contains((*written)[0], leak) {
			t.Errorf("the clipboard holds %q: %q", leak, (*written)[0])
		}
	}
	for _, name := range []string{"-e API_TOKEN", "-e LOG_LEVEL"} {
		if !strings.Contains((*written)[0]+" ", name+" ") {
			t.Errorf("the clipboard lacks %q: %q", name, (*written)[0])
		}
	}

	m, timer := step(t, m, msg)
	if m.footer.Level() != sharedcomponents.LevelInfo {
		t.Errorf("footer level = %v, want Info", m.footer.Level())
	}
	if !strings.Contains(m.footer.Text(), "values are not copied") {
		t.Errorf("footer = %q, want the values-are-not-copied notice", m.footer.Text())
	}
	if strings.Contains(m.footer.Text(), previewCanary) {
		t.Errorf("footer = %q repeats the value", m.footer.Text())
	}
	if timer == nil {
		t.Error("the notice was set with no clear timer (Rule 128)")
	}
}

func TestACopyThatFailsStillReportsAnError(t *testing.T) {
	m := launchingModel(t)
	prev := writeClipboard
	writeClipboard = func(string) error { return errClipboardForTest }
	t.Cleanup(func() { writeClipboard = prev })

	m, cmd := step(t, m, testutil.Key("ctrl+y"))
	m, _ = step(t, m, run(t, cmd))

	if m.footer.Level() != sharedcomponents.LevelError {
		t.Errorf("footer level = %v, want Error", m.footer.Level())
	}
}

// Criterion 4: the help of the launch form says what ctrl+y copies.
func TestTheLaunchFormHelpSaysCtrlYCopiesNamesOnly(t *testing.T) {
	var body string
	for _, s := range launchingModel(t).GetHelpContent().Sections {
		if s.Title == "Launch Form" {
			body = s.Body
		}
	}
	if body == "" {
		t.Fatal("the help has no Launch Form section")
	}
	for _, want := range []string{"Ctrl+Y", "-e NAME", "values are not copied"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Launch Form help lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "-e NAME=VALUE") {
		t.Errorf("the Launch Form help still describes a value in the command:\n%s", body)
	}
}
