package ociresources

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// OC-21 (DEVDESK-819, requirement S4), the form's half of "a typed value is one
// argv element and never a flag". The launcher refuses the same values
// (internal/docker/launch_privileged_test.go); the form says so first, the way
// OC-14 does for a bad environment entry:
//
//   - A value that STARTS with "-" (after the trim the submit applies) in Name,
//     Entrypoint, User, or in an entry of Volumes or Additional ports (comma
//     separated, each entry trimmed), refuses the submit: no LaunchFormSubmitMsg,
//     the field is underlined through theme.StyleInvalidField (Rule 140), and the
//     footer says why at level Error, derived on every render, with a fixed
//     sentence that never repeats the value. Nothing is invalid before the first
//     refused submit; after it every change revalidates, so correcting the value
//     clears the underline and the footer on the next render.
//   - The sentences are "<Label> cannot start with a dash", with the label as the
//     form shows it: Name, Entrypoint, User, Volumes, Additional ports.
//   - Network is a ←→ cycle field: there is nothing to underline. A network of the
//     list that starts with "-" refuses the submit all the same, and the footer
//     says "Network cannot start with a dash". The image is not a field either:
//     "Image cannot start with a dash".
//   - What stays valid: a dash anywhere but first (`sh -c x`, `my-app`), an
//     environment value that starts with one (`TOKEN=-x`: the value travels in the
//     child's environment, OC-14), volumes that start with `/`, `.`, `~` or a name,
//     and a --mount value (it starts with `type=`).
//
// The entrypoint (criterion 4) is a single string everywhere: Opts.Entrypoint is
// what was typed, trimmed, and the argv holds one element after --entrypoint.

const (
	dashSuffix       = " cannot start with a dash"
	imageDashMessage = "Image" + dashSuffix
	netDashMessage   = "Network" + dashSuffix
)

type dashField struct {
	name, label string
	set         func(*LaunchForm, string)
	input       func(*LaunchForm) *textinput.Model
}

var dashFields = []dashField{
	{"name", "Name", func(f *LaunchForm, v string) { f.nameInput.SetValue(v) },
		func(f *LaunchForm) *textinput.Model { return &f.nameInput }},
	{"entrypoint", "Entrypoint", func(f *LaunchForm, v string) { f.entrypointInput.SetValue(v) },
		func(f *LaunchForm) *textinput.Model { return &f.entrypointInput }},
	{"user", "User", func(f *LaunchForm, v string) { f.userInput.SetValue(v) },
		func(f *LaunchForm) *textinput.Model { return &f.userInput }},
	{"volumes", "Volumes", func(f *LaunchForm, v string) { f.volInput.SetValue(v) },
		func(f *LaunchForm) *textinput.Model { return &f.volInput }},
	{"additional ports", "Additional ports", func(f *LaunchForm, v string) { f.extraPortsInput.SetValue(v) },
		func(f *LaunchForm) *textinput.Model { return &f.extraPortsInput }},
}

func newDashForm() *LaunchForm {
	return NewLaunchForm("api:v1", []string{"80/tcp"}, []docker.Network{{Name: "bridge"}}, 90)
}

// dashValues are typed into every field of dashFields: each starts with a dash
// once trimmed.
var dashValues = []string{"-x", "--privileged", "  -x", "--pid=host", "-"}

// renderedInputLine returns the raw line of the form that holds the input of the
// field called label: the line under its label line.
func renderedInputLine(t *testing.T, f *LaunchForm, label string) string {
	t.Helper()
	lines := strings.Split(f.View(), "\n")
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), label+" "+theme.IconChevronRight) && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("no field %q in:\n%s", label, ansi.Strip(f.View()))
	return ""
}

