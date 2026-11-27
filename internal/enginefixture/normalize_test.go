package enginefixture_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09b2 (DEVDESK-807): a fixture of a `ps` must not change at every recording. The
// CreatedAt column holds the date of the recording to the nanosecond and the status a
// duration. A scenario that declares it (Redaction.FixedTimestamps, Redaction.FixedDurations,
// set by c.normalize in a recording only) gets fixed values in stdout and stderr, never in the
// argv:
//
//	every timestamp   -> 2026-01-01 00:00:00 +0000 UTC, or 2026-01-01T00:00:00Z when it was RFC 3339
//	every duration    -> 5 seconds ("Up 3 minutes" -> "Up 5 seconds", "Up Less than a second" ->
//	                     "Up 5 seconds", "Created About an hour ago" -> "Created 5 seconds ago")
//
// The forms are the ones the Comparer of the contract (Tolerating) reads, so a verification of
// a fixed fixture against a real output of another day passes. The header names the categories
// ("durations", "timestamps"). Nothing is replaced unless declared.

const (
	fixedGoTime  = "2026-01-01 00:00:00 +0000 UTC"
	fixedRFCTime = "2026-01-01T00:00:00Z"
)

func psRow(created, status string) string {
	return "a1b2c3d4e5f6\tweb\talpine\trunning\t" + status + "\t" + created + "\t\n"
}

func TestRedactFixesTheTimestampsWhenDeclared(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"the CreatedAt of ps", "x\t2026-11-27 12:34:56.123456789 +0100 CET\ty", "x\t" + fixedGoTime + "\ty"},
		{"a monotonic reading", "2026-11-27 12:00:00.5 +0000 UTC m=+0.012345678", fixedGoTime},
		{"RFC 3339 keeps its form", "at 2026-11-27T12:34:56.5+01:00 done", "at " + fixedRFCTime + " done"},
		{"two of them", "2026-11-27 12:00:00 +0000 UTC 2026-11-28 01:02:03 +0000 UTC", fixedGoTime + " " + fixedGoTime},
		{"no timestamp", "Up 3 seconds", "Up 3 seconds"},
		{"a date alone", "2026-11-27", "2026-11-27"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := enginefixture.Exchange{Argv: []string{"ps"}, Stdout: []byte(tt.in), Stderr: []byte(tt.in)}

			got, header := enginefixture.Redact(ex, enginefixture.Redaction{FixedTimestamps: true})

			if string(got.Stdout) != tt.want || string(got.Stderr) != tt.want {
				t.Errorf("stdout = %q, stderr = %q, want %q", got.Stdout, got.Stderr, tt.want)
			}
			if wantHeader := map[bool]string{true: "timestamps", false: "none"}[tt.in != tt.want]; header != wantHeader {
				t.Errorf("header = %q, want %q", header, wantHeader)
			}
		})
	}
}

func TestRedactFixesTheDurationsWhenDeclared(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Up 3 seconds (healthy)", "Up 5 seconds (healthy)"},
		{"Up Less than a second", "Up 5 seconds"},
		{"Up About a minute", "Up 5 seconds"},
		{"Up About an hour", "Up 5 seconds"},
		{"Up 1 year", "Up 5 seconds"},
		{"Created 2 minutes ago", "Created 5 seconds ago"},
		{"Exited (137) 3 weeks ago", "Exited (137) 5 seconds ago"},
		{"Up 3 sec", "Up 3 sec"},
		{"Exited (137)", "Exited (137)"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, _ := enginefixture.Redact(enginefixture.Exchange{Stdout: []byte(tt.in)}, enginefixture.Redaction{FixedDurations: true})

			if string(got.Stdout) != tt.want {
				t.Errorf("stdout = %q, want %q", got.Stdout, tt.want)
			}
		})
	}
}

func TestRedactFixesNothingUnlessDeclared(t *testing.T) {
	ex := enginefixture.Exchange{Argv: []string{"ps"}, Stdout: []byte(psRow("2026-11-27 12:00:00 +0000 UTC", "Up 3 seconds"))}

	got, header := enginefixture.Redact(ex, enginefixture.Redaction{})

	if string(got.Stdout) != string(ex.Stdout) || header != "none" {
		t.Errorf("stdout = %q, header = %q, want nothing replaced", got.Stdout, header)
	}
	onlyTimes, _ := enginefixture.Redact(ex, enginefixture.Redaction{FixedTimestamps: true})
	if !strings.Contains(string(onlyTimes.Stdout), "Up 3 seconds") {
		t.Errorf("stdout = %q: the durations are fixed only when asked", onlyTimes.Stdout)
	}
}

func TestRedactNeverFixesTheArgv(t *testing.T) {
	ex := enginefixture.Exchange{Argv: []string{"run", "--label", "since=2026-11-27T12:00:00Z", "sleep", "3 seconds"}}

	got, _ := enginefixture.Redact(ex, enginefixture.Redaction{FixedTimestamps: true, FixedDurations: true})

	if strings.Join(got.Argv, " ") != strings.Join(ex.Argv, " ") {
		t.Errorf("argv = %q, want it unchanged", got.Argv)
	}
}

func TestRedactNamesTheFixedCategoriesInTheHeader(t *testing.T) {
	ex := enginefixture.Exchange{Argv: []string{"ps"}, Stdout: []byte(psRow("2026-11-27 12:00:00 +0000 UTC", "Up 3 seconds") + "run-xyz\n")}

	_, both := enginefixture.Redact(ex, enginefixture.Redaction{FixedTimestamps: true, FixedDurations: true})
	_, withRun := enginefixture.Redact(ex, enginefixture.Redaction{FixedTimestamps: true, FixedDurations: true, RunID: "run-xyz"})

	if both != "durations, timestamps" || withRun != "durations, run, timestamps" {
		t.Errorf("headers = %q and %q, want the categories sorted", both, withRun)
	}
}

// Two recordings made days apart give the same fixture; and the same fixture is what a
// second pass gives.
func TestRedactFixedValuesDoNotDependOnTheDayOfTheRecording(t *testing.T) {
	r := enginefixture.Redaction{FixedTimestamps: true, FixedDurations: true}
	monday := enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte(psRow("2026-11-23 08:00:00.123456789 +0000 UTC", "Up Less than a second (healthy)"))}
	friday := enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte(psRow("2026-11-27 17:45:10.987654321 +0000 UTC", "Up 4 seconds (healthy)"))}

	a, _ := enginefixture.Redact(monday, r)
	b, _ := enginefixture.Redact(friday, r)
	twice, _ := enginefixture.Redact(a, r)

	if string(a.Stdout) != string(b.Stdout) || string(twice.Stdout) != string(a.Stdout) {
		t.Errorf("fixtures differ: %q, %q, and %q after a second pass", a.Stdout, b.Stdout, twice.Stdout)
	}
}

func TestRedactFixedValuesDoNotChangeTheInput(t *testing.T) {
	ex := enginefixture.Exchange{Stdout: []byte("2026-11-27 12:00:00 +0000 UTC Up 3 seconds"), Stderr: []byte("3 seconds")}

	enginefixture.Redact(ex, enginefixture.Redaction{FixedTimestamps: true, FixedDurations: true})

	if string(ex.Stdout) != "2026-11-27 12:00:00 +0000 UTC Up 3 seconds" || string(ex.Stderr) != "3 seconds" {
		t.Errorf("the input changed: %q %q", ex.Stdout, ex.Stderr)
	}
}
