package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b1 (DEVDESK-807), helper (3): two Comparers for the outputs that carry a
// value that moves with the clock. `ps` prints how long a container has been up or
// how long ago it was created or stopped ("Up 3 seconds", "Created 2 minutes ago",
// "Exited (137) 1 second ago", "Up About a minute"), and a status carries the health
// of the container ("(healthy)", "(health: starting)", "(starting)"), which depends
// on when it is read. The argv and the exit code are compared exactly before a
// Comparer is asked (Session.Verify), so a Comparer only judges the two streams.
//
//	// SameUpToDurations: the streams are equal once every duration an engine prints
//	// is replaced by one placeholder, on both sides. Nothing else is tolerated: the
//	// number in "Exited (137)", a port, a name, the word "ago", a health state.
//	func SameUpToDurations() Comparer
//
//	// SameUpToDurationsAndHealth: the same, and the health marker of a status
//	// ("(starting)", "(health: starting)", "(healthy)", "(unhealthy)") is one
//	// placeholder too. A status with a marker and a status without one still differ.
//	func SameUpToDurationsAndHealth() Comparer
//
// A duration is the word the engines print for one: a number and a unit in the
// singular or the plural (second, minute, hour, day, week, month, year), "Less than
// a second", "About a minute", "About an hour".

func streams(stdout, stderr string) enginefixture.Exchange {
	return enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte(stdout), Stderr: []byte(stderr)}
}

type durationCase struct {
	name     string
	recorded enginefixture.Exchange
	got      enginefixture.Exchange
}

// agreeOnDurations are pairs that differ only by a duration: both Comparers accept them.
func agreeOnDurations() []durationCase {
	psRow := func(status string) string {
		return "a1b2c3d4e5f6\tweb\talpine\trunning\t" + status + "\t2026-11-27 10:00:00 +0000 UTC\n"
	}
	return []durationCase{
		{"identical", streams("Up 3 seconds\n", ""), streams("Up 3 seconds\n", "")},
		{"seconds", streams("Up 3 seconds\n", ""), streams("Up 5 seconds\n", "")},
		{"one second, in the singular", streams("Up 1 second\n", ""), streams("Up 17 seconds\n", "")},
		{"less than a second and a second", streams("Up Less than a second\n", ""), streams("Up 1 second\n", "")},
		{"about a minute and minutes", streams("Up About a minute\n", ""), streams("Up 2 minutes\n", "")},
		{"an hour", streams("Up 1 hour\n", ""), streams("Up About an hour\n", "")},
		{"days and weeks", streams("Up 2 days\n", ""), streams("Up 3 weeks\n", "")},
		{"months and years", streams("Up 3 months\n", ""), streams("Up 1 year\n", "")},
		{"a creation time", streams("Created 2 minutes ago\n", ""), streams("Created Less than a second ago\n", "")},
		{"a stop time, with the exit code of the container", streams("Exited (137) 1 second ago\n", ""), streams("Exited (137) 3 minutes ago\n", "")},
		{"a whole ps row, columns separated by tabs", streams(psRow("Up 3 seconds (healthy)"), ""), streams(psRow("Up 11 seconds (healthy)"), "")},
		{"two rows, one duration each", streams(psRow("Up 3 seconds")+psRow("Exited (0) 4 minutes ago"), ""),
			streams(psRow("Up 9 minutes")+psRow("Exited (0) 1 hour ago"), "")},
		{"a podman status", streams("running|Up 2 seconds (starting)\n", ""), streams("running|Up Less than a second (starting)\n", "")},
		{"the default table, columns separated by two spaces", streams("web  alpine  Up 3 seconds  \n", ""), streams("web  alpine  Up 4 seconds  \n", "")},
		{"a duration in stderr", streams("", "Error: stopped 3 seconds ago\n"), streams("", "Error: stopped 4 seconds ago\n")},
		{"a duration in each stream", streams("Up 3 seconds\n", "warning: 5 minutes ago\n"), streams("Up 8 seconds\n", "warning: 9 minutes ago\n")},
	}
}