func TestAValueThatStartsWithADashRefusesTheSubmitAndSaysWhere(t *testing.T) {
	for _, fld := range dashFields {
		for _, value := range dashValues {
			t.Run(fld.name+"/"+value, func(t *testing.T) {
				f := newDashForm()
				fld.set(f, value)

				f, submitted := submitLaunch(f)

				if submitted {
					t.Fatalf("a %s of %q produced LaunchFormSubmitMsg, want a refusal", fld.name, value)
				}
				if !strings.Contains(fld.input(f).View(), curlyUnderline) {
					t.Errorf("the %s input is not underlined after the refused submit", fld.name)
				}
				if line := renderedInputLine(t, f, fld.label); !strings.Contains(line, curlyUnderline) {
					t.Errorf("the rendered %s line is not underlined: %q", fld.name, line)
				}
				for _, other := range dashFields {
					if other.name != fld.name && strings.Contains(other.input(f).View(), curlyUnderline) {
						t.Errorf("the %s input is underlined, only the faulty %s should be", other.name, fld.name)
					}
				}
				st := footerStatus(f)
				if want := fld.label + dashSuffix; st.Text != want || st.Level != sharedcomponents.LevelError {
					t.Errorf("formStatus() = {%q, level %v}, want {%q, LevelError}", st.Text, st.Level, want)
				}
				if strings.Contains(st.Text, "--") {
					t.Errorf("the footer %q repeats the typed value", st.Text)
				}
			})
		}
	}
}

// A dash-first entry among good ones, in the comma separated fields.
func TestAnEntryThatStartsWithADashAmongGoodOnesRefusesTheSubmit(t *testing.T) {
	tests := []struct{ field, value string }{
		{"volumes", "data:/d, --privileged"},
		{"volumes", "-data:/d"},
		{"volumes", "./d:/d,  -v /etc:/e"},
		{"additional ports", "8080:80, -8080:80"},
		{"additional ports", "--privileged,8080:80"},
	}
	for _, tt := range tests {
		t.Run(tt.field+"/"+tt.value, func(t *testing.T) {
			f := newDashForm()
			for _, fld := range dashFields {
				if fld.name == tt.field {
					fld.set(f, tt.value)
				}
			}

			f, submitted := submitLaunch(f)

			if submitted {
				t.Fatalf("%s %q was accepted", tt.field, tt.value)
			}
			if st := footerStatus(f); st.Level != sharedcomponents.LevelError || !strings.HasSuffix(st.Text, dashSuffix) {
				t.Errorf("formStatus() = {%q, level %v}, want an Error that says a value cannot start with a dash", st.Text, st.Level)
			}
		})
	}
}

func TestNothingIsUnderlinedForADashBeforeTheFirstSubmit(t *testing.T) {
	f := newDashForm()
	f.nameInput.SetValue("--privileged")
	f.Update(testutil.Key("x")) // a change before any submit must not arm validation

	if strings.Contains(f.View(), curlyUnderline) {
		t.Error("a name that starts with a dash is underlined before any submit")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("formStatus().Text = %q before any submit, want empty", st.Text)
	}
}

// Rule 140: after a refusal every change revalidates, so correcting the value
// clears both carriers on the next render, and the submit goes through.
func TestCorrectingADashValueClearsTheUnderlineAndTheFooter(t *testing.T) {
	for _, fld := range dashFields {
		t.Run(fld.name, func(t *testing.T) {
			f := newDashForm()
			focusLaunchField(f, firstFieldOf(f, fld))
			fld.set(f, "-x")
			fld.input(f).CursorEnd()
			f, _ = submitLaunch(f)
			if st := footerStatus(f); st.Text == "" {
				t.Fatal("precondition: the refused submit must set a footer error")
			}

			focusLaunchField(f, firstFieldOf(f, fld))
			f, _ = f.Update(testutil.Key("backspace")) // "-x" -> "-"
			if !strings.Contains(fld.input(f).View(), curlyUnderline) {
				t.Error("a value that still starts with a dash lost its underline")
			}
			f, _ = f.Update(testutil.Key("backspace")) // "-" -> ""

			if strings.Contains(f.View(), curlyUnderline) {
				t.Error("the field is still underlined after the correction")
			}
			if st := footerStatus(f); st.Text != "" {
				t.Errorf("formStatus().Text = %q after the correction, want empty", st.Text)
			}
			if _, ok := submitLaunch(f); !ok {
				t.Error("the submit is still refused after the correction")
			}
		})
	}
}

