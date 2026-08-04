package registrymgr

import (
	"context"
	"errors"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// The two packages state the same vocabulary and neither imports the other:
// config owns what a file may say, this package owns what can be detected. The
// duplication is deliberate; this is what stops it from drifting.
func TestTheProviderVocabularyMatchesTheConfig(t *testing.T) {
	pairs := map[string]string{
		ProviderGeneric:     config.ProviderGeneric,
		ProviderNexus:       config.ProviderNexus,
		ProviderHarbor:      config.ProviderHarbor,
		ProviderArtifactory: config.ProviderArtifactory,
		ProviderGitLab:      config.ProviderGitLab,
	}
	for mine, theirs := range pairs {
		if mine != theirs {
			t.Errorf("registrymgr says %q where config says %q", mine, theirs)
		}
	}
	if len(config.Providers()) != len(pairs) {
		t.Errorf("config offers %d providers, this package knows %d", len(config.Providers()), len(pairs))
	}
	for _, p := range config.Providers() {
		if _, ok := pairs[p]; !ok {
			t.Errorf("config offers %q, which no detector can be selected for", p)
		}
	}
}

// stubDetector records whether it was the one chosen.
type stubDetector struct {
	provider string
	called   *bool
	members  []GroupMember
	err      error
}

func (s stubDetector) Provider() string { return s.provider }

func (s stubDetector) DetectGroup(context.Context, RegistryInfo) ([]GroupMember, error) {
	*s.called = true
	return s.members, s.err
}

// withDetectors swaps the global list for the duration of one test.
func withDetectors(t *testing.T, list ...Detector) {
	t.Helper()
	saved := detectors
	detectors = list
	t.Cleanup(func() { detectors = saved })
}

// The whole point of decision F: which detector runs is decided by what the
// entry declares, not by what its URL happens to look like.
func TestTheDeclaredProviderPicksTheDetector(t *testing.T) {
	var nexusCalled, genericCalled bool
	withDetectors(t,
		stubDetector{provider: ProviderNexus, called: &nexusCalled,
			members: []GroupMember{{Alias: "hosted", URL: "https://n.example.com/repository/hosted"}}},
		stubDetector{provider: ProviderGeneric, called: &genericCalled},
	)

	members, err := DetectGroup(context.Background(), RegistryInfo{
		Provider: ProviderNexus,
		URL:      "https://looks-like-nothing.example.com",
	})

	if err != nil {
		t.Fatalf("DetectGroup: %v", err)
	}
	if !nexusCalled {
		t.Error("the declared provider's detector was not the one asked")
	}
	if genericCalled {
		t.Error("the generic detector ran alongside the declared one")
	}
	if len(members) != 1 {
		t.Errorf("Members = %+v, want the one the detector returned", members)
	}
}

// The inverse, and the behaviour that changed: a Nexus-shaped URL that declares
// nothing is no longer probed as Nexus.
func TestANexusShapedURLIsNotProbedUnlessDeclared(t *testing.T) {
	var nexusCalled, genericCalled bool
	withDetectors(t,
		stubDetector{provider: ProviderNexus, called: &nexusCalled},
		stubDetector{provider: ProviderGeneric, called: &genericCalled},
	)

	if _, err := DetectGroup(context.Background(), RegistryInfo{
		URL: "https://nexus.example.com/repository/docker-group",
	}); err != nil {
		t.Fatalf("DetectGroup: %v", err)
	}

	if nexusCalled {
		t.Error("the URL shape alone selected the Nexus detector")
	}
	if !genericCalled {
		t.Error("nothing handled an undeclared registry, not even the fallback")
	}
}

// A provider named in the form but not implemented has to land somewhere, and
// the answer "not a group" is the honest one.
func TestAnUnimplementedProviderFallsToTheGenericDetector(t *testing.T) {
	var genericCalled bool
	withDetectors(t,
		&NexusDetector{},
		stubDetector{provider: ProviderGeneric, called: &genericCalled},
	)

	members, err := DetectGroup(context.Background(), RegistryInfo{
		Provider: ProviderHarbor,
		URL:      "https://harbor.example.com",
	})

	if err != nil {
		t.Fatalf("DetectGroup: %v", err)
	}
	if !genericCalled {
		t.Error("a provider with no detector reached nothing at all")
	}
	if members != nil {
		t.Errorf("Members = %+v, want nothing discovered without a manager to ask", members)
	}
}

// Registration order used to decide everything. It must not any more, or adding
// a second detector would be a source of order-dependent bugs.
func TestRegistrationOrderDoesNotDecide(t *testing.T) {
	var called bool
	generic := stubDetector{provider: ProviderGeneric, called: new(bool)}
	nexus := stubDetector{provider: ProviderNexus, called: &called}

	for _, order := range [][]Detector{{generic, nexus}, {nexus, generic}} {
		called = false
		withDetectors(t, order...)
		if _, err := DetectGroup(context.Background(), RegistryInfo{Provider: ProviderNexus}); err != nil {
			t.Fatalf("DetectGroup: %v", err)
		}
		if !called {
			t.Errorf("the Nexus detector was skipped when registered at position %d", len(order))
		}
	}
}

// An error from the chosen detector is the caller's to see: the browser shows
// it rather than reporting an empty group.
func TestTheChosenDetectorsErrorIsReturned(t *testing.T) {
	want := errors.New("nexus unreachable")
	withDetectors(t, stubDetector{provider: ProviderNexus, called: new(bool), err: want})

	if _, err := DetectGroup(context.Background(), RegistryInfo{Provider: ProviderNexus}); !errors.Is(err, want) {
		t.Errorf("err = %v, want the detector's own", err)
	}
}

// A build with no detectors registered at all must answer rather than panic.
func TestNoDetectorsMeansNoGroup(t *testing.T) {
	withDetectors(t)

	members, err := DetectGroup(context.Background(), RegistryInfo{Provider: ProviderNexus})

	if err != nil || members != nil {
		t.Errorf("DetectGroup = %+v, %v, want nothing and no error", members, err)
	}
}

// The two detectors the binary actually ships have to be reachable, or every
// test above is checking stubs.
func TestTheShippedDetectorsAreRegistered(t *testing.T) {
	found := map[string]bool{}
	for _, d := range detectors {
		found[d.Provider()] = true
	}
	for _, want := range []string{ProviderNexus, ProviderGeneric} {
		if !found[want] {
			t.Errorf("no detector is registered for %q", want)
		}
	}
}

func TestTheGenericDetectorDiscoversNothing(t *testing.T) {
	members, err := GenericDetector{}.DetectGroup(context.Background(), RegistryInfo{
		Provider: ProviderGeneric,
		URL:      "https://registry.example.com/repository/docker-group",
	})

	if err != nil {
		t.Fatalf("DetectGroup: %v", err)
	}
	if members != nil {
		t.Errorf("Members = %+v, want nothing guessed from the URL", members)
	}
}

func TestTheNexusDetectorSaysWhichProviderItServes(t *testing.T) {
	if got := (&NexusDetector{}).Provider(); got != ProviderNexus {
		t.Errorf("Provider() = %q, want %q", got, ProviderNexus)
	}
}
