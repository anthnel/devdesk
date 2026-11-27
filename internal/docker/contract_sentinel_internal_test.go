package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b1 (DEVDESK-807): the sentinel of contract.sh is a container of somebody else that a `ps`
// of a contract lists. Its id (the variable DEVDESK_ENGINE_CONTRACT_SENTINEL) is redacted to
// the role "sentinel", as a full id and as the 12 characters ps prints, and its name
// ddc-sentinel-<run> loses the run id like every name. The names of these tests do not start
// with "TestContract" (guard G5, R3).

// sentinelID stands for the id of the sentinel: 64 lower-case hex.
var sentinelID = strings.Repeat("5e", 32)

func TestRedactionMapsTheSentinelToItsOwnRole(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, sentinelID)
	ct := &contractT{TB: t}
	c := newContract(ct, labSession(enginefixture.Podman, labRunID))
	c.ours[idA] = "c1"

	r := c.redaction()

	if r.IDs[sentinelID] != "sentinel" || r.IDs[idA] != "c1" {
		t.Errorf("Redaction.IDs = %v, want the sentinel as %q next to the containers of the run", r.IDs, "sentinel")
	}
	// A listing of the VM: the container of the run first, then the sentinel (short id, its name).
	row := func(id, name, status string) string {
		return id + "\t" + name + "\talpine\trunning\t" + status + "\t2026-11-27 12:00:00 +0000 UTC\t\n"
	}
	listing := row(idA[:12], ctrNameOf("c1"), "Up 3 seconds") + row(sentinelID[:12], "ddc-sentinel-"+labRunID, "Up 40 seconds")
	got, header := enginefixture.Redact(enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte(listing)}, r)

	for _, leaked := range []string{sentinelID[:12], idA[:12], labRunID} {
		if strings.Contains(string(got.Stdout), leaked) {
			t.Errorf("redacted listing = %q, want %q replaced", got.Stdout, leaked)
		}
	}
	want := enginefixture.PlaceholderID("sentinel")[:12] + "\tddc-sentinel-" + enginefixture.PlaceholderRun + "\t"
	if !strings.Contains(string(got.Stdout), want) {
		t.Errorf("redacted listing = %q, want a row that starts %q", got.Stdout, want)
	}
	if header == "none" {
		t.Errorf("redacted header = %q, want the categories that replaced something", header)
	}
}

// The two runs a fixture is recorded from have two sentinels: the fixture is the same.
func TestTwoSentinelsMakeTheSameFixture(t *testing.T) {
	other := strings.Repeat("9a", 32)
	redactedWith := func(sentinel string) string {
		t.Setenv(enginecontract.SentinelEnv, sentinel)
		c := newContract(&contractT{TB: t}, labSession(enginefixture.Podman, labRunID))
		got, _ := enginefixture.Redact(enginefixture.Exchange{Argv: []string{"ps", "-a", "-q"}, Stdout: []byte(sentinel[:12] + "\n" + sentinel + "\n")}, c.redaction())
		return string(got.Stdout)
	}

	if a, b := redactedWith(sentinelID), redactedWith(other); a != b {
		t.Errorf("fixtures differ: %q and %q, want the sentinel to be one role whatever its id", a, b)
	}
}

func TestRedactionWithoutASentinelKnowsNone(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, "")
	c := newContract(&contractT{TB: t}, labSession(enginefixture.Podman, labRunID))

	r := c.redaction()

	if len(r.IDs) != 0 {
		t.Errorf("Redaction.IDs = %v, want none: a run by hand has no sentinel", r.IDs)
	}
}

// A value that is not a full id is a mistake of the script: the contract ends rather than
// record a listing that holds an id nobody redacted.
func TestRedactionRefusesASentinelThatIsNotAFullID(t *testing.T) {
	for _, bad := range []string{sentinelID[:12], strings.ToUpper(sentinelID), "sha256:" + sentinelID, "ddc-sentinel-" + labRunID, sentinelID + "\n"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(enginecontract.SentinelEnv, bad)
			ct := &contractT{TB: t}
			c := newContract(ct, labSession(enginefixture.Podman, labRunID))

			ended := ct.run(func() { c.redaction() })

			if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], enginecontract.SentinelEnv) {
				t.Errorf("ended = %v, fatals = %q, want one Fatalf that names %s", ended, ct.fatals, enginecontract.SentinelEnv)
			}
		})
	}
}
