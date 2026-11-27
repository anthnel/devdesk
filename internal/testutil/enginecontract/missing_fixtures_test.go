package enginecontract_test

import (
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09b2 (DEVDESK-807): fixtures are required (RequireFixtures is true), so a contract with no
// fixture fails. The tests of the helpers of internal/docker run scenarios nobody recorded on a fake
// engine and say so with Session.MissingFixturesOK; Require never does.

func TestSessionMissingFixturesOKTurnsTheFailureIntoALog(t *testing.T) {
	ex, red := rawInspect()
	s := verifySession()
	s.MissingFixturesOK = true
	rt := &recT{TB: t}

	rt.run(func() { s.VerifyAgainst(rt, nil, "inspect-absent", ex, red, nil) })

	if !rt.quiet() {
		t.Errorf("reported %s, want only a log", rt.text())
	}
}

func TestMissingFixturesFailByDefault(t *testing.T) {
	ex, red := rawInspect()
	rt := &recT{TB: t}

	rt.run(func() { verifySession().VerifyAgainst(rt, nil, "inspect-absent", ex, red, nil) })

	if len(rt.fatals) != 1 {
		t.Errorf("fatals = %q, want one: fixtures are required", rt.fatals)
	}
}

// What exists is still compared, whatever the flag says: it relaxes only "no fixture at all".
func TestMissingFixturesOKStillComparesWhatExists(t *testing.T) {
	ex, red := rawInspect()
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Exchange.Exit = 99
	s := verifySession()
	s.MissingFixturesOK = true
	rt := &recT{TB: t}

	rt.run(func() { s.VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "inspect-absent", ex, red, nil) })

	if len(rt.errs) == 0 {
		t.Error("a difference with the recorded fixture was not reported")
	}
}

func TestRequireNeverSetsMissingFixturesOK(t *testing.T) {
	openGate(t, enginefixture.Podman)

	got := callRequire(t, podmanN, nil)

	if got.session == nil || got.session.MissingFixturesOK {
		t.Errorf("session = %+v (failed %v %q), want one that does not relax the fixtures", got.session, got.c.failed, got.c.msg)
	}
}