// firstFieldOf is the index of the focus stop that edits fld.
func firstFieldOf(f *LaunchForm, fld dashField) int {
	switch fld.name {
	case "name":
		return 0
	case "entrypoint":
		return f.fieldEntrypoint()
	case "user":
		return f.fieldUser()
	case "volumes":
		return f.fieldVolumes()
	default:
		return f.fieldExtraPorts()
	}
}

func TestTwoDashValuesUnderlineBothAndTheFooterNamesTheFirstOnScreen(t *testing.T) {
	f := newDashForm()
	f.nameInput.SetValue("-a")
	f.userInput.SetValue("-b")

	f, submitted := submitLaunch(f)

	if submitted {
		t.Fatal("two values that start with a dash were accepted")
	}
	if !strings.Contains(f.nameInput.View(), curlyUnderline) || !strings.Contains(f.userInput.View(), curlyUnderline) {
		t.Error("both faulty fields must be underlined")
	}
	if st := footerStatus(f); st.Text != "Name"+dashSuffix {
		t.Errorf("formStatus().Text = %q, want the first faulty field in screen order (Name)", st.Text)
	}
}

// The network is a cycle field and the image is not a field: the footer carries
// the refusal alone.
func TestANetworkOrAnImageThatStartsWithADashRefusesTheSubmit(t *testing.T) {
	t.Run("network", func(t *testing.T) {
		f := NewLaunchForm("api:v1", nil, []docker.Network{{Name: "-evil"}}, 90)

		f, submitted := submitLaunch(f)

		if submitted {
			t.Fatal("a network named -evil was accepted")
		}
		if st := footerStatus(f); st.Text != netDashMessage || st.Level != sharedcomponents.LevelError {
			t.Errorf("formStatus() = {%q, level %v}, want {%q, LevelError}", st.Text, st.Level, netDashMessage)
		}
	})
	t.Run("image", func(t *testing.T) {
		f := NewLaunchForm("--privileged", nil, nil, 90)

		f, submitted := submitLaunch(f)

		if submitted {
			t.Fatal("an image named --privileged was accepted")
		}
		if st := footerStatus(f); st.Text != imageDashMessage || st.Level != sharedcomponents.LevelError {
			t.Errorf("formStatus() = {%q, level %v}, want {%q, LevelError}", st.Text, st.Level, imageDashMessage)
		}
	})
}

// What must keep working (green today, and after): the fix is "starts with".
func TestValuesThatDoNotStartWithADashAreStillAccepted(t *testing.T) {
	f := NewLaunchForm("api:v1", []string{"80/tcp"}, []docker.Network{{Name: "my-net"}}, 90)
	f.nameInput.SetValue("  my-app-1 ")
	f.entrypointInput.SetValue(`sh -c "x" -y`)
	f.userInput.SetValue("1000:1000")
	f.envInput.SetValue("API_TOKEN=-secret, FLAG=--privileged")
	f.volInput.SetValue("./data:/d, ../x:/x, ~/p:/p, /srv/p:/p:ro, pg-data:/v, .:/dot, type=bind,source=-x,target=/t")
	f.extraPortsInput.SetValue("8080:80, 127.0.0.1:9090:90")

	opts, ok := submitOpts(f)

	if !ok {
		t.Fatalf("the form refused valid values: %q", footerStatus(f).Text)
	}
	if opts.Name != "my-app-1" || opts.Entrypoint != `sh -c "x" -y` || opts.User != "1000:1000" || opts.Network != "my-net" {
		t.Errorf("Opts = %#v, want the trimmed values as typed", opts)
	}
	if want := []string{"API_TOKEN=-secret", "FLAG=--privileged"}; !slices.Equal(opts.Env, want) {
		t.Errorf("Opts.Env = %q, want %q: a value that starts with a dash is a value", opts.Env, want)
	}
	if want := []string{"./data:/d", "../x:/x", "~/p:/p", "/srv/p:/p:ro", "pg-data:/v", ".:/dot"}; !slices.Equal(opts.Volumes, want) {
		t.Errorf("Opts.Volumes = %q, want %q", opts.Volumes, want)
	}
	if want := []string{"type=bind,source=-x,target=/t"}; !slices.Equal(opts.Mounts, want) {
		t.Errorf("Opts.Mounts = %q, want %q", opts.Mounts, want)
	}
}

