package docker

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09b1 (DEVDESK-807), helper (1): the condition of a prune. contractArgvRule
// refuses every prune, because a prune acts on the whole store and the functions of
// DevDesk take no label filter. Since OC-22 (DEVDESK-820) production code prunes
// only networks (PruneNetworks); the container and volume prunes of this window are
// built by pruneKindForTest (removal_helpers_test.go), in test code. The
// scenarios of OC-09b may still want one, so a contract may open a window for ONE
// `<kind> prune -f`, and only when the store of that kind holds nothing but
// resources of the run, read just before the call. A foreign resource, a listing
// that fails, a kind that has no window (image, system), a missing lab mark: the
// prune is not sent. The signatures are in contract_oc09b_helpers_internal_test.go.

// pruneKindCase describes one kind of prune for the tests below.
type pruneKindCase struct {
	kind string
	// list is the read of the whole store of the kind, with no filter.
	list []string
	// ours are the references the contract owns; only is what the listing answers when
	// the store holds nothing else (the default networks of the engines excepted), and
	// withStranger what it answers with one resource of somebody else added.
	ours         []string
	only         string
	withStranger string
	stranger     string
	argv         []string
	call         func() error
}

func pruneKindCases() []pruneKindCase {
	vol, net := ctrNameOf("v1"), ctrNameOf("n1")
	return []pruneKindCase{
		{
			kind: "container", list: []string{"ps", "-a", "-q", "--no-trunc"},
			ours: []string{idA}, only: idA + "\n", withStranger: idA + "\n" + idB + "\n", stranger: idB,
			argv: []string{"container", "prune", "-f"},
			call: func() error { _, err := pruneKindForTest("container"); return err },
		},
		{
			kind: "network", list: []string{"network", "ls", "--format", "{{.Name}}"},
			ours: []string{net}, only: "bridge\nhost\nnone\npodman\n" + net + "\n", withStranger: "bridge\nbackend\n" + net + "\n", stranger: "backend",
			argv: []string{"network", "prune", "-f"},
			call: func() error { _, err := PruneNetworks(); return err },
		},
		{
			kind: "volume", list: []string{"volume", "ls", "-q"},
			ours: []string{vol}, only: vol + "\n", withStranger: vol + "\npgdata\n", stranger: "pgdata",
			argv: []string{"volume", "prune", "-f"},
			call: func() error { _, err := pruneKindForTest("volume"); return err },
		},
	}
}

func seedOurs(c *contract, refs []string) {
	for _, ref := range refs {
		c.ours[ref] = "seeded"
	}
}

// ---- The rule of the window (pure). ----

func TestPruneWindowRuleGrantsTheOneArgvOfItsKindOnce(t *testing.T) {
	for _, kind := range []string{"container", "network", "volume"} {
		t.Run(kind, func(t *testing.T) {
			rule := pruneWindowRule(kind)
			argv := []string{kind, "prune", "-f"}

			if err := rule(argv); err != nil {
				t.Fatalf("rule(%q) = %v, want the one prune of the window allowed", argv, err)
			}
			if err := rule(argv); err == nil {
				t.Errorf("rule(%q) = nil a second time, want a refusal: the window grants one attempt", argv)
			}
		})
	}
}

func TestPruneWindowRuleRefusesEverythingElse(t *testing.T) {
	tests := []struct {
		name string
		argv string
	}{
		{"without -f, the engine would ask on stdin", "volume prune"},
		{"-a", "volume prune -a -f"},
		{"-f -a", "volume prune -f -a"},
		{"--all", "volume prune --all -f"},
		{"--force", "volume prune --force"},
		{"a filter", "volume prune -f --filter label=x"},
		{"an extra word", "volume prune -f extra"},
		{"another kind", "network prune -f"},
		{"container prune", "container prune -f"},
		{"image prune", "image prune -f"},
		{"image prune -a", "image prune -a -f"},
		{"system prune", "system prune -f"},
		{"system prune -a", "system prune -a -f"},
		{"system prune with volumes", "system prune -a --volumes -f"},
		{"the bare verb", "prune"},
		{"a global option in front", "--host x volume prune -f"},
		{"a global option after", "volume prune -f --context x"},
		{"a destructive verb of another kind", "rm -f " + idA},
		{"volume rm", "volume rm pgdata"},
		{"a read", "volume ls -q"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := pruneWindowRule("volume")
			argv := strings.Fields(tt.argv)

			if err := rule(argv); err == nil {
				t.Errorf("rule(%q) = nil, want a refusal: the window grants `volume prune -f` and nothing else", argv)
			}
		})
	}
	t.Run("no argv", func(t *testing.T) {
		if err := pruneWindowRule("volume")(nil); err == nil {
			t.Error("rule(nil) = nil, want a refusal")
		}
	})
}