// differByMoreThanDurations are pairs that a duration-only Comparer refuses.
func differByMoreThanDurations() []durationCase {
	return []durationCase{
		{"up and exited, with the same duration", streams("Up 3 seconds\n", ""), streams("Exited (0) 3 seconds ago\n", "")},
		{"another exit code", streams("Exited (137) 1 second ago\n", ""), streams("Exited (1) 1 second ago\n", "")},
		{"the word ago is missing", streams("Created 2 minutes ago\n", ""), streams("Created 2 minutes\n", "")},
		{"a number with no unit", streams("Up 3 seconds\n", ""), streams("Up 3\n", "")},
		{"a unit the engines never print", streams("Up 3 sec\n", ""), streams("Up 4 sec\n", "")},
		{"a published port", streams("127.0.0.1:18080->80/tcp\n", ""), streams("127.0.0.1:18081->80/tcp\n", "")},
		{"another name, same duration", streams("web  Up 3 seconds\n", ""), streams("db  Up 3 seconds\n", "")},
		{"an extra line", streams("Up 3 seconds\n", ""), streams("Up 3 seconds\nUp 4 seconds\n", "")},
		{"a missing line", streams("Up 3 seconds\nUp 4 seconds\n", ""), streams("Up 3 seconds\n", "")},
		{"another stderr", streams("", "Error: a\n"), streams("", "Error: b\n")},
		{"nothing against something", streams("", ""), streams("Up 3 seconds\n", "")},
		{"a trailing byte", streams("Up 3 seconds\n", ""), streams("Up 3 seconds\n\x00", "")},
	}
}

func TestSameUpToDurationsAcceptsWhatDiffersOnlyByADuration(t *testing.T) {
	for _, comparer := range []struct {
		name string
		cmp  enginecontract.Comparer
	}{
		{"SameUpToDurations", enginecontract.SameUpToDurations()},
		{"SameUpToDurationsAndHealth", enginecontract.SameUpToDurationsAndHealth()},
	} {
		for _, tt := range agreeOnDurations() {
			t.Run(comparer.name+"/"+tt.name, func(t *testing.T) {
				if err := comparer.cmp(tt.recorded, tt.got); err != nil {
					t.Errorf("%s(%q, %q) = %v, want nil", comparer.name, tt.recorded.Stdout, tt.got.Stdout, err)
				}
				// The comparison is symmetric.
				if err := comparer.cmp(tt.got, tt.recorded); err != nil {
					t.Errorf("%s with the exchanges swapped = %v, want nil", comparer.name, err)
				}
			})
		}
	}
}

func TestSameUpToDurationsRefusesEverythingElse(t *testing.T) {
	for _, comparer := range []struct {
		name string
		cmp  enginecontract.Comparer
	}{
		{"SameUpToDurations", enginecontract.SameUpToDurations()},
		{"SameUpToDurationsAndHealth", enginecontract.SameUpToDurationsAndHealth()},
	} {
		for _, tt := range differByMoreThanDurations() {
			t.Run(comparer.name+"/"+tt.name, func(t *testing.T) {
				err := comparer.cmp(tt.recorded, tt.got)

				if err == nil {
					t.Errorf("%s(%q, %q) = nil, want a difference", comparer.name, tt.recorded.Stdout, tt.got.Stdout)
				}
			})
		}
	}
}

// The health of a status is a value a duration-only Comparer still judges.
func TestSameUpToDurationsStillSeesTheHealthState(t *testing.T) {
	tests := []struct {
		name     string
		recorded string
		got      string
	}{
		{"healthy and unhealthy", "Up 3 seconds (healthy)\n", "Up 3 seconds (unhealthy)\n"},
		{"docker's starting and healthy", "Up 3 seconds (health: starting)\n", "Up 3 seconds (healthy)\n"},
		{"podman's starting and unhealthy", "Up 3 seconds (starting)\n", "Up 3 seconds (unhealthy)\n"},
		{"a marker and none", "Up 3 seconds (healthy)\n", "Up 3 seconds\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := enginecontract.SameUpToDurations()(streams(tt.recorded, ""), streams(tt.got, "")); err == nil {
				t.Errorf("SameUpToDurations(%q, %q) = nil, want a difference", tt.recorded, tt.got)
			}
		})
	}
}

func TestSameUpToDurationsAndHealthAcceptsAnyHealthMarker(t *testing.T) {
	tests := []struct {
		name     string
		recorded string
		got      string
	}{
		{"docker's starting and healthy", "Up 3 seconds (health: starting)\n", "Up 10 seconds (healthy)\n"},
		{"podman's starting and unhealthy", "Up 3 seconds (starting)\n", "Up 3 seconds (unhealthy)\n"},
		{"docker's starting and podman's starting", "Up 3 seconds (health: starting)\n", "Up 3 seconds (starting)\n"},
		{"healthy and unhealthy", "Up 3 seconds (healthy)\n", "Up 3 seconds (unhealthy)\n"},
		{"a marker on a stopped container", "Exited (137) 1 second ago (starting)\n", "Exited (137) 4 seconds ago (healthy)\n"},
		{"a column that holds the marker alone", "healthy\t(healthy)\n", "healthy\t(health: starting)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := enginecontract.SameUpToDurationsAndHealth()(streams(tt.recorded, ""), streams(tt.got, "")); err != nil {
				t.Errorf("SameUpToDurationsAndHealth(%q, %q) = %v, want nil", tt.recorded, tt.got, err)
			}
		})
	}
}

