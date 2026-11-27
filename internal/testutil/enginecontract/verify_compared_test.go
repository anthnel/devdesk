package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09b2 (DEVDESK-807): a green verification must not be able to hide a comparison that did not
// happen. When Verify compares, it logs `compared <campaign>/<scenario> against <fixture file name>`;
// when nothing is recorded and the session allows it, `no recorded fixture ...`; contract.sh counts
// the first (see script_compared_test.go). The negative controls below run on the fixtures that are
// versioned, with no engine.

func TestVerifyLogsWhatItComparedAgainst(t *testing.T) {
	ex, _ := rawInspect()
	ex.Argv = []string{"inspect", enginefixture.PlaceholderID("c1")}
	red := enginefixture.Redaction{}
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Exchange.Stdout, fixture.Exchange.Stderr, fixture.Exchange.Exit = ex.Stdout, ex.Stderr, ex.Exit
	rt := verify(t, []enginefixture.Fixture{fixture}, ex, red, nil)

	logged := strings.Join(rt.logs, "\n")

	if !rt.quiet() {
		t.Fatalf("reported %s", rt.text())
	}
	if want := "compared containers/inspect-absent against inspect-absent--podman-6.2.1-rootless"; !strings.Contains(logged, want) {
		t.Errorf("logs = %q, want one that says %q", logged, want)
	}
	if strings.Contains(logged, "no recorded fixture") {
		t.Errorf("logs = %q: a comparison that happened is not a missing fixture", logged)
	}
}

func TestVerifyLogsThatNothingWasComparedWhenTheFixtureIsMissingAndAllowed(t *testing.T) {
	ex, red := rawInspect()
	rt := verifyNotRequiring(t, nil, ex, red, nil)

	logged := strings.Join(rt.logs, "\n")

	if !strings.Contains(logged, "no recorded fixture") || strings.Contains(logged, "compared ") {
		t.Errorf("logs = %q, want `no recorded fixture` and no `compared`", logged)
	}
}

// A comparison that found a difference still says what it compared.
func TestVerifyLogsTheComparisonThatFailed(t *testing.T) {
	ex, red := rawInspect()
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Exchange.Exit = 99
	rt := verify(t, []enginefixture.Fixture{fixture}, ex, red, nil)

	if len(rt.errs) == 0 || !strings.Contains(strings.Join(rt.logs, "\n"), "compared containers/inspect-absent against") {
		t.Errorf("errs = %q, logs = %q, want the difference and the line", rt.errs, rt.logs)
	}
}

// ---- Negative controls, on the versioned fixtures. ----

// A recorded exchange of the embedded set, replayed as it is, agrees; one byte of stdout changed does not.
func TestVerifyOverTheVersionedFixturesSeesOneChangedByte(t *testing.T) {
	all, err := enginefixture.Load("containers")
	if err != nil {
		t.Fatalf("Load(containers) = %v", err)
	}
	config := enginefixture.ForConfig(all, enginefixture.Podman, "6.2.1", enginefixture.Rootless)
	var fixture *enginefixture.Fixture
	for i := range config {
		if config[i].Scenario == "ps-health" {
			fixture = &config[i]
		}
	}
	if fixture == nil || len(fixture.Exchange.Stdout) == 0 {
		t.Fatalf("no versioned fixture containers/ps-health for podman 6.2.1 rootless: %d for the configuration", len(config))
	}
	same := fixture.Exchange
	same.Stdout = append([]byte(nil), same.Stdout...)
	changed := same
	changed.Stdout = append([]byte(nil), same.Stdout...)
	changed.Stdout[0] ^= 1 // the first byte of the first id: one bit
	check := func(ex enginefixture.Exchange) *recT {
		rt := &recT{TB: t}
		rt.run(func() { verifySession().Verify(rt, "containers", "ps-health", ex, enginefixture.Redaction{}, nil) })
		return rt
	}

	if rt := check(same); !rt.quiet() || !strings.Contains(strings.Join(rt.logs, "\n"), "compared containers/ps-health against ps-health--podman-6.2.1-rootless") {
		t.Errorf("the recorded exchange itself: reports %s, logs %q, want agreement and the `compared` line", rt.text(), rt.logs)
	}
	if rt := check(changed); len(rt.errs) == 0 || !strings.Contains(strings.Join(rt.errs, "\n"), "stdout") {
		t.Errorf("one byte of stdout changed: errs %q, want a difference that names stdout", rt.errs)
	}
}

// With fixtures required and the session not relaxed, a scenario nobody recorded fails, in a campaign
// that exists and in one that does not.
func TestVerifyOverTheVersionedFixturesFailsWithoutAFixtureForTheScenario(t *testing.T) {
	ex, red := rawInspect()
	for _, campaign := range []string{"containers", "no-such-campaign-recorded"} {
		rt := &recT{TB: t}
		rt.run(func() { verifySession().Verify(rt, campaign, "a-scenario-nobody-recorded", ex, red, nil) })

		if len(rt.fatals) != 1 {
			t.Errorf("campaign %s: fatals = %q, errs = %q, want one Fatal", campaign, rt.fatals, rt.errs)
		}
	}
}
