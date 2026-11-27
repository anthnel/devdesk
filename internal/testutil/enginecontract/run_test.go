package enginecontract_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// The API these tests expect:
//
//	func ValidRunID(id string) bool                               // ^[a-z0-9][a-z0-9-]{5,31}$
//	func NewRunID(random io.Reader, now time.Time) string         // "20261127-a1b2c3d4": date in UTC, 4 random bytes in hex
//	func ProcessRunID(getenv func(string) string) (string, error) // RunEnv if valid, an error if set and invalid, else one generated id per process
//	func Label(runID string) string                               // "devdesk.test=<runID>"
//	func ResourceName(runID, role string) string                  // "ddc-<runID>-<role>", role ^[a-z0-9]{1,16}$, panics otherwise

func TestValidRunID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"20261127-a1b2c3d4", true},
		// The placeholder a recorded fixture carries (OC-09a3) must be replayable in a name.
		{"fixture-run", true},
		{"abcdef", true},                     // 6 characters, the shortest
		{strings.Repeat("a", 32), true},      // the longest
		{"abcde", false},                     // 5
		{strings.Repeat("a", 33), false},     // 33
		{"", false},                          // an empty id would make `label=devdesk.test=` match every run
		{"-abcdef", false},                   // starts with a dash
		{"ABCDEF1", false},                   // upper case
		{"abc/def1", false},                  // a path separator
		{"abc def1", false},                  // a space
		{"abc=def1", false},                  // would break the label
		{"abcdef\n", false},                  // a trailing newline slips past a careless $
		{"abc_def1", false},                  // underscore is not in the alphabet
		{"abc.def1", false},                  // nor is a dot
		{"20261127-a1b2c3d4\x00", false},     // NUL
		{"20261127-a1b2c3d4 --force", false}, // an argument smuggled in
	}
	for _, tt := range tests {
		if got := enginecontract.ValidRunID(tt.id); got != tt.want {
			t.Errorf("ValidRunID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestNewRunID(t *testing.T) {
	random := func() *bytes.Reader { return bytes.NewReader([]byte{0xa1, 0xb2, 0xc3, 0xd4, 0xee, 0xee}) }

	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"the date and the four bytes", time.Date(2026, 11, 27, 23, 59, 0, 0, time.UTC), "20261127-a1b2c3d4"},
		// contract.sh builds its id with `date -u`; both must agree on the day.
		{"the date is read in UTC", time.Date(2026, 11, 28, 2, 0, 0, 0, time.FixedZone("UTC+5", 5*3600)), "20261127-a1b2c3d4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enginecontract.NewRunID(random(), tt.now)
			if got != tt.want {
				t.Errorf("NewRunID() = %q, want %q", got, tt.want)
			}
			if !enginecontract.ValidRunID(got) {
				t.Errorf("NewRunID() = %q is not a valid run id", got)
			}
		})
	}
}

func TestProcessRunID(t *testing.T) {
	t.Run("a valid value of the variable is taken as it is", func(t *testing.T) {
		got, err := enginecontract.ProcessRunID(env(enginecontract.RunEnv, "20261127-a1b2c3d4"))
		if err != nil || got != "20261127-a1b2c3d4" {
			t.Errorf("ProcessRunID() = %q, %v, want the value of the variable", got, err)
		}
	})

	t.Run("an invalid value is an error, never replaced silently", func(t *testing.T) {
		for _, bad := range []string{"X", "has space", "../x", "UPPERCASE1"} {
			got, err := enginecontract.ProcessRunID(env(enginecontract.RunEnv, bad))
			if err == nil {
				t.Errorf("ProcessRunID(%q) = %q, want an error", bad, got)
			} else if !strings.Contains(err.Error(), enginecontract.RunEnv) {
				t.Errorf("ProcessRunID(%q) error = %q, want it to name %s", bad, err, enginecontract.RunEnv)
			}
		}
	})

	t.Run("without the variable, one id per process", func(t *testing.T) {
		first, err1 := enginecontract.ProcessRunID(env())
		second, err2 := enginecontract.ProcessRunID(env())
		if err1 != nil || err2 != nil {
			t.Fatalf("ProcessRunID() errors = %v, %v", err1, err2)
		}
		if first != second {
			t.Errorf("two calls gave %q and %q, want the same id: the resources of a process share one run", first, second)
		}
		if !enginecontract.ValidRunID(first) {
			t.Errorf("the generated id %q is not valid", first)
		}
	})

	t.Run("a generated id does not hide the variable", func(t *testing.T) {
		_, _ = enginecontract.ProcessRunID(env()) // memoises the generated id
		got, err := enginecontract.ProcessRunID(env(enginecontract.RunEnv, "20261127-a1b2c3d4"))
		if err != nil || got != "20261127-a1b2c3d4" {
			t.Errorf("ProcessRunID() = %q, %v, want the value of the variable", got, err)
		}
	})
}

func TestLabelAndResourceName(t *testing.T) {
	if got := enginecontract.Label("20261127-a1b2c3d4"); got != "devdesk.test=20261127-a1b2c3d4" {
		t.Errorf("Label() = %q", got)
	}
	if got := enginecontract.ResourceName("r1-abcdef", "c1"); got != "ddc-r1-abcdef-c1" {
		t.Errorf("ResourceName() = %q, want ddc-r1-abcdef-c1", got)
	}
	if got := enginecontract.ResourceName("r1", "c1"); got != "ddc-r1-c1" {
		t.Errorf("ResourceName() = %q, want ddc-r1-c1", got)
	}
	if got := enginecontract.ResourceName("r1", strings.Repeat("a", 16)); !strings.HasSuffix(got, strings.Repeat("a", 16)) {
		t.Errorf("ResourceName() with a 16 character role = %q", got)
	}
}

func TestResourceNameRefusesAnInvalidRole(t *testing.T) {
	for _, role := range []string{"", "C1", "a-b", "a b", "a/b", strings.Repeat("a", 17), "c1\n", "é"} {
		t.Run(role, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("ResourceName(%q) returned, want a panic: a role is spelled in the source of a contract", role)
				}
			}()
			enginecontract.ResourceName("r1", role)
		})
	}
}
