package ociresources

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-14 (DEVDESK-812, S9): what the launch form accepts in Environment, what it
// does with a refused entry (Rule 140: the field is underlined and the footer
// says why, at level Error, derived on every render), and what a launch shows
// when it fails (a fixed sentence). The argv and child-environment half of the
// story is internal/docker/launch_env_test.go; the end-to-end test here runs the
// whole path, form to fake engine, on both engines.
//
// Expected production behavior, pinned below:
//
//   - LaunchForm.validate() returns one more FieldError, key "env", when an
//     Environment entry (comma separated, trimmed, blank ones skipped) has no "=",
//     an empty value after it, or a name docker.ValidEnvName refuses. The text is
//     a sentence of the form's own and never repeats a value.
//   - applyInvalid() underlines envInput through components.InvalidMarks.Sync
//     with that key, and updateActiveInput revalidates while the env field is edited.
//   - ApplyCached and the form opening validate nothing (OC-14 criterion 7).
//   - LaunchFormSubmitMsg.Opts.Env stays the list of "NAME=VALUE" entries.

func newEnvForm() *LaunchForm { return NewLaunchForm("api:v1", nil, nil, 120) }

func setEnv(f *LaunchForm, value string) { f.envInput.SetValue(value) }

// submitOpts presses enter on the Launch button and returns the options it
// submitted, or false when the submit was refused.
func submitOpts(f *LaunchForm) (docker.ContainerLaunchOptions, bool) {
	focusLaunchField(f, f.fieldSubmit())
	_, cmd := f.Update(testutil.Key("enter"))
	if cmd == nil {
		return docker.ContainerLaunchOptions{}, false
	}
	msg, ok := testutil.MsgOf[LaunchFormSubmitMsg](cmd)
	return msg.Opts, ok
}

// envUnderlined reports whether the Environment input carries the invalid style,
// on its own and in the rendered form.
func envUnderlined(f *LaunchForm) (input, rendered bool) {
	return strings.Contains(f.envInput.View(), curlyUnderline), strings.Contains(f.View(), curlyUnderline)
}

// Criteria 3 and 4. Every row is refused, underlined, named in the footer at
// level Error, and nothing is submitted. The footer never holds the typed value.
func TestLaunchFormRefusesAnEnvironmentEntryWithNoValueOrAnInvalidName(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"a name with no value", "API_TOKEN"},
		{"a name with an empty value", "API_TOKEN="},
		{"an empty value among valid entries", "LOG_LEVEL=info, API_TOKEN="},
		{"a bare name among valid entries", "LOG_LEVEL=info, API_TOKEN, REGION=eu"},
		{"a space in the name", "API TOKEN=canary"},
		{"a space before the equals sign", "API_TOKEN =canary"},
		{"an empty name", "=canary"},
		{"an empty name among valid entries", "LOG_LEVEL=info, =canary"},
		{"a lone equals sign", "="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, tt.input)

			if opts, ok := submitOpts(f); ok {
				t.Fatalf("submit with Environment %q produced LaunchFormSubmitMsg %+v, want a refusal", tt.input, opts)
			}
			if input, rendered := envUnderlined(f); !input || !rendered {
				t.Errorf("Environment %q: underlined = input %v, form %v, want both after the refused submit", tt.input, input, rendered)
			}
			if strings.Contains(f.nameInput.View(), curlyUnderline) {
				t.Error("the Name field is underlined, only Environment is faulty")
			}
			st := footerStatus(f)
			if st.Level != sharedcomponents.LevelError || st.Text == "" {
				t.Fatalf("formStatus() = {%q, level %v}, want an Error sentence", st.Text, st.Level)
			}
			if strings.Contains(st.Text, "canary") || strings.Contains(st.Text, tt.input) {
				t.Errorf("footer %q repeats what was typed: a value must never be echoed", st.Text)
			}
		})
	}
}

// What the form accepts: a name, a first "=", then at least one character, which
// may hold any "=". Blank entries between commas are skipped, as before.
func TestLaunchFormAcceptsEveryEnvironmentEntryThatHasANameAndAValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"nothing", "", nil},
		{"blanks", "   ", nil},
		{"one variable", "API_TOKEN=canary", []string{"API_TOKEN=canary"}},
		{"two variables", "API_TOKEN=canary, LOG_LEVEL=info", []string{"API_TOKEN=canary", "LOG_LEVEL=info"}},
		{"equals signs in the value", "DSN=a=b", []string{"DSN=a=b"}},
		{"blank entries are skipped", "A=1, ,B=2", []string{"A=1", "B=2"}},
		{"a leading underscore", "_X=1", []string{"_X=1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, tt.input)

			opts, ok := submitOpts(f)

			if !ok {
				t.Fatalf("Environment %q was refused: %q", tt.input, footerStatus(f).Text)
			}
			if !slices.Equal(opts.Env, tt.want) {
				t.Errorf("Opts.Env = %q, want %q", opts.Env, tt.want)
			}
			if input, rendered := envUnderlined(f); input || rendered {
				t.Error("an accepted Environment is underlined")
			}
			if st := footerStatus(f); st.Text != "" {
				t.Errorf("formStatus() = %q after an accepted submit, want nothing", st.Text)
			}
		})
	}
}

