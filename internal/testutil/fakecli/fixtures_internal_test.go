package fakecli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// A selection the replay cannot serve fails the test at once (Fatalf), rather
// than serving half of it. failedT and attempt are in internal_test.go.

func plainFixture(name string, argv ...string) enginefixture.Fixture {
	return enginefixture.Fixture{
		Name: name, Scenario: name,
		Meta: enginefixture.Meta{
			Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful,
			Recorded: "2026-11-27", Scenario: name, Redacted: "none", RecordedBy: "synthetic",
		},
		Exchange: enginefixture.Exchange{Argv: argv, Exit: 1, Stderr: []byte("e\n")},
	}
}

func TestFixturesFailsTheTestOnAnUnusableSelection(t *testing.T) {
	pending := plainFixture("rm-pending", "rm", "a")
	pending.Meta.Record = enginefixture.RecordPending

	tests := []struct {
		name string
		in   []enginefixture.Fixture
	}{
		{"no fixture", nil},
		{"a pending fixture", []enginefixture.Fixture{plainFixture("ps", "ps"), pending}},
		{"two fixtures with one tool and argv", []enginefixture.Fixture{plainFixture("one", "rm", "a"), plainFixture("two", "rm", "a")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := &failedT{TB: t}

			failed := attempt(func() { Fixtures(ft, tt.in...) })

			if !failed {
				t.Fatal("Fixtures went on with a selection the replay refuses")
			}
		})
	}
}

// The failure names the fixtures, which Fatalf formats; failedT keeps only the
// format string, so this one keeps the formatted message.
type messageT struct {
	testing.TB
	msg string
}

func (m *messageT) Fatalf(format string, args ...any) {
	m.msg = fmt.Sprintf(format, args...)
	panic(fatal{})
}

func TestFixturesNamesTheOffendingFixtures(t *testing.T) {
	mt := &messageT{TB: t}

	failed := attempt(func() {
		Fixtures(mt, plainFixture("first-twin", "rm", "a"), plainFixture("second-twin", "rm", "a"))
	})

	if !failed {
		t.Fatal("Fixtures accepted two fixtures with one key")
	}
	if !strings.Contains(mt.msg, "first-twin") || !strings.Contains(mt.msg, "second-twin") {
		t.Errorf("message = %q, want it to name both fixtures", mt.msg)
	}
}