// An environment name that starts with a dash was refused before OC-21 and still
// is (ValidEnvName): characterization.
func TestAnEnvironmentNameThatStartsWithADashStaysRefused(t *testing.T) {
	f := newDashForm()
	f.envInput.SetValue("-X=1")

	if _, ok := submitOpts(f); ok {
		t.Error("an environment variable named -X was accepted")
	}
}

// ── The launch, through the router ───────────────────────────────────────────

// A launch with a dash value starts nothing, keeps the form open on the
// field, and puts the Error in the footer.
func TestADashValueInTheModelStartsNothing(t *testing.T) {
	forEachLaunchEngine(t, func(t *testing.T, name string) {
		for _, fld := range dashFields {
			t.Run(fld.name, func(t *testing.T) {
				r := newMountRig(t, name)
				fld.set(r.m.launchForm, "--privileged")

				r.submit(t)

				if runs := r.runs(); len(runs) != 0 {
					t.Fatalf("the engine was started with a flag-like %s: %v", fld.name, runs)
				}
				if r.m.launchForm == nil {
					t.Fatal("the form closed on a refused launch")
				}
				if !strings.Contains(fld.input(r.m.launchForm).View(), curlyUnderline) {
					t.Errorf("the %s field is not underlined", fld.name)
				}
				if line := r.footerLine(); !strings.Contains(line, fld.label+dashSuffix) {
					t.Errorf("the rendered footer is %q, want %q", line, fld.label+dashSuffix)
				}
				if st := r.m.formStatus(); st.Level != sharedcomponents.LevelError {
					t.Errorf("footer level = %v, want LevelError", st.Level)
				}
			})
		}
	})
}

// submit goes to the Launch button and presses enter, settling after.
func (r *mountRig) submit(t *testing.T) {
	t.Helper()
	focusLaunchField(r.m.launchForm, r.m.launchForm.fieldSubmit())
	r.press(t, "enter")
}

// Criterion 4: the entrypoint `sh -c "x"` is one element after --entrypoint and
// nothing else of it is an argv element, on both engines. (Green today.)
func TestAnEntrypointWithWordsReachesTheEngineAsOneElement(t *testing.T) {
	const ep = `sh -c "x"`
	forEachLaunchEngine(t, func(t *testing.T, name string) {
		r := newMountRig(t, name)
		r.m.launchForm.entrypointInput.SetValue("  " + ep + "  ")

		r.submit(t)

		runs := r.runs()
		if len(runs) != 1 {
			t.Fatalf("the engine saw %v, want one run", runs)
		}
		argv := runs[0]
		if !containsPair(argv, "--entrypoint", ep) {
			t.Errorf("argv = %q, want --entrypoint followed by the one element %q", argv, ep)
		}
		if argv[len(argv)-1] != "api:v1" {
			t.Errorf("argv = %q, want the image last: no word of the entrypoint after it", argv)
		}
		for _, word := range []string{"-c", `"x"`, "x", "sh"} {
			if slices.Contains(argv, word) {
				t.Errorf("argv = %q holds %q as an element of its own", argv, word)
			}
		}
	})
}

// ── The property, form side ──────────────────────────────────────────────────

var (
	dashPool = []string{
		"--privileged", "-privileged", "--pid=host", "--cap-add=SYS_ADMIN", "--userns=host", "--ipc=host",
		"--uts=host", "--security-opt=seccomp=unconfined", "--device=/dev/mem", "-v", "-e", "--", "-", "-x",
	}
	plainPool = []string{
		" ", "host", "a b", "'; id", "$(id)", "x y z", "é", "SYS_ADMIN", "ALL", "type=bind,source=-x,target=/t",
		"./d:/d", "~/d:/d", "/etc:/x", "name:/p", "8080:80", "0.0.0.0:80:80", "sh -c x", "my-app",
	}
	propertyNetworks = []string{"bridge", "host", " HOST ", "container:abc", "-evil"}
)

