package enginefixture_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08a step 4, the matching half (criteria 1 and 5): a Replay answers an argv
// from a selection of fixtures, exactly, and remembers what it answered.
//
// The key is the tool and the argv, element by element and in order. Version and
// mode are not in the key: the test chooses its configuration (ForConfig).

// fx builds a fixture for a replay test; the name is what failures cite.
func fx(name string, tool enginefixture.Tool, argv ...string) enginefixture.Fixture {
	return enginefixture.Fixture{
		Name: name, Scenario: name,
		Meta: enginefixture.Meta{
			Tool: tool, Version: "29.10.1", Mode: enginefixture.Rootful,
			Recorded: "2026-11-27", Scenario: name, Redacted: "none", RecordedBy: "synthetic",
		},
		Exchange: enginefixture.Exchange{Argv: argv, Exit: 1, Stdout: []byte(name + "-out"), Stderr: []byte(name + "-err")},
	}
}

func TestNewReplayRefusesASelectionItCannotServe(t *testing.T) {
	pending := fx("rm-pending", enginefixture.Docker, "rm", "a")
	pending.Meta.Record = enginefixture.RecordPending

	tests := []struct {
		name     string
		in       []enginefixture.Fixture
		contains []string
	}{
		{"none", nil, nil},
		{"a pending one names itself", []enginefixture.Fixture{fx("ps", enginefixture.Docker, "ps"), pending}, []string{"rm-pending"}},
		{"two with the same tool and argv name both",
			[]enginefixture.Fixture{fx("first", enginefixture.Docker, "rm", "a"), fx("second", enginefixture.Docker, "rm", "a")},
			[]string{"first", "second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := enginefixture.NewReplay(tt.in...)
			if err == nil {
				t.Fatalf("NewReplay accepted the selection: %v", r)
			}
			for _, want := range tt.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// The same argv under docker and under podman are two keys, and a selection may
// hold both: forEachEngine tests choose the tool at run time.
func TestTheSameArgvUnderTwoToolsIsAccepted(t *testing.T) {
	r, err := enginefixture.NewReplay(
		fx("docker-rm", enginefixture.Docker, "rm", "a"),
		fx("podman-rm", enginefixture.Podman, "rm", "a"))
	if err != nil {
		t.Fatalf("NewReplay: %v", err)
	}

	for tool, want := range map[enginefixture.Tool]string{enginefixture.Docker: "docker-rm", enginefixture.Podman: "podman-rm"} {
		got, ok := r.Answer(tool, []string{"rm", "a"})
		if !ok || got.Name != want {
			t.Errorf("Answer(%s) = %q, %v, want %q", tool, got.Name, ok, want)
		}
	}
}

// Version and mode are not part of the key: two fixtures that differ only by
// them collide, and the test must pick one (ForConfig).
func TestVersionAndModeAreNotPartOfTheKey(t *testing.T) {
	a := fx("rm--docker-29.10.1-rootful", enginefixture.Docker, "rm", "a")
	b := fx("rm--docker-28.5.2-rootless", enginefixture.Docker, "rm", "a")
	b.Meta.Version, b.Meta.Mode = "28.5.2", enginefixture.Rootless

	_, err := enginefixture.NewReplay(a, b)

	if err == nil || !strings.Contains(err.Error(), a.Name) || !strings.Contains(err.Error(), b.Name) {
		t.Errorf("NewReplay error = %v, want it to refuse and name both fixtures", err)
	}
}

func TestAnswerMatchesTheExactArgvOnly(t *testing.T) {
	rm := fx("rm", enginefixture.Docker, "rm", "-f", "ddm-run")
	r, err := enginefixture.NewReplay(rm, fx("ps", enginefixture.Docker, "ps", "--all"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		tool enginefixture.Tool
		argv []string
		want string // fixture name, "" for none
	}{
		{"exact", enginefixture.Docker, []string{"rm", "-f", "ddm-run"}, "rm"},
		{"another declared argv", enginefixture.Docker, []string{"ps", "--all"}, "ps"},
		{"shorter", enginefixture.Docker, []string{"rm", "-f"}, ""},
		{"a bare verb", enginefixture.Docker, []string{"rm"}, ""},
		{"longer", enginefixture.Docker, []string{"rm", "-f", "ddm-run", "x"}, ""},
		{"permuted: no flag is reordered", enginefixture.Docker, []string{"rm", "ddm-run", "-f"}, ""},
		{"another element", enginefixture.Docker, []string{"rm", "-f", "ddm-other"}, ""},
		{"a case difference", enginefixture.Docker, []string{"RM", "-f", "ddm-run"}, ""},
		{"a joined word is not two words", enginefixture.Docker, []string{"rm -f", "ddm-run"}, ""},
		{"another tool", enginefixture.Podman, []string{"rm", "-f", "ddm-run"}, ""},
		{"nil argv", enginefixture.Docker, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.Answer(tt.tool, tt.argv)

			if ok != (tt.want != "") || got.Name != tt.want {
				t.Fatalf("Answer(%s, %q) = %q, %v, want %q", tt.tool, tt.argv, got.Name, ok, tt.want)
			}
			if ok && !sameExchange(got.Exchange, map[string]enginefixture.Fixture{"rm": rm, "ps": fx("ps", enginefixture.Docker, "ps", "--all")}[tt.want].Exchange) {
				t.Errorf("Answer returned the wrong exchange: %+v", got.Exchange)
			}
		})
	}
}

// Unused names what was never asked, sorted, and an answered fixture leaves it.
func TestUnusedListsWhatWasNeverAnswered(t *testing.T) {
	r, err := enginefixture.NewReplay(
		fx("zeta", enginefixture.Docker, "z"),
		fx("alpha", enginefixture.Docker, "a"),
		fx("mid", enginefixture.Podman, "m"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Unused(), []string{"alpha", "mid", "zeta"}; !slices.Equal(got, want) {
		t.Errorf("Unused() before any call = %v, want %v (sorted)", got, want)
	}

	r.Answer(enginefixture.Docker, []string{"z"})
	if got, want := r.Unused(), []string{"alpha", "mid"}; !slices.Equal(got, want) {
		t.Errorf("Unused() after zeta = %v, want %v", got, want)
	}

	// A miss does not use anything, and a second answer changes nothing.
	r.Answer(enginefixture.Docker, []string{"nope"})
	r.Answer(enginefixture.Podman, []string{"a"}) // the right argv, the wrong tool
	r.Answer(enginefixture.Docker, []string{"z"})
	if got, want := r.Unused(), []string{"alpha", "mid"}; !slices.Equal(got, want) {
		t.Errorf("Unused() after misses = %v, want %v", got, want)
	}

	r.Answer(enginefixture.Docker, []string{"a"})
	r.Answer(enginefixture.Podman, []string{"m"})
	if got := r.Unused(); len(got) != 0 {
		t.Errorf("Unused() after everything = %v, want none", got)
	}
}

// Answer is safe for concurrent use: the code under test calls the engine from
// several goroutines (Bubble Tea Cmds). Run under -race.
func TestAnswerIsSafeForConcurrentUse(t *testing.T) {
	var in []enginefixture.Fixture
	for i := range 20 {
		in = append(in, fx(fmt.Sprintf("f%02d", i), enginefixture.Docker, "cmd", fmt.Sprint(i)))
	}
	r, err := enginefixture.NewReplay(in...)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				n := (i + g) % 25 // five misses in 25
				got, ok := r.Answer(enginefixture.Docker, []string{"cmd", fmt.Sprint(n)})
				if ok != (n < 20) {
					t.Errorf("Answer(cmd %d) found = %v", n, ok)
				}
				if ok && got.Name != fmt.Sprintf("f%02d", n) {
					t.Errorf("Answer(cmd %d) = %s", n, got.Name)
				}
				_ = r.Unused()
			}
		}()
	}
	wg.Wait()

	if got := r.Unused(); len(got) != 0 {
		t.Errorf("Unused() = %v after every fixture was answered", got)
	}
}

// The returned Fixture is a copy of the data: a caller that edits it does not
// change what the next call answers.
func TestAnswerDoesNotShareItsBytesWithTheReplay(t *testing.T) {
	r, err := enginefixture.NewReplay(fx("rm", enginefixture.Docker, "rm", "a"))
	if err != nil {
		t.Fatal(err)
	}

	first, _ := r.Answer(enginefixture.Docker, []string{"rm", "a"})
	first.Exchange.Stdout[0] = 'X'
	first.Exchange.Argv[0] = "changed"

	second, ok := r.Answer(enginefixture.Docker, []string{"rm", "a"})
	if !ok || string(second.Exchange.Stdout) != "rm-out" {
		t.Errorf("second Answer = %q, %v: the first caller's edit leaked into the replay", second.Exchange.Stdout, ok)
	}
}
