package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09b1 hardening (DEVDESK-807, security B1 and B2, review 1 and 2). The signatures:
//
//	func (c *contract) exchangeListing(kind string, cmp enginecontract.Comparer, campaign, scenario, text string, call func()) enginefixture.Exchange
//	func findContainer(list []Container, id string) (Container, bool)
//	func volumePruneAgrees(report, volume string, stillListed bool) error
//
// The names of these tests do not start with "TestContract" (guard G5, R3).

// ---- B2: a recording without the sentinel. ----

func TestRedactionInARecordingNeedsTheSentinel(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, "")
	s := labSession(enginefixture.Podman, labRunID)
	s.Mode = enginecontract.Record
	ct := &contractT{TB: t}
	c := newContract(ct, s)

	ended := ct.run(func() { c.redaction() })

	if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], enginecontract.SentinelEnv) {
		t.Errorf("ended = %v, fatals = %q, want one Fatalf that names %s", ended, ct.fatals, enginecontract.SentinelEnv)
	}
}

func TestRedactionWhenVerifyingNeedsNoSentinel(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, "")
	ct := &contractT{TB: t}
	c := newContract(ct, labSession(enginefixture.Podman, labRunID))

	if ended := ct.run(func() { c.redaction() }); ended {
		t.Errorf("verifying ended on %q: a run by hand has no sentinel", ct.fatals)
	}
}

// ---- B1: a recorded list holds this run and the sentinel, nothing else. ----

func listingCall(kind string) (argv []string, call func()) {
	switch kind {
	case "container":
		argv = []string{"ps", "-a", "-q", "--no-trunc"}
	default:
		argv = []string{"volume", "ls", "-q"}
	}
	return argv, func() { _, _ = dockerOutput(argv...) }
}

func TestExchangeListingRecordsAStoreOfThisRunAndTheSentinel(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		t.Setenv(enginecontract.SentinelEnv, sentinelID)
		vol := ctrNameOf("v1")
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stdout: vol + "\n"}, "volume", "ls", "-q"))
		out := t.TempDir()
		c.s.Mode, c.s.OutDir = enginecontract.Record, out
		c.ours[vol] = "v1"
		_, call := listingCall("volume")

		mustReachTheEnd(t, ct, func() { c.exchangeListing("volume", nil, "volumes", "ls", "the volumes of the run", call) })

		if files, _ := filepath.Glob(filepath.Join(out, "volumes", "*.cli")); len(files) != 1 {
			t.Errorf("%d fixture(s) written, want 1", len(files))
		}
		// The store is read first, then the exchange that is recorded.
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{{"volume", "ls", "-q"}, {"volume", "ls", "-q"}}) {
			t.Errorf("engine calls = %q, want the read of the store and then the call", got)
		}
	})
}

func TestExchangeListingAllowsTheSentinelAmongContainers(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		t.Setenv(enginecontract.SentinelEnv, sentinelID)
		argv, call := listingCall("container")
		c, ct, _ := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: idA + "\n" + sentinelID + "\n"}, argv...))
		c.s.Mode, c.s.OutDir = enginecontract.Record, t.TempDir()
		c.ours[idA] = "c1"

		mustReachTheEnd(t, ct, func() { c.exchangeListing("container", nil, "containers", "ps", "the containers of the run", call) })

		if len(ct.errs) != 0 {
			t.Errorf("reports = %q, want none", ct.errs)
		}
	})
}