// Rule 140: nothing is invalid before the first refused submit, then every change
// revalidates the whole form and the footer follows on the next render.
func TestLaunchFormEnvironmentIsNotFlaggedBeforeTheFirstRefusedSubmit(t *testing.T) {
	f := newEnvForm()
	setEnv(f, "API_TOKEN")
	focusLaunchField(f, f.fieldEnv())
	f.envInput.CursorEnd()
	f, _ = f.Update(testutil.Key("x")) // a change before any submit must not arm validation

	if input, rendered := envUnderlined(f); input || rendered {
		t.Error("a bare name is underlined before any submit")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("formStatus().Text = %q before any submit, want empty", st.Text)
	}
}

func TestLaunchFormEnvironmentErrorFollowsEditsAfterARefusal(t *testing.T) {
	f := newEnvForm()
	setEnv(f, "API_TOKEN")
	if _, ok := submitOpts(f); ok {
		t.Fatal("precondition: a bare name must be refused")
	}
	if footerStatus(f).Text == "" {
		t.Fatal("precondition: the refused submit must set a footer error")
	}

	focusLaunchField(f, f.fieldEnv())
	f.envInput.CursorEnd() // SetValue while blurred leaves the cursor at 0
	for _, m := range testutil.Type("=canary") {
		f, _ = f.Update(m)
	}
	if got := f.envInput.Value(); got != "API_TOKEN=canary" {
		t.Fatalf("Environment = %q after the correction, want API_TOKEN=canary", got)
	}
	if input, rendered := envUnderlined(f); input || rendered {
		t.Error("Environment is still underlined after the correction")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("formStatus().Text = %q after the correction, want empty", st.Text)
	}

	// Revalidation stays live: breaking it again shows at once, without a submit.
	// (This comes before the accepted submit: an accepted submit disarms the
	// live revalidation, components.Validation.Submit, and closes the form.)
	focusLaunchField(f, f.fieldEnv())
	f.envInput.CursorEnd()
	for _, m := range testutil.Type(", OTHER") {
		f, _ = f.Update(m)
	}
	if input, _ := envUnderlined(f); !input {
		t.Error("a bare name typed after a refusal was not underlined at once")
	}
	if footerStatus(f).Level != sharedcomponents.LevelError {
		t.Error("the footer did not follow the new error")
	}

	setEnv(f, "API_TOKEN=canary")
	if _, ok := submitOpts(f); !ok {
		t.Error("submit refused after the correction")
	}
}

// Environment errors and host port errors share the one footer status: the first
// in display order is named, with the count of the others.
func TestLaunchFormEnvironmentErrorJoinsThePortErrors(t *testing.T) {
	f := newValidationForm() // two EXPOSE ports
	setHostPort(f, 0, "80a0", true)
	setEnv(f, "API_TOKEN")

	if _, ok := submitOpts(f); ok {
		t.Fatal("submit with an invalid port and a bare name was accepted")
	}
	if !strings.Contains(portLine(t, f, "80/tcp"), curlyUnderline) {
		t.Error("the invalid port lost its underline")
	}
	if input, _ := envUnderlined(f); !input {
		t.Error("Environment is not underlined next to the port")
	}
	if st := footerStatus(f); !strings.Contains(st.Text, "(+1 more)") {
		t.Errorf("formStatus().Text = %q, want the first error and (+1 more)", st.Text)
	}
}

// Criterion 7: a name restored from the cache (OC-17) with no value raises nothing
// when the form opens. The refusal belongs to the submit, and only while the value
// is still empty. Both spellings of "a name without a value" are covered because
// OC-17 has not fixed how the cache prefills them.
func TestACachedNameWithNoValueIsNotAnErrorUntilTheSubmit(t *testing.T) {
	for _, env := range []string{"API_TOKEN=", "API_TOKEN", "REGION=eu, API_TOKEN="} {
		t.Run(env, func(t *testing.T) {
			m := launchingModel(t, "80/tcp")

			m = feed(t, m, LaunchOptionsCacheLoadedMsg{
				ImageName: "api:v1",
				Entry:     &cache.LaunchOptionsEntry{Env: env, Detach: true},
			})

			if got := m.launchForm.envInput.Value(); got != env {
				t.Fatalf("Environment = %q, want the cached %q", got, env)
			}
			if st := m.formStatus(); st.Text != "" {
				t.Errorf("formStatus() = %q when the form opens, want no error", st.Text)
			}
			if input, rendered := envUnderlined(m.launchForm); input || rendered {
				t.Error("a cached name with no value is underlined when the form opens")
			}

			// Submitting without typing the value is the refusal of criterion 3.
			if _, ok := submitOpts(m.launchForm); ok {
				t.Fatal("the submit went through with a name that still has no value")
			}
			if input, _ := envUnderlined(m.launchForm); !input {
				t.Error("the field is not underlined after the refused submit")
			}
			if st := m.formStatus(); st.Level != sharedcomponents.LevelError || st.Text == "" {
				t.Errorf("formStatus() = {%q, %v}, want an Error sentence", st.Text, st.Level)
			}
		})
	}
}

