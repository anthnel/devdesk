package viewer

import (
	"errors"
	"os/exec"
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

func feedModel(t *testing.T, feed *exec.Cmd, err error) Model {
	t.Helper()
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
