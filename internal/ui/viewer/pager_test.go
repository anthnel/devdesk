package viewer

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// OC-84 (DEVDESK-801, requirement S1). `V` no longer asks the source for a
// finished `*exec.Cmd` that runs a shell; it asks for the command whose output
// is paged, and DevDesk pipes it into the pager it resolved itself.
//
// The contract these tests expect from internal/viewer.Pageable:
//
//	type Pageable interface {
//		Source
//		// PagerFeed returns the command whose combined output is paged, built
//		// as an argv and not started. An error means the source refuses to
//		// start it (a malformed container ID); nothing is launched.
//		PagerFeed() (*exec.Cmd, error)
//	}
//
// and from this package, on `V`:
//   - a refused feed ends in the footer, level Error, as "Pager refused — <the
//     source's error>", and no process is handed the terminal;
//   - a pager that cannot be resolved (PAGER, or app.pager, with an
//     unterminated quote) ends in the footer, level Error, naming the pager;
//   - otherwise teaexec.Pipe(feed, pager, ...) is returned, the pager being
//     pager.Resolve(cfg.App.Pager, os.Getenv("PAGER"), runtime.GOOS).
//
// feedSource embeds capableSource, which still carries the old PagerCmd: the
// new interface does not ask for it, so the type is Pageable under both.

// feedSource is a source whose pager feed the test chooses.
type feedSource struct {
	capableSource
	feed *exec.Cmd
	err  error
}

func (s feedSource) PagerFeed() (*exec.Cmd, error) { return s.feed, s.err }

// pagersOnPath makes the named programs findable: PATH becomes a directory of
// empty executables, so openPager's look-up of the pager succeeds without any
// real pager being started. It returns the directory.
func pagersOnPath(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		files := []string{name}
		if runtime.GOOS == "windows" {
			files = append(files, name+".exe")
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f), nil, 0o755); err != nil { //nolint:gosec // a stub the test must be able to look up
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func feedModel(t *testing.T, feed *exec.Cmd, err error) Model {
	t.Helper()
	// A pager that is not found is refused before the feed is asked for; these
	// tests are about the feed and the pager line, so the usual ones exist.
	pagersOnPath(t, "less", "less;", "most")
	return open(t, feedSource{
		capableSource: capableSource{fakeSource: fakeSource{
			name: "logs · api", kind: viewerpkg.KindLog, content: "a\nb\n",
		}},
		feed: feed,
		err:  err,
	})
}

func TestPagerKeyRefusesWhenTheSourceRefusesTheFeed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"a malformed container ID", errors.New(`docker logs: "abc; rm -rf ~" is not a container ID: invalid container ID`),
			[]string{"refused", "invalid container ID"}},
		{"any other refusal carries its reason", errors.New("no engine"), []string{"refused", "no engine"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			m := feedModel(t, nil, tt.err)

			m, cmd := step(t, m, testutil.Key("V"))

			if got := m.footer.Level(); got != components.LevelError {
				t.Errorf("footer level = %v, want Error", got)
			}
			text := strings.ToLower(m.footer.Text())
			for _, want := range tt.want {
				if !strings.Contains(text, strings.ToLower(want)) {
					t.Errorf("footer = %q, want it to contain %q", m.footer.Text(), want)
				}
			}
			if cmd == nil {
				t.Fatal("no command: the footer message would never clear")
			}
			for _, msg := range testutil.Msgs(cmd) {
				if _, ok := msg.(components.ClearFooterMsg); !ok {
					t.Errorf("V returned %T besides the footer's expiry: something was handed the terminal", msg)
				}
			}
		})
	}
}