func TestSameUpToDurationsAndHealthStillSeesWhatIsNotHealth(t *testing.T) {
	tests := []struct {
		name     string
		recorded string
		got      string
	}{
		{"a marker and none: one container has a healthcheck, the other not", "Up 3 seconds (healthy)\n", "Up 3 seconds\n"},
		{"up and exited", "Up 3 seconds (healthy)\n", "Exited (137) 3 seconds ago (healthy)\n"},
		{"a word between parentheses that is not a health state", "Up 3 seconds (unknown)\n", "Up 3 seconds (healthy)\n"},
		{"the exit code between parentheses", "Exited (137) 1 second ago\n", "Exited (1) 1 second ago\n"},
		{"another name", "web\tUp 3 seconds (healthy)\n", "db\tUp 3 seconds (healthy)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := enginecontract.SameUpToDurationsAndHealth()(streams(tt.recorded, ""), streams(tt.got, "")); err == nil {
				t.Errorf("SameUpToDurationsAndHealth(%q, %q) = nil, want a difference", tt.recorded, tt.got)
			}
		})
	}
}

// The error names the stream that differs, so a failing contract says where to look.
func TestSameUpToDurationsNamesTheStreamThatDiffers(t *testing.T) {
	tests := []struct {
		name     string
		recorded enginefixture.Exchange
		got      enginefixture.Exchange
		want     string
		notWant  string
	}{
		{"stdout", streams("web\tUp 3 seconds\n", ""), streams("db\tUp 3 seconds\n", ""), "stdout", "stderr"},
		{"stderr", streams("", "Error: a\n"), streams("", "Error: b\n"), "stderr", "stdout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enginecontract.SameUpToDurations()(tt.recorded, tt.got)

			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), tt.notWant) {
				t.Errorf("error = %v, want it to name %s and not %s", err, tt.want, tt.notWant)
			}
		})
	}
}

// A Comparer reads the exchanges and changes nothing in them.
func TestSameUpToDurationsDoesNotChangeItsInputs(t *testing.T) {
	recorded, got := streams("Up 3 seconds (healthy)\n", "warn 5 minutes ago\n"), streams("Up 9 seconds (healthy)\n", "warn 6 minutes ago\n")

	for _, cmp := range []enginecontract.Comparer{enginecontract.SameUpToDurations(), enginecontract.SameUpToDurationsAndHealth()} {
		if err := cmp(recorded, got); err != nil {
			t.Fatalf("comparer = %v, want nil", err)
		}
	}

	if string(recorded.Stdout) != "Up 3 seconds (healthy)\n" || string(got.Stdout) != "Up 9 seconds (healthy)\n" ||
		string(recorded.Stderr) != "warn 5 minutes ago\n" || string(got.Stderr) != "warn 6 minutes ago\n" {
		t.Errorf("the exchanges changed: recorded %q %q, got %q %q", recorded.Stdout, recorded.Stderr, got.Stdout, got.Stderr)
	}
}

// Plugged into Verify: a fixture whose status read "Up 3 seconds (healthy)" is
// matched by a run that read "Up 11 seconds (healthy)", and not with no Comparer.
func TestVerifyWithADurationComparer(t *testing.T) {
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Scenario, fixture.Name = "ps-health", "ps-health--podman-6.2.1-rootless"
	fixture.Exchange = enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte("web\tUp 3 seconds (healthy)\n")}
	run := enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte("web\tUp 11 seconds (healthy)\n")}
	check := func(cmp enginecontract.Comparer) *recT {
		rt := &recT{TB: t}
		rt.run(func() {
			verifySession().VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "ps-health", run, enginefixture.Redaction{}, cmp)
		})
		return rt
	}

	if rt := check(enginecontract.SameUpToDurations()); !rt.quiet() {
		t.Errorf("a run that differs by a duration was reported: %s", rt.text())
	}
	if rt := check(nil); len(rt.errs) == 0 {
		t.Error("with no Comparer the same runs agree: the test proves nothing")
	}
	changed := enginefixture.Exchange{Argv: []string{"ps", "-a", "--no-trunc"}, Stdout: run.Stdout}
	rt := &recT{TB: t}
	rt.run(func() {
		verifySession().VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "ps-health", changed, enginefixture.Redaction{}, enginecontract.SameUpToDurations())
	})
	if len(rt.errs) == 0 || !strings.Contains(strings.Join(rt.errs, "\n"), "argv") {
		t.Errorf("reports = %q, want the argv difference: a Comparer tolerates durations in the streams only", rt.errs)
	}
}