// Entering the missing value clears the refusal and lets the launch through.
func TestACachedNameRetypedWithItsValueIsLaunched(t *testing.T) {
	m := launchingModel(t, "80/tcp")
	m = feed(t, m, LaunchOptionsCacheLoadedMsg{
		ImageName: "api:v1", Entry: &cache.LaunchOptionsEntry{Env: "API_TOKEN=", Detach: true},
	})
	if _, ok := submitOpts(m.launchForm); ok {
		t.Fatal("precondition: the empty value must be refused")
	}

	m.launchForm.envInput.SetValue("API_TOKEN=canary")
	opts, ok := submitOpts(m.launchForm)

	if !ok {
		t.Fatalf("the retyped value was refused: %q", m.formStatus().Text)
	}
	if !slices.Equal(opts.Env, []string{"API_TOKEN=canary"}) {
		t.Errorf("Opts.Env = %q, want the retyped variable", opts.Env)
	}
}

// useEngineNamed pins the engine of the process for one test.
func useEngineNamed(t *testing.T, name string) {
	t.Helper()
	previous := engine.Current()
	engine.SetCurrent(engine.ShapeFor(name))
	t.Cleanup(func() { engine.SetCurrent(previous) })
}

// envMap reads NAME=VALUE entries the way exec does: the last entry of a name wins.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		name, value, _ := strings.Cut(e, "=")
		m[name] = value
	}
	return m
}

const (
	hostOnlyVar   = "DEVDESK_OC14_HOST_ONLY"
	hostOnlyValue = "host-secret-value"
)

// Criteria 1, 2 and 6, end to end: the form, the submit message, the command the
// model returns, the real docker package and a fake engine, on both engines. The
// journal of fakecli holds what the process list would have shown and the
// environment of the child.
func TestAFormWithAnAPITokenLaunchesItByNameOnEveryEngine(t *testing.T) {
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			useEngineNamed(t, name)
			fake := installFakeTools(t, fakeScript{name + " run": {Stdout: "9f2c1d4e5b6a\n"}}, name)
			t.Setenv(hostOnlyVar, hostOnlyValue)

			m := launchingModel(t, "80/tcp")
			setEnv(m.launchForm, "API_TOKEN=canary, LOG_LEVEL=info")
			opts, ok := submitOpts(m.launchForm)
			if !ok {
				t.Fatalf("the form refused a valid Environment: %q", m.formStatus().Text)
			}
			_, cmd := step(t, m, LaunchFormSubmitMsg{Opts: opts})
			base := os.Environ()

			done, isLaunch := run(t, cmd).(ContainerLaunchCompleteMsg)

			if !isLaunch || done.Err != nil {
				t.Fatalf("the launch ended with %+v, want a started container", done)
			}
			calls := fake.Invocations()
			if len(calls) != 1 {
				t.Fatalf("the engine was started %d times, want one run: %v", len(calls), calls)
			}
			got := calls[0]
			for i := range got.Args {
				if strings.Contains(got.Args[i], "canary") || strings.Contains(got.Args[i], "info") {
					t.Errorf("argv word %q holds a typed value: it would show in the process list", got.Args[i])
				}
			}
			if !containsPair(got.Args, "-e", "API_TOKEN") || !containsPair(got.Args, "-e", "LOG_LEVEL") {
				t.Errorf("argv = %q, want -e API_TOKEN and -e LOG_LEVEL", got.Args)
			}
			if slices.Contains(got.Args, hostOnlyVar) {
				t.Errorf("argv names %s, a variable of DevDesk the form never set", hostOnlyVar)
			}
			want := envMap(base)
			maps.Copy(want, map[string]string{"API_TOKEN": "canary", "LOG_LEVEL": "info"})
			if !maps.Equal(envMap(got.Env), want) {
				t.Errorf("child environment is not DevDesk's environment plus the two typed variables\n got %v\nwant %v",
					envMap(got.Env), want)
			}
			if !slices.Contains(got.Env, "API_TOKEN=canary") {
				t.Error("the child environment does not hold API_TOKEN=canary")
			}
		})
	}
}

// containsPair reports whether args holds first immediately followed by second.
func containsPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}