func TestPruneWindowRuleHasNoWindowForAKindItDoesNotKnow(t *testing.T) {
	for _, kind := range []string{"image", "system", "pod", "builder", "", "volume ", "Volume"} {
		t.Run("kind "+kind, func(t *testing.T) {
			rule := pruneWindowRule(kind)

			if err := rule([]string{kind, "prune", "-f"}); err == nil {
				t.Errorf("rule(%q prune -f) = nil, want a refusal: only container, network and volume have a window", kind)
			}
		})
	}
}

func TestPruneWindowRuleKeepsItsAttemptForTheArgvItGrants(t *testing.T) {
	rule := pruneWindowRule("volume")

	if err := rule([]string{"volume", "prune", "-a", "-f"}); err == nil {
		t.Fatal("rule(volume prune -a -f) = nil, want a refusal")
	}
	if err := rule([]string{"volume", "prune", "-f"}); err != nil {
		t.Errorf("rule(volume prune -f) = %v after a refused argv, want the attempt to be still there", err)
	}
}

// ---- The window on a contract. ----

func TestExpectPruneSendsTheOnePruneWhenTheStoreIsOnlyThisRuns(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range pruneKindCases() {
			t.Run(tt.kind, func(t *testing.T) {
				c, ct, fake := startOnFake(t, shape,
					replyTo(fakecli.Reply{Stdout: tt.only}, tt.list...),
					replyTo(fakecli.Reply{Stdout: "Deleted Volumes:\n" + ctrNameOf("v1") + "\n\nTotal reclaimed space: 0B\n"}, tt.argv...))
				seedOurs(c, tt.ours)
				var (
					ex  enginefixture.Exchange
					err error
				)

				mustReachTheEnd(t, ct, func() {
					ex = c.expectPrune("campaign", "prune", "prune of a store that holds only this run", tt.kind, func() { err = tt.call() })
				})

				if err != nil {
					t.Errorf("the prune of the window = %v, want it to succeed", err)
				}
				if !slices.Equal(ex.Argv, tt.argv) || ex.Exit != 0 {
					t.Errorf("exchange = %+v, want argv %q and exit 0", ex, tt.argv)
				}
				// The store is read right before the call, then the prune, nothing in between.
				if got := invokedArgs(fake); !sameArgvs(got, [][]string{tt.list, tt.argv}) {
					t.Errorf("engine calls = %q, want the listing %q and then %q", got, tt.list, tt.argv)
				}
				if len(ct.errs) != 0 {
					t.Errorf("reports = %q, want none", ct.errs)
				}
				// The window is closed once the call returns: the same argv is refused again.
				before := len(fake.Invocations())
				if _, again := c.rec.Run(dockerCmd{Args: tt.argv, Combined: true}); !errors.Is(again, errNotContracted) {
					t.Errorf("the prune after the window = %v, want errNotContracted", again)
				}
				ct.errs = nil // the refusal above was the point
				if n := len(fake.Invocations()); n != before {
					t.Errorf("the engine was started %d more time(s) after the window closed", n-before)
				}
			})
		}
	})
}

func TestExpectPruneAcceptsAnEmptyStore(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		tt := pruneKindCases()[2] // volume
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{}, tt.list...),
			replyTo(fakecli.Reply{}, tt.argv...))

		mustReachTheEnd(t, ct, func() {
			c.expectPrune("campaign", "prune", "prune of an empty store", tt.kind, func() { _ = tt.call() })
		})

		if got := invokedArgs(fake); !sameArgvs(got, [][]string{tt.list, tt.argv}) {
			t.Errorf("engine calls = %q, want the listing and the prune: nothing foreign is there", got)
		}
	})
}

// A foreign resource anywhere in the store ends the contract before the prune: it is
// the whole point of the condition. The stranger is named in the message.
func TestExpectPruneRefusesWhenTheStoreHoldsAForeignResource(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range pruneKindCases() {
			t.Run(tt.kind, func(t *testing.T) {
				c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: tt.withStranger}, tt.list...))
				seedOurs(c, tt.ours)
				called := false

				ended := ct.run(func() {
					c.expectPrune("campaign", "prune", "prune of a shared store", tt.kind, func() { called = true; _ = tt.call() })
				})

				if !ended || len(ct.fatals) != 1 {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
				}
				if !strings.Contains(ct.fatals[0], tt.stranger) {
					t.Errorf("fatal = %q, want it to name the foreign resource %q", ct.fatals[0], tt.stranger)
				}
				if called {
					t.Error("the call ran: the prune of a shared store must not be reachable")
				}
				if got := invokedArgs(fake); !sameArgvs(got, [][]string{tt.list}) {
					t.Errorf("engine calls = %q, want the listing only", got)
				}
			})
		}
	})
}

