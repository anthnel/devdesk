package ociresources

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/canary"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-23 (DEVDESK-821, S8): SECRET=canary is typed in the launch form, and the
// test proves it is nowhere it must not be, at every step a value can pass
// through: the form and its preview, ctrl+y, the launch (the argv and the
// environment of the child), the launch cache, a launch the engine refuses
// while repeating the value on its stderr, and then every file under HOME, the
// log, the footer and the rendered view. The inspect half of the story (the
// masked `Config.Env`, `Y`, `m`) is in internal/ui/viewer/canary_inspect_test.go:
// its seam is in another package.
//
// The set of surfaces is internal/testutil/canary's: a story that adds a place
// that can hold a value adds one canary.Surface to the list below.
//
// What is NOT a surface here, on purpose: the Environment input of the form
// while it is open (the user typed the value there: the test proves it holds it,
// then looks at the rest of the form without it), the child's environment
// (that is where the value must travel) and the error value a refused launch
// returns (docker.LaunchError carries the engine's output by design,
// TestALaunchThatDockerRefusesCarriesTheReason; what must not carry it is what
// DevDesk shows or writes, which is every surface below).

// canaryClipboard replaces the clipboard seam with a recorder for one test.
func canaryClipboard(t *testing.T) *canary.Recorder {
	t.Helper()
	var rec canary.Recorder
	previous := writeClipboard
	writeClipboard = func(text string) error { rec.Record(text); return nil }
	t.Cleanup(func() { writeClipboard = previous })
	return &rec
}

// typeCanary types SECRET=canary into the Environment field through the keys.
func typeCanary(t *testing.T, m Model) Model {
	t.Helper()
	focusLaunchField(m.launchForm, m.launchForm.fieldEnv())
	m.launchForm.envInput.CursorEnd()
	m = feed(t, m, testutil.Type(canary.Entry)...)
	if got := m.launchForm.envInput.Value(); got != canary.Entry {
		t.Fatalf("Environment = %q after typing, want %q", got, canary.Entry)
	}
	return m
}

// submitFromModel presses enter on the Launch button and returns the submit the
// form produced.
func submitFromModel(t *testing.T, m Model) LaunchFormSubmitMsg {
	t.Helper()
	focusLaunchField(m.launchForm, m.launchForm.fieldSubmit())
	_, cmd := m.launchForm.Update(testutil.Key("enter"))
	msg, ok := testutil.MsgOf[LaunchFormSubmitMsg](cmd)
	if !ok {
		t.Fatalf("the form refused SECRET=canary: %q", m.formStatus().Text)
	}
	return msg
}

// viewSurfaces are the places the view shows text a value must not be in, read
// from the model as it is when the surface is asserted.
//
// The rendered body is only a surface once the form is closed: while it is open it
// shows the Environment input, where the user typed the value.
func viewSurfaces(prefix string, current func() Model, body bool) canary.Surfaces {
	var s canary.Surfaces
	s.Add(
		canary.Text(prefix+": footer message", func() string { m := current(); return m.footer.Text() }),
		canary.Text(prefix+": footer status", func() string { return current().formStatus().Text }),
		canary.Text(prefix+": rendered footer", func() string { return ansi.Strip(current().RenderFooter(current().width)) }),
	)
	if body {
		s.Add(canary.Text(prefix+": rendered view", func() string { return ansi.Strip(current().Render()) }))
	}
	return s
}