// Criterion 5: a launch the engine refuses shows one fixed sentence, the same
// whatever the engine said, with neither the value nor any piece of its stderr.
func TestAFailedLaunchShowsAFixedSentenceWithNoValueAndNoStderr(t *testing.T) {
	errs := []error{
		errors.New("docker run failed: Error response from daemon: invalid env API_TOKEN=canary"),
		fmt.Errorf("podman run failed: %s", "Error: crun: canary: no such file or directory"),
		errors.New("port is already allocated"),
		errors.New("exit status 125"),
	}
	var first string
	for i, launchErr := range errs {
		t.Run(fmt.Sprintf("error %d", i), func(t *testing.T) {
			m := launchingModel(t)
			m = feed(t, m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{
				Image: "api:v1", Env: []string{"API_TOKEN=canary"}, Detach: true,
			}})

			next, cmd := step(t, m, ContainerLaunchCompleteMsg{Err: launchErr})

			text := next.footer.Text()
			if text == "" || cmd == nil {
				t.Fatalf("a failed launch reported %q (timer %v), want a sentence with its Rule 128 timer", text, cmd != nil)
			}
			if next.footer.Level() != sharedcomponents.LevelError {
				t.Errorf("level = %v, want Error", next.footer.Level())
			}
			for _, leak := range []string{"canary", "daemon", "crun", "exit status", "allocated", "run failed"} {
				if strings.Contains(text, leak) {
					t.Errorf("footer %q holds %q taken from the engine's answer or the typed value", text, leak)
				}
			}
			if first == "" {
				first = text
			} else if text != first {
				t.Errorf("footer = %q, want the same fixed sentence as before (%q)", text, first)
			}
		})
	}
}

// The same, from the real command: the engine exits 125 and echoes the value on
// its stderr, which docker.LaunchContainer returns in its error. What reaches the
// footer is the fixed sentence anyway, on both engines.
func TestAnEngineThatEchoesTheValueNeverReachesTheFooter(t *testing.T) {
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			useEngineNamed(t, name)
			installFakeTools(t, fakeScript{name + " run": {Stderr: "Error: invalid value canary for API_TOKEN\n", Exit: 125}}, name)
			m := launchingModel(t)
			opts := docker.ContainerLaunchOptions{Image: "api:v1", Env: []string{"API_TOKEN=canary"}, Detach: true}
			m = feed(t, m, LaunchFormSubmitMsg{Opts: opts})

			done, ok := run(t, launchContainerCmd(opts)).(ContainerLaunchCompleteMsg)
			if !ok || done.Err == nil {
				t.Fatalf("the refused launch ended with %+v, want an error", done)
			}
			next, _ := step(t, m, done)

			if text := next.footer.Text(); text == "" || strings.Contains(text, "canary") || strings.Contains(text, "invalid value") {
				t.Errorf("footer = %q, want a fixed sentence with no value and no stderr", text)
			}
		})
	}
}

// The log gets a summary too: the engine's output can echo the value, and the
// footer sentence tells the user to check the logs.
func TestAFailedLaunchLogsNoValueAndNoStderr(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	useEngineNamed(t, "docker")
	installFakeTools(t, fakeScript{"docker run": {Stderr: "Error: invalid value canary for API_TOKEN\n", Exit: 125}}, "docker")
	m := launchingModel(t)
	opts := docker.ContainerLaunchOptions{Image: "api:v1", Env: []string{"API_TOKEN=canary"}, Detach: true}
	m = feed(t, m, LaunchFormSubmitMsg{Opts: opts})
	done, ok := run(t, launchContainerCmd(opts)).(ContainerLaunchCompleteMsg)
	if !ok || done.Err == nil {
		t.Fatalf("the refused launch ended with %+v, want an error", done)
	}

	step(t, m, done)

	logged := buf.String()
	if !strings.Contains(logged, "launch container") {
		t.Fatalf("nothing logged for the failed launch: %q", logged)
	}
	for _, leak := range []string{"canary", "invalid value", "API_TOKEN"} {
		if strings.Contains(logged, leak) {
			t.Errorf("log %q holds %q", logged, leak)
		}
	}
	if !strings.Contains(logged, "exit code 125") {
		t.Errorf("log %q lacks the exit code", logged)
	}
}

// OC-16 will take the values out of the preview and of ctrl+y. Until then the
// preview must at least survive an entry the form has not validated yet.
func TestThePreviewSurvivesAnEnvironmentEntryThatIsNotValidated(t *testing.T) {
	for _, env := range []string{"API_TOKEN", "=x", "API_TOKEN=", "A B=c", ",,"} {
		t.Run(env, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, env)

			if f.buildCommandString() == "" || len(f.commandPreviewLines()) == 0 {
				t.Errorf("no preview for Environment %q", env)
			}
		})
	}
}