// A resource left by a killed run, or by anybody else, must not enter a fixture.
func TestExchangeListingRefusesToRecordAStoreWithAStranger(t *testing.T) {
	tests := []struct {
		name, kind, reply, stranger string
	}{
		{"a container of nobody we know", "container", idA + "\n" + idB + "\n", idB},
		{"a volume", "volume", ctrNameOf("v1") + "\npgdata\n", "pgdata"},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Setenv(enginecontract.SentinelEnv, sentinelID)
				argv, call := listingCall(tt.kind)
				c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: tt.reply}, argv...))
				out := t.TempDir()
				c.s.Mode, c.s.OutDir = enginecontract.Record, out
				c.ours[idA], c.ours[ctrNameOf("v1")] = "c1", "v1"
				called := false

				ended := ct.run(func() {
					c.exchangeListing(tt.kind, nil, "campaign", "ls", "a list", func() { called = true; call() })
				})

				if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], tt.stranger) {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf that names %s", ended, ct.fatals, tt.stranger)
				}
				if called || len(fake.Invocations()) != 1 {
					t.Errorf("called = %v, engine calls = %q, want the read of the store only", called, invokedArgs(fake))
				}
				if entries, _ := os.ReadDir(out); len(entries) != 0 {
					t.Errorf("%d entries written for a refused recording", len(entries))
				}
			})
		}
	})
}

func TestExchangeListingRefusesToRecordWhenTheStoreCannotBeRead(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		t.Setenv(enginecontract.SentinelEnv, sentinelID)
		argv, call := listingCall("volume")
		c, ct, _ := startOnFake(t, shape, replyTo(fakecli.Reply{Stderr: "cannot connect", Exit: 1}, argv...))
		c.s.Mode, c.s.OutDir = enginecontract.Record, t.TempDir()

		ended := ct.run(func() { c.exchangeListing("volume", nil, "volumes", "ls", "a list", call) })

		if !ended || len(ct.fatals) != 1 {
			t.Errorf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
		}
	})
}

// Verifying only logs what it finds: a developer's VM may hold more than a fresh one.
func TestExchangeListingWhenVerifyingOnlyLogsAStranger(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		argv, call := listingCall("volume")
		c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: "pgdata\n"}, argv...))
		called := false

		mustReachTheEnd(t, ct, func() {
			c.exchangeListing("volume", nil, "volumes", "ls", "a list", func() { called = true; call() })
		})

		if !called || len(ct.errs) != 0 {
			t.Errorf("called = %v, reports = %q, want the call made and nothing reported", called, ct.errs)
		}
		if len(fake.Invocations()) != 2 {
			t.Errorf("engine calls = %q, want the read of the store and the call", invokedArgs(fake))
		}
	})
}

// ---- Review 1: an empty id is a prefix of every id. ----

func TestFindContainerNeverMatchesAnEmptyID(t *testing.T) {
	list := []Container{{ID: "", Name: "ghost"}, {ID: idB[:12], Name: "other"}, {ID: idA[:12], Name: "ours"}}

	got, ok := findContainer(list, idA)

	if !ok || got.Name != "ours" {
		t.Errorf("findContainer = %+v, %v, want the row whose id starts the full id", got, ok)
	}
	if got, ok := findContainer(list[:1], idA); ok {
		t.Errorf("findContainer(empty id) = %+v, true, want no match: an empty id is a prefix of every id", got)
	}
	if _, ok := findContainer(nil, idA); ok {
		t.Error("findContainer(nil) matched")
	}
}

// ---- Review 2: what the prune reports agrees with what is left. ----

func TestVolumePruneAgreesWithTheStateOfTheVolume(t *testing.T) {
	name := ctrNameOf("v1")
	tests := []struct {
		name        string
		report      string
		stillListed bool
		wantErr     bool
	}{
		{"podman 5: the report names it and it is gone", name + "\n", false, false},
		{"docker: the report names it under Deleted Volumes and it is gone", "Deleted Volumes:\n" + name + "\n\nTotal reclaimed space: 0B", false, false},
		{"docker 28 and 29: nothing deleted, the volume is still there", "Total reclaimed space: 0B", true, false},
		{"podman 6: an empty report, the volume is still there", "", true, false},
		{"the report names it and it is still listed", name + "\n", true, true},
		{"the report says nothing and the volume is gone", "Total reclaimed space: 0B", false, true},
		{"a longer name is not the volume, and it is still there", name + "-x\n", true, false},
		{"a longer name is not the volume, and the volume is gone", name + "-x\n", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := volumePruneAgrees(tt.report, name, tt.stillListed)

			if (err != nil) != tt.wantErr {
				t.Errorf("volumePruneAgrees = %v, want an error: %v", err, tt.wantErr)
			}
		})
	}
}