func TestExpectPruneRefusesWhenTheStoreCannotBeRead(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		tt := pruneKindCases()[2]
		c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stderr: "cannot connect to the engine", Exit: 1}, tt.list...))
		called := false

		ended := ct.run(func() {
			c.expectPrune("campaign", "prune", "prune of a store nobody could read", tt.kind, func() { called = true })
		})

		if !ended || len(ct.fatals) != 1 {
			t.Fatalf("ended = %v, fatals = %q, want one Fatalf: when in doubt, nothing is pruned", ended, ct.fatals)
		}
		if called || len(fake.Invocations()) != 1 {
			t.Errorf("called = %v, engine calls = %q, want only the failed listing", called, invokedArgs(fake))
		}
	})
}

// image and system have no window at all: a Fatal before the engine is asked anything.
func TestExpectPruneKnowsOnlyContainerNetworkAndVolume(t *testing.T) {
	for _, kind := range []string{"image", "system", "pod", "builder", ""} {
		t.Run("kind "+kind, func(t *testing.T) {
			forEachEngine(t, func(t *testing.T, shape engine.Shape) {
				c, ct, fake := startOnFake(t, shape)
				called := false

				ended := ct.run(func() {
					c.expectPrune("campaign", "prune", "prune of a kind with no window", kind, func() { called = true })
				})

				if !ended || len(ct.fatals) != 1 {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
				}
				if called || len(fake.Invocations()) != 0 {
					t.Errorf("called = %v, engine calls = %q, want nothing at all", called, invokedArgs(fake))
				}
			})
		})
	}
}

// What the call does inside the window is judged by the rule: a prune that is not the
// one of the window never reaches the engine, and the contract fails (a fixture is one
// exchange, and the refusal is reported).
func TestExpectPruneDoesNotLetTheCallPruneAnythingElse(t *testing.T) {
	bad := []struct {
		name string
		argv []string
	}{
		{"volume prune -a", []string{"volume", "prune", "-a", "-f"}},
		{"system prune", []string{"system", "prune", "-f"}},
		{"system prune -a", []string{"system", "prune", "-a", "-f"}},
		{"image prune -a", []string{"image", "prune", "-a", "-f"}},
		{"container prune", []string{"container", "prune", "-f"}},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, b := range bad {
			t.Run(b.name, func(t *testing.T) {
				tt := pruneKindCases()[2] // the window is a volume one
				c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: tt.only}, tt.list...))
				seedOurs(c, tt.ours)

				ended := ct.run(func() {
					c.expectPrune("campaign", "prune", "a window that is asked for another prune", tt.kind, func() { _, _ = dockerCombined(b.argv...) })
				})

				if !ended {
					t.Error("the contract went on: no exchange happened in the window, a fixture is one exchange")
				}
				if len(ct.errs) == 0 {
					t.Error("the refusal was not reported to the test")
				}
				if got := invokedArgs(fake); !sameArgvs(got, [][]string{tt.list}) {
					t.Errorf("engine calls = %q, want the listing only: %q must not reach the engine", got, b.argv)
				}
			})
		}
	})
}

// One attempt: a second prune inside the same window is refused and reported.
func TestExpectPruneGrantsOneAttempt(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		tt := pruneKindCases()[2]
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stdout: tt.only}, tt.list...),
			replyTo(fakecli.Reply{}, tt.argv...))
		seedOurs(c, tt.ours)

		mustReachTheEnd(t, ct, func() {
			c.expectPrune("campaign", "prune", "a window that prunes twice", tt.kind, func() { _ = tt.call(); _ = tt.call() })
		})

		if len(ct.errs) == 0 {
			t.Error("the second prune was not reported")
		}
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{tt.list, tt.argv}) {
			t.Errorf("engine calls = %q, want one listing and one prune", got)
		}
	})
}

// A prune destroys: the lab marks are asked first, before any read.
func TestExpectPruneNeedsTheLabMarkers(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		setLab(t, false, true)
		q := queued(t) // no step: nothing may reach the engine
		ct := &contractT{TB: t}
		c := newContract(ct, sessionOf(shape, labRunID))
		called := false

		ended := ct.run(func() {
			c.expectPrune("campaign", "prune", "prune outside a lab", "volume", func() { called = true })
		})

		if !ended || len(ct.fatals) != 1 {
			t.Fatalf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
		}
		if called || len(q.calls) != 0 {
			t.Errorf("called = %v, engine calls = %v, want nothing", called, q.calls)
		}
	})
}
