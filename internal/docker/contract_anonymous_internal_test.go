package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09b2 (DEVDESK-807): c.anonymous(n) declares how many ids nobody owns the scenario of this
// contract accepts to replace (Redaction.Anonymous), for a message that names the network endpoint
// of a container that never ran. It is a declaration of the contract, never a default. The names of
// these tests do not start with "TestContract" (guard G5, R3).
//
//	func (c *contract) anonymous(n int)

func TestAnonymousDeclaresTheNumberOfUnknownIDsOfTheScenario(t *testing.T) {
	c := newContract(&contractT{TB: t}, labSession(enginefixture.Podman, labRunID))
	if got := c.redaction().Anonymous; got != 0 {
		t.Fatalf("Redaction.Anonymous = %d with no declaration, want 0", got)
	}

	c.anonymous(2)

	if got := c.redaction().Anonymous; got != 2 {
		t.Errorf("Redaction.Anonymous = %d, want 2", got)
	}
}

func TestAnonymousRefusesNumbersThatMeanNothingOrTooMuch(t *testing.T) {
	for _, n := range []int{0, -1, 9, 1000} {
		ct := &contractT{TB: t}
		c := newContract(ct, labSession(enginefixture.Podman, labRunID))

		ended := ct.run(func() { c.anonymous(n) })

		if !ended || len(ct.fatals) != 1 || c.redaction().Anonymous != 0 {
			t.Errorf("anonymous(%d): ended = %v, fatals = %q, want one Fatalf and nothing declared", n, ended, ct.fatals)
		}
	}
}

// End to end through the recorder: the ids are replaced in what is written, and a third one is refused.
func TestAnonymousReachesTheRecordedFixture(t *testing.T) {
	a, b, extra := strings.Repeat("1f", 32), strings.Repeat("2e", 32), strings.Repeat("3d", 32)
	c := newContract(&contractT{TB: t}, labSession(enginefixture.Podman, labRunID))
	c.anonymous(2)

	got, _ := enginefixture.Redact(enginefixture.Exchange{Argv: []string{"x"}, Stderr: []byte(a + " " + b + " " + extra)}, c.redaction())

	if string(got.Stderr) != enginefixture.PlaceholderID("anon-1")+" "+enginefixture.PlaceholderID("anon-2")+" "+extra {
		t.Errorf("stderr = %q, want the first two replaced and the third left for the rule to refuse", got.Stderr)
	}
}
