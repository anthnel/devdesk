package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b2 (DEVDESK-807): the refusal of an rmi names ONE of the containers that use the image, and
// the engine picks the one it finds first in a table with no order: the sentinel of contract.sh
// or the container of the run (docker prints 12 characters after "container", podman 64 after "used
// by"). The Comparer of that scenario tolerates which of them it names:
//
//	enginecontract.ContainerIDs // a Tolerance: the id right after "container " or "used by "
//	func SameUpToContainerIDs() Comparer // Tolerating(ContainerIDs)
//
// Narrow on purpose: only a word of 12 or of 64 hexadecimal characters right after one of those
// two phrases; the image id in the same message, the other words, the exit code and any id
// elsewhere are still compared.

const (
	idSentinel12 = "3a84144b327a"
	idRun12      = "c6fb2d45647e"
)

func refusal(stderr string) enginefixture.Exchange {
	return enginefixture.Exchange{Argv: []string{"rmi", strings.Repeat("3e", 32)}, Exit: 1, Stderr: []byte(stderr)}
}

func TestSameUpToContainerIDsAcceptsTheOtherContainerThatUsesTheImage(t *testing.T) {
	long1, long2 := strings.Repeat("a1", 32), strings.Repeat("b2", 32)
	tests := []struct{ name, recorded, got string }{
		{"docker, running container", "Error response from daemon: conflict: unable to delete 3ea06e4cb95e (cannot be forced) - image is being used by running container " + idSentinel12,
			"Error response from daemon: conflict: unable to delete 3ea06e4cb95e (cannot be forced) - image is being used by running container " + idRun12},
		{"docker, stopped container", "image is being used by stopped container " + idSentinel12, "image is being used by stopped container " + idRun12},
		{"podman, used by", "Error: image used by " + long1 + ": image is in use by a container: consider force-removing image",
			"Error: image used by " + long2 + ": image is in use by a container: consider force-removing image"},
		{"the same one", "used by " + long1 + ".", "used by " + long1 + "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmp := enginecontract.SameUpToContainerIDs()

			if err := cmp(refusal(tt.recorded), refusal(tt.got)); err != nil {
				t.Errorf("SameUpToContainerIDs = %v, want nil", err)
			}
			if err := cmp(refusal(tt.got), refusal(tt.recorded)); err != nil {
				t.Errorf("with the exchanges swapped = %v, want nil", err)
			}
			if err := enginecontract.Tolerating(enginecontract.ContainerIDs)(refusal(tt.recorded), refusal(tt.got)); err != nil {
				t.Errorf("Tolerating(ContainerIDs) = %v, want the same comparer", err)
			}
		})
	}
}

func TestSameUpToContainerIDsStillSeesEverythingElse(t *testing.T) {
	long := strings.Repeat("a1", 32)
	base := "unable to delete 3ea06e4cb95e (cannot be forced) - image is being used by running container " + idSentinel12
	tests := []struct{ name, recorded, got string }{
		{"another image id in the same message", base, strings.Replace(base, "3ea06e4cb95e", "4fb17f5dc06f", 1)},
		{"running and stopped", base, strings.Replace(base, "running", "stopped", 1)},
		{"another sentence", base, strings.Replace(base, "cannot be forced", "must be forced", 1)},
		{"a name instead of an id", base, strings.Replace(base, idSentinel12, "web", 1)},
		{"an id of another length", base, strings.Replace(base, idSentinel12, idSentinel12[:11], 1)},
		{"an id after another phrase", "name \"" + idSentinel12 + "\"", "name \"" + idRun12 + "\""},
		{"an id that is not after the phrase", "container web " + idSentinel12, "container web " + idRun12},
		{"an id of 64 against none", "used by " + long, "used by"},
		{"the id is a word and not a prefix of one", "used by " + idSentinel12 + "ab", "used by " + idRun12 + "ab"},
		{"upper case is not an id", "used by " + strings.ToUpper(idSentinel12), "used by " + strings.ToUpper(idRun12)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := enginecontract.SameUpToContainerIDs()(refusal(tt.recorded), refusal(tt.got)); err == nil {
				t.Errorf("SameUpToContainerIDs(%q, %q) = nil, want a difference", tt.recorded, tt.got)
			}
		})
	}
}

// The exit code and the argv are not the Comparer's: Verify compares them exactly first.
func TestSameUpToContainerIDsDoesNotTolerateAnythingBeforeAContainerPhrase(t *testing.T) {
	recorded := refusal("a used by " + idSentinel12)
	got := refusal("b used by " + idRun12)

	if err := enginecontract.SameUpToContainerIDs()(recorded, got); err == nil || !strings.Contains(err.Error(), "stderr") {
		t.Errorf("error = %v, want the stderr difference", err)
	}
}

func TestContainerIDsCombinesWithTheOtherKinds(t *testing.T) {
	rec := enginefixture.Exchange{Stdout: []byte("Up 3 seconds used by " + idSentinel12)}
	got := enginefixture.Exchange{Stdout: []byte("Up 9 minutes used by " + idRun12)}

	if err := enginecontract.Tolerating(enginecontract.Durations, enginecontract.ContainerIDs)(rec, got); err != nil {
		t.Errorf("Tolerating(Durations, ContainerIDs) = %v, want nil", err)
	}
	if err := enginecontract.Tolerating(enginecontract.Durations)(rec, got); err == nil {
		t.Error("without ContainerIDs the ids differ, but nothing was reported")
	}
}

// Plugged into Verify, as the contract does.
func TestVerifyOfAnRmiRefusalNamingTheOtherContainer(t *testing.T) {
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Scenario, fixture.Name = "rmi-used", "rmi-used--podman-6.2.1-rootless"
	fixture.Exchange = refusal("Error: image used by " + strings.Repeat("a1", 32) + ": image is in use by a container")
	run := refusal("Error: image used by " + strings.Repeat("b2", 32) + ": image is in use by a container")
	check := func(cmp enginecontract.Comparer) *recT {
		rt := &recT{TB: t}
		rt.run(func() {
			verifySession().VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "rmi-used", run, enginefixture.Redaction{}, cmp)
		})
		return rt
	}

	if rt := check(enginecontract.SameUpToContainerIDs()); !rt.quiet() {
		t.Errorf("a refusal that names the other container was reported: %s", rt.text())
	}
	if rt := check(nil); len(rt.errs) == 0 {
		t.Error("with no Comparer the two agree: the test proves nothing")
	}
}