func TestCanaryTypedInTheLaunchFormIsNowhereItMustNotBe(t *testing.T) {
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			useEngineNamed(t, name)
			home, homeFiles := canary.Home(t)
			canary.AssertWired(t, home, homeFiles)
			logs := canary.CaptureLog(t, "DevDesk log")
			clipboard := canaryClipboard(t)

			// 1. The form. The value is in the Environment input, and only there.
			started := installFakeTools(t, fakeScript{name + " run": {Stdout: "9f2c1d4e5b6a\n"}}, name)
			m := typeCanary(t, launchingModel(t, "80/tcp"))
			form := m.launchForm
			canary.AssertPresent(t, canary.Text("Environment input", form.envInput.Value))

			var surfaces canary.Surfaces
			surfaces.Add(homeFiles, logs, clipboard.Surface("clipboard"), canary.Environ("DevDesk process environment"))
			surfaces.Add(
				canary.Lines("form preview lines", form.buildCommandLines),
				canary.Text("form copied command", form.buildCommandString),
				canary.Text("form preview column", func() string { return ansi.Strip(strings.Join(form.commandPreviewLines(), "\n")) }),
				canary.Text("form footer status", func() string { return form.ValidationStatus().Text }),
				canary.Text("form, Environment input left out", func() string {
					typed := form.envInput.Value()
					form.envInput.SetValue("")
					defer form.envInput.SetValue(typed)
					return ansi.Strip(form.View())
				}),
			)
			surfaces.AssertAbsent(t)

			// 2. ctrl+y copies the command line, and says values are not copied.
			m, copyCmd := step(t, m, testutil.Key("ctrl+y"))
			m = feed(t, m, run(t, copyCmd))
			if got := clipboard.Items(); len(got) != 1 || !strings.Contains(" "+got[0]+" ", " -e "+canary.Name+" ") {
				t.Fatalf("ctrl+y wrote %q to the clipboard, want one command with -e %s", got, canary.Name)
			}
			if got := m.footer.Text(); got != commandCopiedText {
				t.Errorf("footer after ctrl+y = %q, want %q", got, commandCopiedText)
			}
			var latest = &m
			surfaces.Add(viewSurfaces("after ctrl+y", func() Model { return *latest }, false)...)
			surfaces.AssertAbsent(t)

			// 3. The launch. The argv names the variable; the child's environment
			// carries the value (the proof that the test can see it).
			submit := submitFromModel(t, m)
			m, launchCmd := step(t, m, submit)
			done, isDone := run(t, launchCmd).(ContainerLaunchCompleteMsg)
			if !isDone || done.Err != nil {
				t.Fatalf("the launch ended with %+v, want a started container", done)
			}
			m, saveCmd := step(t, m, done)
			_ = run(t, saveCmd) // writes the launch cache under the test's home
			latest = &m
			if m.launchForm != nil {
				t.Fatal("the form is still open after the submit")
			}
			calls := started.Invocations()
			if len(calls) != 1 {
				t.Fatalf("the engine was started %d times, want one run", len(calls))
			}
			canary.AssertPresent(t, canary.Lines("child environment", func() []string { return calls[0].Env }))
			if !slices.Contains(calls[0].Env, canary.Entry) {
				t.Fatalf("the child environment lacks %s", canary.Entry)
			}
			if !containsPair(calls[0].Args, "-e", canary.Name) {
				t.Errorf("argv = %q, want -e %s", calls[0].Args, canary.Name)
			}
			surfaces.Add(canary.Lines("engine argv", func() []string { return append([]string{calls[0].Tool}, calls[0].Args...) }))
			surfaces.Add(viewSurfaces("after the launch", func() Model { return *latest }, true)...)
			surfaces.AssertAbsent(t)

			// 4. The cache keeps the name, never the value.
			cachePath := filepath.Join(home, ".devdesk", "cache", "launch-options.json")
			body, err := os.ReadFile(cachePath)
			if err != nil {
				t.Fatalf("the launch left no cache file: %v", err)
			}
			var cached map[string]struct {
				EnvNames []string `json:"env_names"`
			}
			if err := json.Unmarshal(body, &cached); err != nil || !slices.Equal(cached["api:v1"].EnvNames, []string{canary.Name}) {
				t.Errorf("the cache holds env_names %v (err %v), want [%s]", cached["api:v1"].EnvNames, err, canary.Name)
			}
			surfaces.Add(canary.Files("launch cache directory", filepath.Dir(cachePath)))
			surfaces.AssertAbsent(t)

			// 5. A launch the engine refuses, repeating the value on its stderr.
			refused := installFakeTools(t, fakeScript{name + " run": {
				Stderr: "Error: invalid environment " + canary.Entry + " (" + canary.Value + ")\n", Exit: 125,
			}}, name)
			m = typeCanary(t, launchingModel(t, "80/tcp"))
			latest = &m
			submit = submitFromModel(t, m)
			m, launchCmd = step(t, m, submit)
			done, isDone = run(t, launchCmd).(ContainerLaunchCompleteMsg)
			if !isDone || done.Err == nil {
				t.Fatalf("the refused launch ended with %+v, want an error", done)
			}
			canary.AssertPresent(t, canary.Text("the engine's own error", done.Err.Error))
			m, _ = step(t, m, done)
			latest = &m
			if m.footer.Text() == "" {
				t.Fatal("a refused launch said nothing in the footer")
			}
			refusedCalls := refused.Invocations()
			if len(refusedCalls) != 1 || !slices.Contains(refusedCalls[0].Env, canary.Entry) {
				t.Fatalf("the refused launch did not reach the engine with the value in its environment: %+v", refusedCalls)
			}
			surfaces.Add(canary.Lines("refused launch argv", func() []string {
				return append([]string{refusedCalls[0].Tool}, refusedCalls[0].Args...)
			}))
			surfaces.Add(viewSurfaces("after a refused launch", func() Model { return *latest }, true)...)
			surfaces.AssertAbsent(t)

			// The log has the summary of the refusal and nothing else of it.
			if got := logSnapshot(logs); !strings.Contains(got, "launch container") || !strings.Contains(got, "exit code 125") {
				t.Error("the log lacks the summary of the refused launch (\"launch container\" and \"exit code 125\")")
			}
		})
	}
}

// logSnapshot reads the log surface as one string.
func logSnapshot(s canary.Surface) string {
	var b bytes.Buffer
	parts, _ := s.Read()
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// The negative control of the helper on this very flow: a surface that does hold
// the value is found, so the assertions above are not vacuous. The cache file of
// an older build, which kept the whole Environment field, is the realistic one.
func TestCanaryIsFoundInALegacyCacheFile(t *testing.T) {
	home, homeFiles := canary.Home(t)
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"api:v1":{"env":"` + canary.Entry + `","detach":true}}`
	if err := os.WriteFile(filepath.Join(dir, "launch-options.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	canary.AssertPresent(t, homeFiles)

	// Opening the cache migrates it: the value is gone from disk (OC-17).
	_ = run(t, loadLaunchOptionsCmd("api:v1"))
	canary.AssertAbsent(t, homeFiles)
}
