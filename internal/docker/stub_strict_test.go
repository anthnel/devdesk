package docker

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// OC-07 criterion 2: a stub that is told which argv to expect fails the test
// on any other one, and says which.
//
// The stub itself lives in exec_test.go (reply, reporter, stubRunner.replies
// and .report, errUnexpectedArgv, stubStrict). A failure message contains
// fmt.Sprintf("%q", argv), so a reader sees the exact words the code under test
// passed.

// recorder stands in for *testing.T so the failure can be observed instead of
// failing the run.
type recorder struct{ msgs []string }

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func TestStrictStubAcceptsOnlyTheDeclaredArgv(t *testing.T) {
	boom := errors.New("exit status 1")
	declared := []reply{
		{argv: []string{"volume", "ls", "--format", "{{.Name}}"}, out: "pgdata\n"},
		{argv: []string{"volume", "rm", "pgdata"}, err: boom},
	}

	tests := []struct {
		name    string
		argv    []string
		wantOut string
		wantErr func(error) bool
		wantMsg string // substring of the one failure, "" for none
	}{
		{
			name:    "a declared argv gets its canned output",
			argv:    []string{"volume", "ls", "--format", "{{.Name}}"},
			wantOut: "pgdata\n",
			wantErr: func(err error) bool { return err == nil },
		},
		{
			name:    "a declared argv gets its canned error",
			argv:    []string{"volume", "rm", "pgdata"},
			wantErr: func(err error) bool { return errors.Is(err, boom) },
		},
		{
			name:    "an undeclared subcommand fails with its argv",
			argv:    []string{"volume", "prune", "--force"},
			wantErr: func(err error) bool { return errors.Is(err, errUnexpectedArgv) },
			wantMsg: `["volume" "prune" "--force"]`,
		},
		{
			// The old stub keyed on the first word, so this passed whatever
			// template the code asked for. That is the hole OC-07 closes.
			name:    "the right subcommand with another template fails",
			argv:    []string{"volume", "ls", "--format", "{{.Name}}\t{{.Driver}}"},
			wantErr: func(err error) bool { return errors.Is(err, errUnexpectedArgv) },
			wantMsg: `["volume" "ls" "--format" "{{.Name}}\t{{.Driver}}"]`,
		},
		{
			name:    "a declared argv with an extra argument fails",
			argv:    []string{"volume", "rm", "pgdata", "cache"},
			wantErr: func(err error) bool { return errors.Is(err, errUnexpectedArgv) },
			wantMsg: `["volume" "rm" "pgdata" "cache"]`,
		},
		{
			name:    "a bare declared prefix fails",
			argv:    []string{"volume"},
			wantErr: func(err error) bool { return errors.Is(err, errUnexpectedArgv) },
			wantMsg: `["volume"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			s := &stubRunner{replies: declared, report: rec}

			out, err := s.Run(dockerCmd{Args: tt.argv})

			if !tt.wantErr(err) {
				t.Errorf("Run(%q) error = %v", tt.argv, err)
			}
			if string(out) != tt.wantOut {
				t.Errorf("Run(%q) output = %q, want %q", tt.argv, out, tt.wantOut)
			}
			switch {
			case tt.wantMsg == "" && len(rec.msgs) != 0:
				t.Errorf("a declared argv was reported: %v", rec.msgs)
			case tt.wantMsg != "" && len(rec.msgs) != 1:
				t.Fatalf("reports = %v, want exactly one naming the argv", rec.msgs)
			case tt.wantMsg != "" && !strings.Contains(rec.msgs[0], tt.wantMsg):
				t.Errorf("report = %q, want it to contain the argv %s", rec.msgs[0], tt.wantMsg)
			}
			// A rejected call is still on the record, so a failing test can be
			// read from s.calls and lastArgs as well as from the message.
			if !slices.Equal(s.lastArgs(), tt.argv) {
				t.Errorf("lastArgs() = %q, want the call recorded as %q", s.lastArgs(), tt.argv)
			}
		})
	}
}

// Build is the other way in: the launch and exec paths hand the command to the
// terminal instead of running it, and must not slip past the check.
func TestStrictStubChecksBuildToo(t *testing.T) {
	rec := &recorder{}
	s := &stubRunner{
		replies: []reply{{argv: []string{"exec", "-it", "abc", "sh"}}},
		report:  rec,
	}

	s.Build("exec", "-it", "abc", "sh")
	if len(rec.msgs) != 0 {
		t.Errorf("a declared Build was reported: %v", rec.msgs)
	}

	s.Build("exec", "-it", "abc", "bash")
	if len(rec.msgs) != 1 || !strings.Contains(rec.msgs[0], `["exec" "-it" "abc" "bash"]`) {
		t.Errorf("reports = %v, want one naming the undeclared Build argv", rec.msgs)
	}
}

// A stub with no reporter is the one every test not yet migrated builds: keyed
// by subcommand, answering anything. Strictness is opted into, so the migration
// can go test by test.
func TestAStubWithoutAReporterStaysKeyedBySubcommand(t *testing.T) {
	s := &stubRunner{output: map[string][]byte{"volume": []byte("pgdata\n")}}

	out, err := s.Run(dockerCmd{Args: []string{"volume", "anything", "at", "all"}})

	if err != nil || string(out) != "pgdata\n" {
		t.Errorf("Run() = %q, %v, want the subcommand's canned output", out, err)
	}
}

// End to end: strictness exists to catch a template that is wrong for the
// engine. Here podman is asked for `network ls` while the stub only knows
// docker's template, and the test must fail naming that argv.
func TestStrictStubCatchesATemplateOfTheWrongEngine(t *testing.T) {
	rec := &recorder{}
	s := &stubRunner{
		replies: []reply{{argv: []string{"network", "ls", "--format", dockerNetworkLS}}},
		report:  rec,
	}
	stub(t, s)
	useEngine(t, "podman")

	_, _ = ListNetworks()

	if len(rec.msgs) != 1 {
		t.Fatalf("reports = %v, want one for podman's own template", rec.msgs)
	}
	if !strings.Contains(rec.msgs[0], `"{{.ID}}\t{{.Name}}\t{{.Driver}}\t\t{{.Created}}"`) {
		t.Errorf("report = %q, want it to show podman's NetworkLS template", rec.msgs[0])
	}
}

// dockerNetworkLS is written out rather than read from the shape, so that a
// change to docker's template is a visible edit here too.
const dockerNetworkLS = "{{.ID}}\t{{.Name}}\t{{.Driver}}\t{{.Scope}}\t{{.CreatedAt}}"

// capture stands in for the *testing.T given to stubStrict, so the cleanup
// check can be run by hand and its failures read instead of failing the run.
type capture struct {
	testing.TB
	msgs     []string
	cleanups []func()
}

func (c *capture) Errorf(format string, args ...any) {
	c.msgs = append(c.msgs, fmt.Sprintf(format, args...))
}
func (c *capture) Cleanup(fn func()) { c.cleanups = append(c.cleanups, fn) }

// A reply the code never asked for is a stale expectation: the test would keep
// passing on an argv nothing runs any more.
func TestStrictStubFailsOnADeclaredArgvNeverRun(t *testing.T) {
	c := &capture{TB: t}
	s := stubStrict(c,
		reply{argv: []string{"volume", "ls"}},
		reply{argv: []string{"volume", "rm", "pgdata"}},
	)

	_, _ = s.Run(dockerCmd{Args: []string{"volume", "ls"}})
	// End of the test, by hand: last registered first, so the check runs and
	// then the stub puts the runner and the engine back.
	for i := len(c.cleanups) - 1; i >= 0; i-- {
		c.cleanups[i]()
	}

	if len(c.msgs) != 1 || !strings.Contains(c.msgs[0], `["volume" "rm" "pgdata"]`) {
		t.Errorf("reports = %v, want one naming the argv never run", c.msgs)
	}
}