// For every combination of fields drawn from a fixed seed, a value that starts
// with a dash never gets through the form, a submit that gets through hands the
// launcher options it accepts, and the argv it builds holds no flag-like value
// and no privileged option. A refusal always comes with a footer Error.
func TestAnyCombinationOfFormFieldsIsSafe(t *testing.T) {
	const iterations = 300
	useEngineNamed(t, engine.Docker)
	installFakeTools(t, fakeScript{"docker": {}}, "docker")
	r := rand.New(rand.NewPCG(21, 819))
	accepted, refused := 0, 0

	pick := func(dash bool) string {
		if dash && r.IntN(2) == 0 {
			return dashPool[r.IntN(len(dashPool))]
		}
		return plainPool[r.IntN(len(plainPool))]
	}
	field := func(dash bool) string {
		switch n := r.IntN(10); {
		case n < 3:
			return ""
		case n < 7:
			return pick(dash)
		default:
			return pick(dash) + []string{",", ", ", " "}[r.IntN(3)] + pick(dash)
		}
	}

	for i := range iterations {
		dash := i%2 == 0
		f := NewLaunchForm("api:v1", []string{"80/tcp"}, networksNamed(propertyNetworks), 90)
		f.nameInput.SetValue(field(dash))
		f.entrypointInput.SetValue(field(dash))
		f.userInput.SetValue(field(dash))
		f.volInput.SetValue(field(dash))
		f.extraPortsInput.SetValue(field(dash))
		f.networkIdx = r.IntN(len(f.networks))
		f.optionRemove = r.IntN(2) == 0

		opts, ok := submitOpts(f)

		if !ok {
			refused++
			st := footerStatus(f)
			if st.Level != sharedcomponents.LevelError || st.Text == "" {
				t.Fatalf("a refused submit says nothing at level Error: %+v (name %q entrypoint %q user %q volumes %q ports %q network %q)",
					st, f.nameInput.Value(), f.entrypointInput.Value(), f.userInput.Value(), f.volInput.Value(),
					f.extraPortsInput.Value(), f.networks[f.networkIdx])
			}
			if strings.Contains(st.Text, "--") {
				t.Fatalf("the footer %q repeats a typed value", st.Text)
			}
			continue
		}
		accepted++
		for _, v := range slices.Concat([]string{opts.Name, opts.Entrypoint, opts.User, opts.Image, opts.Network},
			opts.Volumes, opts.Mounts, opts.Ports) {
			if strings.HasPrefix(v, "-") {
				t.Fatalf("the form handed the launcher %q, which reads as a flag: %#v", v, opts)
			}
		}
		cmd, err := docker.BuildLaunchCmd(opts)
		if err != nil {
			t.Fatalf("the form accepted what the launcher refuses (%v): %#v", err, opts)
		}
		if problem := flagLikeElement(cmd.Args[1:]); problem != "" {
			t.Fatalf("%s: argv %q from %#v", problem, cmd.Args[1:], opts)
		}
	}
	if accepted < 20 || refused < 20 {
		t.Errorf("%d submits accepted and %d refused out of %d draws, want at least 20 of each", accepted, refused, iterations)
	}
}

func networksNamed(names []string) []docker.Network {
	out := make([]docker.Network, len(names))
	for i, n := range names {
		out[i] = docker.Network{Name: n}
	}
	return out
}

// flagLikeElement reports a privileged option, or a value that reads as a flag,
// in an argv built by the launcher.
func flagLikeElement(argv []string) string {
	valueFlags := []string{"--name", "--entrypoint", "-p", "-e", "-v", "--mount", "--network", "--user"}
	privileged := []string{"--privileged", "--pid", "--userns", "--ipc", "--uts", "--cap-add", "--security-opt", "--device"}
	for i, e := range argv {
		for _, p := range privileged {
			if strings.HasPrefix(e, p) {
				return "the argv holds " + e
			}
		}
		if i > 0 && slices.Contains(valueFlags, argv[i-1]) && strings.HasPrefix(e, "-") {
			return "the value of " + argv[i-1] + " reads as a flag: " + e
		}
	}
	if n := len(argv); n > 0 && strings.HasPrefix(argv[n-1], "-") {
		return "the image reads as a flag"
	}
	return ""
}
