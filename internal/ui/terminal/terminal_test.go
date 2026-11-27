package terminal

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// The emulators LookPath resolves (cosmic-term, gnome-terminal, ...) are the
// test binary under that name (internal/testutil/fakecli): nothing is started,
// the lookup only has to succeed.
func TestMain(m *testing.M) {
	os.Exit(fakecli.Main(m))
}

// detectionVars are every variable ForCmd reads to pick an emulator. A test
// clears them all, then sets the one of the emulator it exercises, so the
// developer's own terminal never decides the outcome.
var detectionVars = []string{
	"WT_SESSION", "KITTY_WINDOW_ID", "WEZTERM_PANE", "WEZTERM_UNIX_SOCKET",
	"ALACRITTY_SOCKET", "XDG_CURRENT_DESKTOP", "VTE_VERSION", "TERM_PROGRAM",
}

// emulators is one environment per emulator ForCmd supports. Tools lists the
// fake binaries that environment needs on PATH.
var emulators = []struct {
	name  string
	env   map[string]string
	tools []string
	bin   string
}{
	{"Windows Terminal", map[string]string{"WT_SESSION": "1"}, nil, "wt.exe"},
	{"Kitty", map[string]string{"KITTY_WINDOW_ID": "1"}, nil, "kitty"},
	{"WezTerm", map[string]string{"WEZTERM_PANE": "1"}, nil, "wezterm"},
	{"Alacritty", map[string]string{"ALACRITTY_SOCKET": "/tmp/a"}, nil, "alacritty"},
	{"COSMIC Terminal", map[string]string{"XDG_CURRENT_DESKTOP": "COSMIC"}, []string{"cosmic-term"}, "cosmic-term"},
	{"GNOME Terminal", map[string]string{"VTE_VERSION": "7000"}, []string{"gnome-terminal"}, "gnome-terminal"},
	{"XFCE Terminal", map[string]string{"VTE_VERSION": "7000"}, []string{"xfce4-terminal"}, "xfce4-terminal"},
	{"Tilix", map[string]string{"VTE_VERSION": "7000"}, []string{"tilix"}, "tilix"},
}

// OC-18 criterion 3 (non-regression: the code already builds the window this
// way). The variable elements of a shell window, the container ID and the shell,
// reach the emulator as separate argv elements, exactly as given, after the
// emulator's own words. No emulator ForCmd supports takes a command string, so
// none of them joins, quotes or hands the argv to `sh -c`, `cmd /c` or
// `osascript`: the hostile element below comes out untouched, in one piece.
func TestForCmdPassesTheInnerArgvAsSeparateElements(t *testing.T) {
	const id = "0123456789ab"
	inners := map[string][]string{
		"an exec of a shell":   {"/usr/bin/docker", "exec", "-it", id, "/bin/bash"},
		"a hostile element":    {"/usr/bin/docker", "exec", "-it", "x; touch /tmp/pwned $(id) `id`", "/bin/sh"},
		"an element with a sp": {"/usr/bin/docker", "exec", "-it", "has space", "/bin/sh"},
	}

	for _, em := range emulators {
		for label, inner := range inners {
			t.Run(em.name+"/"+label, func(t *testing.T) {
				for _, v := range detectionVars {
					t.Setenv(v, "")
				}
				for k, v := range em.env {
					t.Setenv(k, v)
				}
				fakecli.Install(t, fakecli.Scenario{}, em.tools...)

				bin, args, ok := ForCmd(slices.Clone(inner))

				if !ok {
					t.Fatalf("ForCmd found no emulator for %v", em.env)
				}
				if filepath.Base(bin) != em.bin {
					t.Fatalf("emulator = %q, want %q", bin, em.bin)
				}
				if len(args) < len(inner) || !slices.Equal(args[len(args)-len(inner):], inner) {
					t.Fatalf("args = %q: the inner argv is not its tail, element by element: %q", args, inner)
				}
				for _, w := range args[:len(args)-len(inner)] {
					for _, interpreter := range []string{"sh", "bash", "-c", "/c", "/bin/sh", "cmd", "cmd.exe", "osascript"} {
						if w == interpreter {
							t.Errorf("args = %q: the window goes through %q", args, interpreter)
						}
					}
				}
			})
		}
	}
}

// With no emulator detected nothing is built, so the caller cannot fall back to
// a string-based launch of its own.
func TestForCmdFindsNothingWithoutAnEmulator(t *testing.T) {
	for _, v := range detectionVars {
		t.Setenv(v, "")
	}
	fakecli.Install(t, fakecli.Scenario{})

	bin, args, ok := ForCmd([]string{"/usr/bin/docker", "exec", "-it", "0123456789ab", "/bin/sh"})

	if ok || bin != "" || args != nil {
		t.Errorf("ForCmd() = %q, %q, %v, want nothing", bin, args, ok)
	}
}