// A pager line DevDesk cannot read is reported and nothing runs: guessing
// another pager would run a program the user did not pick.
func TestPagerKeyRefusesAnUnreadablePager(t *testing.T) {
	for _, line := range []string{`"less -R`, `'less`, `less "-R`} {
		t.Run(line, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			t.Setenv("PAGER", line)
			m := feedModel(t, exec.Command("echo", "never started"), nil)

			m, cmd := step(t, m, testutil.Key("V"))

			if got := m.footer.Level(); got != components.LevelError {
				t.Errorf("footer level = %v, want Error", got)
			}
			if text := strings.ToLower(m.footer.Text()); !strings.Contains(text, "pager") {
				t.Errorf("footer = %q, want it to name the pager", m.footer.Text())
			}
			if cmd == nil {
				t.Fatal("no command: the footer message would never clear")
			}
			for _, msg := range testutil.Msgs(cmd) {
				if _, ok := msg.(components.ClearFooterMsg); !ok {
					t.Errorf("V returned %T besides the footer's expiry: something was handed the terminal", msg)
				}
			}
		})
	}
}

// A good feed and a pager with arguments hand over the terminal: a command comes
// back, and the footer stays empty. The command is not run: Bubble Tea does.
func TestPagerKeyHandsOverTheTerminalWithAGoodFeed(t *testing.T) {
	tests := []struct {
		name  string
		pager string
	}{
		{"a pager with an argument", "less -R"},
		{"metacharacters are ordinary characters", "less; touch x"},
		{"no PAGER at all", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PAGER", tt.pager)
			m := feedModel(t, exec.Command("echo", "never started"), nil)

			m, cmd := step(t, m, testutil.Key("V"))

			if cmd == nil {
				t.Fatal("V returned no command")
			}
			if m.footer.IsSet() {
				t.Errorf("footer = %q, want none: nothing was refused", m.footer.Text())
			}
		})
	}
}

// A pager that is not on PATH is reported before the feed is asked for, so no
// engine command is built for it: the source's PagerFeed fails the test if it
// is called. The footer names the pager and the reason, and nothing is handed
// the terminal.
func TestPagerKeyRefusesAPagerThatIsNotFound(t *testing.T) {
	tests := []struct {
		name  string
		pager string
		want  string
	}{
		{"a program that does not exist", "oc84-no-such-pager -R", "oc84-no-such-pager"},
		{"the default pager on an empty PATH", "", "less"},
		{"a shell metacharacter makes the first word a missing program", "less; touch x", "less;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			m := open(t, noFeedSource{capableSource{fakeSource: fakeSource{
				name: "logs · api", kind: viewerpkg.KindLog, content: "a\nb\n",
			}}, t})
			t.Setenv("PATH", t.TempDir()) // no pager, no engine
			t.Setenv("PAGER", tt.pager)

			m, cmd := step(t, m, testutil.Key("V"))

			if got := m.footer.Level(); got != components.LevelError {
				t.Errorf("footer level = %v, want Error", got)
			}
			text := m.footer.Text()
			if !strings.Contains(strings.ToLower(text), "pager") || !strings.Contains(text, tt.want) {
				t.Errorf("footer = %q, want it to name the pager (%q) and say it was not found", text, tt.want)
			}
			if cmd == nil {
				t.Fatal("no command: the footer message would never clear")
			}
			for _, msg := range testutil.Msgs(cmd) {
				if _, ok := msg.(components.ClearFooterMsg); !ok {
					t.Errorf("V returned %T besides the footer's expiry: something was handed the terminal", msg)
				}
			}
		})
	}
}

// noFeedSource fails the test when its feed is asked for.
type noFeedSource struct {
	capableSource
	t *testing.T
}

func (s noFeedSource) PagerFeed() (*exec.Cmd, error) {
	s.t.Helper()
	s.t.Error("PagerFeed was called although the pager could not be found")
	return nil, errors.New("unexpected")
}

// A pager line whose program name is empty (PAGER='""') is refused, naming the
// pager, and nothing runs.
func TestPagerKeyRefusesAnEmptyPagerProgram(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	t.Setenv("PAGER", `""`)
	m := feedModel(t, exec.Command("echo", "never started"), nil)

	m, _ = step(t, m, testutil.Key("V"))

	if got := m.footer.Level(); got != components.LevelError {
		t.Errorf("footer level = %v, want Error", got)
	}
	if text := strings.ToLower(m.footer.Text()); !strings.Contains(text, "pager") {
		t.Errorf("footer = %q, want it to name the pager", m.footer.Text())
	}
}
